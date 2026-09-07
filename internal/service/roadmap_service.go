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

// --- Request DTOs ---

type CreateRoadmapNodeRequest struct {
	TempID      string // Client-provided temp ID for edge references during bulk create
	Title       string
	Description string
	NodeType    string
	Status      string
	PositionX   float64
	PositionY   float64
	ParentID    string
	RefType     string // Reference type: entry, task, session, research, question
	RefID       string // ID of the referenced entity
	Metadata    string // JSON blob for node-type-specific data
	Stage       string // Stage/column for the stages view
	NodeDate    string // ISO YYYY-MM-DD for the timeline view (or empty)
	NodeEndDate string // ISO YYYY-MM-DD end of a timeline range (or empty)
}

type CreateRoadmapEdgeRequest struct {
	SourceNodeRef string // TempID or real node ID
	TargetNodeRef string // TempID or real node ID
	Label         string
	EdgeType      string
}

type CreateRoadmapRequest struct {
	ResearchID  string
	Title       string
	Description string
	Statuses    []string
	Stages      []string
	View        string
	Nodes       []CreateRoadmapNodeRequest
	Edges       []CreateRoadmapEdgeRequest
}

type UpdateRoadmapRequest struct {
	Title       *string
	Description *string
	Statuses    []string
	Stages      []string
	View        *string
	Status      *domain.RoadmapStatus
}

type UpdateRoadmapNodeRequest struct {
	Title       *string
	Description *string
	NodeType    *string
	Status      *string
	PositionX   *float64
	PositionY   *float64
	ParentID    *string
	RefType     *string
	RefID       *string
	Metadata    *string
	Stage       *string
	NodeDate    *string
	NodeEndDate *string
}

// --- Service ---

type RoadmapService struct {
	roadmaps   *storage.RoadmapRepository
	nodes      *storage.RoadmapNodeRepository
	edges      *storage.RoadmapEdgeRepository
	researches *storage.ResearchRepository
	access     *Access
	events     EventNotifier
	log        *slog.Logger
	// Optional repos for reference resolution (set via SetRefResolvers)
	entries   *storage.EntryRepository
	tasks     *storage.TaskRepository
	sessions  *storage.SessionRepository
	questions *storage.QuestionRepository
	sections  *storage.SectionRepository
	// dangling repairs references that named a roadmap or node code before it
	// existed. Optional, set after construction.
	dangling DanglingResolver
}

func NewRoadmapService(
	roadmaps *storage.RoadmapRepository,
	nodes *storage.RoadmapNodeRepository,
	edges *storage.RoadmapEdgeRepository,
	researches *storage.ResearchRepository,
	access *Access,
	events EventNotifier,
	log *slog.Logger,
) *RoadmapService {
	return &RoadmapService{
		roadmaps:   roadmaps,
		nodes:      nodes,
		edges:      edges,
		researches: researches,
		access:     access,
		events:     events,
		log:        log,
	}
}

// SetRefResolvers injects repositories needed for resolving node references.
func (s *RoadmapService) SetRefResolvers(
	entries *storage.EntryRepository,
	tasks *storage.TaskRepository,
	sessions *storage.SessionRepository,
	questions *storage.QuestionRepository,
	sections *storage.SectionRepository,
) {
	s.entries = entries
	s.tasks = tasks
	s.sessions = sessions
	s.questions = questions
	s.sections = sections
}

