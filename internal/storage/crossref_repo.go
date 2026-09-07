package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/dovod-app/app/internal/domain"
	"github.com/uptrace/bun"
)

type CrossRefRepository struct {
	db *bun.DB
}

func NewCrossRefRepository(db *bun.DB) *CrossRefRepository {
	return &CrossRefRepository{db: db}
}

// ReplaceForSource deletes all existing refs from this source and inserts new ones.
func (r *CrossRefRepository) ReplaceForSource(ctx context.Context, sourceType, sourceID string, refs []domain.CrossRef) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.NewDelete().
		Table("crossrefs").
		Where("source_type=? AND source_id=?", sourceType, sourceID).
		Exec(ctx); err != nil {
		return fmt.Errorf("delete old crossrefs: %w", err)
	}

	// Inserts below are built by Bun on the same transaction.

	for _, ref := range refs {
		resolved := 0
		if ref.Resolved {
			resolved = 1
		}
		var targetEntryID, targetResearchID, targetRoadmapID, targetNodeID *string
		if ref.TargetEntryID != "" {
			targetEntryID = &ref.TargetEntryID
		}
		if ref.TargetResearchID != "" {
			targetResearchID = &ref.TargetResearchID
		}
		if ref.TargetRoadmapID != "" {
			targetRoadmapID = &ref.TargetRoadmapID
		}
		if ref.TargetNodeID != "" {
			targetNodeID = &ref.TargetNodeID
		}
		// source_entry_id kept for backward compat (NULL for non-entry sources)
		var sourceEntryID *string
		if ref.SourceType == "entry" {
			sourceEntryID = &ref.SourceID
		}
		if _, err := tx.NewInsert().Table("crossrefs").Model(&map[string]any{
			"source_type":        ref.SourceType,
			"source_id":          ref.SourceID,
			"source_entry_id":    sourceEntryID,
			"source_research_id": ref.SourceResearchID,
			"target_entry_id":    targetEntryID,
			"target_research_id": targetResearchID,
			"target_roadmap_id":  targetRoadmapID,
			"target_node_id":     targetNodeID,
			"target_ref":         ref.TargetRef,
			"resolved":           resolved,
		}).Exec(ctx); err != nil {
			return fmt.Errorf("insert crossref: %w", err)
		}
	}

	return tx.Commit()
}

// DanglingMatch names one shape of reference that an entity just created would
// satisfy.
//
// Two shapes exist because two code scopes do, and the line between them is
// narrower than it looks. Entry, task, roadmap and node codes are all allocated
// **per research** — every research has an E1, and every research's first
// roadmap is RM1 — so `[[E20]]`, `[[T4]]`, `[[RM1]]` and `[[RM1:N3]]` may be
// resolved only against sources in the same research, which is what
// SourceResearchID carries. Only a research code is global: `[[R2]]`, and
// `[[R3:E20]]`, which names its research on its face. Those two set Global.
//
// Getting it backwards is how one team's `[[RM1]]` starts pointing at another
// team's roadmap, and it is not recoverable: the `resolved=0` guard below then
// refuses to repair the row when the research's own RM1 finally appears.
type DanglingMatch struct {
	Ref string
	// SourceResearchID restricts the match to references written inside one
	// research. Required unless Global is set.
	SourceResearchID string
	// Global says this code carries its own scope — a research code, or a
	// reference qualified by one — so any source anywhere may name it.
	//
	// A separate flag rather than "empty means global", which is how the first
	// version of this was written and was wrong in the dangerous direction: the
	// zero value of the field that decides tenancy meant *all* tenants, so a
	// caller that simply forgot to set it rewrote every research's rows.
	Global bool
}

