package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dovod-app/app/internal/api/ws"
	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
	"github.com/dovod-app/app/internal/testdb"
)

// This file drives the real mux, because for the server-wide template library
// the routing *is* the decision. A service test can prove that a caller without
// the operator marker is refused; only the mux can prove that the api_token is
// what sets the marker, and that it gets past the user authentication which
// would otherwise 401 a credential that is not a JWT and not an API key.

type templateServer struct {
	mux   http.Handler
	token string // the instance api_token: the operator credential
	user  string // a JWT: a real, fully authenticated person
	team  string
}

func newTemplateServer(t *testing.T) *templateServer {
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

	access := service.NewAccess(teamRepo, true)
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
		researchRepo, access, events, log)
	shareSvc := service.NewShareService(storage.NewShareRepository(db), access, events, log)
	skillSvc := service.NewSkillService(storage.NewSkillRepository(db), researchRepo, teamRepo, access, events, log)
	templateSvc := service.NewTemplateService(storage.NewTemplateRepository(db), storage.NewSkillRepository(db),
		teamRepo, access, log)
	templateSvc.SetSkillService(skillSvc)

	authSvc := service.NewAuthService(storage.NewUserRepository(db), storage.NewAPIKeyRepository(db),
		storage.NewOAuthRepository(db), researchRepo, teamRepo,
		auth.NewJWTManager("test-secret", time.Hour), true, log)

	// Both credentials configured at once, which is the case that used to be
	// unreachable: with auth_enabled the api_token was checked by nothing.
	cfg := ServerConfig{Port: 0, AuthEnabled: true, APIToken: "operator-token"}
	annotationSvc := service.NewAnnotationService(storage.NewAnnotationRepository(db), entryRepo,
		storage.NewEntryRevisionRepository(db), access, entrySvc, entrySvc, events, log)

	srv := NewServer(cfg, researchSvc, sectionSvc, entrySvc, sessionSvc, taskSvc,
		roadmapSvc, exportSvc, obsidianSvc, teamSvc, shareSvc, skillSvc, templateSvc, annotationSvc, access, authSvc, db,
		entryRepo, researchRepo, crossrefRepo, externalLinkRepo, hub, log)

	ctx := context.Background()
	if _, err := skillSvc.LoadBuiltinSkills(ctx); err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if _, err := templateSvc.LoadBuiltinTemplates(ctx); err != nil {
		t.Fatalf("load templates: %v", err)
	}

	user, jwt, err := authSvc.Register(ctx, "operator-test@test.com", "hunter2hunter2", "Person")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	teams, err := teamRepo.ListByUser(ctx, user.ID)
	if err != nil || len(teams) == 0 {
		t.Fatalf("personal team: %v", err)
	}

	return &templateServer{mux: srv.mux, token: cfg.APIToken, user: jwt, team: teams[0].ID}
}

func (s *templateServer) do(t *testing.T, method, path, bearer string, body any) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func globalTemplateBody(name string) map[string]any {
	return map[string]any{
		"name":        name,
		"when_to_use": "Use when this instance says so.",
		"body":        "## Before you propose anything\n\nAsk the house questions.",
	}
}