// Create creates a roadmap with initial nodes and edges in one call.
func (s *RoadmapService) Create(ctx context.Context, req CreateRoadmapRequest) (*domain.Roadmap, error) {
	if err := s.access.Write(ctx, req.ResearchID); err != nil {
		return nil, fmt.Errorf("research %s: %w", req.ResearchID, err)
	}

	statuses := req.Statuses
	if statuses == nil {
		statuses = []string{}
	}
	stages := req.Stages
	if stages == nil {
		stages = []string{}
	}
	view, err := normalizeRoadmapView(req.View)
	if err != nil {
		return nil, err
	}

	rm := &domain.Roadmap{
		ID:          uuid.New().String(),
		ResearchID:  req.ResearchID,
		Title:       normalizeTitle(req.Title),
		Description: normalizeContent(req.Description),
		Statuses:    statuses,
		Stages:      stages,
		View:        view,
		Status:      domain.RoadmapActive,
	}

	if err := s.roadmaps.Create(ctx, rm); err != nil {
		return nil, fmt.Errorf("create roadmap: %w", err)
	}

	nodes, edges, err := s.createNodesAndEdges(ctx, rm, req.Nodes, req.Edges)
	if err != nil {
		return nil, err
	}

	rm.Nodes = nodes
	rm.Edges = edges

	// A roadmap and its nodes both carry codes that references name.
	s.resolveDangling(ctx, rm, nodes, true)
	emit(ctx, s.events, Event{Type: "roadmap.created", ResearchID: rm.ResearchID, EntityID: rm.ID, Entity: "roadmap"})
	return rm, nil
}

// Get returns a roadmap with all nodes and edges. Accepts UUID or short code (e.g. RM1).
func (s *RoadmapService) Get(ctx context.Context, id string) (*domain.Roadmap, error) {
	rm, err := s.roadmaps.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find roadmap: %w", err)
	}
	if rm == nil && isCode(id) {
		rm, err = s.roadmaps.FindByCode(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("find roadmap by code: %w", err)
		}
	}
	if rm == nil {
		return nil, ErrNotFound
	}
	if err := s.access.Read(ctx, rm.ResearchID); err != nil {
		return nil, ErrNotFound
	}

	rm.Nodes, err = s.nodes.FindByRoadmap(ctx, rm.ID)
	if err != nil {
		return nil, fmt.Errorf("find nodes: %w", err)
	}
	rm.Edges, err = s.edges.FindByRoadmap(ctx, rm.ID)
	if err != nil {
		return nil, fmt.Errorf("find edges: %w", err)
	}

	// Resolve references (lazy sync)
	s.resolveNodeRefs(ctx, rm.Nodes)

	return rm, nil
}

// GetByIDOrCode returns a roadmap scoped to a research. Accepts UUID or short code (e.g. RM1).
// Validates that the roadmap belongs to the given research.
func (s *RoadmapService) GetByIDOrCode(ctx context.Context, researchID, idOrCode string) (*domain.Roadmap, error) {
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, ErrNotFound
	}

	rm, err := s.roadmaps.FindByID(ctx, idOrCode)
	if err != nil {
		return nil, fmt.Errorf("find roadmap: %w", err)
	}
	if rm == nil && isCode(idOrCode) {
		rm, err = s.roadmaps.FindByCodeAndResearch(ctx, idOrCode, researchID)
		if err != nil {
			return nil, fmt.Errorf("find roadmap by code: %w", err)
		}
	}
	if rm == nil {
		return nil, ErrNotFound
	}
	if rm.ResearchID != researchID {
		return nil, ErrNotFound
	}

	rm.Nodes, err = s.nodes.FindByRoadmap(ctx, rm.ID)
	if err != nil {
		return nil, fmt.Errorf("find nodes: %w", err)
	}
	rm.Edges, err = s.edges.FindByRoadmap(ctx, rm.ID)
	if err != nil {
		return nil, fmt.Errorf("find edges: %w", err)
	}

	s.resolveNodeRefs(ctx, rm.Nodes)

	return rm, nil
}

// resolveNodeRefs populates RefData for nodes that have ref_type/ref_id set.
func (s *RoadmapService) resolveNodeRefs(ctx context.Context, nodes []*domain.RoadmapNode) {
	for _, node := range nodes {
		if node.RefType == "" || node.RefID == "" {
			continue
		}
		refData := s.resolveRef(ctx, node.RefType, node.RefID)
		if refData != nil {
			node.RefData = refData
		}
	}
}

// refAllowed reports whether the caller may be shown data from researchID.
// A node lives in the caller's own roadmap, but the entity it points at can live
// in someone else's research: resolving without this check handed out another
// user's titles, statuses and a 200-character content preview.
func (s *RoadmapService) refAllowed(ctx context.Context, researchID string) bool {
	if researchID == "" {
		return false
	}
	return s.access.Read(ctx, researchID) == nil
}

