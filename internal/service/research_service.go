package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/google/uuid"
)

type CreateResearchRequest struct {
	Name        string
	Description string
	Goal        string
	Tags        []string
	Sections    []CreateSectionRequest
	// TeamID puts the research in a team other than the creator's personal
	// one. Empty is the ordinary case and the only one a solo user ever hits.
	TeamID string
}

type CreateSectionRequest struct {
	Name        string
	DisplayName string
	Description string
	Position    int
	// Instruction says how to write a document in this section. Carried on
	// creation for the same reason FieldSpec is: without it an exported
	// research comes back with its writing conventions dropped, and the next
	// eighteen documents are again written from scratch.
	//
	// It is not exposed on any create-a-section surface a person or an agent
	// drives — section_update and PUT /api/sections/{id} are where an
	// instruction is written. This field exists so a round-trip keeps one.
	Instruction string
	// FieldSpec declares what documents in this section record. Almost always
	// empty — most sections are topics, not document classes — and carried here
	// so an imported research arrives with the declaration its documents were
	// written against rather than with values nothing explains.
	FieldSpec []domain.FieldSpec
}

type UpdateResearchRequest struct {
	Name        *string
	Description *string
	Goal        *string
	Status      *domain.ResearchStatus
	Tags        []string
	AddMemory   *string // append single entry
	SessionID   string  // optional research session for add_memory
}

type ResearchService struct {
	researches *storage.ResearchRepository
	sections   *storage.SectionRepository
	teams      *storage.TeamRepository
	access     *Access
	events     EventNotifier
	log        *slog.Logger
}

func NewResearchService(researches *storage.ResearchRepository, sections *storage.SectionRepository, teams *storage.TeamRepository, access *Access, events EventNotifier, log *slog.Logger) *ResearchService {
	return &ResearchService{researches: researches, sections: sections, teams: teams, access: access, events: events, log: log}
}

func (s *ResearchService) Create(ctx context.Context, req CreateResearchRequest) (*domain.Research, []*domain.Section, error) {
	teamID, err := s.resolveCreateTeam(ctx, req.TeamID)
	if err != nil {
		return nil, nil, err
	}

	research := &domain.Research{
		ID:          uuid.New().String(),
		UserID:      auth.UserIDFromContext(ctx),
		TeamID:      teamID,
		Name:        normalizeTitle(req.Name),
		Description: normalizeContent(req.Description),
		Goal:        normalizeContent(req.Goal),
		Status:      domain.ResearchActive,
		Memory:      domain.Memory{},
		Tags:        req.Tags,
	}
	if research.Tags == nil {
		research.Tags = []string{}
	}

	if err := s.researches.Create(ctx, research); err != nil {
		return nil, nil, fmt.Errorf("create research: %w", err)
	}

	var sections []*domain.Section
	for _, sec := range req.Sections {
		// The drop is reported by the importer, which is the only caller that
		// can name the file it came from; here it is the last guard.
		instruction, _ := validInstruction(sec.Instruction)
		section := &domain.Section{
			ID:          uuid.New().String(),
			ResearchID:  research.ID,
			Name:        normalizeTitle(sec.Name),
			DisplayName: normalizeTitle(sec.DisplayName),
			Description: normalizeContent(sec.Description),
			Status:      domain.SectionDraft,
			Position:    sec.Position,
			Instruction: instruction,
			FieldSpec:   validFieldSpec(sec.FieldSpec),
			SpecVersion: specVersionFor(sec.FieldSpec),
		}
		if err := s.sections.Create(ctx, section); err != nil {
			return nil, nil, fmt.Errorf("create section %s: %w", sec.Name, err)
		}
		sections = append(sections, section)
	}

	s.decorate(ctx, research)
	emit(ctx, s.events, Event{Type: "research.created", ResearchID: research.ID, EntityID: research.ID, Entity: "research"})
	return research, sections, nil
}

// resolveCreateTeam decides which team a new research lands in.
//
// A research with no team would be reachable by nobody once auth is on, so
// there is no "unassigned" outcome here: either the caller named a team they
// may write to, or it goes to their personal one.
func (s *ResearchService) resolveCreateTeam(ctx context.Context, requested string) (string, error) {
	uid := auth.UserIDFromContext(ctx)
	if uid == "" {
		if s.access.AccountsEnabled() {
			// An instance with accounts has no anonymous author. Letting the
			// research through would file it in the local team, where the
			// person who asked for it could not read it back.
			return "", ErrNoAuth
		}
		// Local mode: no users, one team, everything in it.
		return domain.LocalTeamID, nil
	}

	if requested != "" {
		role, ok, err := s.teams.FindRole(ctx, requested, uid)
		if err != nil {
			return "", fmt.Errorf("find team role: %w", err)
		}
		if !ok {
			return "", ErrNotFound
		}
		if !role.CanWrite() {
			return "", ErrForbidden
		}
		return requested, nil
	}

	team, err := s.teams.FindPersonal(ctx, uid)
	if err != nil {
		return "", fmt.Errorf("find personal team: %w", err)
	}
	if team == nil {
		// Every user gets one at registration and the migration backfilled the
		// rest, so this is a broken account rather than a missing feature.
		return "", fmt.Errorf("user %s has no personal team", uid)
	}
	return team.ID, nil
}

