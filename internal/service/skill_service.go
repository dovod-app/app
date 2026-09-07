package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/google/uuid"
)

var (
	// ErrSkillCapReached and ErrSkillAlreadyAttached are both 409s and the UI
	// writes a different sentence for each, so they are separate errors rather
	// than one message the client would have to string-match.
	ErrSkillCapReached       = errors.New("this research already follows the maximum number of skills")
	ErrSkillAlreadyAttached  = errors.New("that skill is already attached to this research")
	ErrSkillAmbient          = errors.New("product skills are always on and cannot be detached")
	ErrSkillBuiltinWrite     = errors.New("built-in skills cannot be changed; edit forks a copy for your team")
	ErrSkillNameEmpty        = errors.New("name is required")
	ErrSkillBodyEmpty        = errors.New("body is required")
	ErrSkillDescriptionEmpty = errors.New("description is required: it is the line the agent decides from, so start it with \"Use when\"")
	ErrSkillDescriptionLong  = fmt.Errorf("description must be %d characters or fewer", domain.SkillDescriptionMax)
	ErrSkillBodyLong         = fmt.Errorf("body must be %d characters or fewer; longer than that is two skills", domain.SkillBodyMax)
	ErrSkillSlugTaken        = errors.New("a skill with that name already exists here")
	ErrSkillInUse            = errors.New("that skill is attached to other researches")
	ErrSkillNotPrivate       = errors.New("only a skill belonging to this research can be promoted")
)

// SkillService owns every rule about skills. The two that are easy to lose and
// expensive to lose are here rather than at the call sites:
//
//   - the cap counts only skills somebody chose, never the ambient product
//     ones, or a template attaching two methodologies breaches it on creation;
//   - fork, copy and promote swap the attachment in one step, so a research is
//     never briefly following one fewer skill than it did.
type SkillService struct {
	skills     *storage.SkillRepository
	researches *storage.ResearchRepository
	teams      *storage.TeamRepository
	access     *Access
	events     EventNotifier
	log        *slog.Logger
}

func NewSkillService(skills *storage.SkillRepository, researches *storage.ResearchRepository, teams *storage.TeamRepository, access *Access, events EventNotifier, log *slog.Logger) *SkillService {
	return &SkillService{skills: skills, researches: researches, teams: teams, access: access, events: events, log: log}
}

type SkillInput struct {
	Name        string
	Description string
	Body        string
	// DescriptionUntouched says the caller never supplied a description and the
	// one in this struct was read back out of the row.
	//
	// It exists for the partial write the MCP tools make. Update clears
	// NeedsTrigger on the reasoning that anybody sending a description has
	// looked at it — true of the REST form, which puts the field in front of a
	// person every time. An agent changing nothing but a body has not looked at
	// it, and clearing the flag there removes the only prompt telling somebody
	// that a migration invented that trigger line, while the invented line is
	// still there.
	DescriptionUntouched bool
}

// ListAttached is what the research follows, in precedence order.
//
// Every read method here refuses a share context of its own accord. Access
// resolves a share to viewer on the research, which is right for findings and
// wrong for methodology, and relying on the routing alone leaves one layer.
func (s *SkillService) ListAttached(ctx context.Context, researchID string) ([]*domain.Skill, error) {
	if auth.ShareFromContext(ctx) != nil {
		return nil, ErrNotFound
	}
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}
	attached, err := s.skills.ListAttached(ctx, researchID)
	if err != nil {
		return nil, err
	}
	return s.withAmbient(ctx, attached), nil
}

// withAmbient folds in the product skills that are on by definition.
//
// Marking them "ambient" in the database was not enough: nothing attached them,
// so a research nobody had curated followed nothing, research_get omitted the
// skills key entirely, and the agent never learned the feature existed. Rather
// than write six attachment rows per research and a backfill for every existing
// one, the always-on set is unioned in on read — which is what "always on"
// means, and which cannot drift out of date.
//
// They lead the list within the built-in tier and are deduplicated against
// anything genuinely attached, including a team's fork of one.
func (s *SkillService) withAmbient(ctx context.Context, attached []*domain.Skill) []*domain.Skill {
	ambient, err := s.skills.ListAmbient(ctx)
	if err != nil {
		s.log.Warn("ambient skills unavailable", "error", err)
		return attached
	}
	seen := make(map[string]bool, len(attached))
	for _, sk := range attached {
		seen[sk.Slug] = true
	}
	var out []*domain.Skill
	out = append(out, attached...)
	for _, sk := range ambient {
		if !seen[sk.Slug] {
			out = append(out, sk)
		}
	}
	return out
}

