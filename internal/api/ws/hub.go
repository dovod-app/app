package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dovod-app/app/internal/auth"
)

const (
	// broadcastQueue is deep enough that a burst of writes never reaches the
	// caller. It is drained by one goroutine, so events keep their order.
	broadcastQueue = 1024

	// verdictTTL is a backstop, not the mechanism. Membership changes flush the
	// cache outright (see forgetOn); the expiry only bounds how long a verdict
	// could survive a change that somehow arrived without an event.
	verdictTTL = time.Minute

	// verdictCacheLimit bounds the memoized verdicts. It is a ceiling, not a
	// working set: the live set is connections times researches touched.
	verdictCacheLimit = 8192
)

// Event is broadcast to the WebSocket clients allowed to see it.
type Event struct {
	Type       string `json:"type"`        // e.g. "research.updated", "entry.created"
	ResearchID string `json:"research_id"` // scope
	EntityID   string `json:"entity_id"`   // ID of the changed entity
	Entity     string `json:"entity"`      // "research", "section", "entry", "session", "question", "task", "team"

	// ResearchCode is the same scope said the other way. Every link in the web
	// UI is built from the short code, so a page routed as /research/R7 has no
	// UUID to compare against and dropped every event it was sent. Carrying
	// both identities is what stops that from being rediscovered per page.
	ResearchCode string `json:"research_code,omitempty"`

	// ParentID and ParentCode name the entity this one hangs off — the entry an
	// annotation is attached to. Without them an open document page cannot tell
	// whether an annotation event concerns the document it is showing.
	ParentID   string `json:"parent_id,omitempty"`
	ParentCode string `json:"parent_code,omitempty"`

	// ActorUserID is who caused the change; ActorClientID is which tab did. The
	// second is the one a client compares against its own, because the first is
	// empty with auth off and shared between a person's own tabs.
	ActorUserID   string `json:"actor_user_id,omitempty"`
	ActorClientID string `json:"actor_client_id,omitempty"`

	// Reason and Name carry what the recipient of a revocation can no longer
	// look up for themselves.
	Reason string `json:"reason,omitempty"`
	Name   string `json:"name,omitempty"`

	// At is when the server sent it. Receipt time is not a substitute: a tab
	// waking from sleep takes a queued backlog all at once and would date every
	// one of them to the moment it woke.
	At int64 `json:"at,omitempty"`

	// TargetUserID addresses one person directly and never reaches the wire —
	// a recipient learns nothing from it that the delivery itself did not
	// already tell them.
	TargetUserID string `json:"-"`
}

// Authorizer answers, for one connected reader, whether an event is theirs to
// see. It is the same question every HTTP read asks, asked again at send time
// rather than at subscribe time — which is what makes a removed member stop
// receiving updates on the connection they already have open.
type Authorizer interface {
	// CanReadResearch reports whether the user may read the research.
	CanReadResearch(ctx context.Context, userID, researchID string) bool
	// IsTeamMember reports whether the user belongs to the team.
	IsTeamMember(ctx context.Context, userID, teamID string) bool
}

// Hub manages WebSocket connections and delivers events to the clients
// entitled to them.
//
// Delivery is decided per client per event. The alternative — deciding once,
// when a client subscribes — is what makes a revoked membership keep receiving
// updates until the tab is closed, and "removing a member revokes access
// immediately" is the property this whole feature rests on.
//
// The deciding happens on the hub's own goroutine rather than the caller's.
// Each verdict is a database read, the database allows one connection at a
// time, and Broadcast is called from the middle of a write handler — so doing
// this inline charged every writer for every connected reader, and put a
// deadlock one misplaced call inside a transaction away.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
	auth    Authorizer
	// authEnabled mirrors the server's configuration. With auth off there are
	// no users to scope by and everything is one local team, so every client
	// sees everything — which is the behaviour that mode has always had.
	authEnabled bool
	log         *slog.Logger

	validator TokenValidator
	shares    ShareValidator
	codes     CodeLookup
	queue     chan Event
	verdicts  *verdictCache
}

// ShareValidator resolves a share token to the capability it carries, or nil.
//
// The hub holds one for the same reason it holds a TokenValidator: a link can
// be revoked or expire while the socket it opened is still connected, and
// "revoking a share takes effect immediately" has to mean the open sockets too.
type ShareValidator interface {
	Scope(ctx context.Context, token, unlock string) *auth.Share
}

// CodeLookup resolves a research id to its short code. The hub asks for it on
// its own goroutine, so a cache miss costs the reader a moment rather than
// charging the writer that produced the event.
type CodeLookup interface {
	Code(ctx context.Context, researchID string) string
}

