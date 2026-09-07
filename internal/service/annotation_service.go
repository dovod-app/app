package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/google/uuid"
)

type CreateAnnotationRequest struct {
	EntryID string
	BlockID string
	Quote   domain.Quote
	Kind    domain.AnnotationKind
	Body    string
}

type UpdateAnnotationRequest struct {
	Kind   *domain.AnnotationKind
	Body   *string
	Status *domain.AnnotationStatus
	// Reason accompanies a rejection — sending a mark back to `open`. The agent
	// is required to read it before trying again, so an empty one is a wasted
	// round trip rather than a neutral default.
	Reason *string
	TaskID *string
	// BlockID and Quote together re-point a mark a person has repaired by hand:
	// the text drifted, they found where it went, and said so.
	BlockID *string
	Quote   *domain.Quote
}

type AnswerAnnotationRequest struct {
	Resolution string
	TaskID     string
}

type AnnotationService struct {
	annotations *storage.AnnotationRepository
	entries     *storage.EntryRepository
	revisions   *storage.EntryRevisionRepository
	access      *Access
	docs        *EntryService
	crossrefs   CrossRefParser
	events      EventNotifier
	log         *slog.Logger
}

func NewAnnotationService(
	annotations *storage.AnnotationRepository,
	entries *storage.EntryRepository,
	revisions *storage.EntryRevisionRepository,
	access *Access,
	docs *EntryService,
	crossrefs CrossRefParser,
	events EventNotifier,
	log *slog.Logger,
) *AnnotationService {
	return &AnnotationService{
		annotations: annotations, entries: entries, revisions: revisions,
		access: access, docs: docs, crossrefs: crossrefs, events: events, log: log,
	}
}

// Create records a mark on a place in a document.
//
// The author kind is taken from the credential, not from the request: a browser
// session is a person reading, anything else is a machine. Nothing here refuses
// a machine — the REST API is a legitimate client — but there is deliberately no
// MCP tool that reaches this method, because the gesture the feature exists for
// is a person pointing at a sentence, and a queue filled by agents would hide
// whether that gesture ever happened.
func (s *AnnotationService) Create(ctx context.Context, req CreateAnnotationRequest) (*domain.Annotation, error) {
	entry, err := s.entries.FindByID(ctx, req.EntryID)
	if err != nil {
		return nil, fmt.Errorf("find entry: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("entry %s: %w", req.EntryID, ErrNotFound)
	}
	if err := s.access.Write(ctx, entry.ResearchID); err != nil {
		return nil, fmt.Errorf("research %s: %w", entry.ResearchID, err)
	}

	quote := normalizeQuoteInput(req.Quote)
	if quote.Exact == "" {
		return nil, fmt.Errorf("quote is required: %w", ErrValidation)
	}
	if !req.Kind.Valid() {
		return nil, fmt.Errorf("unknown kind %q: %w", req.Kind, ErrValidation)
	}

	// A block id on a markdown entry would be a promise the document cannot
	// keep. Dropping it here rather than at the edge means every entry point
	// gets the same answer.
	blockID := strings.TrimSpace(req.BlockID)
	if entry.Type != domain.EntryBlocks {
		blockID = ""
	}

	a := &domain.Annotation{
		ID:               uuid.New().String(),
		ResearchID:       entry.ResearchID,
		EntryID:          entry.ID,
		BlockID:          blockID,
		Quote:            quote,
		AnchoredRevision: s.currentRevision(ctx, entry.ID),
		Kind:             req.Kind,
		Body:             clampRunes(req.Body, domain.MaxAnnotationBody),
		AuthorKind:       AuthorFromContext(ctx),
		Status:           domain.AnnotationOpen,
	}
	if u := auth.UserFromContext(ctx); u != nil {
		a.UserID = u.ID
	}

	if err := s.annotations.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("create annotation: %w", err)
	}

	s.resolve(ctx, entry, []*domain.Annotation{a})
	emit(ctx, s.events, annotationEvent("annotation.created", a, entry))
	return a, nil
}

