package service

import (
	"context"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
)

// VisibleCrossRefs returns the references with everything the reader may not
// follow stripped out: the target ids blanked and `resolved` set to false, so
// the `[[…]]` renders as plain text rather than as a link into nothing.
//
// This is where cross-research resolution is decided, and it is decided per
// reader rather than per author.
//
// It used to be decided when the reference was written, by refusing to store a
// target the author could not read. That had it backwards in both directions:
// a colleague who *could* open the target still saw plain text, because the
// author's permissions had been frozen into the row; and an author who lost
// access later kept a working link. Storing the resolution and filtering it on
// the way out fixes both, and closes the id-harvesting the old rule existed to
// prevent — writing [[R1:E1]], [[R1:E2]], … now reads back exactly as
// unresolved as it looked going in.
func (a *Access) VisibleCrossRefs(ctx context.Context, refs []domain.CrossRef) []domain.CrossRef {
	mayRead := a.readMemo(ctx)
	share := auth.ShareFromContext(ctx)

	out := make([]domain.CrossRef, 0, len(refs))
	for _, ref := range refs {
		// A reference written BY something this link does not expose is dropped
		// whole, not blanked. An annotation's resolution can name `[[E19]]`, and
		// the row that produces carries the annotation's own id in
		// `source_type`/`source_id` — so a link that shows no annotation route
		// at all was still answering "somebody disputes this document, here is
		// the id of the objection". Blanking the target would not help: the
		// disclosure is the existence of the source. The same is true of a task
		// or an answer under a link that leaves those out.
		if share != nil && !shareableCrossRefSource(ref.SourceType, share.Include) {
			continue
		}
		// An unresolved row says nothing about what it failed to reach — not
		// even whether the research it named exists.
		//
		// It used to carry the target research's id: `[[R2:E50]]` stored R2's
		// uuid the moment R2 resolved, whether or not E50 did. Null-versus-uuid
		// is then a two-state answer to "is there an R2", readable by anyone
		// holding the source, and writing [[R1:E1]], [[R2:E1]], … enumerates
		// another tenant's researches without ever resolving a single link.
		// The write path no longer stores it (see resolveRefs); this is the
		// belt for the rows already in the table.
		if !ref.Resolved {
			blankCrossRefTarget(&ref)
			out = append(out, ref)
			continue
		}
		// A resolved reference INTO a part the link leaves out is admitted as
		// far as its text and no further. `[[T4]]` resolved says "this research
		// really has a T4", which is the fact a link with tasks off exists to
		// withhold; a roadmap or node target carries the roadmap's id outright.
		if share != nil && !shareableCrossRefTarget(ref.TargetRef, share.Include) {
			blankCrossRefTarget(&ref)
			out = append(out, ref)
			continue
		}
		// A reference with no target research is a same-research one, and the
		// reader is already holding the source.
		target := ref.TargetResearchID
		if target == "" {
			target = ref.SourceResearchID
		}
		if mayRead(target) {
			out = append(out, ref)
			continue
		}

		blankCrossRefTarget(&ref)
		out = append(out, ref)
	}
	return out
}

// blankCrossRefTarget strips a row down to the text the reader is already
// holding: the raw `[[…]]` and where it was written. The reference then renders
// as plain text rather than as a link into something the reader may not know
// about.
func blankCrossRefTarget(ref *domain.CrossRef) {
	ref.Resolved = false
	ref.TargetEntryID = ""
	ref.TargetResearchID = ""
	ref.TargetRoadmapID = ""
	ref.TargetNodeID = ""
}

// shareableCrossRefSource says which kinds of source a share link may admit to,
// given what its creator chose to include.
//
// A closed list rather than a deny-list: the next source type added is a
// decision about what a stranger may learn, and defaulting it to "visible" is
// how this leaked in the first place.
//
// The include flags belong here and not only on the routes. `/crossrefs` is
// mounted ungated because documents are always in a link — but the rows it
// serves are written by tasks and by answers too, so a link with tasks off was
// handing over task ids and the codes they cite on a route the flag never
// touched. It also moved: the summary's `total` climbed the second a task was
// created, which is the same "something just happened in there" the WebSocket
// filter withholds. Gated per row, both doors now say the same thing.
func shareableCrossRefSource(sourceType string, inc domain.ShareInclude) bool {
	switch sourceType {
	case "entry", "":
		return true
	case "task":
		return inc.Tasks
	case "question":
		// Questions and answers are the interview, which is what Sessions
		// covers. `[[SS2]]` and `[[Q3]]` as *targets* are not resolvable at all
		// yet (#119); when they become so, they belong in the target rule below.
		return inc.Sessions
	case "roadmap", "roadmap_node":
		return inc.Roadmaps
	}
	return false
}

// shareableCrossRefTarget says whether a link may admit that the thing a
// reference points at exists.
//
// The default is "yes", unlike the source rule, and the difference is not an
// oversight: an unknown source type is a row a stranger should not be holding
// at all, while an unknown target kind is a code the reader is already looking
// at in the prose. Withholding the resolution of something already on the page
// protects nothing.
func shareableCrossRefTarget(targetRef string, inc domain.ShareInclude) bool {
	kind, _, _ := parseRef(targetRef)
	switch kind {
	case "task":
		return inc.Tasks
	case "roadmap", "node":
		return inc.Roadmaps
	}
	return true
}

// VisibleIncomingCrossRefs drops the references *into* something the reader can
// see that come *from* something they cannot.
//
// Blanking is the wrong treatment here, which is why this is a second function
// rather than a flag on the first. An outgoing reference is text the reader is
// already holding — `[[R2:E5]]` is in the entry they are looking at, and
// rendering it inert is the honest answer. An incoming one is nothing they
// possess: the row exists only because somebody else wrote it, and even the
// stripped version announces that an unseen research cites this entry. For a
// share visitor that is the whole shape of the workspace behind the link.
func (a *Access) VisibleIncomingCrossRefs(ctx context.Context, refs []domain.CrossRef) []domain.CrossRef {
	mayRead := a.readMemo(ctx)
	share := auth.ShareFromContext(ctx)

	out := make([]domain.CrossRef, 0, len(refs))
	for _, ref := range refs {
		// Same rule as the outgoing direction, and it bites harder here: an
		// incoming reference from an annotation says a mark on some document
		// points at the one being read, and one from a task says a task exists
		// that cites it. There is no target rule in this direction — the target
		// is the entry the reader asked for.
		if share != nil && !shareableCrossRefSource(ref.SourceType, share.Include) {
			continue
		}
		if ref.SourceResearchID == "" || !mayRead(ref.SourceResearchID) {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// readMemo answers "may this reader open that research" once per research. An
// entry that points at a colleague's research twenty times asks once.
func (a *Access) readMemo(ctx context.Context) func(string) bool {
	readable := make(map[string]bool, 4)
	return func(researchID string) bool {
		if researchID == "" {
			return false
		}
		if seen, ok := readable[researchID]; ok {
			return seen
		}
		ok := a.Read(ctx, researchID) == nil
		readable[researchID] = ok
		return ok
	}
}
