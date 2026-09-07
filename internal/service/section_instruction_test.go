package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/uptrace/bun"
)

// An instruction survives an export and an import. Without this the convention
// a team wrote once is lost the first time the research moves, and the next
// eighteen documents are written from scratch again — which is the failure the
// whole feature exists to end.
func TestSectionInstruction_SurvivesAPortableRoundTrip(t *testing.T) {
	ctx := context.Background()
	exportSvc, researchSvc, sectionSvc, _, _, _, _ := setupExportService(t)

	const instruction = "Назови производящий сервис в поле service.\nНазови потребителя."
	source, sections, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name:     "Спецификации",
		Sections: []CreateSectionRequest{{Name: "specs", DisplayName: "Спецификации"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := sectionSvc.Update(ctx, sections[0].ID, UpdateSectionRequest{
		Instruction: ptr(instruction),
	}); err != nil {
		t.Fatalf("set instruction: %v", err)
	}

	dump, err := exportSvc.Export(ctx, source.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if got := dump.Research.Sections[0].Instruction; got != instruction {
		t.Fatalf("the export dropped the instruction: %q", got)
	}

	restored, warnings, err := exportSvc.Import(ctx, dump, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("a clean round trip warned about something: %v", warnings)
	}
	back, err := sectionSvc.List(ctx, restored.ID)
	if err != nil || len(back) != 1 {
		t.Fatalf("list: %+v %v", back, err)
	}
	if back[0].Instruction != instruction {
		t.Errorf("the import dropped the instruction: %q", back[0].Instruction)
	}
}

// An over-long instruction in a file does not fail the import — the documents
// are the point — but it does not vanish quietly either. The importer says
// which section lost one and how far over it was, because nobody reads a
// silently empty field.
func TestSectionInstruction_ImportWarnsWhenItDropsOne(t *testing.T) {
	ctx := context.Background()
	exportSvc, _, sectionSvc, _, _, _, _ := setupExportService(t)

	long := strings.Repeat("я", domain.SectionInstructionMax+31)
	research, warnings, err := exportSvc.Import(ctx, &domain.ExportData{
		Version: 1,
		Research: domain.ExportResearch{
			Name:   "Restored from a file somebody edited",
			Status: domain.ResearchActive,
			Sections: []domain.ExportSection{
				{Name: "specs", Status: domain.SectionDraft, Instruction: long},
				{Name: "notes", Status: domain.SectionDraft, Instruction: "Одно решение на документ."},
			},
		},
	}, "")
	if err != nil {
		t.Fatalf("an over-long instruction failed the whole import: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %v", warnings)
	}
	for _, want := range []string{"specs", "531", "500"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("the warning does not name %q: %q", want, warnings[0])
		}
	}

	sections, err := sectionSvc.List(ctx, research.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, sec := range sections {
		switch sec.Name {
		case "specs":
			// Dropped, not cut in half: a truncated rule reads as a whole one.
			if sec.Instruction != "" {
				t.Errorf("an over-long instruction was truncated rather than dropped: %q", sec.Instruction)
			}
		case "notes":
			if sec.Instruction != "Одно решение на документ." {
				t.Errorf("a good instruction beside a refused one was lost: %q", sec.Instruction)
			}
		}
	}
}

func instructionService(t *testing.T, db *bun.DB) *SectionService {
	t.Helper()
	return NewSectionService(
		storage.NewSectionRepository(db),
		storage.NewEntryRepository(db),
		storage.NewResearchRepository(db),
		testAccess(db),
		&mockNotifier{},
		slog.Default(),
	)
}

// The cap is the mechanism, not a storage budget: an instruction that runs to
// paragraphs is summarised and diluted by the reader it was written for. So it
// is refused rather than truncated, and counted in runes — a Cyrillic
// instruction is not worth half a Latin one.
func TestSectionInstruction_CapIsCountedInRunesAndRefusesRatherThanTruncates(t *testing.T) {
	db := setupTestDB(t)
	svc := instructionService(t, db)
	ctx := context.Background()

	t.Run("a full-length Cyrillic instruction is accepted whole", func(t *testing.T) {
		_, sec := createTestResearchWithSection(t, db)
		text := strings.Repeat("я", domain.SectionInstructionMax)

		updated, err := svc.Update(ctx, sec.ID, UpdateSectionRequest{Instruction: &text})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if updated.Instruction != text {
			t.Fatalf("a %d-rune instruction came back %d runes long — the cap is counting bytes",
				domain.SectionInstructionMax, len([]rune(updated.Instruction)))
		}
	})

	t.Run("one rune over is refused, and nothing is stored", func(t *testing.T) {
		_, sec := createTestResearchWithSection(t, db)
		kept := "Name the producing service. State the consumer."
		if _, err := svc.Update(ctx, sec.ID, UpdateSectionRequest{Instruction: &kept}); err != nil {
			t.Fatalf("seed instruction: %v", err)
		}

		long := strings.Repeat("я", domain.SectionInstructionMax+1)
		_, err := svc.Update(ctx, sec.ID, UpdateSectionRequest{Instruction: &long})
		if !errors.Is(err, ErrSectionInstructionLong) {
			t.Fatalf("expected ErrSectionInstructionLong, got %v", err)
		}

		got, err := svc.Get(ctx, sec.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Instruction != kept {
			// Truncation would leave a rule whose second sentence is silently
			// gone, and the agent re-reading it could not tell.
			t.Fatalf("a refused instruction still changed the section: %q", got.Instruction)
		}
	})

	t.Run("a refusal does not commit the rest of the same update", func(t *testing.T) {
		_, sec := createTestResearchWithSection(t, db)
		long := strings.Repeat("x", domain.SectionInstructionMax+1)

		_, err := svc.Update(ctx, sec.ID, UpdateSectionRequest{
			DisplayName: ptr("Renamed by a refused call"),
			Instruction: &long,
		})
		if !errors.Is(err, ErrSectionInstructionLong) {
			t.Fatalf("expected ErrSectionInstructionLong, got %v", err)
		}
		got, err := svc.Get(ctx, sec.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.DisplayName == "Renamed by a refused call" {
			t.Fatal("a refused update wrote its other fields anyway")
		}
	})
}

func TestSectionInstruction_TrimsAndClears(t *testing.T) {
	db := setupTestDB(t)
	svc := instructionService(t, db)
	ctx := context.Background()
	_, sec := createTestResearchWithSection(t, db)

	// Trailing whitespace out of a textarea must not cost the writer a
	// sentence: the cap is applied after trimming.
	padded := "\n  Name the producing service.\n  State the consumer.\n\n"
	updated, err := svc.Update(ctx, sec.ID, UpdateSectionRequest{Instruction: &padded})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Instruction != "Name the producing service.\n  State the consumer." {
		t.Fatalf("instruction was not trimmed: %q", updated.Instruction)
	}
	// The line break survives — this field is three to six lines of
	// imperatives, and collapsing them into a paragraph is what it is against.
	if !strings.Contains(updated.Instruction, "\n") {
		t.Fatal("normalisation flattened a multi-line instruction into one line")
	}

	// An omitted instruction leaves the stored one alone; only an explicit
	// empty string removes it.
	untouched, err := svc.Update(ctx, sec.ID, UpdateSectionRequest{DisplayName: ptr("Specs")})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if untouched.Instruction == "" {
		t.Fatal("an update that never mentioned the instruction erased it")
	}

	cleared, err := svc.Update(ctx, sec.ID, UpdateSectionRequest{Instruction: ptr("")})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared.Instruction != "" {
		t.Fatalf("instruction was not cleared: %q", cleared.Instruction)
	}
}

// Creation takes an instruction from somewhere else — an import, or a restore
// of a file somebody edited — so it drops an unusable one rather than refusing
// the whole research, exactly as validFieldSpec does beside it.
//
// Dropped, never truncated: a half-instruction reads as a whole one, and the
// agent re-reading it on every write could not tell it had been cut.
func TestSectionInstruction_CreationDropsAnOverLongOneInsteadOfFailing(t *testing.T) {
	db := setupTestDB(t)
	teams := storage.NewTeamRepository(db)
	svc := NewResearchService(storage.NewResearchRepository(db), storage.NewSectionRepository(db),
		teams, NewAccess(teams), &mockNotifier{}, slog.Default())
	ctx := context.Background()

	_, sections, err := svc.Create(ctx, CreateResearchRequest{
		Name: "Imported",
		Sections: []CreateSectionRequest{
			{Name: "keeps", Instruction: "  Name the producing service.  "},
			{Name: "drops", Instruction: strings.Repeat("x", domain.SectionInstructionMax+1)},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0].Instruction != "Name the producing service." {
		t.Errorf("a valid instruction was not carried into creation: %q", sections[0].Instruction)
	}
	if sections[1].Instruction != "" {
		t.Errorf("an over-long instruction was stored on creation: %q", sections[1].Instruction)
	}
}
