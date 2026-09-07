package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/uptrace/bun"
)

func setupRoadmapService(t *testing.T) (*RoadmapService, *mockNotifier, *bun.DB, context.Context) {
	t.Helper()
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	svc := NewRoadmapService(storage.NewRoadmapRepository(db),
		storage.NewRoadmapNodeRepository(db),
		storage.NewRoadmapEdgeRepository(db),
		storage.NewResearchRepository(db),
		testAccess(db),
		notifier,
		slog.Default(),
	)
	return svc, notifier, db, context.Background()
}

func TestRoadmapService_Create(t *testing.T) {
	t.Run("creates empty roadmap", func(t *testing.T) {
		svc, notifier, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)
		notifier.reset()

		rm, err := svc.Create(ctx, CreateRoadmapRequest{
			ResearchID:  r.ID,
			Title:       "My Roadmap",
			Description: "A test roadmap",
			Statuses:    []string{"todo", "doing", "done"},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if rm.ID == "" {
			t.Fatal("expected non-empty ID")
		}
		if rm.Code == "" {
			t.Fatal("expected auto-generated code")
		}
		if rm.Status != domain.RoadmapActive {
			t.Errorf("expected status active, got %s", rm.Status)
		}
		if len(rm.Statuses) != 3 {
			t.Errorf("expected 3 statuses, got %d", len(rm.Statuses))
		}
		if !notifier.hasEvent("roadmap.created") {
			t.Error("expected roadmap.created event")
		}
	})

	t.Run("creates roadmap with nodes and edges", func(t *testing.T) {
		svc, _, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)

		rm, err := svc.Create(ctx, CreateRoadmapRequest{
			ResearchID: r.ID,
			Title:      "Full Graph",
			Statuses:   []string{"not_started", "completed"},
			Nodes: []CreateRoadmapNodeRequest{
				{TempID: "n1", Title: "Step A", NodeType: "step", Status: "not_started"},
				{TempID: "n2", Title: "Step B", NodeType: "milestone", Status: "not_started"},
			},
			Edges: []CreateRoadmapEdgeRequest{
				{SourceNodeRef: "n1", TargetNodeRef: "n2", Label: "next", EdgeType: "default"},
			},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if len(rm.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(rm.Nodes))
		}
		if len(rm.Edges) != 1 {
			t.Errorf("expected 1 edge, got %d", len(rm.Edges))
		}

		// Verify edge references were resolved from temp_id to real IDs
		edge := rm.Edges[0]
		if edge.SourceNodeID == "n1" || edge.TargetNodeID == "n2" {
			t.Error("expected temp_ids to be resolved to real UUIDs")
		}
		if edge.SourceNodeID != rm.Nodes[0].ID {
			t.Errorf("edge source: got %s, want %s", edge.SourceNodeID, rm.Nodes[0].ID)
		}
		if edge.TargetNodeID != rm.Nodes[1].ID {
			t.Errorf("edge target: got %s, want %s", edge.TargetNodeID, rm.Nodes[1].ID)
		}
	})

	t.Run("research not found", func(t *testing.T) {
		svc, _, _, ctx := setupRoadmapService(t)

		_, err := svc.Create(ctx, CreateRoadmapRequest{
			ResearchID: "nonexistent",
			Title:      "Fail",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("default node type is step", func(t *testing.T) {
		svc, _, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)

		rm, err := svc.Create(ctx, CreateRoadmapRequest{
			ResearchID: r.ID,
			Title:      "Defaults",
			Nodes: []CreateRoadmapNodeRequest{
				{TempID: "n1", Title: "No Type"},
			},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if rm.Nodes[0].NodeType != "step" {
			t.Errorf("expected default node_type 'step', got %s", rm.Nodes[0].NodeType)
		}
	})
}

func TestRoadmapService_Get(t *testing.T) {
	t.Run("returns roadmap with nodes and edges", func(t *testing.T) {
		svc, _, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)

		created, _ := svc.Create(ctx, CreateRoadmapRequest{
			ResearchID: r.ID,
			Title:      "Get Test",
			Nodes: []CreateRoadmapNodeRequest{
				{TempID: "n1", Title: "A", NodeType: "step"},
				{TempID: "n2", Title: "B", NodeType: "step"},
			},
			Edges: []CreateRoadmapEdgeRequest{
				{SourceNodeRef: "n1", TargetNodeRef: "n2"},
			},
		})

		got, err := svc.Get(ctx, created.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Title != "Get Test" {
			t.Errorf("expected title 'Get Test', got %s", got.Title)
		}
		if len(got.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(got.Nodes))
		}
		if len(got.Edges) != 1 {
			t.Errorf("expected 1 edge, got %d", len(got.Edges))
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc, _, _, ctx := setupRoadmapService(t)

		_, err := svc.Get(ctx, "nonexistent")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestRoadmapService_List(t *testing.T) {
	svc, _, db, ctx := setupRoadmapService(t)
	r := createTestResearch(t, db)

	_, _ = svc.Create(ctx, CreateRoadmapRequest{ResearchID: r.ID, Title: "RM 1"})
	_, _ = svc.Create(ctx, CreateRoadmapRequest{ResearchID: r.ID, Title: "RM 2"})

	list, err := svc.List(ctx, r.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 roadmaps, got %d", len(list))
	}
}

func TestRoadmapService_Update(t *testing.T) {
	t.Run("partial update", func(t *testing.T) {
		svc, notifier, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)
		rm, _ := svc.Create(ctx, CreateRoadmapRequest{
			ResearchID: r.ID, Title: "Original", Description: "Old",
			Statuses: []string{"a"},
		})
		notifier.reset()

		updated, err := svc.Update(ctx, rm.ID, UpdateRoadmapRequest{
			Title:       ptr("Updated"),
			Description: ptr("New"),
			Statuses:    []string{"x", "y"},
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if updated.Title != "Updated" {
			t.Errorf("title: got %s, want Updated", updated.Title)
		}
		if updated.Description != "New" {
			t.Errorf("description: got %s, want New", updated.Description)
		}
		if len(updated.Statuses) != 2 {
			t.Errorf("statuses: got %d, want 2", len(updated.Statuses))
		}
		if !notifier.hasEvent("roadmap.updated") {
			t.Error("expected roadmap.updated event")
		}
	})

	t.Run("update status", func(t *testing.T) {
		svc, _, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)
		rm, _ := svc.Create(ctx, CreateRoadmapRequest{ResearchID: r.ID, Title: "Status Test"})

		updated, err := svc.Update(ctx, rm.ID, UpdateRoadmapRequest{
			Status: ptr(domain.RoadmapCompleted),
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if updated.Status != domain.RoadmapCompleted {
			t.Errorf("status: got %s, want completed", updated.Status)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc, _, _, ctx := setupRoadmapService(t)

		_, err := svc.Update(ctx, "nonexistent", UpdateRoadmapRequest{Title: ptr("x")})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestRoadmapService_Delete(t *testing.T) {
	t.Run("deletes roadmap", func(t *testing.T) {
		svc, notifier, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)
		rm, _ := svc.Create(ctx, CreateRoadmapRequest{ResearchID: r.ID, Title: "Delete Me"})
		notifier.reset()

		if err := svc.Delete(ctx, rm.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if !notifier.hasEvent("roadmap.deleted") {
			t.Error("expected roadmap.deleted event")
		}

		_, err := svc.Get(ctx, rm.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound after delete, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc, _, _, ctx := setupRoadmapService(t)

		err := svc.Delete(ctx, "nonexistent")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestRoadmapService_AddNodes(t *testing.T) {
	t.Run("adds nodes and edges to existing roadmap", func(t *testing.T) {
		svc, _, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)

		rm, _ := svc.Create(ctx, CreateRoadmapRequest{
			ResearchID: r.ID, Title: "Extend Me",
			Nodes: []CreateRoadmapNodeRequest{
				{TempID: "n1", Title: "Existing", NodeType: "step"},
			},
		})

		existingNodeID := rm.Nodes[0].ID

		updated, err := svc.AddNodes(ctx, rm.ID, []CreateRoadmapNodeRequest{
			{TempID: "n2", Title: "New Node", NodeType: "milestone"},
		}, []CreateRoadmapEdgeRequest{
			{SourceNodeRef: existingNodeID, TargetNodeRef: "n2", Label: "then"},
		})
		if err != nil {
			t.Fatalf("add nodes: %v", err)
		}
		if len(updated.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(updated.Nodes))
		}
		if len(updated.Edges) != 1 {
			t.Errorf("expected 1 edge, got %d", len(updated.Edges))
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc, _, _, ctx := setupRoadmapService(t)

		_, err := svc.AddNodes(ctx, "nonexistent",
			[]CreateRoadmapNodeRequest{{Title: "X", NodeType: "step"}},
			nil,
		)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestRoadmapService_UpdateNode(t *testing.T) {
	t.Run("updates node fields", func(t *testing.T) {
		svc, notifier, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)

		rm, _ := svc.Create(ctx, CreateRoadmapRequest{
			ResearchID: r.ID, Title: "Node Test",
			Nodes: []CreateRoadmapNodeRequest{
				{TempID: "n1", Title: "Original", NodeType: "step", Status: "pending"},
			},
		})
		notifier.reset()

		node, err := svc.UpdateNode(ctx, rm.Nodes[0].ID, UpdateRoadmapNodeRequest{
			Title:       ptr("Updated Node"),
			Description: ptr("New description"),
			NodeType:    ptr("milestone"),
			Status:      ptr("completed"),
			PositionX:   ptr(float64(100)),
			PositionY:   ptr(float64(200)),
		})
		if err != nil {
			t.Fatalf("update node: %v", err)
		}
		if node.Title != "Updated Node" {
			t.Errorf("title: got %s, want 'Updated Node'", node.Title)
		}
		if node.Description != "New description" {
			t.Errorf("description: got %s, want 'New description'", node.Description)
		}
		if node.NodeType != "milestone" {
			t.Errorf("node_type: got %s, want milestone", node.NodeType)
		}
		if node.Status != "completed" {
			t.Errorf("status: got %s, want completed", node.Status)
		}
		if node.PositionX != 100 {
			t.Errorf("position_x: got %f, want 100", node.PositionX)
		}
		if !notifier.hasEvent("roadmap.updated") {
			t.Error("expected roadmap.updated event")
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc, _, _, ctx := setupRoadmapService(t)

		_, err := svc.UpdateNode(ctx, "nonexistent", UpdateRoadmapNodeRequest{
			Title: ptr("x"),
		})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestRoadmapService_RemoveNodes(t *testing.T) {
	t.Run("removes nodes and cascades edges", func(t *testing.T) {
		svc, notifier, db, ctx := setupRoadmapService(t)
		r := createTestResearch(t, db)

		rm, _ := svc.Create(ctx, CreateRoadmapRequest{
			ResearchID: r.ID, Title: "Remove Test",
			Nodes: []CreateRoadmapNodeRequest{
				{TempID: "n1", Title: "A", NodeType: "step"},
				{TempID: "n2", Title: "B", NodeType: "step"},
				{TempID: "n3", Title: "C", NodeType: "step"},
			},
			Edges: []CreateRoadmapEdgeRequest{
				{SourceNodeRef: "n1", TargetNodeRef: "n2"},
				{SourceNodeRef: "n2", TargetNodeRef: "n3"},
			},
		})
		notifier.reset()

		// Remove middle node
		err := svc.RemoveNodes(ctx, rm.ID, []string{rm.Nodes[1].ID})
		if err != nil {
			t.Fatalf("remove nodes: %v", err)
		}
		if !notifier.hasEvent("roadmap.updated") {
			t.Error("expected roadmap.updated event")
		}

		// Verify
		got, _ := svc.Get(ctx, rm.ID)
		if len(got.Nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(got.Nodes))
		}
		if len(got.Edges) != 0 {
			t.Errorf("expected 0 edges (both cascaded), got %d", len(got.Edges))
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc, _, _, ctx := setupRoadmapService(t)

		err := svc.RemoveNodes(ctx, "nonexistent", []string{"x"})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

// A node id is only meaningful inside the roadmap the caller named. Before this
// test, RemoveNodes deleted by bare id: write access to any roadmap of one's own
// reached into every roadmap in the database.
func TestRoadmapService_RemoveNodesStaysInsideItsRoadmap(t *testing.T) {
	svc, notifier, db, ctx := setupRoadmapService(t)
	mine := createTestResearch(t, db)
	theirs := createTestResearch(t, db)

	myRoadmap, err := svc.Create(ctx, CreateRoadmapRequest{ResearchID: mine.ID, Title: "Mine"})
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	theirRoadmap, err := svc.Create(ctx, CreateRoadmapRequest{ResearchID: theirs.ID, Title: "Theirs"})
	if err != nil {
		t.Fatalf("create theirs: %v", err)
	}
	theirRoadmap, err = svc.AddNodes(ctx, theirRoadmap.ID, []CreateRoadmapNodeRequest{{Title: "Keep"}}, nil)
	if err != nil {
		t.Fatalf("add their node: %v", err)
	}
	myRoadmap, err = svc.AddNodes(ctx, myRoadmap.ID, []CreateRoadmapNodeRequest{{Title: "Own"}}, nil)
	if err != nil {
		t.Fatalf("add my node: %v", err)
	}
	foreign := theirRoadmap.Nodes[0].ID
	own := myRoadmap.Nodes[0].ID
	notifier.reset()

	// Naming a foreign node beside one's own must remove neither: the refusal
	// is decided before anything is deleted, and it reads like a missing node.
	err = svc.RemoveNodes(ctx, myRoadmap.ID, []string{own, foreign})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a node outside the roadmap, got %v", err)
	}
	if notifier.hasEvent("roadmap.updated") {
		t.Error("a refused removal must not announce a change")
	}
	for name, id := range map[string]string{"foreign": foreign, "own": own} {
		n, err := storage.NewRoadmapNodeRepository(db).FindByID(ctx, id)
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		if n == nil {
			t.Errorf("%s node was deleted by a refused call", name)
		}
	}

	// The same id, asked through its own roadmap, goes away.
	if err := svc.RemoveNodes(ctx, theirRoadmap.ID, []string{foreign}); err != nil {
		t.Fatalf("remove through the right roadmap: %v", err)
	}
	n, err := storage.NewRoadmapNodeRepository(db).FindByID(ctx, foreign)
	if err != nil {
		t.Fatalf("find after delete: %v", err)
	}
	if n != nil {
		t.Error("node still present after removal through its own roadmap")
	}
}

// Edges and parent links take node ids from the caller. Each may name only a
// temp_id of the same request or a node of the same roadmap; a request that
// reaches outside creates nothing.
func TestRoadmapService_NodeRefsStayInsideTheRoadmap(t *testing.T) {
	svc, _, db, ctx := setupRoadmapService(t)
	nodeRepo := storage.NewRoadmapNodeRepository(db)
	mine := createTestResearch(t, db)
	theirs := createTestResearch(t, db)

	theirRoadmap, err := svc.Create(ctx, CreateRoadmapRequest{ResearchID: theirs.ID, Title: "Theirs",
		Nodes: []CreateRoadmapNodeRequest{{TempID: "a", Title: "A"}}})
	if err != nil {
		t.Fatalf("create theirs: %v", err)
	}
	foreign := theirRoadmap.Nodes[0].ID

	myRoadmap, err := svc.Create(ctx, CreateRoadmapRequest{ResearchID: mine.ID, Title: "Mine",
		Nodes: []CreateRoadmapNodeRequest{{TempID: "m", Title: "M"}}})
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	own := myRoadmap.Nodes[0].ID

	countNodes := func() int {
		nodes, err := nodeRepo.FindByRoadmap(ctx, myRoadmap.ID)
		if err != nil {
			t.Fatalf("list nodes: %v", err)
		}
		return len(nodes)
	}

	t.Run("edge to a node of another roadmap creates nothing", func(t *testing.T) {
		before := countNodes()
		_, err := svc.AddNodes(ctx, myRoadmap.ID,
			[]CreateRoadmapNodeRequest{{TempID: "x", Title: "X"}},
			[]CreateRoadmapEdgeRequest{{SourceNodeRef: "x", TargetNodeRef: foreign}})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		if countNodes() != before {
			t.Error("a refused request must not leave its nodes behind")
		}
	})

	t.Run("parent in another roadmap is refused on add", func(t *testing.T) {
		_, err := svc.AddNodes(ctx, myRoadmap.ID,
			[]CreateRoadmapNodeRequest{{Title: "Child", ParentID: foreign}}, nil)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("parent in another roadmap is refused on update", func(t *testing.T) {
		_, err := svc.UpdateNode(ctx, own, UpdateRoadmapNodeRequest{ParentID: ptr(foreign)})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		n, _ := nodeRepo.FindByID(ctx, own)
		if n.ParentID != "" {
			t.Errorf("parent was stored despite the refusal: %q", n.ParentID)
		}
	})

	t.Run("a node cannot be its own parent", func(t *testing.T) {
		_, err := svc.UpdateNode(ctx, own, UpdateRoadmapNodeRequest{ParentID: ptr(own)})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("expected ErrValidation, got %v", err)
		}
	})

	t.Run("a parent temp_id resolves even when declared after the child", func(t *testing.T) {
		rm, err := svc.Create(ctx, CreateRoadmapRequest{ResearchID: mine.ID, Title: "Nested",
			Nodes: []CreateRoadmapNodeRequest{
				{TempID: "child", Title: "Child", ParentID: "root"},
				{TempID: "root", Title: "Root"},
			}})
		if err != nil {
			t.Fatalf("create nested: %v", err)
		}
		var rootID, childParent string
		for _, n := range rm.Nodes {
			switch n.Title {
			case "Root":
				rootID = n.ID
			case "Child":
				childParent = n.ParentID
			}
		}
		if rootID == "" || childParent != rootID {
			t.Errorf("child parent = %q, want the root's id %q", childParent, rootID)
		}
		stored, _ := nodeRepo.FindByRoadmap(ctx, rm.ID)
		for _, n := range stored {
			if n.Title == "Child" && n.ParentID != rootID {
				t.Errorf("stored child parent = %q, want %q", n.ParentID, rootID)
			}
		}
	})

	t.Run("an existing node of the same roadmap is a valid parent and edge end", func(t *testing.T) {
		rm, err := svc.AddNodes(ctx, myRoadmap.ID,
			[]CreateRoadmapNodeRequest{{TempID: "y", Title: "Y", ParentID: own}},
			[]CreateRoadmapEdgeRequest{{SourceNodeRef: own, TargetNodeRef: "y"}})
		if err != nil {
			t.Fatalf("add inside the roadmap: %v", err)
		}
		if len(rm.Edges) != 1 || rm.Edges[0].SourceNodeID != own {
			t.Errorf("edge not stored against the real node: %+v", rm.Edges)
		}
	})
}
