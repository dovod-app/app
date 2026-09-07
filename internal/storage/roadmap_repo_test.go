package storage

import (
	"context"
	"testing"

	"github.com/dovod-app/app/internal/domain"
	"github.com/google/uuid"
)

func TestRoadmapRepository_CreateAndFindByID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	repo := NewRoadmapRepository(db)

	rm := &domain.Roadmap{
		ID:          uuid.New().String(),
		ResearchID:  research.ID,
		Title:       "Learning Path",
		Description: "Step-by-step guide",
		Statuses:    []string{"not_started", "in_progress", "completed"},
		Status:      domain.RoadmapActive,
	}

	if err := repo.Create(ctx, rm); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if rm.Code == "" {
		t.Error("expected auto-generated code")
	}
	if rm.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	found, err := repo.FindByID(ctx, rm.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("FindByID returned nil")
	}
	if found.Title != "Learning Path" {
		t.Errorf("Title: got %s, want 'Learning Path'", found.Title)
	}
	if found.Description != "Step-by-step guide" {
		t.Errorf("Description: got %s, want 'Step-by-step guide'", found.Description)
	}
	if len(found.Statuses) != 3 {
		t.Errorf("Statuses: got %d items, want 3", len(found.Statuses))
	}
	if found.Status != domain.RoadmapActive {
		t.Errorf("Status: got %s, want active", found.Status)
	}
}

func TestRoadmapRepository_Update(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	repo := NewRoadmapRepository(db)

	rm := &domain.Roadmap{
		ID:         uuid.New().String(),
		ResearchID: research.ID,
		Title:      "Original",
		Statuses:   []string{},
		Status:     domain.RoadmapActive,
	}
	if err := repo.Create(ctx, rm); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rm.Title = "Updated"
	rm.Description = "New desc"
	rm.Statuses = []string{"todo", "done"}
	rm.Status = domain.RoadmapCompleted

	if err := repo.Update(ctx, rm); err != nil {
		t.Fatalf("Update: %v", err)
	}

	found, err := repo.FindByID(ctx, rm.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.Title != "Updated" {
		t.Errorf("Title: got %s, want Updated", found.Title)
	}
	if found.Description != "New desc" {
		t.Errorf("Description: got %s, want 'New desc'", found.Description)
	}
	if len(found.Statuses) != 2 {
		t.Errorf("Statuses: got %d, want 2", len(found.Statuses))
	}
	if found.Status != domain.RoadmapCompleted {
		t.Errorf("Status: got %s, want completed", found.Status)
	}
}

func TestRoadmapRepository_FindByResearch(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	repo := NewRoadmapRepository(db)

	for i, title := range []string{"RM A", "RM B"} {
		rm := &domain.Roadmap{
			ID: uuid.New().String(), ResearchID: research.ID,
			Title: title, Statuses: []string{}, Status: domain.RoadmapActive,
		}
		_ = i
		if err := repo.Create(ctx, rm); err != nil {
			t.Fatalf("Create %s: %v", title, err)
		}
	}

	found, err := repo.FindByResearch(ctx, research.ID)
	if err != nil {
		t.Fatalf("FindByResearch: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 roadmaps, got %d", len(found))
	}
	if found[0].Title != "RM A" {
		t.Errorf("first roadmap: got %s, want 'RM A'", found[0].Title)
	}
}

func TestRoadmapRepository_Delete(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	repo := NewRoadmapRepository(db)

	rm := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "To Delete", Statuses: []string{}, Status: domain.RoadmapActive,
	}
	if err := repo.Create(ctx, rm); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, rm.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	found, err := repo.FindByID(ctx, rm.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found != nil {
		t.Error("expected nil after Delete")
	}
}

func TestRoadmapRepository_FindByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRoadmapRepository(db)
	ctx := context.Background()

	found, err := repo.FindByID(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found != nil {
		t.Error("expected nil for non-existent record")
	}
}

func TestRoadmapNodeRepository_CreateAndFindByRoadmap(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	rmRepo := NewRoadmapRepository(db)
	nodeRepo := NewRoadmapNodeRepository(db)

	rm := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "Test RM", Statuses: []string{}, Status: domain.RoadmapActive,
	}
	if err := rmRepo.Create(ctx, rm); err != nil {
		t.Fatalf("Create roadmap: %v", err)
	}

	node := &domain.RoadmapNode{
		ID: uuid.New().String(), RoadmapID: rm.ID,
		Title: "Step 1", Description: "First step",
		NodeType: "step", Status: "not_started",
		PositionX: 100, PositionY: 200,
	}
	if err := nodeRepo.Create(ctx, node); err != nil {
		t.Fatalf("Create node: %v", err)
	}

	if node.Code == "" {
		t.Error("expected auto-generated code")
	}
	if node.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	found, err := nodeRepo.FindByRoadmap(ctx, rm.ID)
	if err != nil {
		t.Fatalf("FindByRoadmap: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 node, got %d", len(found))
	}
	if found[0].Title != "Step 1" {
		t.Errorf("Title: got %s, want 'Step 1'", found[0].Title)
	}
	if found[0].NodeType != "step" {
		t.Errorf("NodeType: got %s, want step", found[0].NodeType)
	}
	if found[0].PositionX != 100 {
		t.Errorf("PositionX: got %f, want 100", found[0].PositionX)
	}
}