// ListLibrary is what the research may attach. Research-private skills of other
// researches are excluded in the query, not here.
func (s *SkillService) ListLibrary(ctx context.Context, researchID, q string) ([]*domain.Skill, error) {
	if auth.ShareFromContext(ctx) != nil {
		return nil, ErrNotFound
	}
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}
	return s.skills.ListLibrary(ctx, researchID, strings.TrimSpace(q))
}

// Load reads one skill's body, resolving the slug the way the agent means it:
// within this research's scope, highest tier first.
//
// A share visitor is refused before the slug is even resolved. A skill is a
// team's methodology — the same class of working process as the instruction it
// replaces, which redactForShare has always stripped — so a public link must
// not be able to read one, and the refusal belongs here rather than only in the
// routing, where a future route could forget it.
func (s *SkillService) Load(ctx context.Context, researchID, slug string) (*domain.Skill, error) {
	if auth.ShareFromContext(ctx) != nil {
		return nil, ErrNotFound
	}
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}
	sk, err := s.resolveInResearch(ctx, researchID, slug)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, ErrNotFound
	}
	body, err := s.skills.Body(ctx, sk.ID)
	if err != nil {
		return nil, err
	}
	sk.Body = body
	sk.BodyTokens = domain.EstimateTokens(body)
	return sk, nil
}

// resolveInResearch finds the skill a slug means here: what the research is
// actually following first, and only then what it could attach.
//
// The order matters. A team fork shares its built-in's slug, so scope alone
// would hand a research that follows the built-in the team's version instead —
// a body that does not match the index the agent was given.
func (s *SkillService) resolveInResearch(ctx context.Context, researchID, slug string) (*domain.Skill, error) {
	sk, err := s.skills.FindAttachedBySlug(ctx, researchID, slug)
	if err != nil {
		return nil, err
	}
	if sk != nil {
		return sk, nil
	}
	return s.skills.FindInResearchScope(ctx, researchID, slug)
}

// Attach adds a skill this research may see. viaTemplate records that a
// template chose it rather than a person, which the UI shows and nothing else
// depends on.
func (s *SkillService) Attach(ctx context.Context, researchID, slug string, viaTemplate bool) (*domain.Skill, error) {
	if err := s.access.Write(ctx, researchID); err != nil {
		return nil, err
	}
	sk, err := s.resolveAttachable(ctx, researchID, slug)
	if err != nil {
		return nil, err
	}

	attached, err := s.skills.ListAttached(ctx, researchID)
	if err != nil {
		return nil, err
	}
	for _, a := range attached {
		if a.ID == sk.ID {
			return nil, ErrSkillAlreadyAttached
		}
	}
	// Ambient skills are outside the budget, so attaching one is never refused
	// for space.
	if !sk.Ambient {
		n, err := s.skills.CountChosen(ctx, researchID)
		if err != nil {
			return nil, err
		}
		if n >= domain.SkillsPerResearch {
			return nil, ErrSkillCapReached
		}
	}
	if err := s.skills.Attach(ctx, researchID, sk.ID, viaTemplate); err != nil {
		return nil, err
	}
	emit(ctx, s.events, Event{Type: "skill.attached", ResearchID: researchID, EntityID: sk.ID, Entity: "skill"})
	return sk, nil
}

