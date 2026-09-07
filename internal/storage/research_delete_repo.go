package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
		"sections":        &s.Sections,
		"entries":         &s.Entries,
		"sessions":        &s.Sessions,
		"tasks":           &s.Tasks,
		"roadmaps":        &s.Roadmaps,
		"annotations":     &s.Annotations,
		"research_memory": &s.Memory,
	}
	for table, into := range direct {
		if err := selectRow(ctx, r.db.NewSelect().
			ColumnExpr("COUNT(*)").TableExpr(table).
			Where("research_id=?", researchID)).Scan(into); err != nil {
			return s, fmt.Errorf("count %s: %w", table, err)
		}
	}

	// Live links only, the same definition ShareRepository uses. Revoking sets
	// `revoked_at` rather than removing the row, so a plain COUNT told a reader
	// that three links would stop working while the badge on the project page
	// beside it said one — a count disagreeing with the list under it, in the
	// dialog whose whole job is to be accurate.
	if err := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("COUNT(*)").TableExpr("shares").
		Where("research_id=?", researchID).
		Where("revoked_at IS NULL").
		Where("expires_at IS NULL OR expires_at > ?", time.Now().UTC().Format(time.DateTime))).
		Scan(&s.Shares); err != nil {
		return s, fmt.Errorf("count shares: %w", err)
	}

	// Questions hang off sessions, so they are counted through the join rather
	// than by a research_id column they do not have.
	if err := selectRow(ctx, r.db.NewSelect().
		ColumnExpr("COUNT(*)").TableExpr("questions AS q").
		Join("JOIN sessions AS s ON s.id = q.session_id").
		Where("s.research_id=?", researchID)).Scan(&s.Questions); err != nil {
		return s, fmt.Errorf("count questions: %w", err)
	}

	// Which researches cite this one, and how many times each.
	//
	// Grouped rather than counted flat, and carrying the id, because the
	// service has to drop the ones the caller may not read before either the
	// names or the total reach them. Cross-references resolve without asking
	// what their author may see — that is deliberate — so this join would
	// otherwise announce the existence and the *name* of a research in a team
	// the caller is not in. `Access.VisibleIncomingCrossRefs` states the rule
	// this query has to obey: even a stripped version announces that an unseen
	// research cites this one.
	rows, err := r.db.NewSelect().
		ColumnExpr("r.id, r.code, r.name, COUNT(*) AS refs").
		TableExpr("crossrefs AS c").
		Join("JOIN researches AS r ON r.id = c.source_research_id").
		Where("c.target_research_id=?", researchID).
		Where("c.source_research_id<>?", researchID).
		GroupExpr("r.id, r.code, r.name").
		OrderExpr("r.code").
		Rows(ctx)
	if err != nil {
		return s, fmt.Errorf("list citing researches: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.CitingResearch
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.Refs); err != nil {
			return s, fmt.Errorf("scan citing research: %w", err)
		}
		s.IncomingFrom = append(s.IncomingFrom, c)
	}
	if err := rows.Err(); err != nil {
		return s, fmt.Errorf("list citing researches: %w", err)
	}

	return s, nil
}

// CitingResearchLimit caps the names, not the count. The count is what the
// decision turns on; the names are there so it is not an abstraction, and a
// dialog listing eighty of them is neither readable nor a better warning.
const CitingResearchLimit = 10

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
// SectionHasEntriesError is returned when expectEmpty was asked for and the
// section turned out to hold documents. It carries the count because the
// refusal a caller sees names it, and the only place that number is true is
// inside the transaction that just read it.
type SectionHasEntriesError struct{ Count int }

func (e *SectionHasEntriesError) Error() string {
	return fmt.Sprintf("section has %d entries", e.Count)
}

