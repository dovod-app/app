package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/google/uuid"
)

var (
	// ErrLastOwner guards the only irreversible mistake in team management: a
	// team with no owner can never be administered again, by anyone.
	ErrLastOwner = errors.New("a team must keep at least one owner")
	// ErrPersonalTeam is for the operations a personal team does not have. It
	// is where a solo user's researches live; deleting or leaving it would
	// strand them.
	ErrPersonalTeam  = errors.New("a personal team cannot be renamed, deleted or left")
	ErrTeamNotEmpty  = errors.New("move or delete the team's researches first")
	ErrInviteInvalid = errors.New("this invitation is no longer valid")
	ErrAlreadyMember = errors.New("already a member of this team")
	ErrNotMember     = errors.New("not a member of this team")
	ErrInvalidRole   = errors.New("role must be viewer, editor or owner")
	ErrTeamNameEmpty = errors.New("a team needs a name")
	// ErrNoAuth is for the team operations that are meaningless without an
	// identity — every one of them, since a team is a set of people.
	ErrNoAuth = errors.New("sign in to manage teams")
)

// inviteTTL is how long an invitation link works. Long enough to be passed
// along by hand over a weekend, short enough that a link pasted into a channel
// two years ago is not a way in.
const inviteTTL = 14 * 24 * time.Hour

// invitePrefix marks a token as this product's, so one pasted into the wrong
// field is recognisable rather than mysterious.
const invitePrefix = "mri_"

type TeamService struct {
	teams      *storage.TeamRepository
	invites    *storage.TeamInviteRepository
	users      *storage.UserRepository
	researches *storage.ResearchRepository
	events     EventNotifier
	log        *slog.Logger
	// access is here for one question: whether this instance has accounts.
	// An anonymous caller is the owner of a local single-binary instance and a
	// stranger on one with accounts, and this service is the only other place
	// that has to tell them apart. Every research-scoped decision still belongs
	// to Access itself.
	access *Access
}

func NewTeamService(
	teams *storage.TeamRepository,
	invites *storage.TeamInviteRepository,
	users *storage.UserRepository,
	researches *storage.ResearchRepository,
	access *Access,
	events EventNotifier,
	log *slog.Logger,
) *TeamService {
	return &TeamService{teams: teams, invites: invites, users: users, researches: researches,
		access: access, events: events, log: log}
}

// --- Teams ---

// AccountsEnabled says whether this instance has user accounts.
//
// It exists for the one caller that has to explain a refusal rather than just
// return it: `team_list` answers ErrNoAuth with "this server runs without
// accounts", which was the only reason for it until an unauthenticated caller
// on an instance *with* accounts started getting the same error — and being
// told the opposite of the truth.
func (s *TeamService) AccountsEnabled() bool {
	return s.access != nil && s.access.AccountsEnabled()
}

func (s *TeamService) List(ctx context.Context) ([]*domain.Team, error) {
	uid := auth.UserIDFromContext(ctx)
	if uid == "" {
		return nil, ErrNoAuth
	}
	teams, err := s.teams.ListByUser(ctx, uid)
	if err != nil {
		return nil, err
	}
	if teams == nil {
		teams = []*domain.Team{}
	}
	return teams, nil
}

// Get returns a team the caller belongs to, with their role attached.
func (s *TeamService) Get(ctx context.Context, teamID string) (*domain.Team, error) {
	role, err := s.requireRole(ctx, teamID, domain.TeamViewer)
	if err != nil {
		return nil, err
	}
	team, err := s.teams.FindByID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, ErrNotFound
	}
	team.Role = role
	return team, nil
}

func (s *TeamService) Create(ctx context.Context, name string) (*domain.Team, error) {
	uid := auth.UserIDFromContext(ctx)
	if uid == "" {
		return nil, ErrNoAuth
	}
	name = normalizeTitle(name)
	if name == "" {
		return nil, ErrTeamNameEmpty
	}

	team := &domain.Team{
		ID:        uuid.New().String(),
		Name:      name,
		CreatedBy: uid,
	}
	if err := s.teams.CreateWithOwner(ctx, team, uid); err != nil {
		return nil, err
	}
	team.MemberCount = 1
	s.notify(ctx, "team.created", team.ID)
	return team, nil
}

