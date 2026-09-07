package service

import (
	"context"

	"github.com/dovod-app/app/internal/auth"
)

// EventNotifier is called after data mutations to notify listeners.
type EventNotifier interface {
	Notify(event Event)
}

type Event struct {
	Type       string // e.g. "research.created", "entry.updated"
	ResearchID string
	EntityID   string
	Entity     string // "research", "section", "entry", "entry_view", "session", "question", "task", "team", "annotation"

	// ParentID and ParentCode name the thing the entity hangs off, for the
	// entities that are not addressable on their own. An annotation event
	// carries the annotation's id in EntityID, which tells the open document
	// page nothing — it needs to know whether the mark is one of ITS marks, and
	// the only answer is the entry. Same lesson as ResearchCode: a page routed
	// by short code cannot compare against a UUID, so both travel.
	ParentID   string
	ParentCode string

	// ActorUserID is who caused the change. Empty when auth is off, and for
	// anything an agent did over stdio.
	ActorUserID string

	// ActorClientID is which *tab* caused it, and it is the field that actually
	// answers "was this me". The user id cannot: with auth off it is empty, so
	// a browser write and an agent write look identical; and with auth on two
	// tabs of one person share it, so one would suppress a change the other
	// made and must display. A client sends its own id with each write and
	// compares it to what comes back.
	ActorClientID string

	// Reason says why, for the events where the type alone does not — chiefly
	// access.revoked, where the difference between "you were removed from the
	// team" and "the research was moved" is the whole message.
	Reason string

	// Name is the human name of the entity in EntityID, carried only where the
	// recipient cannot look it up: by the time somebody is told they lost
	// access, fetching the thing they lost returns 404.
	Name string

	// TargetUserID addresses the event to one person instead of to everyone who
	// can read the research. It covers personal state such as read receipts, and
	// messages that the normal scope cannot deliver, such as telling somebody
	// they have just lost access.
	TargetUserID string

	// ResearchCode is normally left empty: the hub resolves it from ResearchID,
	// so no call site has to carry it. Set it when the research will not be
	// there to look up by the time the hub asks — which is exactly the deletion
	// event, and which matters because every link in the web UI is built from
	// the code, so a list that only knows `R7` cannot act on an event that only
	// says `9f3c…`.
	ResearchCode string
}

// emit stamps the event with who caused it and hands it on.
//
// The actor is read here rather than at each call site because a call site that
// forgets it degrades silently — the client falls back to treating a remote
// change as its own and drops the refetch.
func emit(ctx context.Context, n EventNotifier, event Event) {
	if n == nil {
		return
	}
	if event.ActorUserID == "" {
		event.ActorUserID = auth.UserIDFromContext(ctx)
	}
	if event.ActorClientID == "" {
		event.ActorClientID = ClientIDFromContext(ctx)
	}
	n.Notify(event)
}

type clientIDKey struct{}

// WithClientID records which browser tab is making this request. It is set from
// the X-Client-Id header on writes and is opaque to the server — it exists only
// so the tab can recognise its own change coming back.
func WithClientID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	if len(id) > 64 {
		id = id[:64]
	}
	return context.WithValue(ctx, clientIDKey{}, id)
}

// ClientIDFromContext returns the calling tab's id, or "" for an agent, a
// script, or a browser too old to send one.
func ClientIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(clientIDKey{}).(string); ok {
		return id
	}
	return ""
}

// NoopNotifier does nothing (default when no WebSocket hub is connected).
type NoopNotifier struct{}

func (NoopNotifier) Notify(Event) {}
