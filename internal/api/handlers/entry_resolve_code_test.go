package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
	"github.com/dovod-app/app/internal/testdb"
	"github.com/google/uuid"
)

// This handler resolved a short code straight from the repository, so any
// authenticated caller who knew a research id could read any entry of it —
// title, status and full content. The test is here rather than in the service
// package on purpose: EntryService.GetByIDOrCode was always scoped correctly,
// and what went wrong was a handler reaching past it.
func TestResolveCode_DoesNotCrossUsers(t *testing.T) {
	log := slog.Default()
	db, err := storage.NewDB(testdb.Config(t), log)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	userRepo := storage.NewUserRepository(db)
	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)

	researchSvc := service.NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), service.NewAccess(storage.NewTeamRepository(db), false), nopNotifier{}, log)
	entrySvc := service.NewEntryService(entryRepo, sectionRepo, researchRepo, service.NewAccess(storage.NewTeamRepository(db), false), nil, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, nopNotifier{}, log)
	handler := NewEntryHandler(entrySvc, researchSvc, entryRepo, researchRepo, storage.NewUserRepository(db), storage.NewTeamRepository(db), log)

	teamRepo := storage.NewTeamRepository(db)
	alice := &domain.User{ID: uuid.New().String(), Email: "alice@test.com", PasswordHash: "x", Name: "Alice"}
	mallory := &domain.User{ID: uuid.New().String(), Email: "mallory@test.com", PasswordHash: "x", Name: "Mallory"}
	for _, u := range []*domain.User{alice, mallory} {
		if err := userRepo.Create(t.Context(), u); err != nil {
			t.Fatalf("create user: %v", err)
		}
		// The personal team registration would have created. Without it the
		// user cannot own a research, which is not a state the product makes.
		team := &domain.Team{ID: uuid.New().String(), Name: u.Name, Personal: true, CreatedBy: u.ID}
		if err := teamRepo.CreateWithOwner(t.Context(), team, u.ID); err != nil {
			t.Fatalf("create personal team: %v", err)
		}
	}

	ctxAlice := auth.WithUser(t.Context(), alice)
	research, sections, err := researchSvc.Create(ctxAlice, service.CreateResearchRequest{
		Name: "Alice's Research", Goal: "Test",
		Sections: []service.CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}
	secret, err := entrySvc.Create(ctxAlice, service.CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[0].ID,
		Title:      "Private findings",
		Content:    "The passphrase is hunter2.",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	call := func(user *domain.User) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/researches/"+research.ID+"/entries/by-code/"+secret.Code, nil)
		req.SetPathValue("id", research.ID)
		req.SetPathValue("code", secret.Code)
		if user != nil {
			req = req.WithContext(auth.WithUser(req.Context(), user))
		}
		rec := httptest.NewRecorder()
		handler.ResolveCode(rec, req)
		return rec
	}

	t.Run("the owner reads their entry", func(t *testing.T) {
		rec := call(alice)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var got struct {
			Data domain.Entry `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Data.ID != secret.ID {
			t.Errorf("id = %q, want %q", got.Data.ID, secret.ID)
		}
	})

	t.Run("another user cannot use a uuid through their own research", func(t *testing.T) {
		// The code branch was fixed; the uuid branch of GetByIDOrCode still
		// resolved globally, so Mallory could pass HER research id and Alice's
		// entry uuid and read it.
		mallorysResearch, _, err := researchSvc.Create(auth.WithUser(t.Context(), mallory), service.CreateResearchRequest{
			Name: "Mallory's Research", Goal: "Test",
			Sections: []service.CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
		})
		if err != nil {
			t.Fatalf("create research: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.SetPathValue("id", mallorysResearch.ID)
		req.SetPathValue("code", secret.ID)
		req = req.WithContext(auth.WithUser(req.Context(), mallory))
		rec := httptest.NewRecorder()
		handler.ResolveCode(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "hunter2") {
			t.Errorf("response leaked the entry: %s", rec.Body.String())
		}
	})

	t.Run("another user gets nothing", func(t *testing.T) {
		rec := call(mallory)
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
		// Body matters as much as the status: a 500 carrying the row would leak
		// just as well as a 200.
		for _, leak := range []string{"hunter2", "Private findings", secret.ID} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("response leaked %q: %s", leak, rec.Body.String())
			}
		}
	})
}

type nopNotifier struct{}

func (nopNotifier) Notify(service.Event) {}
