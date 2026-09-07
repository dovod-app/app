package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/dovod-app/app/internal/domain"
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
	return s.researches.DeletionSummary(ctx, research.ID)
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

	// Two emissions, and they do not overlap.
	//
	// The plain event is what reaches a client when auth is off: the hub lets
	// everything through in that mode, and there is no user id to address.
	// With auth on it reaches nobody, because the visibility check asks whether
	// the caller may read a research that no longer exists.
	//
	// The directed events are the other half. A directed event bypasses the
	// research check by naming a user, which is the only way to tell somebody
	// about a thing after it has stopped existing — but it is dropped when
	// there is no authenticated connection, which is precisely when the plain
	// one is delivered. Together they cover both modes and duplicate in
	// neither.
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

	count, err := s.entries.CountBySection(ctx, sectionID)
	if err != nil {
		return fmt.Errorf("count entries: %w", err)
	}
	if count > 0 && !force {
		return fmt.Errorf("%w: %d document(s) would be deleted with it", ErrSectionNotEmpty, count)
	}

	// The documents, and the references into and out of them, go together in
	// one transaction inside the repository.
	if err := s.sections.DeleteCascade(ctx, sectionID); err != nil {
		return fmt.Errorf("delete section: %w", err)
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
