package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/uptrace/bun"
)

// countWhere is what this file is for. The acceptance criterion asks for the
// child tables to be counted rather than the foreign keys to be trusted — and
// the one table that carries the most interesting rule, `crossrefs`, declares
// no foreign keys at all in any dialect.
func countWhere(t *testing.T, db *bun.DB, table, column, value string) int {
	t.Helper()
	var n int
	err := db.NewSelect().ColumnExpr("COUNT(*)").TableExpr(table).
		Where("?=?", bun.Ident(column), value).Scan(context.Background(), &n)
	if err != nil {
		t.Fatalf("count %s where %s=%s: %v", table, column, value, err)
	}
	return n
}

// deleteEnv wires the services a deletion touches, plus the repositories the
// assertions read directly.
type deleteEnv struct {
	db       *bun.DB
	research *ResearchService
	section  *SectionService
	entry    *EntryService
	session  *SessionService
	task     *TaskService
	roadmap  *RoadmapService
	notifier *mockNotifier
}

func newDeleteEnv(t *testing.T) *deleteEnv {
	t.Helper()
	db := setupTestDB(t)
	log := slog.Default()
	notifier := &mockNotifier{}
	access := testAccess(db)

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	questionRepo := storage.NewQuestionRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	linkRepo := storage.NewExternalLinkRepository(db)

	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, access, sessionRepo,
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db),
		crossrefRepo, linkRepo, notifier, log)

	return &deleteEnv{
		db:       db,
		research: NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), access, notifier, log),
		section:  NewSectionService(sectionRepo, entryRepo, researchRepo, access, notifier, log),
		entry:    entrySvc,
		session:  NewSessionService(db, sessionRepo, questionRepo, researchRepo, access, entrySvc, notifier, log),
		task:     NewTaskService(storage.NewTaskRepository(db), researchRepo, access, entrySvc, notifier, log),
		roadmap: NewRoadmapService(storage.NewRoadmapRepository(db), storage.NewRoadmapNodeRepository(db),
			storage.NewRoadmapEdgeRepository(db), researchRepo, access, notifier, log),
		notifier: notifier,
	}
}

