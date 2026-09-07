package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The gates in this file are the ones nothing else can see.
//
// TestRouter_EveryScopedRouteRefusesAnonymously walks router.Routes() and fires
// every scoped route with no credential — but /mcp and the `/` catch-all go
// through router.undocumented, which that test skips by design, and the
// no-credential posture it never varies. Both holes were real: an instance with
// an api_token refused an anonymous POST /api/entries and then served the same
// caller every tool through /mcp, and an instance with no credential at all
// accepted writes from anywhere while /api/health reported write_api: false.

// mcpProbe stands in for the real MCP transport. It only has to record whether
// the request reached it.
type mcpProbe struct{ reached bool }

func (m *mcpProbe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.reached = true
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
}

const (
	localAddr  = "127.0.0.1:54321"
	remoteAddr = "203.0.113.9:54321"
)

// from stamps a request as coming from one place or the other. Both halves
// matter: auth.LocalRequest reads the peer address *and* the Host header, and
// httptest.NewRequest defaults Host to example.com, which is not loopback.
func from(r *http.Request, addr string) *http.Request {
	r.RemoteAddr = addr
	if addr == localAddr {
		r.Host = "localhost:8088"
	} else {
		r.Host = "research.example.com"
	}
	return r
}

// mcpRequest is shaped the way the catch-all sniffs for MCP traffic: a POST
// carrying JSON. Sent to `/` it exercises the catch-all, sent to `/mcp` the
// endpoint itself.
func mcpRequest(path, addr, bearer string) *http.Request {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	r := from(httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)), addr)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	return r
}

// TestMCP_HonoursTheAPIToken is the guard for the hole the audit found: the
// api_token gated the REST writes and not the MCP transport, so the credential
// bought nothing — every tool, writes included, was reachable without it.
func TestMCP_HonoursTheAPIToken(t *testing.T) {
	probe := &mcpProbe{}
	s := newSpecServer(t, func(c *ServerConfig) {
		c.AuthEnabled = false
		c.APIToken = "write-token"
		c.MCPHandler = probe
	})

	// Both doors, and a local caller as well as a remote one: an api_token
	// instance has made its choice, and loopback does not override it.
	for _, path := range []string{"/mcp", "/"} {
		for _, addr := range []string{remoteAddr, localAddr} {
			t.Run("refuses anonymously via "+path+" from "+addr, func(t *testing.T) {
				probe.reached = false
				w := httptest.NewRecorder()
				s.mux.ServeHTTP(w, mcpRequest(path, addr, ""))

				if w.Code != http.StatusUnauthorized {
					t.Errorf("status = %d, want 401 (body: %s)", w.Code, w.Body.String())
				}
				if probe.reached {
					t.Error("the request reached the MCP handler without a credential")
				}
			})
		}
	}

	t.Run("a wrong token is still refused", func(t *testing.T) {
		probe.reached = false
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, mcpRequest("/mcp", remoteAddr, "not-the-token"))
		if w.Code != http.StatusUnauthorized || probe.reached {
			t.Errorf("status = %d, reached = %v; want 401 and not reached", w.Code, probe.reached)
		}
	})

	t.Run("the configured token gets through", func(t *testing.T) {
		probe.reached = false
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, mcpRequest("/mcp", remoteAddr, "write-token"))
		if !probe.reached {
			t.Errorf("the api_token did not reach the MCP handler: status %d, body %s", w.Code, w.Body.String())
		}
	})
}

// TestMCP_LocalOnlyWithNoCredential covers the other posture: no api_token and
// no accounts. Every tool reaches the same services a write goes through, so
// leaving this door open would make the gate on the REST side decorative.
func TestMCP_LocalOnlyWithNoCredential(t *testing.T) {
	probe := &mcpProbe{}
	s := newSpecServer(t, func(c *ServerConfig) {
		c.AuthEnabled = false
		c.APIToken = ""
		c.MCPHandler = probe
	})

	refused := func(t *testing.T, r *http.Request) {
		t.Helper()
		probe.reached = false
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || probe.reached {
			t.Errorf("status = %d, reached = %v; want 401 and not reached", w.Code, probe.reached)
		}
	}

	t.Run("a remote caller is refused", func(t *testing.T) {
		refused(t, mcpRequest("/mcp", remoteAddr, ""))
	})

	t.Run("a proxied caller from loopback is refused", func(t *testing.T) {
		// The deploy/nginx template is same-host, so every remote caller
		// arrives from 127.0.0.1. The forwarding header is what separates them.
		r := mcpRequest("/mcp", localAddr, "")
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		refused(t, r)
	})

	t.Run("a page on another origin driving the operator's browser is refused", func(t *testing.T) {
		// corsMiddleware answers with Access-Control-Allow-Origin: *, so
		// without this the gate would trust any website the operator visits.
		r := mcpRequest("/mcp", localAddr, "")
		r.Header.Set("Origin", "https://evil.example")
		refused(t, r)
	})

	t.Run("the local client gets through", func(t *testing.T) {
		probe.reached = false
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, mcpRequest("/mcp", localAddr, ""))
		if !probe.reached {
			t.Errorf("a loopback caller was refused: status %d, body %s", w.Code, w.Body.String())
		}
	})
}

