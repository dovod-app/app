package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/google/uuid"
)

var refPattern = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Matches markdown links [title](url) and bare URLs
var mdLinkPattern = regexp.MustCompile(`\[([^\]]*)\]\((https?://[^)]+)\)`)
var bareLinkPattern = regexp.MustCompile(`(?:^|[\s(])((https?://)[^\s)<>]+)`)

// CrossRefParser parses [[...]] references from text and stores them.
type CrossRefParser interface {
	ParseCrossRefs(ctx context.Context, sourceType, sourceID, researchID, text string)
}

type CreateEntryRequest struct {
	ResearchID  string
	SectionID   string
	SessionID   string
	Type        domain.EntryType
	Content     string
	Title       string
	Description string
	Status      domain.EntryStatus
	Tags        []string
	// Metadata is values keyed by the field keys the target section declares.
	// Anything else is reported and dropped — the vocabulary is closed.
	Metadata map[string]any
	// Verbatim says the content arrived as a file rather than as a JSON string,
	// so its escape sequences are content and must not be expanded.
	//
	// normalizeContent turns a literal `\n` into a newline because MCP clients
	// really do send escaped newlines inside JSON strings. A file has no such
	// problem: two characters in it are two characters, and they are most often
	// inside a code block, where expanding them rewrites the code the document
	// was written to explain.
	Verbatim bool
}

type UpdateEntryRequest struct {
	Type        *domain.EntryType
	Title       *string
	Content     *string
	Description *string
	Status      *domain.EntryStatus
	Tags        []string
	TextReplace *TextReplace
	SessionID   *string
	// Metadata is a pointer so an omitted map (leave the values alone) is
	// distinguishable from an empty one (clear them).
	Metadata *map[string]any
	// AllowIncomplete carries the human's override past the completed gate. It
	// is never set by an agent's ordinary write, and the revision summary says
	// it was used.
	AllowIncomplete bool
}

type TextReplace struct {
	From string
	To   string
}

type EntryService struct {
	entries       *storage.EntryRepository
	sections      *storage.SectionRepository
	researches    *storage.ResearchRepository
	access        *Access
	sessions      *storage.SessionRepository
	blocks        *storage.BlockRepository
	revisions     *storage.EntryRevisionRepository
	crossrefs     *storage.CrossRefRepository
	externalLinks *storage.ExternalLinkRepository
	roadmaps      *storage.RoadmapRepository
	roadmapNodes  *storage.RoadmapNodeRepository
	// tasks is optional; see SetTaskRepo. Present, a task_ref block exports as a
	// checklist with real titles instead of a list of codes.
	tasks *storage.TaskRepository
	// questionRepo lets the rebuild reach answers. Optional, like the two above:
	// without it a rebuild repairs documents and tasks and says so honestly in
	// its source count, rather than failing.
	questionRepo *storage.QuestionRepository
	// annotations is optional; see SetAnnotations. Present, an entry write
	// reports which marks it drifted or orphaned.
	annotations *storage.AnnotationRepository
	events      EventNotifier
	log         *slog.Logger
	// revisionLimit keeps the newest N revisions per entry plus revision 1.
	// Zero — the default — keeps everything, which for kilobyte documents is
	// the honest choice; see SetRevisionLimit.
	revisionLimit int
}

func NewEntryService(entries *storage.EntryRepository, sections *storage.SectionRepository, researches *storage.ResearchRepository, access *Access, sessions *storage.SessionRepository, blocks *storage.BlockRepository, revisions *storage.EntryRevisionRepository, crossrefs *storage.CrossRefRepository, externalLinks *storage.ExternalLinkRepository, events EventNotifier, log *slog.Logger) *EntryService {
	return &EntryService{entries: entries, sections: sections, researches: researches, access: access, sessions: sessions, blocks: blocks, revisions: revisions, crossrefs: crossrefs, externalLinks: externalLinks, events: events, log: log}
}

// SetRevisionLimit caps how much history an entry keeps. Revision 1 always
// survives: it is the only record of what the entry looked like when it was
// created, and that is the snapshot a reader asks for months later.
func (s *EntryService) SetRevisionLimit(n int) { s.revisionLimit = n }

// SetTaskRepo enables a task_ref block to be exported as a checklist.
//
// Optional in the same way and for the same reason as SetRoadmapRepos: without
// it the block still exports, as the list of references it stores. Nothing else
// in this service reads tasks — resolution is a projection concern, and the
// tick itself goes through TaskService where the permission lives.
func (s *EntryService) SetTaskRepo(tasks *storage.TaskRepository) { s.tasks = tasks }

// SetRoadmapRepos enables [[RM1]] and [[RM1:N3]] cross-reference resolution.
func (s *EntryService) SetRoadmapRepos(roadmaps *storage.RoadmapRepository, nodes *storage.RoadmapNodeRepository) {
	s.roadmaps = roadmaps
	s.roadmapNodes = nodes
}

