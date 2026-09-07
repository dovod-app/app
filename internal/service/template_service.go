package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/google/uuid"
)

var (
	ErrTemplateNameEmpty     = errors.New("name is required")
	ErrTemplateBodyEmpty     = errors.New("body is required: a template is the methodology, and an empty one teaches nothing")
	ErrTemplateWhenEmpty     = errors.New("when_to_use is required: it is what an agent matches on before it has read anything else")
	ErrTemplateCriterionLong = fmt.Errorf("when_to_use and when_not_to_use must be %d characters or fewer", domain.TemplateCriterionMax)
	ErrTemplateBodyLong      = fmt.Errorf("body must be %d characters or fewer", domain.TemplateBodyMax)
	ErrTemplateSlugTaken     = errors.New("a template with that name already exists here: a name derives the slug, so pick a different name — and if the clash is with one that ships with the app, what you want is to fork it")
	ErrTemplateGlobalWrite   = errors.New("templates that ship with the app cannot be changed; editing one makes a copy for your team")
	// ErrTemplateOperatorRequired is the refusal a team gets when it reaches for
	// the server-wide library. Deliberately not ErrForbidden's generic text: the
	// caller is not lacking a role they could be given, they are asking for
	// something no role in this product grants.
	ErrTemplateOperatorRequired = errors.New("only the operator of this server can write a template every team sees: authenticate with the instance api_token, or create it in a team")
	ErrTemplateUnknownSkill     = errors.New("template names a skill that does not exist")
	// ErrTemplateBuiltinLoad reports that some of what we ship did not load.
	// Distinct from the unknown-skill case because a bad frontmatter line is
	// not a missing skill, and a log line that says otherwise sends whoever
	// reads it to the wrong file.
	ErrTemplateBuiltinLoad = errors.New("some built-in templates did not load")
)

// TemplateService owns kickoff methodologies.
//
// The thing to hold on to while reading this: a template produces no rows. It is
// text a model reads before a research exists, and the only structural act in
// the whole feature is stamping which one was followed. Everything that looks
// like it should instantiate something does not.
type TemplateService struct {
	templates *storage.TemplateRepository
	skills    *storage.SkillRepository
	teams     *storage.TeamRepository
	access    *Access
	// skillSvc is set after construction because the two services would
	// otherwise be a construction cycle. Attaching goes through it rather than
	// through the repository: the six-skill cap, the access check and the
	// skill.attached event all live there, and reaching past them meant a
	// template naming nine skills put a research permanently over budget.
	skillSvc *SkillService
	log      *slog.Logger
}

// SetSkillService wires the attach path. Without it AttachSkills does nothing
// and says so, rather than silently falling back to an unchecked write.
func (s *TemplateService) SetSkillService(svc *SkillService) { s.skillSvc = svc }

func NewTemplateService(templates *storage.TemplateRepository, skills *storage.SkillRepository, teams *storage.TeamRepository, access *Access, log *slog.Logger) *TemplateService {
	return &TemplateService{templates: templates, skills: skills, teams: teams, access: access, log: log}
}

type TemplateInput struct {
	Name         string
	Description  string
	WhenToUse    string
	WhenNotToUse string
	Body         string
	Skills       []string
}

// List is every template the caller may use: the global set plus their teams'.
//
// A share visitor gets nothing. A template is a team's methodology in the same
// sense a skill is, and a public link has never carried working process.
func (s *TemplateService) List(ctx context.Context) ([]*domain.Template, error) {
	if auth.ShareFromContext(ctx) != nil {
		return nil, ErrNotFound
	}
	scope, err := s.callerScope(ctx)
	if err != nil {
		return nil, err
	}
	return s.templates.ListVisible(ctx, scope)
}