func (s *TeamService) Rename(ctx context.Context, teamID, name string) (*domain.Team, error) {
	if _, err := s.requireRole(ctx, teamID, domain.TeamOwner); err != nil {
		return nil, err
	}
	team, err := s.teams.FindByID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, ErrNotFound
	}
	if team.Personal {
		return nil, ErrPersonalTeam
	}
	name = normalizeTitle(name)
	if name == "" {
		return nil, ErrTeamNameEmpty
	}
	if err := s.teams.Rename(ctx, teamID, name); err != nil {
		return nil, err
	}
	team.Name = name
	team.Role = domain.TeamOwner
	s.notify(ctx, "team.updated", teamID)
	return team, nil
}

// Delete removes a team once its researches have been moved elsewhere. The
// schema would cascade them away instead; refusing is the kinder answer,
// because "delete team" does not read as "delete everything in it".
func (s *TeamService) Delete(ctx context.Context, teamID string) error {
	if _, err := s.requireRole(ctx, teamID, domain.TeamOwner); err != nil {
		return err
	}
	team, err := s.teams.FindByID(ctx, teamID)
	if err != nil {
		return err
	}
	if team == nil {
		return ErrNotFound
	}
	if team.Personal {
		return ErrPersonalTeam
	}
	n, err := s.researches.CountByTeam(ctx, teamID)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrTeamNotEmpty
	}
	// Read before the delete. Membership rows cascade away with the team, and
	// a team event is delivered by asking who is in the team — so announcing
	// this afterwards announces it to nobody, which is precisely the audience
	// that needs it.
	members, err := s.teams.ListMembers(ctx, teamID)
	if err != nil {
		return err
	}

	if err := s.teams.Delete(ctx, teamID); err != nil {
		return err
	}
	s.notify(ctx, "team.deleted", teamID)
	for _, m := range members {
		s.revoked(ctx, Event{
			Entity:   "team",
			EntityID: teamID,
			Name:     team.Name,
			Reason:   "team_deleted",
		}, m.UserID)
	}
	return nil
}

// --- Members ---

func (s *TeamService) Members(ctx context.Context, teamID string) ([]*domain.TeamMember, error) {
	if _, err := s.requireRole(ctx, teamID, domain.TeamViewer); err != nil {
		return nil, err
	}
	members, err := s.teams.ListMembers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if members == nil {
		members = []*domain.TeamMember{}
	}
	return members, nil
}

func (s *TeamService) UpdateRole(ctx context.Context, teamID, userID string, role domain.TeamRole) error {
	if _, err := s.requireRole(ctx, teamID, domain.TeamOwner); err != nil {
		return err
	}
	if !domain.ValidTeamRole(role) {
		return ErrInvalidRole
	}

	current, ok, err := s.teams.FindRole(ctx, teamID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotMember
	}
	if current == role {
		return nil
	}
	if current == domain.TeamOwner {
		if err := s.guardLastOwner(ctx, teamID); err != nil {
			return err
		}
	}

	if err := s.teams.UpdateRole(ctx, teamID, userID, role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotMember
		}
		return err
	}
	s.notify(ctx, "team.member_role_changed", teamID)
	// The team event says the team changed; it does not say whose rights did.
	// An owner demoted to viewer keeps Edit and Delete on screen until they
	// press one and collect a 403, so the person affected is told directly and
	// re-reads their role. The new role is deliberately not in the event: the
	// client asks the API rather than trusting a value pushed at it.
	emit(ctx, s.events, Event{
		Type:         "access.changed",
		Entity:       "team",
		EntityID:     teamID,
		Reason:       "role_changed",
		TargetUserID: userID,
	})
	return nil
}