func (s *EntryService) Create(ctx context.Context, req CreateEntryRequest) (*domain.Entry, error) {
	// Validate research exists and current user has access
	if err := s.access.Write(ctx, req.ResearchID); err != nil {
		return nil, fmt.Errorf("research %s: %w", req.ResearchID, err)
	}

	// Validate section exists and belongs to research
	section, err := s.sections.FindByID(ctx, req.SectionID)
	if err != nil {
		return nil, fmt.Errorf("find section: %w", err)
	}
	if section == nil {
		return nil, fmt.Errorf("section %s: %w", req.SectionID, ErrNotFound)
	}
	if section.ResearchID != req.ResearchID {
		return nil, fmt.Errorf("section %s does not belong to research %s", req.SectionID, req.ResearchID)
	}

	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("content is required")
	}

	metadata, metaReport := applyMetadata(section, req.Metadata, nil)

	entryType := req.Type
	if entryType == "" {
		entryType = domain.EntryMarkdown
	}
	if !entryType.Valid() {
		return nil, fmt.Errorf("invalid entry_type %q: want %q or %q (%q is accepted as a single html block)",
			entryType, domain.EntryMarkdown, domain.EntryBlocks, domain.EntryArtifact)
	}

	// Kept because normalization is about to strip server-owned state out of it,
	// and on creation that state is the only copy there is — an imported research
	// carries its ticks in the file and nowhere else.
	authored := req.Content

	// Content normalization depends on the type and must happen after it is known:
	// normalizeContent expands a literal \n, which inside a block document's JSON
	// strings would produce a real newline and make the JSON unparseable.
	content, entryType, err := s.normalizeEntryContent(req.Content, entryType, req.Verbatim)
	if err != nil {
		return nil, err
	}
	req.Content = content

	// Normalize the same way Update does, so the same input stored through
	// entry_create and entry_update ends up identical.
	title := normalizeTitle(req.Title)
	description := normalizeContent(req.Description)

	if entryType == domain.EntryBlocks {
		doc, derr := NormalizeBlockDocument(req.Content)
		if derr != nil {
			return nil, derr
		}
		if title == "" {
			title = BlockDocumentTitle(doc)
		}
		if title == "" {
			return nil, fmt.Errorf("title is required: the document has no heading to take one from")
		}
		if description == "" {
			description = BlockDocumentDescription(doc, title)
		}
	} else {
		if title == "" {
			title = autoTitle(req.Content)
		}
		if description == "" {
			description = autoDescription(req.Content)
		}
	}

	status := req.Status
	if status == "" {
		status = domain.EntryDraft
	}

	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}

	// Auto-assign active session if not specified
	sessionID := req.SessionID
	if sessionID == "" && s.sessions != nil {
		if active, _ := s.sessions.FindActive(ctx, req.ResearchID); active != nil {
			sessionID = active.ID
		}
	} else if sessionID != "" {
		if err := s.validateSession(ctx, req.ResearchID, sessionID); err != nil {
			return nil, err
		}
	}

	entry := &domain.Entry{
		ID:          uuid.New().String(),
		ResearchID:  req.ResearchID,
		SectionID:   req.SectionID,
		SessionID:   sessionID,
		Type:        entryType,
		Title:       title,
		Content:     req.Content,
		Description: description,
		Status:      status,
		Tags:        tags,
		Metadata:    metadata,
		SpecVersion: section.SpecVersion,
	}

	if err := s.entries.Create(ctx, entry); err != nil {
		return nil, fmt.Errorf("create entry: %w", err)
	}
	entry.MetaReport = metaReport

	if entry.Type == domain.EntryBlocks {
		doc, derr := NormalizeBlockDocument(entry.Content)
		if derr != nil {
			return nil, derr
		}
		// On creation the author owns the state: this is how an imported research
		// keeps the ticks it was exported with. Every later write strips it —
		// Terraform draws the same line, honouring a field on create and ignoring
		// it on update.
		if incoming, perr := ParseStoredBlockDocument(authored); perr == nil {
			carryAuthoredState(doc, incoming)
		}
		report, serr := s.saveBlockDocument(ctx, entry, doc, stateAuthoritative, revisionNote{skip: true})
		if serr != nil {
			return nil, serr
		}
		entry.BlockReport = &report
	}

	// Revision 1, after the content is final — for a block document that means
	// after the rows were written, because entries.content only becomes the
	// projection of those rows inside saveBlockDocument.
	//
	// This is the one write that records its revision outside the transaction
	// that produced it: creating an entry is already two statements (the row,
	// then its blocks), and a document that exists without a first snapshot is
	// recoverable — the next update opens the history — while a snapshot of a
	// document that failed to store is not.
	//
	// The session is the entry's own, already resolved above: explicit if the
	// caller named one, the active session otherwise. Asking for the active
	// session again here would override an explicit choice made microseconds
	// earlier — which is exactly what an import does, where every entry names a
	// session and one unrelated session happens to be active. Its Changes tab
	// would then claim it created the whole research.
	if err := s.recordRevision(ctx, nil, entry, revisionNote{
		sessionID:  entry.SessionID,
		sessionSet: true,
	}); err != nil {
		s.log.Error("record initial revision", "entry", entry.ID, "error", err)
	}

	s.updateCrossRefs(ctx, entry)
	s.updateExternalLinks(ctx, entry)
	// This document's own outgoing references are stored above. This repairs the
	// incoming ones — everything that already cited this code while it named
	// nothing. See crossref_resolve.go.
	s.ResolveDanglingEntry(ctx, entry.ResearchID, entry.Code, entry.ID)
	emit(ctx, s.events, Event{Type: "entry.created", ResearchID: entry.ResearchID, EntityID: entry.ID, Entity: "entry"})
	return entry, nil
}

// validateSession refuses a session_id that does not belong to the entry's own
// research.
//
// The field is caller-supplied on both create and update, and until revisions
// existed nothing turned it into anything visible, so an id from someone else's
// research was inert. It is not any more: a revision records the session it was
// written under, and the history resolves that id to a code and a title. An
// unvalidated field on a write is a leak waiting for a reader.
//
// Empty means "no session" and is always allowed — that is how an entry is
// unlinked.
func (s *EntryService) validateSession(ctx context.Context, researchID, sessionID string) error {
	if sessionID == "" || s.sessions == nil {
		return nil
	}
	sess, err := s.sessions.FindByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("find session: %w", err)
	}
	// Same reply for "no such session" and "not this research": a caller must not
	// learn that a session exists somewhere they cannot see.
	if sess == nil || sess.ResearchID != researchID {
		return fmt.Errorf("session %s: %w", sessionID, ErrNotFound)
	}
	return nil
}

func (s *EntryService) Get(ctx context.Context, id string) (*domain.Entry, error) {
	entry, err := s.entries.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find entry: %w", err)
	}
	if entry == nil {
		return nil, ErrNotFound
	}
	if err := s.access.Read(ctx, entry.ResearchID); err != nil {
		return nil, ErrNotFound
	}
	s.attachMetadataStatus(ctx, entry)
	redactEntryForShare(ctx, entry)
	return entry, nil
}

