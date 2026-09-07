package handlers

import (
	"bytes"
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

func TestEntryViewHandlers_QueueCheckpointAndPrivateEntryState(t *testing.T) {
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
	viewSvc := service.NewEntryViewService(storage.NewEntryViewRepository(db), entryRepo, access, nopNotifier{})
	viewHandler := NewEntryViewHandler(viewSvc, researchSvc)
	entryHandler := NewEntryHandler(entrySvc, researchSvc, entryRepo, researchRepo, userRepo, teamRepo, log)
	entryHandler.SetEntryViewService(viewSvc)

	reader := &domain.User{ID: uuid.New().String(), Email: "reader-views@test.com", PasswordHash: "x", Name: "Reader"}
	if err := userRepo.Create(t.Context(), reader); err != nil {
		t.Fatalf("create user: %v", err)
	}
	team := &domain.Team{ID: uuid.New().String(), Name: "Reader", Personal: true, CreatedBy: reader.ID}
	if err := teamRepo.CreateWithOwner(t.Context(), team, reader.ID); err != nil {
		t.Fatalf("create team: %v", err)
	}
	ctx := auth.WithUser(t.Context(), reader)
	research, sections, err := researchSvc.Create(ctx, service.CreateResearchRequest{
		Name: "Updates", Goal: "Test",
		Sections: []service.CreateSectionRequest{{Name: "findings", DisplayName: "Findings"}},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}
	entry, err := entrySvc.Create(ctx, service.CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID, Title: "Finding", Content: "v1",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	callList := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/researches/"+research.Code+"/updates", nil)
		req.SetPathValue("id", research.Code)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		viewHandler.List(rec, req)
		return rec
	}
	decodeQueue := func(rec *httptest.ResponseRecorder) service.EntryUpdates {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("queue status = %d: %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Data service.EntryUpdates `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode queue: %v", err)
		}
		return out.Data
	}

	initial := decodeQueue(callList())
	if initial.Count != 1 || initial.New != 1 || initial.Entries[0].EntryID != entry.ID {
		t.Fatalf("initial queue = %+v", initial)
	}

	readEntry := func(requestCtx *auth.Share) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/researches/"+research.Code+"/entries/"+entry.Code, nil)
		req.SetPathValue("id", research.Code)
		req.SetPathValue("entryId", entry.Code)
		if requestCtx == nil {
			req = req.WithContext(ctx)
		} else {
			req = req.WithContext(auth.WithShare(req.Context(), requestCtx))
		}
		rec := httptest.NewRecorder()
		entryHandler.GetByResearch(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("entry status = %d: %s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode entry: %v", err)
		}
		return out
	}

	payload := readEntry(nil)
	state, ok := payload["view_state"].(map[string]any)
	if !ok || state["kind"] != "new" || state["current_revision"] != float64(1) {
		t.Fatalf("entry view state = %#v", payload["view_state"])
	}
	shared := readEntry(&auth.Share{ID: "share", ResearchID: research.ID})
	if _, leaked := shared["view_state"]; leaked {
		t.Fatalf("share payload leaked personal state: %v", shared["view_state"])
	}

	body, _ := json.Marshal(map[string]int{"revision": 1})
	markReq := httptest.NewRequest(http.MethodPut, "/api/entries/"+entry.ID+"/seen", bytes.NewReader(body))
	markReq.SetPathValue("id", entry.ID)
	markReq = markReq.WithContext(ctx)
	markRec := httptest.NewRecorder()
	viewHandler.MarkSeen(markRec, markReq)
	if markRec.Code != http.StatusOK {
		t.Fatalf("mark status = %d: %s", markRec.Code, markRec.Body.String())
	}
	if got := decodeQueue(callList()); got.Count != 0 {
		t.Fatalf("seen entry remains queued: %+v", got)
	}

	content := "v2"
	if _, err := entrySvc.Update(ctx, entry.ID, service.UpdateEntryRequest{Content: &content}); err != nil {
		t.Fatalf("update entry: %v", err)
	}
	changed := decodeQueue(callList())
	if changed.Count != 1 || changed.Changed != 1 || changed.Entries[0].SeenRevision != 1 || changed.Entries[0].CurrentRevision != 2 {
		t.Fatalf("changed queue = %+v", changed)
	}

	// Reproduce a concurrent commit between the entry projection read and the
	// revision read: writeEntry receives the stale r2 object after r3 exists.
	// The response must be rebuilt wholly from r3, or the browser would mark r3
	// seen after rendering r2 content.
	stale, err := entrySvc.Get(ctx, entry.ID)
	if err != nil {
		t.Fatalf("read stale entry: %v", err)
	}
	content = "v3"
	if _, err := entrySvc.Update(ctx, entry.ID, service.UpdateEntryRequest{Content: &content}); err != nil {
		t.Fatalf("update entry to r3: %v", err)
	}
	snapshotReq := httptest.NewRequest(http.MethodGet, "/api/researches/"+research.Code+"/entries/"+entry.Code, nil).WithContext(ctx)
	snapshotRec := httptest.NewRecorder()
	entryHandler.writeEntry(snapshotRec, snapshotReq, stale)
	if snapshotRec.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d: %s", snapshotRec.Code, snapshotRec.Body.String())
	}
	var snapshot map[string]any
	if err := json.Unmarshal(snapshotRec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	snapshotEntry, _ := snapshot["data"].(map[string]any)
	snapshotState, _ := snapshot["view_state"].(map[string]any)
	if snapshot["revision"] != float64(3) || snapshotEntry["content"] != "v3" || snapshotState["current_revision"] != float64(3) {
		t.Fatalf("mixed entry/revision snapshot: %#v", snapshot)
	}
}
