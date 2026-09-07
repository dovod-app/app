package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/dovod-app/app/internal/domain"
	"github.com/uptrace/bun"
)

type RoadmapRepository struct {
	db *bun.DB
}

func NewRoadmapRepository(db *bun.DB) *RoadmapRepository {
	return &RoadmapRepository{db: db}
}

// Create inserts a roadmap record.
func (r *RoadmapRepository) Create(ctx context.Context, rm *domain.Roadmap) error {
	now := time.Now().UTC().Format(time.DateTime)

	if rm.Code == "" {
		code, err := NextCode(ctx, r.db, "roadmaps", "RM", "research_id", rm.ResearchID)
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		rm.Code = code
	}

	_, err := r.db.NewInsert().Table("roadmaps").Model(&map[string]any{
		"id":          rm.ID,
		"code":        rm.Code,
		"research_id": rm.ResearchID,
		"title":       rm.Title,
		"description": rm.Description,
		"statuses":    marshalJSON(rm.Statuses),
		"status":      rm.Status,
		"stages":      marshalJSON(rm.Stages),
		"view":        rm.View,
		"created_at":  now,
		"updated_at":  now,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("insert roadmap: %w", err)
	}
	rm.CreatedAt, _ = time.Parse(time.DateTime, now)
	rm.UpdatedAt = rm.CreatedAt
	return nil
}

// Update updates roadmap metadata.
func (r *RoadmapRepository) Update(ctx context.Context, rm *domain.Roadmap) error {
	now := time.Now().UTC().Format(time.DateTime)
	_, err := r.db.NewUpdate().
		Table("roadmaps").
		Set("title=?", rm.Title).
		Set("description=?", rm.Description).
		Set("statuses=?", marshalJSON(rm.Statuses)).
		Set("status=?", rm.Status).
		Set("stages=?", marshalJSON(rm.Stages)).
		Set("view=?", rm.View).
		Set("updated_at=?", now).
		Where("id=?", rm.ID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update roadmap: %w", err)
	}
	rm.UpdatedAt, _ = time.Parse(time.DateTime, now)
	return nil
}

// FindByID returns a roadmap without nodes/edges.
func (r *RoadmapRepository) FindByID(ctx context.Context, id string) (*domain.Roadmap, error) {
	row := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("id, code, research_id, title, description, statuses, status, stages, view, created_at, updated_at").
		TableExpr("roadmaps").
		Where("id=?", id))
	return r.scanRoadmap(row)
}

// FindByCode returns a roadmap by its short code (e.g. RM1).
func (r *RoadmapRepository) FindByCode(ctx context.Context, code string) (*domain.Roadmap, error) {
	row := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("id, code, research_id, title, description, statuses, status, stages, view, created_at, updated_at").
		TableExpr("roadmaps").
		Where("code=?", code))
	return r.scanRoadmap(row)
}

// FindByCodeAndResearch returns a roadmap by its short code scoped to a research.
func (r *RoadmapRepository) FindByCodeAndResearch(ctx context.Context, code, researchID string) (*domain.Roadmap, error) {
	row := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("id, code, research_id, title, description, statuses, status, stages, view, created_at, updated_at").
		TableExpr("roadmaps").
		Where("code=? AND research_id=?", code, researchID))
	return r.scanRoadmap(row)
}

// FindByResearch returns all roadmaps for a research.
func (r *RoadmapRepository) FindByResearch(ctx context.Context, researchID string) ([]*domain.Roadmap, error) {
	rows, err := r.db.NewSelect().
		ColumnExpr("id, code, research_id, title, description, statuses, status, stages, view, created_at, updated_at").
		TableExpr("roadmaps").
		Where("research_id=?", researchID).
		OrderExpr("created_at ASC, LENGTH(code), code, id").
		Rows(ctx)
	if err != nil {
		return nil, fmt.Errorf("query roadmaps: %w", err)
	}
	defer rows.Close()

	var result []*domain.Roadmap
	for rows.Next() {
		rm, err := r.scanRoadmapRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, rm)
	}
	return result, rows.Err()
}