func (s *SkillService) Detach(ctx context.Context, researchID, slug string) error {
	if err := s.access.Write(ctx, researchID); err != nil {
		return err
	}
	sk, err := s.skills.FindAttachedBySlug(ctx, researchID, slug)
	if err != nil {
		return err
	}
	if sk == nil {
		// A product skill is in the list without an attachment row, so the
		// lookup above misses it. Answering "not found" about something the
		// caller is looking at is the wrong refusal — say it is always on.
		if inScope, scopeErr := s.skills.FindInResearchScope(ctx, researchID, slug); scopeErr == nil && inScope != nil && inScope.Ambient {
			return ErrSkillAmbient
		}
		return ErrNotFound
	}
	if sk.Ambient {
		return ErrSkillAmbient
	}
	if err := s.skills.Detach(ctx, researchID, sk.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// A research-private skill exists only for this attachment. Leaving the row
	// behind made it unreachable — absent from both listings, addressable by an
	// id no route hands out again — while still holding its slug, so writing it
	// again answered "a skill with that name already exists here" about
	// something the user could not see.
	if sk.Tier == domain.SkillPrivate {
		if err := s.skills.Delete(ctx, sk.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	emit(ctx, s.events, Event{Type: "skill.detached", ResearchID: researchID, EntityID: sk.ID, Entity: "skill"})
	return nil
}

// CreatePrivate writes a skill that belongs to this research alone and attaches
// it. This is where a rule that applies only here lives, and it is what the
// instruction migration produces.
func (s *SkillService) CreatePrivate(ctx context.Context, researchID string, in SkillInput) (*domain.Skill, error) {
	if err := s.access.Write(ctx, researchID); err != nil {
		return nil, err
	}
	// Creating a private skill attaches it, so it spends the budget exactly the
	// way attaching an existing one does. Checking only in Attach left this as
	// a way around the cap entirely — write your seventh skill instead of
	// attaching it.
	n, err := s.skills.CountChosen(ctx, researchID)
	if err != nil {
		return nil, err
	}
	if n >= domain.SkillsPerResearch {
		return nil, ErrSkillCapReached
	}
	sk, err := s.build(ctx, domain.SkillPrivate, "", researchID, in)
	if err != nil {
		return nil, err
	}
	if err := s.skills.Create(ctx, sk); err != nil {
		return nil, err
	}
	// A private skill is attached on creation: it has no other research to
	// belong to, and one that exists unattached would be invisible everywhere.
	// If the attach fails the row is removed rather than left orphaned —
	// otherwise the caller retries, gets `slug_taken`, and cannot find the
	// thing holding the name.
	if err := s.skills.Attach(ctx, researchID, sk.ID, false); err != nil {
		if delErr := s.skills.Delete(ctx, sk.ID); delErr != nil {
			s.log.Error("orphaned skill after failed attach", "skill_id", sk.ID, "error", delErr)
		}
		return nil, err
	}
	emit(ctx, s.events, Event{Type: "skill.created", ResearchID: researchID, EntityID: sk.ID, Entity: "skill"})
	return sk, nil
}

// CreateTeam writes a skill into a team's library. Authoring is a team act, so
// the role check is TeamService's rule — editor or above — asked here through
// the same repository it uses.
func (s *SkillService) CreateTeam(ctx context.Context, teamID string, in SkillInput) (*domain.Skill, error) {
	if err := s.requireTeamWrite(ctx, teamID); err != nil {
		return nil, err
	}
	sk, err := s.build(ctx, domain.SkillTeam, teamID, "", in)
	if err != nil {
		return nil, err
	}
	if err := s.skills.Create(ctx, sk); err != nil {
		return nil, err
	}
	return sk, nil
}

// Update changes a team or private skill in place. A built-in is refused here
// on purpose: forking it is a different act with a different result, and
// Fork is the method that does it.
func (s *SkillService) Update(ctx context.Context, id string, in SkillInput) (*domain.Skill, error) {
	sk, err := s.skills.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, ErrNotFound
	}
	if sk.Tier == domain.SkillBuiltin {
		return nil, ErrSkillBuiltinWrite
	}
	if err := s.authorizeSkillWrite(ctx, sk); err != nil {
		return nil, err
	}
	// Historical instructions may exceed today's body rules. Metadata repair
	// must preserve their exact bytes; only a changed body is newly validated.
	sk.Body, err = s.skills.Body(ctx, sk.ID)
	if err != nil {
		return nil, err
	}
	bodyChanged := in.Body != sk.Body
	if err := validateSkillFields(in, bodyChanged); err != nil {
		return nil, err
	}
	sk.Name = normalizeTitle(in.Name)
	sk.Description = normalizeContent(in.Description)
	if bodyChanged {
		sk.Body = normalizeContent(in.Body)
	}
	// A person has now written the trigger line, whatever a migration put there
	// — unless they never sent one, in which case nothing about it was reviewed.
	if !in.DescriptionUntouched {
		sk.NeedsTrigger = false
	}
	if err := s.skills.Update(ctx, sk); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// Every research following it, not just the one that owns it: editing a team
	// skill changes the name and trigger line on every page that lists it.
	followers, err := s.skills.ResearchesFollowing(ctx, sk.ID)
	if err != nil {
		return nil, err
	}
	s.announce(ctx, "skill.updated", sk.ID, followers)
	return sk, nil
}

// Fork copies a built-in into the caller's team, applies the edit, and swaps
// this research's attachment onto the copy — all before returning, so the
// research never passes through a state with the skill missing. Doing it as
// detach-then-attach could hit the cap in between and leave a hole.
func (s *SkillService) Fork(ctx context.Context, researchID, slug string, in SkillInput) (*domain.Skill, error) {
	if err := s.access.Write(ctx, researchID); err != nil {
		return nil, err
	}
	src, err := s.resolveInResearch(ctx, researchID, slug)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, ErrNotFound
	}
	if src.Tier != domain.SkillBuiltin {
		return nil, ErrNotFound
	}
	teamID, err := s.researchTeam(ctx, researchID)
	if err != nil {
		return nil, err
	}
	if err := s.requireTeamWrite(ctx, teamID); err != nil {
		return nil, err
	}
	// Omitted fields are inherited rather than blanked. A fork sent with only a
	// body used to land with an empty description — a skill in the index that
	// could never be chosen, produced by the operation whose whole purpose is
	// to keep the parent's meaning.
	in = inheritFrom(in, src, func() (string, error) { return s.skills.Body(ctx, src.ID) })
	if err := validateSkill(in); err != nil {
		return nil, err
	}

	forked := &domain.Skill{
		ID:          uuid.New().String(),
		TeamID:      teamID,
		UserID:      auth.UserIDFromContext(ctx),
		Slug:        src.Slug,
		Name:        normalizeTitle(in.Name),
		Tier:        domain.SkillTeam,
		Description: normalizeContent(in.Description),
		Body:        normalizeContent(in.Body),
		ForkedFrom:  src.Slug,
		Version:     1,
		// Ambient travels with the copy. Without it, forking a product skill
		// converted it into a chosen one: it started counting against the
		// budget and became detachable, so four forks bought four extra slots
		// and left the research able to switch off "how to write an entry".
		Ambient: src.Ambient,
	}
	// The fork keeps the built-in's slug — it is the same methodology, edited —
	// and the partial unique indexes allow it because they are per tier. Only a
	// second fork of the same built-in in the same team would clash.
	taken, err := s.skills.SlugTaken(ctx, domain.SkillTeam, teamID, "", forked.Slug)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrSkillSlugTaken
	}
	if err := s.skills.Create(ctx, forked); err != nil {
		return nil, err
	}
	if err := s.attachOrUnmake(ctx, researchID, src.ID, forked.ID); err != nil {
		return nil, err
	}
	emit(ctx, s.events, Event{Type: "skill.updated", ResearchID: researchID, EntityID: forked.ID, Entity: "skill"})
	return forked, nil
}