func TestRoadmapNodeRepository_UpdateAndFindByID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	rmRepo := NewRoadmapRepository(db)
	nodeRepo := NewRoadmapNodeRepository(db)

	rm := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "Test RM", Statuses: []string{}, Status: domain.RoadmapActive,
	}
	if err := rmRepo.Create(ctx, rm); err != nil {
		t.Fatalf("Create roadmap: %v", err)
	}

	node := &domain.RoadmapNode{
		ID: uuid.New().String(), RoadmapID: rm.ID,
		Title: "Original", NodeType: "step", Status: "pending",
	}
	if err := nodeRepo.Create(ctx, node); err != nil {
		t.Fatalf("Create node: %v", err)
	}

	node.Title = "Updated"
	node.Status = "completed"
	node.NodeType = "milestone"
	node.PositionX = 50
	node.PositionY = 75

	if err := nodeRepo.Update(ctx, node); err != nil {
		t.Fatalf("Update: %v", err)
	}

	found, err := nodeRepo.FindByID(ctx, node.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("FindByID returned nil")
	}
	if found.Title != "Updated" {
		t.Errorf("Title: got %s, want Updated", found.Title)
	}
	if found.Status != "completed" {
		t.Errorf("Status: got %s, want completed", found.Status)
	}
	if found.NodeType != "milestone" {
		t.Errorf("NodeType: got %s, want milestone", found.NodeType)
	}
	if found.PositionX != 50 {
		t.Errorf("PositionX: got %f, want 50", found.PositionX)
	}
}

func TestRoadmapNodeRepository_ParentID(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	rmRepo := NewRoadmapRepository(db)
	nodeRepo := NewRoadmapNodeRepository(db)

	rm := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "Test RM", Statuses: []string{}, Status: domain.RoadmapActive,
	}
	if err := rmRepo.Create(ctx, rm); err != nil {
		t.Fatalf("Create roadmap: %v", err)
	}

	parent := &domain.RoadmapNode{
		ID: uuid.New().String(), RoadmapID: rm.ID,
		Title: "Parent", NodeType: "group",
	}
	if err := nodeRepo.Create(ctx, parent); err != nil {
		t.Fatalf("Create parent: %v", err)
	}

	child := &domain.RoadmapNode{
		ID: uuid.New().String(), RoadmapID: rm.ID,
		Title: "Child", NodeType: "step", ParentID: parent.ID,
	}
	if err := nodeRepo.Create(ctx, child); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	found, err := nodeRepo.FindByID(ctx, child.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.ParentID != parent.ID {
		t.Errorf("ParentID: got %s, want %s", found.ParentID, parent.ID)
	}

	// Node without parent should have empty ParentID
	foundParent, _ := nodeRepo.FindByID(ctx, parent.ID)
	if foundParent.ParentID != "" {
		t.Errorf("Parent's ParentID: got %s, want empty", foundParent.ParentID)
	}
}