// Delete removes a roadmap (cascade deletes nodes and edges).
func (r *RoadmapRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Table("roadmaps").Where("id=?", id).Exec(ctx)
	return err
}

func (r *RoadmapRepository) scanRoadmap(row scanner) (*domain.Roadmap, error) {
	var rm domain.Roadmap
	var createdAt, updatedAt string
	var statuses, stages sql.NullString

	err := row.Scan(
		&rm.ID, &rm.Code, &rm.ResearchID, &rm.Title, &rm.Description,
		&statuses, &rm.Status, &stages, &rm.View,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan roadmap: %w", err)
	}
	rm.Statuses = unmarshalStringSlice(statuses)
	rm.Stages = unmarshalStringSlice(stages)
	rm.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	rm.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
	return &rm, nil
}

func (r *RoadmapRepository) scanRoadmapRow(rows *sql.Rows) (*domain.Roadmap, error) {
	var rm domain.Roadmap
	var createdAt, updatedAt string
	var statuses, stages sql.NullString

	err := rows.Scan(
		&rm.ID, &rm.Code, &rm.ResearchID, &rm.Title, &rm.Description,
		&statuses, &rm.Status, &stages, &rm.View,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan roadmap row: %w", err)
	}
	rm.Statuses = unmarshalStringSlice(statuses)
	rm.Stages = unmarshalStringSlice(stages)
	rm.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	rm.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
	return &rm, nil
}

// --- Roadmap Nodes ---

type RoadmapNodeRepository struct {
	db *bun.DB
}

func NewRoadmapNodeRepository(db *bun.DB) *RoadmapNodeRepository {
	return &RoadmapNodeRepository{db: db}
}

// Create inserts a node.
func (r *RoadmapNodeRepository) Create(ctx context.Context, node *domain.RoadmapNode) error {
	now := time.Now().UTC().Format(time.DateTime)

	if node.Code == "" {
		code, err := NextCode(ctx, r.db, "roadmap_nodes", "N", "roadmap_id", node.RoadmapID)
		if err != nil {
			return fmt.Errorf("generate node code: %w", err)
		}
		node.Code = code
	}

	var parentID *string
	if node.ParentID != "" {
		parentID = &node.ParentID
	}
	var refType, refID, metadata *string
	if node.RefType != "" {
		refType = &node.RefType
	}
	if node.RefID != "" {
		refID = &node.RefID
	}
	if node.Metadata != "" {
		metadata = &node.Metadata
	}

	_, err := r.db.NewInsert().Table("roadmap_nodes").Model(&map[string]any{
		"id":            node.ID,
		"code":          node.Code,
		"roadmap_id":    node.RoadmapID,
		"title":         node.Title,
		"description":   node.Description,
		"node_type":     node.NodeType,
		"status":        node.Status,
		"position_x":    node.PositionX,
		"position_y":    node.PositionY,
		"parent_id":     parentID,
		"ref_type":      refType,
		"ref_id":        refID,
		"metadata":      metadata,
		"stage":         node.Stage,
		"node_date":     node.NodeDate,
		"node_end_date": node.NodeEndDate,
		"created_at":    now,
		"updated_at":    now,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("insert roadmap node: %w", err)
	}
	node.CreatedAt, _ = time.Parse(time.DateTime, now)
	node.UpdatedAt = node.CreatedAt
	return nil
}

// Update updates a node.
func (r *RoadmapNodeRepository) Update(ctx context.Context, node *domain.RoadmapNode) error {
	now := time.Now().UTC().Format(time.DateTime)

	var parentID *string
	if node.ParentID != "" {
		parentID = &node.ParentID
	}
	var refType, refID, metadata *string
	if node.RefType != "" {
		refType = &node.RefType
	}
	if node.RefID != "" {
		refID = &node.RefID
	}
	if node.Metadata != "" {
		metadata = &node.Metadata
	}

	_, err := r.db.NewUpdate().
		Table("roadmap_nodes").
		Set("title=?", node.Title).
		Set("description=?", node.Description).
		Set("node_type=?", node.NodeType).
		Set("status=?", node.Status).
		Set("position_x=?", node.PositionX).
		Set("position_y=?", node.PositionY).
		Set("parent_id=?", parentID).
		Set("ref_type=?", refType).
		Set("ref_id=?", refID).
		Set("metadata=?", metadata).
		Set("stage=?", node.Stage).
		Set("node_date=?", node.NodeDate).
		Set("node_end_date=?", node.NodeEndDate).
		Set("updated_at=?", now).
		Where("id=?", node.ID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update roadmap node: %w", err)
	}
	node.UpdatedAt, _ = time.Parse(time.DateTime, now)
	return nil
}

// FindByID returns a single node.
func (r *RoadmapNodeRepository) FindByID(ctx context.Context, id string) (*domain.RoadmapNode, error) {
	row := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("id, code, roadmap_id, title, description, node_type, status, position_x, position_y, parent_id, ref_type, ref_id, metadata, stage, node_date, node_end_date, created_at, updated_at").
		TableExpr("roadmap_nodes").
		Where("id=?", id))
	return r.scanNode(row)
}