func (s *AnnotationService) Get(ctx context.Context, id string) (*domain.Annotation, error) {
	a, err := s.annotations.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find annotation: %w", err)
	}
	if a == nil {
		// Bare, and identical to the refusal a stranger gets for a mark that
		// does exist. Naming the id back is what turns a guess into a
		// confirmation; see annotationRefusal.
		return nil, ErrNotFound
	}
	if err := s.access.Read(ctx, a.ResearchID); err != nil {
		return nil, annotationRefusal(err)
	}
	if entry, err := s.entries.FindByID(ctx, a.EntryID); err == nil && entry != nil {
		s.resolve(ctx, entry, []*domain.Annotation{a})
	}
	return a, nil
}

// annotationRefusal keeps "no such mark" and "not yours" the same sentence.
//
// A mark is addressed by a bare id, with no research in the URL, so the refusal
// is the only thing a caller learns from. Naming the research turned a guessed
// id into a confirmed one — and `Bulk` answers 200 with a per-row message, up
// to sixty at a time, which is the shape that makes an oracle worth using.
// Losing the right to write is still distinguishable, because a member who may
// read but not write already knows the research exists.
func annotationRefusal(err error) error {
	if errors.Is(err, ErrForbidden) {
		return err
	}
	return ErrNotFound
}

// ListByEntry is what the entry page reads: every mark in one document, placed
// against the document as it stands now.
func (s *AnnotationService) ListByEntry(ctx context.Context, entryID string) ([]*domain.Annotation, error) {
	entry, err := s.entries.FindByID(ctx, entryID)
	if err != nil {
		return nil, fmt.Errorf("find entry: %w", err)
	}
	if entry == nil {
		return nil, ErrNotFound
	}
	if err := s.access.Read(ctx, entry.ResearchID); err != nil {
		return nil, annotationRefusal(err)
	}

	anns, err := s.annotations.FindByEntry(ctx, entryID)
	if err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}
	// Filled from the entry in hand rather than by a join, because this read has
	// one. A client cannot tell "markdown document, no blocks to pin to" from
	// "the block is gone" without it, and those are different sentences.
	for _, a := range anns {
		a.EntryCode, a.EntryTitle, a.EntryType = entry.Code, entry.Title, entry.Type
	}
	s.resolve(ctx, entry, anns)
	return anns, nil
}

// ListByResearch is the queue.
//
// Anchors are resolved one document at a time, which is why the repository
// orders by entry: a queue spanning six documents loads six documents, not one
// per mark. It is also what makes match consumption correct — two marks on the
// same repeated sentence must be placed by the same pass over that document.
func (s *AnnotationService) ListByResearch(ctx context.Context, researchID string, filter storage.AnnotationFilter) ([]*domain.Annotation, error) {
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, fmt.Errorf("research %s: %w", researchID, err)
	}
	if filter.Limit > domain.MaxAnnotationBatch {
		filter.Limit = domain.MaxAnnotationBatch
	}

	anns, err := s.annotations.FindByResearch(ctx, researchID, filter)
	if err != nil {
		return nil, fmt.Errorf("list annotations: %w", err)
	}

	byEntry := map[string][]*domain.Annotation{}
	order := make([]string, 0, 4)
	for _, a := range anns {
		if _, seen := byEntry[a.EntryID]; !seen {
			order = append(order, a.EntryID)
		}
		byEntry[a.EntryID] = append(byEntry[a.EntryID], a)
	}
	for _, entryID := range order {
		entry, err := s.entries.FindByID(ctx, entryID)
		if err != nil || entry == nil {
			continue
		}
		s.resolve(ctx, entry, byEntry[entryID])
	}
	return anns, nil
}