// RemoveMember drops someone from a team. It doubles as "leave team" when the
// caller names themselves, which is the same operation with a different
// permission: anyone may leave, only an owner may remove someone else.
func (s *TeamService) RemoveMember(ctx context.Context, teamID, userID string) error {
	uid := auth.UserIDFromContext(ctx)
	if uid == "" {
		return ErrNoAuth
	}

	leaving := uid == userID
	needed := domain.TeamOwner
	if leaving {
		needed = domain.TeamViewer
	}
	if _, err := s.requireRole(ctx, teamID, needed); err != nil {
		return err
	}

	team, err := s.teams.FindByID(ctx, teamID)
	if err != nil {
		return err
	}
	if team == nil {
		return ErrNotFound
	}
	if team.Personal {
		return ErrPersonalTeam
	}

	current, ok, err := s.teams.FindRole(ctx, teamID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotMember
	}
	if current == domain.TeamOwner {
		if err := s.guardLastOwner(ctx, teamID); err != nil {
			return err
		}
	}

	if err := s.teams.RemoveMember(ctx, teamID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotMember
		}
		return err
	}
	s.notify(ctx, "team.member_removed", teamID)
	if leaving {
		// They chose this and have already been told so by the button they
		// pressed. An eviction notice on top of their own confirmation reads as
		// two contradictory accounts of one action.
		return nil
	}
	// The team event above reaches the members who remain. It cannot reach the
	// one person who most needs it, because they are no longer a member — so
	// they get told directly.
	s.revoked(ctx, Event{
		Entity:   "team",
		EntityID: teamID,
		Name:     team.Name,
		Reason:   "removed_from_team",
	}, userID)
	return nil
}

// guardLastOwner refuses the change that would leave a team unadministrable.
func (s *TeamService) guardLastOwner(ctx context.Context, teamID string) error {
	owners, err := s.teams.CountOwners(ctx, teamID)
	if err != nil {
		return err
	}
	if owners <= 1 {
		return ErrLastOwner
	}
	return nil
}

// --- Invitations ---

// InviteResult carries the one and only sighting of the token. It is hashed at
// rest, so an owner who closes the dialog without copying the link has to
// issue a new invitation — the same trade the API keys already make.
type InviteResult struct {
	Invite *domain.TeamInvite `json:"invite"`
	Token  string             `json:"token"`
}