// GetByIDOrCode resolves an entry by UUID or short code within a research.
func (s *EntryService) GetByIDOrCode(ctx context.Context, researchID, idOrCode string) (*domain.Entry, error) {
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, ErrNotFound
	}
	// Try UUID first
	entry, err := s.entries.FindByID(ctx, idOrCode)
	if err != nil {
		return nil, fmt.Errorf("find entry: %w", err)
	}
	// A UUID resolves globally, so it can name an entry of a research the caller
	// was never checked against — the access check above only covered researchID.
	if entry != nil && entry.ResearchID != researchID {
		return nil, ErrNotFound
	}
	// If not found and looks like a code, try by code
	if entry == nil && isCode(idOrCode) {
		entry, err = s.entries.FindByCode(ctx, researchID, idOrCode)
		if err != nil {
			return nil, fmt.Errorf("find entry by code: %w", err)
		}
	}
	if entry == nil {
		return nil, ErrNotFound
	}
	s.attachMetadataStatus(ctx, entry)
	redactEntryForShare(ctx, entry)
	return entry, nil
}

func (s *EntryService) List(ctx context.Context, researchID, sectionID string, filter storage.EntryFilter) ([]*domain.Entry, error) {
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}
	entries, err := s.entries.FindBySection(ctx, researchID, sectionID, filter)
	if err != nil {
		return nil, err
	}
	s.attachMetadataStatusAll(ctx, entries)
	redactEntriesForShare(ctx, entries)
	return entries, nil
}

func (s *EntryService) ListByResearch(ctx context.Context, researchID string, filter storage.EntryFilter) ([]*domain.Entry, error) {
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}
	entries, err := s.entries.FindByResearch(ctx, researchID, filter)
	if err != nil {
		return nil, err
	}
	s.attachMetadataStatusAll(ctx, entries)
	redactEntriesForShare(ctx, entries)
	return entries, nil
}

// ListWithContent returns every entry of a research with its body.
//
// It exists because three exporters — the markdown and JSON handlers, the
// portable dump and the Obsidian vault — were reading the repository directly,
// which is how document metadata reached a share visitor: the redaction lives
// on the service, and the repository has never known what a share is.
//
// Anything that needs entries with content goes through here.
func (s *EntryService) ListWithContent(ctx context.Context, researchID string) ([]*domain.Entry, error) {
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}
	entries, err := s.entries.FindByResearchWithContent(ctx, researchID)
	if err != nil {
		return nil, fmt.Errorf("list entries with content: %w", err)
	}
	redactEntriesForShare(ctx, entries)
	return entries, nil
}

func (s *EntryService) Update(ctx context.Context, id string, req UpdateEntryRequest) (*domain.Entry, error) {
	return s.update(ctx, id, req, revisionNote{summary: summarizeUpdate(req)})
}

