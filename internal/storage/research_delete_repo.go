package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/dovod-app/app/internal/domain"
	"github.com/uptrace/bun"
)

// DeleteCascade removes a research and everything that belongs to it.
//
// Most of the work is done by the schema: `sections`, `entries`, `sessions`,
// `questions`, `tasks`, `entry_blocks`, `entry_revisions`, `external_links`,
// `roadmaps` (and their nodes and edges), `shares`, `annotations`,
// `entry_views`, `research_memory`, `research_skills` and research-tier
// `skills` all declare ON DELETE CASCADE in every dialect, and SQLite runs with
// `foreign_keys=ON`.
//
// Two things do not, and they are the reason this is a method rather than one
// DELETE statement:
//
//   - **crossrefs has no foreign keys at all.** Migration 007 recreated the
//     table to make `source_entry_id` nullable and did not carry the REFERENCES
//     clauses over; the Postgres and MySQL baselines were generated from that
//     shape, so all three agree. Nothing about these rows is automatic.
//
//   - **storage_counters is keyed by a string**, deliberately outside the
//     foreign-key graph, so short codes survive deleting a document. Once the
//     whole research is gone its counters can never be reached again — the ids
//     in their keys are uuids — so they are dead rows that would accumulate.
//
// Everything happens in one transaction. A half-deleted research is worse than
// either outcome: the row list would disagree with what is on disk, and there
// is no screen anywhere that would show you which half survived.
func (r *ResearchRepository) DeleteCascade(ctx context.Context, researchID string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// The counter keys of children are scoped by *their* ids, not the
		// research's, so those ids have to be read while the rows still exist.
		scopes := []string{researchID}

		var sessionIDs []string
		if err := tx.NewSelect().Column("id").Table("sessions").
			Where("research_id=?", researchID).Scan(ctx, &sessionIDs); err != nil {
			return fmt.Errorf("collect sessions: %w", err)
		}
		scopes = append(scopes, sessionIDs...)

		var roadmapIDs []string
		if err := tx.NewSelect().Column("id").Table("roadmaps").
			Where("research_id=?", researchID).Scan(ctx, &roadmapIDs); err != nil {
			return fmt.Errorf("collect roadmaps: %w", err)
		}
		scopes = append(scopes, roadmapIDs...)

		// Outgoing references die with the documents that wrote them.
		if _, err := tx.NewDelete().Table("crossrefs").
			Where("source_research_id=?", researchID).Exec(ctx); err != nil {
			return fmt.Errorf("delete outgoing crossrefs: %w", err)
		}

		// Incoming references do not. If R2 says "this rests on [[R1:E5]]" and
		// R1 is deleted, deleting that row would edit R2's documents — it would
		// make R2's own history a lie about what it once cited. The row stays,
		// keeping `target_ref` verbatim, and becomes unresolved, which is the
		// state the UI already renders as inert text rather than a link into
		// nothing.
		//
		// One predicate covers every kind of target: entry, research, roadmap
		// and node references all carry target_research_id when they resolve.
		if _, err := tx.NewUpdate().Table("crossrefs").
			Set("target_research_id=NULL").
			Set("target_entry_id=NULL").
			Set("target_roadmap_id=NULL").
			Set("target_node_id=NULL").
			Set("resolved=0").
			Where("target_research_id=?", researchID).Exec(ctx); err != nil {
			return fmt.Errorf("unresolve incoming crossrefs: %w", err)
		}

		// Keys look like `entries:E:<researchID>` or `questions:Q:<sessionID>`,
		// so the id is always the last segment. A uuid contains no LIKE
		// metacharacter, and LIKE is the one pattern operator all three
		// dialects spell the same way.
		for _, scope := range scopes {
			if _, err := tx.NewDelete().Table("storage_counters").
				Where("scope_key LIKE ?", "%:"+scope).Exec(ctx); err != nil {
				return fmt.Errorf("delete counters for %s: %w", scope, err)
			}
		}

		res, err := tx.NewDelete().Table("researches").Where("id=?", researchID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete research: %w", err)
		}
		// Rows-affected rather than a prior SELECT: two callers deleting the
		// same research concurrently must not both report success, and the
		// service turns this into ErrNotFound.
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("delete research: %w", err)
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

// DeletionSummary counts what deleting this research would destroy.
//
// It is read before the confirmation is shown, because "delete R7" and "delete
// 4 sections, 12 documents, 3 sessions, 21 questions and 6 tasks" are different
// decisions, and the second one is the true one.
//
// Counted per table rather than derived: the point of the number is that
// somebody can check it against what they see, and a figure inferred from a
// parent count would be the same guess the reader is trying to avoid making.
func (r *ResearchRepository) DeletionSummary(ctx context.Context, researchID string) (domain.DeletionSummary, error) {
	var s domain.DeletionSummary

	direct := map[string]*int{
		"sections":    &s.Sections,
		"entries":     &s.Entries,
		"sessions":    &s.Sessions,
		"tasks":       &s.Tasks,
		"roadmaps":    &s.Roadmaps,
		"annotations": &s.Annotations,
		"shares":      &s.Shares,
	}
	for table, into := range direct {
		if err := selectRow(ctx, r.db.NewSelect().
			ColumnExpr("COUNT(*)").TableExpr(table).
			Where("research_id=?", researchID)).Scan(into); err != nil {
			return s, fmt.Errorf("count %s: %w", table, err)
		}
	}

	// Questions hang off sessions, so they are counted through the join rather
	// than by a research_id column they do not have.
	if err := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("COUNT(*)").TableExpr("questions AS q").
		Join("JOIN sessions AS s ON s.id = q.session_id").
		Where("s.research_id=?", researchID)).Scan(&s.Questions); err != nil {
		return s, fmt.Errorf("count questions: %w", err)
	}

	// References from other researches into this one. Not destroyed — they
	// survive as unresolved text — but the reader is about to break them, and
	// that is worth saying before rather than after.
	if err := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("COUNT(*)").TableExpr("crossrefs").
		Where("target_research_id=?", researchID).
		Where("source_research_id<>?", researchID)).Scan(&s.IncomingRefs); err != nil {
		return s, fmt.Errorf("count incoming refs: %w", err)
	}

	if s.IncomingRefs > 0 {
		// Whose work is about to be left with dead references. DISTINCT because
		// one research citing this one eight times is one research to name.
		rows, err := r.db.NewSelect().
			ColumnExpr("DISTINCT r.code, r.name").
			TableExpr("crossrefs AS c").
			Join("JOIN researches AS r ON r.id = c.source_research_id").
			Where("c.target_research_id=?", researchID).
			Where("c.source_research_id<>?", researchID).
			OrderExpr("r.code").
			Limit(citingResearchLimit).
			Rows(ctx)
		if err != nil {
			return s, fmt.Errorf("list citing researches: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var c domain.CitingResearch
			if err := rows.Scan(&c.Code, &c.Name); err != nil {
				return s, fmt.Errorf("scan citing research: %w", err)
			}
			s.IncomingFrom = append(s.IncomingFrom, c)
		}
		if err := rows.Err(); err != nil {
			return s, fmt.Errorf("list citing researches: %w", err)
		}
	}

	return s, nil
}