func TestRoadmapNodeRepository_Delete_CascadesEdges(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	rmRepo := NewRoadmapRepository(db)
	nodeRepo := NewRoadmapNodeRepository(db)
	edgeRepo := NewRoadmapEdgeRepository(db)

	rm := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "Test RM", Statuses: []string{}, Status: domain.RoadmapActive,
	}
	if err := rmRepo.Create(ctx, rm); err != nil {
		t.Fatalf("Create roadmap: %v", err)
	}

	nodeA := &domain.RoadmapNode{ID: uuid.New().String(), RoadmapID: rm.ID, Title: "A", NodeType: "step"}
	nodeB := &domain.RoadmapNode{ID: uuid.New().String(), RoadmapID: rm.ID, Title: "B", NodeType: "step"}
	for _, n := range []*domain.RoadmapNode{nodeA, nodeB} {
		if err := nodeRepo.Create(ctx, n); err != nil {
			t.Fatalf("Create node: %v", err)
		}
	}

	edge := &domain.RoadmapEdge{
		ID: uuid.New().String(), RoadmapID: rm.ID,
		SourceNodeID: nodeA.ID, TargetNodeID: nodeB.ID,
		Label: "next", EdgeType: "default",
	}
	if err := edgeRepo.Create(ctx, edge); err != nil {
		t.Fatalf("Create edge: %v", err)
	}

	// The same id through a roadmap it does not belong to is not a delete.
	if gone, err := nodeRepo.DeleteFromRoadmap(ctx, uuid.New().String(), nodeA.ID); err != nil || gone {
		t.Fatalf("DeleteFromRoadmap with a foreign roadmap: gone=%v err=%v", gone, err)
	}

	// Delete source node — edge should cascade
	if gone, err := nodeRepo.DeleteFromRoadmap(ctx, rm.ID, nodeA.ID); err != nil || !gone {
		t.Fatalf("DeleteFromRoadmap: gone=%v err=%v", gone, err)
	}

	edges, err := edgeRepo.FindByRoadmap(ctx, rm.ID)
	if err != nil {
		t.Fatalf("FindByRoadmap edges: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after node delete, got %d", len(edges))
	}
}

func TestRoadmapEdgeRepository_CreateAndFindByRoadmap(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	rmRepo := NewRoadmapRepository(db)
	nodeRepo := NewRoadmapNodeRepository(db)
	edgeRepo := NewRoadmapEdgeRepository(db)

	rm := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "Test RM", Statuses: []string{}, Status: domain.RoadmapActive,
	}
	if err := rmRepo.Create(ctx, rm); err != nil {
		t.Fatalf("Create roadmap: %v", err)
	}

	nodeA := &domain.RoadmapNode{ID: uuid.New().String(), RoadmapID: rm.ID, Title: "A", NodeType: "step"}
	nodeB := &domain.RoadmapNode{ID: uuid.New().String(), RoadmapID: rm.ID, Title: "B", NodeType: "step"}
	nodeC := &domain.RoadmapNode{ID: uuid.New().String(), RoadmapID: rm.ID, Title: "C", NodeType: "milestone"}
	for _, n := range []*domain.RoadmapNode{nodeA, nodeB, nodeC} {
		if err := nodeRepo.Create(ctx, n); err != nil {
			t.Fatalf("Create node: %v", err)
		}
	}

	edges := []*domain.RoadmapEdge{
		{ID: uuid.New().String(), RoadmapID: rm.ID, SourceNodeID: nodeA.ID, TargetNodeID: nodeB.ID, Label: "next", EdgeType: "default"},
		{ID: uuid.New().String(), RoadmapID: rm.ID, SourceNodeID: nodeB.ID, TargetNodeID: nodeC.ID, Label: "complete", EdgeType: "success"},
	}
	for _, e := range edges {
		if err := edgeRepo.Create(ctx, e); err != nil {
			t.Fatalf("Create edge: %v", err)
		}
	}

	found, err := edgeRepo.FindByRoadmap(ctx, rm.ID)
	if err != nil {
		t.Fatalf("FindByRoadmap: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(found))
	}
	// Both edges may share the same second-resolution created_at. SQL makes
	// no promise about their order within that tie (MySQL can return either).
	// Check every field by identity instead of depending on physical row order.
	byID := make(map[string]*domain.RoadmapEdge, len(found))
	for _, edge := range found {
		byID[edge.ID] = edge
	}
	for _, want := range edges {
		got := byID[want.ID]
		if got == nil || got.RoadmapID != want.RoadmapID || got.SourceNodeID != want.SourceNodeID || got.TargetNodeID != want.TargetNodeID || got.Label != want.Label || got.EdgeType != want.EdgeType {
			t.Errorf("edge %s: got %+v, want %+v", want.ID, got, want)
		}
	}
}

