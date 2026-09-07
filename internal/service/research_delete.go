package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
)

// ErrSectionNotEmpty refuses a section delete that would take documents with it
// without the caller having said so.
var ErrSectionNotEmpty = errors.New("section is not empty")

// DeletionSummary reports what deleting this research would destroy.
//
// A read, deliberately: anyone who can open the research can see the shape of
// it, and gating the numbers behind ownership would mean the confirmation
// dialog could not tell an editor why the delete control is not theirs.
func (s *ResearchService) DeletionSummary(ctx context.Context, idOrCode string) (domain.DeletionSummary, error) {
	research, err := s.Get(ctx, idOrCode)
	if err != nil {
		return domain.DeletionSummary{}, err
	}
	summary, err := s.researches.DeletionSummary(ctx, research.ID)
	if err != nil {
		return summary, err
	}

	// The citing researches are filtered here, not in the query, because only
	// the service knows who is asking.
	//
	// A cross-reference resolves without asking what its author may see, so the
	// raw list names researches in teams the caller is not in — their codes and
	// their names. Counting only the visible ones follows the rule
	// `Access.VisibleIncomingCrossRefs` already states: even the count
	// announces that an unseen research cites this one. The owner is therefore
	// warned about the breakage they can see and not about the breakage they
	// cannot, which is the same trade the rest of the cross-reference surface
	// makes.
	// Asked of the guard rather than of the user id. `uid != "" && ...` failed
	// open for a caller who is nobody, which on an instance with accounts is a
	// stranger — an anonymous stdio session was handed the codes and the names
	// of every project citing this one. Access.Read answers for the caller the
	// context actually carries, in both postures: with accounts off it lets
	// everything through, which is that mode's rule everywhere else too.
	visible := summary.IncomingFrom[:0]
	total := 0
	for _, c := range summary.IncomingFrom {
		if err := s.access.Read(ctx, c.ID); err != nil {
			continue
		}
		total += c.Refs
		visible = append(visible, c)
	}
	summary.IncomingFromTotal = len(visible)
	if len(visible) > storage.CitingResearchLimit {
		visible = visible[:storage.CitingResearchLimit]
	}
	summary.IncomingFrom = visible
	summary.IncomingRefs = total
	return summary, nil
}

// Delete destroys a research and everything under it.
//
// Real deletion, not a tombstone. The product's promise is that the data is a
// file you own, and a hidden graveyard inside that file is the opposite of it;
// `archived` is the reversible path and already exists.
//
// Owner only. An editor is trusted with the contents of a research and not with
// its existence — deleting one destroys work belonging to everybody else in the
// team, which is not a decision a confirmation dialog can transfer.
func (s *ResearchService) Delete(ctx context.Context, idOrCode string) error {
	// Get first, so an unknown id or one belonging to a team the caller is not
	// in comes back as ErrNotFound before anything else happens — including
	// before the ownership check, whose ErrForbidden would otherwise confirm
	// that the research exists.
	research, err := s.Get(ctx, idOrCode)
	if err != nil {
		return err
	}
	if err := s.access.Admin(ctx, research.ID); err != nil {
		return err
	}

	// Read the audience before destroying the thing that defines it.
	//
	// A research event is delivered by asking who may read the research, and
	// after this returns nobody may, because it is not there. Announcing it
	// afterwards would announce it to exactly nobody — the audience that needs
	// it most. Same reason TeamService.Delete reads its member list first.
	var members []*domain.TeamMember
	if research.TeamID != "" && s.teams != nil {
		members, err = s.teams.ListMembers(ctx, research.TeamID)
		if err != nil {
			return fmt.Errorf("list members: %w", err)
		}
	}

	if err := s.researches.DeleteCascade(ctx, research.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Someone else deleted it between the Get and here.
			return ErrNotFound
		}
		return fmt.Errorf("delete research: %w", err)
	}

	// Two emissions, and they must not overlap for a member.
	//
	// The plain event is what reaches a client when auth is off: the hub lets
	// everything through in that mode, and there is no user id to address. With
	// auth on it reaches no *member*, because the visibility check asks whether
	// the caller may read a research that no longer exists.
	//
	// It does reach a share socket, which is right and was not designed: the
	// hub decides a share connection from the token's own scope and never asks
	// Access, so a visitor watching the public page is told the page is gone
	// rather than finding out by failing. There is no duplicate there — a share
	// connection has no user id, so no directed event can name it — and the
	// socket closes with 4401 at the next credential sweep either way.
	//
	// The directed events are the other half. A directed event bypasses the
	// research check by naming a user, which is the only way to tell somebody
	// about a thing after it has stopped existing — but it is dropped when there
	// is no authenticated connection, which is precisely when the plain one is
	// delivered.
	//
	// That non-overlap is not free, and this comment used to assert it as though
	// it were. The hub answers from a verdict cache before it asks anything, so
	// a `true` cached from any event in the previous minute served the plain
	// event too and every member was told twice — twice, on a notice that raises
	// a toast with no timeout. `forgetOn` in internal/api/ws/hub.go lists
	// `research.deleted` for exactly this reason; if that line goes, this one
	// becomes a lie again.
	base := Event{
		Type:         "research.deleted",
		ResearchID:   research.ID,
		ResearchCode: research.Code,
		EntityID:     research.ID,
		Entity:       "research",
		Name:         research.Name,
	}
	emit(ctx, s.events, base)
	for _, m := range members {
		directed := base
		directed.TargetUserID = m.UserID
		emit(ctx, s.events, directed)
	}
	return nil
}