// ResolveDangling points references that were written before their target
// existed at the target that now does, and returns the researches the repaired
// rows were written in — deduplicated, and never including a research none of
// whose rows moved.
//
// It returns those rather than a count because the caller announces the repair,
// and the announcement belongs to the research that holds the *source*. A
// `[[R3:E20]]` written in R1 is repaired when E20 appears in R3; telling R3
// tells the wrong tenant, who learns that somebody out of sight cites them, and
// leaves R1 — the only page that changed — unrepainted.
//
// This is the targeted half of RebuildCrossRefs. The rebuild re-reads every
// document in the research and rewrites the whole table; this touches only rows
// that are still unresolved and name exactly this code, which is what makes it
// affordable on the hot path of every create.
//
// `resolved=0` in the predicate is not an optimisation. A resolved row already
// points somewhere, and re-pointing it at a newly created entity with the same
// code would silently move a link a reader had already followed.
func (r *CrossRefRepository) ResolveDangling(ctx context.Context, matches []DanglingMatch, target domain.CrossRef) ([]string, error) {
	if len(matches) == 0 {
		return nil, nil
	}

	// The predicate is built as text because it has to be asked twice: once to
	// learn which researches wrote the rows about to move, and once to move
	// them. After the UPDATE those rows no longer match, so the order is not a
	// preference.
	//
	// A match with neither Global nor a research is dropped here rather than
	// widened. An empty condition list would leave `WHERE resolved=0` standing
	// alone over every tenant's rows — a stronger version of the cross-tenant
	// bug this file exists to close — so the early return below is load-bearing.
	var conds []string
	var args []any
	for _, m := range matches {
		if m.Global {
			conds = append(conds, "target_ref = ?")
			args = append(args, m.Ref)
			continue
		}
		if m.SourceResearchID == "" {
			continue
		}
		conds = append(conds, "(target_ref = ? AND source_research_id = ?)")
		args = append(args, m.Ref, m.SourceResearchID)
	}
	if len(conds) == 0 {
		return nil, nil
	}
	pred := "(" + strings.Join(conds, " OR ") + ")"

	// Rows whose source research is unknown are excluded from this read only,
	// never from the UPDATE: they are repaired like any other, there is simply
	// nobody to tell.
	var sources []string
	if err := r.db.NewSelect().
		Table("crossrefs").
		ColumnExpr("DISTINCT source_research_id").
		Where("resolved = ?", 0).
		Where("source_research_id IS NOT NULL AND source_research_id != ?", "").
		Where(pred, args...).
		Scan(ctx, &sources); err != nil {
		return nil, fmt.Errorf("find dangling crossref sources: %w", err)
	}

	q := r.db.NewUpdate().
		Table("crossrefs").
		Set("resolved=?", 1).
		Where("resolved=?", 0).
		Where(pred, args...)

	// Only the ids the caller actually knows. A roadmap has no entry id and an
	// entry has no node id; writing NULL over a column this target says nothing
	// about would erase what the resolver had already worked out.
	if target.TargetEntryID != "" {
		q = q.Set("target_entry_id=?", target.TargetEntryID)
	}
	if target.TargetResearchID != "" {
		q = q.Set("target_research_id=?", target.TargetResearchID)
	}
	if target.TargetRoadmapID != "" {
		q = q.Set("target_roadmap_id=?", target.TargetRoadmapID)
	}
	if target.TargetNodeID != "" {
		q = q.Set("target_node_id=?", target.TargetNodeID)
	}

	if _, err := q.Exec(ctx); err != nil {
		return nil, fmt.Errorf("resolve dangling crossrefs: %w", err)
	}
	// No rows-affected count is consulted, and that is a gain rather than an
	// omission: the sources were read from the rows this very predicate matched
	// a moment ago. MySQL reports rows *changed* rather than matched and some
	// drivers decline the count outright, which used to mean a repair that
	// really happened announced nothing.
	return sources, nil
}