// Get takes an id or a slug and returns the body with it.
//
// Both, because neither alone is enough. A fork keeps its parent's slug, so
// outside a research a slug names two rows and the caller cannot say which —
// an id can. But an agent reading `template_list` has slugs, and asking it to
// carry ids would be pointless ceremony. Id first, then slug.
func (s *TemplateService) Get(ctx context.Context, idOrSlug string) (*domain.Template, error) {
	if auth.ShareFromContext(ctx) != nil {
		return nil, ErrNotFound
	}
	scope, err := s.callerScope(ctx)
	if err != nil {
		return nil, err
	}
	tp, err := s.templates.FindByID(ctx, idOrSlug)
	if err != nil {
		return nil, err
	}
	if tp != nil && !scope.Sees(tp.TeamID) {
		// Visible only through the same rule the list obeys: an id is not a
		// way past team scoping.
		tp = nil
	}
	if tp == nil {
		tp, err = s.templates.FindVisibleBySlug(ctx, scope, idOrSlug)
		if err != nil {
			return nil, err
		}
	}
	if tp == nil {
		return nil, ErrNotFound
	}
	body, err := s.templates.Body(ctx, tp.ID)
	if err != nil {
		return nil, err
	}
	tp.Body = body
	tp.SkillsResolved = s.resolveSkills(ctx, tp)
	countScope := scope
	if auth.OperatorFromContext(ctx) == auth.OperatorByToken {
		// The one thing the narrowed scope takes away that the operator actually
		// needs. Their scope is empty, and ResearchCount short-circuits an empty
		// scope to zero — so every global they wrote reported "used 0 times",
		// which is indistinguishable from nobody using it and is the only
		// feedback the feature offers them.
		//
		// Counting across every team is a widening, and a deliberate one: this is
		// an aggregate over rows they already own, on a template that is theirs,
		// and it names no team and no research. The content scoping above is
		// untouched.
		countScope = storage.TemplateScope{All: true}
	}
	if n, err := s.templates.ResearchCount(ctx, countScope, tp); err == nil {
		tp.ResearchCount = n
	}
	return tp, nil
}

// UnknownSkills names the slugs in a template that resolve to nothing, so a
// write can warn the person who typed them.
func (s *TemplateService) UnknownSkills(ctx context.Context, tp *domain.Template) []string {
	var unknown []string
	for _, slug := range tp.Skills {
		sk, err := s.skills.FindForTemplate(ctx, tp.TeamID, slug)
		if err != nil || sk == nil {
			unknown = append(unknown, "no skill named "+slug)
		}
	}
	return unknown
}

// resolveSkills turns the stored slug list into something a reader can judge.
// A slug that resolves to nothing is reported as missing rather than dropped:
// silently shortening the list would hide a broken methodology.
func (s *TemplateService) resolveSkills(ctx context.Context, tp *domain.Template) []domain.TemplateSkill {
	if len(tp.Skills) == 0 {
		return nil
	}
	out := make([]domain.TemplateSkill, 0, len(tp.Skills))
	for _, slug := range tp.Skills {
		sk, err := s.skills.FindForTemplate(ctx, tp.TeamID, slug)
		if err != nil || sk == nil {
			out = append(out, domain.TemplateSkill{Slug: slug, Missing: true})
			continue
		}
		out = append(out, domain.TemplateSkill{
			Slug: sk.Slug, Name: sk.Name, Description: sk.Description, Ambient: sk.Ambient,
		})
	}
	return out
}

// ListByTeam is one team's own library, for a team browsing what it has written.
func (s *TemplateService) ListByTeam(ctx context.Context, teamID string) ([]*domain.Template, error) {
	if err := s.requireTeamRead(ctx, teamID); err != nil {
		return nil, err
	}
	return s.templates.ListByTeam(ctx, teamID)
}

func (s *TemplateService) CreateTeam(ctx context.Context, teamID string, in TemplateInput) (*domain.Template, error) {
	if err := s.requireTeamWrite(ctx, teamID); err != nil {
		return nil, err
	}
	if err := s.validate(ctx, in); err != nil {
		return nil, err
	}
	name := normalizeTitle(in.Name)
	slug := Slugify(name)
	taken, err := s.templates.SlugTaken(ctx, teamID, slug)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrTemplateSlugTaken
	}
	tp := &domain.Template{
		ID:           uuid.New().String(),
		TeamID:       teamID,
		UserID:       auth.UserIDFromContext(ctx),
		Slug:         slug,
		Name:         name,
		Tier:         domain.TemplateTeam,
		Description:  normalizeContent(in.Description),
		WhenToUse:    normalizeContent(in.WhenToUse),
		WhenNotToUse: normalizeContent(in.WhenNotToUse),
		Body:         normalizeContent(in.Body),
		Skills:       in.Skills,
		Version:      1,
	}
	if tp.Skills == nil {
		tp.Skills = []string{}
	}
	if err := s.templates.Create(ctx, tp); err != nil {
		return nil, err
	}
	return tp, nil
}