// FindByCode returns a node by its short code (e.g. N3) within a roadmap.
func (r *RoadmapNodeRepository) FindByCode(ctx context.Context, roadmapID, code string) (*domain.RoadmapNode, error) {
	row := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("id, code, roadmap_id, title, description, node_type, status, position_x, position_y, parent_id, ref_type, ref_id, metadata, stage, node_date, node_end_date, created_at, updated_at").
		TableExpr("roadmap_nodes").
		Where("roadmap_id=? AND code=?", roadmapID, code))
	return r.scanNode(row)
}

// FindByRoadmap returns all nodes for a roadmap.
// Codes break timestamp ties in creation order, including N10 and beyond.
func (r *RoadmapNodeRepository) FindByRoadmap(ctx context.Context, roadmapID string) ([]*domain.RoadmapNode, error) {
	rows, err := r.db.NewSelect().
		ColumnExpr("id, code, roadmap_id, title, description, node_type, status, position_x, position_y, parent_id, ref_type, ref_id, metadata, stage, node_date, node_end_date, created_at, updated_at").
		TableExpr("roadmap_nodes").
		Where("roadmap_id=?", roadmapID).
		OrderExpr("created_at ASC, LENGTH(code), code, id").
		Rows(ctx)
	if err != nil {
		return nil, fmt.Errorf("query roadmap nodes: %w", err)
	}
	defer rows.Close()

	var result []*domain.RoadmapNode
	for rows.Next() {
		n, err := r.scanNodeRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// DeleteFromRoadmap removes a node that belongs to the given roadmap (cascade
// deletes edges). It reports whether a row went away.
//
// The roadmap is part of the predicate on purpose: the caller has proved write
// access to one roadmap, and a bare node id would let that grant reach into a
// roadmap — and a research — the caller never qualified for.
func (r *RoadmapNodeRepository) DeleteFromRoadmap(ctx context.Context, roadmapID, id string) (bool, error) {
	res, err := r.db.NewDelete().Table("roadmap_nodes").
		Where("id=?", id).Where("roadmap_id=?", roadmapID).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *RoadmapNodeRepository) scanNode(row scanner) (*domain.RoadmapNode, error) {
	var n domain.RoadmapNode
	var createdAt, updatedAt string
	var parentID, refType, refID, metadata sql.NullString

	err := row.Scan(
		&n.ID, &n.Code, &n.RoadmapID, &n.Title, &n.Description,
		&n.NodeType, &n.Status, &n.PositionX, &n.PositionY, &parentID,
		&refType, &refID, &metadata, &n.Stage, &n.NodeDate, &n.NodeEndDate,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan roadmap node: %w", err)
	}
	if parentID.Valid {
		n.ParentID = parentID.String
	}
	if refType.Valid {
		n.RefType = refType.String
	}
	if refID.Valid {
		n.RefID = refID.String
	}
	if metadata.Valid {
		n.Metadata = metadata.String
	}
	n.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	n.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
	return &n, nil
}

func (r *RoadmapNodeRepository) scanNodeRow(rows *sql.Rows) (*domain.RoadmapNode, error) {
	var n domain.RoadmapNode
	var createdAt, updatedAt string
	var parentID, refType, refID, metadata sql.NullString

	err := rows.Scan(
		&n.ID, &n.Code, &n.RoadmapID, &n.Title, &n.Description,
		&n.NodeType, &n.Status, &n.PositionX, &n.PositionY, &parentID,
		&refType, &refID, &metadata, &n.Stage, &n.NodeDate, &n.NodeEndDate,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan roadmap node row: %w", err)
	}
	if parentID.Valid {
		n.ParentID = parentID.String
	}
	if refType.Valid {
		n.RefType = refType.String
	}
	if refID.Valid {
		n.RefID = refID.String
	}
	if metadata.Valid {
		n.Metadata = metadata.String
	}
	n.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	n.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
	return &n, nil
}

// --- Roadmap Edges ---

type RoadmapEdgeRepository struct {
	db *bun.DB
}

func NewRoadmapEdgeRepository(db *bun.DB) *RoadmapEdgeRepository {
	return &RoadmapEdgeRepository{db: db}
}

// Create inserts an edge.
func (r *RoadmapEdgeRepository) Create(ctx context.Context, edge *domain.RoadmapEdge) error {
	now := time.Now().UTC().Format(time.DateTime)
	_, err := r.db.NewInsert().Table("roadmap_edges").Model(&map[string]any{
		"id":             edge.ID,
		"roadmap_id":     edge.RoadmapID,
		"source_node_id": edge.SourceNodeID,
		"target_node_id": edge.TargetNodeID,
		"label":          edge.Label,
		"edge_type":      edge.EdgeType,
		"created_at":     now,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("insert roadmap edge: %w", err)
	}
	edge.CreatedAt, _ = time.Parse(time.DateTime, now)
	return nil
}

// FindByRoadmap returns all edges for a roadmap.
func (r *RoadmapEdgeRepository) FindByRoadmap(ctx context.Context, roadmapID string) ([]*domain.RoadmapEdge, error) {
	rows, err := r.db.NewSelect().
		ColumnExpr("id, roadmap_id, source_node_id, target_node_id, label, edge_type, created_at").
		TableExpr("roadmap_edges").
		Where("roadmap_id=?", roadmapID).
		OrderExpr("created_at ASC").
		Rows(ctx)
	if err != nil {
		return nil, fmt.Errorf("query roadmap edges: %w", err)
	}
	defer rows.Close()

	var result []*domain.RoadmapEdge
	for rows.Next() {
		e, err := r.scanEdgeRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// DeleteByRoadmap removes all edges for a roadmap.
func (r *RoadmapEdgeRepository) DeleteByRoadmap(ctx context.Context, roadmapID string) error {
	_, err := r.db.NewDelete().Table("roadmap_edges").Where("roadmap_id=?", roadmapID).Exec(ctx)
	return err
}

func (r *RoadmapEdgeRepository) scanEdgeRow(rows *sql.Rows) (*domain.RoadmapEdge, error) {
	var e domain.RoadmapEdge
	var createdAt string

	err := rows.Scan(
		&e.ID, &e.RoadmapID, &e.SourceNodeID, &e.TargetNodeID,
		&e.Label, &e.EdgeType, &createdAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan roadmap edge row: %w", err)
	}
	e.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	return &e, nil
}
