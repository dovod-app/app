package tools

import (
	"context"
	"log/slog"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SectionUpdateInput struct {
	SectionID   string              `json:"section_id" jsonschema:"ID of the section to update"`
	DisplayName *string             `json:"display_name" jsonschema:"New display name"`
	Description *string             `json:"description" jsonschema:"New description"`
	Status      *string             `json:"status" jsonschema:"New status: draft, active, completed, or archived. Note: completed requires at least one entry."`
	Position    *int                `json:"position" jsonschema:"New sort position"`
	Instruction *string             `json:"instruction" jsonschema:"How to write a document in this section, read before every document filed here. Three to six lines of imperatives — 'Name the producing service. State the consumer. One paragraph of rationale, then the payload.' Name the section's declared fields by key where it has any, or the instruction and the field spec drift apart. Scope it to what a document here looks like: research-wide tone and methodology belong in the research memory or a skill, and restating them here is misfiled. At most 500 characters, refused rather than truncated. Send null to leave it alone; send \"\" to remove it"`
	FieldSpec   *[]domain.FieldSpec `json:"field_spec" jsonschema:"Replace what documents in this section record: a list of {key,label,type,required,repeated,options,help}. Types: enum (needs options — prefer it, a named choice gets filled far more often than free text), ref, date, text, number, url. Send null to leave the declaration alone; send [] to remove every field, which never deletes values documents already carry. At most 12 fields and 5 required; the eleven export keys (code, title, aliases, research, section, type, status, tags, created, updated, session) are refused"`
}

func RegisterSectionUpdate(srv *mcp.Server, svc *service.SectionService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "section_update",
		Description: "Updates a section's properties. Only provided fields are updated. Setting status to 'completed' requires the section to have at least one entry.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input SectionUpdateInput) (*mcp.CallToolResult, any, error) {
		if input.SectionID == "" {
			return validationErrorResult([]string{"section_id is required"})
		}

		var status *domain.SectionStatus
		if input.Status != nil {
			s := domain.SectionStatus(*input.Status)
			status = &s
		}

		section, err := svc.Update(ctx, input.SectionID, service.UpdateSectionRequest{
			FieldSpec:   input.FieldSpec,
			Instruction: input.Instruction,
			DisplayName: input.DisplayName,
			Description: input.Description,
			Status:      status,
			Position:    input.Position,
		})
		if err != nil {
			return errorResult(err.Error())
		}

		return successResult(map[string]any{
			"section_id":   section.ID,
			"display_name": section.DisplayName,
			"status":       section.Status,
			"updated":      true,
		})
	})
}
