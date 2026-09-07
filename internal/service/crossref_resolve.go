package service

import (
	"context"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
)

// A reference written before its target exists is the normal order, not an edge
// case: an agent says "this rests on [[E20]]" and writes E20 next. Until this
// existed, that reference was stored `resolved: false` and stayed there — the
// graph and the mind map drew no edge, while the prose rendered the link as
// live, so the document said two things were connected and the graph said they
// were not. The only cure was POST /api/researches/{id}/crossrefs/rebuild,
// which had no button and no tool.
//
// So every path that brings a referenceable entity into existence calls back
// here with the code it was given. The repair is a single UPDATE over rows that
// are still unresolved and name exactly that code — not a rescan of the
// research, because this runs on the hot path of every create.
//
// What it deliberately does not do is re-point an already-resolved row. A code
// is reused after a delete, and quietly moving a link somebody has already
// followed is worse than leaving one dangling.

// DanglingResolver is the half of EntryService the other services need: they
// create the things references point at, and it owns the reference table.
//
// A separate interface from CrossRefParser because the two are asked for by
// different callers for different reasons — a task carries text that may cite
// something (parser), and a task is also itself a thing that may be cited
// (resolver). RoadmapService needs only the second.
type DanglingResolver interface {
	ResolveDanglingEntry(ctx context.Context, researchID, entryCode, entryID string)
	ResolveDanglingTask(ctx context.Context, researchID, taskCode string)
	ResolveDanglingRoadmap(ctx context.Context, researchID, roadmapCode, roadmapID string)
	ResolveDanglingNode(ctx context.Context, researchID, roadmapCode, nodeCode, roadmapID, nodeID string)
	ResolveDanglingResearch(ctx context.Context, researchID, researchCode string)
}

// ResolveDanglingEntry repairs references to an entry that has just been given
// its code.
//
// Two shapes name one entry. `[[E20]]` is only meaningful inside the entry's
// own research, because entry codes repeat across researches — matching it
// globally would point one team's reference at another team's document.
// `[[R3:E20]]` carries the research code and may be written from anywhere, so
// it is matched everywhere.
func (s *EntryService) ResolveDanglingEntry(ctx context.Context, researchID, entryCode, entryID string) {
	if entryCode == "" || entryID == "" {
		return
	}
	matches := []storage.DanglingMatch{{Ref: entryCode, SourceResearchID: researchID}}
	if code := s.researchCode(ctx, researchID); code != "" {
		// Qualified by a research code, which is global — so this form may be
		// written from anywhere and is matched everywhere.
		matches = append(matches, storage.DanglingMatch{Ref: code + ":" + entryCode, Global: true})
	}
	s.resolveDangling(ctx, researchID, matches, domain.CrossRef{
		TargetEntryID:    entryID,
		TargetResearchID: researchID,
	})
}

// ResolveDanglingTask repairs `[[T4]]`.
//
// A task has no page of its own, so there is no id to store: `resolved` means
// "this research really has a T4", exactly as the resolver records it on the
// write path. There is no cross-research form of a task reference, so this
// never reaches outside the research.
func (s *EntryService) ResolveDanglingTask(ctx context.Context, researchID, taskCode string) {
	if taskCode == "" {
		return
	}
	s.resolveDangling(ctx, researchID,
		[]storage.DanglingMatch{{Ref: taskCode, SourceResearchID: researchID}},
		domain.CrossRef{TargetResearchID: researchID})
}

// ResolveDanglingRoadmap repairs `[[RM1]]`.
//
// Scoped to the research, because roadmap codes are allocated per research
// (`NextCode(..., "research_id", ...)`) — every research that has a roadmap has
// an RM1. The first version of this matched globally on the argument that it
// was "matching the resolver", which reads globally. That was wrong twice over:
// the resolver's global read is itself the bug, and a global *write* rewrites
// rows belonging to researches the caller has no role on. Worse, it was
// unrecoverable — the `resolved=0` guard then refuses to repair the row when
// the research's own RM1 finally appears.
func (s *EntryService) ResolveDanglingRoadmap(ctx context.Context, researchID, roadmapCode, roadmapID string) {
	if roadmapCode == "" || roadmapID == "" {
		return
	}
	s.resolveDangling(ctx, researchID,
		[]storage.DanglingMatch{{Ref: roadmapCode, SourceResearchID: researchID}},
		domain.CrossRef{TargetRoadmapID: roadmapID, TargetResearchID: researchID})
}