func NewHub(log *slog.Logger) *Hub {
	h := &Hub{
		clients:  make(map[*Client]struct{}),
		log:      log,
		queue:    make(chan Event, broadcastQueue),
		verdicts: newVerdictCache(),
	}
	go h.run()
	return h
}

// SetAuthorizer turns on per-client scoping. It is called after construction
// because the services the authorizer reads are wired later; until it is, the
// hub refuses to deliver anything to an identified client rather than
// defaulting to delivering everything.
func (h *Hub) SetAuthorizer(auth Authorizer, authEnabled bool) {
	h.mu.Lock()
	h.auth = auth
	h.authEnabled = authEnabled
	h.mu.Unlock()
}

// SetTokenValidator gives the hub the credential check used at the handshake
// and again, periodically, for the life of each connection.
func (h *Hub) SetTokenValidator(v TokenValidator) {
	h.mu.Lock()
	h.validator = v
	h.mu.Unlock()
}

// TokenValidator returns the configured validator, or nil.
func (h *Hub) TokenValidator() TokenValidator {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.validator
}

// SetShareValidator lets share links open connections. Until one is set, the
// handshake refuses every share token rather than falling back to letting it
// through unscoped.
func (h *Hub) SetShareValidator(v ShareValidator) {
	h.mu.Lock()
	h.shares = v
	h.mu.Unlock()
}

// ShareValidator returns the configured share validator, or nil.
func (h *Hub) ShareValidator() ShareValidator {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.shares
}

// credentialStillValid re-checks the token a connection was opened with, and
// that it still belongs to the same person. A token that now resolves to
// somebody else is not a renewal — it is a connection that should not survive.
func (h *Hub) credentialStillValid(c *Client) bool {
	h.mu.RLock()
	validator, authEnabled, shares := h.validator, h.authEnabled, h.shares
	h.mu.RUnlock()

	// A share connection is re-checked whether or not auth is enabled: the link
	// is the credential, and it can be revoked in either mode.
	if c.share != nil {
		if shares == nil {
			return false
		}
		scope := shares.Scope(context.Background(), c.shareToken, c.shareUnlock)
		// A link that now opens onto a different research is not a renewal. It
		// cannot happen today — a share's research never changes — but the
		// connection was authorized for one id and must not silently follow it.
		if scope == nil || scope.ResearchID != c.share.ResearchID {
			return false
		}
		// The link's include flags can change while the socket is open — an
		// owner narrowing what it shows. The socket filters by the flags it was
		// opened with, so without this a visitor whose link no longer includes
		// tasks kept hearing about them until they reconnected. Written under
		// the hub lock, because deliver reads it under the same lock.
		h.mu.Lock()
		c.share = scope
		h.mu.Unlock()
		return true
	}

	if !authEnabled {
		return true
	}
	if validator == nil || c.token == "" {
		return false
	}
	userID, decided := validator.ValidateCredential(context.Background(), c.token)
	if !decided {
		// The lookup itself failed — a busy database, a shutdown race. Closing
		// on that would tell somebody still signed in that their session ended,
		// and the client treats that verdict as terminal and stops reconnecting.
		return true
	}
	return userID == c.userID
}

// SetCodeLookup gives the hub a way to name a research the way the URLs do.
// Without one, events still carry the id and nothing breaks — the client is
// simply left with one way to match instead of two.
func (h *Hub) SetCodeLookup(codes CodeLookup) {
	h.mu.Lock()
	h.codes = codes
	h.mu.Unlock()
}

// AuthEnabled reports whether connections must carry a token.
func (h *Hub) AuthEnabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.authEnabled
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	n := len(h.clients)
	h.mu.Unlock()
	h.log.Debug("ws client connected", "clients", n)
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	n := len(h.clients)
	h.mu.Unlock()
	h.log.Debug("ws client disconnected", "clients", n)
}

// Broadcast queues an event for delivery and returns immediately.
func (h *Hub) Broadcast(event Event) {
	// Before the queue, not after it. Delivery is best-effort and a full queue
	// drops events; if the flush travelled with them, dropping one membership
	// change would leave a cached "may read" standing for a full TTL after the
	// access it describes was taken away. Losing an update is a stale page.
	// Losing this is a stale permission.
	if forgetOn(event) {
		h.verdicts.flush()
	}

	select {
	case h.queue <- event:
	default:
		// Dropping is the least bad option left: blocking here stalls the write
		// that produced the event, on the goroutine holding a database
		// connection. It is logged because a client that misses this has no
		// other way to find out.
		h.log.Warn("ws broadcast queue full, event dropped",
			"type", event.Type, "entity", event.Entity, "entity_id", event.EntityID)
	}
}

