package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dovod-app/app/internal/api/ws"
	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
	"github.com/dovod-app/app/internal/testdb"
	"github.com/uptrace/bun"
)

// These tests drive the real mux, because the OAuth2 flow *is* a sequence of
// routes: what a client presents at /auth/token is only meaningful next to what
// /auth/authorize handed it, and the checks that matter — a code is single-use,
// a redirect_uri is registered, a code_verifier matches — are only reachable
// with both ends attached.
//
// The whole surface used to be untested. It is how ChatGPT and Claude.ai
// authenticate, so a silent regression here is an authentication bypass that
// nothing else in this repository would notice.

type oauthServer struct {
	t        *testing.T
	mux      http.Handler
	db       *bun.DB
	authSvc  *service.AuthService
	oauthSvc *service.OAuthService
	email    string
	password string
	userID   string
}

const oauthRedirect = "https://client.example.com/callback"

func newOAuthServer(t *testing.T) *oauthServer {
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
	oauthRepo := storage.NewOAuthRepository(db)

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
		researchRepo, events, log)
	shareSvc := service.NewShareService(storage.NewShareRepository(db), access, events, log)
	skillSvc := service.NewSkillService(storage.NewSkillRepository(db), researchRepo, teamRepo, access, events, log)
	templateSvc := service.NewTemplateService(storage.NewTemplateRepository(db), storage.NewSkillRepository(db),
		teamRepo, access, log)
	annotationSvc := service.NewAnnotationService(storage.NewAnnotationRepository(db), entryRepo,
		storage.NewEntryRevisionRepository(db), access, entrySvc, entrySvc, events, log)

	authSvc := service.NewAuthService(storage.NewUserRepository(db), storage.NewAPIKeyRepository(db),
		oauthRepo, researchRepo, teamRepo,
		auth.NewJWTManager("test-secret", time.Hour), true, log)
	oauthSvc := service.NewOAuthService(oauthRepo, log)

	cfg := ServerConfig{Port: 0, AuthEnabled: true, OAuthSvc: oauthSvc, BaseURL: "https://research.example.com"}

	srv := NewServer(cfg, researchSvc, sectionSvc, entrySvc, sessionSvc, taskSvc,
		roadmapSvc, exportSvc, obsidianSvc, teamSvc, shareSvc, skillSvc, templateSvc, annotationSvc, access, authSvc, db,
		entryRepo, researchRepo, crossrefRepo, externalLinkRepo, hub, log)

	ctx := context.Background()
	user, _, err := authSvc.Register(ctx, "oauth-user@test.com", "hunter2hunter2", "Person")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	return &oauthServer{
		t: t, mux: srv.mux, db: db, authSvc: authSvc, oauthSvc: oauthSvc,
		email: "oauth-user@test.com", password: "hunter2hunter2", userID: user.ID,
	}
}

func (s *oauthServer) do(method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	s.t.Helper()
	r := httptest.NewRequest(method, path, body)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	return w
}

func (s *oauthServer) form(path string, values url.Values) *httptest.ResponseRecorder {
	return s.do("POST", path, strings.NewReader(values.Encode()), "application/x-www-form-urlencoded")
}

