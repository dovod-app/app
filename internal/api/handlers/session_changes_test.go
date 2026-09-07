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

// The session page asks this route on every load now, not only when somebody
// opens the Changes tab — the count on the tab is the answer to "did anything
// happen last night", and a badge that appears only after a click answers
// nothing. That makes two properties worth pinning at the route rather than the
// service:
//
//   - ?summary=1 must cost no diffs and must agree with the list it labels.
//   - a bare session uuid with no ?research= to scope it is the branch the eager
//     fetch takes, and it must refuse another user's session with the same 404
//     as one that does not exist.
func TestSessionChanges_SummaryAndScope(t *testing.T) {
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
	sessionRepo := storage.NewSessionRepository(db)

	access := service.NewAccess(teamRepo, false)
	researchSvc := service.NewResearchService(researchRepo, sectionRepo, teamRepo, access, nopNotifier{}, log)
	entrySvc := service.NewEntryService(entryRepo, sectionRepo, researchRepo, access, sessionRepo,
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db),
		storage.NewCrossRefRepository(db), nil, nopNotifier{}, log)
	sessionSvc := service.NewSessionService(db, sessionRepo, storage.NewQuestionRepository(db),
		researchRepo, access, nil, nopNotifier{}, log)
	handler := NewRevisionHandler(entrySvc, sessionSvc, researchSvc, log)

	alice := &domain.User{ID: uuid.New().String(), Email: "alice@test.com", PasswordHash: "x", Name: "Alice"}
	mallory := &domain.User{ID: uuid.New().String(), Email: "mallory@test.com", PasswordHash: "x", Name: "Mallory"}
	for _, u := range []*domain.User{alice, mallory} {
		if err := userRepo.Create(t.Context(), u); err != nil {
			t.Fatalf("create user: %v", err)
		}
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

	// One entry predates the session, one is born inside it: the count has to
	// separate them the same way the list does.
	existing, err := entrySvc.Create(ctxAlice, service.CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Title: "Older finding", Content: "Original body.",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	sess, _, err := sessionSvc.Create(ctxAlice, service.CreateSessionRequest{
		ResearchID: research.ID, Title: "Deep dive", Focus: "everything",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	body := "Original body, now revised."
	if _, err := entrySvc.Update(ctxAlice, existing.ID, service.UpdateEntryRequest{Content: &body}); err != nil {
		t.Fatalf("update entry: %v", err)
	}
	if _, err := entrySvc.Create(ctxAlice, service.CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		SessionID: sess.ID, Title: "Fresh finding", Content: "Brand new.",
	}); err != nil {
		t.Fatalf("create session entry: %v", err)
	}

	call := func(user *domain.User, sessionID, query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/changes"+query, nil)
		req.SetPathValue("id", sessionID)
		if user != nil {
			req = req.WithContext(auth.WithUser(req.Context(), user))
		}
		rec := httptest.NewRecorder()
		handler.SessionChanges(rec, req)
		return rec
	}

	t.Run("summary counts agree with the list and carry no diffs", func(t *testing.T) {
		rec := call(alice, sess.ID, "?summary=1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var summary struct {
			Data struct {
				Created  int `json:"created"`
				Modified int `json:"modified"`
				Count    int `json:"count"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if summary.Data.Created != 1 || summary.Data.Modified != 1 || summary.Data.Count != 2 {
			t.Errorf("summary = %+v, want 1 created, 1 modified, 2 total", summary.Data)
		}

		// The badge must not be able to disagree with the screen underneath it.
		full := call(alice, sess.ID, "")
		var list struct {
			Data struct {
				Created  int              `json:"created"`
				Modified int              `json:"modified"`
				Changes  []map[string]any `json:"changes"`
			} `json:"data"`
		}
		if err := json.Unmarshal(full.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(list.Data.Changes) != summary.Data.Count {
			t.Errorf("summary count %d disagrees with %d cards", summary.Data.Count, len(list.Data.Changes))
		}
		if list.Data.Created != summary.Data.Created || list.Data.Modified != summary.Data.Modified {
			t.Errorf("summary %d/%d disagrees with list %d/%d",
				summary.Data.Created, summary.Data.Modified, list.Data.Created, list.Data.Modified)
		}

		// A diff in the summary would mean the cheap path is not cheap.
		if _, has := summary.Data.Count, false; has {
			t.Fatal("unreachable")
		}
		if bytesHave(rec.Body.Bytes(), "\"diff\"") || bytesHave(rec.Body.Bytes(), "\"changes\"") {
			t.Errorf("summary response carries the list it was meant to avoid: %s", rec.Body.String())
		}
	})

	t.Run("another user cannot read the session by its uuid", func(t *testing.T) {
		for _, query := range []string{"", "?summary=1"} {
			rec := call(mallory, sess.ID, query)
			if rec.Code != http.StatusNotFound {
				t.Errorf("query %q: status = %d, want 404: %s", query, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("an unknown session uuid refuses the same way", func(t *testing.T) {
		rec := call(alice, uuid.New().String(), "?summary=1")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})
}

func bytesHave(haystack []byte, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(string(haystack), needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
