package service

import (
	"context"
	"log/slog"
	"testing"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
)

func TestCrossRefParsing_SameResearch(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	notifier := &mockNotifier{}

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)

	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, slog.Default())
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, slog.Default())

	r, sections, _ := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Test",
		Sections: []CreateSectionRequest{
			{Name: "s1", DisplayName: "S1"},
		},
	})

	// Create E1
	e1, err := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: r.ID, SectionID: sections[0].ID,
		Content: "# First entry",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e1.Code != "E1" {
		t.Errorf("e1.Code: got %q, want E1", e1.Code)
	}

	// Create E2 referencing [[E1]]
	e2, err := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: r.ID, SectionID: sections[0].ID,
		Content: "See [[E1]] for details",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e2.Code != "E2" {
		t.Errorf("e2.Code: got %q, want E2", e2.Code)
	}

	// Check crossrefs
	refs, err := crossrefRepo.FindByResearch(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}

	// E2 -> E1 should be resolved
	var found bool
	for _, ref := range refs {
		if ref.SourceID == e2.ID && ref.TargetRef == "E1" && ref.Resolved {
			found = true
		}
	}
	if !found {
		t.Error("expected resolved crossref from E2 to E1")
	}
}

func TestCrossRefParsing_ForwardReference(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	notifier := &mockNotifier{}

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)

	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, slog.Default())
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, slog.Default())

	r, sections, _ := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Test", Sections: []CreateSectionRequest{{Name: "s1"}},
	})

	// Create E1 referencing [[E2]] (not yet created)
	e1, _ := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: r.ID, SectionID: sections[0].ID,
		Content: "See [[E2]] for more",
	})

	refs, _ := crossrefRepo.FindByResearch(ctx, r.ID)
	for _, ref := range refs {
		if ref.SourceID == e1.ID && ref.TargetRef == "E2" && ref.Resolved {
			t.Error("forward reference should NOT be resolved before target exists")
		}
	}

	// Creating E2 resolves it, with no rebuild — that is the whole of P0-4, and
	// crossref_forward_test.go is where it is covered properly. This test keeps
	// the rebuild honest: it must still work, and must not undo the repair.
	entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: r.ID, SectionID: sections[0].ID,
		Content: "# Second entry",
	})

	refs, _ = crossrefRepo.FindByResearch(ctx, r.ID)
	for _, ref := range refs {
		if ref.SourceID == e1.ID && ref.TargetRef == "E2" && !ref.Resolved {
			t.Error("creating the target did not resolve the forward reference")
		}
	}

	report, err := entrySvc.RebuildCrossRefs(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Sources != 2 {
		t.Errorf("RebuildCrossRefs sources: got %d, want 2", report.Sources)
	}
	// The number the API reports is references, not documents — it used to be
	// documents under the name "rebuilt", and the spec called it references.
	if report.References != 1 {
		t.Errorf("RebuildCrossRefs references: got %d, want 1", report.References)
	}
	if report.Unresolved != 0 {
		t.Errorf("RebuildCrossRefs unresolved: got %d, want 0", report.Unresolved)
	}

	refs, _ = crossrefRepo.FindByResearch(ctx, r.ID)
	var resolved bool
	for _, ref := range refs {
		if ref.SourceID == e1.ID && ref.TargetRef == "E2" && ref.Resolved {
			resolved = true
		}
	}
	if !resolved {
		t.Error("after rebuild, forward reference should still be resolved")
	}
}