// register performs Dynamic Client Registration the way an external client does.
func (s *oauthServer) register() (clientID, clientSecret string) {
	s.t.Helper()
	w := s.do("POST", "/auth/register",
		strings.NewReader(`{"client_name":"ChatGPT","redirect_uris":["`+oauthRedirect+`"]}`),
		"application/json")
	if w.Code != http.StatusCreated {
		s.t.Fatalf("DCR: status %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		GrantTypes   []string `json:"grant_types"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		s.t.Fatal(err)
	}
	if out.ClientID == "" || out.ClientSecret == "" {
		s.t.Fatalf("DCR returned no credentials: %s", w.Body.String())
	}
	return out.ClientID, out.ClientSecret
}

// authorizeURL builds the URL the client sends the person to.
func authorizeURL(clientID, challenge, method, state string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", oauthRedirect)
	q.Set("response_type", "code")
	q.Set("scope", "read write")
	if state != "" {
		q.Set("state", state)
	}
	if challenge != "" {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", method)
	}
	return "/auth/authorize?" + q.Encode()
}

// approve signs the person in on the consent page and returns the code the
// client is redirected back with.
func (s *oauthServer) approve(clientID, challenge, method, state string) string {
	s.t.Helper()
	w := s.form(authorizeURL(clientID, challenge, method, state), url.Values{
		"email":    {s.email},
		"password": {s.password},
	})
	if w.Code != http.StatusFound {
		s.t.Fatalf("authorize: status %d, want 302: %s", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		s.t.Fatal(err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		s.t.Fatalf("no code in redirect %q", w.Header().Get("Location"))
	}
	return code
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
}

func (s *oauthServer) token(values url.Values) (*httptest.ResponseRecorder, tokenResponse) {
	s.t.Helper()
	w := s.form("/auth/token", values)
	var out tokenResponse
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

// s256 is the PKCE challenge a client derives from its verifier — RFC 7636's
// BASE64URL(SHA256(verifier)), computed here the way a client would rather than
// by calling the server's own helper.
func s256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// TestOAuthFlow_HappyPath walks the eight steps the README documents, and then
// spends the token on a real API route — the only proof that what the flow
// mints is a credential and not just a string.
func TestOAuthFlow_HappyPath(t *testing.T) {
	s := newOAuthServer(t)
	clientID, clientSecret := s.register()

	verifier := "a-verifier-long-enough-to-be-legal-per-rfc7636"
	code := s.approve(clientID, s256(verifier), "S256", "opaque-state")

	w, tok := s.token(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {oauthRedirect},
		"code_verifier": {verifier},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("token: status %d: %s", w.Code, w.Body.String())
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Fatalf("token payload: %s", w.Body.String())
	}
	if tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 {
		t.Fatalf("token_type %q expires_in %d", tok.TokenType, tok.ExpiresIn)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control %q — a token must not be cached", cc)
	}

	// The access token authenticates an ordinary API request as that user.
	r := httptest.NewRequest("GET", "/api/researches", nil)
	r.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("api with oauth token: status %d: %s", rec.Code, rec.Body.String())
	}
	if got := s.authSvc.UserIDForToken(context.Background(), tok.AccessToken); got != s.userID {
		t.Fatalf("token resolves to %q, want %q", got, s.userID)
	}
}

// TestOAuthFlow_StateIsReturned — a client that cannot match its state cannot
// tell its own redirect from a forged one.
func TestOAuthFlow_StateIsReturned(t *testing.T) {
	s := newOAuthServer(t)
	clientID, _ := s.register()
	w := s.form(authorizeURL(clientID, "", "", "state-42"), url.Values{
		"email": {s.email}, "password": {s.password},
	})
	loc, _ := url.Parse(w.Header().Get("Location"))
	if loc.Query().Get("state") != "state-42" {
		t.Fatalf("state not echoed: %q", w.Header().Get("Location"))
	}
}

func TestOAuthFlow_CodeIsSingleUse(t *testing.T) {
	s := newOAuthServer(t)
	clientID, clientSecret := s.register()
	verifier := "verifier-for-the-single-use-test-0000"
	code := s.approve(clientID, s256(verifier), "S256", "")

	exchange := func() *httptest.ResponseRecorder {
		w, _ := s.token(url.Values{
			"grant_type": {"authorization_code"}, "code": {code},
			"client_id": {clientID}, "client_secret": {clientSecret},
			"redirect_uri": {oauthRedirect}, "code_verifier": {verifier},
		})
		return w
	}
	if w := exchange(); w.Code != http.StatusOK {
		t.Fatalf("first exchange: %d: %s", w.Code, w.Body.String())
	}
	if w := exchange(); w.Code != http.StatusBadRequest {
		t.Fatalf("replayed code: status %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestOAuthFlow_PKCEIsEnforced(t *testing.T) {
	s := newOAuthServer(t)
	clientID, clientSecret := s.register()
	verifier := "the-verifier-the-legitimate-client-holds"

	cases := []struct {
		name     string
		verifier string
	}{
		{"wrong verifier", "an-attacker-guess"},
		{"no verifier at all", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := s.approve(clientID, s256(verifier), "S256", "")
			w, _ := s.token(url.Values{
				"grant_type": {"authorization_code"}, "code": {code},
				"client_id": {clientID}, "client_secret": {clientSecret},
				"redirect_uri": {oauthRedirect}, "code_verifier": {tc.verifier},
			})
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
			}
			// The failed attempt must not have spent the code — but the correct
			// verifier still must, so check the real one now works exactly once.
			ok, _ := s.token(url.Values{
				"grant_type": {"authorization_code"}, "code": {code},
				"client_id": {clientID}, "client_secret": {clientSecret},
				"redirect_uri": {oauthRedirect}, "code_verifier": {verifier},
			})
			if ok.Code != http.StatusOK {
				t.Fatalf("legitimate exchange after a failed one: %d: %s", ok.Code, ok.Body.String())
			}
		})
	}
}

func TestOAuthFlow_TokenRefusals(t *testing.T) {
	s := newOAuthServer(t)
	clientID, clientSecret := s.register()
	verifier := "verifier-for-the-refusal-matrix-000000"

	// A second registered client, to prove a code is bound to the one that
	// asked for it.
	otherID, otherSecret := s.register()

	cases := []struct {
		name   string
		mutate func(url.Values)
	}{
		{"wrong client_secret", func(v url.Values) { v.Set("client_secret", "not-the-secret") }},
		{"unknown client_id", func(v url.Values) { v.Set("client_id", "00000000-0000-0000-0000-000000000000") }},
		{"another client's credentials", func(v url.Values) {
			v.Set("client_id", otherID)
			v.Set("client_secret", otherSecret)
		}},
		{"redirect_uri mismatch", func(v url.Values) { v.Set("redirect_uri", "https://client.example.com/other") }},
		{"unknown code", func(v url.Values) { v.Set("code", "a-code-nobody-issued") }},
		{"unsupported grant_type", func(v url.Values) { v.Set("grant_type", "password") }},
		{"missing code", func(v url.Values) { v.Del("code") }},
		{"missing client credentials", func(v url.Values) { v.Del("client_id"); v.Del("client_secret") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code := s.approve(clientID, s256(verifier), "S256", "")
			v := url.Values{
				"grant_type": {"authorization_code"}, "code": {code},
				"client_id": {clientID}, "client_secret": {clientSecret},
				"redirect_uri": {oauthRedirect}, "code_verifier": {verifier},
			}
			tc.mutate(v)
			w, out := s.token(v)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
			}
			if out.AccessToken != "" {
				t.Fatalf("a refusal handed out a token: %s", w.Body.String())
			}
		})
	}
}

// TestOAuthFlow_BasicAuthAndJSON — ChatGPT sends form-encoded with the secret in
// the body; other clients use HTTP Basic or a JSON body. All three reach the
// same exchange, and all three used to be unproven.
func TestOAuthFlow_BasicAuthAndJSON(t *testing.T) {
	s := newOAuthServer(t)
	clientID, clientSecret := s.register()
	verifier := "verifier-for-the-transport-shapes-00000"

	t.Run("basic auth", func(t *testing.T) {
		code := s.approve(clientID, s256(verifier), "S256", "")
		body := url.Values{
			"grant_type": {"authorization_code"}, "code": {code},
			"redirect_uri": {oauthRedirect}, "code_verifier": {verifier},
		}
		r := httptest.NewRequest("POST", "/auth/token", strings.NewReader(body.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetBasicAuth(clientID, clientSecret)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("json body", func(t *testing.T) {
		code := s.approve(clientID, s256(verifier), "S256", "")
		payload, _ := json.Marshal(map[string]string{
			"grant_type": "authorization_code", "code": code,
			"client_id": clientID, "client_secret": clientSecret,
			"redirect_uri": oauthRedirect, "code_verifier": verifier,
		})
		w := s.do("POST", "/auth/token", strings.NewReader(string(payload)), "application/json")
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		w := s.do("POST", "/auth/token", strings.NewReader("{"), "application/json")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", w.Code)
		}
	})
}

// TestOAuthAuthorize_RefusesUnregisteredRedirect is the open-redirect guard: an
// attacker who knows a client_id must not be able to aim the code at a host of
// their choosing.
func TestOAuthAuthorize_RefusesUnregisteredRedirect(t *testing.T) {
	s := newOAuthServer(t)
	clientID, _ := s.register()

	q := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {"https://evil.example.com/steal"},
		"response_type": {"code"},
	}
	// Even with correct credentials in the body, nothing may be redirected.
	w := s.form("/auth/authorize?"+q.Encode(), url.Values{
		"email": {s.email}, "password": {s.password},
	})
	if w.Code == http.StatusFound {
		t.Fatalf("redirected to an unregistered uri: %q", w.Header().Get("Location"))
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("Location header set on a refusal: %q", loc)
	}
}

func TestOAuthAuthorize_PageAndRefusals(t *testing.T) {
	s := newOAuthServer(t)
	clientID, _ := s.register()

	t.Run("renders the sign-in form", func(t *testing.T) {
		w := s.do("GET", authorizeURL(clientID, "", "", ""), nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status %d", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, `name="email"`) || !strings.Contains(body, `name="password"`) {
			t.Fatalf("no sign-in form rendered: %s", body)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("Content-Type %q", ct)
		}
	})

	t.Run("missing client_id is a 400 page", func(t *testing.T) {
		w := s.do("GET", "/auth/authorize", nil, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", w.Code)
		}
	})

	t.Run("unknown client is refused without redirecting", func(t *testing.T) {
		q := url.Values{"client_id": {"nope"}, "redirect_uri": {oauthRedirect}}
		w := s.do("GET", "/auth/authorize?"+q.Encode(), nil, "")
		if w.Code == http.StatusFound {
			t.Fatalf("redirected for an unknown client")
		}
	})

	t.Run("implicit flow is refused", func(t *testing.T) {
		q := url.Values{
			"client_id": {clientID}, "redirect_uri": {oauthRedirect}, "response_type": {"token"},
		}
		w := s.do("GET", "/auth/authorize?"+q.Encode(), nil, "")
		if w.Code == http.StatusFound {
			t.Fatalf("implicit flow was honoured")
		}
		if !strings.Contains(w.Body.String(), "response_type") {
			t.Fatalf("no explanation rendered: %s", w.Body.String())
		}
	})

	t.Run("wrong password issues no code", func(t *testing.T) {
		w := s.form(authorizeURL(clientID, "", "", ""), url.Values{
			"email": {s.email}, "password": {"wrong-password"},
		})
		if w.Code == http.StatusFound {
			t.Fatalf("signed in with the wrong password: %q", w.Header().Get("Location"))
		}
		var n int
		if err := s.db.NewSelect().ColumnExpr("count(*)").TableExpr("oauth_codes").
			Scan(context.Background(), &n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d authorization codes exist after a failed sign-in", n)
		}
	})

	t.Run("empty credentials issue no code", func(t *testing.T) {
		w := s.form(authorizeURL(clientID, "", "", ""), url.Values{})
		if w.Code == http.StatusFound {
			t.Fatalf("empty credentials were accepted")
		}
	})
}

func TestOAuthDCR_Contract(t *testing.T) {
	s := newOAuthServer(t)

	t.Run("registration returns a usable client", func(t *testing.T) {
		w := s.do("POST", "/auth/register",
			strings.NewReader(`{"client_name":"ChatGPT","redirect_uris":["`+oauthRedirect+`"]}`),
			"application/json")
		if w.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"client_id", "client_secret", "client_name", "redirect_uris",
			"grant_types", "response_types", "token_endpoint_auth_method"} {
			if _, ok := out[k]; !ok {
				t.Fatalf("RFC 7591 response is missing %q: %s", k, w.Body.String())
			}
		}
	})

	t.Run("redirect_uris is required", func(t *testing.T) {
		w := s.do("POST", "/auth/register", strings.NewReader(`{"client_name":"No URIs"}`), "application/json")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
		}
	})

	t.Run("malformed body is a 400", func(t *testing.T) {
		w := s.do("POST", "/auth/register", strings.NewReader("not json"), "application/json")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", w.Code)
		}
	})

	t.Run("an unnamed client still registers", func(t *testing.T) {
		w := s.do("POST", "/auth/register",
			strings.NewReader(`{"redirect_uris":["`+oauthRedirect+`"]}`), "application/json")
		if w.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
	})
}

// TestOAuthMetadata_MatchesTheRoutes — a client discovers this server entirely
// from these two documents. An endpoint named here that the mux does not serve
// is a client that cannot connect.
func TestOAuthMetadata_MatchesTheRoutes(t *testing.T) {
	s := newOAuthServer(t)

	for _, path := range []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/openid-configuration",
	} {
		w := s.do("GET", path, nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, w.Code)
		}
		var m map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if m["issuer"] != "https://research.example.com" {
			t.Fatalf("%s: issuer %v — base_url is what a client trusts", path, m["issuer"])
		}
		for key, want := range map[string]string{
			"authorization_endpoint": "https://research.example.com/auth/authorize",
			"token_endpoint":         "https://research.example.com/auth/token",
			"registration_endpoint":  "https://research.example.com/auth/register",
		} {
			if m[key] != want {
				t.Fatalf("%s: %s = %v, want %v", path, key, m[key], want)
			}
		}
		grants, _ := m["grant_types_supported"].([]any)
		var hasRefresh bool
		for _, g := range grants {
			if g == "refresh_token" {
				hasRefresh = true
			}
		}
		if !hasRefresh {
			t.Fatalf("%s: refresh_token is served but not advertised: %v", path, grants)
		}
	}

	w := s.do("GET", "/.well-known/oauth-protected-resource", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("protected-resource: status %d", w.Code)
	}
	var pr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &pr)
	if pr["resource"] != "https://research.example.com/mcp" {
		t.Fatalf("resource %v", pr["resource"])
	}
}