// CopyHere turns an attached team or built-in skill into a research-private
// copy, swapping the attachment atomically. It is the replacement for what the
// instruction field used to allow: saying something that applies only here.
func (s *SkillService) CopyHere(ctx context.Context, researchID, slug string) (*domain.Skill, error) {
	if err := s.access.Write(ctx, researchID); err != nil {
		return nil, err
	}
	src, err := s.resolveInResearch(ctx, researchID, slug)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, ErrNotFound
	}
	if src.Tier == domain.SkillPrivate {
		return src, nil
	}
	body, err := s.skills.Body(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	taken, err := s.skills.SlugTaken(ctx, domain.SkillPrivate, "", researchID, src.Slug)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrSkillSlugTaken
	}
	copied := &domain.Skill{
		ID:          uuid.New().String(),
		ResearchID:  researchID,
		UserID:      auth.UserIDFromContext(ctx),
		Slug:        src.Slug,
		Name:        src.Name,
		Tier:        domain.SkillPrivate,
		Description: src.Description,
		Body:        body,
		ForkedFrom:  src.Slug,
		Version:     1,
		// Same rule as Fork: a private copy of a product skill is still a
		// product skill.
		Ambient: src.Ambient,
	}
	if err := s.skills.Create(ctx, copied); err != nil {
		return nil, err
	}
	if err := s.attachOrUnmake(ctx, researchID, src.ID, copied.ID); err != nil {
		return nil, err
	}
	emit(ctx, s.events, Event{Type: "skill.created", ResearchID: researchID, EntityID: copied.ID, Entity: "skill"})
	return copied, nil
}

