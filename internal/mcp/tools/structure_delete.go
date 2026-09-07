package tools

import (
	"context"
	"log/slog"

	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SectionDeleteInput struct {
	SectionID string `json:"section_id" jsonschema:"ID of the section to delete"`
	Force     *bool  `json:"force" jsonschema:"Delete the section even though it holds documents, and delete them with it"`
}

func RegisterSectionDelete(srv *mcp.Server, svc *service.SectionService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "section_delete",
		Description: "Deletes a section from a project. " +
			"Refuses while the section still holds documents, and says how many; pass force: true to delete them with it. " +
			"Set force only when the person asked for the documents to go too — the refusal exists so a tidy-up does not quietly destroy findings. " +
			"Documents cannot currently be moved between sections, so there is no way to empty a section except by deleting its documents.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input SectionDeleteInput) (*mcp.CallToolResult, any, error) {
		if input.SectionID == "" {
			return validationErrorResult([]string{"section_id is required"})
		}
		if err := svc.Delete(ctx, input.SectionID, derefBool(input.Force)); err != nil {
			return errorResult(err.Error())
		}
		return successResult(map[string]any{"deleted": true})
	})
}

type SessionDeleteInput struct {
	SessionID string `json:"session_id" jsonschema:"ID of the session to delete"`
}

func RegisterSessionDelete(srv *mcp.Server, svc *service.SessionService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "session_delete",
		Description: "Deletes a session and the questions asked in it. " +
			"Documents written during the session are kept — a finding is not an artefact of the conversation that produced it — and lose only their link back to it. " +
			"To close a session without destroying the questions, use session_update with status: completed.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input SessionDeleteInput) (*mcp.CallToolResult, any, error) {
		if input.SessionID == "" {
			return validationErrorResult([]string{"session_id is required"})
		}
		if err := svc.Delete(ctx, input.SessionID); err != nil {
			return errorResult(err.Error())
		}
		return successResult(map[string]any{"deleted": true})
	})
}

type QuestionDeleteInput struct {
	QuestionID string `json:"question_id" jsonschema:"ID of the question to delete"`
}

func RegisterQuestionDelete(srv *mcp.Server, svc *service.SessionService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "question_delete",
		Description: "Deletes one question. Follow-up questions asked under it survive and lose only their parent link. " +
			"Prefer question_update with status: skipped for a question that turned out not to be worth asking — that keeps the record of having considered it, which is usually what a reader wants.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input QuestionDeleteInput) (*mcp.CallToolResult, any, error) {
		if input.QuestionID == "" {
			return validationErrorResult([]string{"question_id is required"})
		}
		if err := svc.DeleteQuestion(ctx, input.QuestionID); err != nil {
			return errorResult(err.Error())
		}
		return successResult(map[string]any{"deleted": true})
	})
}