// TestOAuthToken_ExpiresAndRefreshes is the pair of behaviours that used to be
// missing together: the token endpoint announced expires_in and nothing
// enforced it, so an access token authenticated forever and the refresh token
// it was issued with had no grant to redeem it.
func TestOAuthToken_ExpiresAndRefreshes(t *testing.T) {
	s := newOAuthServer(t)
	clientID, clientSecret := s.register()
	verifier := "verifier-for-the-expiry-test-000000000"
	code := s.approve(clientID, s256(verifier), "S256", "")

	_, tok := s.token(url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {clientID}, "client_secret": {clientSecret},
		"redirect_uri": {oauthRedirect}, "code_verifier": {verifier},
	})
	if tok.AccessToken == "" {
		t.Fatal("no access token")
	}

	apiStatus := func(token string) int {
		r := httptest.NewRequest("GET", "/api/researches", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w.Code
	}
	if got := apiStatus(tok.AccessToken); got != http.StatusOK {
		t.Fatalf("fresh token: status %d", got)
	}

	// Age the token past its expires_at, the way an hour of wall clock would.
	hash := sha256.Sum256([]byte(tok.AccessToken))
	past := time.Now().UTC().Add(-time.Minute).Format(time.DateTime)
	if _, err := s.db.NewUpdate().Table("oauth_tokens").
		Set("expires_at=?", past).
		Where("access_token_hash=?", hex.EncodeToString(hash[:])).
		Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := apiStatus(tok.AccessToken); got != http.StatusUnauthorized {
		t.Fatalf("expired token: status %d, want 401", got)
	}

	// The refresh token is the way back, and it rotates.
	w, fresh := s.token(url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken},
		"client_id": {clientID}, "client_secret": {clientSecret},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("refresh: status %d: %s", w.Code, w.Body.String())
	}
	if fresh.AccessToken == "" || fresh.AccessToken == tok.AccessToken {
		t.Fatalf("refresh did not mint a new access token")
	}
	if fresh.RefreshToken == tok.RefreshToken {
		t.Fatal("refresh token was not rotated — a stolen one would live forever")
	}
	if got := apiStatus(fresh.AccessToken); got != http.StatusOK {
		t.Fatalf("refreshed token: status %d", got)
	}

	// The retired refresh token is dead, and the old access token stays dead.
	if again, _ := s.token(url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken},
		"client_id": {clientID}, "client_secret": {clientSecret},
	}); again.Code != http.StatusBadRequest {
		t.Fatalf("replayed refresh token: status %d, want 400", again.Code)
	}
	if got := apiStatus(tok.AccessToken); got != http.StatusUnauthorized {
		t.Fatalf("old access token after refresh: status %d, want 401", got)
	}
}

