package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/google/uuid"
)

type ExportService struct {
	research *ResearchService
	section  *SectionService
	entry    *EntryService
	entries  *storage.EntryRepository
	session  *SessionService
	task     *TaskService
	roadmap  *RoadmapService
	// annotations is optional; see SetAnnotations.
	annotations *storage.AnnotationRepository
	log         *slog.Logger
}

// exportAnnotations reads the marks on one document, without the identities
// that only mean something on this server.
func (s *ExportService) exportAnnotations(ctx context.Context, entryID string) []domain.ExportAnnotation {
	if s.annotations == nil {
		return nil
	}
	rows, err := s.annotations.FindByEntry(ctx, entryID)
	if err != nil || len(rows) == 0 {
		return nil
	}
	out := make([]domain.ExportAnnotation, 0, len(rows))
	for _, a := range rows {
		out = append(out, domain.ExportAnnotation{
			BlockID:    a.BlockID,
			Quote:      a.Quote,
			Kind:       a.Kind,
			Body:       a.Body,
			AuthorKind: a.AuthorKind,
			Status:     a.Status,
			Resolution: a.Resolution,
			Rejections: a.Rejections,
			Attempts:   a.Attempts,
			CreatedAt:  a.CreatedAt,
			UpdatedAt:  a.UpdatedAt,
		})
	}
	return out
}

// importAnnotations recreates the marks on a document that has just arrived.
//
// The anchor is resolved on the next read, against the document as imported —
// which is the honest outcome: a mark whose sentence survived the move is
// anchored, and one whose sentence did not is orphaned, exactly as it would be
// after any other rewrite. The anchored revision becomes 1 because that is the
// only revision an imported document has; pointing at a history that did not
// travel would be worse than pointing at the beginning.
func (s *ExportService) importAnnotations(ctx context.Context, entry *domain.Entry, marks []domain.ExportAnnotation) {
	if s.annotations == nil || len(marks) == 0 {
		return
	}
	for _, m := range marks {
		if strings.TrimSpace(m.Quote.Exact) == "" || !m.Kind.Valid() {
			continue
		}
		status := m.Status
		if !status.Valid() {
			status = domain.AnnotationOpen
		}
		author := m.AuthorKind
		if !author.Valid() {
			author = domain.AuthorHuman
		}
		a := &domain.Annotation{
			ID:               uuid.New().String(),
			ResearchID:       entry.ResearchID,
			EntryID:          entry.ID,
			BlockID:          m.BlockID,
			Quote:            m.Quote,
			AnchoredRevision: 1,
			Kind:             m.Kind,
			Body:             m.Body,
			AuthorKind:       author,
			Status:           status,
			Resolution:       m.Resolution,
			Rejections:       m.Rejections,
			Attempts:         m.Attempts,
		}
		if err := s.annotations.Create(ctx, a); err != nil {
			s.log.Warn("import annotation", "entry", entry.Code, "error", err)
		}
	}
}

// SetAnnotations lets a portable dump carry the marks people left.
//
// Optional for the same reason it is optional on EntryService: every existing
// caller and test builds this service without one, and a nil repository means
// "no annotations in the dump", which is what the dump has always contained.
func (s *ExportService) SetAnnotations(annotations *storage.AnnotationRepository) {
	s.annotations = annotations
}

func NewExportService(
	research *ResearchService,
	section *SectionService,
	entry *EntryService,
	entries *storage.EntryRepository,
	session *SessionService,
	task *TaskService,
	roadmap *RoadmapService,
	log *slog.Logger,
) *ExportService {
	return &ExportService{
		research: research, section: section,
		entry: entry, entries: entries,
		session: session, task: task,
		roadmap: roadmap, log: log,
	}
}