// update is Update with the caller's say over how the revision is labelled.
// Restore uses it to record its own author kind rather than masquerading as an
// ordinary edit.
func (s *EntryService) update(ctx context.Context, id string, req UpdateEntryRequest, note revisionNote) (*domain.Entry, error) {
	entry, err := s.entries.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find entry: %w", err)
	}
	if entry == nil {
		return nil, ErrNotFound
	}
	if err := s.access.Write(ctx, entry.ResearchID); err != nil {
		return nil, err
	}
	// Remembered before anything below can rewrite it: an entry that stops being
	// a block document has to take its rows with it.
	prevType := entry.Type
	// Where the marks sit now, read before this write touches the document.
	// Afterwards there is nothing left to compare against.
	anchorsBefore := s.captureAnchors(ctx, entry)

	var metaReport *domain.MetadataReport

	// The target type decides how new content is normalized, so settle it before
	// touching content — and remember whether the type itself was asked to change,
	// because switching type without new content has to convert what is stored.
	targetType := entry.Type
	if req.Type != nil {
		if !req.Type.Valid() {
			return nil, fmt.Errorf("invalid entry_type %q: want %q or %q (%q is accepted as a single html block)",
				*req.Type, domain.EntryMarkdown, domain.EntryBlocks, domain.EntryArtifact)
		}
		targetType = *req.Type
	}

	if req.Title != nil {
		entry.Title = normalizeTitle(*req.Title)
	}
	if req.Content != nil {
		// Update has no verbatim form: an update arrives as a JSON string from
		// an MCP client or a form, never as a file. Importing never updates —
		// a file has no identity we trust, so there is nothing to match on.
		content, stored, err := s.normalizeEntryContent(*req.Content, targetType, false)
		if err != nil {
			return nil, err
		}
		entry.Content = content
		entry.Type = stored
	} else if req.Type != nil {
		// Type changed with no new body: convert what is already stored, otherwise
		// the entry would claim a shape its content does not have.
		content, stored, err := s.convertStoredContent(entry, targetType)
		if err != nil {
			return nil, err
		}
		entry.Content = content
		entry.Type = stored
	}
	if req.Description != nil {
		entry.Description = normalizeContent(*req.Description)
	}
	if req.Tags != nil {
		entry.Tags = req.Tags
	}
	if req.Metadata != nil {
		// The declaration is read now rather than remembered from creation: an
		// entry written before the section grew a field is validated against
		// what the section says today, which is what makes topping up on the
		// next ordinary write the whole migration mechanism.
		section, serr := s.sections.FindByID(ctx, entry.SectionID)
		if serr != nil {
			return nil, fmt.Errorf("find section: %w", serr)
		}
		metadata, report := applyMetadata(section, *req.Metadata, entry.Metadata)
		entry.Metadata = metadata
		if section != nil {
			entry.SpecVersion = section.SpecVersion
		}
		metaReport = report
	}
	// Checked after the metadata above has been applied, never before: the
	// natural call is "finish this document and fill in its fields", and
	// evaluating the gate against the pre-write values refused exactly that —
	// then offered an override for an incompleteness the same request had
	// already fixed.
	if req.Status != nil {
		// The one place incompleteness means anything. Everywhere else a write
		// is accepted and reported on, because the author is usually a model
		// mid-interview; declaring a document finished is a deliberate act, and
		// it is worth stopping.
		if *req.Status == domain.EntryCompleted && entry.Status != domain.EntryCompleted && !req.AllowIncomplete {
			if missing := s.missingRequiredFor(ctx, entry); len(missing) > 0 {
				return nil, &IncompleteMetadataError{Missing: missing}
			}
		}
		entry.Status = *req.Status
	}
	if req.SessionID != nil {
		if err := s.validateSession(ctx, entry.ResearchID, *req.SessionID); err != nil {
			return nil, err
		}
		entry.SessionID = *req.SessionID
	}

	// text_replace
	if req.TextReplace != nil && entry.Type == domain.EntryBlocks {
		// The replacement runs over the stored string after normalization and is
		// never re-parsed, so on a block document it is unvalidated surgery on
		// JSON: one quote in the replacement and the document stops parsing, with
		// a 200 in reply and the page rendering raw JSON.
		return nil, ErrTextReplaceOnBlocks
	}
	if req.TextReplace != nil {
		if !strings.Contains(entry.Content, req.TextReplace.From) {
			return nil, ErrTextReplaceNotFound
		}
		entry.Content = strings.Replace(entry.Content, req.TextReplace.From, req.TextReplace.To, 1)
	}

	// Everything the revision needs from the database is read here, before any
	// transaction opens — see revisionNote.sessionID.
	note = s.resolveSession(ctx, entry, note)

	if entry.Type == domain.EntryBlocks {
		// Rows are the document; entries.content is the projection written beside
		// them in the same transaction. Both happen inside saveBlockDocument,
		// and so does the revision — a snapshot that survived a rolled-back
		// write would describe a document that never existed.
		doc, derr := NormalizeBlockDocument(entry.Content)
		if derr != nil {
			return nil, derr
		}
		report, serr := s.saveBlockDocument(ctx, entry, doc, stateFromAuthor, note)
		if serr != nil {
			return nil, serr
		}
		entry.BlockReport = &report
	} else {
		if terr := s.inTx(ctx, func(tx storage.Querier) error {
			if err := s.entries.UpdateTx(ctx, tx, entry); err != nil {
				return fmt.Errorf("update entry: %w", err)
			}
			if prevType == domain.EntryBlocks {
				// It is markdown now, so the rows describe nothing. Leaving them
				// would resurrect stale ticks if it ever became a block document
				// again.
				if derr := s.blocks.DeleteForEntry(ctx, tx, entry.ID); derr != nil {
					return derr
				}
			}
			return s.recordRevision(ctx, tx, entry, note)
		}); terr != nil {
			return nil, terr
		}
	}

	s.updateCrossRefs(ctx, entry)
	s.updateExternalLinks(ctx, entry)
	entry.MetaReport = metaReport
	entry.AnnReport = s.anchorReport(ctx, entry, anchorsBefore)
	emit(ctx, s.events, Event{Type: "entry.updated", ResearchID: entry.ResearchID, EntityID: entry.ID, Entity: "entry"})
	return entry, nil
}

func (s *EntryService) Delete(ctx context.Context, id string) error {
	entry, err := s.entries.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find entry: %w", err)
	}
	if entry == nil {
		return ErrNotFound
	}
	if err := s.access.Write(ctx, entry.ResearchID); err != nil {
		return err
	}

	// Clean up cross-references and external links
	if s.crossrefs != nil {
		_ = s.crossrefs.ReplaceForSource(ctx, "entry", id, nil)
	}
	if s.externalLinks != nil {
		_ = s.externalLinks.ReplaceForSource(ctx, "entry", id, nil)
	}

	if err := s.entries.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	emit(ctx, s.events, Event{Type: "entry.deleted", ResearchID: entry.ResearchID, EntityID: id, Entity: "entry"})
	return nil
}

// RebuildReport is what a rebuild actually did.
//
// It replaces a single integer that was named `rebuilt` and documented as "how
// many references were found", while being neither: it counted documents
// rescanned. A person pressing the button on a research with 22 documents and
// three links was told "22 references".
type RebuildReport struct {
	// Sources rescanned — documents, task results, question answers.
	Sources int `json:"sources"`
	// References found across all of them, and how many of those still name
	// nothing. Unresolved is the number worth acting on: after the create-time
	// repair it should be a typo or a deleted target, not a timing race.
	References int `json:"references"`
	Unresolved int `json:"unresolved"`
}

// RebuildCrossRefs rescans a research and rebuilds its cross-references.
//
// It rewrites stored rows, so it is a write however much it reads: a viewer
// pointing this at a research would edit its index.
//
// It is the last resort, not the mechanism. A reference written before its
// target exists is repaired the moment the target is created (see
// crossref_resolve.go); this exists for the cases nothing can hook — an import,
// a restore, codes backfilled onto records that predate them.
//
// It rescans documents, task results and question answers — not documents
// alone, which is what it used to do, leaving exactly the references written
// from a task or an answer broken while reporting success. Annotation-sourced
// rows are counted in the report but not rescanned; an annotation is not a
// source this rewrites.
func (s *EntryService) RebuildCrossRefs(ctx context.Context, researchID string) (RebuildReport, error) {
	var report RebuildReport
	// Accept an R code, because the tool that calls this is handed one by
	// research_get and its schema says it may pass one. Everything below is
	// scoped to the resolved id, never to the caller's string.
	researchID = s.resolveResearchID(ctx, researchID)
	if err := s.access.Write(ctx, researchID); err != nil {
		return report, err
	}
	entries, err := s.entries.FindByResearchWithContent(ctx, researchID)
	if err != nil {
		return report, fmt.Errorf("fetch entries: %w", err)
	}

	for _, entry := range entries {
		s.updateCrossRefs(ctx, entry)
		s.updateExternalLinks(ctx, entry)
		report.Sources++
	}
	report.Sources += s.rebuildOtherSources(ctx, researchID)

	// Counted from the table rather than tallied while writing, so the number
	// describes what a reader of the graph will actually find — and counted
	// through the same visibility filter the list route uses, because the card
	// shows both numbers and a reader comparing them must not see one verdict
	// before pressing the button and a different one after.
	if refs, err := s.crossrefs.FindByResearch(ctx, researchID); err == nil {
		refs = s.access.VisibleCrossRefs(ctx, refs)
		report.References = len(refs)
		for _, ref := range refs {
			if !ref.Resolved {
				report.Unresolved++
			}
		}
	}

	// The link tables this rewrites are what the graph and the mind map are
	// drawn from, and nothing else announces the change — so both stayed on the
	// previous link set until someone reloaded the page by hand.
	emit(ctx, s.events, Event{Type: "crossrefs.rebuilt", ResearchID: researchID, EntityID: researchID, Entity: "crossref"})
	return report, nil
}