// decorate fills the per-request fields — the owning team's name and what the
// caller may do — so a reader can tell whose team a research is in and the UI
// can hide controls instead of letting them fail on click.
func (s *ResearchService) decorate(ctx context.Context, research *domain.Research) {
	if research == nil {
		return
	}
	research.Role = s.access.RoleOrEmpty(ctx, research.ID)
	if research.TeamID == "" {
		return
	}
	if team, err := s.teams.FindByID(ctx, research.TeamID); err == nil && team != nil {
		research.TeamName = team.Name
		research.TeamPersonal = team.Personal
	}
}

func (s *ResearchService) Get(ctx context.Context, id string) (*domain.Research, error) {
	research, err := s.researches.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find research: %w", err)
	}
	// If not found by UUID, try by short code
	if research == nil && isCode(id) {
		research, err = s.researches.FindByCode(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("find research by code: %w", err)
		}
	}
	if research == nil {
		return nil, ErrNotFound
	}
	// Membership, not ownership: a colleague in the same team sees this, and
	// the creator of a research they were since removed from does not.
	if err := s.access.Read(ctx, research.ID); err != nil {
		return nil, err
	}
	s.decorate(ctx, research)
	redactForShare(ctx, research)
	return research, nil
}

// redactForShare strips the fields a share visitor must never see.
//
// It lives here, on the one function every reader of a research goes through —
// the page, both exports and the portable dump all call Get — rather than in
// each of them. Memory holds the agent's working notes about
// how to conduct the research; they are not a result, and their author did not
// choose to publish them by sending someone a link to the findings.
//
// The team fields go too. They are not secret to a member, but they name people
// and workspaces to somebody who is neither, and a share is about one research
// rather than about the organisation behind it.
func redactForShare(ctx context.Context, research *domain.Research) {
	if research == nil || auth.ShareFromContext(ctx) == nil {
		return
	}
	// An empty slice, not nil: `"memory": null` is not a list, and the frontend
	// renders one either way.
	research.Memory = domain.Memory{}
	research.UserID = ""
	research.TeamID = ""
	research.TeamName = ""
	research.TeamPersonal = false
	// The role survives, and is a viewer's. It is what the UI reads to decide
	// whether to draw an editing control at all.
	research.Role = domain.TeamViewer
	// Which methodology a team follows is working process, exactly like the
	// memory above it. The slug is Slugify(a name the team chose), so it
	// reads back as that name — "acme-q4-layoff-diligence" is not something a
	// client holding a read-only link should learn. The version leaks too: for
	// a research started from a shipped template it is a number about our
	// releases, not about them.
	research.TemplateSlug = ""
	research.TemplateVersion = 0
}

