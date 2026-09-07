package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
)

func setupExportService(t *testing.T) (*ExportService, *ResearchService, *SectionService, *EntryService, *SessionService, *TaskService, *RoadmapService) {
	t.Helper()
	db := setupTestDB(t)
	events := &mockNotifier{}

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	questionRepo := storage.NewQuestionRepository(db)
	taskRepo := storage.NewTaskRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	externalLinkRepo := storage.NewExternalLinkRepository(db)
	roadmapRepo := storage.NewRoadmapRepository(db)
	roadmapNodeRepo := storage.NewRoadmapNodeRepository(db)
	roadmapEdgeRepo := storage.NewRoadmapEdgeRepository(db)

	log := slog.Default()

	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), events, log)
	sectionSvc := NewSectionService(sectionRepo, entryRepo, researchRepo, testAccess(db), events, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), sessionRepo, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, externalLinkRepo, events, log)
	entrySvc.SetRoadmapRepos(roadmapRepo, roadmapNodeRepo)
	sessionSvc := NewSessionService(db, sessionRepo, questionRepo, researchRepo, testAccess(db), entrySvc, events, log)
	taskSvc := NewTaskService(taskRepo, researchRepo, testAccess(db), entrySvc, events, log)
	roadmapSvc := NewRoadmapService(roadmapRepo, roadmapNodeRepo, roadmapEdgeRepo, researchRepo, testAccess(db), events, log)

	exportSvc := NewExportService(researchSvc, sectionSvc, entrySvc, entryRepo, sessionSvc, taskSvc, roadmapSvc, log)

	return exportSvc, researchSvc, sectionSvc, entrySvc, sessionSvc, taskSvc, roadmapSvc
}

// --- Export tests ---

func TestExport_MinimalResearch(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, _, _ := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Minimal",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if data.Version != 2 {
		t.Errorf("version = %d, want 2", data.Version)
	}
	if data.ExportedAt.IsZero() {
		t.Error("exported_at should not be zero")
	}
	if data.Research.Name != "Minimal" {
		t.Errorf("name = %q", data.Research.Name)
	}
	if data.Research.Status != domain.ResearchActive {
		t.Errorf("status = %q", data.Research.Status)
	}
	if len(data.Research.Sections) != 0 {
		t.Errorf("sections = %d, want 0", len(data.Research.Sections))
	}
	if len(data.Research.Sessions) != 0 {
		t.Errorf("sessions = %d, want 0", len(data.Research.Sessions))
	}
	if len(data.Research.Tasks) != 0 {
		t.Errorf("tasks = %d, want 0", len(data.Research.Tasks))
	}
	if len(data.Research.Roadmaps) != 0 {
		t.Errorf("roadmaps = %d, want 0", len(data.Research.Roadmaps))
	}
}

func TestExport_NonExistentResearch(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, _, _, _, _ := setupExportService(t)

	_, err := exportSvc.Export(ctx, "non-existent-id")
	if err == nil {
		t.Fatal("expected error for non-existent research")
	}
}

func TestExport_ByShortCode(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, _, _ := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Code Lookup",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	data, err := exportSvc.Export(ctx, research.Code)
	if err != nil {
		t.Fatalf("export by code: %v", err)
	}
	if data.Research.Name != "Code Lookup" {
		t.Errorf("name = %q", data.Research.Name)
	}
}

func TestExport_SectionsWithoutEntries(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, _, _ := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Sections Only",
		Sections: []CreateSectionRequest{
			{Name: "sec-a", DisplayName: "Section A", Description: "First", Position: 0},
			{Name: "sec-b", DisplayName: "Section B", Description: "Second", Position: 1},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if len(data.Research.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(data.Research.Sections))
	}
	sec := data.Research.Sections[0]
	if sec.Name != "sec-a" {
		t.Errorf("section name = %q", sec.Name)
	}
	if sec.DisplayName != "Section A" {
		t.Errorf("display name = %q", sec.DisplayName)
	}
	if sec.Description != "First" {
		t.Errorf("description = %q", sec.Description)
	}
	if len(sec.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(sec.Entries))
	}
}