// taskIndexText is the text a task contributes to the reference table.
//
// One definition, because the write path and the rebuild disagreeing about it
// makes a reference flicker: whichever ran last decides whether the row exists.
func taskIndexText(t *domain.Task) string {
	return t.Description + "\n" + t.Result
}

// rebuildOtherSources rescans the sources that are not documents.
//
// Both repos are optional on this service — the narrower tests construct it
// without them — so a missing one costs those sources rather than the rebuild.
func (s *EntryService) rebuildOtherSources(ctx context.Context, researchID string) int {
	n := 0
	if s.tasks != nil {
		if tasks, err := s.tasks.FindByResearch(ctx, researchID, storage.TaskFilter{}); err == nil {
			for _, t := range tasks {
				// Exactly what TaskService indexes, and no more. Adding the title
				// here would create rows the next task edit deletes again —
				// ReplaceForSource is a whole-source replacement — so a
				// reference would appear and disappear depending on which write
				// happened last, and the `unresolved` count would move with it.
				s.parseCrossRefs(ctx, "task", t.ID, researchID, taskIndexText(t))
				n++
			}
		}
	}
	if s.sessions != nil && s.questionRepo != nil {
		if sessions, err := s.sessions.FindByResearch(ctx, researchID); err == nil {
			for _, sess := range sessions {
				qs, err := s.questionRepo.FindBySession(ctx, sess.ID, storage.QuestionFilter{})
				if err != nil {
					continue
				}
				for _, q := range qs {
					if q.Answer == "" {
						continue
					}
					s.parseCrossRefs(ctx, "question", q.ID, researchID, q.Answer)
					n++
				}
			}
		}
	}
	return n
}

// updateCrossRefs parses [[...]] references from entry content and stores them.
func (s *EntryService) updateCrossRefs(ctx context.Context, entry *domain.Entry) {
	s.parseCrossRefs(ctx, "entry", entry.ID, entry.ResearchID, EntryIndexText(entry))
}

// updateExternalLinks extracts URLs from entry content and stores them.
func (s *EntryService) updateExternalLinks(ctx context.Context, entry *domain.Entry) {
	s.parseExternalLinks(ctx, "entry", entry.ID, entry.ResearchID, EntryIndexText(entry))
}

// EntryIndexText is the entry's prose, whatever its type. For a blocks entry the
// stored content is JSON, so scanning it directly would index keys and quoted
// fragments — and every [[E3]] inside block text would be missed.
func EntryIndexText(entry *domain.Entry) string {
	if entry == nil {
		return ""
	}
	if entry.Type != domain.EntryBlocks {
		return entry.Content
	}
	doc, err := NormalizeBlockDocument(entry.Content)
	if err != nil {
		return ""
	}
	return BlockPlainText(doc)
}

// ParseExternalLinks extracts URLs from text and stores them.
func (s *EntryService) ParseExternalLinks(ctx context.Context, sourceType, sourceID, researchID, text string) {
	s.parseExternalLinks(ctx, sourceType, sourceID, researchID, text)
}

func (s *EntryService) parseExternalLinks(ctx context.Context, sourceType, sourceID, researchID, text string) {
	if s.externalLinks == nil || text == "" {
		return
	}

	seen := make(map[string]bool)
	var links []domain.ExternalLink

	// Extract [title](url) markdown links
	for _, m := range mdLinkPattern.FindAllStringSubmatch(text, -1) {
		rawURL := m[2]
		if seen[rawURL] {
			continue
		}
		seen[rawURL] = true
		links = append(links, domain.ExternalLink{
			ID:         uuid.New().String(),
			SourceType: sourceType,
			SourceID:   sourceID,
			ResearchID: researchID,
			URL:        rawURL,
			Title:      m[1],
			Domain:     extractDomain(rawURL),
		})
	}

	// Extract bare URLs not already captured
	for _, m := range bareLinkPattern.FindAllStringSubmatch(text, -1) {
		rawURL := m[1]
		if seen[rawURL] {
			continue
		}
		seen[rawURL] = true
		links = append(links, domain.ExternalLink{
			ID:         uuid.New().String(),
			SourceType: sourceType,
			SourceID:   sourceID,
			ResearchID: researchID,
			URL:        rawURL,
			Title:      "",
			Domain:     extractDomain(rawURL),
		})
	}

	if err := s.externalLinks.ReplaceForSource(ctx, sourceType, sourceID, links); err != nil {
		s.log.Error("failed to update external links", "source_type", sourceType, "source_id", sourceID, "error", err)
	}
}

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// ParseCrossRefs extracts [[...]] references from text and stores them.
// Can be called for entries, questions, or tasks.
func (s *EntryService) ParseCrossRefs(ctx context.Context, sourceType, sourceID, researchID, text string) {
	s.parseCrossRefs(ctx, sourceType, sourceID, researchID, text)
}