// Update is the human path. It is the only way a mark reaches a terminal state.
func (s *AnnotationService) Update(ctx context.Context, id string, req UpdateAnnotationRequest) (*domain.Annotation, error) {
	a, err := s.annotations.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find annotation: %w", err)
	}
	if a == nil {
		// Bare, and identical to the refusal a stranger gets for a mark that
		// does exist. Naming the id back is what turns a guess into a
		// confirmation; see annotationRefusal.
		return nil, ErrNotFound
	}
	if err := s.access.Write(ctx, a.ResearchID); err != nil {
		return nil, annotationRefusal(err)
	}

	if req.Kind != nil {
		if !req.Kind.Valid() {
			return nil, fmt.Errorf("unknown kind %q: %w", *req.Kind, ErrValidation)
		}
		a.Kind = *req.Kind
	}
	if req.Body != nil {
		a.Body = clampRunes(*req.Body, domain.MaxAnnotationBody)
	}
	if req.TaskID != nil {
		a.TaskID = strings.TrimSpace(*req.TaskID)
	}

	// Re-pointing by hand. Both halves are required together: a block id without
	// a quote is an address with nothing to verify it, which is the failure mode
	// the quote exists to prevent.
	if req.Quote != nil {
		q := normalizeQuoteInput(*req.Quote)
		if q.Exact == "" {
			return nil, fmt.Errorf("quote is required: %w", ErrValidation)
		}
		a.Quote = q
		if req.BlockID != nil {
			a.BlockID = strings.TrimSpace(*req.BlockID)
		}
		a.AnchoredRevision = s.currentRevision(ctx, a.EntryID)
	}

	if req.Status != nil {
		if err := s.applyStatus(ctx, a, *req.Status, req.Reason); err != nil {
			return nil, err
		}
	}

	if err := s.annotations.Update(ctx, a); err != nil {
		return nil, fmt.Errorf("update annotation: %w", err)
	}

	entry, _ := s.entries.FindByID(ctx, a.EntryID)
	if entry != nil {
		s.resolve(ctx, entry, []*domain.Annotation{a})
	}
	emit(ctx, s.events, annotationEvent("annotation.updated", a, entry))
	return a, nil
}

// applyStatus holds the one rule this feature would be worthless without.
//
// `closed` and `dismissed` mean a person looked at the work and agreed. An agent
// that can set them is an agent that accepts its own homework, and the product
// becomes a self-confirmation protocol with a queue attached — the exact failure
// the rigor work exists to prevent, arrived at from a different direction.
func (s *AnnotationService) applyStatus(ctx context.Context, a *domain.Annotation, next domain.AnnotationStatus, reason *string) error {
	if !next.Valid() {
		return fmt.Errorf("unknown status %q: %w", next, ErrValidation)
	}
	human := AuthorFromContext(ctx) == domain.AuthorHuman
	if next.Terminal() && !human {
		return fmt.Errorf("only a person may close or dismiss an annotation: %w", ErrForbidden)
	}

	now := time.Now().UTC()
	switch next {
	case domain.AnnotationOpen:
		// Reopening after an answer is a rejection. Counting it is what lets the
		// queue notice a mark nobody can settle and escalate it to a question
		// instead of grinding on it a third time.
		if a.Status == domain.AnnotationAnswered {
			a.Attempts++
			// The reason is recorded BESIDE the answer it refuses, never over
			// it: writing it into Resolution would erase the thing being
			// rejected, leaving the next attempt with neither the answer nor
			// the objection to it.
			rej := domain.Rejection{At: now}
			if reason != nil {
				rej.Reason = clampRunes(strings.TrimSpace(*reason), domain.MaxAnnotationResolution)
			}
			if a.ResolvedRevision != nil {
				rej.Revision = *a.ResolvedRevision
			}
			a.Rejections = append(a.Rejections, rej)
		}
		a.AnsweredAt = nil
		a.ClosedAt = nil
	case domain.AnnotationAnswered:
		t := now
		a.AnsweredAt = &t
		a.ClosedAt = nil
	case domain.AnnotationClosed, domain.AnnotationDismissed:
		t := now
		a.ClosedAt = &t
	}
	a.Status = next
	return nil
}

