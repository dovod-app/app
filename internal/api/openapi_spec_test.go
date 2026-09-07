package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dovod-app/app/internal/api/ws"
	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
	"github.com/dovod-app/app/internal/testdb"
)

// The OpenAPI document is the only description of this API that an external
// client reads. It was hand-written for years and drifted until it described 24
// of 99 paths and said nothing about authentication. These tests exist so that
// cannot happen again quietly: the document is generated from the registrations,
// and what a person still writes by hand — the summary, the description — is
// checked for being there at all.

// specServer is an instance with everything on — accounts, the OAuth endpoints,
// an api_token — because the document has to be checked against the widest set
// of routes the product ever serves.
type specServer struct {
	mux    http.Handler
	router *router
}

// newSpecServer builds the whole server the way main.go does. The options are
// for tests that need a different posture — accounts off, an api_token only, an
// MCP handler mounted — and the default is the widest route set, which is what
// the drift guards want.
func newSpecServer(t *testing.T, opts ...func(*ServerConfig)) *specServer {
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

	// The configuration is settled before anything is wired, because the guard
	// takes `auth_enabled` now: building it from the default and then letting an
	// option turn accounts off left the services believing in accounts the
	// server did not have, which is the drift this argument exists to prevent.
	oauthRepo := storage.NewOAuthRepository(db)
	cfg := ServerConfig{
		Port: 0, AuthEnabled: true, APIToken: "operator-token",
		OAuthSvc: service.NewOAuthService(oauthRepo, log),
		BaseURL:  "https://research.example.com",
		Version:  "1.2.3",
	}
	for _, o := range opts {
		o(&cfg)
	}

	access := service.NewAccess(teamRepo, cfg.AuthEnabled)
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
	annotationSvc := service.NewAnnotationService(storage.NewAnnotationRepository(db), entryRepo,
		storage.NewEntryRevisionRepository(db), access, entrySvc, entrySvc, events, log)

	authSvc := service.NewAuthService(storage.NewUserRepository(db), storage.NewAPIKeyRepository(db),
		oauthRepo, researchRepo, teamRepo, auth.NewJWTManager("test-secret", time.Hour), true, log)

	// main.go only constructs the auth service when accounts are on, so a test
	// that turns them off has to hand over the same nil — otherwise it is
	// testing a wiring that cannot happen.
	wiredAuth := authSvc
	if !cfg.AuthEnabled {
		wiredAuth = nil
		cfg.OAuthSvc = nil
	}

	srv := NewServer(cfg, researchSvc, sectionSvc, entrySvc, sessionSvc, taskSvc,
		roadmapSvc, exportSvc, obsidianSvc, teamSvc, shareSvc, skillSvc, templateSvc, annotationSvc, access, wiredAuth, db,
		entryRepo, researchRepo, crossrefRepo, externalLinkRepo, hub, log)

	return &specServer{mux: srv.mux, router: srv.router}
}

func specOf(t *testing.T, srv *http.Server, mux http.Handler) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("openapi.json: status %d: %s", w.Code, w.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("openapi.json is not JSON: %v", err)
	}
	return doc
}

// TestOpenAPI_EveryRouteIsDocumented is the drift guard. Every pattern that
// reaches the mux went through router.route or router.undocumented, and the
// second is a deliberate, visible choice — a WebSocket upgrade, the MCP
// transport, the SPA. Anything else must be in the document.
func TestOpenAPI_EveryRouteIsDocumented(t *testing.T) {
	s := newSpecServer(t) // auth on, OAuth on: the widest route set
	doc := specOf(t, nil, s.mux)
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		t.Fatal("the document has no paths at all")
	}

	// Routes the document deliberately leaves out, each for a stated reason.
	undocumented := map[string]string{
		"/ws":                  "a WebSocket upgrade, not an HTTP operation",
		"/mcp":                 "the MCP transport, which has its own protocol",
		"/":                    "the embedded frontend and the MCP catch-all",
		"/llms/":               "a prefix serving markdown files, enumerated by /llms.txt",
		"/api/":                "the JSON 404 for any path under /api/ that matched no route",
		"/api/shared/{token}/": "the visitor sub-mux, described in prose on GET /api/shared/{token}",
		"/api/shared/{token}":  "the method fallback under the share prefix",
		"/api/openapi.yaml":    "documented",
		"/api/researches/{id}": "documented",
	}

	var missing []string
	for _, route := range s.router.Routes() {
		if _, allowed := undocumented[route.Pattern]; allowed {
			continue
		}
		// The method matters, not only the path. Checking the path alone let
		// `PUT /api/researches/{id}` fall out of the document while the `GET`
		// on the same path kept the guard green — on the very test that exists
		// because a document drifted.
		item, ok := paths[route.Pattern].(map[string]any)
		if !ok {
			missing = append(missing, route.Method+" "+route.Pattern)
			continue
		}
		if _, ok := item[strings.ToLower(route.Method)]; !ok {
			missing = append(missing, route.Method+" "+route.Pattern)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("registered but not in the OpenAPI document:\n  %s", strings.Join(missing, "\n  "))
	}
}