func TestExport_EntryFields(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, entrySvc, _, _, _ := setupExportService(t)

	research, sections, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Entry Fields",
		Sections: []CreateSectionRequest{
			{Name: "main", Position: 0},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID:  research.ID,
		SectionID:   sections[0].ID,
		Title:       "My Title",
		Content:     "# Heading\n\nBody text here.",
		Description: "Custom description",
		Status:      domain.EntryActive,
		Tags:        []string{"tag1", "tag2"},
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	e := data.Research.Sections[0].Entries[0]
	if e.Title != "My Title" {
		t.Errorf("title = %q", e.Title)
	}
	if e.Content != "# Heading\n\nBody text here." {
		t.Errorf("content = %q", e.Content)
	}
	if e.Description != "Custom description" {
		t.Errorf("description = %q", e.Description)
	}
	if e.Status != domain.EntryActive {
		t.Errorf("status = %q", e.Status)
	}
	if len(e.Tags) != 2 || e.Tags[0] != "tag1" || e.Tags[1] != "tag2" {
		t.Errorf("tags = %v", e.Tags)
	}
	if e.CreatedAt.IsZero() {
		t.Error("created_at should not be zero")
	}
}

func TestExport_EntrySessionCodeLinking(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, entrySvc, sessionSvc, _, _ := setupExportService(t)

	research, sections, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name:     "Session Link",
		Sections: []CreateSectionRequest{{Name: "main", Position: 0}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Create session (becomes active)
	session, _, err := sessionSvc.Create(ctx, CreateSessionRequest{
		ResearchID: research.ID,
		Title:      "Active Session",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Create entry — should auto-link to active session
	_, err = entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[0].ID,
		Content:    "Linked to session",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	e := data.Research.Sections[0].Entries[0]
	if e.SessionCode != session.Code {
		t.Errorf("session_code = %q, want %q", e.SessionCode, session.Code)
	}
}

func TestExport_SessionFields(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, sessionSvc, _, _ := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{Name: "Sessions"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	session, _, err := sessionSvc.Create(ctx, CreateSessionRequest{
		ResearchID: research.ID,
		Title:      "Deep Dive",
		Focus:      "Architecture",
		Questions: []CreateQuestionRequest{
			{Text: "Q1", Area: "design", Rationale: "important", Priority: domain.PriorityHigh, Position: 0},
		},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Add notes and complete
	notes := "Some session notes"
	completed := domain.SessionCompleted
	sessionSvc.Update(ctx, session.ID, UpdateSessionRequest{
		Notes:  &notes,
		Status: &completed,
	})

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if len(data.Research.Sessions) != 1 {
		t.Fatalf("sessions = %d", len(data.Research.Sessions))
	}
	s := data.Research.Sessions[0]
	if s.Title != "Deep Dive" {
		t.Errorf("title = %q", s.Title)
	}
	if s.Focus != "Architecture" {
		t.Errorf("focus = %q", s.Focus)
	}
	if s.Status != domain.SessionCompleted {
		t.Errorf("status = %q", s.Status)
	}
	if s.Notes != "Some session notes" {
		t.Errorf("notes = %q", s.Notes)
	}
	if s.Code == "" {
		t.Error("code should not be empty")
	}
	if len(s.Questions) != 1 {
		t.Fatalf("questions = %d", len(s.Questions))
	}

	q := s.Questions[0]
	if q.Text != "Q1" {
		t.Errorf("question text = %q", q.Text)
	}
	if q.Area != "design" {
		t.Errorf("area = %q", q.Area)
	}
	if q.Rationale != "important" {
		t.Errorf("rationale = %q", q.Rationale)
	}
	if q.Priority != domain.PriorityHigh {
		t.Errorf("priority = %q", q.Priority)
	}
}

func TestExport_QuestionHierarchy(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, sessionSvc, _, _ := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{Name: "Hierarchy"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	session, _, err := sessionSvc.Create(ctx, CreateSessionRequest{
		ResearchID: research.ID,
		Title:      "Session",
		Questions: []CreateQuestionRequest{
			{Text: "Root question", Priority: domain.PriorityHigh, Position: 0},
		},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Get root question ID
	swq, _ := sessionSvc.Get(ctx, session.ID)
	rootID := swq.Questions[0].ID

	// Add child questions
	sessionSvc.AddQuestions(ctx, session.ID, []CreateQuestionRequest{
		{Text: "Child 1", ParentID: rootID, Priority: domain.PriorityMedium, Position: 0},
		{Text: "Child 2", ParentID: rootID, Priority: domain.PriorityLow, Position: 1},
	})

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	q := data.Research.Sessions[0].Questions
	if len(q) != 1 {
		t.Fatalf("root questions = %d, want 1", len(q))
	}
	if q[0].Text != "Root question" {
		t.Errorf("root text = %q", q[0].Text)
	}
	if len(q[0].Children) != 2 {
		t.Fatalf("children = %d, want 2", len(q[0].Children))
	}
	if q[0].Children[0].Text != "Child 1" {
		t.Errorf("child[0] text = %q", q[0].Children[0].Text)
	}
	if q[0].Children[1].Text != "Child 2" {
		t.Errorf("child[1] text = %q", q[0].Children[1].Text)
	}
}

func TestExport_QuestionAnswers(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, sessionSvc, _, _ := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{Name: "Answers"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	session, _, err := sessionSvc.Create(ctx, CreateSessionRequest{
		ResearchID: research.ID,
		Title:      "Session",
		Questions: []CreateQuestionRequest{
			{Text: "Answered Q", Priority: domain.PriorityHigh, Position: 0},
			{Text: "Deferred Q", Priority: domain.PriorityLow, Position: 1},
		},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	swq, _ := sessionSvc.Get(ctx, session.ID)

	answered := domain.QuestionAnswered
	answer := "This is the answer"
	sessionSvc.UpdateQuestion(ctx, swq.Questions[0].ID, &answered, &answer)

	deferred := domain.QuestionDeferred
	sessionSvc.UpdateQuestion(ctx, swq.Questions[1].ID, &deferred, nil)

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	qs := data.Research.Sessions[0].Questions
	if qs[0].Status != domain.QuestionAnswered {
		t.Errorf("q[0] status = %q", qs[0].Status)
	}
	if qs[0].Answer != "This is the answer" {
		t.Errorf("q[0] answer = %q", qs[0].Answer)
	}
	if qs[1].Status != domain.QuestionDeferred {
		t.Errorf("q[1] status = %q", qs[1].Status)
	}
}

func TestExport_MultipleSessions(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, sessionSvc, _, _ := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{Name: "Multi Sessions"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	sessionSvc.Create(ctx, CreateSessionRequest{
		ResearchID: research.ID,
		Title:      "Session 1",
		Questions:  []CreateQuestionRequest{{Text: "Q1", Position: 0}},
	})
	sessionSvc.Create(ctx, CreateSessionRequest{
		ResearchID: research.ID,
		Title:      "Session 2",
		Questions:  []CreateQuestionRequest{{Text: "Q2", Position: 0}},
	})

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if len(data.Research.Sessions) != 2 {
		t.Errorf("sessions = %d, want 2", len(data.Research.Sessions))
	}
}

func TestExport_TaskFields(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, taskSvc, _ := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{Name: "Tasks"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	task, _ := taskSvc.Create(ctx, CreateTaskRequest{
		ResearchID:  research.ID,
		Title:       "Important task",
		Description: "Do something important",
		Priority:    domain.PriorityHigh,
	})
	taskSvc.Update(ctx, task.ID, UpdateTaskRequest{
		Status: ptr(domain.TaskCompleted),
		Result: ptr("Done successfully"),
	})

	// Add a pending task too
	taskSvc.Create(ctx, CreateTaskRequest{
		ResearchID: research.ID,
		Title:      "Pending task",
		Priority:   domain.PriorityLow,
	})

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if len(data.Research.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(data.Research.Tasks))
	}

	// Find completed task
	var completed *domain.ExportTask
	for i := range data.Research.Tasks {
		if data.Research.Tasks[i].Status == domain.TaskCompleted {
			completed = &data.Research.Tasks[i]
		}
	}
	if completed == nil {
		t.Fatal("no completed task found")
	}
	if completed.Title != "Important task" {
		t.Errorf("title = %q", completed.Title)
	}
	if completed.Description != "Do something important" {
		t.Errorf("description = %q", completed.Description)
	}
	if completed.Priority != domain.PriorityHigh {
		t.Errorf("priority = %q", completed.Priority)
	}
	if completed.Result != "Done successfully" {
		t.Errorf("result = %q", completed.Result)
	}
	if completed.CompletedAt == nil {
		t.Error("completed_at should not be nil")
	}
}

func TestExport_RoadmapFields(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, _, roadmapSvc := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{Name: "Roadmaps"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	roadmapSvc.Create(ctx, CreateRoadmapRequest{
		ResearchID:  research.ID,
		Title:       "My Roadmap",
		Description: "Description",
		Statuses:    []string{"todo", "doing", "done"},
		Nodes: []CreateRoadmapNodeRequest{
			{TempID: "a", Title: "Node A", Description: "First", NodeType: "milestone", Status: "done", PositionX: 10, PositionY: 20},
			{TempID: "b", Title: "Node B", NodeType: "step", Status: "todo", PositionX: 100, PositionY: 200},
		},
		Edges: []CreateRoadmapEdgeRequest{
			{SourceNodeRef: "a", TargetNodeRef: "b", Label: "depends on", EdgeType: "dependency"},
		},
	})

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if len(data.Research.Roadmaps) != 1 {
		t.Fatalf("roadmaps = %d", len(data.Research.Roadmaps))
	}
	rm := data.Research.Roadmaps[0]
	if rm.Title != "My Roadmap" {
		t.Errorf("title = %q", rm.Title)
	}
	if rm.Description != "Description" {
		t.Errorf("description = %q", rm.Description)
	}
	if len(rm.Statuses) != 3 {
		t.Errorf("statuses = %v", rm.Statuses)
	}
	if rm.Status != domain.RoadmapActive {
		t.Errorf("status = %q", rm.Status)
	}

	if len(rm.Nodes) != 2 {
		t.Fatalf("nodes = %d", len(rm.Nodes))
	}
	n := rm.Nodes[0]
	if n.Title != "Node A" {
		t.Errorf("node title = %q", n.Title)
	}
	if n.Description != "First" {
		t.Errorf("node desc = %q", n.Description)
	}
	if n.NodeType != "milestone" {
		t.Errorf("node_type = %q", n.NodeType)
	}
	if n.Status != "done" {
		t.Errorf("node status = %q", n.Status)
	}
	if n.PositionX != 10 || n.PositionY != 20 {
		t.Errorf("position = (%f, %f)", n.PositionX, n.PositionY)
	}
	if n.Code == "" {
		t.Error("node code should not be empty")
	}

	if len(rm.Edges) != 1 {
		t.Fatalf("edges = %d", len(rm.Edges))
	}
	edge := rm.Edges[0]
	if edge.Label != "depends on" {
		t.Errorf("edge label = %q", edge.Label)
	}
	if edge.EdgeType != "dependency" {
		t.Errorf("edge_type = %q", edge.EdgeType)
	}
	if edge.SourceCode == "" || edge.TargetCode == "" {
		t.Error("edge codes should not be empty")
	}
}

func TestExport_ResearchInstructionAndMemory(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, _, _ := setupExportService(t)

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "With Instruction",
		Tags: []string{"ai", "research"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := researchSvc.researches.ImportProcess(ctx, research.ID, "Be concise and technical", domain.Memory{{Text: "User prefers Go", Author: "unknown"}, {Text: "Focus on backend", Author: "unknown"}}, nil); err != nil {
		t.Fatal(err)
	}

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if data.Research.Instruction != "" || len(data.Research.PrivateSkills) != 1 || data.Research.PrivateSkills[0].Body != "Be concise and technical" {
		t.Errorf("private skills = %+v", data.Research.PrivateSkills)
	}
	if len(data.Research.Memory) != 2 {
		t.Fatalf("memory = %d", len(data.Research.Memory))
	}
	if data.Research.Memory[0].Text != "User prefers Go" {
		t.Errorf("memory[0] = %q", data.Research.Memory[0])
	}
	if len(data.Research.Tags) != 2 || data.Research.Tags[0] != "ai" {
		t.Errorf("tags = %v", data.Research.Tags)
	}
}

// --- Import tests ---

func TestImport_InvalidVersion(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, _, _, _, _ := setupExportService(t)

	_, _, err := exportSvc.Import(ctx, &domain.ExportData{Version: 99}, "")
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestImport_MinimalResearch(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, _, _, _, _ := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:        "Imported Minimal",
			Description: "Minimal import",
			Goal:        "Testing",
			Status:      domain.ResearchActive,
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if research.Name != "Imported Minimal" {
		t.Errorf("name = %q", research.Name)
	}
	if research.ID == "" {
		t.Error("should have generated ID")
	}
	if research.Code == "" {
		t.Error("should have generated code")
	}
}

func TestImport_WithSections(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, sectionSvc, _, _, _, _ := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "With Sections",
			Status: domain.ResearchActive,
			Sections: []domain.ExportSection{
				{Name: "intro", DisplayName: "Introduction", Description: "Intro desc", Status: domain.SectionDraft, Position: 0},
				{Name: "body", DisplayName: "Body", Description: "Body desc", Status: domain.SectionDraft, Position: 1},
			},
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	sections, err := sectionSvc.List(ctx, research.ID)
	if err != nil {
		t.Fatalf("list sections: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(sections))
	}
	if sections[0].Name != "intro" {
		t.Errorf("section[0] name = %q", sections[0].Name)
	}
	if sections[0].DisplayName != "Introduction" {
		t.Errorf("section[0] display_name = %q", sections[0].DisplayName)
	}
	if sections[1].Name != "body" {
		t.Errorf("section[1] name = %q", sections[1].Name)
	}
}

func TestImport_WithEntries(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, entrySvc, _, _, _ := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "With Entries",
			Status: domain.ResearchActive,
			Sections: []domain.ExportSection{
				{
					Name: "main", Position: 0, Status: domain.SectionDraft,
					Entries: []domain.ExportEntry{
						{Title: "Entry 1", Content: "Content 1", Status: domain.EntryActive, Tags: []string{"a", "b"}},
						{Title: "Entry 2", Content: "Content 2", Status: domain.EntryDraft},
					},
				},
			},
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	entries, err := entrySvc.ListByResearch(ctx, research.ID, storage.EntryFilter{})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	// Verify entry details (entries are returned without content)
	e1, _ := entrySvc.Get(ctx, entries[0].ID)
	if e1.Content == "" {
		t.Error("entry content should not be empty")
	}
}

func TestImport_WithEntrySessionLinking(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, entrySvc, sessionSvc, _, _ := setupExportService(t)

	// Use a completed session so it doesn't auto-link to unlinked entries
	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "Entry-Session Link",
			Status: domain.ResearchActive,
			Sections: []domain.ExportSection{
				{
					Name: "main", Position: 0, Status: domain.SectionDraft,
					Entries: []domain.ExportEntry{
						{Title: "Linked", Content: "Content", Status: domain.EntryDraft, SessionCode: "SS1"},
						{Title: "Unlinked", Content: "Content 2", Status: domain.EntryDraft},
					},
				},
			},
			Sessions: []domain.ExportSession{
				{Code: "SS1", Title: "My Session", Status: domain.SessionCompleted},
			},
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	sessions, _ := sessionSvc.ListByResearch(ctx, research.ID)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d", len(sessions))
	}
	sessionID := sessions[0].ID

	entries, _ := entrySvc.ListByResearch(ctx, research.ID, storage.EntryFilter{})
	if len(entries) != 2 {
		t.Fatalf("entries = %d", len(entries))
	}

	// Only the explicitly linked entry should have a session ID
	linked := 0
	for _, e := range entries {
		full, _ := entrySvc.Get(ctx, e.ID)
		if full.SessionID == sessionID {
			linked++
		}
	}
	if linked != 1 {
		t.Errorf("explicitly linked entries = %d, want 1", linked)
	}
}

func TestImport_WithSessions(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, _, sessionSvc, _, _ := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "With Sessions",
			Status: domain.ResearchActive,
			Sessions: []domain.ExportSession{
				{
					Code:   "SS1",
					Title:  "Session 1",
					Focus:  "Focus 1",
					Status: domain.SessionCompleted,
					Notes:  "Session notes here",
					Questions: []domain.ExportQuestion{
						{Text: "Q1", Priority: domain.PriorityHigh, Status: domain.QuestionAnswered, Answer: "A1", Position: 0},
						{Text: "Q2", Priority: domain.PriorityMedium, Status: domain.QuestionPending, Position: 1},
					},
				},
			},
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	sessions, _ := sessionSvc.ListByResearch(ctx, research.ID)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d", len(sessions))
	}

	swq, _ := sessionSvc.Get(ctx, sessions[0].ID)
	if swq.Session.Title != "Session 1" {
		t.Errorf("title = %q", swq.Session.Title)
	}
	if swq.Session.Focus != "Focus 1" {
		t.Errorf("focus = %q", swq.Session.Focus)
	}
	if swq.Session.Status != domain.SessionCompleted {
		t.Errorf("status = %q", swq.Session.Status)
	}
	if swq.Session.Notes != "Session notes here" {
		t.Errorf("notes = %q", swq.Session.Notes)
	}
	if len(swq.Questions) != 2 {
		t.Fatalf("questions = %d", len(swq.Questions))
	}

	// Find answered question
	for _, q := range swq.Questions {
		if q.Text == "Q1" {
			if q.Status != domain.QuestionAnswered {
				t.Errorf("Q1 status = %q", q.Status)
			}
			if q.Answer != "A1" {
				t.Errorf("Q1 answer = %q", q.Answer)
			}
		}
	}
}

func TestImport_WithQuestionHierarchy(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, _, sessionSvc, _, _ := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "Question Hierarchy",
			Status: domain.ResearchActive,
			Sessions: []domain.ExportSession{
				{
					Code:   "SS1",
					Title:  "Session",
					Status: domain.SessionActive,
					Questions: []domain.ExportQuestion{
						{
							Text:     "Root Q",
							Priority: domain.PriorityHigh,
							Status:   domain.QuestionAnswered,
							Answer:   "Root answer",
							Position: 0,
							Children: []domain.ExportQuestion{
								{Text: "Child Q1", Priority: domain.PriorityMedium, Status: domain.QuestionPending, Position: 0},
								{Text: "Child Q2", Priority: domain.PriorityLow, Status: domain.QuestionAnswered, Answer: "Child answer", Position: 1},
							},
						},
					},
				},
			},
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	sessions, _ := sessionSvc.ListByResearch(ctx, research.ID)
	swq, _ := sessionSvc.Get(ctx, sessions[0].ID)

	if len(swq.Questions) != 3 {
		t.Fatalf("total questions = %d, want 3 (1 root + 2 children)", len(swq.Questions))
	}

	// Find root and children
	var root *domain.Question
	children := 0
	for _, q := range swq.Questions {
		if q.ParentID == "" {
			root = q
		} else {
			children++
		}
	}
	if root == nil {
		t.Fatal("root question not found")
	}
	if root.Text != "Root Q" {
		t.Errorf("root text = %q", root.Text)
	}
	if root.Answer != "Root answer" {
		t.Errorf("root answer = %q", root.Answer)
	}
	if children != 2 {
		t.Errorf("children = %d, want 2", children)
	}

	// Verify child answer was preserved
	for _, q := range swq.Questions {
		if q.Text == "Child Q2" {
			if q.Answer != "Child answer" {
				t.Errorf("child answer = %q", q.Answer)
			}
			if q.Status != domain.QuestionAnswered {
				t.Errorf("child status = %q", q.Status)
			}
		}
	}
}

func TestImport_WithTasks(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, _, _, taskSvc, _ := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "With Tasks",
			Status: domain.ResearchActive,
			Tasks: []domain.ExportTask{
				{Title: "Completed task", Description: "Do it", Status: domain.TaskCompleted, Priority: domain.PriorityHigh, Result: "Done"},
				{Title: "Pending task", Status: domain.TaskPending, Priority: domain.PriorityLow},
				{Title: "Failed task", Status: domain.TaskFailed, Priority: domain.PriorityMedium, Result: "Could not complete"},
			},
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	tasks, _ := taskSvc.List(ctx, research.ID, storage.TaskFilter{})
	if len(tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(tasks))
	}

	statusCounts := make(map[domain.TaskStatus]int)
	for _, task := range tasks {
		statusCounts[task.Status]++
	}
	if statusCounts[domain.TaskCompleted] != 1 {
		t.Errorf("completed = %d", statusCounts[domain.TaskCompleted])
	}
	if statusCounts[domain.TaskPending] != 1 {
		t.Errorf("pending = %d", statusCounts[domain.TaskPending])
	}
	if statusCounts[domain.TaskFailed] != 1 {
		t.Errorf("failed = %d", statusCounts[domain.TaskFailed])
	}

	// Verify result preserved
	for _, task := range tasks {
		if task.Title == "Completed task" && task.Result != "Done" {
			t.Errorf("completed task result = %q", task.Result)
		}
		if task.Title == "Failed task" && task.Result != "Could not complete" {
			t.Errorf("failed task result = %q", task.Result)
		}
	}
}

func TestImport_WithRoadmaps(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, _, _, _, roadmapSvc := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "With Roadmaps",
			Status: domain.ResearchActive,
			Roadmaps: []domain.ExportRoadmap{
				{
					Title:       "My Roadmap",
					Description: "Path",
					Statuses:    []string{"new", "wip", "done"},
					Status:      domain.RoadmapActive,
					Nodes: []domain.ExportRoadmapNode{
						{Code: "N1", Title: "Start", NodeType: "milestone", Status: "done", PositionX: 0, PositionY: 0},
						{Code: "N2", Title: "Middle", NodeType: "step", Status: "wip", PositionX: 100, PositionY: 0},
						{Code: "N3", Title: "End", NodeType: "milestone", Status: "new", PositionX: 200, PositionY: 0},
					},
					Edges: []domain.ExportRoadmapEdge{
						{SourceCode: "N1", TargetCode: "N2", Label: "step 1", EdgeType: "default"},
						{SourceCode: "N2", TargetCode: "N3", Label: "step 2", EdgeType: "default"},
					},
				},
			},
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	roadmaps, _ := roadmapSvc.List(ctx, research.ID)
	if len(roadmaps) != 1 {
		t.Fatalf("roadmaps = %d", len(roadmaps))
	}

	rm, _ := roadmapSvc.Get(ctx, roadmaps[0].ID)
	if rm.Title != "My Roadmap" {
		t.Errorf("title = %q", rm.Title)
	}
	if len(rm.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(rm.Nodes))
	}
	if len(rm.Edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(rm.Edges))
	}

	// Verify node attributes
	for _, n := range rm.Nodes {
		if n.Title == "Start" {
			if n.NodeType != "milestone" {
				t.Errorf("Start node_type = %q", n.NodeType)
			}
			if n.Status != "done" {
				t.Errorf("Start status = %q", n.Status)
			}
		}
		if n.Title == "Middle" && n.PositionX != 100 {
			t.Errorf("Middle position_x = %f", n.PositionX)
		}
	}
}

func TestImport_WithInstructionAndMemory(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, _, _ := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:        "Instruction Test",
			Status:      domain.ResearchActive,
			Instruction: "Be helpful",
			Memory:      domain.Memory{{Text: "Remember this", Author: "unknown"}, {Text: "And this", Author: "unknown"}},
			Tags:        []string{"tag1"},
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	r, _ := researchSvc.Get(ctx, research.ID)
	skills, err := researchSvc.researches.ExportPrivateSkills(ctx, r.ID)
	if err != nil || len(skills) != 1 || skills[0].Body != "Be helpful" || !skills[0].NeedsTrigger {
		t.Fatalf("legacy instruction migration: %+v %v", skills, err)
	}
	if len(r.Memory) != 2 || r.Memory[0].Text != "Remember this" {
		t.Errorf("memory = %v", r.Memory)
	}
	if len(r.Tags) != 1 || r.Tags[0] != "tag1" {
		t.Errorf("tags = %v", r.Tags)
	}
}

func TestImport_ArchivedResearchStatus(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, _, _ := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "Archived",
			Status: domain.ResearchArchived,
		},
	}

	research, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	r, _ := researchSvc.Get(ctx, research.ID)
	if r.Status != domain.ResearchArchived {
		t.Errorf("status = %q, want archived", r.Status)
	}
}

// --- Round-trip tests ---

func TestExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, entrySvc, sessionSvc, taskSvc, roadmapSvc := setupExportService(t)

	// Create a research with sections
	research, sections, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name:        "Test Export Research",
		Description: "Testing import/export",
		Goal:        "Verify round-trip",
		Tags:        []string{"test", "export"},
		Sections: []CreateSectionRequest{
			{Name: "overview", DisplayName: "Overview", Description: "Overview section", Position: 0},
			{Name: "details", DisplayName: "Details", Description: "Details section", Position: 1},
		},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}

	if err := researchSvc.researches.ImportProcess(ctx, research.ID, "You are a research assistant", domain.Memory{{Text: "Note 1", Author: "unknown"}, {Text: "Note 2", Author: "unknown"}}, nil); err != nil {
		t.Fatal(err)
	}

	session, _, err := sessionSvc.Create(ctx, CreateSessionRequest{
		ResearchID: research.ID,
		Title:      "Initial session",
		Focus:      "Getting started",
		Questions: []CreateQuestionRequest{
			{Text: "What is the goal?", Area: "planning", Priority: domain.PriorityHigh, Position: 0},
			{Text: "How to proceed?", Area: "execution", Priority: domain.PriorityMedium, Position: 1},
		},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	swq, _ := sessionSvc.Get(ctx, session.ID)
	answered := domain.QuestionAnswered
	answer := "The goal is to test export/import with [[E1]] reference"
	sessionSvc.UpdateQuestion(ctx, swq.Questions[0].ID, &answered, &answer)

	_, err = entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[0].ID,
		Title:      "First entry",
		Content:    "# First entry\n\nContent of the first entry.",
		Status:     domain.EntryActive,
		Tags:       []string{"important"},
	})
	if err != nil {
		t.Fatalf("create entry1: %v", err)
	}

	_, err = entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[1].ID,
		Title:      "Second entry",
		Content:    "Content referencing [[E1]] first entry.",
		Status:     domain.EntryDraft,
	})
	if err != nil {
		t.Fatalf("create entry2: %v", err)
	}

	task, err := taskSvc.Create(ctx, CreateTaskRequest{
		ResearchID:  research.ID,
		Title:       "Review findings",
		Description: "Review all entries",
		Priority:    domain.PriorityHigh,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskSvc.Update(ctx, task.ID, UpdateTaskRequest{
		Status: ptr(domain.TaskCompleted),
		Result: ptr("All entries reviewed"),
	})

	_, err = roadmapSvc.Create(ctx, CreateRoadmapRequest{
		ResearchID:  research.ID,
		Title:       "Learning Path",
		Description: "Step by step",
		Statuses:    []string{"not_started", "in_progress", "done"},
		Nodes: []CreateRoadmapNodeRequest{
			{TempID: "t1", Title: "Step 1", NodeType: "step", Status: "done", PositionX: 0, PositionY: 0},
			{TempID: "t2", Title: "Step 2", NodeType: "step", Status: "not_started", PositionX: 100, PositionY: 0},
		},
		Edges: []CreateRoadmapEdgeRequest{
			{SourceNodeRef: "t1", TargetNodeRef: "t2", Label: "next", EdgeType: "default"},
		},
	})
	if err != nil {
		t.Fatalf("create roadmap: %v", err)
	}

	// Export
	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Serialize and deserialize (simulate network transfer)
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var imported domain.ExportData
	if err := json.Unmarshal(jsonBytes, &imported); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Import
	newResearch, _, err := exportSvc.Import(ctx, &imported, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if newResearch.Name != "Test Export Research" {
		t.Errorf("imported name = %q", newResearch.Name)
	}
	if newResearch.ID == research.ID {
		t.Error("imported research should have a new ID")
	}
	if newResearch.Code == research.Code {
		t.Error("imported research should have a new code")
	}

	// Re-export and compare structure
	reExported, err := exportSvc.Export(ctx, newResearch.ID)
	if err != nil {
		t.Fatalf("re-export: %v", err)
	}

	if len(reExported.Research.Sections) != 2 {
		t.Errorf("re-exported sections = %d", len(reExported.Research.Sections))
	}
	if len(reExported.Research.Sessions) != 1 {
		t.Errorf("re-exported sessions = %d", len(reExported.Research.Sessions))
	}
	if len(reExported.Research.Tasks) != 1 {
		t.Errorf("re-exported tasks = %d", len(reExported.Research.Tasks))
	}
	if len(reExported.Research.Roadmaps) != 1 {
		t.Errorf("re-exported roadmaps = %d", len(reExported.Research.Roadmaps))
	}

	totalEntries := 0
	for _, sec := range reExported.Research.Sections {
		totalEntries += len(sec.Entries)
	}
	if totalEntries != 2 {
		t.Errorf("re-exported total entries = %d, want 2", totalEntries)
	}

	if len(reExported.Research.Sessions[0].Questions) != 2 {
		t.Errorf("re-exported questions = %d, want 2", len(reExported.Research.Sessions[0].Questions))
	}

	if reExported.Research.Tasks[0].Status != domain.TaskCompleted {
		t.Errorf("re-exported task status = %q", reExported.Research.Tasks[0].Status)
	}
	if reExported.Research.Tasks[0].Result != "All entries reviewed" {
		t.Errorf("re-exported task result = %q", reExported.Research.Tasks[0].Result)
	}

	if reExported.Research.Instruction != "" || len(reExported.Research.PrivateSkills) != 1 || reExported.Research.PrivateSkills[0].Body != "You are a research assistant" {
		t.Errorf("re-exported private skills = %+v", reExported.Research.PrivateSkills)
	}
	if len(reExported.Research.Memory) != 2 {
		t.Errorf("re-exported memory = %d", len(reExported.Research.Memory))
	}

	reRM := reExported.Research.Roadmaps[0]
	if len(reRM.Nodes) != 2 {
		t.Errorf("re-exported roadmap nodes = %d", len(reRM.Nodes))
	}
	if len(reRM.Edges) != 1 {
		t.Errorf("re-exported roadmap edges = %d", len(reRM.Edges))
	}
}

func TestExportImportRoundTrip_PreservesEntryContent(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, entrySvc, _, _, _ := setupExportService(t)

	research, sections, _ := researchSvc.Create(ctx, CreateResearchRequest{
		Name:     "Content Test",
		Sections: []CreateSectionRequest{{Name: "main", Position: 0}},
	})

	content := "# Complex Content\n\n- List item 1\n- List item 2\n\n```go\nfunc main() {}\n```\n\n> Blockquote\n\n[[E1]] reference"
	entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[0].ID,
		Content:    content,
		Status:     domain.EntryActive,
	})

	data, _ := exportSvc.Export(ctx, research.ID)
	jsonBytes, _ := json.Marshal(data)
	var imported domain.ExportData
	json.Unmarshal(jsonBytes, &imported)

	newResearch, _, err := exportSvc.Import(ctx, &imported, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	reExported, _ := exportSvc.Export(ctx, newResearch.ID)
	reContent := reExported.Research.Sections[0].Entries[0].Content
	if reContent != content {
		t.Errorf("content not preserved:\ngot:  %q\nwant: %q", reContent, content)
	}
}

func TestExportImportRoundTrip_PreservesEntryType(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, entrySvc, _, _, _ := setupExportService(t)

	research, sections, _ := researchSvc.Create(ctx, CreateResearchRequest{
		Name:     "Artifact Test",
		Sections: []CreateSectionRequest{{Name: "main", Position: 0}},
	})

	if _, err := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[0].ID,
		Type:       domain.EntryArtifact,
		Content:    artifactHTML,
	}); err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	if _, err := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[0].ID,
		Title:      "Plain note",
		Content:    "# Plain\n\nbody",
	}); err != nil {
		t.Fatalf("create markdown: %v", err)
	}

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	exported := data.Research.Sections[0].Entries
	if len(exported) != 2 {
		t.Fatalf("exported entries = %d, want 2", len(exported))
	}
	// artifact is an input alias now: it is stored, exported and imported as a
	// blocks document holding one html block.
	if exported[0].Type != domain.EntryBlocks {
		t.Errorf("exported type = %q, want %q", exported[0].Type, domain.EntryBlocks)
	}
	if exported[1].Type != domain.EntryMarkdown {
		t.Errorf("exported type = %q, want %q", exported[1].Type, domain.EntryMarkdown)
	}

	jsonBytes, _ := json.Marshal(data)
	var imported domain.ExportData
	if err := json.Unmarshal(jsonBytes, &imported); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	newResearch, _, err := exportSvc.Import(ctx, &imported, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	reExported, err := exportSvc.Export(ctx, newResearch.ID)
	if err != nil {
		t.Fatalf("re-export: %v", err)
	}
	reEntries := reExported.Research.Sections[0].Entries
	if len(reEntries) != 2 {
		t.Fatalf("re-exported entries = %d, want 2", len(reEntries))
	}
	if reEntries[0].Type != domain.EntryBlocks {
		t.Errorf("artifact imported as %q, want %q", reEntries[0].Type, domain.EntryBlocks)
	}
	// The HTML itself must survive byte for byte inside the html block — that is
	// what "not mangled" means once the wrapper is the storage shape.
	doc, err := NormalizeBlockDocument(reEntries[0].Content)
	if err != nil {
		t.Fatalf("re-imported content is not a block document: %v", err)
	}
	if len(doc.Blocks) != 1 || doc.Blocks[0].Type != domain.BlockHTML {
		t.Fatalf("blocks = %v, want a single html block", doc.Blocks)
	}
	if got := str(doc.Blocks[0].Data, "html"); got != artifactHTML {
		t.Errorf("artifact HTML not preserved:\ngot:  %q\nwant: %q", got, artifactHTML)
	}
	if reEntries[1].Type != domain.EntryMarkdown {
		t.Errorf("markdown imported as %q, want %q", reEntries[1].Type, domain.EntryMarkdown)
	}
}

// An export written before artifacts existed carries no entry_type; those entries
// were markdown and must import as markdown rather than fail validation.
func TestImport_EntryWithoutTypeIsMarkdown(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, entrySvc, _, _, _ := setupExportService(t)

	research, _, err := exportSvc.Import(ctx, &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "Legacy Export",
			Status: domain.ResearchActive,
			Sections: []domain.ExportSection{{
				Name: "main", Position: 0, Status: domain.SectionDraft,
				Entries: []domain.ExportEntry{
					{Title: "Old", Content: "body", Status: domain.EntryDraft},
				},
			}},
		},
	}, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	entries, err := entrySvc.ListByResearch(ctx, research.ID, storage.EntryFilter{})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Type != domain.EntryMarkdown {
		t.Errorf("Type = %q, want %q", entries[0].Type, domain.EntryMarkdown)
	}
}

// Import is not transactional, so an entry Create would refuse must be caught
// before anything is written — otherwise the failed import leaves a half-built
// research behind.
func TestImport_RejectsBadEntriesBeforeWriting(t *testing.T) {
	cases := []struct {
		name  string
		entry domain.ExportEntry
		want  string
	}{
		{
			name:  "unknown entry_type",
			entry: domain.ExportEntry{Title: "Odd", Type: "html", Content: "body"},
			want:  "invalid entry_type",
		},
		{
			name:  "artifact without a title anywhere",
			entry: domain.ExportEntry{Type: domain.EntryArtifact, Content: "<html><body>chart</body></html>"},
			want:  "no title",
		},
		{
			name:  "entry without content",
			entry: domain.ExportEntry{Title: "Empty"},
			want:  "no content",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			exportSvc, researchSvc, _, _, _, _, _ := setupExportService(t)

			data := &domain.ExportData{
				Version: 1,
				Research: domain.ExportResearch{
					Name:   "Broken Import",
					Status: domain.ResearchActive,
					Sections: []domain.ExportSection{{
						Name: "main", Position: 0, Status: domain.SectionDraft,
						Entries: []domain.ExportEntry{
							{Title: "Fine", Content: "body", Status: domain.EntryDraft},
							tc.entry,
						},
					}},
				},
			}

			_, _, err := exportSvc.Import(ctx, data, "")
			if err == nil {
				t.Fatal("import succeeded, want a validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}

			// The rejected import must not have created the research, its sections
			// or the entries that came before the bad one.
			list, err := researchSvc.List(ctx, storage.ResearchFilter{})
			if err != nil {
				t.Fatalf("list researches: %v", err)
			}
			if len(list) != 0 {
				t.Errorf("researches = %d, want 0 — the failed import left debris", len(list))
			}
		})
	}
}

func TestExportImportRoundTrip_MultipleRoadmaps(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, _, roadmapSvc := setupExportService(t)

	research, _, _ := researchSvc.Create(ctx, CreateResearchRequest{Name: "Multi Roadmaps"})

	roadmapSvc.Create(ctx, CreateRoadmapRequest{
		ResearchID: research.ID,
		Title:      "Roadmap 1",
		Nodes:      []CreateRoadmapNodeRequest{{TempID: "a", Title: "A", NodeType: "step"}},
	})
	roadmapSvc.Create(ctx, CreateRoadmapRequest{
		ResearchID: research.ID,
		Title:      "Roadmap 2",
		Nodes:      []CreateRoadmapNodeRequest{{TempID: "b", Title: "B", NodeType: "step"}},
	})

	data, _ := exportSvc.Export(ctx, research.ID)
	if len(data.Research.Roadmaps) != 2 {
		t.Fatalf("roadmaps = %d, want 2", len(data.Research.Roadmaps))
	}

	jsonBytes, _ := json.Marshal(data)
	var imported domain.ExportData
	json.Unmarshal(jsonBytes, &imported)

	newResearch, _, err := exportSvc.Import(ctx, &imported, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	roadmaps, _ := roadmapSvc.List(ctx, newResearch.ID)
	if len(roadmaps) != 2 {
		t.Errorf("imported roadmaps = %d, want 2", len(roadmaps))
	}
}

func TestExportImportRoundTrip_TaskStatuses(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, _, _, taskSvc, _ := setupExportService(t)

	research, _, _ := researchSvc.Create(ctx, CreateResearchRequest{Name: "Task Statuses"})

	statuses := []domain.TaskStatus{
		domain.TaskPending,
		domain.TaskInProgress,
		domain.TaskCompleted,
		domain.TaskFailed,
		domain.TaskDeferred,
		domain.TaskBlocked,
	}

	for _, s := range statuses {
		task, _ := taskSvc.Create(ctx, CreateTaskRequest{
			ResearchID: research.ID,
			Title:      "Task " + string(s),
		})
		if s != domain.TaskPending {
			taskSvc.Update(ctx, task.ID, UpdateTaskRequest{Status: &s})
		}
	}

	data, _ := exportSvc.Export(ctx, research.ID)
	jsonBytes, _ := json.Marshal(data)
	var imported domain.ExportData
	json.Unmarshal(jsonBytes, &imported)

	newResearch, _, err := exportSvc.Import(ctx, &imported, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	tasks, _ := taskSvc.List(ctx, newResearch.ID, storage.TaskFilter{})
	if len(tasks) != len(statuses) {
		t.Fatalf("tasks = %d, want %d", len(tasks), len(statuses))
	}

	importedStatuses := make(map[domain.TaskStatus]bool)
	for _, task := range tasks {
		importedStatuses[task.Status] = true
	}
	for _, s := range statuses {
		if !importedStatuses[s] {
			t.Errorf("missing status %q after import", s)
		}
	}
}

// --- JSON serialization tests ---

func TestExportData_JSONRoundTrip(t *testing.T) {
	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:        "JSON Test",
			Description: "Desc",
			Goal:        "Goal",
			Status:      domain.ResearchActive,
			Instruction: "Instruction",
			Memory:      domain.Memory{{Text: "mem1", Author: "unknown"}},
			Tags:        []string{"tag1", "tag2"},
			Sections: []domain.ExportSection{
				{
					Name: "sec", Status: domain.SectionDraft, Position: 0,
					Entries: []domain.ExportEntry{
						{Title: "E", Content: "C", Status: domain.EntryActive, Tags: []string{"t"}, SessionCode: "SS1"},
					},
				},
			},
			Sessions: []domain.ExportSession{
				{
					Code: "SS1", Title: "S", Status: domain.SessionActive,
					Questions: []domain.ExportQuestion{
						{
							Text: "Q", Priority: domain.PriorityHigh, Status: domain.QuestionAnswered, Answer: "A",
							Children: []domain.ExportQuestion{
								{Text: "CQ", Priority: domain.PriorityLow, Status: domain.QuestionPending},
							},
						},
					},
				},
			},
			Tasks: []domain.ExportTask{
				{Title: "T", Status: domain.TaskPending, Priority: domain.PriorityMedium},
			},
			Roadmaps: []domain.ExportRoadmap{
				{
					Title: "RM", Status: domain.RoadmapActive, Statuses: []string{"a"},
					Nodes: []domain.ExportRoadmapNode{
						{Code: "N1", Title: "N", NodeType: "step", PositionX: 1.5, PositionY: 2.5},
					},
					Edges: []domain.ExportRoadmapEdge{
						{SourceCode: "N1", TargetCode: "N1", EdgeType: "self"},
					},
				},
			},
		},
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var result domain.ExportData
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.Version != 1 {
		t.Errorf("version = %d", result.Version)
	}
	if result.Research.Name != "JSON Test" {
		t.Errorf("name = %q", result.Research.Name)
	}
	if result.Research.Instruction != "Instruction" {
		t.Errorf("instruction = %q", result.Research.Instruction)
	}
	if len(result.Research.Memory) != 1 {
		t.Errorf("memory = %d", len(result.Research.Memory))
	}
	if len(result.Research.Tags) != 2 {
		t.Errorf("tags = %d", len(result.Research.Tags))
	}
	if len(result.Research.Sections) != 1 {
		t.Fatalf("sections = %d", len(result.Research.Sections))
	}
	if result.Research.Sections[0].Entries[0].SessionCode != "SS1" {
		t.Errorf("session_code = %q", result.Research.Sections[0].Entries[0].SessionCode)
	}
	if len(result.Research.Sessions[0].Questions[0].Children) != 1 {
		t.Errorf("children = %d", len(result.Research.Sessions[0].Questions[0].Children))
	}
	if result.Research.Roadmaps[0].Nodes[0].PositionX != 1.5 {
		t.Errorf("position_x = %f", result.Research.Roadmaps[0].Nodes[0].PositionX)
	}
}

func TestExportData_OmitsEmptyFields(t *testing.T) {
	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "Minimal",
			Status: domain.ResearchActive,
		},
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	jsonStr := string(jsonBytes)
	// omitempty fields should not appear
	for _, field := range []string{"instruction", "memory", "sessions", "tasks", "roadmaps"} {
		if json.Valid(jsonBytes) {
			var m map[string]any
			json.Unmarshal(jsonBytes, &m)
			r := m["research"].(map[string]any)
			if _, ok := r[field]; ok {
				t.Errorf("field %q should be omitted when empty, but found in JSON: %s", field, jsonStr)
			}
		}
	}
}

func TestImport_DuplicateImportCreatesSeparateResearch(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, _, _, _, _, _ := setupExportService(t)

	data := &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "Duplicate Test",
			Status: domain.ResearchActive,
			Sections: []domain.ExportSection{
				{Name: "sec1", Position: 0, Status: domain.SectionDraft},
			},
		},
	}

	r1, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import 1: %v", err)
	}

	r2, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import 2: %v", err)
	}

	if r1.ID == r2.ID {
		t.Error("duplicate imports should create different researches")
	}
	if r1.Code == r2.Code {
		t.Error("duplicate imports should have different codes")
	}
}

// A blocks entry must survive the portable round trip with its type: before
// entry_type was carried in the export file, importing one stored its JSON as
// markdown and the entry rendered as a wall of braces.
func TestExportImportRoundTrip_PreservesBlockDocument(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, entrySvc, _, _, _ := setupExportService(t)

	research, sections, _ := researchSvc.Create(ctx, CreateResearchRequest{
		Name:     "Blocks Round Trip",
		Sections: []CreateSectionRequest{{Name: "main", Position: 0}},
	})

	doc := `{"version":1,"blocks":[
		{"type":"heading","data":{"level":2,"text":"Findings"}},
		{"type":"paragraph","data":{"text":"Refers to [[E1]]."}},
		{"type":"html","data":{"html":"<html><body>chart</body></html>","title":"Chart"}}
	]}`
	created, err := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[0].ID,
		Type:       domain.EntryBlocks,
		Content:    doc,
		Status:     domain.EntryActive,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Type != domain.EntryBlocks {
		t.Fatalf("created type = %q, want blocks", created.Type)
	}

	data, _ := exportSvc.Export(ctx, research.ID)
	if got := data.Research.Sections[0].Entries[0].Type; got != domain.EntryBlocks {
		t.Fatalf("exported entry_type = %q, want blocks — the type is not in the file", got)
	}

	jsonBytes, _ := json.Marshal(data)
	var imported domain.ExportData
	json.Unmarshal(jsonBytes, &imported)

	newResearch, _, err := exportSvc.Import(ctx, &imported, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	reExported, _ := exportSvc.Export(ctx, newResearch.ID)
	re := reExported.Research.Sections[0].Entries[0]
	if re.Type != domain.EntryBlocks {
		t.Errorf("re-exported type = %q, want blocks", re.Type)
	}

	back, err := NormalizeBlockDocument(re.Content)
	if err != nil {
		t.Fatalf("re-exported content is not a block document: %v", err)
	}
	if len(back.Blocks) != 3 {
		t.Fatalf("got %d blocks after the round trip, want 3", len(back.Blocks))
	}
	if str(back.Blocks[0].Data, "text") != "Findings" {
		t.Error("heading text was lost")
	}
	if !strings.Contains(str(back.Blocks[1].Data, "text"), "[[E1]]") {
		t.Error("the cross-reference inside block text was lost")
	}
	if str(back.Blocks[2].Data, "html") != "<html><body>chart</body></html>" {
		t.Error("the html block body was altered")
	}

	// And the entry title came from the document, not from the JSON.
	if created.Title != "Findings" {
		t.Errorf("title = %q, want it taken from the first heading", created.Title)
	}
}

// A markdown file written before block documents existed has no entry_type; it
// must still import as markdown rather than being rejected.
func TestImport_LegacyFileWithoutEntryType(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, _, entrySvc, _, _, _ := setupExportService(t)

	research, sections, _ := researchSvc.Create(ctx, CreateResearchRequest{
		Name:     "Legacy",
		Sections: []CreateSectionRequest{{Name: "main", Position: 0}},
	})
	entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Content: "# Plain\n\nbody", Status: domain.EntryActive,
	})

	data, _ := exportSvc.Export(ctx, research.ID)
	// Strip the field the way an older file would not have it.
	data.Research.Sections[0].Entries[0].Type = ""

	newResearch, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	re, _ := exportSvc.Export(ctx, newResearch.ID)
	if got := re.Research.Sections[0].Entries[0].Type; got != domain.EntryMarkdown {
		t.Errorf("type = %q, want markdown for a file with no entry_type", got)
	}
}

// A portable dump is a move. Dropping the marks would silently discard the only
// record of what somebody did not believe — and the round trip is the whole
// point of the format, so both halves are checked here.
func TestExport_AnnotationsSurviveARoundTrip(t *testing.T) {
	db := setupTestDB(t)
	log := slog.Default()
	notifier := &mockNotifier{}
	access := testAccess(db)

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	annotationRepo := storage.NewAnnotationRepository(db)

	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), access, notifier, log)
	sectionSvc := NewSectionService(sectionRepo, entryRepo, researchRepo, access, notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, access, sessionRepo,
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db),
		storage.NewCrossRefRepository(db), nil, notifier, log)
	sessionSvc := NewSessionService(db, sessionRepo, storage.NewQuestionRepository(db), researchRepo, access, entrySvc, notifier, log)
	taskSvc := NewTaskService(storage.NewTaskRepository(db), researchRepo, access, entrySvc, notifier, log)
	roadmapSvc := NewRoadmapService(storage.NewRoadmapRepository(db), storage.NewRoadmapNodeRepository(db),
		storage.NewRoadmapEdgeRepository(db), researchRepo, access, notifier, log)
	annSvc := NewAnnotationService(annotationRepo, entryRepo, storage.NewEntryRevisionRepository(db),
		access, entrySvc, entrySvc, notifier, log)

	exportSvc := NewExportService(researchSvc, sectionSvc, entrySvc, entryRepo, sessionSvc, taskSvc, roadmapSvc, log)
	exportSvc.SetAnnotations(annotationRepo)

	ctx := context.Background()
	research, sections, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Latency", Goal: "budget",
		Sections: []CreateSectionRequest{{Name: "findings", DisplayName: "Findings"}},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}
	entry, err := entrySvc.Create(ctx, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Title: "Load test", Content: "p99 stays under 40 ms at 10k rps.",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := annSvc.Create(ctx, CreateAnnotationRequest{
		EntryID: entry.ID,
		Quote:   domain.Quote{Exact: "under 40 ms"},
		Kind:    domain.AnnotationVerify,
		Body:    "where does this come from?",
	}); err != nil {
		t.Fatalf("create annotation: %v", err)
	}

	data, err := exportSvc.Export(ctx, research.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	marks := data.Research.Sections[0].Entries[0].Annotations
	if len(marks) != 1 {
		t.Fatalf("exported %d marks, want 1", len(marks))
	}
	if marks[0].Quote.Exact != "under 40 ms" || marks[0].Kind != domain.AnnotationVerify {
		t.Errorf("mark travelled wrong: %+v", marks[0])
	}

	imported, _, err := exportSvc.Import(ctx, data, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	open := domain.AnnotationOpen
	got, err := annSvc.ListByResearch(ctx, imported.ID, storage.AnnotationFilter{Status: &open})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("imported %d marks, want 1", len(got))
	}
	if got[0].Body != "where does this come from?" {
		t.Errorf("note lost: %q", got[0].Body)
	}
	// The anchor is resolved against the document as it arrived, so a mark whose
	// sentence survived the move is anchored — no special case needed.
	if got[0].Anchor == nil || got[0].Anchor.State != domain.AnchorAnchored {
		t.Errorf("anchor = %+v, want anchored after the round trip", got[0].Anchor)
	}
}