// It returns the ids of the documents that went with the section, because the
// service has to name each of them in an event: a tab open on one of those
// documents hears `section.deleted` and has no way to know it was looking at a
// child of it, so it kept the page and 404ed on the next save.
func (r *SectionRepository) DeleteCascade(ctx context.Context, sectionID string, expectEmpty bool) (deleted []string, err error) {
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var entryIDs []string
		if err := tx.NewSelect().Column("id").Table("entries").
			Where("section_id=?", sectionID).Scan(ctx, &entryIDs); err != nil {
			return fmt.Errorf("collect entries: %w", err)
		}

		// The refusal is decided here, inside the transaction, and not from a
		// count the service took first. Counted outside, it was advisory: the
		// connection is released between the COUNT and the DELETE, so an agent
		// filing a document into the section through `entry_create` in that
		// window had it destroyed with `force` never set and nothing in the
		// response saying a document had gone.
		if expectEmpty && len(entryIDs) > 0 {
			return &SectionHasEntriesError{Count: len(entryIDs)}
		}

		if len(entryIDs) > 0 {
			if err := deleteRefsBySource(ctx, tx, "entry", entryIDs); err != nil {
				return err
			}
			// A mark writes references of its own, from its resolution text,
			// under source_type "annotation". The marks themselves cascade with
			// their documents; their references do not, because `crossrefs` has
			// no foreign keys — so the cited document kept a backlink from a
			// mark that no longer exists, and the deletion preview counted it.
			if err := deleteRefsOfAnnotationsOn(ctx, tx, entryIDs); err != nil {
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
		deleted = entryIDs
		return nil
	})
	if err != nil {
		// Nothing was committed, so nothing was deleted — the caller must not
		// announce documents that are still there.
		return nil, err
	}
	return deleted, nil
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
		// `questions:Q:<sessionID>` is scoped by the session, so once the
		// session is gone the row can never be reached again. Left behind, it
		// also survives the research delete, which looks its counters up by the
		// ids of the sessions that still exist.
		if _, err := tx.NewDelete().Table("storage_counters").
			Where("scope_key LIKE ?", "%:"+sessionID).Exec(ctx); err != nil {
			return fmt.Errorf("delete counters for session %s: %w", sessionID, err)
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

// UnresolveTargetEntries turns references pointing at these documents back into
// unresolved text, keeping `target_ref` so the reader still sees what was cited.
//
// Used by the single-document delete, which has no transaction of its own; the
// section and research cascades do the same thing inside theirs.
func (r *CrossRefRepository) UnresolveTargetEntries(ctx context.Context, entryIDs []string) error {
	if len(entryIDs) == 0 {
		return nil
	}
	_, err := r.db.NewUpdate().Table("crossrefs").
		Set("target_entry_id=NULL").
		Set("target_research_id=NULL").
		Set("resolved=0").
		Where("target_entry_id IN (?)", bun.In(entryIDs)).Exec(ctx)
	return err
}

// deleteRefsOfAnnotationsOn clears the references written by the marks on these
// documents, without needing their ids: one statement, so the section cascade
// and the single-document delete cannot drift.
//
// A subquery rather than two round trips because the section cascade runs
// inside a transaction and the count of marks is unbounded; `annotations` is a
// different table from `crossrefs`, so no dialect refuses it.
func deleteRefsOfAnnotationsOn(ctx context.Context, q Querier, entryIDs []string) error {
	if len(entryIDs) == 0 {
		return nil
	}
	sub := q.NewSelect().Column("id").Table("annotations").Where("entry_id IN (?)", bun.In(entryIDs))
	if _, err := q.NewDelete().Table("crossrefs").
		Where("source_type=?", "annotation").
		Where("source_id IN (?)", sub).Exec(ctx); err != nil {
		return fmt.Errorf("delete crossrefs written by marks: %w", err)
	}
	return nil
}

// DeleteForAnnotationsOn is the same cleanup for a caller with no transaction of
// its own — the single-document delete.
func (r *CrossRefRepository) DeleteForAnnotationsOn(ctx context.Context, entryIDs []string) error {
	return deleteRefsOfAnnotationsOn(ctx, r.db, entryIDs)
}

// DeleteBySource clears the references one source wrote. `ReplaceForSource` with
// no refs does the same thing; this name says what the caller means.
func (r *CrossRefRepository) DeleteBySource(ctx context.Context, sourceType, sourceID string) error {
	_, err := r.db.NewDelete().Table("crossrefs").
		Where("source_type=?", sourceType).
		Where("source_id=?", sourceID).Exec(ctx)
	return err
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