// run drains the queue on one goroutine, and only one.
//
// That is not just simplicity. A verdict read before a membership change must
// not be written back after the flush that change triggered — with a single
// drainer the two cannot interleave, and a worker pool here would reintroduce
// exactly that race, invisibly.
func (h *Hub) run() {
	for event := range h.queue {
		h.deliver(event)
	}
}

func (h *Hub) deliver(event Event) {
	h.mu.RLock()
	authz, authEnabled, codes := h.auth, h.authEnabled, h.codes
	clients := make([]*Client, 0, len(h.clients))
	// The share capability is snapshotted here, under the lock, because the
	// re-check on the client's own goroutine replaces it when the link's flags
	// change.
	shares := make(map[*Client]*auth.Share, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
		shares[c] = c.share
	}
	h.mu.RUnlock()

	ctx := context.Background()
	if codes != nil && event.ResearchID != "" && event.ResearchCode == "" {
		event.ResearchCode = codes.Code(ctx, event.ResearchID)
	}

	data, err := json.Marshal(event)
	if err != nil {
		h.log.Error("ws event marshal failed", "type", event.Type, "error", err)
		return
	}

	// A share visitor gets a different payload, not merely a different
	// selection. The actor fields name a user account and a browser tab inside
	// the owner's organisation; they exist so a member's own tab can recognise
	// its own change, and there is no such tab on the other side of a link.
	// Marshalled once and only if somebody needs it.
	var shareData []byte

	for _, c := range clients {
		payload := data
		if share := shares[c]; share != nil {
			if !visibleToShare(share, event) {
				continue
			}
			if shareData == nil {
				scrubbed := event
				scrubbed.ActorUserID = ""
				scrubbed.ActorClientID = ""
				b, err := json.Marshal(scrubbed)
				if err != nil {
					continue
				}
				shareData = b
			}
			payload = shareData
		} else if !h.visibleTo(ctx, authz, authEnabled, c.userID, event) {
			continue
		}
		select {
		case c.send <- payload:
		default:
			// Client buffer full, skip
		}
	}
}

// visibleTo is visible() with the cache in front of it. The cache only ever
// covers the research check: team membership and directed delivery are cheap
// and rare enough that a stale answer would buy nothing.
func (h *Hub) visibleTo(ctx context.Context, authz Authorizer, authEnabled bool, userID string, event Event) bool {
	if !authEnabled || userID == "" || authz == nil ||
		event.TargetUserID != "" || event.Entity == "team" || event.ResearchID == "" {
		return visible(ctx, authz, authEnabled, userID, event)
	}
	if allowed, ok := h.verdicts.get(userID, event.ResearchID); ok {
		return allowed
	}
	allowed := authz.CanReadResearch(ctx, userID, event.ResearchID)
	h.verdicts.put(userID, event.ResearchID, allowed)
	return allowed
}

