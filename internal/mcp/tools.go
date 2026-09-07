package mcp

import (
	"github.com/dovod-app/app/internal/mcp/tools"
)

func (s *Server) registerTools() {
	// Research tools
	tools.RegisterResearchCreate(s.server, s.research, s.template, s.log)
	tools.RegisterResearchGet(s.server, s.research, s.section, s.session, s.skill, s.log)
	tools.RegisterResearchList(s.server, s.research, s.log)
	tools.RegisterResearchUpdate(s.server, s.research, s.log)
	tools.RegisterResearchDeletePreview(s.server, s.research, s.log)
	tools.RegisterResearchDelete(s.server, s.research, s.log)
	tools.RegisterResearchMemory(s.server, s.research)
	tools.RegisterResearchAddSection(s.server, s.research, s.log)
	// Read-only, and next to research_get on purpose: one carries the
	// constraints a research works under, the other the work still open in it.
	tools.RegisterResearchResume(s.server, s.resume, s.log)

	// Section tools
	tools.RegisterSectionList(s.server, s.section, s.log)
	tools.RegisterSectionUpdate(s.server, s.section, s.log)
	tools.RegisterSectionDelete(s.server, s.section, s.log)

	// Entry tools
	tools.RegisterEntryCreate(s.server, s.entry, s.log)
	tools.RegisterEntryList(s.server, s.entry, s.log)
	tools.RegisterEntryRead(s.server, s.entry, s.log)
	tools.RegisterEntryPatch(s.server, s.entry, s.log)
	tools.RegisterEntryHistory(s.server, s.entry, s.log)
	// The repair had a REST route, no button and no tool, so the one caller who
	// could have run it — the agent that wrote the references — could not.
	tools.RegisterCrossRefRebuild(s.server, s.entry, s.log)
	tools.RegisterEntryDiff(s.server, s.entry, s.log)
	tools.RegisterEntryUpdate(s.server, s.entry, s.log)
	tools.RegisterEntryDelete(s.server, s.entry, s.log)

	// Session tools
	tools.RegisterSessionCreate(s.server, s.session, s.log)
	tools.RegisterSessionGet(s.server, s.session, s.log)
	tools.RegisterSessionUpdate(s.server, s.session, s.log)
	tools.RegisterSessionDelete(s.server, s.session, s.log)

	// Question tools
	tools.RegisterQuestionCreate(s.server, s.session, s.log)
	tools.RegisterQuestionUpdate(s.server, s.session, s.log)
	tools.RegisterQuestionDelete(s.server, s.session, s.log)
	tools.RegisterQuestionList(s.server, s.session, s.log)

	// Task tools
	tools.RegisterTaskCreate(s.server, s.task, s.log)
	tools.RegisterTaskUpdate(s.server, s.task, s.log)
	tools.RegisterTaskList(s.server, s.task, s.log)
	tools.RegisterTaskDelete(s.server, s.task, s.log)

	// Annotation tools — read and answer only. Creating one is a person's
	// gesture, made in the web UI; see AnnotationService.Create.
	tools.RegisterAnnotationList(s.server, s.annotation, s.log)
	tools.RegisterAnnotationAnswer(s.server, s.annotation, s.log)

	// Roadmap tools
	tools.RegisterRoadmapCreate(s.server, s.roadmap, s.log)
	tools.RegisterRoadmapGet(s.server, s.roadmap, s.log)
	tools.RegisterRoadmapList(s.server, s.roadmap, s.log)
	tools.RegisterRoadmapUpdate(s.server, s.roadmap, s.log)
	tools.RegisterRoadmapDelete(s.server, s.roadmap, s.log)
	tools.RegisterRoadmapAddNodes(s.server, s.roadmap, s.log)
	tools.RegisterRoadmapUpdateNode(s.server, s.roadmap, s.log)
	tools.RegisterRoadmapRemoveNodes(s.server, s.roadmap, s.log)

	// Template tools
	tools.RegisterTemplateList(s.server, s.template, s.log)
	tools.RegisterTemplateGet(s.server, s.template, s.log)

	// Skill tools
	tools.RegisterSkillLoad(s.server, s.skill, s.research, s.log)
	tools.RegisterSkillList(s.server, s.skill, s.research, s.log)
	tools.RegisterSkillAttach(s.server, s.skill, s.research, s.log)
	tools.RegisterSkillDetach(s.server, s.skill, s.research, s.log)
	tools.RegisterSkillCreate(s.server, s.skill, s.research, s.log)
	tools.RegisterSkillUpdate(s.server, s.skill, s.research, s.log)
	tools.RegisterSkillFork(s.server, s.skill, s.research, s.log)
	tools.RegisterSkillCopy(s.server, s.skill, s.research, s.log)
	tools.RegisterSkillPromote(s.server, s.skill, s.research, s.log)
	tools.RegisterSkillDelete(s.server, s.skill, s.research, s.log)

	// Team tools
	tools.RegisterTeamList(s.server, s.team, s.log)

	// Import/Export tools
	tools.RegisterResearchExport(s.server, s.export, s.research, func() string { return s.baseURL }, s.log)
	tools.RegisterResearchImport(s.server, s.export, s.log)
}