// CreateGlobal writes a methodology every team on this instance will see.
//
// The one write in the product that no role grants. Roles are team-scoped by
// design and a team publishing server-wide was refused on purpose; what changed
// is only that the *operator* — who already owns the binary and the database —
// now has an API for it instead of having to ship a new build. The credential is
// checked at the HTTP layer, but the decision is made here, so a second entry
// point cannot arrive without one.
func (s *TemplateService) CreateGlobal(ctx context.Context, in TemplateInput) (*domain.Template, error) {
	if auth.ShareFromContext(ctx) != nil {
		return nil, ErrNotFound
	}
	if !auth.IsOperator(ctx) {
		return nil, ErrTemplateOperatorRequired
	}
	if err := s.validate(ctx, in); err != nil {
		return nil, err
	}
	name := normalizeTitle(in.Name)
	slug := Slugify(name)
	// Empty team means the global tier, which is what the partial unique index
	// covers — so this catches a clash with what we ship as well as with another
	// operator-written one. Both are the same answer: pick a different name.
	taken, err := s.templates.SlugTaken(ctx, "", slug)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrTemplateSlugTaken
	}
	tp := &domain.Template{
		ID:   uuid.New().String(),
		Slug: slug,
		Name: name,
		Tier: domain.TemplateGlobal,
		// Written here, so the boot-time refresh must never touch it. This is
		// the field that makes the difference, and getting it wrong loses the
		// operator's text on the next upgrade rather than at the next request.
		Source:       domain.TemplateSourceUser,
		UserID:       auth.UserIDFromContext(ctx),
		Description:  normalizeContent(in.Description),
		WhenToUse:    normalizeContent(in.WhenToUse),
		WhenNotToUse: normalizeContent(in.WhenNotToUse),
		Body:         normalizeContent(in.Body),
		Skills:       in.Skills,
		Version:      1,
	}
	if tp.Skills == nil {
		tp.Skills = []string{}
	}
	if err := s.templates.Create(ctx, tp); err != nil {
		return nil, err
	}
	return tp, nil
}

// Update edits a template in place.
//
// Two refusals, and they are different refusals. What ships in the binary cannot
// be edited by anyone — the next boot would overwrite the edit, so allowing it
// would be a lie; Fork is the method that does what the caller meant. A global
// template written on this instance can be edited, by the operator who wrote it
// and by nobody else.
func (s *TemplateService) Update(ctx context.Context, id string, in TemplateInput) (*domain.Template, error) {
	tp, err := s.templates.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if tp == nil {
		return nil, ErrNotFound
	}
	if err := s.requireWritable(ctx, tp); err != nil {
		return nil, err
	}
	// Omitted fields are inherited rather than blanked: an edit that restates
	// only the body should not silently drop the line an agent matches on.
	in = inheritTemplate(in, tp, func() (string, error) { return s.templates.Body(ctx, tp.ID) })
	if err := s.validate(ctx, in); err != nil {
		return nil, err
	}
	tp.Name = normalizeTitle(in.Name)
	tp.Description = normalizeContent(in.Description)
	tp.WhenToUse = normalizeContent(in.WhenToUse)
	tp.WhenNotToUse = normalizeContent(in.WhenNotToUse)
	tp.Body = normalizeContent(in.Body)
	tp.Skills = in.Skills
	if tp.Skills == nil {
		tp.Skills = []string{}
	}
	if err := s.templates.Update(ctx, tp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return tp, nil
}

// Fork copies a global template into a team and applies the edit. The original
// keeps updating with the binary; the copy is the team's from then on.
func (s *TemplateService) Fork(ctx context.Context, teamID, slug string, in TemplateInput) (*domain.Template, error) {
	if err := s.requireTeamWrite(ctx, teamID); err != nil {
		return nil, err
	}
	src, err := s.templates.FindGlobalBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, ErrNotFound
	}
	body, err := s.templates.Body(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	src.Body = body
	in = inheritTemplate(in, src, func() (string, error) { return body, nil })
	if err := s.validate(ctx, in); err != nil {
		return nil, err
	}
	// The fork keeps its parent's slug — it is the same methodology, edited —
	// and the partial unique indexes allow it because they are per tier.
	taken, err := s.templates.SlugTaken(ctx, teamID, src.Slug)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrTemplateSlugTaken
	}
	forked := &domain.Template{
		ID:           uuid.New().String(),
		TeamID:       teamID,
		UserID:       auth.UserIDFromContext(ctx),
		Slug:         src.Slug,
		Name:         normalizeTitle(in.Name),
		Tier:         domain.TemplateTeam,
		Description:  normalizeContent(in.Description),
		WhenToUse:    normalizeContent(in.WhenToUse),
		WhenNotToUse: normalizeContent(in.WhenNotToUse),
		Body:         normalizeContent(in.Body),
		Skills:       in.Skills,
		ForkedFrom:   src.Slug,
		Version:      1,
	}
	if forked.Skills == nil {
		forked.Skills = []string{}
	}
	if err := s.templates.Create(ctx, forked); err != nil {
		return nil, err
	}
	return forked, nil
}

