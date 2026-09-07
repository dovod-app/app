package tools

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ResearchImportInput struct {
	Data json.RawMessage `json:"data" jsonschema:"Complete export JSON (as returned by research_export)"`
	// Omitted, the research lands in the caller's personal team, which is
	// where a solo user's work already lives.
	TeamID *string `json:"team_id,omitempty" jsonschema:"Team to import into. Defaults to your personal team."`
}

func RegisterResearchImport(srv *mcp.Server, exportSvc *service.ExportService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "research_import",
		Description: "Import a research from portable JSON (as produced by research_export). Creates a new research with all sections, entries, sessions, questions, tasks, and roadmaps, in your personal team unless team_id names another. Cross-references are rebuilt automatically.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ResearchImportInput) (*mcp.CallToolResult, any, error) {
		if len(input.Data) == 0 {
			return validationErrorResult([]string{"data is required"})
		}

		var data domain.ExportData
		if err := json.Unmarshal(input.Data, &data); err != nil {
			return errorResult("invalid export data: " + err.Error())
		}

		teamID := ""
		if input.TeamID != nil {
			teamID = *input.TeamID
		}

		research, warnings, err := exportSvc.Import(ctx, &data, teamID)
		if err != nil {
			log.Error("research_import failed", "error", err)
			return errorResult(err.Error())
		}

		out := map[string]any{
			"status":      "imported",
			"research_id": research.ID,
			"code":        research.Code,
			"name":        research.Name,
		}
		// What the file carried and the import could not — a section whose
		// writing instruction was over the limit arrives without one, and the
		// agent that just imported it is the only reader in a position to put
		// it back. Absent when there is nothing to report.
		if len(warnings) > 0 {
			out["warnings"] = warnings
		}
		return successResult(out)
	})
}