// citingResearchLimit caps the names, not the count. The count is what the
// decision turns on; the names are there so it is not an abstraction, and a
// dialog listing eighty of them is neither readable nor a better warning.
const citingResearchLimit = 10

// DeleteCascade removes a section and the documents filed in it.
//
// The documents go by foreign key. What does not is everything keyed by
// `source_type`/`source_id` — `crossrefs` has no foreign keys at all, and
// `external_links` has one on `research_id`, which does not fire here because
// the research is staying. Both would be left pointing at rows that are gone.
//
// The cleanup lives here rather than in the service so it shares the delete's
// transaction: a section whose documents are gone but whose references still
// resolve is a worse state than either end of the operation.
func (r *SectionRepository) DeleteCascade(ctx context.Context, sectionID string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var entryIDs []string
		if err := tx.NewSelect().Column("id").Table("entries").
			Where("section_id=?", sectionID).Scan(ctx, &entryIDs); err != nil {
			return fmt.Errorf("collect entries: %w", err)
		}

		if len(entryIDs) > 0 {
			if err := deleteRefsBySource(ctx, tx, "entry", entryIDs); err != nil {
				return err
			}
			// References from elsewhere into these documents survive as
			// unresolved text, for the same reason they do when a whole
			// research goes: deleting them would edit somebody else's document.
			if _, err := tx.NewUpdate().Table("crossrefs").
				Set("target_entry_id=NULL").
				Set("target_research_id=NULL").
				Set("resolved=0").
				Where("target_entry_id IN (?)", bun.In(entryIDs)).Exec(ctx); err != nil {
				return fmt.Errorf("unresolve references to entries: %w", err)
			}
		}

		if _, err := tx.NewDelete().Table("sections").Where("id=?", sectionID).Exec(ctx); err != nil {
			return fmt.Errorf("delete section: %w", err)
		}
		return nil
	})
}

// DeleteCascade removes a session and the questions asked in it.
//
// Questions cascade; their cross-references do not, and are cleared here.
// Documents written during the session survive with `session_id` set to NULL,
// which every dialect declares — a finding is not an artefact of the
// conversation that produced it, and deleting a transcript should not delete
// the conclusions drawn in it.
func (r *SessionRepository) DeleteCascade(ctx context.Context, sessionID string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var questionIDs []string
		if err := tx.NewSelect().Column("id").Table("questions").
			Where("session_id=?", sessionID).Scan(ctx, &questionIDs); err != nil {
			return fmt.Errorf("collect questions: %w", err)
		}
		if err := deleteRefsBySource(ctx, tx, "question", questionIDs); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Table("sessions").Where("id=?", sessionID).Exec(ctx); err != nil {
			return fmt.Errorf("delete session: %w", err)
		}
		return nil
	})
}

// DeleteCascade removes one question and the references it wrote. Replies to it
// survive with `parent_id` set to NULL rather than vanishing with their parent.
func (r *QuestionRepository) DeleteCascade(ctx context.Context, questionID string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := deleteRefsBySource(ctx, tx, "question", []string{questionID}); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Table("questions").Where("id=?", questionID).Exec(ctx); err != nil {
			return fmt.Errorf("delete question: %w", err)
		}
		return nil
	})
}

// deleteRefsBySource clears both reference tables for a set of sources. They are
// written together on every edit and have to be cleared together too; splitting
// them is how one of the two gets forgotten at the next call site.
func deleteRefsBySource(ctx context.Context, tx bun.Tx, sourceType string, sourceIDs []string) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	if _, err := tx.NewDelete().Table("crossrefs").
		Where("source_type=?", sourceType).
		Where("source_id IN (?)", bun.In(sourceIDs)).Exec(ctx); err != nil {
		return fmt.Errorf("delete crossrefs for %s: %w", sourceType, err)
	}
	if _, err := tx.NewDelete().Table("external_links").
		Where("source_type=?", sourceType).
		Where("source_id IN (?)", bun.In(sourceIDs)).Exec(ctx); err != nil {
		return fmt.Errorf("delete external links for %s: %w", sourceType, err)
	}
	return nil
}