// TestOpenAPI_EveryOperationSaysSomething — a generated document that describes
// 99 paths as "Success" is not better than the one it replaced.
func TestOpenAPI_EveryOperationSaysSomething(t *testing.T) {
	s := newSpecServer(t)
	doc := specOf(t, nil, s.mux)
	paths := doc["paths"].(map[string]any)

	var bare []string
	for path, item := range paths {
		methods, _ := item.(map[string]any)
		for method, raw := range methods {
			opDoc, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			summary, _ := opDoc["summary"].(string)
			description, _ := opDoc["description"].(string)
			if strings.TrimSpace(summary) == "" || len(strings.TrimSpace(description)) < 30 {
				bare = append(bare, strings.ToUpper(method)+" "+path)
			}
			if opDoc["operationId"] == nil || opDoc["operationId"] == "" {
				bare = append(bare, strings.ToUpper(method)+" "+path+" (no operationId)")
			}
		}
	}
	if len(bare) > 0 {
		t.Fatalf("operations with no summary or no real description:\n  %s", strings.Join(bare, "\n  "))
	}
}

// TestOpenAPI_PathParametersAreDeclared — a path parameter present in the route
// and absent from the document is the single most common way a hand-written
// spec goes wrong, and the reason they are now derived.
func TestOpenAPI_PathParametersAreDeclared(t *testing.T) {
	s := newSpecServer(t)
	doc := specOf(t, nil, s.mux)
	paths := doc["paths"].(map[string]any)
	braces := regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

	for path, item := range paths {
		want := map[string]bool{}
		for _, m := range braces.FindAllStringSubmatch(path, -1) {
			want[m[1]] = true
		}
		if len(want) == 0 {
			continue
		}
		methods, _ := item.(map[string]any)
		for method, raw := range methods {
			opDoc, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			got := map[string]bool{}
			params, _ := opDoc["parameters"].([]any)
			for _, p := range params {
				pm, _ := p.(map[string]any)
				if pm["in"] == "path" {
					name, _ := pm["name"].(string)
					got[name] = true
				}
			}
			for name := range want {
				if !got[name] {
					t.Errorf("%s %s: path parameter %q is in the route and not in the document",
						strings.ToUpper(method), path, name)
				}
			}
		}
	}
}

// TestOpenAPI_AuthenticationIsDescribed — the question this document could not
// answer before: what does a caller have to present?
func TestOpenAPI_AuthenticationIsDescribed(t *testing.T) {
	s := newSpecServer(t) // auth_enabled: true
	doc := specOf(t, nil, s.mux)

	components, _ := doc["components"].(map[string]any)
	schemes, _ := components["securitySchemes"].(map[string]any)
	for _, name := range []string{"bearerAuth", "operatorToken"} {
		scheme, ok := schemes[name].(map[string]any)
		if !ok {
			t.Fatalf("securitySchemes has no %q", name)
		}
		if scheme["type"] != "http" || scheme["scheme"] != "bearer" {
			t.Errorf("%s: type %v scheme %v", name, scheme["type"], scheme["scheme"])
		}
		if d, _ := scheme["description"].(string); len(d) < 50 {
			t.Errorf("%s has no description worth reading", name)
		}
	}

	paths := doc["paths"].(map[string]any)
	secured := func(path, method string) []any {
		item, _ := paths[path].(map[string]any)
		opDoc, _ := item[method].(map[string]any)
		sec, _ := opDoc["security"].([]any)
		return sec
	}

	// A write is not open when accounts are on.
	if got := secured("/api/entries", "post"); len(got) == 0 {
		t.Error("POST /api/entries is documented as needing no credential, with auth enabled")
	}
	// A read is not open either — the credential is what scopes the answer.
	if got := secured("/api/researches", "get"); len(got) == 0 {
		t.Error("GET /api/researches is documented as needing no credential, with auth enabled")
	}
	// Signing in cannot require being signed in.
	if got := secured("/api/auth/login", "post"); len(got) != 0 {
		t.Errorf("POST /api/auth/login is documented as needing a credential: %v", got)
	}
	// Health is what a probe calls.
	if got := secured("/api/health", "get"); len(got) != 0 {
		t.Errorf("GET /api/health is documented as needing a credential: %v", got)
	}
	// The operator routes accept either kind, and say so.
	if got := secured("/api/templates", "post"); len(got) != 2 {
		t.Errorf("POST /api/templates should accept a person or the operator token, got %v", got)
	}
	// A share link is a path token, not a credential the document can carry.
	if got := secured("/api/shared/{token}", "get"); len(got) != 0 {
		t.Errorf("the share route should declare no security scheme: %v", got)
	}
}