func (s *EntryService) parseCrossRefs(ctx context.Context, sourceType, sourceID, researchID, text string) {
	if s.crossrefs == nil || text == "" {
		return
	}
	refs := s.resolveRefs(ctx, sourceType, sourceID, researchID, text)
	if err := s.crossrefs.ReplaceForSource(ctx, sourceType, sourceID, refs); err != nil {
		s.log.Error("failed to update crossrefs", "source_type", sourceType, "source_id", sourceID, "error", err)
	}
}

// resolveRefs finds every [[...]] in the text and says what each one points at,
// storing nothing.
//
// Split out of parseCrossRefs for the markdown import, which needs the question
// "does this reference resolve here" answered before it decides whether to
// write anything at all. Keeping one resolver means the answer the preview
// gives and the rows the write stores cannot disagree.
func (s *EntryService) resolveRefs(ctx context.Context, sourceType, sourceID, researchID, text string) []domain.CrossRef {
	matches := refPattern.FindAllStringSubmatch(text, -1)
	var refs []domain.CrossRef

	for _, m := range matches {
		raw := m[1]
		cr := domain.CrossRef{
			SourceType:       sourceType,
			SourceID:         sourceID,
			SourceResearchID: researchID,
			TargetRef:        raw,
		}

		kind, first, second := parseRef(raw)

		switch kind {
		// Every branch below resolves whatever the code names, without asking
		// what the author may see. Codes are global, so these lookups reach
		// anyone's work — and the reader, not the author, is who decides
		// whether a resolved reference is shown: see Access.VisibleCrossRefs.
		case "roadmap":
			// [[RM1]] — link to a roadmap in this research.
			//
			// Scoped, because roadmap codes are allocated per research: an
			// unscoped lookup returned whichever research happened to hold the
			// first RM1, so one project's document named another's plan.
			if s.roadmaps != nil {
				rm, err := s.roadmaps.FindByCodeAndResearch(ctx, first, researchID)
				if err == nil && rm != nil {
					cr.TargetRoadmapID = rm.ID
					cr.TargetResearchID = rm.ResearchID
					cr.Resolved = true
				}
			}
		case "node":
			// [[RM1:N3]] — a node of a roadmap in this research. Scoped for the
			// same reason as the roadmap above.
			if s.roadmaps != nil && s.roadmapNodes != nil {
				rm, err := s.roadmaps.FindByCodeAndResearch(ctx, first, researchID)
				if err == nil && rm != nil {
					cr.TargetRoadmapID = rm.ID
					cr.TargetResearchID = rm.ResearchID
					node, err := s.roadmapNodes.FindByCode(ctx, rm.ID, second)
					if err == nil && node != nil {
						cr.TargetNodeID = node.ID
						cr.Resolved = true
					}
				}
			}
		case "task":
			// [[T4]] — link to a task on the board.
			//
			// A task has no page of its own, so there is nothing to store an id
			// against: `resolved` here means "this research really has a T4", and
			// the reader's link is built from the code, the way every task link in
			// this product is. Codes are per-research and there is no cross-research
			// form, so this never reaches outside the source research.
			if s.tasks != nil {
				task, err := s.tasks.FindByCode(ctx, researchID, first)
				if err == nil && task != nil {
					cr.TargetResearchID = researchID
					cr.Resolved = true
				}
			}
		case "research":
			// [[R2]] — link to a research
			targetResearch, err := s.researches.FindByCode(ctx, first)
			if err == nil && targetResearch != nil {
				cr.TargetResearchID = targetResearch.ID
				cr.Resolved = true
			}
		case "entry":
			// [[E3]] or [[R2:E5]] — link to an entry
			if first != "" {
				// Cross-research: [[R2:E5]]
				//
				// The target research's id is stored only if the entry is found
				// too. It used to be stored the moment R2 resolved, which made
				// the row itself an answer to "does R2 exist" — a question the
				// author is not entitled to ask about a research they have no
				// role on. Writing [[R1:E1]], [[R2:E1]], … and reading the rows
				// back enumerated other tenants' researches without a single
				// link ever resolving. VisibleCrossRefs blanks it on the way out
				// as well, for the rows already written.
				targetResearch, err := s.researches.FindByCode(ctx, first)
				if err == nil && targetResearch != nil {
					if second != "" {
						targetEntry, err := s.entries.FindByCode(ctx, targetResearch.ID, second)
						if err == nil && targetEntry != nil {
							cr.TargetResearchID = targetResearch.ID
							cr.TargetEntryID = targetEntry.ID
							cr.Resolved = true
						}
					} else {
						cr.TargetResearchID = targetResearch.ID
						cr.Resolved = true
					}
				}
			} else if second != "" {
				// Same-research: [[E3]]
				cr.TargetResearchID = researchID
				targetEntry, err := s.entries.FindByCode(ctx, researchID, second)
				if err == nil && targetEntry != nil {
					cr.TargetEntryID = targetEntry.ID
					cr.Resolved = true
				}
			}
		}

		refs = append(refs, cr)
	}
	return refs
}

// parseRef splits references:
//
//	"R2:E5"  → kind="entry",   first="R2",  second="E5"
//	"E5"     → kind="entry",   first="",    second="E5"
//	"R2"     → kind="research",first="R2",  second=""
//	"RM1"    → kind="roadmap", first="RM1", second=""
//	"RM1:N3" → kind="node",    first="RM1", second="N3"
//	"T4"     → kind="task",    first="T4",  second=""
func parseRef(ref string) (kind, first, second string) {
	if idx := strings.IndexByte(ref, ':'); idx >= 0 {
		left, right := ref[:idx], ref[idx+1:]
		if strings.HasPrefix(left, "RM") {
			return "node", left, right
		}
		return "entry", left, right
	}
	if strings.HasPrefix(ref, "RM") {
		return "roadmap", ref, ""
	}
	// A task code is the whole reference; anything else beginning with T is an
	// entry code as far as this is concerned, which is why the digits matter.
	if taskRefPattern.MatchString(ref) {
		return "task", ref, ""
	}
	if len(ref) > 1 && ref[0] == 'R' {
		return "research", ref, ""
	}
	return "entry", "", ref
}

