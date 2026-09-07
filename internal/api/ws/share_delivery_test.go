package ws

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
)

// A share connection is the one reader on the hub with no account. Every rule
// in visible() is written in terms of a user id, so the failure mode to guard
// against is not "a share sees too little" — it is a share falling through into
// a rule that was never meant for it.

func (h *Hub) attachShare(researchID string, include domain.ShareInclude, buf int) *Client {
	c := &Client{
		hub:   h,
		send:  make(chan []byte, buf),
		share: &auth.Share{ID: "share-1", ResearchID: researchID, Include: include},
	}
	h.Register(c)
	return c
}

func TestShareDelivery_OnlyItsOwnResearch(t *testing.T) {
	hub := quietHub()
	hub.SetAuthorizer(fakeAuth{}, true)

	visitor := hub.attachShare("r1", domain.ShareInclude{Roadmaps: true}, 8)

	hub.deliver(Event{Type: "entry.updated", Entity: "entry", EntityID: "e1", ResearchID: "r1"})
	if _, ok := received(t, visitor); !ok {
		t.Error("a share visitor did not get an update to the research they are reading")
	}

	hub.deliver(Event{Type: "entry.updated", Entity: "entry", EntityID: "e9", ResearchID: "r2"})
	if _, ok := received(t, visitor); ok {
		t.Error("a share visitor got an event for a research outside the link")
	}
}

// With auth off every ordinary connection sees everything, which is what that
// mode has always done. A share must not inherit it: the whole point is a
// stranger on a public URL.
func TestShareDelivery_AuthDisabledDoesNotOpenTheStream(t *testing.T) {
	hub := quietHub()
	hub.SetAuthorizer(fakeAuth{}, false)

	visitor := hub.attachShare("r1", domain.ShareInclude{}, 8)
	local := hub.attach("", 8)

	hub.deliver(Event{Type: "entry.updated", Entity: "entry", EntityID: "e9", ResearchID: "r2"})

	if _, ok := received(t, visitor); ok {
		t.Error("with auth off, a share visitor received another research's event")
	}
	if _, ok := received(t, local); !ok {
		t.Error("the local single-user connection stopped seeing its own events")
	}
}

func TestShareDelivery_IncludeFlagsFilterTheStream(t *testing.T) {
	hub := quietHub()
	hub.SetAuthorizer(fakeAuth{}, true)

	// Content only: the link excludes sessions, tasks and roadmaps.
	visitor := hub.attachShare("r1", domain.ShareInclude{}, 16)

	for _, e := range []Event{
		{Type: "session.updated", Entity: "session", EntityID: "s1", ResearchID: "r1"},
		{Type: "question.answered", Entity: "question", EntityID: "q1", ResearchID: "r1"},
		{Type: "task.updated", Entity: "task", EntityID: "t1", ResearchID: "r1"},
		{Type: "roadmap.updated", Entity: "roadmap", EntityID: "rm1", ResearchID: "r1"},
		{Type: "share.created", Entity: "share", EntityID: "sh1", ResearchID: "r1"},
		{Type: "team.updated", Entity: "team", EntityID: "team1"},
		// A mark is one person saying they do not believe a sentence. There is
		// no include flag for it and never will be — and `parent_code` names the
		// document being disputed, so the default `return true` at the bottom of
		// visibleToShare was handing a stranger the shape of the argument.
		{Type: "annotation.created", Entity: "annotation", EntityID: "a1", ResearchID: "r1", ParentID: "e1", ParentCode: "E7"},
		{Type: "annotation.answered", Entity: "annotation", EntityID: "a1", ResearchID: "r1", ParentID: "e1", ParentCode: "E7"},
		{Type: "annotation.deleted", Entity: "annotation", EntityID: "a1", ResearchID: "r1", ParentID: "e1", ParentCode: "E7"},
		// Auth-disabled read receipts have no target user, so their entity is the
		// only signal that they are still private working state.
		{Type: "entry_view.updated", Entity: "entry_view", EntityID: "e1", ResearchID: "r1"},
		{Type: "research.updated", Entity: "memory", EntityID: "private-note-id", ResearchID: "r1"},
	} {
		hub.deliver(e)
		if got, ok := received(t, visitor); ok {
			t.Errorf("a link excluding %s delivered %s: %+v", e.Entity, e.Type, got)
		}
	}

	// And the content it does include still arrives.
	hub.deliver(Event{Type: "entry.created", Entity: "entry", EntityID: "e1", ResearchID: "r1"})
	if _, ok := received(t, visitor); !ok {
		t.Error("the flags filtered out the content the link exists to show")
	}
}