func (s *TeamService) Invites(ctx context.Context, teamID string) ([]*domain.TeamInvite, error) {
	if _, err := s.requireRole(ctx, teamID, domain.TeamOwner); err != nil {
		return nil, err
	}
	invites, err := s.invites.ListOpenByTeam(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if invites == nil {
		invites = []*domain.TeamInvite{}
	}
	return invites, nil
}

func (s *TeamService) CreateInvite(ctx context.Context, teamID, email string, role domain.TeamRole) (*InviteResult, error) {
	uid := auth.UserIDFromContext(ctx)
	if _, err := s.requireRole(ctx, teamID, domain.TeamOwner); err != nil {
		return nil, err
	}

	// A personal team refuses every removal, so letting someone in would be a
	// one-way door: neither the owner nor the joiner could undo it.
	team, err := s.teams.FindByID(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if team == nil {
		return nil, ErrNotFound
	}
	if team.Personal {
		return nil, ErrPersonalTeam
	}
	if role == "" {
		role = domain.TeamViewer
	}
	if !domain.ValidTeamRole(role) {
		return nil, ErrInvalidRole
	}

	email = strings.TrimSpace(strings.ToLower(email))

	// Inviting someone who is already here is a mistake worth naming rather
	// than a link that will dead-end when they follow it.
	if email != "" {
		if user, err := s.users.FindByEmail(ctx, email); err == nil && user != nil {
			if _, ok, err := s.teams.FindRole(ctx, teamID, user.ID); err == nil && ok {
				return nil, ErrAlreadyMember
			}
		}
	}

	token := invitePrefix + strings.ReplaceAll(uuid.New().String(), "-", "") +
		strings.ReplaceAll(uuid.New().String(), "-", "")
	invite := &domain.TeamInvite{
		ID:        uuid.New().String(),
		TeamID:    teamID,
		Email:     email,
		Role:      role,
		InvitedBy: uid,
		ExpiresAt: time.Now().UTC().Add(inviteTTL),
	}
	if err := s.invites.Create(ctx, invite, auth.HashAPIKey(token)); err != nil {
		return nil, err
	}
	s.notify(ctx, "team.invited", teamID)
	return &InviteResult{Invite: invite, Token: token}, nil
}

func (s *TeamService) RevokeInvite(ctx context.Context, inviteID string) error {
	invite, err := s.invites.FindByID(ctx, inviteID)
	if err != nil {
		return err
	}
	if invite == nil {
		return ErrNotFound
	}
	if _, err := s.requireRole(ctx, invite.TeamID, domain.TeamOwner); err != nil {
		return err
	}
	if err := s.invites.Revoke(ctx, inviteID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	s.notify(ctx, "team.invite_revoked", invite.TeamID)
	return nil
}

// InviteStatus is why a link cannot be used, or that it can. The recipient's
// next action differs for each, so collapsing them into one 404 would leave
// them with a dead page and no idea what to do.
type InviteStatus string

const (
	InvitePending  InviteStatus = "pending"
	InviteExpired  InviteStatus = "expired"
	InviteRevoked  InviteStatus = "revoked"
	InviteAccepted InviteStatus = "accepted"
	InviteUnknown  InviteStatus = "unknown"
)

// InvitePreview is what the landing page renders before anyone commits to
// anything. It answers deliberately without requiring a session: a recipient
// who is not signed in still has to be told what they are being invited to,
// and by whom, before being asked to create an account.
type InvitePreview struct {
	Status      InviteStatus    `json:"status"`
	TeamID      string          `json:"team_id,omitempty"`
	TeamName    string          `json:"team_name,omitempty"`
	Role        domain.TeamRole `json:"role,omitempty"`
	Email       string          `json:"email,omitempty"`
	InviterName string          `json:"inviter_name,omitempty"`
	// InviterEmail is the only next action an expired link can offer.
	InviterEmail string     `json:"inviter_email,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	// AlreadyMember and EmailMatches are resolved only when the request
	// carries a session, so the page can say "you are already in this team" or
	// "this invitation was sent to someone else" instead of finding out on
	// submit.
	AlreadyMember bool `json:"already_member"`
	EmailMatches  bool `json:"email_matches"`
	SignedIn      bool `json:"signed_in"`
}

// PreviewInvite reads a link without consuming it. It takes no permission at
// all: whoever holds the token is the audience.
func (s *TeamService) PreviewInvite(ctx context.Context, token string) (*InvitePreview, error) {
	invite, err := s.invites.FindByHash(ctx, auth.HashAPIKey(token))
	if err != nil {
		return nil, err
	}
	if invite == nil {
		return &InvitePreview{Status: InviteUnknown}, nil
	}

	preview := &InvitePreview{
		Status:       InvitePending,
		TeamID:       invite.TeamID,
		TeamName:     invite.TeamName,
		Role:         invite.Role,
		Email:        invite.Email,
		InviterName:  invite.InviterName,
		InviterEmail: invite.InviterEmail,
	}
	expires := invite.ExpiresAt
	preview.ExpiresAt = &expires

	switch {
	case invite.AcceptedAt != nil:
		preview.Status = InviteAccepted
	case invite.RevokedAt != nil:
		// Revoking is the owner withdrawing the invitation, most often
		// because it went to the wrong person. Going on naming the team and
		// the sender to whoever holds the link would leave the disclosure
		// standing after the access was taken back.
		return &InvitePreview{Status: InviteRevoked}, nil
	case !invite.ExpiresAt.After(time.Now().UTC()):
		preview.Status = InviteExpired
	}

	if user := auth.UserFromContext(ctx); user != nil {
		preview.SignedIn = true
		preview.EmailMatches = invite.Email == "" || strings.EqualFold(invite.Email, user.Email)
		if _, ok, err := s.teams.FindRole(ctx, invite.TeamID, user.ID); err == nil && ok {
			preview.AlreadyMember = true
		}
	}
	return preview, nil
}

// AcceptResult is what the caller needs to land somewhere useful afterwards.
type AcceptResult struct {
	TeamID   string          `json:"team_id"`
	TeamName string          `json:"team_name"`
	Role     domain.TeamRole `json:"role"`
}

// AcceptInvite joins the signed-in user to the team.
//
// A mismatch between the invited address and the signed-in one is allowed on
// purpose: people are invited at a work address and signed in with a personal
// one all the time, and the token is the capability. The frontend warns; it
// does not block.
func (s *TeamService) AcceptInvite(ctx context.Context, token string) (*AcceptResult, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return nil, ErrNoAuth
	}

	invite, err := s.invites.FindByHash(ctx, auth.HashAPIKey(token))
	if err != nil {
		return nil, err
	}
	if invite == nil {
		return nil, ErrInviteInvalid
	}

	// Membership first, so someone reopening the link they already used is
	// told they are in rather than that their invitation is broken.
	if _, ok, err := s.teams.FindRole(ctx, invite.TeamID, user.ID); err == nil && ok {
		return nil, ErrAlreadyMember
	}
	if !invite.Pending(time.Now().UTC()) {
		return nil, ErrInviteInvalid
	}

	// Consuming the link and joining the team are one act. Split, a failure
	// on the second leaves the invitation spent and the person still outside,
	// with no way back in. The WHERE clause inside still decides the race
	// between two people opening the same link.
	if err := s.teams.AcceptInviteTx(ctx, invite.ID, invite.TeamID, user.ID, invite.Role, invite.InvitedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInviteInvalid
		}
		return nil, err
	}

	s.notify(ctx, "team.member_added", invite.TeamID)
	return &AcceptResult{TeamID: invite.TeamID, TeamName: invite.TeamName, Role: invite.Role}, nil
}

// --- Research transfer ---

// TransferResearch moves a research to another team, which is the only way its
// audience changes. It takes ownership on the way out and write access on the
// way in: handing your work to a team you merely read would be a way to lose
// it, and pushing it into a team you have no say in would be a way to litter.
func (s *TeamService) TransferResearch(ctx context.Context, researchID, targetTeamID string) error {
	research, err := s.checkTransfer(ctx, researchID, targetTeamID)
	if err != nil || research == nil {
		return err
	}
	return s.moveResearch(ctx, research, targetTeamID)
}

// TransferResearches pulls several researches into one team.
//
// It exists because the team page asks the inverse question from the research
// page: not "where should this go" but "what belongs in here". Answering that
// one research at a time meant one request, one failure state and one event per
// row — so moving twelve could half-succeed in eleven different ways, and the
// reader had no single place to be told.
//
// Every research is checked before any is moved. That is not a database
// transaction — these repositories share one connection and no Tx plumbing —
// but it covers the failure that actually happens: a permission the caller
// does not have, or a research that is gone. A move that fails after the checks
// pass stops the run and reports how many had already been made, because
// claiming zero when eight moved would be worse than the partial truth.
//
// Each move still emits its own `research.transferred`, and its own
// `access.revoked` to whoever just lost it. Those are per-research facts and
// collapsing them into a count would leave open tabs showing a research their
// reader can no longer load.
func (s *TeamService) TransferResearches(ctx context.Context, targetTeamID string, researchIDs []string) (int, error) {
	if len(researchIDs) == 0 {
		return 0, nil
	}

	// Deduplicated, because a list arriving from a form can repeat an id, and
	// the second move of the same research is a no-op that would still be
	// counted.
	seen := make(map[string]struct{}, len(researchIDs))
	pending := make([]*domain.Research, 0, len(researchIDs))
	for _, id := range researchIDs {
		if _, done := seen[id]; done {
			continue
		}
		seen[id] = struct{}{}
		research, err := s.checkTransfer(ctx, id, targetTeamID)
		if err != nil {
			return 0, err
		}
		if research != nil {
			pending = append(pending, research)
		}
	}

	moved := 0
	for _, research := range pending {
		if err := s.moveResearch(ctx, research, targetTeamID); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

// checkTransfer answers whether this caller may move this research into that
// team, and returns nil with no error when the research is already there —
// which is a request that has nothing left to do, not a refusal.
func (s *TeamService) checkTransfer(ctx context.Context, researchID, targetTeamID string) (*domain.Research, error) {
	uid := auth.UserIDFromContext(ctx)
	research, err := s.researches.FindByID(ctx, researchID)
	if err != nil {
		return nil, err
	}
	if research == nil {
		return nil, ErrNotFound
	}

	if uid == "" && s.access != nil && s.access.AccountsEnabled() {
		// The last copy of "nobody means no check" in this service, and the one
		// that moves a research between teams. It was unreachable — both routes
		// are accessWrite, so RequireAuth stops an anonymous caller with
		// accounts on, and no MCP tool calls this — but unreachable is a
		// property of today's route table, and this is the exact shape of the
		// bug the rest of this change removes.
		return nil, ErrNoAuth
	}
	if uid != "" {
		if _, err := s.requireRole(ctx, research.TeamID, domain.TeamOwner); err != nil {
			return nil, err
		}
		role, ok, err := s.teams.FindRole(ctx, targetTeamID, uid)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrNotFound
		}
		if !role.CanWrite() {
			return nil, ErrForbidden
		}
	}

	if research.TeamID == targetTeamID {
		return nil, nil
	}
	return research, nil
}

func (s *TeamService) moveResearch(ctx context.Context, research *domain.Research, targetTeamID string) error {
	researchID := research.ID
	// With no caller the role checks above are skipped, so the target still has
	// to be shown to exist — otherwise a typo comes back as a foreign-key
	// failure from the database.
	target, err := s.teams.FindByID(ctx, targetTeamID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrNotFound
	}
	// Who is about to lose it, read before the move: afterwards the old team's
	// claim on this research is gone and there is nothing left to ask.
	losing := s.membersLosingAccess(ctx, research.TeamID, targetTeamID)

	if err := s.researches.SetTeam(ctx, researchID, targetTeamID); err != nil {
		return err
	}
	emit(ctx, s.events, Event{Type: "research.transferred", ResearchID: researchID, EntityID: researchID, Entity: "research"})
	for _, userID := range losing {
		s.revoked(ctx, Event{
			Entity:     "research",
			EntityID:   researchID,
			ResearchID: researchID,
			Name:       research.Name,
			Reason:     "research_transferred",
		}, userID)
	}
	return nil
}

// membersLosingAccess is everyone in `from` who is not also in `to`. Someone in
// both teams keeps the research and must not be told they lost it.
func (s *TeamService) membersLosingAccess(ctx context.Context, from, to string) []string {
	if from == "" || from == to {
		return nil
	}
	oldMembers, err := s.teams.ListMembers(ctx, from)
	if err != nil {
		s.log.Warn("transfer: cannot list the old team's members", "team_id", from, "error", err)
		return nil
	}
	newMembers, err := s.teams.ListMembers(ctx, to)
	if err != nil {
		s.log.Warn("transfer: cannot list the new team's members", "team_id", to, "error", err)
		return nil
	}
	keeps := make(map[string]struct{}, len(newMembers))
	for _, m := range newMembers {
		keeps[m.UserID] = struct{}{}
	}

	var losing []string
	for _, m := range oldMembers {
		if _, ok := keeps[m.UserID]; !ok {
			losing = append(losing, m.UserID)
		}
	}
	return losing
}

// revoked tells one person, and only that person, that something they were
// reading is no longer theirs to read.
//
// It has to be addressed by user id because by the time it is sent the ordinary
// rule already refuses to deliver it — which is exactly why their open tab
// would otherwise go on showing a research that no longer loads.
// The name and the reason travel with it because the recipient cannot look
// either one up any more: the moment this is sent, fetching the thing they lost
// answers 404, and a notice that cannot say what was lost or why is barely
// better than the silence it replaces.
func (s *TeamService) revoked(ctx context.Context, ev Event, userID string) {
	ev.Type = "access.revoked"
	ev.TargetUserID = userID
	emit(ctx, s.events, ev)
}

// requireRole is the team-level counterpart of Access: a non-member is told
// the team does not exist, and a member who is merely too junior is told no.
func (s *TeamService) requireRole(ctx context.Context, teamID string, needed domain.TeamRole) (domain.TeamRole, error) {
	uid := auth.UserIDFromContext(ctx)
	if uid == "" {
		return "", ErrNoAuth
	}
	role, ok, err := s.teams.FindRole(ctx, teamID, uid)
	if err != nil {
		return "", fmt.Errorf("find team role: %w", err)
	}
	if !ok || !domain.ValidTeamRole(role) {
		return "", ErrNotFound
	}
	switch needed {
	case domain.TeamOwner:
		if !role.CanAdmin() {
			return role, ErrForbidden
		}
	case domain.TeamEditor:
		if !role.CanWrite() {
			return role, ErrForbidden
		}
	}
	return role, nil
}

func (s *TeamService) notify(ctx context.Context, eventType, teamID string) {
	emit(ctx, s.events, Event{Type: eventType, EntityID: teamID, Entity: "team"})
}