// autoTitle extracts title from the first non-empty line of content.
func autoTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip leading markdown heading markers
		line = strings.TrimLeft(line, "# ")
		line = strings.TrimSpace(line)
		// Cut by runes, not bytes. `line[:100]` splits a multi-byte character
		// in half, and the broken tail is stored — every Cyrillic entry created
		// without an explicit title ended up with U+FFFD on the end of it, shown
		// in the entries list, the change cards and the history rail.
		if runes := []rune(line); len(runes) > 100 {
			line = string(runes[:100])
		}
		return line
	}
	return "Untitled"
}

// autoDescription extracts description from lines 2-5 of content.
func autoDescription(content string) string {
	lines := strings.Split(content, "\n")
	var descLines []string
	started := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !started {
			if line != "" {
				started = true // skip first non-empty line (title)
			}
			continue
		}
		if line == "" {
			continue
		}
		// Strip markdown
		line = strings.TrimLeft(line, "# *_>-")
		line = strings.TrimSpace(line)
		descLines = append(descLines, line)
		if len(descLines) >= 4 {
			break
		}
	}
	desc := strings.Join(descLines, " ")
	// By runes, for the same reason as autoTitle: a byte cut lands inside a
	// character and stores the half of it that is left.
	if runes := []rune(desc); len(runes) > 200 {
		desc = string(runes[:200])
	}
	return desc
}

var (
	htmlTitleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	htmlDescRe  = regexp.MustCompile(`(?is)<meta[^>]+name\s*=\s*["']description["'][^>]*>`)
	htmlContent = regexp.MustCompile(`(?is)content\s*=\s*["']([^"']*)["']`)
)