func TestCrossRefParsing_QuestionAnswer(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	notifier := &mockNotifier{}

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	questionRepo := storage.NewQuestionRepository(db)

	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, slog.Default())
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, slog.Default())
	sessionSvc := NewSessionService(db, sessionRepo, questionRepo, researchRepo, testAccess(db), entrySvc, notifier, slog.Default())

	r, sections, _ := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Test", Sections: []CreateSectionRequest{{Name: "s1"}},
	})

	// Create entry
	e1, _ := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: r.ID, SectionID: sections[0].ID,
		Content: "# Entry one",
	})

	// Create session with question
	_, questions, _ := sessionSvc.Create(ctx, CreateSessionRequest{
		ResearchID: r.ID, Title: "Test Session",
		Questions: []CreateQuestionRequest{
			{Text: "How does this work?", Priority: domain.PriorityHigh},
		},
	})

	// Answer question with [[E1]]
	q, err := sessionSvc.UpdateQuestion(ctx, questions[0].ID, ptr(domain.QuestionAnswered), ptr("Related to [[E1]]"))
	if err != nil {
		t.Fatal(err)
	}

	// Check crossrefs
	refs, _ := crossrefRepo.FindByResearch(ctx, r.ID)
	var found bool
	for _, ref := range refs {
		if ref.SourceType == "question" && ref.SourceID == q.ID && ref.TargetEntryID == e1.ID && ref.Resolved {
			found = true
		}
	}
	if !found {
		t.Error("expected resolved crossref from question to E1")
	}
}

func TestCrossRefParsing_TaskResult(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	notifier := &mockNotifier{}

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	taskRepo := storage.NewTaskRepository(db)

	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, slog.Default())
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, slog.Default())
	taskSvc := NewTaskService(taskRepo, researchRepo, testAccess(db), entrySvc, notifier, slog.Default())

	r, sections, _ := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Test", Sections: []CreateSectionRequest{{Name: "s1"}},
	})

	e1, _ := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: r.ID, SectionID: sections[0].ID,
		Content: "# Entry one",
	})

	task, _ := taskSvc.Create(ctx, CreateTaskRequest{
		ResearchID: r.ID, Title: "Do something",
	})

	// Update task with result referencing [[E1]]
	task, err := taskSvc.Update(ctx, task.ID, UpdateTaskRequest{
		Status: ptr(domain.TaskCompleted),
		Result: ptr("Done, see [[E1]]"),
	})
	if err != nil {
		t.Fatal(err)
	}

	refs, _ := crossrefRepo.FindByResearch(ctx, r.ID)
	var found bool
	for _, ref := range refs {
		if ref.SourceType == "task" && ref.SourceID == task.ID && ref.TargetEntryID == e1.ID && ref.Resolved {
			found = true
		}
	}
	if !found {
		t.Error("expected resolved crossref from task to E1")
	}
}

func TestResearchService_ResolveID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	notifier := &mockNotifier{}

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	svc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, slog.Default())

	r, _, _ := svc.Create(ctx, CreateResearchRequest{Name: "Test"})

	// Resolve by UUID
	id, err := svc.ResolveID(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if id != r.ID {
		t.Errorf("ResolveID by UUID: got %s, want %s", id, r.ID)
	}

	// Resolve by code
	id, err = svc.ResolveID(ctx, r.Code)
	if err != nil {
		t.Fatal(err)
	}
	if id != r.ID {
		t.Errorf("ResolveID by code: got %s, want %s", id, r.ID)
	}

	// Not found
	_, err = svc.ResolveID(ctx, "R999")
	if err == nil {
		t.Error("ResolveID should fail for unknown code")
	}
}

func TestEntryService_GetByIDOrCode(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	notifier := &mockNotifier{}

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)

	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, slog.Default())
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, slog.Default())

	r, sections, _ := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Test", Sections: []CreateSectionRequest{{Name: "s1"}},
	})

	e, _ := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: r.ID, SectionID: sections[0].ID, Content: "test",
	})

	// By UUID
	found, err := entrySvc.GetByIDOrCode(ctx, r.ID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != e.ID {
		t.Error("GetByIDOrCode by UUID failed")
	}

	// By code
	found, err = entrySvc.GetByIDOrCode(ctx, r.ID, e.Code)
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != e.ID {
		t.Error("GetByIDOrCode by code failed")
	}
}