// Answer is the agent path, and it stops one step short of done on purpose.
func (s *AnnotationService) Answer(ctx context.Context, id string, req AnswerAnnotationRequest) (*domain.Annotation, error) {
	a, err := s.annotations.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find annotation: %w", err)
	}
	if a == nil {
		// Bare, and identical to the refusal a stranger gets for a mark that
		// does exist. Naming the id back is what turns a guess into a
		// confirmation; see annotationRefusal.
		return nil, ErrNotFound
	}
	if err := s.access.Write(ctx, a.ResearchID); err != nil {
		return nil, annotationRefusal(err)
	}
	if strings.TrimSpace(req.Resolution) == "" {
		return nil, fmt.Errorf("resolution is required: %w", ErrValidation)
	}
	if a.Status.Terminal() {
		return nil, fmt.Errorf("annotation %s is already settled: %w", a.Code, ErrConflict)
	}

	a.Resolution = clampRunes(strings.TrimSpace(req.Resolution), domain.MaxAnnotationResolution)
	if req.TaskID != "" {
		a.TaskID = req.TaskID
	}
	rev := s.currentRevision(ctx, a.EntryID)
	a.ResolvedRevision = &rev
	// Which pass settled this. The session is already the container for a run of
	// work, so "what did SS4 actually settle" needs no new entity.
	entry, _ := s.entries.FindByID(ctx, a.EntryID)
	if entry != nil && entry.SessionID != "" {
		a.SessionID = entry.SessionID
	}

	t := time.Now().UTC()
	a.AnsweredAt = &t
	a.Status = domain.AnnotationAnswered

	if err := s.annotations.Update(ctx, a); err != nil {
		return nil, fmt.Errorf("update annotation: %w", err)
	}

	// A resolution naming [[E19]] produces a real link, exactly as a task's
	// description does. This is what makes "the agent wrote a child entry and
	// pointed at it" navigable rather than a sentence somebody has to read.
	s.parseRefs(ctx, a)

	emit(ctx, s.events, annotationEvent("annotation.answered", a, entry))
	return a, nil
}

func (s *AnnotationService) Delete(ctx context.Context, id string) error {
	a, err := s.annotations.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find annotation: %w", err)
	}
	if a == nil {
		return ErrNotFound
	}
	if err := s.access.Write(ctx, a.ResearchID); err != nil {
		return annotationRefusal(err)
	}
	entry, _ := s.entries.FindByID(ctx, a.EntryID)
	// A resolution can cite other work, and those references are stored under
	// source_type "annotation". Nothing removes them with the row — `crossrefs`
	// has no foreign keys — so the cited document went on showing a backlink
	// from a mark that is gone.
	if s.docs != nil {
		s.docs.ClearCrossRefs(ctx, "annotation", id)
	}
	if err := s.annotations.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete annotation: %w", err)
	}
	emit(ctx, s.events, annotationEvent("annotation.deleted", a, entry))
	return nil
}

// Counts backs the badge on the research page.
func (s *AnnotationService) Counts(ctx context.Context, researchID string) (map[domain.AnnotationStatus]int, error) {
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, fmt.Errorf("research %s: %w", researchID, err)
	}
	return s.annotations.CountByStatus(ctx, researchID)
}

// resolve places a set of marks against one document.
func (s *AnnotationService) resolve(ctx context.Context, entry *domain.Entry, anns []*domain.Annotation) {
	if len(anns) == 0 {
		return
	}
	doc, markdown := s.documentFor(ctx, entry)
	ResolveAnchors(anns, doc, markdown)
}

// documentFor returns whichever projection the entry actually has. A blocks
// document that fails to load is treated as absent rather than as empty: the
// difference decides whether every mark in it is reported as orphaned.
func (s *AnnotationService) documentFor(ctx context.Context, entry *domain.Entry) (*domain.BlockDocument, string) {
	if entry.Type == domain.EntryBlocks && s.docs != nil {
		doc, err := s.docs.LoadBlockDocument(ctx, entry)
		if err == nil && doc != nil {
			return doc, ""
		}
		return nil, ""
	}
	return nil, entry.Content
}

func (s *AnnotationService) currentRevision(ctx context.Context, entryID string) int {
	if s.revisions == nil {
		return 0
	}
	rev, err := s.revisions.Latest(ctx, s.revisions.DB(), entryID)
	if err != nil || rev == nil {
		return 0
	}
	return rev.Revision
}

func (s *AnnotationService) parseRefs(ctx context.Context, a *domain.Annotation) {
	if s.crossrefs == nil || a.Resolution == "" {
		return
	}
	s.crossrefs.ParseCrossRefs(ctx, "annotation", a.ID, a.ResearchID, a.Resolution)
}