// attachOrUnmake is replaceOrAttach with the copy removed when it fails.
//
// Fork and CopyHere both write the new row before attaching it, and the attach
// can be refused — replaceOrAttach falls back to a plain attach when the source
// was never attached here, and that fallback goes through the cap. A refusal
// used to leave the copy behind, and the wreckage was worse than the failure:
//
//   - the row holds the source's slug, and a slug resolves in this research's
//     scope before it resolves to a built-in, so `skill_load evidence-grading`
//     started answering with a private copy the caller never made;
//   - it is attached to nothing, so no listing shows it and nothing offers to
//     remove it;
//   - and CopyHere short-circuits on an already-private source, so retrying
//     after freeing a slot answers "already private, nothing copied" forever.
//
// One refused call, and that slug is unusable for the life of the research.
// CreatePrivate has cleaned up after itself since it was written; these two did
// not, and an agent driving them at a full cap is how it surfaced.
func (s *SkillService) attachOrUnmake(ctx context.Context, researchID, oldID, newID string) error {
	err := s.replaceOrAttach(ctx, researchID, oldID, newID)
	if err == nil {
		return nil
	}
	if delErr := s.skills.Delete(ctx, newID); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		// The original refusal is what the caller needs to hear; a failed
		// cleanup is ours to notice.
		s.log.Error("orphaned skill copy after failed attach", "skill_id", newID, "error", delErr)
	}
	return err
}