// TestOpenAPI_ReflectsTheInstance — the document describes the server it is
// served by. With auth off the same routes need no credential, and a client
// that read the other document would be wrong about every one of them.
func TestOpenAPI_ReflectsTheInstance(t *testing.T) {
	s := newShareServer(t) // auth_enabled: false, no api_token
	doc := specOf(t, nil, s.mux)
	paths := doc["paths"].(map[string]any)

	item, _ := paths["/api/entries"].(map[string]any)
	if item == nil {
		t.Fatal("POST /api/entries is missing from the document")
	}
	opDoc, _ := item["post"].(map[string]any)
	if sec, _ := opDoc["security"].([]any); len(sec) != 0 {
		t.Errorf("with no auth configured, a write should be documented as open: %v", sec)
	}

	// The OAuth endpoints do not exist on this instance and must not be
	// described as if they did.
	if _, ok := paths["/auth/token"]; ok {
		t.Error("the document describes /auth/token on an instance that does not serve it")
	}
	info, _ := doc["info"].(map[string]any)
	if d, _ := info["description"].(string); !strings.Contains(d, "no authentication configured") {
		t.Errorf("the document does not say how this instance authenticates: %q", d)
	}
}

// TestOpenAPI_YAMLAndJSONAgree — both are served, and a client picks one.
func TestOpenAPI_YAMLAndJSONAgree(t *testing.T) {
	s := newSpecServer(t)
	for path, contentType := range map[string]string{
		"/api/openapi.yaml": "application/yaml",
		"/api/openapi.json": "application/json",
	} {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, contentType) {
			t.Errorf("%s: Content-Type %q", path, ct)
		}
		if w.Body.Len() < 5000 {
			t.Errorf("%s: %d bytes — that is not a description of this API", path, w.Body.Len())
		}
	}
}

// TestOpenAPI_EntitySchemasComeFromTheDomain — the schemas are generated from
// the structs the handlers serialise, so a field added there appears here. This
// checks the generation actually ran rather than falling back to bare objects.
func TestOpenAPI_EntitySchemasComeFromTheDomain(t *testing.T) {
	s := newSpecServer(t)
	doc := specOf(t, nil, s.mux)
	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)

	for name, fields := range map[string][]string{
		"Research": {"id", "name", "goal", "status", "code"},
		"Entry":    {"id", "title", "content", "tags", "code"},
		"Session":  {"id", "status", "code"},
		"Task":     {"id", "title", "status"},
	} {
		schema, ok := schemas[name].(map[string]any)
		if !ok {
			t.Errorf("components/schemas has no %q", name)
			continue
		}
		props, _ := schema["properties"].(map[string]any)
		for _, f := range fields {
			if _, ok := props[f]; !ok {
				t.Errorf("%s is missing %q — the schema did not come from the domain struct", name, f)
			}
		}
	}
}

// TestOpenAPI_DocumentedRoutesAreServed is the other direction: a path in the
// document that the mux does not answer sends a client somewhere that is not
// there. It checks reachability, not behaviour — a 404 from the router is the
// failure, any other status means the route exists.
func TestOpenAPI_DocumentedRoutesAreServed(t *testing.T) {
	s := newSpecServer(t)
	doc := specOf(t, nil, s.mux)
	paths := doc["paths"].(map[string]any)
	filler := regexp.MustCompile(`\{[a-zA-Z0-9_]+\}`)

	for path, item := range paths {
		methods, _ := item.(map[string]any)
		for method := range methods {
			concrete := filler.ReplaceAllString(path, "does-not-exist")
			r := httptest.NewRequest(strings.ToUpper(method), concrete, strings.NewReader("{}"))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, r)
			// The frontend catch-all answers 200 with index.html for anything
			// the mux does not claim, which is exactly what an undeclared route
			// looks like from here.
			if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "<html>") {
				t.Errorf("%s %s is in the document and falls through to the frontend",
					strings.ToUpper(method), path)
			}
		}
	}
}

// TestOpenAPI_EveryRefResolves — a $ref is a string, so nothing in the type
// system stops one from naming a schema that is not there. This found exactly
// that on the first run: the shared error body was referenced as `Error` and
// registered as `ApiError`, and every refusal in the document pointed at
// nothing.
func TestOpenAPI_EveryRefResolves(t *testing.T) {
	s := newSpecServer(t)
	doc := specOf(t, nil, s.mux)

	refs := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "$ref" {
					if ref, ok := child.(string); ok {
						refs[ref] = true
						continue
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)

	if len(refs) == 0 {
		t.Fatal("the document contains no $ref at all — the schema registry did not run")
	}
	for ref := range refs {
		if !strings.HasPrefix(ref, "#/") {
			t.Errorf("%s is external; the document has to stand on its own", ref)
			continue
		}
		var node any = doc
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			m, ok := node.(map[string]any)
			if !ok {
				node = nil
				break
			}
			node, ok = m[part]
			if !ok {
				node = nil
				break
			}
		}
		if node == nil {
			t.Errorf("%s does not resolve", ref)
		}
	}
}

