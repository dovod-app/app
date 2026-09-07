package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
)

// ErrSectionInstructionLong refuses an over-long instruction rather than
// truncating one. Truncation would leave a rule whose second half is silently
// missing, and the reader — an agent re-reading this on every write — would
// have no way to know it was cut.
var ErrSectionInstructionLong = fmt.Errorf(
	"instruction must be %d characters or fewer: it is read before every document written here, and a longer one gets summarised and diluted by the reader it was written for",
	domain.SectionInstructionMax)

type UpdateSectionRequest struct {
	DisplayName *string
	Description *string
	Status      *domain.SectionStatus
	Position    *int
	// Instruction replaces how to write in this section. A pointer so an
	// omitted instruction (leave it alone) is distinguishable from an empty
	// string, which removes it.
	Instruction *string
	// FieldSpec replaces the section's whole declaration. A pointer so an
	// omitted spec (leave it alone) is distinguishable from an empty one, which
	// removes every field — and removing a field never deletes the values
	// documents already carry under it.
	FieldSpec *[]domain.FieldSpec
}

type SectionService struct {
	sections   *storage.SectionRepository
	entries    *storage.EntryRepository
	researches *storage.ResearchRepository
	access     *Access
	events     EventNotifier
	log        *slog.Logger
}

func NewSectionService(sections *storage.SectionRepository, entries *storage.EntryRepository, researches *storage.ResearchRepository, access *Access, events EventNotifier, log *slog.Logger) *SectionService {
	return &SectionService{sections: sections, entries: entries, researches: researches, access: access, events: events, log: log}
}

func (s *SectionService) List(ctx context.Context, researchID string) ([]*domain.Section, error) {
	if err := s.access.Read(ctx, researchID); err != nil {
		return nil, err
	}
	sections, err := s.sections.FindByResearch(ctx, researchID)
	if err != nil {
		return nil, err
	}
	redactSectionsForShare(ctx, sections)
	return sections, nil
}

func (s *SectionService) Get(ctx context.Context, id string) (*domain.Section, error) {
	section, err := s.sections.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find section: %w", err)
	}
	if section == nil {
		return nil, ErrNotFound
	}
	if err := s.access.Read(ctx, section.ResearchID); err != nil {
		return nil, ErrNotFound
	}
	redactSectionForShare(ctx, section)
	return section, nil
}

func (s *SectionService) Update(ctx context.Context, id string, req UpdateSectionRequest) (*domain.Section, error) {
	section, err := s.sections.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find section: %w", err)
	}
	if section == nil {
		return nil, ErrNotFound
	}
	if err := s.access.Write(ctx, section.ResearchID); err != nil {
		return nil, err
	}

	if req.Status != nil && *req.Status == domain.SectionCompleted {
		count, err := s.entries.CountBySection(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("count entries: %w", err)
		}
		if count == 0 {
			return nil, ErrSectionHasNoEntries
		}
	}

	if req.DisplayName != nil {
		section.DisplayName = normalizeTitle(*req.DisplayName)
	}
	if req.Description != nil {
		section.Description = normalizeContent(*req.Description)
	}
	if req.Status != nil {
		section.Status = *req.Status
	}
	if req.Position != nil {
		section.Position = *req.Position
	}
	if req.Instruction != nil {
		instruction := strings.TrimSpace(normalizeContent(*req.Instruction))
		// Runes, not bytes: a Cyrillic instruction is not worth half a Latin
		// one. Counted after trimming, so trailing whitespace from a textarea
		// never costs the writer a sentence.
		if utf8.RuneCountInString(instruction) > domain.SectionInstructionMax {
			return nil, ErrSectionInstructionLong
		}
		section.Instruction = instruction
	}
	if req.FieldSpec != nil {
		specs := *req.FieldSpec
		if errs := domain.ValidateFieldSpecs(specs); len(errs) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrInvalidFieldSpec, strings.Join(errs, "; "))
		}
		for i := range specs {
			specs[i].Label = normalizeTitle(specs[i].Label)
			specs[i].Help = normalizeTitle(specs[i].Help)
		}
		// The version is bumped whenever the declaration changes, and only then.
		// Documents record the version they were validated against, so without
		// this a spec edit would silently restate history: a document written
		// under the old rules would be indistinguishable from one breaking the
		// new ones.
		if !sameFieldSpec(section.FieldSpec, specs) {
			section.SpecVersion++
		}
		section.FieldSpec = specs
	}

	if err := s.sections.Update(ctx, section); err != nil {
		return nil, fmt.Errorf("update section: %w", err)
	}

	emit(ctx, s.events, Event{Type: "section.updated", ResearchID: section.ResearchID, EntityID: section.ID, Entity: "section"})
	return section, nil
}

func (s *SectionService) CountEntries(ctx context.Context, sectionID string) (int, error) {
	return s.entries.CountBySection(ctx, sectionID)
}

// sameFieldSpec reports whether a submitted declaration is the one already
// stored, so saving a form without changing anything does not bump the version
// and re-date every document's compliance.
func sameFieldSpec(a, b []domain.FieldSpec) bool {
	if len(a) != len(b) {
		return false
	}
	ja, err := json.Marshal(a)
	if err != nil {
		return false
	}
	jb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ja) == string(jb)
}

// redactSectionForShare hides what a team decided to record about its
// documents. The values are stripped on the entry; the declaration is stripped
// here, because a list of twelve field labels with nothing in them still says
// what the team tracks.
//
// The instruction goes for the same reason the research's memory does: it is
// working process, and how a team writes is not part of what they published by
// sending someone a link to the findings.
//
// This is called from List and Get, and from nowhere else — which means the
// write path's safety is not this function's doing. Update returns the section
// unredacted, and so do ResearchService.Create and AddSection; what stops a
// share reading its own instruction back out of them is Access.Write refusing
// a share before the field is ever touched. Two guarantees, not one.
//
// It mutates in place. The repository builds a fresh struct per row today, so
// there is no aliasing; put a cache in front of it and one share read would
// blank the instruction for every owner read after it.
func redactSectionForShare(ctx context.Context, section *domain.Section) {
	if section == nil || auth.ShareFromContext(ctx) == nil {
		return
	}
	section.Instruction = ""
	section.FieldSpec = nil
	section.SpecVersion = 0
}

func redactSectionsForShare(ctx context.Context, sections []*domain.Section) {
	if auth.ShareFromContext(ctx) == nil {
		return
	}
	for _, sec := range sections {
		redactSectionForShare(ctx, sec)
	}
}