// FindByResearch returns all cross-references where the source belongs to the given research.
func (r *CrossRefRepository) FindByResearch(ctx context.Context, researchID string) ([]domain.CrossRef, error) {
	rows, err := r.db.NewSelect().
		ColumnExpr("source_type, source_id, source_research_id, COALESCE(target_entry_id, ''), COALESCE(target_research_id, ''), COALESCE(target_roadmap_id, ''), COALESCE(target_node_id, ''), target_ref, resolved").
		TableExpr("crossrefs").
		Where("source_research_id=?", researchID).
		OrderExpr("created_at").
		Rows(ctx)
	if err != nil {
		return nil, fmt.Errorf("query crossrefs: %w", err)
	}
	defer rows.Close()

	var result []domain.CrossRef
	for rows.Next() {
		var cr domain.CrossRef
		var resolved int
		if err := rows.Scan(
			&cr.SourceType, &cr.SourceID, &cr.SourceResearchID,
			&cr.TargetEntryID, &cr.TargetResearchID,
			&cr.TargetRoadmapID, &cr.TargetNodeID,
			&cr.TargetRef, &resolved,
		); err != nil {
			return nil, fmt.Errorf("scan crossref: %w", err)
		}
		cr.Resolved = resolved == 1
		result = append(result, cr)
	}
	return result, rows.Err()
}

// FindBySourceEntry returns all cross-references where the given entry is the source (outgoing links).
func (r *CrossRefRepository) FindBySourceEntry(ctx context.Context, entryID string) ([]domain.CrossRef, error) {
	rows, err := r.db.NewSelect().
		ColumnExpr("source_type, source_id, source_research_id, COALESCE(target_entry_id, ''), COALESCE(target_research_id, ''), COALESCE(target_roadmap_id, ''), COALESCE(target_node_id, ''), target_ref, resolved").
		TableExpr("crossrefs").
		Where("source_type='entry' AND source_id=?", entryID).
		OrderExpr("created_at").
		Rows(ctx)
	if err != nil {
		return nil, fmt.Errorf("query outgoing crossrefs: %w", err)
	}
	defer rows.Close()

	var result []domain.CrossRef
	for rows.Next() {
		var cr domain.CrossRef
		var resolved int
		if err := rows.Scan(
			&cr.SourceType, &cr.SourceID, &cr.SourceResearchID,
			&cr.TargetEntryID, &cr.TargetResearchID,
			&cr.TargetRoadmapID, &cr.TargetNodeID,
			&cr.TargetRef, &resolved,
		); err != nil {
			return nil, fmt.Errorf("scan crossref: %w", err)
		}
		cr.Resolved = resolved == 1
		result = append(result, cr)
	}
	return result, rows.Err()
}

// FindByTargetEntry returns all sources that reference the given entry (incoming links).
func (r *CrossRefRepository) FindByTargetEntry(ctx context.Context, entryID string) ([]domain.CrossRef, error) {
	rows, err := r.db.NewSelect().
		ColumnExpr("source_type, source_id, source_research_id, COALESCE(target_entry_id, ''), COALESCE(target_research_id, ''), COALESCE(target_roadmap_id, ''), COALESCE(target_node_id, ''), target_ref, resolved").
		TableExpr("crossrefs").
		Where("target_entry_id=?", entryID).
		OrderExpr("created_at").
		Rows(ctx)
	if err != nil {
		return nil, fmt.Errorf("query incoming crossrefs: %w", err)
	}
	defer rows.Close()

	var result []domain.CrossRef
	for rows.Next() {
		var cr domain.CrossRef
		var resolved int
		if err := rows.Scan(
			&cr.SourceType, &cr.SourceID, &cr.SourceResearchID,
			&cr.TargetEntryID, &cr.TargetResearchID,
			&cr.TargetRoadmapID, &cr.TargetNodeID,
			&cr.TargetRef, &resolved,
		); err != nil {
			return nil, fmt.Errorf("scan crossref: %w", err)
		}
		cr.Resolved = resolved == 1
		result = append(result, cr)
	}
	return result, rows.Err()
}