// visibleToShare is the delivery rule for a connection opened from a share
// link. It is separate from visible() rather than a branch inside it, because
// every clause in that function is written in terms of a user and a share
// visitor is not one — the "auth disabled means everyone sees everything" rule
// in particular would hand a stranger the whole event stream.
//
// The rule itself is short: one research, and only the parts of it the link
// includes. A `task.updated` on a link that excludes tasks is not harmless
// noise — it says a task exists and when somebody touched it.
func visibleToShare(share *auth.Share, event Event) bool {
	// Directed events address a user id. A share visitor has none, so none of
	// them are ever theirs, including the access-revoked message.
	if event.TargetUserID != "" {
		return false
	}
	if event.Entity == "team" || event.ResearchID == "" || event.ResearchID != share.ResearchID {
		return false
	}
	switch event.Entity {
	case "session", "question":
		return share.Include.Sessions
	case "task":
		return share.Include.Tasks
	case "roadmap", "roadmap_node":
		return share.Include.Roadmaps
	case "share":
		// A visitor does not need to be told that another link was made, and
		// being told their own was revoked is what the closed socket says.
		return false
	case "template":
		// Templates emit nothing today. The case is here anyway, because the
		// default below is `return true` and the day somebody adds a
		// template.* event carrying a research id, this file is not where they
		// will think to look.
		return false
	case "section":
		// Yes, and deliberately, though a section now carries two fields a share
		// never sees: `field_spec` and `instruction`. The event body carries
		// neither — this struct has no data field, so what crosses is "a section
		// changed, and when".
		//
		// It is here as a case rather than falling through the default because
		// the trade is a decision, not an omission. A visitor must see a rename,
		// a status change and a reorder: the shared page refetches on exactly
		// this event, and suppressing it would leave a stale section list on
		// screen. The cost is that an edit touching only the instruction still
		// rings their socket. Narrowing that needs the event to say which facet
		// moved, which nothing in this envelope can express today.
		return true
	case "skill", "memory":
		// Never. Which methodology a team follows is working process — the same
		// class as `instruction` and `memory`, which no share has ever carried.
		// The HTTP surface refuses skills/memory and the service refuses a share
		// context; without this line the socket announced every change anyway,
		// telling a stranger that the set changed, when, and with which ids.
		return false
	case "annotation":
		// Never, and there is no include flag that could turn it on. A mark is
		// one person saying they do not believe a sentence — working process of
		// the same class as revision history. The sub-mux carries no annotation
		// route, but the socket has its own default, and without this line a
		// visitor to a link created with nothing switched on was told which
		// document is being disputed (`parent_code`), how many times, and when
		// the agent answered.
		return false
	case "entry_view":
		// Personal read receipts are never part of a shared document. In local
		// mode they have no TargetUserID, so this explicit refusal is what keeps
		// them off share sockets.
		return false
	}
	return true
}

// visible is the delivery rule, kept in one place so a new event type cannot
// be added without deciding who it is for.
func visible(ctx context.Context, authz Authorizer, authEnabled bool, userID string, event Event) bool {
	// A directed event is checked before anything else, because the reason it
	// exists is to reach someone the ordinary rule would now refuse: the
	// message saying they have lost access. It is addressed by user id, so it
	// can only ever reach the one person it names.
	if event.TargetUserID != "" {
		return userID != "" && userID == event.TargetUserID
	}

	if !authEnabled {
		return true
	}
	// With auth on, an unidentified connection sees nothing. So does every
	// connection if the hub was never given an authorizer — failing closed is
	// the only safe direction for a broadcast.
	if userID == "" || authz == nil {
		return false
	}

	if event.Entity == "team" {
		return authz.IsTeamMember(ctx, userID, event.EntityID)
	}
	if event.ResearchID == "" {
		// An event with no scope cannot be shown to anyone: there is nothing
		// to check it against, and "when in doubt, send it" is how the hub
		// became a public activity feed.
		return false
	}
	return authz.CanReadResearch(ctx, userID, event.ResearchID)
}

// forgetOn reports whether an event could have changed who may read what. Only
// these can: everything else edits content inside a research whose team is
// unchanged.
func forgetOn(event Event) bool {
	return event.Entity == "team" ||
		strings.HasPrefix(event.Type, "team.") ||
		event.Type == "research.transferred" ||
		event.Type == "access.revoked" ||
		event.Type == "access.changed"
}

// verdictCache remembers "may this user read this research" for a moment.
//
// Without it, one entry update with twenty readers connected is twenty
// serialized queries against a database that permits one connection. The
// answers are dropped wholesale the instant anything touches a membership, so
// the cache cannot be the reason someone keeps seeing what they lost.
type verdictCache struct {
	mu      sync.RWMutex
	entries map[string]verdict
}

type verdict struct {
	allowed bool
	expires time.Time
}

func newVerdictCache() *verdictCache {
	return &verdictCache{entries: make(map[string]verdict)}
}

func (c *verdictCache) get(userID, researchID string) (bool, bool) {
	c.mu.RLock()
	v, ok := c.entries[userID+"\x00"+researchID]
	c.mu.RUnlock()
	if !ok || time.Now().After(v.expires) {
		return false, false
	}
	return v.allowed, true
}

func (c *verdictCache) put(userID, researchID string, allowed bool) {
	c.mu.Lock()
	// Negative verdicts are cached too, so this grows with connections times
	// researches and nothing but a membership change shrinks it. Dropping the
	// lot is always safe — it costs one lookup per live pair.
	if len(c.entries) >= verdictCacheLimit {
		c.entries = make(map[string]verdict, verdictCacheLimit)
	}
	c.entries[userID+"\x00"+researchID] = verdict{allowed: allowed, expires: time.Now().Add(verdictTTL)}
	c.mu.Unlock()
}

func (c *verdictCache) flush() {
	c.mu.Lock()
	c.entries = make(map[string]verdict)
	c.mu.Unlock()
}