// refTypeShared reports whether a share link is allowed to be shown a ref of
// this kind.
//
// A roadmap node is a pointer, and resolving it inlines the thing it points at:
// a task node carries the task's `result`, a question node its `answer`, a
// session node its title and progress. Those are exactly the fields the
// `tasks` and `sessions` include flags exist to withhold — so a link that
// switched sessions off but left roadmaps on was handing over the interview
// through the graph, having refused it on the route built to serve it.
//
// The route gate in share_routes.go cannot catch this: it knows the request is
// for a roadmap and nothing about what the roadmap points at.
func refTypeShared(ctx context.Context, refType domain.RoadmapNodeRefType) bool {
	sc := auth.ShareFromContext(ctx)
	if sc == nil {
		return true
	}
	switch refType {
	case domain.RefTypeTask:
		return sc.Include.Tasks
	case domain.RefTypeSession, domain.RefTypeQuestion:
		return sc.Include.Sessions
	}
	return true
}

func (s *RoadmapService) resolveRef(ctx context.Context, refType, refID string) *domain.RoadmapNodeRefData {
	if !refTypeShared(ctx, domain.RoadmapNodeRefType(refType)) {
		return nil
	}
	switch domain.RoadmapNodeRefType(refType) {
	case domain.RefTypeEntry:
		return s.resolveEntryRef(ctx, refID)
	case domain.RefTypeTask:
		return s.resolveTaskRef(ctx, refID)
	case domain.RefTypeSession:
		return s.resolveSessionRef(ctx, refID)
	case domain.RefTypeResearch:
		return s.resolveResearchRef(ctx, refID)
	case domain.RefTypeQuestion:
		return s.resolveQuestionRef(ctx, refID)
	default:
		return nil
	}
}

func (s *RoadmapService) resolveEntryRef(ctx context.Context, id string) *domain.RoadmapNodeRefData {
	if s.entries == nil {
		return nil
	}
	entry, err := s.entries.FindByID(ctx, id)
	if err != nil || entry == nil {
		return nil
	}
	if !s.refAllowed(ctx, entry.ResearchID) {
		return nil
	}
	data := &domain.RoadmapNodeRefData{
		Title:       entry.Title,
		Status:      string(entry.Status),
		Code:        entry.Code,
		Description: entry.Description,
		ResearchID:  entry.ResearchID,
	}
	// Include content preview (first 200 characters — by rune, so a preview that
	// stops mid-character does not ship half of one).
	//
	// A blocks document is stored as JSON, and 200 characters of that is
	// `{"version":1,"blocks":[{"id":"…` — the roadmap card for an HTML artifact
	// showed exactly that. The markdown rendering is what a person would call
	// its content; an html block is named there rather than inlined.
	preview := entry.Content
	if entry.Type == domain.EntryBlocks {
		if doc, err := ParseStoredBlockDocument(entry.Content); err == nil {
			preview = strings.TrimSpace(BlockDocumentToMarkdown(doc))
		} else {
			preview = entry.Description
		}
	}
	if runes := []rune(preview); len(runes) > 200 {
		data.Content = string(runes[:200]) + "..."
	} else {
		data.Content = preview
	}
	// Resolve section name
	if s.sections != nil && entry.SectionID != "" {
		section, err := s.sections.FindByID(ctx, entry.SectionID)
		if err == nil && section != nil {
			data.SectionName = section.DisplayName
			if data.SectionName == "" {
				data.SectionName = section.Name
			}
		}
	}
	return data
}

func (s *RoadmapService) resolveTaskRef(ctx context.Context, id string) *domain.RoadmapNodeRefData {
	if s.tasks == nil {
		return nil
	}
	task, err := s.tasks.FindByID(ctx, id)
	if err != nil || task == nil {
		return nil
	}
	if !s.refAllowed(ctx, task.ResearchID) {
		return nil
	}
	return &domain.RoadmapNodeRefData{
		Title:      task.Title,
		Status:     string(task.Status),
		Code:       task.Code,
		ResearchID: task.ResearchID,
		Priority:   string(task.Priority),
		Result:     task.Result,
	}
}