// annotationEvent names the document as well as the mark.
//
// EntityID is the annotation, which tells an open document page nothing it can
// act on: the question that page asks is "is this one of mine", and only the
// entry answers it. The code travels beside the id for the same reason
// ResearchCode does — a page routed by short code has no UUID to compare.
func annotationEvent(kind string, a *domain.Annotation, entry *domain.Entry) Event {
	ev := Event{
		Type:       kind,
		ResearchID: a.ResearchID,
		EntityID:   a.ID,
		Entity:     "annotation",
		ParentID:   a.EntryID,
		ParentCode: a.EntryCode,
	}
	if ev.ParentCode == "" && entry != nil {
		ev.ParentCode = entry.Code
	}
	return ev
}

// normalizeQuoteInput trims the selection and caps the context either side.
//
// The context is capped hard because a long one makes re-anchoring brittle for
// no gain: it exists to tell two identical sentences apart, and a paragraph of
// surrounding text fails to match as soon as anything near it is edited.
func normalizeQuoteInput(q domain.Quote) domain.Quote {
	return domain.Quote{
		Exact:  clampRunes(strings.TrimSpace(q.Exact), domain.MaxQuoteLen),
		Prefix: clampRunesEnd(strings.TrimSpace(q.Prefix), domain.MaxQuoteContext),
		Suffix: clampRunes(strings.TrimSpace(q.Suffix), domain.MaxQuoteContext),
	}
}

// clampRunes keeps the first n runes.
func clampRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// clampRunesEnd keeps the LAST n runes, which is what a prefix needs: the words
// immediately before the quote are the ones that identify it, and trimming from
// the wrong end throws exactly those away.
func clampRunesEnd(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// BulkResult is one row's outcome in a batch decision.
type BulkResult struct {
	ID    string `json:"id"`
	Code  string `json:"code,omitempty"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Bulk applies one human decision to a set of marks — the accept-all at the end
// of reviewing a pass.
//
// It is not a convenience wrapper. Reviewing a pass of fifteen marks one request
// at a time makes accepting the work cost more than the work, which is how a
// queue stops being read at all. Each row is still decided on its own and
// reported on its own: a partial failure has to be visible, because "twelve of
// fourteen" is a different situation from "done" and the screen must say which.
func (s *AnnotationService) Bulk(ctx context.Context, researchID string, ids []string, status domain.AnnotationStatus, reason string) ([]BulkResult, error) {
	if !status.Valid() {
		return nil, fmt.Errorf("unknown status %q: %w", status, ErrValidation)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no annotations given: %w", ErrValidation)
	}
	if len(ids) > domain.MaxAnnotationBatch*4 {
		return nil, fmt.Errorf("too many annotations in one call (max %d): %w", domain.MaxAnnotationBatch*4, ErrValidation)
	}

	// The research in the route is not decoration. Every row is authorized on
	// its own inside Update, so nothing crosses a user boundary either way — but
	// without this a caller who belongs to two researches could close the marks
	// of one through the URL of the other, and a per-research audit of who
	// accepted what would be reading the wrong research.
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}

	req := UpdateAnnotationRequest{Status: &status}
	if reason != "" {
		req.Reason = &reason
	}

	out := make([]BulkResult, 0, len(ids))
	for _, id := range ids {
		a, err := s.annotations.FindByID(ctx, id)
		if err != nil || a == nil || a.ResearchID != researchID {
			out = append(out, BulkResult{ID: id, Error: ErrNotFound.Error()})
			continue
		}
		updated, err := s.Update(ctx, id, req)
		if err != nil {
			// The code is known even on failure, so a partial batch names rows a
			// person can find rather than raw uuids.
			out = append(out, BulkResult{ID: id, Code: a.Code, Error: err.Error()})
			continue
		}
		out = append(out, BulkResult{ID: id, Code: updated.Code, OK: true})
	}
	return out, nil
}

// FilterByAnchor keeps the marks whose text is in a given state.
//
// Anchor state is computed from the document, so it cannot be a WHERE clause —
// which means a caller filtering for orphans has to read more rows than it
// keeps. That is the honest trade and it is why the caller does the paging: the
// orphan list is short by nature, and a limit applied before this would return
// an arbitrary subset of an already small set.
func FilterByAnchor(anns []*domain.Annotation, state domain.AnchorState) []*domain.Annotation {
	out := make([]*domain.Annotation, 0, len(anns))
	for _, a := range anns {
		if a.Anchor != nil && a.Anchor.State == state {
			out = append(out, a)
		}
	}
	return out
}
