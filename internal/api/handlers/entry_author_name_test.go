package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
	"github.com/dovod-app/app/internal/testdb"
	"github.com/google/uuid"
)

// The right to read a document does not carry the right to know who wrote it.
//
// Those two come apart through ordinary use — a research moves to another team,
// or a member leaves and new ones join — and the entry payload was handing the
// author's name, or their email when they had no name, to whoever could open
// the document. This test is at the handler because that is where the decision
// is made: EntryService was always scoped correctly.
func TestAuthorName_OnlyForSomebodyInTheOwningTeam(t *testing.T) {
	log := slog.Default()
	db, err := storage.NewDB(testdb.Config(t), log)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	userRepo := storage.NewUserRepository(db)
	teamRepo := storage.NewTeamRepository(db)
	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	access := service.NewAccess(teamRepo, false)

	researchSvc := service.NewResearchService(researchRepo, sectionRepo, teamRepo, access, nopNotifier{}, log)
	entrySvc := service.NewEntryService(entryRepo, sectionRepo, researchRepo, access, nil,
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db),
		storage.NewCrossRefRepository(db), nil, nopNotifier{}, log)
	handler := NewEntryHandler(entrySvc, researchSvc, entryRepo, researchRepo, userRepo, teamRepo, log)

	// Alice writes; Bob shares her team; Carol has no name at all, which is the
	// normal state for an API-key or OAuth account and used to mean her email
	// was published instead.
	alice := &domain.User{ID: uuid.New().String(), Email: "alice.private@example.com", PasswordHash: "x", Name: "Alice"}
	bob := &domain.User{ID: uuid.New().String(), Email: "bob@example.com", PasswordHash: "x", Name: "Bob"}
	carol := &domain.User{ID: uuid.New().String(), Email: "carol.private@example.com", PasswordHash: "x"}
	for _, u := range []*domain.User{alice, bob, carol} {
		if err := userRepo.Create(t.Context(), u); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	team := &domain.Team{ID: uuid.New().String(), Name: "Shared", CreatedBy: alice.ID}
	if err := teamRepo.CreateWithOwner(t.Context(), team, alice.ID); err != nil {
		t.Fatalf("create team: %v", err)
	}
	if err := teamRepo.AddMember(t.Context(), team.ID, bob.ID, domain.TeamEditor, alice.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	ctxAlice := auth.WithUser(t.Context(), alice)
	research, sections, err := researchSvc.Create(ctxAlice, service.CreateResearchRequest{
		TeamID: team.ID, Name: "Shared research", Goal: "Test",
		Sections: []service.CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}
	entry, err := entrySvc.Create(ctxAlice, service.CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Title: "A finding", Content: "the body",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	read := func(as *domain.User) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/entries/"+entry.ID, nil)
		req.SetPathValue("id", entry.ID)
		req = req.WithContext(auth.WithUser(req.Context(), as))
		rec := httptest.NewRecorder()
		handler.Get(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	// The case the field exists for. If this stops being true the negative
	// assertions below become vacuous, which is how the first version of this
	// guard passed while protecting nothing.
	if got := read(bob)["author_name"]; got != "Alice" {
		t.Fatalf("a teammate did not get the author's name: %v", got)
	}

	// Alice leaves. Bob can still read the document; he can no longer be told
	// whose it was.
	if err := teamRepo.RemoveMember(t.Context(), team.ID, alice.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	payload := read(bob)
	if got, ok := payload["author_name"]; ok {
		t.Errorf("the name of a former member was still published: %v", got)
	}
	// The rest of the provenance survives. Only the identity goes: the document
	// is still known to have been written, when, and by what kind of author —
	// which is what the line said before a name was ever added to it.
	if payload["author_kind"] == nil || payload["revision"] == nil {
		t.Errorf("removing a member cost the whole provenance line: %v", payload)
	}
}