// Delete removes a section.
//
// It refuses a section that still holds documents unless the caller passes
// force. Deleting a section silently taking five documents with it is the exact
// accident the confirmation is there to prevent, and an API that makes it a
// one-liner has moved the accident rather than removed it. The error carries
// the count so the caller can say how much is at stake without a second round
// trip.
func (s *SectionService) Delete(ctx context.Context, sectionID string, force bool) error {
	section, err := s.sections.FindByID(ctx, sectionID)
	if err != nil {
		return fmt.Errorf("find section: %w", err)
	}
	if section == nil {
		return ErrNotFound
	}
	if err := s.access.Write(ctx, section.ResearchID); err != nil {
		return err
	}

	// The documents, the references into and out of them, and the not-empty
	// decision all happen in one transaction inside the repository. Counting
	// here first would make the refusal advisory: the connection is released
	// between a COUNT and the DELETE, and a document filed in that window would
	// be destroyed with `force` never set.
	deletedEntries, err := s.sections.DeleteCascade(ctx, sectionID, !force)
	if err != nil {
		var held *storage.SectionHasEntriesError
		if errors.As(err, &held) {
			return fmt.Errorf(
				"%w: %d document(s) would be deleted with it. Empty it first by deleting them, or set force only if the person asked for the documents to go too — never as a retry",
				ErrSectionNotEmpty, held.Count)
		}
		return fmt.Errorf("delete section: %w", err)
	}
	// The documents first, then the section that held them.
	//
	// A page open on one of these documents subscribes by document id; it hears
	// `section.deleted` and cannot tell whether it was looking at a child of
	// that section, so without these it kept rendering a document that is gone
	// and failed on the next save. The order matters only for readability —
	// both arrive before anything can act on either.
	for _, id := range deletedEntries {
		emit(ctx, s.events, Event{
			Type:       "entry.deleted",
			ResearchID: section.ResearchID,
			EntityID:   id,
			Entity:     "entry",
			ParentID:   sectionID,
		})
	}
	emit(ctx, s.events, Event{
		Type:       "section.deleted",
		ResearchID: section.ResearchID,
		EntityID:   sectionID,
		Entity:     "section",
		Name:       section.DisplayName,
	})
	return nil
}

// Delete removes a session and the questions asked in it.
//
// Documents written during the session survive with their `session_id` cleared
// — every dialect declares ON DELETE SET NULL for that column, and it is the
// right rule: a finding is not an artefact of the conversation that produced
// it, and deleting the transcript should not delete the conclusions.
func (s *SessionService) Delete(ctx context.Context, sessionID string) error {
	session, err := s.sessions.FindByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("find session: %w", err)
	}
	if session == nil {
		return ErrNotFound
	}
	if err := s.access.Write(ctx, session.ResearchID); err != nil {
		return err
	}

	if err := s.sessions.DeleteCascade(ctx, sessionID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	emit(ctx, s.events, Event{
		Type:       "session.deleted",
		ResearchID: session.ResearchID,
		EntityID:   sessionID,
		Entity:     "session",
		Name:       session.Title,
	})
	return nil
}

// DeleteQuestion removes one question. Replies to it survive with `parent_id`
// cleared rather than vanishing with their parent.
func (s *SessionService) DeleteQuestion(ctx context.Context, questionID string) error {
	question, err := s.questions.FindByID(ctx, questionID)
	if err != nil {
		return fmt.Errorf("find question: %w", err)
	}
	if question == nil {
		return ErrNotFound
	}
	session, err := s.sessions.FindByID(ctx, question.SessionID)
	if err != nil {
		return fmt.Errorf("find session: %w", err)
	}
	if session == nil {
		return ErrNotFound
	}
	if err := s.access.Write(ctx, session.ResearchID); err != nil {
		return err
	}

	if err := s.questions.DeleteCascade(ctx, questionID); err != nil {
		return fmt.Errorf("delete question: %w", err)
	}
	emit(ctx, s.events, Event{
		Type:       "question.deleted",
		ResearchID: session.ResearchID,
		EntityID:   questionID,
		Entity:     "question",
		ParentID:   session.ID,
		ParentCode: session.Code,
		Name:       question.Text,
	})
	return nil
}
