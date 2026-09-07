package service

import (
	"context"
	"log/slog"
	"testing"

	"github.com/dovod-app/app/internal/storage"
	"github.com/uptrace/bun"
)

// forwardKit is the shape every case here needs: the services that create the
// things references point at, all wired to one reference table.
type forwardKit struct {
	db        *bun.DB
	entry     *EntryService
	research  *ResearchService
	task      *TaskService
	roadmap   *RoadmapService
	crossrefs *storage.CrossRefRepository
	notifier  *mockNotifier
}

func newForwardKit(t *testing.T) *forwardKit {
	t.Helper()
	db := setupTestDB(t)
	log := slog.Default()
	notifier := &mockNotifier{}

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	taskRepo := storage.NewTaskRepository(db)
	roadmapRepo := storage.NewRoadmapRepository(db)
	nodeRepo := storage.NewRoadmapNodeRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)

	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil,
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, log)
	entrySvc.SetRoadmapRepos(roadmapRepo, nodeRepo)
	entrySvc.SetTaskRepo(taskRepo)

	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	taskSvc := NewTaskService(taskRepo, researchRepo, testAccess(db), entrySvc, notifier, log)
	roadmapSvc := NewRoadmapService(roadmapRepo, nodeRepo, storage.NewRoadmapEdgeRepository(db),
		researchRepo, testAccess(db), notifier, log)

	// The wiring under test: main.go does exactly this.
	researchSvc.SetDanglingResolver(entrySvc)
	taskSvc.SetDanglingResolver(entrySvc)
	roadmapSvc.SetDanglingResolver(entrySvc)

	return &forwardKit{
		db: db, entry: entrySvc, research: researchSvc, task: taskSvc,
		roadmap: roadmapSvc, crossrefs: crossrefRepo, notifier: notifier,
	}
}

// citing creates a document whose text names a code that does not exist yet,
// and asserts that it is stored dangling.
func (k *forwardKit) citing(t *testing.T, ctx context.Context, researchID, sectionID, text, ref string) string {
	t.Helper()
	e, err := k.entry.Create(ctx, CreateEntryRequest{
		ResearchID: researchID, SectionID: sectionID, Content: text,
	})
	if err != nil {
		t.Fatalf("create citing entry: %v", err)
	}
	if k.resolvedRef(t, ctx, researchID, e.ID, ref) {
		t.Fatalf("%q resolved before its target existed, so this test proves nothing", ref)
	}
	return e.ID
}

func (k *forwardKit) resolvedRef(t *testing.T, ctx context.Context, researchID, sourceID, ref string) bool {
	t.Helper()
	refs, err := k.crossrefs.FindByResearch(ctx, researchID)
	if err != nil {
		t.Fatalf("list crossrefs: %v", err)
	}
	for _, r := range refs {
		if r.SourceID == sourceID && r.TargetRef == ref {
			return r.Resolved
		}
	}
	t.Fatalf("no reference row for %q from %s", ref, sourceID)
	return false
}

func (k *forwardKit) refRow(t *testing.T, ctx context.Context, researchID, sourceID, ref string) (targetEntryID, targetResearchID, targetRoadmapID, targetNodeID string) {
	t.Helper()
	refs, _ := k.crossrefs.FindByResearch(ctx, researchID)
	for _, r := range refs {
		if r.SourceID == sourceID && r.TargetRef == ref {
			return r.TargetEntryID, r.TargetResearchID, r.TargetRoadmapID, r.TargetNodeID
		}
	}
	t.Fatalf("no reference row for %q", ref)
	return "", "", "", ""
}

