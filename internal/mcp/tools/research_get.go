package tools

import (
	"context"
	"log/slog"

	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ResearchGetInput struct {
	ResearchID string `json:"research_id" jsonschema:"ID of the research to retrieve"`
}

func RegisterResearchGet(srv *mcp.Server, researchSvc *service.ResearchService, sectionSvc *service.SectionService, sessionSvc *service.SessionService, skillSvc *service.SkillService, log *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "research_get",
		Description: "Returns full research context including sections with entry counts — each with its writing instruction and declared fields where it has them — and the active session. Use this to understand the current state of a research project.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ResearchGetInput) (*mcp.CallToolResult, any, error) {
		if input.ResearchID == "" {
			return validationErrorResult([]string{"research_id is required"})
		}

		research, err := researchSvc.Get(ctx, input.ResearchID)
		if err != nil {
			return errorResult(err.Error())
		}

		// Everything below uses the resolved id, never the caller's string.
		// research_get accepts a short code — Get resolves one — but the calls
		// after it did not, so `research_get("R1")` answered "not found" from
		// the section lookup after the research had already been found.
		sections, err := sectionSvc.List(ctx, research.ID)
		if err != nil {
			return errorResult(err.Error())
		}

		var sectionData []map[string]any
		for _, s := range sections {
			count, _ := sectionSvc.CountEntries(ctx, s.ID)
			item := map[string]any{
				"id":            s.ID,
				"name":          s.Name,
				"display_name":  s.DisplayName,
				"description":   s.Description,
				"status":        s.Status,
				"position":      s.Position,
				"entries_count": count,
				"spec_version":  s.SpecVersion,
			}
			// Both of these only when the section has one. Most sections are
			// topics rather than document classes and carry neither, and an
			// empty field_spec on every one of them is noise in a payload the
			// conductor reads on every call.
			//
			// The instruction rides here rather than waiting for a lookup
			// because this is the call the writer already makes before writing:
			// a convention reachable only from a tool nobody runs does not
			// exist, whatever the section stores.
			if s.Instruction != "" {
				item["instruction"] = s.Instruction
			}
			if len(s.FieldSpec) > 0 {
				item["field_spec"] = s.FieldSpec
			}

			sectionData = append(sectionData, item)
		}

		var activeSession map[string]any
		if sessionSvc != nil {
			session, _ := sessionSvc.FindActive(ctx, research.ID)
			if session != nil {
				activeSession = map[string]any{
					"id":     session.ID,
					"title":  session.Title,
					"focus":  session.Focus,
					"status": session.Status,
				}
			}
		}

		// The skills index rides in this call because it is the one the
		// conductor always makes. A skill unreachable from the tool the model
		// actually runs does not exist, whatever the documentation says.
		//
		// Names and trigger lines only — the bodies are what skill_load is
		// for, and putting them here would rebuild the always-loaded field
		// this feature replaced.
		var skillIndex []map[string]any
		if skillSvc != nil {
			for _, sk := range skillSvc.Index(ctx, research.ID) {
				skillIndex = append(skillIndex, map[string]any{
					"slug":        sk.Slug,
					"name":        sk.Name,
					"tier":        sk.Tier,
					"description": sk.Description,
				})
			}
		}

		out := map[string]any{
			"research":       research,
			"sections":       sectionData,
			"active_session": activeSession,
			"usage_hint":     "Use entry_create with section_id from the sections list above.",
		}
		if len(skillIndex) > 0 {
			out["skills"] = skillIndex
			out["skills_hint"] = "Each skill says when to use it. Call skill_load with its slug when you are about to do that work — one at a time, not up front. This is what the research follows, not everything it could: skill_list shows the rest, and skill_attach or skill_create adds one when the work in front of you has no methodology here."
		}
		return successResult(out)
	})
}
