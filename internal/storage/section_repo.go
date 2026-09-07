package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/dovod-app/app/internal/domain"
	"github.com/uptrace/bun"
)

type SectionRepository struct {
	db *bun.DB
}

func NewSectionRepository(db *bun.DB) *SectionRepository {
	return &SectionRepository{db: db}
}

func (r *SectionRepository) Create(ctx context.Context, section *domain.Section) error {
	now := time.Now().UTC().Format(time.DateTime)

	if section.Code == "" {
		code, err := NextCode(ctx, r.db, "sections", "S", "research_id", section.ResearchID)
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		section.Code = code
	}

	_, err := r.db.NewInsert().Table("sections").Model(&map[string]any{
		"id":           section.ID,
		"code":         section.Code,
		"research_id":  section.ResearchID,
		"name":         section.Name,
		"display_name": section.DisplayName,
		"description":  section.Description,
		"status":       section.Status,
		"position":     section.Position,
		"instruction":  section.Instruction,
		"field_spec":   marshalJSON(section.FieldSpec),
		"spec_version": section.SpecVersion,
		"created_at":   now,
		"updated_at":   now,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("insert section: %w", err)
	}
	section.CreatedAt, _ = time.Parse(time.DateTime, now)
	section.UpdatedAt = section.CreatedAt
	return nil
}

func (r *SectionRepository) CreateTx(ctx context.Context, tx bun.Tx, section *domain.Section) error {
	now := time.Now().UTC().Format(time.DateTime)

	if section.Code == "" {
		code, err := NextCode(ctx, tx, "sections", "S", "research_id", section.ResearchID)
		if err != nil {
			return fmt.Errorf("generate code: %w", err)
		}
		section.Code = code
	}

	_, err := tx.NewInsert().Table("sections").Model(&map[string]any{
		"id":           section.ID,
		"code":         section.Code,
		"research_id":  section.ResearchID,
		"name":         section.Name,
		"display_name": section.DisplayName,
		"description":  section.Description,
		"status":       section.Status,
		"position":     section.Position,
		"instruction":  section.Instruction,
		"field_spec":   marshalJSON(section.FieldSpec),
		"spec_version": section.SpecVersion,
		"created_at":   now,
		"updated_at":   now,
	}).Exec(ctx)
	if err != nil {
		return fmt.Errorf("insert section: %w", err)
	}
	section.CreatedAt, _ = time.Parse(time.DateTime, now)
	section.UpdatedAt = section.CreatedAt
	return nil
}

func (r *SectionRepository) Update(ctx context.Context, section *domain.Section) error {
	now := time.Now().UTC().Format(time.DateTime)
	_, err := r.db.NewUpdate().
		Table("sections").
		Set("name=?", section.Name).
		Set("display_name=?", section.DisplayName).
		Set("description=?", section.Description).
		Set("status=?", section.Status).
		Set("position=?", section.Position).
		Set("instruction=?", section.Instruction).
		Set("code=?", section.Code).
		Set("field_spec=?", marshalJSON(section.FieldSpec)).
		Set("spec_version=?", section.SpecVersion).
		Set("updated_at=?", now).
		Where("id=?", section.ID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update section: %w", err)
	}
	section.UpdatedAt, _ = time.Parse(time.DateTime, now)
	return nil
}

func (r *SectionRepository) FindByID(ctx context.Context, id string) (*domain.Section, error) {
	row := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("id, code, research_id, name, display_name, description, status, position, instruction, field_spec, spec_version, created_at, updated_at").
		TableExpr("sections").
		Where("id=?", id))
	return r.scanSection(row)
}

func (r *SectionRepository) FindByResearch(ctx context.Context, researchID string) ([]*domain.Section, error) {
	rows, err := r.db.NewSelect().
		ColumnExpr("id, code, research_id, name, display_name, description, status, position, instruction, field_spec, spec_version, created_at, updated_at").
		TableExpr("sections").
		Where("research_id=?", researchID).
		OrderExpr("position ASC").
		Rows(ctx)
	if err != nil {
		return nil, fmt.Errorf("query sections: %w", err)
	}
	defer rows.Close()

	var result []*domain.Section
	for rows.Next() {
		s, err := r.scanSectionRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (r *SectionRepository) CountEntriesBySection(ctx context.Context, sectionID string) (int, error) {
	var count int
	err := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("COUNT(*)").
		TableExpr("entries").
		Where("section_id=?", sectionID)).
		Scan(&count)
	return count, err
}

func (r *SectionRepository) FindByResearchAndName(ctx context.Context, researchID, name string) (*domain.Section, error) {
	row := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("id, code, research_id, name, display_name, description, status, position, instruction, field_spec, spec_version, created_at, updated_at").
		TableExpr("sections").
		Where("research_id=? AND name=?", researchID, name))
	return r.scanSection(row)
}

func (r *SectionRepository) scanSection(row scanner) (*domain.Section, error) {
	var s domain.Section
	var createdAt, updatedAt string
	var fieldSpec sql.NullString
	err := row.Scan(
		&s.ID, &s.Code, &s.ResearchID, &s.Name, &s.DisplayName,
		&s.Description, &s.Status, &s.Position,
		&s.Instruction, &fieldSpec, &s.SpecVersion,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan section: %w", err)
	}
	s.FieldSpec = unmarshalFieldSpec(fieldSpec)
	s.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	s.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
	return &s, nil
}

func (r *SectionRepository) scanSectionRow(rows *sql.Rows) (*domain.Section, error) {
	var s domain.Section
	var createdAt, updatedAt string
	var fieldSpec sql.NullString
	err := rows.Scan(
		&s.ID, &s.Code, &s.ResearchID, &s.Name, &s.DisplayName,
		&s.Description, &s.Status, &s.Position,
		&s.Instruction, &fieldSpec, &s.SpecVersion,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan section row: %w", err)
	}
	s.FieldSpec = unmarshalFieldSpec(fieldSpec)
	s.CreatedAt, _ = time.Parse(time.DateTime, createdAt)
	s.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt)
	return &s, nil
}
