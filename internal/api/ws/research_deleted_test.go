package ws

import "testing"

// TestForgetOn_ResearchDeleted holds the delivery rule the deletion feature
// rests on.
//
// `ResearchService.Delete` emits one plain `research.deleted` and one directed
// event per team member, on the understanding that with accounts on the plain
// one is refused — the visibility check asks whether the caller may read a
// research that is no longer there. That is only true if the check is made, and
// the verdict cache is what stops it being made: a `true` cached from any event
// in the previous minute stands, the plain event is delivered too, and every
// member is told twice. The notice it raises is a toast with no timeout, so
// twice is not a cosmetic problem.
func TestForgetOn_ResearchDeleted(t *testing.T) {
	if !forgetOn(Event{Type: "research.deleted", Entity: "research", ResearchID: "r1"}) {
		t.Error("research.deleted must flush the verdict cache: without it a stale allow delivers the plain event alongside the directed one")
	}
	// The neighbours, so a future edit cannot narrow this to nothing.
	for _, e := range []Event{
		{Type: "access.revoked", Entity: "research"},
		{Type: "research.transferred", Entity: "research"},
		{Type: "team.member_added", Entity: "team"},
	} {
		if !forgetOn(e) {
			t.Errorf("%s must flush the cache", e.Type)
		}
	}
	// And something that must not, or the cache buys nothing.
	if forgetOn(Event{Type: "entry.updated", Entity: "entry", ResearchID: "r1"}) {
		t.Error("entry.updated must not flush the cache — it changes content, not who may read it")
	}
}