// Export collects all research data into a portable ExportData struct.
func (s *ExportService) Export(ctx context.Context, researchID string) (*domain.ExportData, error) {
	research, err := s.research.Get(ctx, researchID)
	if err != nil {
		return nil, fmt.Errorf("get research: %w", err)
	}

	sections, err := s.section.List(ctx, research.ID)
	if err != nil {
		return nil, fmt.Errorf("list sections: %w", err)
	}

	allEntries, err := s.entry.ListWithContent(ctx, research.ID)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	entriesBySection := make(map[string][]*domain.Entry)
	for _, e := range allEntries {
		entriesBySection[e.SectionID] = append(entriesBySection[e.SectionID], e)
	}

	sessions, err := s.session.ListByResearch(ctx, research.ID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	// Build session ID -> code map for entry linking
	sessionIDToCode := make(map[string]string)
	var exportSessions []domain.ExportSession
	for _, sess := range sessions {
		sessionIDToCode[sess.ID] = sess.Code
		qs, _ := s.session.ListQuestions(ctx, sess.ID, storage.QuestionFilter{})
		exportSessions = append(exportSessions, buildExportSession(sess, qs))
	}

	tasks, err := s.task.List(ctx, research.ID, storage.TaskFilter{})
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	// Roadmaps
	roadmaps, err := s.roadmap.List(ctx, research.ID)
	if err != nil {
		return nil, fmt.Errorf("list roadmaps: %w", err)
	}
	var exportRoadmaps []domain.ExportRoadmap
	for _, rm := range roadmaps {
		full, err := s.roadmap.Get(ctx, rm.ID)
		if err != nil {
			return nil, fmt.Errorf("get roadmap %s: %w", rm.ID, err)
		}
		exportRoadmaps = append(exportRoadmaps, buildExportRoadmap(full))
	}

	// Build sections with entries
	var exportSections []domain.ExportSection
	for _, sec := range sections {
		es := domain.ExportSection{
			Name:        sec.Name,
			DisplayName: sec.DisplayName,
			Description: sec.Description,
			Status:      sec.Status,
			Position:    sec.Position,
			Instruction: sec.Instruction,
			FieldSpec:   sec.FieldSpec,
			CreatedAt:   sec.CreatedAt,
			UpdatedAt:   sec.UpdatedAt,
		}
		for _, e := range entriesBySection[sec.ID] {
			ee := domain.ExportEntry{
				Title:       e.Title,
				Type:        e.Type,
				Content:     e.Content,
				Description: e.Description,
				Status:      e.Status,
				Tags:        e.Tags,
				Metadata:    e.Metadata,
				SessionCode: sessionIDToCode[e.SessionID],
				CreatedAt:   e.CreatedAt,
				UpdatedAt:   e.UpdatedAt,
				Annotations: s.exportAnnotations(ctx, e.ID),
			}
			es.Entries = append(es.Entries, ee)
		}
		exportSections = append(exportSections, es)
	}

	// Build tasks
	var exportTasks []domain.ExportTask
	for _, t := range tasks {
		exportTasks = append(exportTasks, domain.ExportTask{
			Title:       t.Title,
			Description: t.Description,
			Status:      t.Status,
			Priority:    t.Priority,
			Result:      t.Result,
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
			CompletedAt: t.CompletedAt,
		})
	}

	var privateSkills []domain.ExportPrivateSkill
	if auth.ShareFromContext(ctx) == nil {
		privateSkills, err = s.research.researches.ExportPrivateSkills(ctx, research.ID)
		if err != nil {
			return nil, fmt.Errorf("export private skills: %w", err)
		}
	}
	// Local IDs are not portable; session codes are remapped during import.
	memory := append(domain.Memory{}, research.Memory...)
	for i := range memory {
		memory[i].ID = ""
		memory[i].SessionID = ""
		memory[i].Version = 0
	}
	return &domain.ExportData{
		Version:    2,
		ExportedAt: time.Now().UTC(),
		Research: domain.ExportResearch{
			Name:          research.Name,
			Description:   research.Description,
			Goal:          research.Goal,
			Status:        research.Status,
			Memory:        memory,
			PrivateSkills: privateSkills,
			Tags:          research.Tags,
			CreatedAt:     research.CreatedAt,
			UpdatedAt:     research.UpdatedAt,
			Sections:      exportSections,
			Sessions:      exportSessions,
			Tasks:         exportTasks,
			Roadmaps:      exportRoadmaps,
		},
	}, nil
}

// Import creates a full research from ExportData.
// Returns the newly created research.
// Import rebuilds a research from an export file, in the caller's personal
// team unless teamID names another one they may write to.
func (s *ExportService) Import(ctx context.Context, data *domain.ExportData, teamID string) (*domain.Research, []string, error) {
	// What the file carried and this import could not. Not an error: an
	// import that refuses a whole research over one over-long note would be
	// the wrong trade. But a section that arrives without the convention its
	// documents were written under has to say so — nobody reads a silently
	// empty field.
	var warnings []string

	if data.Version != 1 && data.Version != 2 {
		return nil, warnings, fmt.Errorf("unsupported export version: %d", data.Version)
	}

	r := data.Research

	if err := validateImportEntries(r); err != nil {
		return nil, warnings, err
	}
	if err := validateImportProcess(r); err != nil {
		return nil, warnings, err
	}

	// Every entry this creates gets revision 1 attributed to the import rather
	// than to an agent that never wrote it. The imported file is where the text
	// came from, and a history that claims otherwise is worse than no history.
	ctx = WithAuthor(ctx, domain.AuthorImport)

	// 1. Create research with sections
	sectionReqs := make([]CreateSectionRequest, len(r.Sections))
	for i, sec := range r.Sections {
		// Dropped, never truncated — a half-instruction reads as a whole one.
		// Said out loud here rather than in the service, because this is the
		// only layer that knows the text came out of a file somebody can fix.
		if InstructionTooLong(sec.Instruction) {
			warnings = append(warnings, fmt.Sprintf(
				"section %q: its writing instruction is %d characters, over the %d-character limit, so the section was imported without one — shorten it in the file and set it with section_update",
				sec.Name, len([]rune(strings.TrimSpace(sec.Instruction))), domain.SectionInstructionMax))
		}
		sectionReqs[i] = CreateSectionRequest{
			Name:        sec.Name,
			DisplayName: sec.DisplayName,
			Description: sec.Description,
			Position:    sec.Position,
			Instruction: sec.Instruction,
			FieldSpec:   sec.FieldSpec,
		}
	}

	research, sections, err := s.research.Create(ctx, CreateResearchRequest{
		TeamID:      teamID,
		Name:        r.Name,
		Description: r.Description,
		Goal:        r.Goal,
		Tags:        r.Tags,
		Sections:    sectionReqs,
	})
	if err != nil {
		return nil, warnings, fmt.Errorf("create research: %w", err)
	}

	// Update research fields that Create doesn't set
	if r.Status != domain.ResearchActive {
		updateReq := UpdateResearchRequest{}
		if r.Status != domain.ResearchActive {
			updateReq.Status = &r.Status
		}
		if _, err := s.research.Update(ctx, research.ID, updateReq); err != nil {
			return nil, warnings, fmt.Errorf("update research fields: %w", err)
		}
	}

	// Build section name -> ID map
	sectionNameToID := make(map[string]string)
	for _, sec := range sections {
		sectionNameToID[sec.Name] = sec.ID
	}

	// Update section statuses (Create sets them all to Draft)
	for _, sec := range r.Sections {
		sectionID := sectionNameToID[sec.Name]
		if sec.Status != domain.SectionDraft {
			if _, err := s.section.Update(ctx, sectionID, UpdateSectionRequest{
				Status: &sec.Status,
			}); err != nil {
				// If section can't be marked completed (no entries yet), skip
				s.log.Warn("skip section status update", "section", sec.Name, "error", err)
			}
		}
	}

	// 2. Create sessions, build code -> ID map
	sessionCodeToID := make(map[string]string)
	for _, sess := range r.Sessions {
		questions := flattenQuestions(sess.Questions)

		session, _, err := s.session.Create(ctx, CreateSessionRequest{
			ResearchID: research.ID,
			Title:      sess.Title,
			Focus:      sess.Focus,
			Questions:  questions,
		})
		if err != nil {
			return nil, warnings, fmt.Errorf("create session %q: %w", sess.Title, err)
		}
		sessionCodeToID[sess.Code] = session.ID

		// Update session status/notes if needed
		if sess.Status != domain.SessionActive || sess.Notes != "" {
			updateReq := UpdateSessionRequest{}
			if sess.Status != domain.SessionActive {
				updateReq.Status = &sess.Status
			}
			if sess.Notes != "" {
				updateReq.Notes = &sess.Notes
			}
			if _, err := s.session.Update(ctx, session.ID, updateReq); err != nil {
				s.log.Warn("skip session update", "session", sess.Title, "error", err)
			}
		}

		// Update question statuses and answers
		swq, err := s.session.Get(ctx, session.ID)
		if err == nil {
			updateImportedQuestions(ctx, s.session, sess.Questions, swq.Questions)
		}
	}

	// Restore notes only after sessions exist. Never trust foreign-instance IDs.
	memory := append(domain.Memory{}, r.Memory...)
	for i := range memory {
		memory[i].SessionID = ""
		if memory[i].SessionCode != "" {
			memory[i].SessionID = sessionCodeToID[memory[i].SessionCode]
		}
	}
	if err := s.research.researches.ImportProcess(ctx, research.ID, r.Instruction, memory, r.PrivateSkills); err != nil {
		return nil, warnings, fmt.Errorf("import research memory and private skills: %w", err)
	}

	// 3. Create entries (ordered by section position to preserve code order)
	for _, sec := range r.Sections {
		sectionID := sectionNameToID[sec.Name]
		for _, e := range sec.Entries {
			sessionID := ""
			if e.SessionCode != "" {
				sessionID = sessionCodeToID[e.SessionCode]
			}

			created, err := s.entry.Create(ctx, CreateEntryRequest{
				ResearchID:  research.ID,
				SectionID:   sectionID,
				SessionID:   sessionID,
				Type:        e.Type,
				Title:       e.Title,
				Content:     e.Content,
				Description: e.Description,
				Status:      e.Status,
				Tags:        e.Tags,
				Metadata:    e.Metadata,
			})
			if err != nil {
				return nil, warnings, fmt.Errorf("create entry %q: %w", e.Title, err)
			}
			// After the document, because a mark needs the entry it marks. A
			// failure here is logged and skipped rather than failing the import:
			// losing the queue is bad, losing the research is worse.
			s.importAnnotations(ctx, created, e.Annotations)
		}
	}

	// 4. Create tasks
	for _, t := range r.Tasks {
		task, err := s.task.Create(ctx, CreateTaskRequest{
			ResearchID:  research.ID,
			Title:       t.Title,
			Description: t.Description,
			Priority:    t.Priority,
		})
		if err != nil {
			return nil, warnings, fmt.Errorf("create task %q: %w", t.Title, err)
		}

		// Update status/result if not default
		if t.Status != domain.TaskPending || t.Result != "" {
			updateReq := UpdateTaskRequest{}
			if t.Status != domain.TaskPending {
				updateReq.Status = &t.Status
			}
			if t.Result != "" {
				updateReq.Result = &t.Result
			}
			if _, err := s.task.Update(ctx, task.ID, updateReq); err != nil {
				s.log.Warn("skip task update", "task", t.Title, "error", err)
			}
		}
	}

	// 5. Create roadmaps
	for _, rm := range r.Roadmaps {
		nodeReqs, edgeReqs := buildRoadmapRequests(rm)
		if _, err := s.roadmap.Create(ctx, CreateRoadmapRequest{
			ResearchID:  research.ID,
			Title:       rm.Title,
			Description: rm.Description,
			Statuses:    rm.Statuses,
			Nodes:       nodeReqs,
			Edges:       edgeReqs,
		}); err != nil {
			return nil, warnings, fmt.Errorf("create roadmap %q: %w", rm.Title, err)
		}
	}

	// 6. Rebuild cross-references (entries now have codes assigned)
	if _, err := s.entry.RebuildCrossRefs(ctx, research.ID); err != nil {
		s.log.Warn("rebuild crossrefs failed", "error", err)
	}

	// Re-read research to get final state
	out, err := s.research.Get(ctx, research.ID)
	return out, warnings, err
}

// validateImportEntries rejects entries EntryService.Create would refuse, before
// Import writes anything. Import is not transactional: it creates the research,
// its sections, then sessions and questions before the first entry, so an entry
// that fails validation halfway through leaves a research with a code, some of
// its entries and none of its tasks or roadmaps — debris nothing in the UI marks
// as partial. The checks mirror Create's own, which is the point.
func validateImportEntries(r domain.ExportResearch) error {
	for _, sec := range r.Sections {
		for _, e := range sec.Entries {
			if e.Type != "" && !e.Type.Valid() {
				return fmt.Errorf("entry %q: invalid entry_type %q: want %q, %q or %q",
					e.Title, e.Type, domain.EntryMarkdown, domain.EntryBlocks, domain.EntryArtifact)
			}
			if strings.TrimSpace(e.Content) == "" {
				return fmt.Errorf("entry %q in section %q has no content", e.Title, sec.Name)
			}

			switch e.Type {
			case domain.EntryBlocks:
				// normalizeContent is deliberately NOT applied here: it expands a
				// literal \n, which inside the document's JSON strings would make it
				// unparseable and turn a valid entry into an import failure.
				doc, err := ParseStoredBlockDocument(e.Content)
				if err != nil {
					return fmt.Errorf("entry %q in section %q: %w", e.Title, sec.Name, err)
				}
				if normalizeTitle(e.Title) == "" && BlockDocumentTitle(doc) == "" {
					return fmt.Errorf("blocks entry in section %q has no title and its document has no heading to take one from", sec.Name)
				}

			case domain.EntryArtifact:
				// An artifact cannot get a title from its content the markdown way, so
				// Create insists on one — either the field or the document's <title>.
				content := normalizeContent(e.Content)
				if normalizeTitle(e.Title) == "" && normalizeTitle(htmlTitle(content)) == "" {
					return fmt.Errorf("artifact entry in section %q has no title and its HTML has no <title>", sec.Name)
				}

			default:
				if normalizeContent(e.Content) == "" {
					return fmt.Errorf("entry %q in section %q has no content", e.Title, sec.Name)
				}
			}
		}
	}
	return nil
}

// buildExportSession converts a session + questions into export format.
func buildExportSession(sess *domain.Session, questions []*domain.Question) domain.ExportSession {
	// Build question hierarchy
	childrenOf := make(map[string][]*domain.Question)
	var roots []*domain.Question
	for _, q := range questions {
		if q.ParentID == "" {
			roots = append(roots, q)
		} else {
			childrenOf[q.ParentID] = append(childrenOf[q.ParentID], q)
		}
	}

	var buildQ func(q *domain.Question) domain.ExportQuestion
	buildQ = func(q *domain.Question) domain.ExportQuestion {
		eq := domain.ExportQuestion{
			Text:      q.Text,
			Area:      q.Area,
			Rationale: q.Rationale,
			Priority:  q.Priority,
			Status:    q.Status,
			Answer:    q.Answer,
			Position:  q.Position,
			CreatedAt: q.CreatedAt,
			UpdatedAt: q.UpdatedAt,
		}
		for _, child := range childrenOf[q.ID] {
			eq.Children = append(eq.Children, buildQ(child))
		}
		return eq
	}

	var exportQs []domain.ExportQuestion
	for _, q := range roots {
		exportQs = append(exportQs, buildQ(q))
	}

	return domain.ExportSession{
		Code:      sess.Code,
		Title:     sess.Title,
		Focus:     sess.Focus,
		Status:    sess.Status,
		Notes:     sess.Notes,
		CreatedAt: sess.CreatedAt,
		UpdatedAt: sess.UpdatedAt,
		Questions: exportQs,
	}
}

// buildExportRoadmap converts a roadmap with nodes/edges into export format.
func buildExportRoadmap(rm *domain.Roadmap) domain.ExportRoadmap {
	nodeIDToCode := make(map[string]string)
	var exportNodes []domain.ExportRoadmapNode
	for _, n := range rm.Nodes {
		nodeIDToCode[n.ID] = n.Code
		en := domain.ExportRoadmapNode{
			Code:        n.Code,
			Title:       n.Title,
			Description: n.Description,
			NodeType:    n.NodeType,
			Status:      n.Status,
			PositionX:   n.PositionX,
			PositionY:   n.PositionY,
			CreatedAt:   n.CreatedAt,
			UpdatedAt:   n.UpdatedAt,
		}
		if n.ParentID != "" {
			en.ParentCode = nodeIDToCode[n.ParentID]
		}
		exportNodes = append(exportNodes, en)
	}

	var exportEdges []domain.ExportRoadmapEdge
	for _, e := range rm.Edges {
		exportEdges = append(exportEdges, domain.ExportRoadmapEdge{
			SourceCode: nodeIDToCode[e.SourceNodeID],
			TargetCode: nodeIDToCode[e.TargetNodeID],
			Label:      e.Label,
			EdgeType:   e.EdgeType,
		})
	}

	return domain.ExportRoadmap{
		Title:       rm.Title,
		Description: rm.Description,
		Statuses:    rm.Statuses,
		Status:      rm.Status,
		CreatedAt:   rm.CreatedAt,
		UpdatedAt:   rm.UpdatedAt,
		Nodes:       exportNodes,
		Edges:       exportEdges,
	}
}

// flattenQuestions converts hierarchical questions to flat list for session.Create.
// Only root-level questions are passed; children are handled separately via AddQuestions.
func flattenQuestions(questions []domain.ExportQuestion) []CreateQuestionRequest {
	var reqs []CreateQuestionRequest
	for _, q := range questions {
		reqs = append(reqs, CreateQuestionRequest{
			Text:      q.Text,
			Area:      q.Area,
			Rationale: q.Rationale,
			Priority:  q.Priority,
			Position:  q.Position,
		})
	}
	return reqs
}

// updateImportedQuestions updates question statuses and answers after import,
// and creates child questions recursively.
func updateImportedQuestions(ctx context.Context, sessionSvc *SessionService, exported []domain.ExportQuestion, created []*domain.Question) {
	// Match by position order
	for i, eq := range exported {
		if i >= len(created) {
			break
		}
		cq := created[i]

		// Update status and answer
		if eq.Status != domain.QuestionPending || eq.Answer != "" {
			status := eq.Status
			var answer *string
			if eq.Answer != "" {
				answer = &eq.Answer
			}
			sessionSvc.UpdateQuestion(ctx, cq.ID, &status, answer)
		}

		// Create children if any
		if len(eq.Children) > 0 {
			var childReqs []CreateQuestionRequest
			for _, child := range eq.Children {
				childReqs = append(childReqs, CreateQuestionRequest{
					Text:      child.Text,
					Area:      child.Area,
					Rationale: child.Rationale,
					Priority:  child.Priority,
					Position:  child.Position,
					ParentID:  cq.ID,
				})
			}
			childQs, err := sessionSvc.AddQuestions(ctx, cq.SessionID, childReqs)
			if err == nil {
				// Recursively update children
				updateImportedQuestions(ctx, sessionSvc, eq.Children, childQs)
			}
		}
	}
}

// buildRoadmapRequests converts export roadmap data to create requests.
func buildRoadmapRequests(rm domain.ExportRoadmap) ([]CreateRoadmapNodeRequest, []CreateRoadmapEdgeRequest) {
	// Use codes as temp IDs for edge resolution
	var nodeReqs []CreateRoadmapNodeRequest
	for _, n := range rm.Nodes {
		nodeReqs = append(nodeReqs, CreateRoadmapNodeRequest{
			TempID:      n.Code,
			Title:       n.Title,
			Description: n.Description,
			NodeType:    n.NodeType,
			Status:      n.Status,
			PositionX:   n.PositionX,
			PositionY:   n.PositionY,
			ParentID:    "", // resolved after creation via parent_code
		})
	}

	var edgeReqs []CreateRoadmapEdgeRequest
	for _, e := range rm.Edges {
		edgeReqs = append(edgeReqs, CreateRoadmapEdgeRequest{
			SourceNodeRef: e.SourceCode,
			TargetNodeRef: e.TargetCode,
			Label:         e.Label,
			EdgeType:      e.EdgeType,
		})
	}

	return nodeReqs, edgeReqs
}