func (s *RoadmapService) resolveSessionRef(ctx context.Context, id string) *domain.RoadmapNodeRefData {
	if s.sessions == nil {
		return nil
	}
	session, err := s.sessions.FindByID(ctx, id)
	if err != nil || session == nil {
		return nil
	}
	if !s.refAllowed(ctx, session.ResearchID) {
		return nil
	}
	data := &domain.RoadmapNodeRefData{
		Title:       session.Title,
		Status:      string(session.Status),
		Code:        session.Code,
		Description: session.Focus,
		ResearchID:  session.ResearchID,
	}
	// Count questions
	if s.questions != nil {
		questions, err := s.questions.FindBySession(ctx, id, storage.QuestionFilter{})
		if err == nil {
			data.TotalQuestions = len(questions)
			for _, q := range questions {
				if q.Answer != "" {
					data.AnsweredQuestions++
				}
			}
		}
	}
	return data
}

func (s *RoadmapService) resolveResearchRef(ctx context.Context, id string) *domain.RoadmapNodeRefData {
	research, err := s.researches.FindByID(ctx, id)
	if err != nil || research == nil {
		return nil
	}
	if !s.refAllowed(ctx, research.ID) {
		return nil
	}
	data := &domain.RoadmapNodeRefData{
		Title:       research.Name,
		Status:      string(research.Status),
		Code:        research.Code,
		Description: research.Description,
		ResearchID:  research.ID,
	}
	// Count sections
	if s.sections != nil {
		sections, err := s.sections.FindByResearch(ctx, id)
		if err == nil {
			data.SectionCount = len(sections)
		}
	}
	// Count entries
	if s.entries != nil {
		entries, err := s.entries.FindByResearch(ctx, id, storage.EntryFilter{})
		if err == nil {
			data.EntryCount = len(entries)
		}
	}
	return data
}

func (s *RoadmapService) resolveQuestionRef(ctx context.Context, id string) *domain.RoadmapNodeRefData {
	if s.questions == nil {
		return nil
	}
	q, err := s.questions.FindByID(ctx, id)
	if err != nil || q == nil {
		return nil
	}
	// A question owns no research id; its session does.
	if s.sessions == nil {
		return nil
	}
	session, err := s.sessions.FindByID(ctx, q.SessionID)
	if err != nil || session == nil || !s.refAllowed(ctx, session.ResearchID) {
		return nil
	}
	return &domain.RoadmapNodeRefData{
		Title:       q.Text,
		Status:      string(q.Status),
		Code:        q.Code,
		Description: q.Answer,
	}
}

// List returns all roadmaps for a research (without nodes/edges).
func (s *RoadmapService) List(ctx context.Context, researchID string) ([]*domain.Roadmap, error) {
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}
	return s.roadmaps.FindByResearch(ctx, researchID)
}

// Update updates roadmap metadata.
func (s *RoadmapService) Update(ctx context.Context, id string, req UpdateRoadmapRequest) (*domain.Roadmap, error) {
	rm, err := s.roadmaps.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find roadmap: %w", err)
	}
	if rm == nil {
		return nil, ErrNotFound
	}
	if err := s.access.Write(ctx, rm.ResearchID); err != nil {
		return nil, err
	}

	if req.Title != nil {
		rm.Title = normalizeTitle(*req.Title)
	}
	if req.Description != nil {
		rm.Description = normalizeContent(*req.Description)
	}
	if req.Statuses != nil {
		rm.Statuses = req.Statuses
	}
	if req.Stages != nil {
		rm.Stages = req.Stages
	}
	if req.View != nil {
		view, err := normalizeRoadmapView(*req.View)
		if err != nil {
			return nil, err
		}
		rm.View = view
	}
	if req.Status != nil {
		rm.Status = *req.Status
	}

	if err := s.roadmaps.Update(ctx, rm); err != nil {
		return nil, fmt.Errorf("update roadmap: %w", err)
	}

	emit(ctx, s.events, Event{Type: "roadmap.updated", ResearchID: rm.ResearchID, EntityID: rm.ID, Entity: "roadmap"})
	return rm, nil
}