// ResolveID resolves a UUID or short code to a research UUID.
func (s *ResearchService) ResolveID(ctx context.Context, idOrCode string) (string, error) {
	r, err := s.Get(ctx, idOrCode)
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

// List returns the researches of every team the caller belongs to. Filtering
// by creator instead would hide a colleague's work in a shared team, which is
// the whole point of having one.
func (s *ResearchService) List(ctx context.Context, filter storage.ResearchFilter) ([]*domain.Research, error) {
	// A share reaches one research and has no list. Without this the filter
	// below would be left unset — there is no user to scope by — and the answer
	// would be every research on the server.
	if auth.ShareFromContext(ctx) != nil {
		return []*domain.Research{}, nil
	}

	uid := auth.UserIDFromContext(ctx)
	if uid == "" && s.access.AccountsEnabled() {
		// Same reason as the share above, and the same shape of mistake: with
		// accounts on there is nobody to scope by, so the filter would be left
		// unset and the answer would be every research on the server. The
		// caller who reaches here is an anonymous stdio session — the one
		// transport that authenticates nobody — and it sees what any stranger
		// sees, which is nothing.
		return []*domain.Research{}, nil
	}
	if uid != "" {
		filter.MemberOf = &uid
	}

	researches, err := s.researches.FindAll(ctx, filter)
	if err != nil {
		return nil, err
	}
	if researches == nil {
		// An empty list is now an ordinary state — a new member of a team that
		// holds nothing yet — and `"data": null` is not a list.
		researches = []*domain.Research{}
	}
	if uid == "" || len(researches) == 0 {
		return researches, nil
	}

	// One query for the caller's teams rather than two per research: the list
	// is the busiest read in the product.
	teams, err := s.teams.ListByUser(ctx, uid)
	if err != nil {
		return researches, nil
	}
	byID := make(map[string]*domain.Team, len(teams))
	for _, t := range teams {
		byID[t.ID] = t
	}
	for _, r := range researches {
		if t := byID[r.TeamID]; t != nil {
			r.TeamName = t.Name
			r.TeamPersonal = t.Personal
			r.Role = t.Role
		}
	}
	return researches, nil
}

func (s *ResearchService) Update(ctx context.Context, id string, req UpdateResearchRequest) (*domain.Research, error) {
	research, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.access.Write(ctx, research.ID); err != nil {
		return nil, err
	}
	// A memory-only request must not write a stale metadata snapshot back.
	if req.AddMemory != nil && req.Name == nil && req.Description == nil && req.Goal == nil && req.Status == nil && req.Tags == nil {
		if _, err := s.AddMemory(ctx, research.ID, *req.AddMemory, req.SessionID); err != nil {
			return nil, err
		}
		return s.Get(ctx, research.ID)
	}

	if req.Name != nil {
		research.Name = normalizeTitle(*req.Name)
	}
	if req.Description != nil {
		research.Description = normalizeContent(*req.Description)
	}
	if req.Goal != nil {
		research.Goal = normalizeContent(*req.Goal)
	}
	if req.Status != nil {
		research.Status = *req.Status
	}
	if req.Tags != nil {
		research.Tags = req.Tags
	}
	var memoryItem *domain.MemoryItem
	if req.AddMemory != nil {
		memoryItem, err = s.prepareMemory(ctx, research.ID, *req.AddMemory, req.SessionID)
		if err != nil {
			return nil, err
		}
	}

	if err := s.researches.UpdateWithMemory(ctx, research, memoryItem); err != nil {
		return nil, fmt.Errorf("update research: %w", err)
	}

	emit(ctx, s.events, Event{Type: "research.updated", ResearchID: research.ID, EntityID: research.ID, Entity: "research"})
	return s.Get(ctx, research.ID)
}

func (s *ResearchService) AddSection(ctx context.Context, researchID string, req CreateSectionRequest) (*domain.Section, error) {
	if err := s.access.Write(ctx, researchID); err != nil {
		return nil, err
	}

	// Check for duplicate name
	existing, err := s.sections.FindByResearchAndName(ctx, researchID, req.Name)
	if err != nil {
		return nil, fmt.Errorf("check section name: %w", err)
	}
	if existing != nil {
		return nil, ErrDuplicateSectionName
	}

	instruction, _ := validInstruction(req.Instruction)
	section := &domain.Section{
		ID:          uuid.New().String(),
		ResearchID:  researchID,
		Name:        normalizeTitle(req.Name),
		DisplayName: normalizeTitle(req.DisplayName),
		Description: normalizeContent(req.Description),
		Status:      domain.SectionDraft,
		Position:    req.Position,
		Instruction: instruction,
		FieldSpec:   validFieldSpec(req.FieldSpec),
		SpecVersion: specVersionFor(req.FieldSpec),
	}

	if err := s.sections.Create(ctx, section); err != nil {
		return nil, fmt.Errorf("create section: %w", err)
	}

	emit(ctx, s.events, Event{Type: "section.created", ResearchID: researchID, EntityID: section.ID, Entity: "section"})
	return section, nil
}

// validFieldSpec keeps a declaration only when it is one this product would
// have accepted.
//
// Creation is the one path that takes a spec from somewhere else — an import,
// or a restore of a file somebody edited — and refusing the whole research
// because one field is malformed would be the wrong trade: the documents are
// the point, and a declaration that cannot be honoured is better dropped than
// enforced half-way. A rejected spec leaves the section a plain topic, which is
// what it will look like until somebody declares one.
func validFieldSpec(specs []domain.FieldSpec) []domain.FieldSpec {
	if len(specs) == 0 {
		return nil
	}
	if errs := domain.ValidateFieldSpecs(specs); len(errs) > 0 {
		return nil
	}
	return specs
}

// validInstruction is validFieldSpec's rule applied to the section's
// instruction: creation is the one path that takes one from somewhere else — an
// import, or a restore of a file somebody edited — and refusing the whole
// research over a note that runs forty characters long would be the wrong
// trade. section_update refuses instead, because there a person is looking at
// the text and can shorten it.
//
// Dropped rather than truncated, on every path. A truncated instruction is the
// worse outcome: it reads as a complete rule whose second half happens to be
// missing, and the agent re-reading it on every write cannot tell. An absent
// one is at least honestly absent — and `ok` is false so the caller can say so
// out loud instead of leaving a silently empty field.
func validInstruction(s string) (value string, ok bool) {
	s = strings.TrimSpace(normalizeContent(s))
	if utf8.RuneCountInString(s) > domain.SectionInstructionMax {
		return "", false
	}
	return s, true
}

// InstructionTooLong reports whether an instruction taken from a file would be
// dropped on import. It exists so the importer can warn about the loss rather
// than let the section arrive quietly without its convention.
func InstructionTooLong(s string) bool {
	_, ok := validInstruction(s)
	return !ok
}

func specVersionFor(specs []domain.FieldSpec) int {
	if len(validFieldSpec(specs)) == 0 {
		return 0
	}
	return 1
}
