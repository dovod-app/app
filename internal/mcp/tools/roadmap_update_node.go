package tools

import (
	"context"
	"log/slog"

	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RoadmapUpdateNodeInput struct {
	NodeID      string   `json:"node_id" jsonschema:"ID of the node to update"`
	Title       *string  `json:"title" jsonschema:"New title"`
	Description *string  `json:"description" jsonschema:"New description text"`
	NodeType    *string  `json:"node_type" jsonschema:"New node type: step, milestone, decision, info, group, checklist, note, link, metric"`
	Status      *string  `json:"status" jsonschema:"New status (must be from the roadmap's statuses list)"`
	PositionX   *float64 `json:"position_x" jsonschema:"New X position"`
	PositionY   *float64 `json:"position_y" jsonschema:"New Y position"`
	ParentID    *string  `json:"parent_id" jsonschema:"New parent node ID; must be a node of the same roadmap (empty string to clear)"`
	RefType     *string  `json:"ref_type" jsonschema:"Reference type: entry, task, session, research, question (empty string to clear)"`
	RefID       *string  `json:"ref_id" jsonschema:"ID of the referenced entity (empty string to clear)"`
	Metadata    *string  `json:"metadata" jsonschema:"JSON string with node-type-specific data"`
	Stage       *string  `json:"stage" jsonschema:"Stage/column name for the stages view (empty string to clear)"`
	NodeDate    *string  `json:"node_date" jsonschema:"ISO date YYYY-MM-DD for the timeline view / range start (empty string to clear)"`
	NodeEndDate *string  `json:"node_end_date" jsonschema:"ISO date YYYY-MM-DD end of a timeline range; with node_date set the node renders as a bar from start to end, and must not be before node_date (empty string to clear); ignored for milestone nodes"`
}

func RegisterRoadmapUpdateNode(srv *mcp.Server, svc *service.RoadmapService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "roadmap_update_node",
		Description: "Updates a single roadmap node. Use this to change node status, content, type, position, stage (stages view) or node_date (timeline view).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input RoadmapUpdateNodeInput) (*mcp.CallToolResult, any, error) {
		if input.NodeID == "" {
			return validationErrorResult([]string{"node_id is required"})
		}

		node, err := svc.UpdateNode(ctx, input.NodeID, service.UpdateRoadmapNodeRequest{
			Title:       input.Title,
			Description: input.Description,
			NodeType:    input.NodeType,
			Status:      input.Status,
			PositionX:   input.PositionX,
			PositionY:   input.PositionY,
			ParentID:    input.ParentID,
			RefType:     input.RefType,
			RefID:       input.RefID,
			Metadata:    input.Metadata,
			Stage:       input.Stage,
			NodeDate:    input.NodeDate,
			NodeEndDate: input.NodeEndDate,
		})
		if err != nil {
			return errorResult(err.Error())
		}

		result := map[string]any{
			"node_id":   node.ID,
			"code":      node.Code,
			"title":     node.Title,
			"node_type": node.NodeType,
			"status":    node.Status,
		}
		if node.RefType != "" {
			result["ref_type"] = node.RefType
			result["ref_id"] = node.RefID
		}
		return successResult(result)
	})
}
