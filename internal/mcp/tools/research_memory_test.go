package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
	"github.com/dovod-app/app/internal/testdb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResearchMemory_MCPContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := storage.NewDB(testdb.Config(t), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := storage.NewResearchRepository(db)
	team := storage.NewTeamRepository(db)
	svc := service.NewResearchService(repo, storage.NewSectionRepository(db), team, service.NewAccess(team, false), service.NoopNotifier{}, slog.Default())
	r, _, err := svc.Create(ctx, service.CreateResearchRequest{Name: "MCP memory"})
	if err != nil {
		t.Fatal(err)
	}
	session := &domain.Session{ID: "memory-session", ResearchID: r.ID, Title: "Research session", Status: domain.SessionActive}
	if err := storage.NewSessionRepository(db).Create(ctx, session); err != nil {
		t.Fatal(err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "memory-test", Version: "1"}, nil)
	RegisterResearchMemory(srv, svc)
	RegisterResearchUpdate(srv, svc, slog.Default())
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name != "research_update" {
			continue
		}
		data, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatal(err)
		}
		if schema.Properties["instruction"] != nil || schema.Properties["memory"] != nil {
			t.Fatal("MCP advertises removed fields")
		}
	}
	call := func(name string, args map[string]any, wantError bool) string {
		t.Helper()
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError != wantError {
			t.Fatalf("%s: unexpected result %+v", name, result)
		}
		return result.Content[0].(*mcp.TextContent).Text
	}
	call("research_update", map[string]any{"research_id": r.Code, "name": nil, "description": nil, "goal": nil, "status": nil, "tags": nil, "add_memory": "convenience", "session_id": session.Code}, false)
	for _, field := range []string{"memory", "instruction"} {
		args := map[string]any{"research_id": r.Code, "name": nil, "description": nil, "goal": nil, "status": nil, "tags": nil, field: nil}
		_, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "research_update", Arguments: args})
		if err == nil || !strings.Contains(err.Error(), "unexpected additional properties") {
			t.Fatalf("removed MCP field %s was not rejected: %v", field, err)
		}
	}
	items, err := svc.ListMemory(ctx, r.ID)
	if err != nil || len(items) != 1 || items[0].Author != "agent" || items[0].SessionID != session.ID {
		t.Fatalf("MCP provenance: %+v %v", items, err)
	}
	call("research_memory", map[string]any{"research_id": r.Code, "action": "update", "item_id": items[0].ID, "version": 1, "text": "edited"}, false)
	call("research_memory", map[string]any{"research_id": r.Code, "action": "update", "item_id": items[0].ID, "version": 1, "text": "stale"}, true)
	call("research_memory", map[string]any{"research_id": r.Code, "action": "delete", "ids": []string{items[0].ID}}, false)
	if got := call("research_memory", map[string]any{"research_id": r.Code, "action": "list"}, false); got != "[]" {
		t.Fatalf("memory after delete: %s", got)
	}
	call("research_memory", map[string]any{"research_id": r.Code, "action": "add", "text": "tool append", "session_id": session.Code}, false)
	call("research_memory", map[string]any{"research_id": r.Code, "action": "add", "text": " "}, true)
	call("research_memory", map[string]any{"research_id": r.Code, "action": "delete", "ids": []string{}}, true)
	call("research_memory", map[string]any{"research_id": r.Code, "action": "unknown"}, true)
	call("research_memory", map[string]any{"research_id": "missing", "action": "list"}, true)
}
