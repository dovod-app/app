package service

import (
	"context"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
)

// Access decides what the caller may do to a research. It is the only place in
// the product where that decision is made, and every service is handed one at
// construction so the check cannot be forgotten on the next method: a service
// built without it fails on its first call rather than quietly letting
// everyone through.
//
// Access is the team's, not the user's. `researches.user_id` records who
// created a research and is never consulted here — two sources of truth for
// who may do what is how this class of bug happens.
//
// Two rules hold across every method:
//
//   - **Nobody in the context means no check.** That is the local
//     single-binary case (`auth_enabled: false`), where there are no users to
//     be wrong about. It is the behaviour this product has always had; turning
//     auth on is what takes it away.
//   - **A non-member gets ErrNotFound, never a refusal.** Confirming that a
//     research exists is itself information about someone else's work. A
//     member who merely lacks the right gets ErrForbidden, because hiding a
//     research from someone who can already read it protects nothing.
//
// There is deliberately no owner-only tier here. Everything that needs one —
// moving a research, managing who can see it — is a decision about the *team*,
// and TeamService.requireRole makes it.
type Access struct {
	teams *storage.TeamRepository
}

func NewAccess(teams *storage.TeamRepository) *Access {
	return &Access{teams: teams}
}

// Read allows any member of the owning team.
func (a *Access) Read(ctx context.Context, researchID string) error {
	_, err := a.Role(ctx, researchID)
	return err
}

// Write allows an editor or an owner to change content.
func (a *Access) Write(ctx context.Context, researchID string) error {
	// A share link is read-only, and says so here rather than relying on the
	// viewer role it resolves to. The rule "a share can never write" is the one
	// this feature rests on, and it should be one greppable line, not a
	// consequence of a role lookup two functions away.
	if auth.ShareFromContext(ctx) != nil {
		return ErrForbidden
	}
	role, err := a.Role(ctx, researchID)
	if err != nil {
		return err
	}
	// An empty role reaches here only when there is no authenticated caller,
	// which Role has already allowed.
	if role != "" && !role.CanWrite() {
		return ErrForbidden
	}
	return nil
}

// Admin allows only an owner. It is the gate for the operations that dispose of
// a research rather than change what is inside it — deleting it, and moving it
// between teams.
//
// Separate from Write because an editor is trusted with the contents and not
// with the container: an editor who deletes a research destroys work belonging
// to everyone else in the team, and no amount of confirmation dialog makes that
// their decision to take.
func (a *Access) Admin(ctx context.Context, researchID string) error {
	// A share is read-only, said here rather than left to fall out of the
	// viewer role it resolves to — the same one greppable line Write carries.
	if auth.ShareFromContext(ctx) != nil {
		return ErrForbidden
	}
	role, err := a.Role(ctx, researchID)
	if err != nil {
		return err
	}
	// An empty role means there is no authenticated caller at all, which Role
	// has already allowed: the local single-binary mode, where there are no
	// roles to be owner of.
	if role != "" && role != domain.TeamOwner {
		return ErrForbidden
	}
	return nil
}

// Role resolves the caller's role, or ErrNotFound when the research is absent
// or belongs to a team they are not in. An empty role with a nil error means
// there is no authenticated caller at all.
//
// Existence is checked even then. "No check" is about permission, not about
// whether the thing is there: without this, a bad id in local mode would slip
// past every service and surface as a foreign-key error from the database.
func (a *Access) Role(ctx context.Context, researchID string) (domain.TeamRole, error) {
	// A share is decided before anything else, and before the no-authenticated-
	// caller rule in particular. That rule exists for the local single-binary
	// case and lets every read through; a share visitor is the opposite
	// situation — a stranger on a public URL — and reaching that rule would
	// hand them the whole database.
	if sh := auth.ShareFromContext(ctx); sh != nil {
		return a.shareRole(sh, researchID)
	}
	return a.roleFor(ctx, auth.UserIDFromContext(ctx), researchID)
}

// shareRole is the whole of what a share link may do: read one research, as a
// viewer, and nothing else. Anything outside that research is ErrNotFound —
// confirming that another research exists is the information a share must not
// carry, and it is what makes a `[[R2:E5]]` reference render as inert text.
func (a *Access) shareRole(sh *auth.Share, researchID string) (domain.TeamRole, error) {
	if researchID == "" || researchID != sh.ResearchID {
		return "", ErrNotFound
	}
	return domain.TeamViewer, nil
}

// CanReadResearch answers for a user who is not in the context.
//
// The WebSocket hub has an id and no request, and it must reach the same
// verdict as every HTTP read — so it asks the same function rather than
// growing a second copy of the rule that could drift from this one.
func (a *Access) CanReadResearch(ctx context.Context, userID, researchID string) bool {
	if userID == "" {
		return false
	}
	_, err := a.roleFor(ctx, userID, researchID)
	return err == nil
}

// IsTeamMember answers the team half of the same question, for the events
// that are about a team rather than about a research.
func (a *Access) IsTeamMember(ctx context.Context, userID, teamID string) bool {
	if userID == "" || teamID == "" {
		return false
	}
	_, ok, err := a.teams.FindRole(ctx, teamID, userID)
	return err == nil && ok
}

func (a *Access) roleFor(ctx context.Context, uid, researchID string) (domain.TeamRole, error) {
	role, teamID, exists, err := a.teams.ResearchRole(ctx, researchID, uid)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrNotFound
	}
	if uid == "" {
		return "", nil
	}
	// The local team holds researches created with no user at all, and it has
	// no members. Leaving them to the membership rule would strand them behind
	// a team nobody can join; granting the caller ownership would let the
	// first person to press "move to team" take them from everyone else, which
	// is a capability that did not exist before teams.
	//
	// So: readable, and no more. That is a tightening of what an ownerless
	// research allowed before — it was writable too — and the safe direction
	// to be wrong in. The first registration still claims them into a real
	// team, which is the path out.
	if teamID == domain.LocalTeamID {
		return domain.TeamViewer, nil
	}
	if role == "" || !domain.ValidTeamRole(role) {
		return "", ErrNotFound
	}
	return role, nil
}

// RoleOrEmpty is Role without the error, for decorating a response with what
// the caller may do. A failure to resolve is reported as no role, which the
// frontend renders as read-only — the safe direction to be wrong in.
func (a *Access) RoleOrEmpty(ctx context.Context, researchID string) domain.TeamRole {
	role, err := a.Role(ctx, researchID)
	if err != nil {
		return ""
	}
	if role == "" {
		// Local mode: there is no team to have a role in, and everything is
		// permitted, so the UI must not draw a read-only screen.
		return domain.TeamOwner
	}
	return role
}