func TestRoadmapRepository_Delete_CascadesNodesAndEdges(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	rmRepo := NewRoadmapRepository(db)
	nodeRepo := NewRoadmapNodeRepository(db)
	edgeRepo := NewRoadmapEdgeRepository(db)

	rm := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "Cascade Test", Statuses: []string{}, Status: domain.RoadmapActive,
	}
	if err := rmRepo.Create(ctx, rm); err != nil {
		t.Fatalf("Create roadmap: %v", err)
	}

	nodeA := &domain.RoadmapNode{ID: uuid.New().String(), RoadmapID: rm.ID, Title: "A", NodeType: "step"}
	nodeB := &domain.RoadmapNode{ID: uuid.New().String(), RoadmapID: rm.ID, Title: "B", NodeType: "step"}
	for _, n := range []*domain.RoadmapNode{nodeA, nodeB} {
		if err := nodeRepo.Create(ctx, n); err != nil {
			t.Fatalf("Create node: %v", err)
		}
	}

	edge := &domain.RoadmapEdge{
		ID: uuid.New().String(), RoadmapID: rm.ID,
		SourceNodeID: nodeA.ID, TargetNodeID: nodeB.ID,
		Label: "next", EdgeType: "default",
	}
	if err := edgeRepo.Create(ctx, edge); err != nil {
		t.Fatalf("Create edge: %v", err)
	}

	// Delete roadmap — everything should cascade
	if err := rmRepo.Delete(ctx, rm.ID); err != nil {
		t.Fatalf("Delete roadmap: %v", err)
	}

	nodes, _ := nodeRepo.FindByRoadmap(ctx, rm.ID)
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes after roadmap delete, got %d", len(nodes))
	}

	edges, _ := edgeRepo.FindByRoadmap(ctx, rm.ID)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after roadmap delete, got %d", len(edges))
	}
}

func TestRoadmapNodeRepository_CodeAutoIncrement(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	rmRepo := NewRoadmapRepository(db)
	nodeRepo := NewRoadmapNodeRepository(db)

	rm := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "Code Test", Statuses: []string{}, Status: domain.RoadmapActive,
	}
	if err := rmRepo.Create(ctx, rm); err != nil {
		t.Fatalf("Create roadmap: %v", err)
	}

	n1 := &domain.RoadmapNode{ID: uuid.New().String(), RoadmapID: rm.ID, Title: "First", NodeType: "step"}
	n2 := &domain.RoadmapNode{ID: uuid.New().String(), RoadmapID: rm.ID, Title: "Second", NodeType: "step"}

	if err := nodeRepo.Create(ctx, n1); err != nil {
		t.Fatalf("Create n1: %v", err)
	}
	if err := nodeRepo.Create(ctx, n2); err != nil {
		t.Fatalf("Create n2: %v", err)
	}

	if n1.Code != "N1" {
		t.Errorf("first node code: got %s, want N1", n1.Code)
	}
	if n2.Code != "N2" {
		t.Errorf("second node code: got %s, want N2", n2.Code)
	}
}

func TestRoadmapRepository_CodeAutoIncrement(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	researchRepo := NewResearchRepository(db)
	research := createTestResearch(t, researchRepo, ctx)
	repo := NewRoadmapRepository(db)

	rm1 := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "First", Statuses: []string{}, Status: domain.RoadmapActive,
	}
	rm2 := &domain.Roadmap{
		ID: uuid.New().String(), ResearchID: research.ID,
		Title: "Second", Statuses: []string{}, Status: domain.RoadmapActive,
	}

	if err := repo.Create(ctx, rm1); err != nil {
		t.Fatalf("Create rm1: %v", err)
	}
	if err := repo.Create(ctx, rm2); err != nil {
		t.Fatalf("Create rm2: %v", err)
	}

	if rm1.Code != "RM1" {
		t.Errorf("first roadmap code: got %s, want RM1", rm1.Code)
	}
	if rm2.Code != "RM2" {
		t.Errorf("second roadmap code: got %s, want RM2", rm2.Code)
	}
}
