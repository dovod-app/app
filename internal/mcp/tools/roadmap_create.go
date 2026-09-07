package tools

import (
	"context"
	"log/slog"

	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RoadmapCreateNode struct {
	TempID      string   `json:"temp_id" jsonschema:"Temporary ID for referencing this node in edges array (e.g. 'n1', 'n2')"`
	Title       string   `json:"title" jsonschema:"Node title"`
	Description *string  `json:"description" jsonschema:"Detailed text content for the node"`
	NodeType    *string  `json:"node_type" jsonschema:"Node type: step, milestone, decision, info, group, checklist, note, link, metric. Default: step"`
	Status      *string  `json:"status" jsonschema:"Initial status from the roadmap's statuses list"`
	PositionX   *float64 `json:"position_x" jsonschema:"X position for layout"`
	PositionY   *float64 `json:"position_y" jsonschema:"Y position for layout"`
	ParentID    *string  `json:"parent_id" jsonschema:"Parent node for hierarchical nesting: a temp_id from this request or the ID of a node already in this roadmap"`
	RefType     *string  `json:"ref_type" jsonschema:"Reference type linking to a research entity: entry, task, session, research, question. Leave empty for standalone nodes"`
	RefID       *string  `json:"ref_id" jsonschema:"ID of the referenced entity (entry ID, task ID, etc.)"`
	Metadata    *string  `json:"metadata" jsonschema:"JSON string with node-type-specific data (e.g. checklist items, URL for link nodes, metric value)"`
	Stage       *string  `json:"stage" jsonschema:"Stage/column name for the stages view. Should match one of the roadmap's stages"`
	NodeDate    *string  `json:"node_date" jsonschema:"ISO date YYYY-MM-DD placing the node on the timeline (its start). Leave empty for an undated node"`
	NodeEndDate *string  `json:"node_end_date" jsonschema:"Optional ISO date YYYY-MM-DD end of a timeline range. With node_date set, the node renders as a bar from start to end; leave empty for a point; ignored for milestone nodes"`
}

type RoadmapCreateEdge struct {
	SourceNodeRef string  `json:"source" jsonschema:"Source node: a temp_id from this request or the ID of a node in this roadmap"`
	TargetNodeRef string  `json:"target" jsonschema:"Target node: a temp_id from this request or the ID of a node in this roadmap"`
	Label         *string `json:"label" jsonschema:"Edge label (e.g. 'next', 'if yes', 'alternative')"`
	EdgeType      *string `json:"edge_type" jsonschema:"Edge type: default, success, warning, optional. Default: default"`
}

type RoadmapCreateInput struct {
	ResearchID  string              `json:"research_id" jsonschema:"ID of the research"`
	Title       string              `json:"title" jsonschema:"Roadmap title"`
	Description *string             `json:"description" jsonschema:"Roadmap description"`
	Statuses    []string            `json:"statuses" jsonschema:"Available node statuses in order (e.g. ['not_started', 'in_progress', 'completed'])"`
	Stages      []string            `json:"stages" jsonschema:"Ordered stage/column names for the stages view (e.g. ['Discovery', 'Design', 'Build', 'Launch']). A stage with no nodes is an empty column"`
	View        *string             `json:"view" jsonschema:"Which layout the roadmap opens in: graph (free node-edge graph), stages (columns), or timeline (by node_date). Default: graph"`
	Nodes       []RoadmapCreateNode `json:"nodes" jsonschema:"Array of nodes to create"`
	Edges       []RoadmapCreateEdge `json:"edges" jsonschema:"Array of edges connecting nodes by temp_id"`
}

func RegisterRoadmapCreate(srv *mcp.Server, svc *service.RoadmapService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "roadmap_create",
		Description: "Creates a complete roadmap graph with nodes and edges in one call. Use temp_id on nodes to reference them in edges. Ideal for building learning paths, strategy maps, or step-by-step guides.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input RoadmapCreateInput) (*mcp.CallToolResult, any, error) {
		var errs []string
		if input.ResearchID == "" {
			errs = append(errs, "research_id is required")
		}
		if input.Title == "" {
			errs = append(errs, "title is required")
		}
		if len(errs) > 0 {
			return validationErrorResult(errs)
		}

		var nodes []service.CreateRoadmapNodeRequest
		for _, n := range input.Nodes {
			nodes = append(nodes, nodeFromInput(n))
		}

		var edges []service.CreateRoadmapEdgeRequest
		for _, e := range input.Edges {
			edges = append(edges, edgeFromInput(e))
		}

		rm, err := svc.Create(ctx, service.CreateRoadmapRequest{
			ResearchID:  input.ResearchID,
			Title:       input.Title,
			Description: derefStr(input.Description),
			Statuses:    input.Statuses,
			Stages:      input.Stages,
			View:        derefStr(input.View),
			Nodes:       nodes,
			Edges:       edges,
		})
		if err != nil {
			return errorResult(err.Error())
		}

		nodesSummary := make([]map[string]any, 0, len(rm.Nodes))
		for _, n := range rm.Nodes {
			nodesSummary = append(nodesSummary, map[string]any{
				"node_id": n.ID, "code": n.Code, "title": n.Title,
			})
		}

		return successResult(map[string]any{
			"roadmap_id":    rm.ID,
			"code":          rm.Code,
			"title":         rm.Title,
			"nodes_created": len(rm.Nodes),
			"edges_created": len(rm.Edges),
			"nodes":         nodesSummary,
		})
	})
}
