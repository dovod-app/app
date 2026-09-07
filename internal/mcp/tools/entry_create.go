package tools

import (
	"context"
	"log/slog"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type EntryCreateInput struct {
	ResearchID  string          `json:"research_id" jsonschema:"ID of the research"`
	SectionID   string          `json:"section_id" jsonschema:"ID of the section"`
	SessionID   *string         `json:"session_id" jsonschema:"Optional session ID to link this entry to a session"`
	EntryType   *string         `json:"entry_type" jsonschema:"Optional content kind. markdown (default): content is markdown. blocks: content is a block document {version:1,blocks:[{type,data}]} — an article of typed blocks (paragraph, heading, list, table, quote, code, callout, divider, image, html, checklist, mermaid, task_ref, transcript), which is how you mix prose with alerts and custom visuals. artifact: sugar for a blocks document holding one html block; pass the HTML document as content. See /llms/blocks.md for the block catalog"`
	Content     string          `json:"content" jsonschema:"Markdown content of the entry"`
	Title       *string         `json:"title" jsonschema:"Optional title (auto-generated from content if empty)"`
	Description *string         `json:"description" jsonschema:"Optional description (auto-generated from content if empty)"`
	Status      *string         `json:"status" jsonschema:"Entry status: draft, active, completed, archived. Default: draft"`
	Tags        []string        `json:"tags" jsonschema:"Tags for categorization"`
	Metadata    *map[string]any `json:"metadata" jsonschema:"Optional values for the fields this section declares, keyed by field key — call section_list to see them. The vocabulary is closed: a key the section does not declare is reported back and dropped, and a section that declares nothing accepts none. Nothing here fails the write; the response says what was stored, what was rejected and which required fields are still empty. A [[E3]] inside a value is not indexed as a cross-reference"`
}

func RegisterEntryCreate(srv *mcp.Server, svc *service.EntryService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "entry_create",
		Description: "Creates a new entry within a section. Follow the section's instruction if it has one — you will usually already have it from research_get, and most sections carry none. It says what a document in this particular section looks like, and it wins over the research memory and any skill on that question only. Title and description are auto-generated from content if not provided. Returns entry_id and auto-generated fields.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input EntryCreateInput) (*mcp.CallToolResult, any, error) {
		var errs []string
		if input.ResearchID == "" {
			errs = append(errs, "research_id is required")
		}
		if input.SectionID == "" {
			errs = append(errs, "section_id is required")
		}
		if input.Content == "" {
			errs = append(errs, "content is required")
		}
		if len(errs) > 0 {
			return validationErrorResult(errs)
		}

		var status domain.EntryStatus
		if s := derefStr(input.Status); s != "" {
			status = domain.EntryStatus(s)
		}

		entry, err := svc.Create(ctx, service.CreateEntryRequest{
			ResearchID:  input.ResearchID,
			SectionID:   input.SectionID,
			SessionID:   derefStr(input.SessionID),
			Type:        domain.EntryType(derefStr(input.EntryType)),
			Content:     input.Content,
			Title:       derefStr(input.Title),
			Description: derefStr(input.Description),
			Status:      status,
			Tags:        input.Tags,
			Metadata:    derefMap(input.Metadata),
		})
		if err != nil {
			return errorResult(err.Error())
		}

		out := map[string]any{
			"entry_id":    entry.ID,
			"code":        entry.Code,
			"title":       entry.Title,
			"description": entry.Description,
			"status":      entry.Status,
			"hint":        "Use [[" + entry.Code + "]] in other entries to cross-reference this entry.",
		}
		addMetadataReport(out, entry)
		return successResult(out)
	})
}