// ResolveDanglingNode repairs `[[RM1:N3]]`. Scoped for the same reason as the
// roadmap it hangs off: the RM code is per-research, so the pair is too.
func (s *EntryService) ResolveDanglingNode(ctx context.Context, researchID, roadmapCode, nodeCode, roadmapID, nodeID string) {
	if roadmapCode == "" || nodeCode == "" || nodeID == "" {
		return
	}
	s.resolveDangling(ctx, researchID,
		[]storage.DanglingMatch{{Ref: roadmapCode + ":" + nodeCode, SourceResearchID: researchID}},
		domain.CrossRef{TargetRoadmapID: roadmapID, TargetNodeID: nodeID, TargetResearchID: researchID})
}

// ResolveDanglingResearch repairs `[[R2]]`.
//
// It does not touch `[[R2:E5]]`. Those need the entry as well, and they are
// repaired by ResolveDanglingEntry when E5 is created — which is the only
// moment they become true. Half-resolving them here would set a target research
// on a row that still points at nothing.
func (s *EntryService) ResolveDanglingResearch(ctx context.Context, researchID, researchCode string) {
	if researchCode == "" {
		return
	}
	// Research codes really are global.
	s.resolveDangling(ctx, researchID,
		[]storage.DanglingMatch{{Ref: researchCode, Global: true}},
		domain.CrossRef{TargetResearchID: researchID})
}

// resolveDangling runs the repair and announces it.
//
// The event carries the research the *target* lives in, which is not
// necessarily where the repaired references were written: a `[[R3:E20]]` in
// research R1 is repaired when E20 appears in R3. Sources elsewhere therefore
// do not repaint until they are next loaded. That is the honest limit of one
// event with one research id, and it is the uncommon direction — the ordinary
// forward reference is written and repaired inside one research.
func (s *EntryService) resolveDangling(ctx context.Context, researchID string, matches []storage.DanglingMatch, target domain.CrossRef) {
	if s.crossrefs == nil || len(matches) == 0 {
		return
	}
	n, err := s.crossrefs.ResolveDangling(ctx, matches, target)
	if err != nil {
		// Never fatal to the create that triggered it. The entity exists and is
		// correct; its inbound links are stale, which is what the rebuild
		// button is for.
		s.log.Error("failed to resolve dangling crossrefs", "research_id", researchID, "error", err)
		return
	}
	if n == 0 {
		return
	}
	// Same entity as the rebuild emits, so the pages already listening for
	// "the link table moved" need no new case.
	emit(ctx, s.events, Event{
		Type: "crossrefs.resolved", ResearchID: researchID, EntityID: researchID, Entity: "crossref",
	})
}

// resolveResearchID turns an R code into a research id, and leaves a uuid
// alone. A failure returns the input unchanged so the caller's access check
// produces the refusal rather than this inventing one.
func (s *EntryService) resolveResearchID(ctx context.Context, idOrCode string) string {
	if s.researches == nil || idOrCode == "" || !isCode(idOrCode) {
		return idOrCode
	}
	r, err := s.researches.FindByCode(ctx, idOrCode)
	if err != nil || r == nil {
		return idOrCode
	}
	return r.ID
}

// researchCode looks up a research's short code so the cross-research form of a
// reference can be matched. Empty on any failure: the same-research match still
// stands, so a lookup that fails costs the rarer half of the repair rather than
// the whole create.
func (s *EntryService) researchCode(ctx context.Context, researchID string) string {
	if s.researches == nil || researchID == "" {
		return ""
	}
	r, err := s.researches.FindByID(ctx, researchID)
	if err != nil || r == nil {
		return ""
	}
	return r.Code
}