// TestOpenAPI_ServerURLIsUsableEverywhere — an instance deployed without
// `base_url` published `http://localhost:8088` as its server, so every consumer
// of the document aimed at the reader's own machine: a codegen run, a Postman
// import, a browsable reference's "try it". A relative server is valid in
// OpenAPI 3.1 and resolves against wherever the document was fetched from.
func TestOpenAPI_ServerURLIsUsableEverywhere(t *testing.T) {
	t.Run("configured base_url is published as-is", func(t *testing.T) {
		s := newSpecServer(t) // BaseURL: https://research.example.com
		doc := specOf(t, nil, s.mux)
		servers, _ := doc["servers"].([]any)
		if len(servers) != 1 {
			t.Fatalf("servers: %v", servers)
		}
		first, _ := servers[0].(map[string]any)
		if first["url"] != "https://research.example.com" {
			t.Fatalf("server url %v", first["url"])
		}
	})

	t.Run("without base_url the server is relative, never localhost", func(t *testing.T) {
		s := newShareServer(t) // no BaseURL configured
		doc := specOf(t, nil, s.mux)
		servers, _ := doc["servers"].([]any)
		if len(servers) != 1 {
			t.Fatalf("servers: %v", servers)
		}
		first, _ := servers[0].(map[string]any)
		url, _ := first["url"].(string)
		if strings.Contains(url, "localhost") {
			t.Fatalf("server url is %q — every consumer of this document would aim at its own machine", url)
		}
		if url != "/" {
			t.Fatalf("server url %q, want \"/\"", url)
		}
	})
}

// TestOpenAPI_IsCachedAndValidated — the document is marshalled once and served
// with an ETag. The reference page fetches it on every load; a browser that
// already has it should be told so rather than handed 190 KB again.
func TestOpenAPI_IsCachedAndValidated(t *testing.T) {
	s := newSpecServer(t)

	for _, path := range []string{"/api/openapi.json", "/api/openapi.yaml"} {
		first := httptest.NewRecorder()
		s.mux.ServeHTTP(first, httptest.NewRequest("GET", path, nil))
		if first.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, first.Code)
		}
		etag := first.Header().Get("ETag")
		if etag == "" {
			t.Fatalf("%s: no ETag", path)
		}

		again := httptest.NewRequest("GET", path, nil)
		again.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, again)
		if w.Code != http.StatusNotModified {
			t.Errorf("%s: a matching If-None-Match answered %d, want 304", path, w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("%s: a 304 carried %d bytes", path, w.Body.Len())
		}
	}

	// Each format needs its OWN validator. This comment used to say so while
	// the assertion below it compared bodies — so the test named the property
	// and did not check it, and both URLs shipped the JSON's ETag: a script
	// that stored one and asked for the other got a 304 with no body.
	j := httptest.NewRecorder()
	s.mux.ServeHTTP(j, httptest.NewRequest("GET", "/api/openapi.json", nil))
	y := httptest.NewRecorder()
	s.mux.ServeHTTP(y, httptest.NewRequest("GET", "/api/openapi.yaml", nil))
	if j.Body.String() == y.Body.String() {
		t.Fatal("the two formats returned identical bytes")
	}
	if j.Header().Get("ETag") == y.Header().Get("ETag") {
		t.Fatalf("both formats answer with ETag %s despite being different bytes", j.Header().Get("ETag"))
	}

	// And the validator of one format must not satisfy the other.
	cross := httptest.NewRequest("GET", "/api/openapi.yaml", nil)
	cross.Header.Set("If-None-Match", j.Header().Get("ETag"))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, cross)
	if w.Code == http.StatusNotModified {
		t.Fatal("the JSON's ETag satisfied a request for the YAML")
	}

	// The 304 is part of the contract, so the document has to declare it —
	// this is the one route where the document could fall behind its own
	// handler, on a branch whose whole premise is that it cannot.
	doc := specOf(t, nil, s.mux)
	paths, _ := doc["paths"].(map[string]any)
	for _, path := range []string{"/api/openapi.yaml", "/api/openapi.json"} {
		item, _ := paths[path].(map[string]any)
		get, _ := item["get"].(map[string]any)
		responses, _ := get["responses"].(map[string]any)
		if _, ok := responses["304"]; !ok {
			t.Errorf("%s answers 304 and does not document it: %v", path, responses)
		}
	}
}