// Delete removes a roadmap and all its nodes/edges.
func (s *RoadmapService) Delete(ctx context.Context, id string) error {
	rm, err := s.roadmaps.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find roadmap: %w", err)
	}
	if rm == nil {
		return ErrNotFound
	}
	if err := s.access.Write(ctx, rm.ResearchID); err != nil {
		return err
	}

	if err := s.roadmaps.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete roadmap: %w", err)
	}

	emit(ctx, s.events, Event{Type: "roadmap.deleted", ResearchID: rm.ResearchID, EntityID: id, Entity: "roadmap"})
	return nil
}

// AddNodes adds new nodes and edges to an existing roadmap.
func (s *RoadmapService) AddNodes(ctx context.Context, roadmapID string, nodeReqs []CreateRoadmapNodeRequest, edgeReqs []CreateRoadmapEdgeRequest) (*domain.Roadmap, error) {
	rm, err := s.roadmaps.FindByID(ctx, roadmapID)
	if err != nil {
		return nil, fmt.Errorf("find roadmap: %w", err)
	}
	if rm == nil {
		return nil, ErrNotFound
	}
	if err := s.access.Write(ctx, rm.ResearchID); err != nil {
		return nil, err
	}

	created, _, err := s.createNodesAndEdges(ctx, rm, nodeReqs, edgeReqs)
	if err != nil {
		return nil, err
	}

	// The roadmap is not new here, only its nodes.
	s.resolveDangling(ctx, rm, created, false)
	emit(ctx, s.events, Event{Type: "roadmap.updated", ResearchID: rm.ResearchID, EntityID: rm.ID, Entity: "roadmap"})

	// Return full roadmap
	return s.Get(ctx, roadmapID)
}

// nodeRefs answers what a node reference in a bulk request may name: a temp_id
// declared in the same request, or the id of a node already in this roadmap.
// Anything else is refused before a row is written — a bare id used to be
// trusted, which let an edge or a parent link point into another research.
type nodeRefs struct {
	roadmapID string
	declared  map[string]bool   // temp ids declared by the request
	created   map[string]string // temp id -> id of the node created for it
}