// A section now carries two fields a share never sees — `field_spec` and, since
// #82, `instruction`. The event that announces a change to either still reaches
// a visitor, and that is a decision rather than an oversight: the shared page
// refetches on this event, so suppressing it would leave a renamed or reordered
// section stale on screen.
//
// What must never widen is what the envelope carries. This pins that: the
// visitor is told a section changed and when, and is told nothing about what
// changed in it.
func TestShareDelivery_SectionEventsCarryNoInstruction(t *testing.T) {
	hub := quietHub()
	hub.SetAuthorizer(fakeAuth{}, true)

	// Nothing switched on — the narrowest link there is.
	visitor := hub.attachShare("r1", domain.ShareInclude{}, 16)

	const secret = "Name the producing service. State the consumer."
	hub.deliver(Event{
		Type: "section.updated", Entity: "section", EntityID: "sec1",
		ResearchID: "r1", ResearchCode: "R1",
	})
	got, ok := received(t, visitor)
	if !ok {
		t.Fatal("a share visitor stopped seeing section changes; the shared page repaints on this event")
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Errorf("a section event carried the instruction itself: %s", encoded)
	}
	// The envelope has no field that could carry it. If one is ever added, this
	// is the line that has to be thought about again.
	for _, field := range []string{"instruction", "field_spec", "description"} {
		if strings.Contains(string(encoded), field) {
			t.Errorf("the section event envelope grew a %q field: %s", field, encoded)
		}
	}
}

func TestShareDelivery_NoDirectedEventsAndNoActorIdentity(t *testing.T) {
	hub := quietHub()
	hub.SetAuthorizer(fakeAuth{}, true)

	visitor := hub.attachShare("r1", domain.ShareInclude{}, 8)
	member := hub.attach("alice", 8)

	// A directed message names a user. A visitor is not one, and the access
	// revocation it carries is not their business.
	hub.deliver(Event{Type: "access.revoked", Entity: "research", ResearchID: "r1", TargetUserID: "alice"})
	if _, ok := received(t, visitor); ok {
		t.Error("a directed event reached a share visitor")
	}
	drain(member)

	hub.deliver(Event{
		Type: "entry.updated", Entity: "entry", EntityID: "e1", ResearchID: "r1",
		ActorUserID: "alice-user-id", ActorClientID: "tab-7",
	})
	got, ok := received(t, visitor)
	if !ok {
		t.Fatal("the visitor got no event at all")
	}
	if got.ActorUserID != "" || got.ActorClientID != "" {
		t.Errorf("the event named an account and a tab inside the owner's organisation: %+v", got)
	}
}

func drain(c *Client) {
	for {
		select {
		case <-c.send:
		default:
			return
		}
	}
}

// narrowingShares answers every re-check with the flags it is told to, which
// is what the validator does after an owner edits the link.
type narrowingShares struct{ share *auth.Share }

func (n narrowingShares) Scope(context.Context, string, string) *auth.Share { return n.share }

// An owner narrowing a live link must narrow its open sockets too. The socket
// filters by the flags it was opened with; the periodic re-check is where the
// new flags have to land.
func TestShareDelivery_ReCheckPicksUpNarrowedFlags(t *testing.T) {
	hub := quietHub()
	hub.SetAuthorizer(fakeAuth{}, true)
	visitor := hub.attachShare("r1", domain.ShareInclude{Tasks: true}, 8)
	visitor.shareToken = "mrs_x"

	hub.deliver(Event{Type: "task.updated", Entity: "task", EntityID: "t1", ResearchID: "r1"})
	if _, ok := received(t, visitor); !ok {
		t.Fatal("a link including tasks did not get a task event")
	}

	// The owner removes tasks from the link.
	hub.SetShareValidator(narrowingShares{share: &auth.Share{ID: "share-1", ResearchID: "r1", Include: domain.ShareInclude{}}})
	if !hub.credentialStillValid(visitor) {
		t.Fatal("a narrowed link is still a live link")
	}
	hub.deliver(Event{Type: "task.updated", Entity: "task", EntityID: "t2", ResearchID: "r1"})
	if got, ok := received(t, visitor); ok {
		t.Errorf("after narrowing, the open socket still delivered a task event: %+v", got)
	}
	hub.deliver(Event{Type: "entry.updated", Entity: "entry", EntityID: "e1", ResearchID: "r1"})
	if _, ok := received(t, visitor); !ok {
		t.Error("narrowing the link stopped the documents it still includes")
	}

	// And a link that now opens onto another research is not renewed.
	hub.SetShareValidator(narrowingShares{share: &auth.Share{ID: "share-1", ResearchID: "r2", Include: domain.ShareInclude{}}})
	if hub.credentialStillValid(visitor) {
		t.Error("a share that resolves to a different research was renewed")
	}
}