// replaceOrAttach swaps one attachment for another, and falls back to a plain
// attach when the source was readable but not actually attached here — copying
// or forking a library skill the research had not taken up yet.
//
// The fallback goes through the budget check. Reaching the repository directly
// was a way past the cap: fork or copy an unattached built-in and the research
// gains a skill nobody counted.
func (s *SkillService) replaceOrAttach(ctx context.Context, researchID, oldID, newID string) error {
	err := s.skills.Replace(ctx, researchID, oldID, newID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	n, err := s.skills.CountChosen(ctx, researchID)
	if err != nil {
		return err
	}
	if n >= domain.SkillsPerResearch {
		return ErrSkillCapReached
	}
	return s.skills.Attach(ctx, researchID, newID, false)
}

// Promote moves a research-private skill into its team's library, so a rule
// that turned out to be reusable stops being trapped on one research. The
// attachment follows the new row.
func (s *SkillService) Promote(ctx context.Context, researchID, slug string) (*domain.Skill, error) {
	if err := s.access.Write(ctx, researchID); err != nil {
		return nil, err
	}
	src, err := s.resolveInResearch(ctx, researchID, slug)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, ErrNotFound
	}
	if src.Tier != domain.SkillPrivate {
		return nil, ErrSkillNotPrivate
	}
	teamID, err := s.researchTeam(ctx, researchID)
	if err != nil {
		return nil, err
	}
	if err := s.requireTeamWrite(ctx, teamID); err != nil {
		return nil, err
	}
	body, err := s.skills.Body(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	taken, err := s.skills.SlugTaken(ctx, domain.SkillTeam, teamID, "", src.Slug)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrSkillSlugTaken
	}
	promoted := &domain.Skill{
		ID:          uuid.New().String(),
		TeamID:      teamID,
		UserID:      auth.UserIDFromContext(ctx),
		Slug:        src.Slug,
		Name:        src.Name,
		Tier:        domain.SkillTeam,
		Description: src.Description,
		Body:        body,
		Version:     1,
		// A migrated instruction carries its flag across, because promoting it
		// does not make the generated trigger line any better.
		NeedsTrigger: src.NeedsTrigger,
	}
	if err := s.skills.Create(ctx, promoted); err != nil {
		return nil, err
	}
	if err := s.skills.Replace(ctx, researchID, src.ID, promoted.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// The private original is gone: leaving it would give the research two
	// skills with the same slug, and the scoped lookup would keep resolving to
	// the private one, so the promotion would appear to have done nothing.
	if err := s.skills.Delete(ctx, src.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	emit(ctx, s.events, Event{Type: "skill.updated", ResearchID: researchID, EntityID: promoted.ID, Entity: "skill"})
	return promoted, nil
}

// Read returns one skill by id, with its body. It exists because Update and
// Delete are addressed by id and there was no way to read back what they act on:
// a maintainer holding a team skill's id could overwrite it without being able
// to see it first.
func (s *SkillService) Read(ctx context.Context, id string) (*domain.Skill, error) {
	sk, err := s.skills.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, ErrNotFound
	}
	if err := s.authorizeSkillRead(ctx, sk); err != nil {
		return nil, err
	}
	body, err := s.skills.Body(ctx, sk.ID)
	if err != nil {
		return nil, err
	}
	sk.Body = body
	sk.BodyTokens = domain.EstimateTokens(body)
	return sk, nil
}

// ListTeam is a team's own library, reachable without going through one of its
// researches — which a team writing its first skill does not have yet.
func (s *SkillService) ListTeam(ctx context.Context, teamID string) ([]*domain.Skill, error) {
	if err := s.requireTeamRead(ctx, teamID); err != nil {
		return nil, err
	}
	return s.skills.ListByTeam(ctx, teamID)
}

// Delete removes a team or private skill. A built-in is not deletable, and a
// team skill other researches follow is refused rather than quietly taken away
// from them.
func (s *SkillService) Delete(ctx context.Context, id string) error {
	sk, err := s.skills.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if sk == nil {
		return ErrNotFound
	}
	if sk.Tier == domain.SkillBuiltin {
		return ErrSkillBuiltinWrite
	}
	if err := s.authorizeSkillWrite(ctx, sk); err != nil {
		return err
	}
	if sk.Tier == domain.SkillTeam {
		n, err := s.skills.UsageCount(ctx, sk.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: %d", ErrSkillInUse, n)
		}
	}
	// Read the followers before deleting: the attachment rows cascade away with
	// the skill, so afterwards there is nobody left to tell. Same reason
	// TeamService.Delete reads its member list first.
	followers, err := s.skills.ResearchesFollowing(ctx, sk.ID)
	if err != nil {
		return err
	}
	if err := s.skills.Delete(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	s.announce(ctx, "skill.deleted", sk.ID, followers)
	return nil
}

// announce sends one event per research that follows the skill. A team skill
// belongs to no single research, so a page showing it has to be named
// explicitly or it keeps rendering a skill that has been renamed or removed.
func (s *SkillService) announce(ctx context.Context, eventType, skillID string, researchIDs []string) {
	for _, rid := range researchIDs {
		emit(ctx, s.events, Event{Type: eventType, ResearchID: rid, EntityID: skillID, Entity: "skill"})
	}
}

// Index is what research_get returns: name, trigger line and tier, no bodies.
// It is the one call the conductor always makes, which is why the index rides
// in it — a skill unreachable from the prompt the model actually runs does not
// exist.
//
// It answers with nothing rather than an error, because a research page must
// not fail to load because the skills query did, and because a share visitor
// gets no index at all.
//
// It checks Read like every other listing here. It used to reach the repository
// directly, on the reasoning that its only caller had already resolved the
// research through an access-checked Get — which was true, and is exactly the
// kind of reasoning this codebase decided not to rely on. Every skill listing
// asks; a method that returns rows for any research id an argument can name is
// one careless caller away from being the leak.
func (s *SkillService) Index(ctx context.Context, researchID string) []*domain.Skill {
	if auth.ShareFromContext(ctx) != nil {
		return nil
	}
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil
	}
	list, err := s.skills.ListAttached(ctx, researchID)
	if err != nil {
		s.log.Warn("skill index failed", "research_id", researchID, "error", err)
		return nil
	}
	return s.withAmbient(ctx, list)
}

// resolveAttachable finds a slug a research may attach. A research-private
// skill of another research is not in scope, so this cannot reach one.
func (s *SkillService) resolveAttachable(ctx context.Context, researchID, slug string) (*domain.Skill, error) {
	sk, err := s.skills.FindInResearchScope(ctx, researchID, slug)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, ErrNotFound
	}
	if sk.Tier == domain.SkillPrivate && sk.ResearchID != researchID {
		return nil, ErrNotFound
	}
	return sk, nil
}