// htmlTitle returns the text of the document's <title>, if it has one.
func htmlTitle(html string) string {
	m := htmlTitleRe.FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// htmlMetaDescription returns the content of <meta name="description">, if present.
func htmlMetaDescription(html string) string {
	tag := htmlDescRe.FindString(html)
	if tag == "" {
		return ""
	}
	m := htmlContent.FindStringSubmatch(tag)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// convertStoredContent handles a type change that arrives without a new body.
// Blocks → markdown is a real conversion; the other direction is refused rather
// than guessed, because wrapping a markdown document in one paragraph block would
// silently throw away its structure.
func (s *EntryService) convertStoredContent(entry *domain.Entry, target domain.EntryType) (string, domain.EntryType, error) {
	from := entry.Type
	if from == "" {
		from = domain.EntryMarkdown
	}

	switch {
	case target == domain.EntryMarkdown && from == domain.EntryBlocks:
		doc, err := ParseStoredBlockDocument(entry.Content)
		if err != nil {
			return "", "", fmt.Errorf("cannot convert to markdown: %w", err)
		}
		return BlockDocumentToMarkdown(doc), domain.EntryMarkdown, nil

	case (target == domain.EntryBlocks || target == domain.EntryArtifact) && from == domain.EntryMarkdown:
		return "", "", fmt.Errorf(
			"changing entry_type to %q needs the content in block form: pass content as {\"version\":1,\"blocks\":[...]} in the same call",
			domain.EntryBlocks)

	default:
		// Same type, or artifact→blocks which is already the stored shape.
		return entry.Content, entry.Type, nil
	}
}

// normalizeEntryContent applies the normalization the type calls for and resolves
// the `artifact` input alias. It returns the content to store and the type to
// store it under — never EntryArtifact, which is an input shape only.
func (s *EntryService) normalizeEntryContent(raw string, t domain.EntryType, verbatim bool) (string, domain.EntryType, error) {
	switch t {
	case domain.EntryArtifact:
		// Sugar: a bare HTML document becomes a blocks document with one html
		// block, so there is one stored shape and one renderer.
		out, err := MarshalBlockDocument(ArtifactToBlockDocument(raw))
		if err != nil {
			return "", "", err
		}
		return out, domain.EntryBlocks, nil

	case domain.EntryBlocks:
		doc, err := NormalizeBlockDocument(raw)
		if err != nil {
			return "", "", err
		}
		out, err := MarshalBlockDocument(doc)
		if err != nil {
			return "", "", err
		}
		return out, domain.EntryBlocks, nil

	default:
		if verbatim {
			// Sanitised, never expanded. See CreateEntryRequest.Verbatim.
			return sanitizeUTF8(raw), domain.EntryMarkdown, nil
		}
		return normalizeContent(raw), domain.EntryMarkdown, nil
	}
}

// applyMetadata validates submitted values against a section's declaration.
//
// A section that declares nothing accepts nothing: every key comes back as
// unknown, which is what "the vocabulary is closed" means at the write path.
// Nothing here can fail a write — the author is usually a model in the middle
// of an interview, and refusing there destroys answers a person already gave.
func applyMetadata(section *domain.Section, values, existing map[string]any) (map[string]any, *domain.MetadataReport) {
	if section == nil {
		return nil, nil
	}
	if values == nil {
		// A write that mentioned no metadata still gets told what the section
		// expects, when the section expects something. The agent that has not
		// read section_list is precisely the one that needs it, and the first
		// two or three documents in a section set the pattern every later one
		// copies.
		if missing := domain.MissingRequired(section.FieldSpec, nil); len(missing) > 0 {
			return nil, &domain.MetadataReport{
				MissingRequired: missing,
				SpecVersion:     section.SpecVersion,
			}
		}
		return nil, nil
	}
	// Single-line normalization, not normalizeContent: in a one-line field a
	// backslash is data, exactly as it is in a title.
	clean := make(map[string]any, len(values))
	for k, v := range values {
		switch t := v.(type) {
		case string:
			clean[k] = normalizeTitle(t)
		case []any:
			items := make([]any, 0, len(t))
			for _, item := range t {
				if str, ok := item.(string); ok {
					items = append(items, normalizeTitle(str))
					continue
				}
				items = append(items, item)
			}
			clean[k] = items
		default:
			clean[k] = v
		}
	}

	stored, report := domain.ValidateMetadata(section.FieldSpec, clean)

	// Values already recorded under keys the section has since stopped
	// declaring survive the write. Without this, the rule that removing a field
	// never deletes what documents carry would hold only until the next save —
	// and the save that destroyed them would be one somebody made to change a
	// different field entirely. A key that was never declared and never stored
	// is still refused: this preserves history, it does not open the vocabulary.
	declared := map[string]bool{}
	for _, f := range section.FieldSpec {
		declared[f.Key] = true
	}
	var carried []string
	for k, v := range existing {
		if declared[k] {
			continue
		}
		if _, replaced := stored[k]; replaced {
			continue
		}
		stored[k] = v
		carried = append(carried, k)
	}
	// A key the writer sent that is neither declared nor already stored is the
	// only real unknown; one it merely restated is not worth reporting.
	if len(carried) > 0 {
		kept := map[string]bool{}
		for _, k := range carried {
			kept[k] = true
		}
		filtered := report.UnknownKeys[:0]
		for _, issue := range report.UnknownKeys {
			if !kept[issue.Key] {
				filtered = append(filtered, issue)
			}
		}
		report.UnknownKeys = filtered
	}

	report.SpecVersion = section.SpecVersion
	if len(stored) == 0 {
		stored = nil
	}
	return stored, &report
}

// attachMetadataStatusAll is attachMetadataStatus over a list, with the
// sections read once rather than once per entry.
//
// The list surfaces need it for a reason the document page does not: a value
// outside its declared vocabulary can be recomputed only against the spec, and
// without this the table shows a wrong answer and a right one identically —
// on the one screen the feature exists to make gaps visible.
func (s *EntryService) attachMetadataStatusAll(ctx context.Context, entries []*domain.Entry) {
	if len(entries) == 0 {
		return
	}
	specs := map[string]*domain.Section{}
	for _, e := range entries {
		if _, seen := specs[e.SectionID]; seen {
			continue
		}
		section, err := s.sections.FindByID(ctx, e.SectionID)
		if err != nil {
			continue
		}
		specs[e.SectionID] = section
	}
	for _, e := range entries {
		section := specs[e.SectionID]
		if section == nil {
			continue
		}
		if len(section.FieldSpec) == 0 && len(e.Metadata) == 0 {
			continue
		}
		missing := domain.MissingRequired(section.FieldSpec, e.Metadata)
		e.MetaStatus = &domain.MetadataStatus{
			MissingRequired: missing,
			Orphaned:        domain.OrphanedKeys(section.FieldSpec, e.Metadata),
			Issues:          domain.MetadataIssues(section.FieldSpec, e.Metadata),
			Complete:        len(missing) == 0,
			SpecVersion:     section.SpecVersion,
		}
	}
}

// attachMetadataStatus answers "how does this document stand against what its
// section declares today", which is deliberately not a stored fact: adding a
// required field must make existing documents incomplete without rewriting a
// single one of them.
func (s *EntryService) attachMetadataStatus(ctx context.Context, entry *domain.Entry) {
	if entry == nil {
		return
	}
	section, err := s.sections.FindByID(ctx, entry.SectionID)
	if err != nil || section == nil {
		return
	}
	if len(section.FieldSpec) == 0 && len(entry.Metadata) == 0 {
		// A section that declares nothing, on a document carrying nothing: the
		// feature is invisible here and should stay that way.
		return
	}
	missing := domain.MissingRequired(section.FieldSpec, entry.Metadata)
	entry.MetaStatus = &domain.MetadataStatus{
		MissingRequired: missing,
		Orphaned:        domain.OrphanedKeys(section.FieldSpec, entry.Metadata),
		Issues:          domain.MetadataIssues(section.FieldSpec, entry.Metadata),
		Complete:        len(missing) == 0,
		SpecVersion:     section.SpecVersion,
	}
}

// missingRequiredFor answers the completed gate. It reads the section's current
// declaration rather than the version the entry was written against: whether a
// document may be called finished is a question about the rules in force now.
func (s *EntryService) missingRequiredFor(ctx context.Context, entry *domain.Entry) []string {
	section, err := s.sections.FindByID(ctx, entry.SectionID)
	if err != nil || section == nil || len(section.FieldSpec) == 0 {
		return nil
	}
	return domain.MissingRequired(section.FieldSpec, entry.Metadata)
}

// redactEntryForShare strips document metadata from anything a share link can
// reach.
//
// It is a separate rule from redactForShare, which covers the research, and it
// has to exist: metadata lives on the entry, so the moment the column arrived
// every share link that includes entries would have started publishing it —
// owner, cost, an interviewee's name, an internal ticket. Those are exactly the
// facts a section spec invites a team to record.
//
// The declaration goes too, in redactSectionForShare: a spec with no values
// renders as a list of everything the team decided to track, which is the same
// disclosure with the answers removed.
func redactEntryForShare(ctx context.Context, entry *domain.Entry) {
	sc := auth.ShareFromContext(ctx)
	if entry == nil || sc == nil {
		return
	}
	entry.Metadata = nil
	entry.MetaStatus = nil
	entry.MetaReport = nil
	entry.SpecVersion = 0
	// Which session produced a document is a session fact. On a link that
	// excludes sessions the id alone says one exists and what it did — the
	// same thing the graph's "produced" edge is withheld for.
	if !sc.Include.Sessions {
		entry.SessionID = ""
	}
}

func redactEntriesForShare(ctx context.Context, entries []*domain.Entry) {
	if auth.ShareFromContext(ctx) == nil {
		return
	}
	for _, e := range entries {
		redactEntryForShare(ctx, e)
	}
}

// SetQuestionRepo lets RebuildCrossRefs rescan the answers in a research.
// `[[...]]` is extracted from an answer on write, so a rebuild that could not
// read them left exactly those references broken while reporting success.
func (s *EntryService) SetQuestionRepo(r *storage.QuestionRepository) { s.questionRepo = r }