func TestOAuthRefresh_Refusals(t *testing.T) {
	s := newOAuthServer(t)
	clientID, clientSecret := s.register()
	otherID, otherSecret := s.register()
	verifier := "verifier-for-the-refresh-refusals-00000"
	code := s.approve(clientID, s256(verifier), "S256", "")
	_, tok := s.token(url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {clientID}, "client_secret": {clientSecret},
		"redirect_uri": {oauthRedirect}, "code_verifier": {verifier},
	})

	cases := []struct {
		name string
		v    url.Values
	}{
		{"unknown refresh token", url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {"nobody-issued-this"},
			"client_id": {clientID}, "client_secret": {clientSecret}}},
		{"missing refresh token", url.Values{
			"grant_type": {"refresh_token"},
			"client_id":  {clientID}, "client_secret": {clientSecret}}},
		{"another client's refresh token", url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken},
			"client_id": {otherID}, "client_secret": {otherSecret}}},
		{"wrong client secret", url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken},
			"client_id": {clientID}, "client_secret": {"not-the-secret"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, out := s.token(tc.v)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
			}
			if out.AccessToken != "" {
				t.Fatalf("a refusal handed out a token")
			}
		})
	}

	// After all of that the legitimate refresh token still works: a refusal
	// must not retire somebody else's credential.
	if w, _ := s.token(url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken},
		"client_id": {clientID}, "client_secret": {clientSecret},
	}); w.Code != http.StatusOK {
		t.Fatalf("legitimate refresh after refusals: status %d: %s", w.Code, w.Body.String())
	}
}

// TestOAuthRoutes_AbsentWithoutAuth — with auth disabled there is nobody to
// authorize, and a live /auth/token would mint credentials against a server
// that has no accounts. The paths still answer, because the SPA catch-all
// serves the frontend for anything the mux does not claim; what must not come
// back is an OAuth response.
func TestOAuthRoutes_AbsentWithoutAuth(t *testing.T) {
	s := newShareServer(t) // auth_enabled: false
	for path, forbidden := range map[string]string{
		"/auth/token":    "access_token",
		"/auth/register": "client_secret",
	} {
		r := httptest.NewRequest("POST", path, strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatalf("%s served %q with auth disabled: %s", path, forbidden, w.Body.String())
		}
	}
}
