package tools

import (
	"context"
	"log/slog"

	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RoadmapRemoveNodesInput struct {
	RoadmapID string   `json:"roadmap_id" jsonschema:"ID of the roadmap"`
	NodeIDs   []string `json:"node_ids" jsonschema:"Array of node IDs to remove; each must belong to this roadmap, otherwise nothing is removed. Connected edges are deleted automatically."`
}

func RegisterRoadmapRemoveNodes(srv *mcp.Server, svc *service.RoadmapService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "roadmap_remove_nodes",
		Description: "Removes nodes from a roadmap. Edges connected to removed nodes are deleted automatically via cascade.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input RoadmapRemoveNodesInput) (*mcp.CallToolResult, any, error) {
		var errs []string
		if input.RoadmapID == "" {
			errs = append(errs, "roadmap_id is required")
		}
		if len(input.NodeIDs) == 0 {
			errs = append(errs, "node_ids is required (at least one)")
		}
		if len(errs) > 0 {
			return validationErrorResult(errs)
		}

		if err := svc.RemoveNodes(ctx, input.RoadmapID, input.NodeIDs); err != nil {
			return errorResult(err.Error())
		}

		return successResult(map[string]any{
			"deleted_count": len(input.NodeIDs),
		})
	})
}
