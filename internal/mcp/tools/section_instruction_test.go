package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
	"github.com/dovod-app/app/internal/testdb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// section_list and research_get build their section payload by hand, from the
// same fields, in two files. An instruction added to only one of them is
// invisible in the call the conductor actually makes — so both are driven here
// through a real MCP client rather than asserted against the structs.
func TestSectionInstruction_MCPPayloads(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := storage.NewDB(testdb.Config(t), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	log := slog.Default()
	teams := storage.NewTeamRepository(db)
	access := service.NewAccess(teams, false)
	sectionRepo := storage.NewSectionRepository(db)
	researchRepo := storage.NewResearchRepository(db)
	researchSvc := service.NewResearchService(researchRepo, sectionRepo, teams, access, service.NoopNotifier{}, log)
	sectionSvc := service.NewSectionService(sectionRepo, storage.NewEntryRepository(db), researchRepo, access, service.NoopNotifier{}, log)

	r, sections, err := researchSvc.Create(ctx, service.CreateResearchRequest{
		Name: "Specifications",
		Sections: []service.CreateSectionRequest{
			{Name: "specs", DisplayName: "Спецификации", Position: 1},
			{Name: "notes", DisplayName: "Notes", Position: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "instruction-test", Version: "1"}, nil)
	RegisterSectionUpdate(srv, sectionSvc, log)
	RegisterSectionList(srv, sectionSvc, log)
	RegisterResearchGet(srv, researchSvc, sectionSvc, nil, nil, log)
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

	call := func(name string, args map[string]any, wantError bool) string {
		t.Helper()
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if result.IsError != wantError {
			t.Fatalf("%s: unexpected result %+v", name, result)
		}
		return result.Content[0].(*mcp.TextContent).Text
	}

	const instruction = "Назови производящий сервис. Назови потребителя. Один абзац обоснования, затем полезная нагрузка."
	update := func(instruction any, wantError bool) string {
		t.Helper()
		return call("section_update", map[string]any{
			"section_id": sections[0].ID, "instruction": instruction,
			"display_name": nil, "description": nil, "status": nil, "position": nil, "field_spec": nil,
		}, wantError)
	}
	update(instruction, false)

	// Over the cap the tool refuses rather than truncating, and it says so
	// instead of returning a Go error.
	update(strings.Repeat("я", 501), true)

	sectionsOf := func(tool, payload string) []map[string]any {
		t.Helper()
		var out struct {
			Sections []map[string]any `json:"sections"`
		}
		if err := json.Unmarshal([]byte(payload), &out); err != nil {
			t.Fatalf("decode %s: %v\n%s", tool, err, payload)
		}
		return out.Sections
	}

	for _, tc := range []struct {
		tool, payload string
	}{
		{"section_list", call("section_list", map[string]any{"research_id": r.ID}, false)},
		{"research_get", call("research_get", map[string]any{"research_id": r.Code}, false)},
	} {
		items := sectionsOf(tc.tool, tc.payload)
		if len(items) != 2 {
			t.Fatalf("%s returned %d sections", tc.tool, len(items))
		}
		if got := items[0]["instruction"]; got != instruction {
			t.Errorf("%s did not carry the section instruction: %v", tc.tool, got)
		}
		// A section with no instruction carries no key at all. Most sections
		// are topics rather than document classes, and an empty field on every
		// one of them is noise in the payload the conductor reads every call.
		if _, present := items[1]["instruction"]; present {
			t.Errorf("%s emitted an empty instruction on a section that has none", tc.tool)
		}
	}
}