func TestTemplateRoutes_OnlyTheInstanceTokenCreatesAGlobal(t *testing.T) {
	s := newTemplateServer(t)

	// No credential at all.
	if code, _ := s.do(t, "POST", "/api/templates", "", globalTemplateBody("Anonymous")); code != http.StatusUnauthorized {
		t.Errorf("no credential: want 401, got %d", code)
	}

	// A fully authenticated person. The refusal has to be 403 and not 401 —
	// they authenticated fine, they are just not the operator — and it has to
	// carry its own code, because `not_allowed` means "fork it instead" and
	// would send a client down the wrong path.
	code, body := s.do(t, "POST", "/api/templates", s.user, globalTemplateBody("A person's"))
	if code != http.StatusForbidden {
		t.Fatalf("authenticated user: want 403, got %d (%v)", code, body)
	}
	if body["code"] != "operator_required" {
		t.Errorf("code: want operator_required, got %v", body["code"])
	}

	// The operator. This is the assertion the mux exists for: requireAuth would
	// have rejected this token as an invalid session, and the route would have
	// been unreachable by the only credential that may use it.
	code, body = s.do(t, "POST", "/api/templates", s.token, globalTemplateBody("House methodology"))
	if code != http.StatusCreated {
		t.Fatalf("operator: want 201, got %d (%v)", code, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["tier"] != "global" {
		t.Errorf("tier: want global, got %v", data["tier"])
	}
	if data["source"] != "user" {
		t.Errorf("source: want user, got %v — a builtin row is refreshed from the binary and this one would be lost", data["source"])
	}

	// And every user can then see it, which is what "global" is for.
	code, body = s.do(t, "GET", "/api/templates", s.user, nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d", code)
	}
	list, _ := body["data"].([]any)
	var found bool
	for _, row := range list {
		if m, ok := row.(map[string]any); ok && m["slug"] == "house-methodology" {
			found = true
		}
	}
	if !found {
		t.Error("the operator's template was invisible to an ordinary user")
	}
}

// The same three routes serve both kinds of template, so the operator wrapper
// must not have shut ordinary callers out of their own libraries.
func TestTemplateRoutes_ATeamStillEditsItsOwnThroughTheSameRoutes(t *testing.T) {
	s := newTemplateServer(t)

	code, body := s.do(t, "POST", "/api/teams/"+s.team+"/templates", s.user, globalTemplateBody("Our playbook"))
	if code != http.StatusCreated {
		t.Fatalf("create team template: %d (%v)", code, body)
	}
	data, _ := body["data"].(map[string]any)
	id, _ := data["id"].(string)

	code, body = s.do(t, "PUT", "/api/templates/"+id, s.user, map[string]any{
		"name": "Our playbook", "when_to_use": "Use when.", "body": "## Rewritten\n\nBy us.",
	})
	if code != http.StatusOK {
		t.Fatalf("update own template: want 200, got %d (%v)", code, body)
	}
	if code, body := s.do(t, "DELETE", "/api/templates/"+id, s.user, nil); code != http.StatusNoContent {
		t.Fatalf("delete own template: want 204, got %d (%v)", code, body)
	}
}

// What ships in the binary is uneditable by everyone, operator included — the
// next boot would undo the edit, so permitting it would be a lie.
func TestTemplateRoutes_WhatShipsIsForkedNotEdited(t *testing.T) {
	s := newTemplateServer(t)

	code, body := s.do(t, "GET", "/api/templates/literature-review", s.user, nil)
	if code != http.StatusOK {
		t.Fatalf("get shipped template: %d", code)
	}
	data, _ := body["data"].(map[string]any)
	if data["source"] != "builtin" {
		t.Errorf("source: want builtin, got %v", data["source"])
	}
	id, _ := data["id"].(string)

	code, body = s.do(t, "PUT", "/api/templates/"+id, s.token, map[string]any{
		"name": "Literature review", "when_to_use": "Use when.", "body": "mine now",
	})
	if code != http.StatusForbidden {
		t.Fatalf("operator editing a shipped template: want 403, got %d (%v)", code, body)
	}
	if body["code"] != "not_allowed" {
		t.Errorf("code: want not_allowed (fork it instead), got %v", body["code"])
	}
}

// The read side of the same credential. Without it the operator could write a
// global and never read one back — and `PUT`/`DELETE` need an id.
func TestTemplateRoutes_TheOperatorCanReadBackWhatTheyWrote(t *testing.T) {
	s := newTemplateServer(t)

	code, body := s.do(t, "POST", "/api/templates", s.token, globalTemplateBody("House methodology"))
	if code != http.StatusCreated {
		t.Fatalf("create: %d (%v)", code, body)
	}
	data, _ := body["data"].(map[string]any)
	id, _ := data["id"].(string)

	// A team template belonging to somebody else, to check what the token does
	// *not* reach.
	code, body = s.do(t, "POST", "/api/teams/"+s.team+"/templates", s.user, globalTemplateBody("Their private playbook"))
	if code != http.StatusCreated {
		t.Fatalf("create team template: %d (%v)", code, body)
	}
	theirs, _ := body["data"].(map[string]any)
	theirID, _ := theirs["id"].(string)

	code, body = s.do(t, "GET", "/api/templates", s.token, nil)
	if code != http.StatusOK {
		t.Fatalf("operator list: want 200, got %d (%v)", code, body)
	}
	list, _ := body["data"].([]any)
	var sawMine, sawTheirs bool
	for _, row := range list {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		if m["id"] == id {
			sawMine = true
		}
		if m["id"] == theirID {
			sawTheirs = true
		}
	}
	if !sawMine {
		t.Error("the operator could not list the global they had just written")
	}
	if sawTheirs {
		t.Error("the api_token listed a team's own template — it proves who runs the server, not membership")
	}
	if code, _ := s.do(t, "GET", "/api/templates/"+theirID, s.token, nil); code != http.StatusNotFound {
		t.Errorf("operator reading a team template by id: want 404, got %d", code)
	}

	// Round trip: read the id back, edit it, delete it.
	if code, body := s.do(t, "PUT", "/api/templates/"+id, s.token, map[string]any{
		"body": "## Rewritten\n\nBy the operator.",
	}); code != http.StatusOK {
		t.Fatalf("operator update: want 200, got %d (%v)", code, body)
	}
	if code, body := s.do(t, "DELETE", "/api/templates/"+id, s.token, nil); code != http.StatusNoContent {
		t.Fatalf("operator delete: want 204, got %d (%v)", code, body)
	}
}

// `team_id` on the global route is a caller who meant the other one. Answering
// 201 with a template the whole server sees would be the wrong kind of helpful.
func TestTemplateRoutes_AGlobalCreateRefusesATeamID(t *testing.T) {
	s := newTemplateServer(t)

	body := globalTemplateBody("Meant for a team")
	body["team_id"] = s.team
	code, out := s.do(t, "POST", "/api/templates", s.token, body)
	if code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%v)", code, out)
	}
	if out["field"] != "team_id" {
		t.Errorf("field: want team_id, got %v", out["field"])
	}
}
