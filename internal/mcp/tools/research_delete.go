package tools

import (
	"context"
	"log/slog"

	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ResearchDeleteInput struct {
	ResearchID string `json:"research_id" jsonschema:"ID or short code (R1) of the project to delete"`
	Confirm    *bool  `json:"confirm" jsonschema:"Must be true. Set it only when the person asked for this specific project to be deleted, in those words"`
}

func RegisterResearchDelete(srv *mcp.Server, svc *service.ResearchService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "research_delete",
		Description: "Permanently deletes a project and everything in it: sections, documents and their revision history, sessions and questions, tasks, roadmaps, annotations, memory, share links and attached methodologies. " +
			"This cannot be undone — there is no trash and no restore. " +
			"Requires confirm: true, and you must set it only when the person asked for this specific project to be deleted, in those words. Never infer it from a sentence that merely sounded like a request to clean up. " +
			"To put a finished project out of the way instead, use research_update with status: archived, which is reversible. " +
			"Call research_delete_preview first to see what would go. Only the project's owner may delete it. " +
			"References to this project from other projects are not deleted: they stop resolving and stay as the text that was written.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ResearchDeleteInput) (*mcp.CallToolResult, any, error) {
		if input.ResearchID == "" {
			return validationErrorResult([]string{"research_id is required"})
		}
		// The confirmation is the whole safety rail on this tool, and it is a
		// validation result rather than a refusal so the model is told what to
		// do about it — and so a mistaken call changes nothing.
		if !derefBool(input.Confirm) {
			return validationErrorResult([]string{
				"confirm must be true to delete a project. Set it only if the person asked for this project to be deleted; if you are inferring it, ask them first. To archive instead, call research_update with status: archived.",
			})
		}

		summary, err := svc.DeletionSummary(ctx, input.ResearchID)
		if err != nil {
			return errorResult(err.Error())
		}
		if err := svc.Delete(ctx, input.ResearchID); err != nil {
			return errorResult(err.Error())
		}

		// Report what went, so the model can tell the person what it did rather
		// than "done".
		return successResult(map[string]any{
			"deleted":       true,
			"destroyed":     summary,
			"unresolved_in": summary.IncomingFrom,
		})
	})
}

type ResearchDeletePreviewInput struct {
	ResearchID string `json:"research_id" jsonschema:"ID or short code (R1) of the project"`
}

func RegisterResearchDeletePreview(srv *mcp.Server, svc *service.ResearchService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "research_delete_preview",
		Description: "Counts what deleting a project would destroy, without deleting anything. " +
			"Call this before research_delete and tell the person the numbers: 'delete R7' and 'delete 4 sections, 12 documents and 3 sessions' are different decisions, and the second one is the true one. " +
			"Also reports references from other projects that would stop resolving.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ResearchDeletePreviewInput) (*mcp.CallToolResult, any, error) {
		if input.ResearchID == "" {
			return validationErrorResult([]string{"research_id is required"})
		}
		summary, err := svc.DeletionSummary(ctx, input.ResearchID)
		if err != nil {
			return errorResult(err.Error())
		}
		return successResult(summary)
	})
}