// populate fills a research with one of everything that hangs off one.
func (e *deleteEnv) populate(t *testing.T, ctx context.Context, name string) (*domain.Research, []*domain.Section) {
	t.Helper()
	research, sections, err := e.research.Create(ctx, CreateResearchRequest{
		Name:        name,
		Description: "d",
		Goal:        "g",
		Sections:    []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}, {Name: "s2", DisplayName: "S2"}},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}

	if _, err := e.entry.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Title: "First", Content: "A finding, with a link to https://example.com/paper",
	}); err != nil {
		t.Fatalf("create entry: %v", err)
	}

	if _, _, err := e.session.Create(ctx, CreateSessionRequest{
		ResearchID: research.ID, Title: "Kickoff",
		Questions: []CreateQuestionRequest{{Text: "Why?"}, {Text: "How?"}},
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := e.task.Create(ctx, CreateTaskRequest{ResearchID: research.ID, Title: "A task"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := e.roadmap.Create(ctx, CreateRoadmapRequest{ResearchID: research.ID, Title: "Plan"}); err != nil {
		t.Fatalf("create roadmap: %v", err)
	}
	return research, sections
}

// TestResearchDelete_CascadeLeavesNothingBehind counts every child table rather
// than trusting the schema.
func TestResearchDelete_CascadeLeavesNothingBehind(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()
	research, _ := env.populate(t, ctx, "To be deleted")

	// Sanity: the fixture is not empty, or the assertions below prove nothing.
	before := map[string]int{
		"sections": countWhere(t, env.db, "sections", "research_id", research.ID),
		"entries":  countWhere(t, env.db, "entries", "research_id", research.ID),
		"sessions": countWhere(t, env.db, "sessions", "research_id", research.ID),
		"tasks":    countWhere(t, env.db, "tasks", "research_id", research.ID),
		"roadmaps": countWhere(t, env.db, "roadmaps", "research_id", research.ID),
	}
	for table, n := range before {
		if n == 0 {
			t.Fatalf("fixture has no %s — the cascade assertion would pass vacuously", table)
		}
	}
	if countWhere(t, env.db, "external_links", "research_id", research.ID) == 0 {
		t.Fatal("fixture has no external_links")
	}

	if err := env.research.Delete(ctx, research.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, table := range []string{"sections", "entries", "sessions", "tasks", "roadmaps",
		"external_links", "entry_blocks", "entry_revisions", "annotations", "shares",
		"research_memory", "research_skills"} {
		if n := countWhere(t, env.db, table, "research_id", research.ID); n != 0 {
			t.Errorf("%s: %d row(s) survived the delete", table, n)
		}
	}
	// Questions have no research_id: they hang off sessions, which is exactly
	// the kind of second-hop the FK declarations are easy to be wrong about.
	var questions int
	if err := env.db.NewSelect().ColumnExpr("COUNT(*)").TableExpr("questions AS q").
		Join("JOIN sessions AS s ON s.id = q.session_id").
		Where("s.research_id=?", research.ID).Scan(ctx, &questions); err != nil {
		t.Fatalf("count questions: %v", err)
	}
	if questions != 0 {
		t.Errorf("questions: %d row(s) survived", questions)
	}
	if n := countWhere(t, env.db, "crossrefs", "source_research_id", research.ID); n != 0 {
		t.Errorf("crossrefs: %d outgoing row(s) survived", n)
	}
	if n := countWhere(t, env.db, "researches", "id", research.ID); n != 0 {
		t.Error("the research row itself survived")
	}
}

// TestResearchDelete_CountersAreReclaimed — storage_counters sits outside the
// foreign-key graph on purpose, so short codes survive deleting a document.
// Once the research is gone its counters can never be reached again.
func TestResearchDelete_CountersAreReclaimed(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()
	research, _ := env.populate(t, ctx, "Counters")

	var before int
	if err := env.db.NewSelect().ColumnExpr("COUNT(*)").TableExpr("storage_counters").
		Where("scope_key LIKE ?", "%:"+research.ID).Scan(ctx, &before); err != nil {
		t.Fatalf("count counters: %v", err)
	}
	if before == 0 {
		t.Fatal("no counters were allocated for the fixture")
	}

	if err := env.research.Delete(ctx, research.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var after int
	if err := env.db.NewSelect().ColumnExpr("COUNT(*)").TableExpr("storage_counters").
		Where("scope_key LIKE ?", "%:"+research.ID).Scan(ctx, &after); err != nil {
		t.Fatalf("count counters: %v", err)
	}
	if after != 0 {
		t.Errorf("%d counter row(s) survived", after)
	}
}

// TestResearchDelete_GlobalResearchCounterNeverRewinds pins the invariant the
// rest of the product rests on: an R code is never handed out twice.
//
// A reference into a deleted research survives as unresolved text keeping its
// `target_ref` — `[[R7]]` stays written as `[[R7]]`. If R7 could ever be
// allocated again, the crossref rebuild would silently re-point somebody's
// citation at a research they have never seen, in a team they may not be in.
// That is why the delete matches counter rows by `%:<uuid>` rather than by
// prefix: the global key is `researches:R:`, whose scope is empty, so it cannot
// match — and `reserveCode` only ever moves a value up. Per-research counters
// (entries, sections, sessions, tasks, roadmaps) going away with their research
// is fine; their scope goes with them.
func TestResearchDelete_GlobalResearchCounterNeverRewinds(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()
	research, _ := env.populate(t, ctx, "First")
	deletedCode := research.Code

	var counterBefore int
	if err := env.db.NewSelect().Column("value").Table("storage_counters").
		Where("scope_key=?", "researches:R:").Scan(ctx, &counterBefore); err != nil {
		t.Fatalf("read the global research counter: %v", err)
	}

	if err := env.research.Delete(ctx, research.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var counterAfter int
	if err := env.db.NewSelect().Column("value").Table("storage_counters").
		Where("scope_key=?", "researches:R:").Scan(ctx, &counterAfter); err != nil {
		t.Fatalf("the global research counter row did not survive the delete: %v", err)
	}
	if counterAfter != counterBefore {
		t.Errorf("global research counter moved %d -> %d; an R code must never be reallocated", counterBefore, counterAfter)
	}

	next, _, err := env.research.Create(ctx, CreateResearchRequest{Name: "Second", Description: "d", Goal: "g"})
	if err != nil {
		t.Fatalf("create after delete: %v", err)
	}
	if next.Code == deletedCode {
		t.Errorf("the research created after the delete reused %s — every [[%s]] written anywhere now points at it",
			deletedCode, deletedCode)
	}
}

// TestResearchDelete_IncomingReferencesBecomeUnresolved is the rule the issue
// singles out. If R2 says "this rests on [[R1:E5]]" and R1 goes, deleting that
// row would edit R2 — it would make R2's own history a lie about what it once
// cited. The row stays and stops resolving.
func TestResearchDelete_IncomingReferencesBecomeUnresolved(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()

	target, targetSections, err := env.research.Create(ctx, CreateResearchRequest{
		Name: "Cited", Description: "d", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if _, err := env.entry.Create(ctx, CreateEntryRequest{
		ResearchID: target.ID, SectionID: targetSections[0].ID, Title: "Cited doc", Content: "the source",
	}); err != nil {
		t.Fatalf("create target entry: %v", err)
	}

	citing, citingSections, err := env.research.Create(ctx, CreateResearchRequest{
		Name: "Citing", Description: "d", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create citing: %v", err)
	}
	citer, err := env.entry.Create(ctx, CreateEntryRequest{
		ResearchID: citing.ID, SectionID: citingSections[0].ID,
		Title: "Cites", Content: "This rests on [[" + target.Code + ":E1]] entirely.",
	})
	if err != nil {
		t.Fatalf("create citing entry: %v", err)
	}

	refs, err := storage.NewCrossRefRepository(env.db).FindBySourceEntry(ctx, citer.ID)
	if err != nil {
		t.Fatalf("read refs: %v", err)
	}
	if len(refs) != 1 || !refs[0].Resolved {
		t.Fatalf("fixture did not resolve the reference: %+v", refs)
	}

	if err := env.research.Delete(ctx, target.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	refs, err = storage.NewCrossRefRepository(env.db).FindBySourceEntry(ctx, citer.ID)
	if err != nil {
		t.Fatalf("read refs after delete: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("the citing document lost its reference: got %d rows, want 1 — deleting it would edit a research nobody asked to change", len(refs))
	}
	if refs[0].Resolved {
		t.Error("the reference still reports itself resolved, so the UI would render a link into nothing")
	}
	if refs[0].TargetResearchID != "" || refs[0].TargetEntryID != "" {
		t.Errorf("target ids survived: research=%q entry=%q", refs[0].TargetResearchID, refs[0].TargetEntryID)
	}
	if refs[0].TargetRef == "" {
		t.Error("target_ref was cleared; the reader must still see what was cited")
	}
}

// TestResearchDelete_EventReachesTheAudienceThatLostIt — a research event is
// delivered by asking who may read the research, and after the delete nobody
// may, because it is not there. The event has to name its recipients before
// they stop qualifying.
func TestResearchDelete_EventNamesItsRecipients(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()

	owner := createTestUser(t, env.db, "owner@example.com", "Owner")
	editor := createTestUser(t, env.db, "editor@example.com", "Editor")
	ownerCtx := auth.WithUser(ctx, owner)

	research, _, err := env.research.Create(ownerCtx, CreateResearchRequest{
		Name: "Shared", Description: "d", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	addToTeam(t, env.db, research.TeamID, editor.ID, domain.TeamEditor)

	env.notifier.reset()
	if err := env.research.Delete(ownerCtx, research.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var plain int
	directed := map[string]bool{}
	for _, e := range env.notifier.events {
		if e.Type != "research.deleted" {
			continue
		}
		if e.ResearchCode != research.Code {
			t.Errorf("event carries research_code %q, want %q — the web UI routes by code and cannot look it up after the delete", e.ResearchCode, research.Code)
		}
		if e.TargetUserID == "" {
			plain++
			continue
		}
		directed[e.TargetUserID] = true
	}
	if plain != 1 {
		t.Errorf("undirected research.deleted events: %d, want 1 (it is what reaches a client when auth is off)", plain)
	}
	for _, u := range []*domain.User{owner, editor} {
		if !directed[u.ID] {
			t.Errorf("no directed research.deleted for %s — with auth on, the ordinary scope cannot reach them", u.Email)
		}
	}
}

// TestResearchDelete_RefusedForNonOwners — an editor is trusted with the
// contents of a research and not with its existence.
func TestResearchDelete_RefusedForNonOwners(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()

	owner := createTestUser(t, env.db, "owner@example.com", "Owner")
	editor := createTestUser(t, env.db, "editor@example.com", "Editor")
	stranger := createTestUser(t, env.db, "stranger@example.com", "Stranger")

	research, _, err := env.research.Create(auth.WithUser(ctx, owner), CreateResearchRequest{
		Name: "Owned", Description: "d", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	addToTeam(t, env.db, research.TeamID, editor.ID, domain.TeamEditor)

	if err := env.research.Delete(auth.WithUser(ctx, editor), research.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("editor: got %v, want ErrForbidden", err)
	}
	// A non-member gets ErrNotFound, not ErrForbidden: confirming the research
	// exists is itself information.
	if err := env.research.Delete(auth.WithUser(ctx, stranger), research.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("stranger: got %v, want ErrNotFound", err)
	}
	if n := countWhere(t, env.db, "researches", "id", research.ID); n != 1 {
		t.Error("a refused delete removed the research anyway")
	}
	if err := env.research.Delete(auth.WithUser(ctx, owner), research.ID); err != nil {
		t.Errorf("owner: %v", err)
	}
}

// TestSectionDelete_RefusesToTakeDocumentsSilently — deleting a section that
// silently takes five documents with it is the accident the confirmation
// exists to prevent, and an API that makes it a one-liner has moved the
// accident rather than removed it.
func TestSectionDelete_RefusesToTakeDocumentsSilently(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()
	research, sections, err := env.research.Create(ctx, CreateResearchRequest{
		Name: "Sections", Description: "d", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "full", DisplayName: "Full"}, {Name: "empty", DisplayName: "Empty"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	entry, err := env.entry.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID, Title: "Doc", Content: "text",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	err = env.section.Delete(ctx, sections[0].ID, false)
	if !errors.Is(err, ErrSectionNotEmpty) {
		t.Fatalf("non-empty section without force: got %v, want ErrSectionNotEmpty", err)
	}
	if !strings.Contains(err.Error(), "1 document") {
		t.Errorf("the refusal does not say how much is at stake: %q", err)
	}
	if n := countWhere(t, env.db, "entries", "id", entry.ID); n != 1 {
		t.Fatal("the refused delete removed the document anyway")
	}

	// An empty one needs no ceremony.
	if err := env.section.Delete(ctx, sections[1].ID, false); err != nil {
		t.Fatalf("empty section: %v", err)
	}

	if err := env.section.Delete(ctx, sections[0].ID, true); err != nil {
		t.Fatalf("forced: %v", err)
	}
	if n := countWhere(t, env.db, "entries", "id", entry.ID); n != 0 {
		t.Error("the document survived a forced section delete")
	}
	// The reference tables have no foreign key that fires here — the research
	// is staying, so only this code cleans them.
	if n := countWhere(t, env.db, "crossrefs", "source_id", entry.ID); n != 0 {
		t.Errorf("crossrefs: %d row(s) left pointing out of a deleted document", n)
	}
	if n := countWhere(t, env.db, "external_links", "source_id", entry.ID); n != 0 {
		t.Errorf("external_links: %d orphaned row(s)", n)
	}
}

// TestSessionDelete_KeepsTheDocumentsItProduced — a finding is not an artefact
// of the conversation that produced it.
func TestSessionDelete_KeepsTheDocumentsItProduced(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()
	research, sections, err := env.research.Create(ctx, CreateResearchRequest{
		Name: "Sessions", Description: "d", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	session, _, err := env.session.Create(ctx, CreateSessionRequest{
		ResearchID: research.ID, Title: "Interview",
		Questions: []CreateQuestionRequest{{Text: "Why?"}},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entry, err := env.entry.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		SessionID: session.ID, Title: "Finding", Content: "what we learned",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	if err := env.session.Delete(ctx, session.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if n := countWhere(t, env.db, "questions", "session_id", session.ID); n != 0 {
		t.Errorf("questions: %d row(s) survived", n)
	}
	if n := countWhere(t, env.db, "entries", "id", entry.ID); n != 1 {
		t.Fatal("the document written during the session was deleted with it")
	}
	var sessionID *string
	if err := env.db.NewSelect().Column("session_id").TableExpr("entries").
		Where("id=?", entry.ID).Scan(ctx, &sessionID); err != nil {
		t.Fatalf("read session_id: %v", err)
	}
	if sessionID != nil && *sessionID != "" {
		t.Errorf("entry.session_id = %q, want NULL", *sessionID)
	}
}

// TestDelete_ClearsReferencesWrittenByMarks is the fourth place the missing
// foreign keys on `crossrefs` bite.
//
// A mark's resolution can cite other work, and those rows are stored under
// source_type "annotation". The marks themselves cascade with the document, so
// nothing was left to point at — but the rows stayed `resolved`, which means
// the cited document went on showing a backlink from a mark that no longer
// exists, and the deletion preview counted it as work that would break.
//
// All three deletes that can destroy a mark are covered here, because each has
// its own path to it: one document, a section holding it, and the mark itself.
func TestDelete_ClearsReferencesWrittenByMarks(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()

	research, sections, err := env.research.Create(ctx, CreateResearchRequest{
		Name: "Marks", Goal: "g",
		Sections: []CreateSectionRequest{
			{Name: "s1", DisplayName: "One"},
			{Name: "s2", DisplayName: "Two"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// E1 is the document everything cites; the marks live on E2 and E3.
	cited, err := env.entry.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Title: "Cited", Content: "Costs fall by 40 percent in year two.",
	})
	if err != nil {
		t.Fatalf("create cited: %v", err)
	}

	annotations := NewAnnotationService(storage.NewAnnotationRepository(env.db),
		storage.NewEntryRepository(env.db), storage.NewEntryRevisionRepository(env.db),
		testAccess(env.db), env.entry, env.entry, env.notifier, slog.Default())

	// A mark on a document in the *second* section, so the section delete below
	// takes it with the document it hangs off.
	marked, err := env.entry.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[1].ID,
		Title: "Marked", Content: "Seat based pricing is assumed throughout.",
	})
	if err != nil {
		t.Fatalf("create marked: %v", err)
	}
	mark, err := annotations.Create(ctx, CreateAnnotationRequest{
		EntryID: marked.ID, Quote: domain.Quote{Exact: "Seat based"},
		Kind: domain.AnnotationDig, Body: "On what evidence?",
	})
	if err != nil {
		t.Fatalf("create mark: %v", err)
	}
	if _, err := annotations.Answer(ctx, mark.ID, AnswerAnnotationRequest{
		Resolution: "Settled in [[" + cited.Code + "]].",
	}); err != nil {
		t.Fatalf("answer mark: %v", err)
	}

	refsFrom := func(sourceID string) int {
		t.Helper()
		var n int
		if err := env.db.NewSelect().ColumnExpr("COUNT(*)").TableExpr("crossrefs").
			Where("source_type=?", "annotation").Where("source_id=?", sourceID).Scan(ctx, &n); err != nil {
			t.Fatalf("count annotation crossrefs: %v", err)
		}
		return n
	}
	if refsFrom(mark.ID) != 1 {
		t.Fatalf("setup: the resolution's reference was not stored")
	}

	// 1. Deleting the mark itself.
	if err := annotations.Delete(ctx, mark.ID); err != nil {
		t.Fatalf("delete mark: %v", err)
	}
	if n := refsFrom(mark.ID); n != 0 {
		t.Errorf("%d reference(s) survived the mark that wrote them", n)
	}

	// 2. Deleting the document the mark is on.
	second, err := annotations.Create(ctx, CreateAnnotationRequest{
		EntryID: marked.ID, Quote: domain.Quote{Exact: "pricing"},
		Kind: domain.AnnotationDig, Body: "Which plan?",
	})
	if err != nil {
		t.Fatalf("create second mark: %v", err)
	}
	if _, err := annotations.Answer(ctx, second.ID, AnswerAnnotationRequest{
		Resolution: "See [[" + cited.Code + "]].",
	}); err != nil {
		t.Fatalf("answer second mark: %v", err)
	}
	if refsFrom(second.ID) != 1 {
		t.Fatalf("setup: the second resolution's reference was not stored")
	}
	if err := env.entry.Delete(ctx, marked.ID); err != nil {
		t.Fatalf("delete document: %v", err)
	}
	if n := refsFrom(second.ID); n != 0 {
		t.Errorf("%d reference(s) survived the document their mark was on", n)
	}

	// 3. Force-deleting a section holding a marked document.
	third, err := env.entry.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[1].ID,
		Title: "Also marked", Content: "Churn is assumed flat.",
	})
	if err != nil {
		t.Fatalf("create third: %v", err)
	}
	thirdMark, err := annotations.Create(ctx, CreateAnnotationRequest{
		EntryID: third.ID, Quote: domain.Quote{Exact: "Churn"},
		Kind: domain.AnnotationDig, Body: "Flat on what basis?",
	})
	if err != nil {
		t.Fatalf("create third mark: %v", err)
	}
	if _, err := annotations.Answer(ctx, thirdMark.ID, AnswerAnnotationRequest{
		Resolution: "Answered in [[" + cited.Code + "]].",
	}); err != nil {
		t.Fatalf("answer third mark: %v", err)
	}
	if refsFrom(thirdMark.ID) != 1 {
		t.Fatalf("setup: the third resolution's reference was not stored")
	}

	env.notifier.reset()
	if err := env.section.Delete(ctx, sections[1].ID, true); err != nil {
		t.Fatalf("force-delete section: %v", err)
	}
	if n := refsFrom(thirdMark.ID); n != 0 {
		t.Errorf("%d reference(s) survived the section that held their mark's document", n)
	}

	// And the documents that went with the section are announced by id. A page
	// open on one of them hears `section.deleted` and cannot tell whether it was
	// looking at a child of it, so without this it kept a document that is gone.
	var announced []string
	for _, e := range env.notifier.events {
		if e.Type == "entry.deleted" {
			announced = append(announced, e.EntityID)
		}
	}
	if len(announced) != 1 || announced[0] != third.ID {
		t.Errorf("entry.deleted events = %v, want exactly [%s]", announced, third.ID)
	}
	if !env.notifier.hasEvent("section.deleted") {
		t.Error("the section itself was not announced")
	}
}

// TestDeletionSummary_HidesCitingResearchesTheCallerMayNotRead is the
// cross-team leak the fleet found, pinned.
//
// A cross-reference resolves without asking what its author may see — that is
// deliberate, and it is why the raw query names every project that cites this
// one. Reporting them unfiltered told an owner the short code and the *name* of
// a project in a team they are not in, in a dialog they open by accident.
//
// The count goes with the names: `Access.VisibleIncomingCrossRefs` already
// states that even a bare count announces that an unseen project cites this
// one.
func TestDeletionSummary_HidesCitingResearchesTheCallerMayNotRead(t *testing.T) {
	env := newDeleteEnv(t)
	alice, bob := setupTwoUsers(t, env.db)
	aliceCtx, bobCtx := userCtx(alice), userCtx(bob)

	target, sections, err := env.research.Create(aliceCtx, CreateResearchRequest{
		Name: "Cited", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create alice's research: %v", err)
	}
	if _, err := env.entry.Create(aliceCtx, CreateEntryRequest{
		ResearchID: target.ID, SectionID: sections[0].ID, Title: "Seed", Content: "body",
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// Bob's research, in a team Alice is not in, citing Alice's.
	citing, bobSections, err := env.research.Create(bobCtx, CreateResearchRequest{
		Name: "Bob's confidential programme", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create bob's research: %v", err)
	}
	if _, err := env.entry.Create(bobCtx, CreateEntryRequest{
		ResearchID: citing.ID, SectionID: bobSections[0].ID,
		Title: "Cites", Content: "This rests on [[" + target.Code + ":E1]] entirely.",
	}); err != nil {
		t.Fatalf("seed citing entry: %v", err)
	}

	got, err := env.research.DeletionSummary(aliceCtx, target.ID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	for _, c := range got.IncomingFrom {
		t.Errorf("the preview named %s %q, which is in a team the caller is not in", c.Code, c.Name)
	}
	if got.IncomingRefs != 0 {
		t.Errorf("incoming_refs = %d, want 0 — the count announces the same thing the names do", got.IncomingRefs)
	}
	if got.IncomingFromTotal != 0 {
		t.Errorf("incoming_from_total = %d, want 0", got.IncomingFromTotal)
	}

	// And Bob, who may read his own, is still told what he would break.
	his, err := env.research.DeletionSummary(bobCtx, citing.ID)
	if err != nil {
		t.Fatalf("summary for bob: %v", err)
	}
	if his.Entries != 1 {
		t.Errorf("bob's own preview counted %d documents, want 1", his.Entries)
	}
}

// TestDeletes_RefuseAStranger is the ownership half, for the four deletes and
// the preview. The role matrix next door covers members of the team; nothing
// covered somebody outside it, and outside is where the interesting answer is:
// ErrNotFound, never ErrForbidden, because confirming that a section or a
// session exists is itself information about someone else's work.
func TestDeletes_RefuseAStranger(t *testing.T) {
	env := newDeleteEnv(t)
	alice, bob := setupTwoUsers(t, env.db)
	aliceCtx, strangerCtx := userCtx(alice), userCtx(bob)

	research, sections, err := env.research.Create(aliceCtx, CreateResearchRequest{
		Name: "Alice's", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	session, questions, err := env.session.Create(aliceCtx, CreateSessionRequest{
		ResearchID: research.ID, Focus: "f",
		Questions: []CreateQuestionRequest{{Text: "Why?"}},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	question := questions[0]

	refusals := map[string]func() error{
		"section delete":  func() error { return env.section.Delete(strangerCtx, sections[0].ID, false) },
		"session delete":  func() error { return env.session.Delete(strangerCtx, session.ID) },
		"question delete": func() error { return env.session.DeleteQuestion(strangerCtx, question.ID) },
		"research delete": func() error { return env.research.Delete(strangerCtx, research.ID) },
		"delete preview":  func() error { _, err := env.research.DeletionSummary(strangerCtx, research.ID); return err },
	}
	for name, call := range refusals {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}

	// Nothing was destroyed on the way to those refusals.
	if n := countWhere(t, env.db, "sections", "research_id", research.ID); n != 1 {
		t.Errorf("%d section(s) left, want 1", n)
	}
	if n := countWhere(t, env.db, "sessions", "research_id", research.ID); n != 1 {
		t.Errorf("%d session(s) left, want 1", n)
	}
	if n := countWhere(t, env.db, "questions", "session_id", session.ID); n != 1 {
		t.Errorf("%d question(s) left, want 1", n)
	}
	if n := countWhere(t, env.db, "researches", "id", research.ID); n != 1 {
		t.Error("the research was deleted by a stranger")
	}
}

// TestDeletionSummary_CountsWhatWouldGo — "delete R7" and "delete 2 sections,
// 1 document, 1 session, 2 questions and 1 task" are different decisions.
func TestDeletionSummary_CountsWhatWouldGo(t *testing.T) {
	env := newDeleteEnv(t)
	ctx := context.Background()
	research, _ := env.populate(t, ctx, "Summary")

	got, err := env.research.DeletionSummary(ctx, research.ID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	want := domain.DeletionSummary{
		Sections: 2, Entries: 1, Sessions: 1, Questions: 2, Tasks: 1, Roadmaps: 1,
	}
	if got.Sections != want.Sections || got.Entries != want.Entries ||
		got.Sessions != want.Sessions || got.Questions != want.Questions ||
		got.Tasks != want.Tasks || got.Roadmaps != want.Roadmaps {
		t.Errorf("summary = %+v, want at least %+v", got, want)
	}
}
