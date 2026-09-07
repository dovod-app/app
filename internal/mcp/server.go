package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type Server struct {
	server   *sdkmcp.Server
	research *service.ResearchService
	section  *service.SectionService
	entry    *service.EntryService
	session  *service.SessionService
	task     *service.TaskService
	roadmap  *service.RoadmapService
	export   *service.ExportService
	team     *service.TeamService
	skill    *service.SkillService
	template *service.TemplateService
	// annotation serves the queue of marks a person left on the documents. It
	// is read and answered over MCP and never written: an annotation is born
	// from someone reading, and there is deliberately no tool that creates one.
	annotation *service.AnnotationService
	// resume answers "what was I doing" for a chat that has no history of this
	// research. It is read-only and aggregates what the other services own.
	resume  *service.ResumeService
	baseURL string
	log     *slog.Logger
}

// SetBaseURL gives the tools a public origin to build download links with. It
// is set after construction because the API server's configuration is resolved
// later; a tool reads it when it is called, not when it is registered.
func (s *Server) SetBaseURL(url string) { s.baseURL = url }

func NewServer(
	research *service.ResearchService,
	section *service.SectionService,
	entry *service.EntryService,
	session *service.SessionService,
	task *service.TaskService,
	roadmap *service.RoadmapService,
	export *service.ExportService,
	team *service.TeamService,
	skill *service.SkillService,
	template *service.TemplateService,
	annotation *service.AnnotationService,
	resume *service.ResumeService,
	log *slog.Logger,
	version string,
) *Server {
	s := &Server{
		server: sdkmcp.NewServer(&sdkmcp.Implementation{
			Name:    "mcp-research",
			Title:   "Dovod",
			Version: version,
		}, &sdkmcp.ServerOptions{
			Logger:       log,
			Instructions: "Dovod — a workspace for structured research and clear decisions with AI. Use research/initialize prompt to start. Use task_create/task_list to manage your todo list.",
		}),
		research:   research,
		section:    section,
		entry:      entry,
		session:    session,
		task:       task,
		roadmap:    roadmap,
		export:     export,
		team:       team,
		skill:      skill,
		template:   template,
		annotation: annotation,
		resume:     resume,
		log:        log,
	}

	s.registerTools()
	s.registerPrompts()

	return s
}

// RunStdio runs the MCP server over stdin/stdout.
// If defaultUser is provided, all operations are scoped to that user.
func (s *Server) RunStdio(ctx context.Context, defaultUser *domain.User) error {
	if defaultUser != nil {
		ctx = auth.WithUser(ctx, defaultUser)
		s.log.Info("stdio: running as user", "email", defaultUser.Email, "id", defaultUser.ID)
	}
	return s.server.Run(ctx, &sdkmcp.StdioTransport{})
}

// RunSSE serves the legacy SSE transport on its own listener.
//
// apiToken is the instance-wide write token. It matters here because this
// listener carries the same 52 tools the HTTP API's writes go through, and it
// used to be gated only when accounts were on: an instance configured with
// `api_token` and no accounts refused an anonymous `POST /api/entries` and then
// handed the same caller a working MCP session on this port. The three cases
// below are the three the REST side has, and they have to stay the same three —
// a credential that closes one door and not the other is not a credential.
func (s *Server) RunSSE(ctx context.Context, port int, authSvc *service.AuthService, apiToken, baseURL string) error {
	sseHandler := sdkmcp.NewSSEHandler(func(r *http.Request) *sdkmcp.Server {
		return s.server
	}, nil)

	var handler http.Handler = sseHandler
	switch {
	case authSvc != nil:
		// Accounts: a JWT or an API key, and the WWW-Authenticate header that
		// starts OAuth discovery.
		handler = sseAuthMiddleware(authSvc, baseURL, sseHandler)
	case apiToken != "":
		handler = sseTokenMiddleware(apiToken, sseHandler)
	default:
		// No credential configured at all: local callers only, the same rule
		// the REST writes take. See auth.LocalRequest for why the address is
		// not enough on its own.
		handler = sseLocalOnlyMiddleware(sseHandler)
	}

	addr := fmt.Sprintf(":%d", port)
	s.log.Info("MCP SSE server listening", "addr", addr)

	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	return srv.ListenAndServe()
}

func (s *Server) MCPServer() *sdkmcp.Server {
	return s.server
}

// StreamableHTTPHandler returns an http.Handler for the Streamable HTTP transport.
// Mount this at /mcp on the API server.
func (s *Server) StreamableHTTPHandler() http.Handler {
	return sdkmcp.NewStreamableHTTPHandler(func(r *http.Request) *sdkmcp.Server {
		return s.server
	}, nil)
}

// sseTokenMiddleware guards the SSE transport with the instance write token,
// for a server that has one and no accounts.
//
// Header only — deliberately not the `?token=` form sseAuthMiddleware also
// accepts. That form exists because EventSource cannot set headers, and it is
// the right trade for a per-user JWT or API key: those are scoped to one
// account and can be revoked one at a time. The instance api_token is neither.
// It is the longest-lived, highest-privilege secret this server has, it does not
// rotate, and in a query string it is written verbatim into every proxy access
// log — deploy/nginx/ uses the default `combined` format, which logs $request —
// as well as browser history and Referer.
//
// Nothing is broken by being strict from the start: until this middleware
// existed the SSE transport took no credential at all in this posture, so there
// is no client anywhere that is passing this token in a URL today.
func sseTokenMiddleware(apiToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if len(h) <= 7 || h[:7] != "Bearer " || h[7:] != apiToken {
			http.Error(w, `{"error":"invalid or missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sseLocalOnlyMiddleware is the no-credential case: the transport answers the
// machine it runs on and nobody else.
func sseLocalOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.LocalRequest(r) {
			http.Error(w, `{"error":"the write API is disabled: set api_token or auth_enabled to accept writes from another machine"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sseAuthMiddleware extracts bearer token from SSE requests and injects user into context.
// Returns WWW-Authenticate header on 401 to enable OAuth discovery per MCP spec.
func sseAuthMiddleware(authSvc *service.AuthService, baseURL string, next http.Handler) http.Handler {
	resourceMetadataURL := baseURL + "/.well-known/oauth-protected-resource"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("sse request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "has_auth", r.Header.Get("Authorization") != "")

		token := ""
		if authHeader := r.Header.Get("Authorization"); len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token = authHeader[7:]
		}
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if token == "" {
			slog.Info("sse auth: no token", "path", r.URL.Path)
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s"`, resourceMetadataURL))
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		user, err := authSvc.ValidateToken(r.Context(), token)
		if err != nil || user == nil {
			slog.Info("sse auth: invalid token", "path", r.URL.Path, "error", err)
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s"`, resourceMetadataURL))
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		ctx := auth.WithUser(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