// authorizeSkillRead is the read half: a built-in is public, a private skill is
// the research's, a team skill is the team's.
func (s *SkillService) authorizeSkillRead(ctx context.Context, sk *domain.Skill) error {
	if auth.ShareFromContext(ctx) != nil {
		return ErrNotFound
	}
	switch {
	case sk.Tier == domain.SkillBuiltin:
		return nil
	case sk.ResearchID != "":
		return s.access.Read(ctx, sk.ResearchID)
	default:
		return s.requireTeamRead(ctx, sk.TeamID)
	}
}

// requireTeamRead allows any member. A non-member gets ErrNotFound, because
// confirming that another team's library holds something is itself information.
func (s *SkillService) requireTeamRead(ctx context.Context, teamID string) error {
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
			// Nobody is everybody only on an instance with no accounts. With
			// accounts on this is a caller who did not authenticate, and a team
			// they are not in is one they cannot know exists.
			return ErrNotFound
		}
		return nil
	}
	if _, ok, err := s.teams.FindRole(ctx, teamID, uid); err != nil {
		return fmt.Errorf("find team role: %w", err)
	} else if !ok {
		return ErrNotFound
	}
	return nil
}

// authorizeSkillWrite asks the right question for the tier: a private skill is
// governed by the research it belongs to, a team skill by the team.
func (s *SkillService) authorizeSkillWrite(ctx context.Context, sk *domain.Skill) error {
	if sk.ResearchID != "" {
		return s.access.Write(ctx, sk.ResearchID)
	}
	return s.requireTeamWrite(ctx, sk.TeamID)
}

// requireTeamWrite is the team-level half of authorization. Access covers
// researches; a team library has no research to ask about.
//
// The no-authenticated-caller rule is repeated here deliberately rather than
// borrowed: local mode has no users and no teams, and a skill written there
// must not be refused for a membership nobody can have.
func (s *SkillService) requireTeamWrite(ctx context.Context, teamID string) error {
	if auth.ShareFromContext(ctx) != nil {
		return ErrForbidden
	}
	// Existence is checked before the no-caller rule, exactly as Access.Role
	// does it and for the same reason: without this a bad team id in local mode
	// slips through to the insert and surfaces as a foreign-key error, so the
	// caller gets a 500 where every other team-scoped route answers 404.
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
	if teamID == domain.LocalTeamID {
		return ErrForbidden
	}
	role, ok, err := s.teams.FindRole(ctx, teamID, uid)
	if err != nil {
		return fmt.Errorf("find team role: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	if !role.CanWrite() {
		return ErrForbidden
	}
	return nil
}

func (s *SkillService) researchTeam(ctx context.Context, researchID string) (string, error) {
	r, err := s.researches.FindByID(ctx, researchID)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", ErrNotFound
	}
	return r.TeamID, nil
}

func (s *SkillService) build(ctx context.Context, tier domain.SkillTier, teamID, researchID string, in SkillInput) (*domain.Skill, error) {
	if err := validateSkill(in); err != nil {
		return nil, err
	}
	name := normalizeTitle(in.Name)
	slug := Slugify(name)
	taken, err := s.skills.SlugTaken(ctx, tier, teamID, researchID, slug)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrSkillSlugTaken
	}
	return &domain.Skill{
		ID:          uuid.New().String(),
		TeamID:      teamID,
		ResearchID:  researchID,
		UserID:      auth.UserIDFromContext(ctx),
		Slug:        slug,
		Name:        name,
		Tier:        tier,
		Description: normalizeContent(in.Description),
		Body:        normalizeContent(in.Body),
		Version:     1,
	}, nil
}

// inheritFrom fills the blanks in an edit from the row being copied, reading the
// body lazily because it is the one field a list never carries.
func inheritFrom(in SkillInput, src *domain.Skill, body func() (string, error)) SkillInput {
	if strings.TrimSpace(in.Name) == "" {
		in.Name = src.Name
	}
	if strings.TrimSpace(in.Description) == "" {
		in.Description = src.Description
	}
	if strings.TrimSpace(in.Body) == "" {
		if b, err := body(); err == nil {
			in.Body = b
		}
	}
	return in
}

