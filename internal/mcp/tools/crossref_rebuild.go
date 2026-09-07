package tools

import (
	"context"
	"log/slog"

	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CrossRefRebuildInput struct {
	ResearchID string `json:"research_id" jsonschema:"Research UUID or R code"`
}

// RegisterCrossRefRebuild exposes the repair that had a REST route, no button
// and no tool.
//
// The description tells the agent when *not* to reach for it, because the
// failure it used to cure is gone: a reference written before its target is
// repaired the moment the target is created. What is left is the cases nothing
// can hook — an import, a restore, codes backfilled onto old records.
func RegisterCrossRefRebuild(srv *mcp.Server, svc *service.EntryService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "crossref_rebuild",
		Description: "Re-scans every source in a research — documents, task results and question answers — and rewrites the [[...]] reference index. " +
			"You rarely need this: a reference written before its target existed is repaired automatically the moment the target is created. " +
			"Run it after restoring a research, after short codes were backfilled onto records that predate them, or when a code has been reused following a delete. " +
			"research_import already rebuilds as its last step, so there is nothing to run after one. " +
			"Returns how many sources were scanned, how many references they hold, and how many still point at nothing — that last number is the one to act on, because after the automatic repair it means a typo or a deleted target rather than a reference waiting its turn.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input CrossRefRebuildInput) (*mcp.CallToolResult, any, error) {
		if input.ResearchID == "" {
			return validationErrorResult([]string{"research_id is required"})
		}

		report, err := svc.RebuildCrossRefs(ctx, input.ResearchID)
		if err != nil {
			return errorResult(err.Error())
		}

		return successResult(map[string]any{
			"sources":    report.Sources,
			"references": report.References,
			"unresolved": report.Unresolved,
		})
	})
}