func (k *forwardKit) research1(t *testing.T, ctx context.Context, name string) (researchID, sectionID string) {
	t.Helper()
	r, sections, err := k.research.Create(ctx, CreateResearchRequest{
		Name: name, Sections: []CreateSectionRequest{{Name: "s1"}},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}
	return r.ID, sections[0].ID
}

// fillTo creates documents in a research until one is given the wanted code,
// and returns its id.
func (k *forwardKit) fillTo(t *testing.T, ctx context.Context, researchID, sectionID, code string) string {
	t.Helper()
	for i := 0; i < 40; i++ {
		e, err := k.entry.Create(ctx, CreateEntryRequest{
			ResearchID: researchID, SectionID: sectionID, Content: "filler",
		})
		if err != nil {
			t.Fatalf("create filler: %v", err)
		}
		if e.Code == code {
			return e.ID
		}
	}
	t.Fatalf("never reached %s", code)
	return ""
}

// "This rests on [[E20]]" written before E20 exists is the normal order, not an
// edge case. Until the fix this reference stayed dangling forever: the graph
// drew no edge while the prose rendered a live link, and only a route with no
// button and no tool repaired it.
func TestForwardRefs_ResolveWhenTheTargetIsCreated(t *testing.T) {
	ctx := context.Background()
	k := newForwardKit(t)
	rid, sid := k.research1(t, ctx, "Forward")

	source := k.citing(t, ctx, rid, sid, "This rests on [[E20]].", "E20")

	// Nineteen documents, then the twentieth — the one the reference named.
	target := k.fillTo(t, ctx, rid, sid, "E20")

	if !k.resolvedRef(t, ctx, rid, source, "E20") {
		t.Fatal("the reference did not resolve when E20 was created")
	}
	gotEntry, gotResearch, _, _ := k.refRow(t, ctx, rid, source, "E20")
	if gotEntry != target {
		t.Errorf("target_entry_id = %q, want %q", gotEntry, target)
	}
	if gotResearch != rid {
		t.Errorf("target_research_id = %q, want %q", gotResearch, rid)
	}

	// And the page that draws the graph is told, or it keeps the old edges.
	if !k.notifier.hasEvent("crossrefs.resolved") {
		t.Error("no crossrefs.resolved event, so an open graph never repaints")
	}
}

// An entry code is unique only inside its research. Resolving `[[E20]]` against
// any E20 anywhere would point one team's reference at another team's document.
func TestForwardRefs_DoNotCrossAResearchBoundaryTheCodeDoesNotName(t *testing.T) {
	ctx := context.Background()
	k := newForwardKit(t)
	ridA, sidA := k.research1(t, ctx, "A")
	ridB, sidB := k.research1(t, ctx, "B")

	// E4, not E1: the citing document is itself A's first entry, and a document
	// that cites its own code resolves immediately and would prove nothing.
	source := k.citing(t, ctx, ridA, sidA, "See [[E4]].", "E4")

	// B gets an E4 first. A's reference must not notice — entry codes repeat
	// across researches, and matching globally would point one team's reference
	// at another team's document.
	k.fillTo(t, ctx, ridB, sidB, "E4")
	if k.resolvedRef(t, ctx, ridA, source, "E4") {
		t.Fatal("a reference in A resolved against a document in B")
	}

	// A's own E4 resolves it.
	k.fillTo(t, ctx, ridA, sidA, "E4")
	if !k.resolvedRef(t, ctx, ridA, source, "E4") {
		t.Fatal("a reference did not resolve against its own research's document")
	}
	_, gotResearch, _, _ := k.refRow(t, ctx, ridA, source, "E4")
	if gotResearch != ridA {
		t.Errorf("target_research_id = %q, want the citing research %q", gotResearch, ridA)
	}
}

// The cross-research form carries the research code, so it is matched
// everywhere — and it is the one shape that must reach outside its own research.
func TestForwardRefs_CrossResearchFormResolves(t *testing.T) {
	ctx := context.Background()
	k := newForwardKit(t)
	ridA, sidA := k.research1(t, ctx, "Citer")

	b, sectionsB, err := k.research.Create(ctx, CreateResearchRequest{
		Name: "Cited", Sections: []CreateSectionRequest{{Name: "s1"}},
	})
	if err != nil {
		t.Fatalf("create research B: %v", err)
	}
	ref := b.Code + ":E1"

	source := k.citing(t, ctx, ridA, sidA, "Decided in [["+ref+"]].", ref)

	target, err := k.entry.Create(ctx, CreateEntryRequest{
		ResearchID: b.ID, SectionID: sectionsB[0].ID, Content: "The decision",
	})
	if err != nil {
		t.Fatalf("create in B: %v", err)
	}

	if !k.resolvedRef(t, ctx, ridA, source, ref) {
		t.Fatalf("%q did not resolve when the target was created", ref)
	}
	gotEntry, gotResearch, _, _ := k.refRow(t, ctx, ridA, source, ref)
	if gotEntry != target.ID {
		t.Errorf("target_entry_id = %q, want %q", gotEntry, target.ID)
	}
	if gotResearch != b.ID {
		t.Errorf("target_research_id = %q, want the cited research %q", gotResearch, b.ID)
	}
}

// Every other kind of code a reference can name.
func TestForwardRefs_TaskRoadmapNodeAndResearch(t *testing.T) {
	ctx := context.Background()
	k := newForwardKit(t)
	rid, sid := k.research1(t, ctx, "Everything")

	task := k.citing(t, ctx, rid, sid, "Chase [[T1]].", "T1")
	roadmap := k.citing(t, ctx, rid, sid, "Plan is [[RM1]].", "RM1")
	node := k.citing(t, ctx, rid, sid, "Step [[RM1:N1]].", "RM1:N1")

	if _, err := k.task.Create(ctx, CreateTaskRequest{ResearchID: rid, Title: "Chase it"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if !k.resolvedRef(t, ctx, rid, task, "T1") {
		t.Error("[[T1]] did not resolve when the task was created")
	}

	rm, err := k.roadmap.Create(ctx, CreateRoadmapRequest{
		ResearchID: rid, Title: "Plan",
		Nodes: []CreateRoadmapNodeRequest{{TempID: "n1", Title: "First step"}},
	})
	if err != nil {
		t.Fatalf("create roadmap: %v", err)
	}
	if !k.resolvedRef(t, ctx, rid, roadmap, "RM1") {
		t.Error("[[RM1]] did not resolve when the roadmap was created")
	}
	if !k.resolvedRef(t, ctx, rid, node, "RM1:N1") {
		t.Error("[[RM1:N1]] did not resolve when the node was created")
	}
	_, _, gotRoadmap, gotNode := k.refRow(t, ctx, rid, node, "RM1:N1")
	if gotRoadmap != rm.ID {
		t.Errorf("target_roadmap_id = %q, want %q", gotRoadmap, rm.ID)
	}
	if gotNode == "" {
		t.Error("a resolved node reference stored no node id, so the graph has no edge to draw")
	}

	// A node added later to a roadmap that already existed.
	later := k.citing(t, ctx, rid, sid, "Then [[RM1:N2]].", "RM1:N2")
	if _, err := k.roadmap.AddNodes(ctx, rm.ID,
		[]CreateRoadmapNodeRequest{{Title: "Second step"}}, nil); err != nil {
		t.Fatalf("add node: %v", err)
	}
	if !k.resolvedRef(t, ctx, rid, later, "RM1:N2") {
		t.Error("[[RM1:N2]] did not resolve when the node was added to an existing roadmap")
	}

	// And a research cited before it was started.
	pending := k.citing(t, ctx, rid, sid, "Compare with [[R9]].", "R9")
	for i := 0; i < 12; i++ {
		r, _, err := k.research.Create(ctx, CreateResearchRequest{Name: "filler"})
		if err != nil {
			t.Fatalf("create research: %v", err)
		}
		if r.Code == "R9" {
			break
		}
	}
	if !k.resolvedRef(t, ctx, rid, pending, "R9") {
		t.Error("[[R9]] did not resolve when R9 was created")
	}
}

// Every research that has a roadmap has an RM1, and every roadmap's first node
// is N1 — the codes are allocated per research, not globally.
//
// The first version of this repair matched them globally, so creating a roadmap
// in one research rewrote another research's `[[RM1]]` to point at it. That was
// worse than the bug being fixed: silent, cross-tenant, and permanent, because
// the `resolved=0` guard then refuses to repair the row when the research's own
// RM1 finally appears.
func TestForwardRefs_RoadmapAndNodeCodesDoNotCrossResearches(t *testing.T) {
	ctx := context.Background()
	k := newForwardKit(t)
	ridA, sidA := k.research1(t, ctx, "Cites its own plan")
	ridB, _ := k.research1(t, ctx, "Has a plan first")

	roadmapRef := k.citing(t, ctx, ridA, sidA, "Plan is [[RM1]].", "RM1")
	nodeRef := k.citing(t, ctx, ridA, sidA, "Step [[RM1:N1]].", "RM1:N1")

	// B gets the first roadmap in the database. A's references must not move.
	if _, err := k.roadmap.Create(ctx, CreateRoadmapRequest{
		ResearchID: ridB, Title: "B's plan",
		Nodes: []CreateRoadmapNodeRequest{{Title: "B's first step"}},
	}); err != nil {
		t.Fatalf("create roadmap in B: %v", err)
	}
	if k.resolvedRef(t, ctx, ridA, roadmapRef, "RM1") {
		t.Fatal("[[RM1]] in research A resolved against research B's roadmap")
	}
	if k.resolvedRef(t, ctx, ridA, nodeRef, "RM1:N1") {
		t.Fatal("[[RM1:N1]] in research A resolved against research B's node")
	}

	// A's own roadmap resolves them, and the row must point at A's ids.
	rmA, err := k.roadmap.Create(ctx, CreateRoadmapRequest{
		ResearchID: ridA, Title: "A's plan",
		Nodes: []CreateRoadmapNodeRequest{{Title: "A's first step"}},
	})
	if err != nil {
		t.Fatalf("create roadmap in A: %v", err)
	}
	if !k.resolvedRef(t, ctx, ridA, roadmapRef, "RM1") {
		t.Fatal("[[RM1]] did not resolve against its own research's roadmap")
	}
	_, _, gotRoadmap, _ := k.refRow(t, ctx, ridA, roadmapRef, "RM1")
	if gotRoadmap != rmA.ID {
		t.Errorf("[[RM1]] points at roadmap %q, want A's own %q", gotRoadmap, rmA.ID)
	}
	_, gotResearch, _, gotNode := k.refRow(t, ctx, ridA, nodeRef, "RM1:N1")
	if gotResearch != ridA {
		t.Errorf("[[RM1:N1]] target_research_id = %q, want A %q", gotResearch, ridA)
	}
	if gotNode == "" {
		t.Error("[[RM1:N1]] resolved without a node id")
	}
}

// The write path is only half of it: a rebuild re-runs the resolver, so a
// resolver that looks a roadmap code up globally would undo the scoping above
// the first time anybody pressed Rebuild.
func TestForwardRefs_RebuildKeepsRoadmapReferencesInTheirOwnResearch(t *testing.T) {
	ctx := context.Background()
	k := newForwardKit(t)
	ridA, sidA := k.research1(t, ctx, "A")
	ridB, _ := k.research1(t, ctx, "B")

	// B's roadmap exists first, so an unscoped lookup finds it.
	if _, err := k.roadmap.Create(ctx, CreateRoadmapRequest{
		ResearchID: ridB, Title: "B's plan",
		Nodes: []CreateRoadmapNodeRequest{{Title: "step"}},
	}); err != nil {
		t.Fatalf("create roadmap in B: %v", err)
	}
	source := k.citing(t, ctx, ridA, sidA, "Plan is [[RM1]].", "RM1")

	if _, err := k.entry.RebuildCrossRefs(ctx, ridA); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if k.resolvedRef(t, ctx, ridA, source, "RM1") {
		_, _, gotRoadmap, _ := k.refRow(t, ctx, ridA, source, "RM1")
		t.Fatalf("a rebuild pointed A's [[RM1]] at a roadmap outside A: %q", gotRoadmap)
	}
}

// A code is reused after a delete. Re-pointing a reference somebody has already
// followed is worse than leaving one dangling, so only unresolved rows move.
func TestForwardRefs_AnAlreadyResolvedReferenceIsNeverRepointed(t *testing.T) {
	ctx := context.Background()
	k := newForwardKit(t)
	rid, sid := k.research1(t, ctx, "Stable")

	target, err := k.entry.Create(ctx, CreateEntryRequest{
		ResearchID: rid, SectionID: sid, Content: "The original",
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	source, err := k.entry.Create(ctx, CreateEntryRequest{
		ResearchID: rid, SectionID: sid, Content: "Rests on [[" + target.Code + "]].",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if !k.resolvedRef(t, ctx, rid, source.ID, target.Code) {
		t.Fatal("the reference did not resolve on the ordinary backward path")
	}

	// Force the repair to run again with a different id behind the same code.
	k.entry.ResolveDanglingEntry(ctx, rid, target.Code, "some-other-entry-id")

	gotEntry, _, _, _ := k.refRow(t, ctx, rid, source.ID, target.Code)
	if gotEntry != target.ID {
		t.Errorf("a resolved reference was re-pointed: target_entry_id = %q, want %q", gotEntry, target.ID)
	}
}

// The rebuild is now a last resort rather than the only cure, and it must stay
// a no-op over a table the create path already repaired.
func TestForwardRefs_RebuildAfterTheFixChangesNothing(t *testing.T) {
	ctx := context.Background()
	k := newForwardKit(t)
	rid, sid := k.research1(t, ctx, "Idempotent")

	source := k.citing(t, ctx, rid, sid, "This rests on [[E2]].", "E2")
	if _, err := k.entry.Create(ctx, CreateEntryRequest{
		ResearchID: rid, SectionID: sid, Content: "The target",
	}); err != nil {
		t.Fatalf("create target: %v", err)
	}
	before, _, _, _ := k.refRow(t, ctx, rid, source, "E2")
	if before == "" {
		t.Fatal("the reference did not resolve on create")
	}

	if _, err := k.entry.RebuildCrossRefs(ctx, rid); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after, _, _, _ := k.refRow(t, ctx, rid, source, "E2")
	if after != before {
		t.Errorf("the rebuild moved a reference the create path had already resolved: %q then %q", before, after)
	}
}