// TestWrites_LocalOnlyWithNoCredential is the REST half of the same promise.
// The config table has always said an unset api_token means the write API is
// disabled; it was not, and anyone who could reach the port could create a
// research — or a share link, publishing content to the internet.
func TestWrites_LocalOnlyWithNoCredential(t *testing.T) {
	s := newSpecServer(t, func(c *ServerConfig) {
		c.AuthEnabled = false
		c.APIToken = ""
	})

	write := func(addr string, headers map[string]string) *httptest.ResponseRecorder {
		body := `{"name":"probe","description":"d","goal":"g"}`
		r := from(httptest.NewRequest(http.MethodPost, "/api/researches", strings.NewReader(body)), addr)
		r.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		return w
	}

	if w := write(remoteAddr, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("remote write: status = %d, want 401 (body: %s)", w.Code, w.Body.String())
	}
	if w := write(localAddr, map[string]string{"Origin": "https://evil.example"}); w.Code != http.StatusUnauthorized {
		t.Errorf("cross-origin write from the operator's browser: status = %d, want 401", w.Code)
	}
	// DNS rebinding: the peer really is loopback, and the name is not.
	r := from(httptest.NewRequest(http.MethodPost, "/api/researches", strings.NewReader(`{"name":"x","description":"d","goal":"g"}`)), localAddr)
	r.Host = "evil.example"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("rebound Host: status = %d, want 401", w.Code)
	}

	// Not asserting 201 — the point is only that the gate is not what stops it.
	if w := write(localAddr, nil); w.Code == http.StatusUnauthorized {
		t.Errorf("local write was refused: %s", w.Body.String())
	}

	// Reads stay open in this mode, as they always have.
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, from(httptest.NewRequest(http.MethodGet, "/api/researches", nil), remoteAddr))
	if rr.Code != http.StatusOK {
		t.Errorf("remote read: status = %d, want 200 — reads are unauthenticated in this mode by design", rr.Code)
	}
}

// TestHealth_WriteAPIAnswersPerCaller — an operator asking whether the port is
// safe to expose was told `write_api: false` by a server that was accepting
// anonymous writes. It is the one field on this route they would act on, and
// the web UI decides whether to render Edit from it.
func TestHealth_WriteAPIAnswersPerCaller(t *testing.T) {
	health := func(s *specServer, addr, bearer string) map[string]any {
		r := from(httptest.NewRequest(http.MethodGet, "/api/health", nil), addr)
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("health is not JSON: %v (%s)", err, w.Body.String())
		}
		return out
	}

	t.Run("no credential configured", func(t *testing.T) {
		s := newSpecServer(t, func(c *ServerConfig) {
			c.AuthEnabled = false
			c.APIToken = ""
		})
		if got := health(s, localAddr, "")["write_api"]; got != true {
			t.Errorf("local caller: write_api = %v, want true — this caller can write", got)
		}
		if got := health(s, remoteAddr, "")["write_api"]; got != false {
			t.Errorf("remote caller: write_api = %v, want false — this caller cannot", got)
		}
	})

	t.Run("api_token configured", func(t *testing.T) {
		// The case that matters for the UI: a browser holds no api_token, so it
		// must be told false even from loopback. Reporting the config here
		// instead of the credential is what rendered Edit buttons that 401.
		s := newSpecServer(t, func(c *ServerConfig) {
			c.AuthEnabled = false
			c.APIToken = "write-token"
		})
		if got := health(s, localAddr, "")["write_api"]; got != false {
			t.Errorf("local browser with no token: write_api = %v, want false", got)
		}
		if got := health(s, remoteAddr, "write-token")["write_api"]; got != true {
			t.Errorf("caller presenting the token: write_api = %v, want true", got)
		}
		if got := health(s, remoteAddr, "wrong")["write_api"]; got != false {
			t.Errorf("caller presenting a wrong token: write_api = %v, want false", got)
		}
	})

	t.Run("accounts on", func(t *testing.T) {
		s := newSpecServer(t)
		if got := health(s, remoteAddr, "")["write_api"]; got != true {
			t.Errorf("write_api = %v, want true — writes exist here, roles decide the rest", got)
		}
	})
}