func validateSkill(in SkillInput) error {
	return validateSkillFields(in, true)
}

func validateSkillFields(in SkillInput, checkBody bool) error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrSkillNameEmpty
	}
	if checkBody && strings.TrimSpace(in.Body) == "" {
		return ErrSkillBodyEmpty
	}
	// The description is the only line always in the agent's context and the
	// only thing it decides from. A skill saved without one is a skill that can
	// never load, so this is required rather than merely documented.
	if strings.TrimSpace(in.Description) == "" {
		return ErrSkillDescriptionEmpty
	}
	// Counted in runes, not bytes: the cap is about how much of the agent's
	// context the line occupies, and a Cyrillic trigger line is not half as
	// long as a Latin one.
	if len([]rune(in.Description)) > domain.SkillDescriptionMax {
		return ErrSkillDescriptionLong
	}
	if checkBody && len([]rune(in.Body)) > domain.SkillBodyMax {
		return ErrSkillBodyLong
	}
	return nil
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify derives the addressable name. It is deliberately lossy for
// non-Latin input, which falls back to a stable generated slug rather than
// producing an empty one — a Russian skill name is common in this product and
// must not fail to save.
func Slugify(name string) string {
	s := slugUnsafe.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" || s == "library" {
		// `library` is a literal path segment on the skills routes, so a skill
		// slugged that way would have its own body permanently shadowed by the
		// listing. Cheaper to refuse the name here than to reorder the routes.
		return "skill-" + uuid.New().String()[:8]
	}
	return s
}

// SkillCap is the budget a client is told about, so the frontend never carries
// its own copy of the number the server enforces.
func SkillCap() int { return domain.SkillsPerResearch }

// SkillErrorCode is the machine-readable name of a refusal, and it exists
// because two transports now have to tell the same two 409s apart.
//
// The REST handler needs it because the UI writes a different sentence for
// "six are already attached" than for "that one is already on". The MCP tools
// need it because an agent is even worse at matching on prose than a frontend
// is: it will retry a cap error forever if the only difference from
// already-attached is the wording. One switch, so an edit to a message cannot
// silently change what either of them does.
//
// An empty string means this is not a skill-specific refusal — the caller falls
// back to its ordinary error mapping.
func SkillErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrSkillCapReached):
		return "skill_cap_reached"
	case errors.Is(err, ErrSkillAlreadyAttached):
		return "already_attached"
	case errors.Is(err, ErrSkillSlugTaken):
		return "slug_taken"
	case errors.Is(err, ErrSkillInUse):
		return "skill_in_use"
	case errors.Is(err, ErrSkillAmbient),
		errors.Is(err, ErrSkillBuiltinWrite),
		errors.Is(err, ErrSkillNotPrivate):
		return "not_allowed"
	default:
		return ""
	}
}

// ResolveSlug turns the name an agent holds into the row the write methods
// take.
//
// Update and Delete are addressed by id, because a slug is only unique inside a
// scope and those two are reachable from a team's library as well as from a
// research. Over MCP there is no id in circulation: everything the agent has
// came out of an index that carries slugs. Rather than teach every tool to read
// before it writes, the resolution is one exported method that applies the same
// scoping rule Load does — what the research follows first, then what it could
// attach.
//
// It is a read, so it checks Read. The write methods it feeds check Write on
// whatever they resolve to, which is not always this research: promoting or
// deleting a team skill is a team act.
func (s *SkillService) ResolveSlug(ctx context.Context, researchID, slug string) (*domain.Skill, error) {
	if auth.ShareFromContext(ctx) != nil {
		return nil, ErrNotFound
	}
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}
	sk, err := s.resolveInResearch(ctx, researchID, slug)
	if err != nil {
		return nil, err
	}
	if sk == nil {
		return nil, ErrNotFound
	}
	return sk, nil
}

// CountChosen is how much of the budget is spent by a list of skills. Ambient
// product skills are outside it, so a list of seven can legitimately read 6.
func CountChosen(list []*domain.Skill) int {
	var n int
	for _, sk := range list {
		if !sk.Ambient {
			n++
		}
	}
	return n
}
