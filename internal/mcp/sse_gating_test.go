package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The SSE transport is the second door into the same 52 tools, on its own
// listener. It used to be guarded only when accounts were on — so an instance
// configured with `api_token` and no accounts refused an anonymous
// `POST /api/entries` and then handed the same caller a working MCP session on
// this port. These tests hold the three cases level with the REST side.

func reached() (http.Handler, *bool) {
	got := false
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = true
		w.WriteHeader(http.StatusOK)
	}), &got
}

func probe(t *testing.T, h http.Handler, target, addr string, headers map[string]string) int {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.RemoteAddr = addr
	// auth.LocalRequest reads Host as well as the peer address, and
	// httptest defaults it to example.com.
	if addr == "127.0.0.1:1" {
		r.Host = "localhost:8081"
	} else {
		r.Host = "research.example.com"
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func TestSSETokenMiddleware(t *testing.T) {
	const token = "write-token"

	t.Run("no credential is refused", func(t *testing.T) {
		next, got := reached()
		if code := probe(t, sseTokenMiddleware(token, next), "/sse", "203.0.113.9:1", nil); code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", code)
		}
		if *got {
			t.Error("the request reached the transport with no credential")
		}
	})

	t.Run("a wrong token is refused", func(t *testing.T) {
		next, got := reached()
		h := sseTokenMiddleware(token, next)
		if code := probe(t, h, "/sse", "203.0.113.9:1", map[string]string{"Authorization": "Bearer nope"}); code != http.StatusUnauthorized || *got {
			t.Errorf("status = %d, reached = %v; want 401 and not reached", code, *got)
		}
	})

	t.Run("the header form gets through", func(t *testing.T) {
		next, got := reached()
		h := sseTokenMiddleware(token, next)
		probe(t, h, "/sse", "203.0.113.9:1", map[string]string{"Authorization": "Bearer " + token})
		if !*got {
			t.Error("a correct bearer token was refused")
		}
	})

	t.Run("the query form is refused, deliberately", func(t *testing.T) {
		// sseAuthMiddleware accepts ?token= because EventSource cannot set
		// headers, and that is the right trade for a per-user JWT or API key:
		// scoped, and revocable one at a time. The instance api_token is
		// neither, it does not rotate, and a query string is written verbatim
		// into every proxy access log. Nothing breaks by being strict — until
		// this middleware existed the transport took no credential at all here,
		// so no client is passing this token in a URL today.
		next, got := reached()
		h := sseTokenMiddleware(token, next)
		if code := probe(t, h, "/sse?token="+token, "203.0.113.9:1", nil); code != http.StatusUnauthorized || *got {
			t.Errorf("status = %d, reached = %v; want 401 and not reached", code, *got)
		}
	})
}

func TestSSELocalOnlyMiddleware(t *testing.T) {
	t.Run("a remote caller is refused", func(t *testing.T) {
		next, got := reached()
		if code := probe(t, sseLocalOnlyMiddleware(next), "/sse", "203.0.113.9:1", nil); code != http.StatusUnauthorized || *got {
			t.Errorf("status = %d, reached = %v; want 401 and not reached", code, *got)
		}
	})

	t.Run("a caller proxied through the same host is refused", func(t *testing.T) {
		next, got := reached()
		h := sseLocalOnlyMiddleware(next)
		code := probe(t, h, "/sse", "127.0.0.1:1", map[string]string{"X-Forwarded-For": "203.0.113.9"})
		if code != http.StatusUnauthorized || *got {
			t.Errorf("status = %d, reached = %v; want 401 and not reached", code, *got)
		}
	})

	t.Run("a page on another origin is refused", func(t *testing.T) {
		next, got := reached()
		h := sseLocalOnlyMiddleware(next)
		code := probe(t, h, "/sse", "127.0.0.1:1", map[string]string{"Origin": "https://evil.example"})
		if code != http.StatusUnauthorized || *got {
			t.Errorf("status = %d, reached = %v; want 401 and not reached", code, *got)
		}
	})

	t.Run("the local client gets through", func(t *testing.T) {
		next, got := reached()
		probe(t, sseLocalOnlyMiddleware(next), "/sse", "127.0.0.1:1", nil)
		if !*got {
			t.Error("a loopback caller was refused")
		}
	})
}