func (s *RoadmapService) checkNodeRefs(ctx context.Context, rm *domain.Roadmap, nodeReqs []CreateRoadmapNodeRequest, edgeReqs []CreateRoadmapEdgeRequest) (*nodeRefs, error) {
	refs := &nodeRefs{roadmapID: rm.ID, declared: map[string]bool{}, created: map[string]string{}}
	for _, nr := range nodeReqs {
		if nr.TempID != "" {
			refs.declared[nr.TempID] = true
		}
	}
	check := func(what, ref string) error {
		if ref == "" {
			return fmt.Errorf("%s needs a node reference: %w", what, ErrValidation)
		}
		if refs.declared[ref] {
			return nil
		}
		node, err := s.nodes.FindByID(ctx, ref)
		if err != nil {
			return fmt.Errorf("find node %s: %w", ref, err)
		}
		if node == nil || node.RoadmapID != rm.ID {
			return fmt.Errorf("%s node %s: %w", what, ref, ErrNotFound)
		}
		return nil
	}
	for _, nr := range nodeReqs {
		if nr.ParentID == "" {
			continue
		}
		if nr.TempID != "" && nr.ParentID == nr.TempID {
			return nil, fmt.Errorf("node %q cannot be its own parent: %w", nr.Title, ErrValidation)
		}
		if err := check("parent", nr.ParentID); err != nil {
			return nil, err
		}
	}
	for _, er := range edgeReqs {
		if err := check("edge source", er.SourceNodeRef); err != nil {
			return nil, err
		}
		if err := check("edge target", er.TargetNodeRef); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

// resolve maps a checked reference to a node id. A temp id whose node has not
// been created yet resolves to "" and the caller fills it in afterwards.
func (r *nodeRefs) resolve(ref string) string {
	if r.declared[ref] {
		return r.created[ref]
	}
	return ref
}

// createNodesAndEdges is the body shared by Create and AddNodes. Every
// reference is checked before the first insert, so a request that names a node
// outside the roadmap creates nothing rather than half of itself.
func (s *RoadmapService) createNodesAndEdges(ctx context.Context, rm *domain.Roadmap, nodeReqs []CreateRoadmapNodeRequest, edgeReqs []CreateRoadmapEdgeRequest) ([]*domain.RoadmapNode, []*domain.RoadmapEdge, error) {
	refs, err := s.checkNodeRefs(ctx, rm, nodeReqs, edgeReqs)
	if err != nil {
		return nil, nil, err
	}

	var nodes []*domain.RoadmapNode
	type pendingParent struct {
		node   *domain.RoadmapNode
		parent string
	}
	var pending []pendingParent
	for _, nr := range nodeReqs {
		nodeType := nr.NodeType
		if nodeType == "" {
			nodeType = "step"
		}
		nodeDate, nodeEnd, err := normalizeNodeRange(nr.NodeDate, nr.NodeEndDate)
		if err != nil {
			return nil, nil, fmt.Errorf("node %q: %w", nr.Title, err)
		}
		node := &domain.RoadmapNode{
			ID:          uuid.New().String(),
			RoadmapID:   rm.ID,
			Title:       normalizeTitle(nr.Title),
			Description: normalizeContent(nr.Description),
			NodeType:    nodeType,
			Status:      nr.Status,
			PositionX:   nr.PositionX,
			PositionY:   nr.PositionY,
			ParentID:    refs.resolve(nr.ParentID),
			RefType:     nr.RefType,
			RefID:       nr.RefID,
			Metadata:    nr.Metadata,
			Stage:       nr.Stage,
			NodeDate:    nodeDate,
			NodeEndDate: nodeEnd,
		}
		if err := s.nodes.Create(ctx, node); err != nil {
			return nil, nil, fmt.Errorf("create node %q: %w", nr.Title, err)
		}
		if nr.TempID != "" {
			refs.created[nr.TempID] = node.ID
		}
		if nr.ParentID != "" && node.ParentID == "" {
			// The parent is declared later in the same request.
			pending = append(pending, pendingParent{node: node, parent: nr.ParentID})
		}
		nodes = append(nodes, node)
	}
	for _, pp := range pending {
		pp.node.ParentID = refs.resolve(pp.parent)
		if err := s.nodes.Update(ctx, pp.node); err != nil {
			return nil, nil, fmt.Errorf("set parent of %q: %w", pp.node.Title, err)
		}
	}

	var edges []*domain.RoadmapEdge
	for _, er := range edgeReqs {
		edgeType := er.EdgeType
		if edgeType == "" {
			edgeType = "default"
		}
		edge := &domain.RoadmapEdge{
			ID:           uuid.New().String(),
			RoadmapID:    rm.ID,
			SourceNodeID: refs.resolve(er.SourceNodeRef),
			TargetNodeID: refs.resolve(er.TargetNodeRef),
			Label:        normalizeTitle(er.Label),
			EdgeType:     edgeType,
		}
		if err := s.edges.Create(ctx, edge); err != nil {
			return nil, nil, fmt.Errorf("create edge: %w", err)
		}
		edges = append(edges, edge)
	}
	return nodes, edges, nil
}

// UpdateNode updates a single node.
func (s *RoadmapService) UpdateNode(ctx context.Context, nodeID string, req UpdateRoadmapNodeRequest) (*domain.RoadmapNode, error) {
	node, err := s.nodes.FindByID(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("find node: %w", err)
	}
	if node == nil {
		return nil, ErrNotFound
	}

	// Validate access via roadmap -> research
	rm, err := s.roadmaps.FindByID(ctx, node.RoadmapID)
	if err != nil {
		return nil, fmt.Errorf("find roadmap: %w", err)
	}
	if rm == nil {
		return nil, ErrNotFound
	}
	if err := s.access.Write(ctx, rm.ResearchID); err != nil {
		return nil, err
	}

	if req.Title != nil {
		node.Title = normalizeTitle(*req.Title)
	}
	if req.Description != nil {
		node.Description = normalizeContent(*req.Description)
	}
	if req.NodeType != nil {
		node.NodeType = *req.NodeType
	}
	if req.Status != nil {
		node.Status = *req.Status
	}
	if req.PositionX != nil {
		node.PositionX = *req.PositionX
	}
	if req.PositionY != nil {
		node.PositionY = *req.PositionY
	}
	if req.ParentID != nil {
		// The parent is a node id the caller supplies, and the only thing it may
		// name is another node of this roadmap: the same rule as removal.
		if *req.ParentID != "" {
			if *req.ParentID == node.ID {
				return nil, fmt.Errorf("node %s cannot be its own parent: %w", node.ID, ErrValidation)
			}
			parent, err := s.nodes.FindByID(ctx, *req.ParentID)
			if err != nil {
				return nil, fmt.Errorf("find parent: %w", err)
			}
			if parent == nil || parent.RoadmapID != node.RoadmapID {
				return nil, fmt.Errorf("parent node %s: %w", *req.ParentID, ErrNotFound)
			}
		}
		node.ParentID = *req.ParentID
	}
	if req.RefType != nil {
		node.RefType = *req.RefType
	}
	if req.RefID != nil {
		node.RefID = *req.RefID
	}
	if req.Metadata != nil {
		node.Metadata = *req.Metadata
	}
	if req.Stage != nil {
		node.Stage = *req.Stage
	}
	// Apply the two dates, then validate the resulting range as a whole — a
	// caller may set only the start, only the end, or both, and end-before-start
	// has to be caught against the final pair, not each field in isolation.
	if req.NodeDate != nil {
		nodeDate, err := normalizeNodeDate(*req.NodeDate)
		if err != nil {
			return nil, err
		}
		node.NodeDate = nodeDate
	}
	if req.NodeEndDate != nil {
		nodeEnd, err := normalizeNodeDate(*req.NodeEndDate)
		if err != nil {
			return nil, ErrInvalidNodeEndDate
		}
		node.NodeEndDate = nodeEnd
	}
	if err := validateNodeRange(node.NodeDate, node.NodeEndDate); err != nil {
		return nil, err
	}

	if err := s.nodes.Update(ctx, node); err != nil {
		return nil, fmt.Errorf("update node: %w", err)
	}

	emit(ctx, s.events, Event{Type: "roadmap.updated", ResearchID: rm.ResearchID, EntityID: rm.ID, Entity: "roadmap"})
	return node, nil
}

// RemoveNodes deletes nodes by IDs (edges cascade).
func (s *RoadmapService) RemoveNodes(ctx context.Context, roadmapID string, nodeIDs []string) error {
	rm, err := s.roadmaps.FindByID(ctx, roadmapID)
	if err != nil {
		return fmt.Errorf("find roadmap: %w", err)
	}
	if rm == nil {
		return ErrNotFound
	}
	if err := s.access.Write(ctx, rm.ResearchID); err != nil {
		return err
	}

	// Every id is checked against this roadmap before anything is deleted, so a
	// list that names a node elsewhere removes nothing rather than half of it.
	// A node in another roadmap is reported exactly like a node that does not
	// exist: the caller has no right to learn which of the two it was.
	for _, nodeID := range nodeIDs {
		node, err := s.nodes.FindByID(ctx, nodeID)
		if err != nil {
			return fmt.Errorf("find node %s: %w", nodeID, err)
		}
		if node == nil || node.RoadmapID != rm.ID {
			return fmt.Errorf("node %s: %w", nodeID, ErrNotFound)
		}
	}
	for _, nodeID := range nodeIDs {
		if _, err := s.nodes.DeleteFromRoadmap(ctx, rm.ID, nodeID); err != nil {
			return fmt.Errorf("delete node %s: %w", nodeID, err)
		}
	}

	emit(ctx, s.events, Event{Type: "roadmap.updated", ResearchID: rm.ResearchID, EntityID: rm.ID, Entity: "roadmap"})
	return nil
}

// --- View / date validation ---

var (
	// ErrInvalidRoadmapView is returned for a view outside graph/stages/timeline.
	ErrInvalidRoadmapView = errors.New("roadmap view must be one of: graph, stages, timeline")
	// ErrInvalidNodeDate is returned for a node_date that is not YYYY-MM-DD.
	ErrInvalidNodeDate = errors.New("node_date must be an ISO date YYYY-MM-DD, or empty")
	// ErrInvalidNodeEndDate is returned for a node_end_date that is not YYYY-MM-DD.
	ErrInvalidNodeEndDate = errors.New("node_end_date must be an ISO date YYYY-MM-DD, or empty")
	// ErrNodeEndBeforeStart is returned when a range ends before it starts.
	ErrNodeEndBeforeStart = errors.New("node_end_date must not be before node_date")
)

// normalizeRoadmapView defaults an empty view to graph and rejects anything not
// in the enum. Empty is the common case — a roadmap created without a view is a
// graph, exactly as it was before this feature existed.
func normalizeRoadmapView(v string) (domain.RoadmapView, error) {
	switch domain.RoadmapView(v) {
	case "":
		return domain.RoadmapViewGraph, nil
	case domain.RoadmapViewGraph, domain.RoadmapViewStages, domain.RoadmapViewTimeline:
		return domain.RoadmapView(v), nil
	default:
		return "", ErrInvalidRoadmapView
	}
}

// normalizeNodeDate accepts an empty string (undated) or a strict ISO calendar
// date. It rejects datetimes and loose forms so the timeline never has to guess
// how to parse what it stored.
func normalizeNodeDate(d string) (string, error) {
	if d == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", d); err != nil {
		return "", ErrInvalidNodeDate
	}
	return d, nil
}

// normalizeNodeRange validates a start/end pair for a create. Each must be an
// ISO date or empty, and a present end must not precede a present start. An end
// with no start is allowed through and simply never renders as a bar — the
// timeline needs a start to place anything.
func normalizeNodeRange(start, end string) (string, string, error) {
	s, err := normalizeNodeDate(start)
	if err != nil {
		return "", "", err
	}
	e, err := normalizeNodeDate(end)
	if err != nil {
		return "", "", ErrInvalidNodeEndDate
	}
	if err := validateNodeRange(s, e); err != nil {
		return "", "", err
	}
	return s, e, nil
}

// validateNodeRange rejects an end before a start when both are present. Both
// are already-normalized ISO dates, so a lexical compare is a date compare.
func validateNodeRange(start, end string) error {
	if start != "" && end != "" && end < start {
		return ErrNodeEndBeforeStart
	}
	return nil
}

// resolveDangling repairs references that named this roadmap, or one of these
// nodes, before it existed. `includeRoadmap` is false when the roadmap itself
// is not new — adding nodes to one that has been cited for weeks must not
// re-announce the roadmap.
func (s *RoadmapService) resolveDangling(ctx context.Context, rm *domain.Roadmap, nodes []*domain.RoadmapNode, includeRoadmap bool) {
	if s.dangling == nil || rm == nil {
		return
	}
	if includeRoadmap {
		s.dangling.ResolveDanglingRoadmap(ctx, rm.ResearchID, rm.Code, rm.ID)
	}
	for _, n := range nodes {
		if n == nil {
			continue
		}
		s.dangling.ResolveDanglingNode(ctx, rm.ResearchID, rm.Code, n.Code, rm.ID, n.ID)
	}
}

// SetDanglingResolver supplies the reference table so that creating a roadmap
// or node repairs the references that already named its code.
//
// Set after construction rather than taken as a parameter because EntryService
// owns the reference table and is built after this one — the same reason
// EntryService.SetRoadmapRepos exists.
func (s *RoadmapService) SetDanglingResolver(r DanglingResolver) { s.dangling = r }
