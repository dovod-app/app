package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovod-app/app/internal/api/ws"
	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
	"github.com/dovod-app/app/internal/testdb"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// These tests drive the real mux, because the routing *is* the security
// boundary here: a share token is checked once at a prefix, and everything
// inside that prefix is a route somebody chose to expose. A service-level test
// cannot tell you that `/api/shared/<token>/researches/{id}` exists and
// `/api/entries/{id}` does not.

type shareServer struct {
	t        *testing.T
	mux      http.Handler
	db       *bun.DB
	research *domain.Research
	entry    *domain.Entry
	other    *domain.Research
	shares   *service.ShareService
	ownerCtx context.Context
	// roadmapID is a roadmap whose nodes point at a task and at a session, which
	// is what the mindmap builds and what makes the include flags reachable
	// through the graph.
	roadmapID string
	sessionID string
	sectionID string
}

func newShareServer(t *testing.T) *shareServer {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, err := storage.NewDB(testdb.Config(t), log)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	teamRepo := storage.NewTeamRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	externalLinkRepo := storage.NewExternalLinkRepository(db)
	shareRepo := storage.NewShareRepository(db)

	access := service.NewAccess(teamRepo)
	hub := ws.NewHub(log)
	events := service.NoopNotifier{}

	entrySvc := service.NewEntryService(entryRepo, sectionRepo, researchRepo, access, sessionRepo,
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db),
		crossrefRepo, externalLinkRepo, events, log)
	researchSvc := service.NewResearchService(researchRepo, sectionRepo, teamRepo, access, events, log)
	sectionSvc := service.NewSectionService(sectionRepo, entryRepo, researchRepo, access, events, log)
	sessionSvc := service.NewSessionService(db, sessionRepo, storage.NewQuestionRepository(db),
		researchRepo, access, entrySvc, events, log)
	taskSvc := service.NewTaskService(storage.NewTaskRepository(db), researchRepo, access, entrySvc, events, log)
	roadmapSvc := service.NewRoadmapService(storage.NewRoadmapRepository(db), storage.NewRoadmapNodeRepository(db),
		storage.NewRoadmapEdgeRepository(db), researchRepo, access, events, log)
	exportSvc := service.NewExportService(researchSvc, sectionSvc, entrySvc, entryRepo, sessionSvc, taskSvc, roadmapSvc, log)
	obsidianSvc := service.NewObsidianService(researchSvc, sectionSvc, entryRepo, sessionSvc, taskSvc, roadmapSvc,
		storage.NewEntryRevisionRepository(db), log)
	teamSvc := service.NewTeamService(teamRepo, storage.NewTeamInviteRepository(db), storage.NewUserRepository(db),
		researchRepo, events, log)
	shareSvc := service.NewShareService(shareRepo, access, events, log)
	skillSvc := service.NewSkillService(storage.NewSkillRepository(db), researchRepo, teamRepo, access, events, log)
	templateSvc := service.NewTemplateService(storage.NewTemplateRepository(db), storage.NewSkillRepository(db), teamRepo, access, log)

	annotationSvc := service.NewAnnotationService(storage.NewAnnotationRepository(db), entryRepo,
		storage.NewEntryRevisionRepository(db), access, entrySvc, entrySvc, events, log)

	srv := NewServer(ServerConfig{Port: 0}, researchSvc, sectionSvc, entrySvc, sessionSvc, taskSvc,
		roadmapSvc, exportSvc, obsidianSvc, teamSvc, shareSvc, skillSvc, templateSvc, annotationSvc, access, nil, db,
		entryRepo, researchRepo, crossrefRepo, externalLinkRepo, hub, log)

	// Auth is off in this fixture, which is the harder case: with nobody in the
	// context every ordinary read is permitted, so a share that leaks is a share
	// that reached a route it should not have — the failure this file exists to
	// catch.
	ctx := context.Background()
	research, sections, err := researchSvc.Create(ctx, service.CreateResearchRequest{
		Name: "Shared research", Goal: "Show a client",
		Sections: []service.CreateSectionRequest{{Name: "s1", DisplayName: "Findings"}},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}
	if err := storage.NewResearchRepository(db).ImportProcess(ctx, research.ID, "internal working note", domain.Memory{{Text: "a memory the client must not read", Author: "unknown"}}, nil); err != nil {
		t.Fatalf("set instruction: %v", err)
	}
	entry, err := entrySvc.Create(ctx, service.CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Title: "A finding", Content: "the body", Tags: []string{"shared"},
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	task, err := taskSvc.Create(ctx, service.CreateTaskRequest{ResearchID: research.ID, Title: "internal todo"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// A second entry whose document PROJECTS that task. Without it every
	// "internal todo" assertion in this file passed over a plain markdown body
	// and proved nothing about the one block that renders a task title inside a
	// document — which is a second way out of the research, on routes the
	// `tasks` flag was written for.
	if _, err := entrySvc.Create(ctx, service.CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Type:  domain.EntryBlocks,
		Title: "What is left",
		Content: `{"version":1,"blocks":[
			{"type":"paragraph","data":{"text":"Still open."}},
			{"type":"task_ref","data":{"tasks":["` + task.Code + `"],"note":"before the call"}}
		]}`,
	}); err != nil {
		t.Fatalf("create task_ref entry: %v", err)
	}

	sess, _, err := sessionSvc.Create(ctx, service.CreateSessionRequest{
		ResearchID: research.ID, Title: "Initial exploration", Focus: "pricing",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := sessionSvc.Update(ctx, sess.ID, service.UpdateSessionRequest{
		Notes: strPtr("the session notes a client must not read"),
	}); err != nil {
		t.Fatalf("set session notes: %v", err)
	}

	// A document the session produced. Its session_id and the graph's
	// "produced" edge are the two ways a session's existence can reach a link
	// that excludes sessions.
	if _, err := entrySvc.Create(ctx, service.CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID, SessionID: sess.ID,
		Title: "Written during the interview", Content: "what was said",
	}); err != nil {
		t.Fatalf("create session entry: %v", err)
	}

	roadmap, err := roadmapSvc.Create(ctx, service.CreateRoadmapRequest{
		ResearchID: research.ID, Title: "Plan",
		Nodes: []service.CreateRoadmapNodeRequest{
			{TempID: "n1", Title: "Root"},
			{TempID: "n2", Title: "Chase it", RefType: "task", RefID: task.ID},
			{TempID: "n3", Title: "The interview", RefType: "session", RefID: sess.ID},
		},
	})
	if err != nil {
		t.Fatalf("create roadmap: %v", err)
	}

	other, otherSections, err := researchSvc.Create(ctx, service.CreateResearchRequest{
		Name: "Not shared", Goal: "Private",
		Sections: []service.CreateSectionRequest{{Name: "s1", DisplayName: "Private"}},
	})
	if err != nil {
		t.Fatalf("create other research: %v", err)
	}
	if _, err := entrySvc.Create(ctx, service.CreateEntryRequest{
		ResearchID: other.ID, SectionID: otherSections[0].ID,
		Title: "Private finding", Content: "secret", Tags: []string{"shared"},
	}); err != nil {
		t.Fatalf("create other entry: %v", err)
	}

	return &shareServer{
		t: t, mux: srv.mux, db: db, research: research, entry: entry,
		other: other, shares: shareSvc, ownerCtx: ctx, roadmapID: roadmap.ID,
		sessionID: sess.ID, sectionID: sections[0].ID,
	}
}

func strPtr(s string) *string { return &s }

func (s *shareServer) newShare(include domain.ShareInclude) string {
	s.t.Helper()
	result, err := s.shares.Create(s.ownerCtx, s.research.ID, service.CreateShareRequest{
		Label: "Client review", Include: include,
	})
	if err != nil {
		s.t.Fatalf("create share: %v", err)
	}
	return result.Token
}

func (s *shareServer) get(path string) (int, string) {
	s.t.Helper()
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

// getVault fetches a zip and unpacks it, so an assertion can be about a file
// rather than about a byte sequence that happens to survive Deflate.
func (s *shareServer) getVault(path string) (int, map[string]string) {
	s.t.Helper()
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		s.t.Fatalf("the response was not a zip: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			s.t.Fatalf("open %s: %v", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			s.t.Fatalf("read %s: %v", f.Name, err)
		}
		files[f.Name] = string(content)
	}
	return rec.Code, files
}

func (s *shareServer) do(method, path, body string) (int, string) {
	s.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// This server runs with no api_token and no accounts, where a write is
	// accepted from a client on the machine the server is on and refused from
	// anywhere else. httptest defaults RemoteAddr to 192.0.2.1 and Host to
	// example.com, and auth.LocalRequest reads both.
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "localhost:8088"
	s.mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func allIn() domain.ShareInclude {
	return domain.ShareInclude{Sessions: true, Tasks: true, Roadmaps: true, Export: true}
}

func TestShareRoutes_PayloadCarriesNoInternalFields(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(allIn())

	code, body := s.get("/api/shared/" + token)
	if code != http.StatusOK {
		t.Fatalf("payload: %d %s", code, body)
	}

	// Field by field, on the serialised bytes. Asserting on a struct would miss
	// exactly the failure that matters — a field that is populated on the way
	// out but was never named in the test.
	var payload struct {
		Data struct {
			Share struct {
				Label        string `json:"label"`
				OwnerName    string `json:"owner_name"`
				ResearchID   string `json:"research_id"`
				ResearchCode string `json:"research_code"`
				Include      domain.ShareInclude
			} `json:"share"`
			Research map[string]any   `json:"research"`
			Sections []map[string]any `json:"sections"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Data.Share.ResearchID != s.research.ID {
		t.Errorf("share names research %q, want %q", payload.Data.Share.ResearchID, s.research.ID)
	}
	if payload.Data.Share.ResearchCode == "" {
		t.Error("no research_code: every URL in the shared view is built from it")
	}

	research := payload.Data.Research
	if got, _ := research["instruction"].(string); got != "" {
		t.Errorf("instruction leaked: %q", got)
	}
	if mem, _ := research["memory"].([]any); len(mem) != 0 {
		t.Errorf("memory leaked: %v", mem)
	}
	for _, field := range []string{"team_id", "team_name", "user_id"} {
		if v, ok := research[field]; ok && v != "" && v != nil {
			t.Errorf("%s leaked to a share visitor: %v", field, v)
		}
	}
	if !strings.Contains(body, "Shared research") {
		t.Error("the payload does not contain the research it is supposed to share")
	}
	// Nothing anywhere in the bytes should name the private research.
	if strings.Contains(body, s.other.ID) || strings.Contains(body, "Not shared") {
		t.Error("the payload mentions a research outside the share")
	}
}

func TestShareRoutes_OwnerRoutesRejectAShareToken(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(allIn())

	// A share token is not a bearer credential. Presenting it where one belongs
	// must leave the caller exactly as unauthenticated as they were — never as
	// the owner.
	for _, path := range []string{
		"/api/researches/" + s.other.ID,
		"/api/researches/" + s.other.ID + "/entries",
		"/api/researches",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		s.mux.ServeHTTP(rec, req)
		// With auth disabled these routes are open to everyone, which is this
		// build's long-standing behaviour; what must not happen is the token
		// granting anything. The assertion that matters is on the prefix below.
		if rec.Code >= 500 {
			t.Errorf("%s with a share token: %d", path, rec.Code)
		}
	}

	// Writes, through the share prefix, in every shape the sub-mux could
	// plausibly be tricked into serving.
	writes := []struct{ method, path, body string }{
		{http.MethodPost, "/api/shared/" + token + "/entries", `{"research_id":"x"}`},
		{http.MethodPut, "/api/shared/" + token + "/researches/" + s.research.ID, `{"name":"hacked"}`},
		{http.MethodDelete, "/api/shared/" + token + "/entries/" + s.entry.ID, ``},
		{http.MethodPost, "/api/shared/" + token + "/researches/" + s.research.ID + "/shares", `{}`},
		// Widening the link through the link itself would hand a visitor the
		// owner's most sensitive control.
		{http.MethodPut, "/api/shared/" + token + "/shares/whatever", `{"include":{"sessions":true,"tasks":true,"roadmaps":true,"export":true}}`},
		{http.MethodPost, "/api/shared/" + token + "/researches/" + s.research.ID + "/crossrefs/rebuild", ``},
		// Importing a markdown file is the newest write and the only one
		// addressed by a bare section id, so the sub-mux has never seen the
		// shape before.
		{http.MethodPost, "/api/shared/" + token + "/sections/" + s.sectionID + "/import", `{"title":"x","body":"y"}`},
		{http.MethodPost, "/api/shared/" + token + "/sections/" + s.sectionID + "/import/preview", ``},
		// Marking a sentence is a write, and a visitor has no business making
		// one — but the assertion that matters is the read below.
		{http.MethodPost, "/api/shared/" + token + "/entries/" + s.entry.ID + "/annotations", `{"quote":{"exact":"x"},"kind":"verify"}`},
		{http.MethodPut, "/api/shared/" + token + "/annotations/whatever", `{"status":"closed"}`},
	}
	for _, w := range writes {
		code, body := s.do(w.method, w.path, w.body)
		if code < 400 {
			t.Errorf("%s %s succeeded through a share link: %d %s", w.method, w.path, code, body)
		}
	}

	// Annotations are working process, like provenance and revision history: who
	// doubted which sentence is not part of what a link shares. The sub-mux
	// simply does not carry these routes, and this is what proves it stays that
	// way when somebody adds one to the main mux.
	for _, path := range []string{
		"/api/shared/" + token + "/entries/" + s.entry.ID + "/annotations",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/annotations",
	} {
		code, body := s.get(path)
		if code < 400 {
			t.Errorf("%s is reachable through a share link: %d %s", path, code, body)
		}
	}

	// An annotation's resolution can name [[E19]], and that writes a crossref row
	// carrying the annotation's own id. The crossref routes are shared, so a link
	// exposing no annotation route at all was still answering "somebody disputes
	// this document, here is the id of the objection".
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO crossrefs (source_type, source_id, source_research_id, target_entry_id, target_ref, resolved)
		 VALUES ('annotation', 'a-leak', ?, ?, 'E1', 1)`, s.research.ID, s.entry.ID); err != nil {
		t.Fatalf("seed annotation crossref: %v", err)
	}
	for _, path := range []string{
		"/api/shared/" + token + "/researches/" + s.research.ID + "/crossrefs",
		"/api/shared/" + token + "/entries/" + s.entry.ID + "/crossrefs",
	} {
		code, body := s.get(path)
		if code < 400 && strings.Contains(body, "a-leak") {
			t.Errorf("%s exposed an annotation crossref: %s", path, body)
		}
	}

	// And the research really is untouched.
	fresh, err := s.db.QueryContext(context.Background(),
		`SELECT name FROM researches WHERE id = ?`, s.research.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	defer fresh.Close()
	for fresh.Next() {
		var name string
		if err := fresh.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name != "Shared research" {
			t.Errorf("the research was renamed through a share link: %q", name)
		}
	}
}

func TestShareRoutes_CannotReachOutsideTheShare(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(allIn())

	for _, path := range []string{
		"/api/shared/" + token + "/researches/" + s.other.ID,
		"/api/shared/" + token + "/researches/" + s.other.ID + "/entries",
		"/api/shared/" + token + "/researches/" + s.other.ID + "/tasks",
		"/api/shared/" + token + "/researches/" + s.other.ID + "/export",
		"/api/shared/" + token + "/researches/" + s.other.Code,
	} {
		code, body := s.get(path)
		if code != http.StatusNotFound {
			t.Errorf("%s: got %d (%s), want 404", path, code, body)
		}
		if strings.Contains(body, "Not shared") {
			t.Errorf("%s named the private research in its refusal", path)
		}
	}

	// Related-by-tags is the one read that does not go through a service, and
	// with no user in the context its team filter is disabled. Both entries
	// carry the tag "shared"; only one of them is inside the link.
	code, body := s.get("/api/shared/" + token + "/entries/" + s.entry.ID + "/related")
	if code != http.StatusOK {
		t.Fatalf("related: %d %s", code, body)
	}
	if strings.Contains(body, "Private finding") || strings.Contains(body, s.other.ID) {
		t.Errorf("related entries reached outside the share: %s", body)
	}
}

func TestShareRoutes_IncludeFlagsGateTheirRoutes(t *testing.T) {
	s := newShareServer(t)
	// Content only: no sessions, no tasks, no roadmaps, no export.
	token := s.newShare(domain.ShareInclude{})

	// What is always in.
	for _, path := range []string{
		"/api/shared/" + token,
		"/api/shared/" + token + "/researches/" + s.research.ID,
		"/api/shared/" + token + "/researches/" + s.research.ID + "/entries",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/entries/" + s.entry.ID,
	} {
		if code, body := s.get(path); code != http.StatusOK {
			t.Errorf("%s: got %d (%s), want 200", path, code, body)
		}
	}

	// The documents list must not name the session that produced one either;
	// with sessions included it may.
	if _, body := s.get("/api/shared/" + token + "/researches/" + s.research.ID + "/entries"); strings.Contains(body, s.sessionID) {
		t.Errorf("a content-only link's documents carry the session id: %s", body)
	}
	if _, body := s.get("/api/shared/" + s.newShare(allIn()) + "/researches/" + s.research.ID + "/entries"); !strings.Contains(body, s.sessionID) {
		t.Error("a link including sessions lost the document's session_id")
	}

	// What the flags hold back — and they answer 404, so a link without
	// sessions looks like a research that has none.
	for _, path := range []string{
		"/api/shared/" + token + "/researches/" + s.research.ID + "/sessions",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/tasks",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/roadmaps",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/export",
	} {
		if code, body := s.get(path); code != http.StatusNotFound {
			t.Errorf("%s: got %d (%s), want 404", path, code, body)
		}
	}
}

func TestShareRoutes_ExportHonoursTheFlagsAndRedacts(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{Export: true})

	code, body := s.get("/api/shared/" + token + "/researches/" + s.research.ID + "/export")
	if code != http.StatusOK {
		t.Fatalf("export: %d %s", code, body)
	}
	if strings.Contains(body, "internal working note") {
		t.Error("the export carried the research instruction")
	}
	if strings.Contains(body, "a memory the client must not read") {
		t.Error("the export carried the research memory")
	}
	if strings.Contains(body, "internal todo") {
		t.Error("the export carried tasks the link does not include")
	}
	// The assertion above only means something if the document that projects
	// that task is in this body at all. Its note is the proof it is.
	if !strings.Contains(body, "before the call") {
		t.Error("the entry holding the task_ref block is missing from the export, so the assertion above proved nothing")
	}
	// And with tasks switched off the block must degrade to its reference, not
	// claim the task was deleted.
	if strings.Contains(body, "every reference in this list has been removed") {
		t.Error("the export told a share visitor the task was deleted; it is simply not published")
	}

	// The portable dump is not mounted under the prefix at all: it is a
	// re-importable copy of the record, not a reading of it.
	if code, _ := s.get("/api/shared/" + token + "/researches/" + s.research.ID + "/export/portable"); code != http.StatusNotFound {
		t.Errorf("portable export through a share: got %d, want 404", code)
	}
}

// The vault is the fourth way to the same data, and the first three are gated by
// routes. It has to obey the flags on its own, because its parts are chosen by a
// query string that the visitor writes.
func TestShareRoutes_VaultObeysTheFlags(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{Export: true, Roadmaps: true})

	// Everything asked for, including the two the link does not carry.
	path := "/api/shared/" + token + "/researches/" + s.research.ID +
		"/export?format=obsidian&sessions=true&tasks=true&revisions=true"
	code, files := s.getVault(path)
	if code != http.StatusOK {
		t.Fatalf("vault through a share: %d", code)
	}

	all := ""
	for name, content := range files {
		all += name + "\n" + content + "\n"
	}

	for _, secret := range []string{
		"internal working note",                    // the research instruction
		"a memory the client must not read",        // the research memory
		"internal todo",                            // a task, not included
		"the session notes a client must not read", // a session, not included
		"Initial exploration",                      // ...and its title
	} {
		if strings.Contains(all, secret) {
			t.Errorf("the vault carried %q, which this link does not publish", secret)
		}
	}
	// Provenance and revision history are never published, by any flag.
	for name := range files {
		if strings.HasPrefix(name, "_history/") {
			t.Errorf("the vault carried a revision history: %s", name)
		}
		if strings.HasPrefix(name, "Sessions/") {
			t.Errorf("the vault carried a session folder: %s", name)
		}
	}
	if strings.Contains(all, "\nsession:") {
		t.Error("an entry's frontmatter named the session that produced it")
	}

	// What the link does publish is there, or the export is useless.
	if !strings.Contains(all, "A finding") {
		t.Error("the vault did not carry the entry")
	}
	if !strings.Contains(all, "Plan") {
		t.Error("the vault did not carry the roadmap the link includes")
	}
}

// Without the export flag the vault is unreachable for the same reason the
// markdown is: the route is gated, and the format is a query on that route.
func TestShareRoutes_VaultNeedsTheExportFlag(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{Roadmaps: true, Sessions: true, Tasks: true})

	path := "/api/shared/" + token + "/researches/" + s.research.ID + "/export?format=obsidian"
	if code, _ := s.get(path); code != http.StatusNotFound {
		t.Errorf("vault without the export flag: got %d, want 404", code)
	}
}

func TestShareRoutes_UnmountedRoutesAreNotReachable(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(allIn())

	// Everything a share was never given. Each of these exists on the
	// authenticated API and each would be a leak here — the search box in
	// particular runs unscoped when nobody is in the context.
	for _, path := range []string{
		"/api/shared/" + token + "/researches",
		"/api/shared/" + token + "/search?q=secret",
		"/api/shared/" + token + "/teams",
		"/api/shared/" + token + "/auth/me",
		// The graph is mounted now, and gated inside its handler rather than at
		// the route — TestShareRoutes_GraphObeysTheIncludeFlags is what holds
		// that boundary. The list of a research's links stays out: which links
		// exist is the owner's business, not a visitor's.
		"/api/shared/" + token + "/researches/" + s.research.ID + "/shares",
		"/api/shared/" + token + "/entries/" + s.entry.ID + "/revisions",
		// A session's change list is the same revision history in a different
		// shape — who edited what, when — so it belongs beside the revisions
		// above. It is listed separately because the component that reads it is
		// now mounted with the session page rather than behind a tab click, so
		// anything that ever mounted it on the share side would call this.
		"/api/shared/" + token + "/sessions/" + s.sessionID + "/changes",
		"/api/shared/" + token + "/sessions/" + s.sessionID + "/changes?summary=1",
		"/api/shared/" + token + "/health",
	} {
		if code, body := s.get(path); code != http.StatusNotFound {
			t.Errorf("%s is reachable through a share: %d %s", path, code, body)
		}
	}
}

func TestShareRoutes_DeadLinksLookAlike(t *testing.T) {
	s := newShareServer(t)

	live, err := s.shares.Create(s.ownerCtx, s.research.ID, service.CreateShareRequest{})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	if err := s.shares.Revoke(s.ownerCtx, live.Share.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	unknown := "mrs_" + strings.Repeat("a", 64)

	var bodies []string
	for _, token := range []string{live.Token, unknown, uuid.New().String()} {
		code, body := s.get("/api/shared/" + token)
		if code != http.StatusNotFound {
			t.Errorf("token %.12s…: got %d, want 404", token, code)
		}
		bodies = append(bodies, body)
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("a revoked link is distinguishable from an unknown one:\n%s\n%s", bodies[0], b)
		}
	}
}

func TestShareRoutes_PasswordGate(t *testing.T) {
	s := newShareServer(t)
	result, err := s.shares.Create(s.ownerCtx, s.research.ID, service.CreateShareRequest{
		Include: allIn(), Password: "open sesame",
	})
	if err != nil {
		t.Fatalf("create protected share: %v", err)
	}
	token := result.Token

	code, body := s.get("/api/shared/" + token)
	if code != http.StatusUnauthorized || !strings.Contains(body, "password_required") {
		t.Fatalf("locked payload: got %d %s, want 401 password_required", code, body)
	}
	// A locked link must not have answered with any of the research.
	if strings.Contains(body, "Shared research") {
		t.Error("the locked response carried the research")
	}

	code, body = s.do(http.MethodPost, "/api/shared/"+token+"/unlock", `{"password":"wrong"}`)
	if code != http.StatusUnauthorized || !strings.Contains(body, "invalid_password") {
		t.Fatalf("wrong password: got %d %s, want 401 invalid_password", code, body)
	}

	code, body = s.do(http.MethodPost, "/api/shared/"+token+"/unlock", `{"password":"open sesame"}`)
	if code != http.StatusOK {
		t.Fatalf("unlock: %d %s", code, body)
	}
	var unlocked struct {
		Data struct {
			Unlock string `json:"unlock"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &unlocked); err != nil || unlocked.Data.Unlock == "" {
		t.Fatalf("unlock returned nothing usable: %v %s", err, body)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/shared/"+token, nil)
	req.Header.Set("X-Share-Unlock", unlocked.Data.Unlock)
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unlocked read: %d %s", rec.Code, rec.Body.String())
	}
}

func TestShareRoutes_CreateReturnsTheTokenOnceAndListNever(t *testing.T) {
	s := newShareServer(t)

	code, body := s.do(http.MethodPost, "/api/researches/"+s.research.ID+"/shares",
		`{"label":"Client","include":{"export":true},"expires_in_days":7}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	var created struct {
		Data struct {
			Token string `json:"token"`
			URL   string `json:"url"`
			Share struct {
				ID          string `json:"id"`
				HasPassword bool   `json:"has_password"`
			} `json:"share"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(created.Data.Token, "mrs_") {
		t.Errorf("token %q does not carry the share prefix", created.Data.Token)
	}
	if !strings.Contains(created.Data.URL, "/s/"+created.Data.Token) {
		t.Errorf("url %q is not a link a person can open", created.Data.URL)
	}

	code, body = s.get("/api/researches/" + s.research.ID + "/shares")
	if code != http.StatusOK {
		t.Fatalf("list: %d %s", code, body)
	}
	if strings.Contains(body, created.Data.Token) {
		t.Fatal("the share list hands the token back; it may be shown exactly once")
	}
	if !strings.Contains(body, created.Data.Share.ID) {
		t.Error("the share is missing from the list")
	}

	// And revocation is reflected on the next read.
	code, body = s.do(http.MethodDelete, "/api/shares/"+created.Data.Share.ID, ``)
	if code != http.StatusOK {
		t.Fatalf("revoke: %d %s", code, body)
	}
	if code, _ := s.get("/api/shared/" + created.Data.Token); code != http.StatusNotFound {
		t.Errorf("a revoked link still opens: %d", code)
	}
}

func TestShareRoutes_RateLimitOnTheUnlockEndpoint(t *testing.T) {
	s := newShareServer(t)
	result, err := s.shares.Create(s.ownerCtx, s.research.ID, service.CreateShareRequest{Password: "open sesame"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	limited := false
	for i := 0; i < 40; i++ {
		code, _ := s.do(http.MethodPost, "/api/shared/"+result.Token+"/unlock", `{"password":"guess"}`)
		if code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("the password endpoint accepted forty guesses without a limit")
	}
}

// The findings below were all live leaks or miscounts caught in review. Each
// test names the fact that escaped, because the fix is one flag away from
// regressing and a test named after the route would not say what went wrong.

func TestShareRoutes_EntryCarriesNoProvenance(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{Sessions: false})

	code, body := s.get("/api/shared/" + token + "/researches/" + s.research.ID + "/entries/" + s.entry.ID)
	if code != http.StatusOK {
		t.Fatalf("entry: %d %s", code, body)
	}
	// `revision_session` names the interview session's code and title. A link
	// created with sessions switched off was printing it on every entry page.
	for _, field := range []string{"revision_session", "author_kind", "revised_at"} {
		if strings.Contains(body, field) {
			t.Errorf("a share visitor was told %q — that is the working process behind the entry: %s", field, body)
		}
	}
}

func TestShareRoutes_ExportHidesRoadmapCountWhenExcluded(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{Export: true})

	code, body := s.get("/api/shared/" + token + "/researches/" + s.research.ID + "/export")
	if code != http.StatusOK {
		t.Fatalf("export: %d %s", code, body)
	}
	if !strings.Contains(body, `"roadmap_count":0`) {
		t.Errorf("a link that excludes roadmaps said how many there are: %s", body)
	}
}

func TestShareRoutes_ViewsCountVisitsNotRefetches(t *testing.T) {
	s := newShareServer(t)
	result, err := s.shares.Create(s.ownerCtx, s.research.ID, service.CreateShareRequest{Include: allIn()})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	// One page load, then four refetches of the same route — which is what the
	// shared page does on every realtime event.
	s.get("/api/shared/" + result.Token + "?visit=1")
	for i := 0; i < 4; i++ {
		s.get("/api/shared/" + result.Token)
	}

	listed, err := s.shares.List(s.ownerCtx, s.research.ID)
	if err != nil || len(listed) == 0 {
		t.Fatalf("list shares: %v", err)
	}
	if listed[0].ViewCount != 1 {
		t.Errorf("view_count is %d after one visit and four refetches; the owner reads this number as people", listed[0].ViewCount)
	}
}

func TestShareRoutes_DotSegmentsDoNotEscapeThePrefix(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(allIn())

	// %2e%2e survives the outer mux, which cleans the escaped path, and used to
	// reach the sub-mux as a literal `..` — which answered with a redirect to
	// /api/researches/{id}, out of the public prefix entirely.
	for _, p := range []string{
		"/api/shared/" + token + "/researches/" + s.research.ID + "/x/%2e%2e",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/%2e%2e/%2e%2e/teams",
	} {
		code, body := s.get(p)
		if code != http.StatusNotFound {
			t.Errorf("%s: got %d (%s), want a uniform 404 — every path under the prefix is a mounted route or nothing", p, code, body)
		}
	}

	// An unescaped `..` is cleaned by the outer mux before this code is reached,
	// which is Go's behaviour for every route in the product. What matters is
	// that the redirect it produces stays inside the prefix.
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/shared/"+token+"/../researches", nil))
	if loc := rec.Header().Get("Location"); loc != "" && !strings.HasPrefix(loc, "/api/shared/") {
		t.Errorf("a dot segment redirected out of the share prefix: %s", loc)
	}
}

func TestShareRoutes_RoadmapRefsObeyTheIncludeFlags(t *testing.T) {
	s := newShareServer(t)
	// A roadmap node is a pointer, and resolving it inlines what it points at:
	// a task node carries the task's result, a question node its answer. Those
	// are the fields the tasks and sessions flags exist to withhold, and the
	// route gate cannot see them — it knows only that a roadmap was asked for.
	token := s.newShare(domain.ShareInclude{Roadmaps: true})

	code, body := s.get("/api/shared/" + token + "/researches/" + s.research.ID + "/roadmaps/" + s.roadmapID)
	if code != http.StatusOK {
		t.Fatalf("roadmap: %d %s", code, body)
	}
	if strings.Contains(body, "internal todo") {
		t.Errorf("a link that excludes tasks read one out of a roadmap node: %s", body)
	}
	if strings.Contains(body, "the session notes a client must not read") {
		t.Errorf("a link that excludes sessions read the interview out of a roadmap node: %s", body)
	}
	// The node itself still renders; only what it points at is withheld.
	if !strings.Contains(body, "Chase it") {
		t.Errorf("the roadmap lost its nodes along with their refs: %s", body)
	}
}

func TestShareRoutes_ActiveSessionObeysTheIncludeFlag(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{})

	// The research route is ungated — it is the research itself, which every
	// link carries — so the optional part of its payload has to gate itself.
	code, body := s.get("/api/shared/" + token + "/researches/" + s.research.ID)
	if code != http.StatusOK {
		t.Fatalf("research: %d %s", code, body)
	}
	if strings.Contains(body, "the session notes a client must not read") {
		t.Errorf("a link that excludes sessions carried the latest session's notes: %s", body)
	}
	if !strings.Contains(body, `"active_session":null`) {
		t.Errorf("active_session should be absent for a link without sessions: %s", body)
	}
}

func TestShareRoutes_RefusalsDoNotSayWhetherAResearchExists(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(allIn())

	// Two different refusal strings turned the prefix into an oracle: a stranger
	// walking R1…Rn learned which ids were real from the wording alone.
	var bodies []string
	for _, p := range []string{
		"/api/shared/" + token + "/researches/" + s.other.ID + "/entries/" + s.entry.ID,
		"/api/shared/" + token + "/researches/R9999/entries/" + s.entry.ID,
		"/api/shared/" + token + "/researches/" + s.other.Code + "/entries/" + s.entry.ID,
	} {
		code, body := s.get(p)
		if code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", p, code)
		}
		bodies = append(bodies, body)
	}
	for i, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("refusals differ, so the prefix says which researches exist:\n%q\n%q", bodies[0], bodies[i+1])
		}
	}
}

// A skill is a team's methodology — the same class of working process as the
// instruction it replaces, which redactForShare has always stripped. A share
// link must not reach one, and the defence is two-layered on purpose: the
// routes are not mounted on the shared sub-mux, and SkillService.Load refuses a
// share context anyway, so a route added later that forgot still fails closed.
func TestShareRoutes_SkillsAreNotReachableThroughAShare(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{Sessions: true, Tasks: true, Roadmaps: true, Export: true})

	for _, path := range []string{
		"/api/shared/" + token + "/researches/" + s.research.ID + "/skills",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/skills/library",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/skills/managing-a-research",
		"/api/shared/" + token + "/skills/managing-a-research",
	} {
		if code, body := s.get(path); code != http.StatusNotFound {
			t.Errorf("%s: got %d (%s), want 404 — no skills route belongs on the share prefix", path, code, body)
		}
	}
}

// The owner-side routes must refuse a share token too. Mounting nothing on the
// public prefix is only half the rule; the other half is that the token cannot
// be used against the authenticated API.
func TestShareRoutes_SkillServiceRefusesAShareContext(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{})

	skills := service.NewSkillService(storage.NewSkillRepository(s.db),
		storage.NewResearchRepository(s.db), storage.NewTeamRepository(s.db),
		service.NewAccess(storage.NewTeamRepository(s.db)), service.NoopNotifier{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := skills.LoadBuiltinSkills(context.Background()); err != nil {
		t.Fatalf("load builtins: %v", err)
	}
	if _, err := skills.Attach(s.ownerCtx, s.research.ID, "managing-a-research", false); err != nil {
		t.Fatalf("attach: %v", err)
	}

	share, err := s.shares.Resolve(context.Background(), token, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	visitor := auth.WithShare(context.Background(), service.Capability(share))

	if _, err := skills.Load(visitor, s.research.ID, "managing-a-research"); err == nil {
		t.Error("a share visitor loaded a skill body")
	}
	if index := skills.Index(visitor, s.research.ID); len(index) != 0 {
		t.Errorf("a share visitor was handed a skills index of %d", len(index))
	}
}

// Which methodology a team follows is working process, the same class as the
// instruction beside it. The stamp rides on the research payload rather than on
// a template route, which is why the two service-level refusals and the absent
// sub-mux routes did not catch it: nothing template-shaped was ever asked for.
func TestShareRoutes_TheTemplateStampIsNotOnAShare(t *testing.T) {
	s := newShareServer(t)
	if _, err := s.db.Exec(
		`UPDATE researches SET template_slug='acme-q4-layoff-diligence', template_version=7 WHERE id=?`,
		s.research.ID); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	token := s.newShare(domain.ShareInclude{Export: true})

	for _, path := range []string{
		"/api/shared/" + token + "/",
		"/api/shared/" + token + "/researches/" + s.research.ID,
		"/api/shared/" + token + "/researches/" + s.research.ID + "/export?format=json",
	} {
		code, body := s.get(path)
		if code != http.StatusOK {
			continue
		}
		// The slug is Slugify of a name the team chose, so it reads back as
		// that name — which is the disclosure, not the number beside it.
		if strings.Contains(body, "acme-q4-layoff-diligence") {
			t.Errorf("%s leaked the template a team follows", path)
		}
		if strings.Contains(body, `"template_version":7`) {
			t.Errorf("%s leaked the template version", path)
		}
	}
}

// And no template route exists on the public prefix at all.
func TestShareRoutes_TemplatesAreNotReachableThroughAShare(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{Sessions: true, Tasks: true, Roadmaps: true, Export: true})

	for _, path := range []string{
		"/api/shared/" + token + "/templates",
		"/api/shared/" + token + "/templates/literature-review",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/templates/draft",
	} {
		if code, body := s.get(path); code != http.StatusNotFound {
			t.Errorf("%s: got %d (%s), want 404", path, code, body)
		}
	}
}

// TestShareRoutes_ExportsCarryNoDocumentMetadata drives the real mux, because
// this is where the leak actually was.
//
// The service-level test asserts on EntryService.Get and its list methods, and
// every one of those redacts — so it could not fail on the bug it was written
// for. The exports read the repository directly, which has never known what a
// share is, and the JSON export serialized the whole entry.
func TestShareRoutes_ExportsCarryNoDocumentMetadata(t *testing.T) {
	s := newShareServer(t)

	// Declare a field on the section and record a value under it, through the
	// same routes an owner would use.
	code, body := s.get("/api/researches/" + s.research.ID)
	if code != http.StatusOK {
		t.Fatalf("read research: %d", code)
	}
	var payload struct {
		Data struct {
			Sections []struct {
				Section struct {
					ID string `json:"id"`
				} `json:"-"`
				ID string `json:"id"`
			} `json:"sections"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode research: %v", err)
	}
	if len(payload.Data.Sections) == 0 {
		t.Fatal("no sections in the research payload")
	}
	sectionID := payload.Data.Sections[0].ID

	put := func(path string, in any) int {
		raw, _ := json.Marshal(in)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr, req.Host = "127.0.0.1:54321", "localhost:8088" // see shareServer.do
		s.mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := put("/api/sections/"+sectionID, map[string]any{
		"field_spec": []map[string]any{{"key": "owner", "label": "Acquisition owner", "type": "text"}},
	}); c != http.StatusOK {
		t.Fatalf("declare field: %d", c)
	}
	if c := put("/api/entries/"+s.entry.ID, map[string]any{
		"metadata": map[string]any{"owner": "SECRETOWNERVALUE"},
	}); c != http.StatusOK {
		t.Fatalf("set metadata: %d", c)
	}

	token := s.newShare(domain.ShareInclude{Sessions: true, Tasks: true, Roadmaps: true, Export: true})

	// Three formats, one rule. The value, the label a team chose, and the
	// version number that would confirm a declaration exists at all.
	forbidden := []string{"SECRETOWNERVALUE", "Acquisition owner"}
	for _, path := range []string{
		"/api/shared/" + token + "/researches/" + s.research.ID + "/export",
		"/api/shared/" + token + "/researches/" + s.research.ID + "/export?format=md",
	} {
		status, out := s.get(path)
		if status != http.StatusOK {
			t.Fatalf("%s: %d", path, status)
		}
		for _, needle := range forbidden {
			if strings.Contains(out, needle) {
				t.Errorf("%s published %q", path, needle)
			}
		}
		if strings.Contains(out, `"spec_version":1`) {
			t.Errorf("%s published a spec version, which confirms a declaration exists", path)
		}
	}

	status, files := s.getVault("/api/shared/" + token + "/researches/" + s.research.ID + "/export?format=obsidian")
	if status != http.StatusOK {
		t.Fatalf("vault: %d", status)
	}
	for name, content := range files {
		for _, needle := range forbidden {
			if strings.Contains(content, needle) {
				t.Errorf("vault file %s published %q", name, needle)
			}
		}
	}
}

// TestShareRoutes_EntryMarkdownIsNotReachable asserts at the boundary that
// actually holds it.
//
// The service cannot refuse this one: a share resolves to viewer on its
// research, so `EntryService.Get` succeeds and the file would be rendered. What
// keeps a document from leaving as a file is that the route is not on this
// mux — which makes the routing the whole guarantee, and therefore the thing
// worth a test.
func TestShareRoutes_EntryMarkdownIsNotReachable(t *testing.T) {
	s := newShareServer(t)
	token := s.newShare(domain.ShareInclude{Sessions: true, Tasks: true, Roadmaps: true, Export: true})

	code, _ := s.get("/api/shared/" + token + "/entries/" + s.entry.ID + "/markdown")
	if code != http.StatusNotFound {
		t.Fatalf("a share reached the per-document download: %d", code)
	}
}

// The graph is the first route under the prefix that assembles every entity
// into one payload, so it is the first place a link's include flags can be
// bypassed by accident: the session and task services do not refuse a share
// visitor on their own, because Access resolves a share to a viewer on this
// research. What proves the flags hold is the body, not the status.
func TestShareRoutes_GraphObeysTheIncludeFlags(t *testing.T) {
	s := newShareServer(t)

	type graphBody struct {
		Nodes              []map[string]any `json:"nodes"`
		Edges              []map[string]any `json:"edges"`
		AvailableNodeTypes []string         `json:"available_node_types"`
	}
	fetch := func(token string) graphBody {
		t.Helper()
		code, body := s.get("/api/shared/" + token + "/researches/" + s.research.ID + "/graph")
		if code != http.StatusOK {
			t.Fatalf("graph: %d %s", code, body)
		}
		var g graphBody
		if err := json.Unmarshal([]byte(body), &g); err != nil {
			t.Fatalf("graph body: %v", err)
		}
		return g
	}
	types := func(g graphBody) map[string]int {
		counts := map[string]int{}
		for _, n := range g.Nodes {
			counts[n["type"].(string)]++
		}
		return counts
	}

	// Content only. Sessions, questions and tasks must be absent — not present
	// with a flag, not empty-labelled, absent — and the list of what this
	// caller could have received must not name them either.
	contentOnly := fetch(s.newShare(domain.ShareInclude{}))
	got := types(contentOnly)
	if got["entry"] < 2 || got["section"] < 1 {
		t.Errorf("documents and sections are always in a link, got %v", got)
	}
	for _, withheld := range []string{"session", "question", "task"} {
		if got[withheld] != 0 {
			t.Errorf("a content-only link drew %d %s nodes", got[withheld], withheld)
		}
	}
	raw, _ := json.Marshal(contentOnly)
	// The session's id is as good as its title: an edge "SS1 produced E2"
	// says a session exists and what it did.
	for _, leak := range []string{"Initial exploration", "internal todo", s.sessionID} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the graph of a content-only link carries %q", leak)
		}
	}
	for _, e := range contentOnly.Edges {
		if e["type"] == "session" {
			t.Errorf("a content-only link drew a session edge: %v", e)
		}
	}
	if strings.Join(contentOnly.AvailableNodeTypes, ",") != "section,entry" {
		t.Errorf("available_node_types = %v, want [section entry]", contentOnly.AvailableNodeTypes)
	}

	// Everything in: the same research draws its session and its task, and
	// says so up front.
	everything := fetch(s.newShare(allIn()))
	got = types(everything)
	if got["session"] != 1 || got["task"] != 1 {
		t.Errorf("a link with sessions and tasks drew %v", got)
	}
	if strings.Join(everything.AvailableNodeTypes, ",") != "section,entry,session,question,task" {
		t.Errorf("available_node_types = %v", everything.AvailableNodeTypes)
	}

	// One flag at a time, so a mix-up between the two cannot pass.
	sessionsOnly := types(fetch(s.newShare(domain.ShareInclude{Sessions: true})))
	if sessionsOnly["session"] != 1 || sessionsOnly["task"] != 0 {
		t.Errorf("sessions-only link drew %v", sessionsOnly)
	}
	tasksOnly := types(fetch(s.newShare(domain.ShareInclude{Tasks: true})))
	if tasksOnly["task"] != 1 || tasksOnly["session"] != 0 {
		t.Errorf("tasks-only link drew %v", tasksOnly)
	}

	// And the graph of the research the link does not reach is not reachable.
	if code, _ := s.get("/api/shared/" + s.newShare(allIn()) + "/researches/" + s.other.ID + "/graph"); code != http.StatusNotFound {
		t.Errorf("the graph of another research answered %d", code)
	}
}

// Changing what a link shows is an owner's action on the owner's surface. The
// address stays the same — which is the point, and also why the prefix must
// not carry the route: a visitor who could widen their own link would own the
// research.
func TestShareRoutes_UpdateChangesALiveLinkInPlace(t *testing.T) {
	s := newShareServer(t)
	issued, err := s.shares.Create(s.ownerCtx, s.research.ID, service.CreateShareRequest{
		Label: "Client review", Include: domain.ShareInclude{Export: true},
	})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	token, id := issued.Token, issued.Share.ID
	graph := "/api/shared/" + token + "/researches/" + s.research.ID + "/graph"
	sessions := "/api/shared/" + token + "/researches/" + s.research.ID + "/sessions"

	if code, _ := s.get(sessions); code != http.StatusNotFound {
		t.Fatalf("sessions before widening: %d", code)
	}

	// Widen. The response is the whole share, so a row can repaint from it.
	code, body := s.do(http.MethodPut, "/api/shares/"+id,
		`{"label":"Website demo","include":{"sessions":true,"tasks":true,"roadmaps":true,"export":true}}`)
	if code != http.StatusOK {
		t.Fatalf("update: %d %s", code, body)
	}
	var updated struct {
		Data struct {
			Share domain.Share `json:"share"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &updated); err != nil {
		t.Fatalf("update body: %v", err)
	}
	if updated.Data.Share.ID != id || updated.Data.Share.Label != "Website demo" || !updated.Data.Share.Include.Sessions {
		t.Errorf("update returned %+v", updated.Data.Share)
	}
	if strings.Contains(body, token) {
		t.Error("the update response carried the token")
	}

	// The same address now reaches what it did not a moment ago.
	if code, _ := s.get(sessions); code != http.StatusOK {
		t.Errorf("sessions after widening: %d", code)
	}
	if _, body := s.get(graph); !strings.Contains(body, "Initial exploration") {
		t.Error("the graph did not pick up the widened flags")
	}

	// Narrow again. `include` is a complete replacement, so an object naming
	// only export takes the other three away.
	if code, body := s.do(http.MethodPut, "/api/shares/"+id, `{"include":{"export":true}}`); code != http.StatusOK {
		t.Fatalf("narrow: %d %s", code, body)
	}
	if code, _ := s.get(sessions); code != http.StatusNotFound {
		t.Errorf("sessions after narrowing: %d", code)
	}
	if _, body := s.get(graph); strings.Contains(body, "Initial exploration") {
		t.Error("the graph kept the session after narrowing")
	}

	// An empty body changes nothing and says so.
	if code, _ := s.do(http.MethodPut, "/api/shares/"+id, `{}`); code != http.StatusBadRequest {
		t.Errorf("empty update: %d, want 400", code)
	}

	// Through the prefix, in the two shapes the sub-mux could be handed.
	for _, path := range []string{
		"/api/shared/" + token + "/shares/" + id,
		"/api/shared/" + token + "/researches/" + s.research.ID + "/shares/" + id,
	} {
		if code, _ := s.do(http.MethodPut, path, `{"include":{"sessions":true,"tasks":true,"roadmaps":true,"export":true}}`); code < 400 {
			t.Errorf("%s widened a link through the link itself: %d", path, code)
		}
	}

	// A revoked link cannot be edited back to life, and — on this surface — the
	// refusal says why.
	if err := s.shares.Revoke(s.ownerCtx, id); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if code, body := s.do(http.MethodPut, "/api/shares/"+id, `{"label":"Back from the dead"}`); code != http.StatusConflict {
		t.Errorf("update of a revoked link: %d %s, want 409", code, body)
	}
	if code, _ := s.get("/api/shared/" + token); code != http.StatusNotFound {
		t.Errorf("the revoked link still answers: %d", code)
	}

	// An unknown id is a plain 404: there is nothing to confirm about a row
	// that does not exist.
	if code, _ := s.do(http.MethodPut, "/api/shares/"+uuid.New().String(), `{"label":"x"}`); code != http.StatusNotFound {
		t.Errorf("update of an unknown share: %d, want 404", code)
	}
}