func (s *TemplateService) Delete(ctx context.Context, id string) error {
	tp, err := s.templates.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if tp == nil {
		return ErrNotFound
	}
	if err := s.requireWritable(ctx, tp); err != nil {
		return err
	}
	if err := s.templates.Delete(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// DraftFromResearch produces a *starting point*, not a capture.
//
// "Save as template" does not mean what it used to mean in the first draft of
// this feature. A template carries no rows, so there is nothing to copy: what
// this returns is a body skeleton — the sections the research actually grew, the
// skills it follows — with the judgement left blank for a human or an agent to
// write. Saying so is the honest part; a caller expecting a working methodology
// out of one call would be disappointed by anything else.
func (s *TemplateService) DraftFromResearch(ctx context.Context, research *domain.Research, sections []*domain.Section, skills []*domain.Skill) TemplateInput {
	var b strings.Builder
	b.WriteString("## Before you propose anything\n\n")
	b.WriteString("_Write the questions this kind of research must not start without._\n\n")
	b.WriteString("## Structure to propose\n\n")
	if len(sections) == 0 {
		b.WriteString("_This research had no sections to learn from._\n")
	} else {
		b.WriteString("These are the sections **" + research.Name + "** grew. Propose them as a\n")
		b.WriteString("starting point, adapted to the conversation — never recite them.\n\n")
		for _, sec := range sections {
			b.WriteString("- **" + sec.DisplayName + "**")
			if sec.Description != "" {
				b.WriteString(" — " + sec.Description)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\nCreate a section when you have something to put in it, not in advance.\n\n")
	b.WriteString("## What good looks like here\n\n")
	b.WriteString("_Write what a finished entry contains, and what the conductor must refuse to write._\n\n")
	b.WriteString("## When it is done\n\n")
	b.WriteString("_Write the stopping rule._\n")

	var slugs []string
	for _, sk := range skills {
		// Ambient product skills are on everywhere already; naming them in a
		// template would be noise the reader has to filter out.
		if !sk.Ambient {
			slugs = append(slugs, sk.Slug)
		}
	}
	return TemplateInput{
		Name:      research.Name + " (draft template)",
		WhenToUse: "Use when starting a research like " + research.Name + ". Rewrite this line — it is what an agent matches on.",
		// Prompted for, not omitted: knowing when a methodology is the wrong
		// choice is what stops it being applied to everything, and a skeleton
		// exists to ask for exactly the judgement the server cannot derive.
		WhenNotToUse: "_Write when this methodology is the wrong choice, and what to reach for instead._",
		Body:         b.String(),
		Skills:       slugs,
	}
}

// AttachResult is what a caller can report back. `Resolved` false is the case
// that used to be invisible: a slug the caller cannot see produced an empty
// result identical to a successful attach, so the research went unstamped and
// nothing anywhere said so.
type AttachResult struct {
	Resolved bool
	Attached []string
	Missed   []string
}

// AttachSkills applies a template to a research that was just created from it:
// it stamps which methodology was followed and attaches the skills that
// methodology names.
//
// It reports rather than fails. A research that exists with five of six skills
// is a better outcome than a create call rolled back because a template named
// something a later build removed — but *silence* is not reporting, which is
// what Resolved is for.
func (s *TemplateService) AttachSkills(ctx context.Context, researchID, slug string) AttachResult {
	// This is a public method that writes to a research id it is handed —
	// StampResearch is a bare UPDATE. Its only caller passes a research it just
	// created, so the check is redundant today and exactly the kind of
	// redundancy that stops the second caller being the bug.
	if s.access != nil {
		if err := s.access.Write(ctx, researchID); err != nil {
			s.log.Warn("template attach refused", "research_id", researchID, "error", err)
			return AttachResult{}
		}
	}
	scope, err := s.callerScope(ctx)
	if err != nil {
		s.log.Error("template attach: scope unavailable", "research_id", researchID, "error", err)
		return AttachResult{}
	}
	tp, err := s.templates.FindVisibleBySlug(ctx, scope, slug)
	if err != nil {
		s.log.Error("template attach: lookup failed", "slug", slug, "error", err)
		return AttachResult{}
	}
	if tp == nil {
		// The commonest cause is a name where a slug was wanted. Saying so is
		// the whole point: the research is otherwise indistinguishable from one
		// that followed a methodology.
		s.log.Warn("template attach: no such template for this caller", "slug", slug)
		return AttachResult{}
	}
	res := AttachResult{Resolved: true}
	if err := s.templates.StampResearch(ctx, researchID, tp.Slug, tp.Version); err != nil {
		s.log.Warn("template stamp failed", "research_id", researchID, "error", err)
	}
	if s.skillSvc == nil {
		s.log.Error("template attach: skill service not wired")
		res.Missed = append(res.Missed, tp.Skills...)
		return res
	}
	for _, skillSlug := range tp.Skills {
		if _, err := s.skillSvc.Attach(ctx, researchID, skillSlug, true); err != nil {
			// Including the cap: a methodology naming more skills than a
			// research may follow does not get to overrun it silently.
			res.Missed = append(res.Missed, skillSlug)
			continue
		}
		res.Attached = append(res.Attached, skillSlug)
	}
	return res
}

func (s *TemplateService) validate(ctx context.Context, in TemplateInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrTemplateNameEmpty
	}
	if strings.TrimSpace(in.Body) == "" {
		return ErrTemplateBodyEmpty
	}
	if strings.TrimSpace(in.WhenToUse) == "" {
		return ErrTemplateWhenEmpty
	}
	// Runes, not bytes: a Cyrillic matching line is not worth half a Latin one.
	if len([]rune(in.WhenToUse)) > domain.TemplateCriterionMax ||
		len([]rune(in.WhenNotToUse)) > domain.TemplateCriterionMax {
		return ErrTemplateCriterionLong
	}
	if len([]rune(in.Body)) > domain.TemplateBodyMax {
		return ErrTemplateBodyLong
	}
	return nil
}

// callerTeams is the visibility scope for templates, taken from the caller's
// memberships rather than from anything they send — so a slug can never resolve
// into a team they are not in.
func (s *TemplateService) callerScope(ctx context.Context) (storage.TemplateScope, error) {
	if auth.OperatorFromContext(ctx) == auth.OperatorByToken {
		// The api_token proves who runs the server. It proves no membership of
		// any team, and it must not become a way to read every team's private
		// methodologies through the API — so an empty scope, which resolves to
		// the global tier and nothing else. That is exactly what the operator
		// needs anyway: the id of a global, to edit or delete it.
		//
		// Checked before the no-caller rule below, for the same reason a share
		// is: falling through would hand this token the whole instance.
		return storage.TemplateScope{}, nil
	}
	uid := auth.UserIDFromContext(ctx)
	if uid == "" {
		if s.access.AccountsEnabled() {
			// With accounts on, an unauthenticated caller is a stranger rather
			// than the owner of the instance, and gets what the operator token
			// gets: the global tier and nothing any team wrote.
			return storage.TemplateScope{}, nil
		}
		// Local mode: there are no memberships to enumerate, so listing the
		// caller's teams returns nothing and a template written on that
		// instance would be invisible to whoever wrote it. Nobody to scope to
		// means everything, which is the rule the rest of the product follows
		// when there is no caller.
		return storage.TemplateScope{All: true}, nil
	}
	teams, err := s.teams.ListByUser(ctx, uid)
	if err != nil {
		return storage.TemplateScope{}, fmt.Errorf("list teams: %w", err)
	}
	ids := make([]string, 0, len(teams))
	for _, t := range teams {
		ids = append(ids, t.ID)
	}
	return storage.TemplateScope{TeamIDs: ids}, nil
}

// requireWritable is the one place that decides who may change an existing
// template, so Update and Delete cannot drift apart — which is how a delete ends
// up permitted where the matching edit is refused.
//
// Read it as three tiers rather than two: what ships in the binary (nobody), the
// server-wide library (the operator), a team's own (that team's editors).
func (s *TemplateService) requireWritable(ctx context.Context, tp *domain.Template) error {
	// A share is refused first, everywhere, so the rule survives an entry point
	// nobody has written yet. It is unreachable today — no template route is
	// mounted under the share prefix — and that is exactly why it must not
	// depend on staying unreachable.
	if auth.ShareFromContext(ctx) != nil {
		return ErrNotFound
	}
	if tp.Tier != domain.TemplateGlobal {
		// The api_token gets no further, and the refusal is ErrNotFound rather
		// than ErrForbidden.
		//
		// Two reasons, and the first is a hole this closes. `teamCheck` reads an
		// empty user id as "local mode, nobody to check" — a rule written when
		// nothing else in the product could produce a user-less context. The
		// token now can, because it is recognised before user authentication, so
		// falling through would have let it edit and delete any team's private
		// methodology on an auth-enabled instance. The second: an empty `Update`
		// body inherits every field from the stored row and returns it, so a 403
		// here would have made PUT an existence oracle for team template ids as
		// well as a way to change them.
		//
		// ErrNotFound is also simply the truth about what the token proves: who
		// runs the server, and no membership of any team. It is the same answer
		// the read side already gives, for the same reason.
		if auth.OperatorFromContext(ctx) == auth.OperatorByToken {
			return ErrNotFound
		}
		return s.requireTeamWrite(ctx, tp.TeamID)
	}
	if tp.Source == domain.TemplateSourceBuiltin {
		// Not a permission the operator is missing — the next boot rewrites this
		// row from the binary, so an edit here would be undone without anybody
		// being told. Fork says so honestly.
		return ErrTemplateGlobalWrite
	}
	if !auth.IsOperator(ctx) {
		return ErrTemplateOperatorRequired
	}
	return nil
}

func (s *TemplateService) requireTeamRead(ctx context.Context, teamID string) error {
	return s.teamCheck(ctx, teamID, false)
}

func (s *TemplateService) requireTeamWrite(ctx context.Context, teamID string) error {
	return s.teamCheck(ctx, teamID, true)
}

// teamCheck is the team-level authorization a template library needs, since
// there is no research to ask service.Access about. It follows the same three
// rules: a share is refused first, existence is checked before the no-caller
// rule, a non-member gets ErrNotFound and a member without the right gets
// ErrForbidden.
func (s *TemplateService) teamCheck(ctx context.Context, teamID string, write bool) error {
	if auth.ShareFromContext(ctx) != nil {
		return ErrNotFound
	}
	if teamID == "" {
		return ErrNotFound
	}
	team, err := s.teams.FindByID(ctx, teamID)
	if err != nil {
		return fmt.Errorf("find team: %w", err)
	}
	if team == nil {
		return ErrNotFound
	}
	uid := auth.UserIDFromContext(ctx)
	if uid == "" {
		if s.access.AccountsEnabled() {
			return ErrNotFound
		}
		return nil
	}
	role, ok, err := s.teams.FindRole(ctx, teamID, uid)
	if err != nil {
		return fmt.Errorf("find team role: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	if write && !role.CanWrite() {
		return ErrForbidden
	}
	return nil
}

func inheritTemplate(in TemplateInput, src *domain.Template, body func() (string, error)) TemplateInput {
	if strings.TrimSpace(in.Name) == "" {
		in.Name = src.Name
	}
	if strings.TrimSpace(in.Description) == "" {
		in.Description = src.Description
	}
	if strings.TrimSpace(in.WhenToUse) == "" {
		in.WhenToUse = src.WhenToUse
	}
	if strings.TrimSpace(in.WhenNotToUse) == "" {
		in.WhenNotToUse = src.WhenNotToUse
	}
	if strings.TrimSpace(in.Body) == "" {
		if b, err := body(); err == nil {
			in.Body = b
		}
	}
	if in.Skills == nil {
		in.Skills = src.Skills
	}
	return in
}