// TestAuthInfo_AnswersWhenAccountsAreOff is the guard for the dead web UI.
//
// The route used to live inside `if cfg.AuthEnabled`, so with accounts off it
// fell to the SPA catch-all, which returned index.html with a 200. The
// composable read a successful fetch as "accounts are on" and sent every page
// to a login screen that could not be used, because /api/auth/login did not
// exist either. Both halves are asserted here: the status, and that the body is
// the answer rather than a web page.
func TestAuthInfo_AnswersWhenAccountsAreOff(t *testing.T) {
	s := newSpecServer(t, func(c *ServerConfig) {
		c.AuthEnabled = false
		c.APIToken = ""
	})

	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, from(httptest.NewRequest(http.MethodGet, "/api/auth/info", nil), localAddr))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON — an HTML page here is the original bug", ct)
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, w.Body.String())
	}
	if out["auth_enabled"] != false {
		t.Errorf("auth_enabled = %v, want false", out["auth_enabled"])
	}
	if out["allow_registration"] != false {
		t.Errorf("allow_registration = %v, want false — there is nothing to register with", out["allow_registration"])
	}
}

// TestAuthInfo_AutoLoginTokenIsLocalOnly — the token is a 30-day JWT for the
// default user on a public route. Served to anyone who asked, it handed a
// stranger that account, and an API key to outlive it.
func TestAuthInfo_AutoLoginTokenIsLocalOnly(t *testing.T) {
	s := newSpecServer(t, func(c *ServerConfig) {
		c.AutoLoginToken = "a-real-jwt-for-the-default-user"
	})

	tokenFor := func(addr string, headers map[string]string) (any, bool) {
		r := from(httptest.NewRequest(http.MethodGet, "/api/auth/info", nil), addr)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, r)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("body is not JSON: %v (%s)", err, w.Body.String())
		}
		v, ok := out["auto_login_token"]
		return v, ok
	}

	if v, ok := tokenFor(localAddr, nil); !ok || v != "a-real-jwt-for-the-default-user" {
		t.Errorf("local caller got %v (present=%v); the token exists for this caller", v, ok)
	}
	if _, ok := tokenFor(remoteAddr, nil); ok {
		t.Error("a remote caller was handed the auto-login token")
	}
	if _, ok := tokenFor(localAddr, map[string]string{"X-Forwarded-For": "203.0.113.9"}); ok {
		t.Error("a caller proxied through the same host was handed the auto-login token")
	}
	if _, ok := tokenFor(localAddr, map[string]string{"Origin": "https://evil.example"}); ok {
		t.Error("a page on another origin read the auto-login token out of the operator's browser")
	}
}

// TestAPI_UnmatchedPathIsJSON404 — a path under /api/ that matched no route used
// to be answered by the catch-all: 200 and the SPA's index page on a GET, an MCP
// protocol error on a POST or DELETE. The 200 is how the auth probe above came
// to read a missing route as a successful answer.
func TestAPI_UnmatchedPathIsJSON404(t *testing.T) {
	s := newSpecServer(t, func(c *ServerConfig) { c.MCPHandler = &mcpProbe{} })

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			r := from(httptest.NewRequest(method, "/api/no/such/endpoint", strings.NewReader(`{}`)), localAddr)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, r)

			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404 (body: %s)", w.Code, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want JSON", ct)
			}
			if strings.Contains(w.Body.String(), "<!DOCTYPE") {
				t.Error("the SPA answered for a missing API route")
			}
		})
	}

	// A method a real path does not serve lands in the same place, rather than
	// in the MCP transport.
	t.Run("wrong method on a real path", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, from(httptest.NewRequest(http.MethodDelete, "/api/health", nil), localAddr))
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404 (body: %s)", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "Mcp-Session-Id") {
			t.Error("the MCP transport answered for a wrong method on an API path")
		}
	})

	// The share prefix keeps its own refusal, which says something different on
	// purpose and must not be swallowed by the catch-all above it.
	t.Run("the share prefix is untouched", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, from(httptest.NewRequest(http.MethodGet, "/api/shared/mrs_nosuchtoken", nil), localAddr))
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
		if !strings.Contains(w.Body.String(), "no longer available") {
			t.Errorf("share refusal was replaced by the generic one: %s", w.Body.String())
		}
	})
}
