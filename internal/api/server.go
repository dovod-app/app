package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dovod-app/app/internal/api/handlers"
	"github.com/dovod-app/app/internal/api/ws"
	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
	"github.com/uptrace/bun"
)

type Server struct {
	mux *http.ServeMux
	// router is kept for the drift test: it is the list of every pattern that
	// reached the mux, which is what the OpenAPI document is checked against.
	router *router
	hub    *ws.Hub
	port   int
	log    *slog.Logger
}

type ServerConfig struct {
	Port           int
	IsInMemory     bool
	APIToken       string
	AuthEnabled    bool
	BaseURL        string // Public base URL for OAuth metadata (e.g. https://mcp.example.com)
	OAuthSvc       *service.OAuthService
	AutoLoginToken string       // JWT for default user auto-login (empty = disabled)
	MCPHandler     http.Handler // Streamable HTTP MCP handler (mounted at /mcp)
	// Version is what the binary was built as. It reaches the web UI through
	// /api/health, which is the one endpoint every page can already call
	// without a credential.
	Version string
}

func NewServer(
	cfg ServerConfig,
	researchSvc *service.ResearchService,
	sectionSvc *service.SectionService,
	entrySvc *service.EntryService,
	sessionSvc *service.SessionService,
	taskSvc *service.TaskService,
	roadmapSvc *service.RoadmapService,
	exportSvc *service.ExportService,
	obsidianSvc *service.ObsidianService,
	teamSvc *service.TeamService,
	shareSvc *service.ShareService,
	skillSvc *service.SkillService,
	templateSvc *service.TemplateService,
	annotationSvc *service.AnnotationService,
	access *service.Access,
	authSvc *service.AuthService, // nil when auth disabled
	db *bun.DB,
	entryRepo *storage.EntryRepository,
	researchRepo *storage.ResearchRepository,
	crossrefRepo *storage.CrossRefRepository,
	externalLinkRepo *storage.ExternalLinkRepository,
	hub *ws.Hub,
	log *slog.Logger,
) *Server {
	mux := http.NewServeMux()

	rh := handlers.NewResearchHandler(researchSvc, sectionSvc, entrySvc, entryRepo, sessionSvc, log)
	eh := handlers.NewEntryHandler(entrySvc, researchSvc, entryRepo, researchRepo, storage.NewUserRepository(db), storage.NewTeamRepository(db), log)
	entryViewSvc := service.NewEntryViewService(storage.NewEntryViewRepository(db), entryRepo, access, ws.NewHubNotifier(hub))
	eh.SetEntryViewService(entryViewSvc)
	evh := handlers.NewEntryViewHandler(entryViewSvc, researchSvc)
	sh := handlers.NewSessionHandler(sessionSvc, entrySvc, researchSvc, log)
	th := handlers.NewTaskHandler(taskSvc, researchSvc, log)
	rmh := handlers.NewRoadmapHandler(roadmapSvc, researchSvc, log)
	tmh := handlers.NewTeamHandler(teamSvc, researchSvc, log)
	shh := handlers.NewShareHandler(shareSvc, researchSvc, sectionSvc, log)
	skh := handlers.NewSkillHandler(skillSvc, researchSvc, log)
	tph := handlers.NewTemplateHandler(templateSvc, researchSvc, sectionSvc, skillSvc, log)
	anh := handlers.NewAnnotationHandler(annotationSvc, researchSvc, storage.NewUserRepository(db), storage.NewTeamRepository(db), log)
	// Built here from the repositories rather than passed in: the summary owns
	// no entity, and threading a tenth service through this signature (and
	// every test fixture that calls it) would buy nothing.
	rsh := handlers.NewResumeHandler(service.NewResumeService(researchSvc,
		storage.NewSessionRepository(db), storage.NewTaskRepository(db),
		storage.NewQuestionRepository(db), storage.NewAnnotationRepository(db),
		entryRepo, storage.NewEntryRevisionRepository(db), access, log))
	rh.SetShareService(shareSvc)

	// Build auth middleware functions
	var requireAuth func(http.Handler) http.Handler
	var optionalAuth func(http.Handler) http.Handler

	if cfg.AuthEnabled && authSvc != nil {
		validator := &serviceTokenValidator{authSvc: authSvc}
		requireAuth = auth.RequireAuth(validator)
		optionalAuth = auth.OptionalAuth(validator)
	}

	// markAuthor records who is writing, for the revision history an entry write
	// leaves behind. The credential is the evidence: a JWT is a browser session,
	// so a person is typing; an API key, an OAuth token or the legacy write token
	// is a machine. With no credential at all this is a local run with auth off,
	// where the only thing on this port is the web UI.
	//
	// MCP needs no equivalent — service.AuthorFromContext defaults to agent,
	// which is what every MCP write is.
	markAuthor := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			kind := domain.AuthorHuman
			if token := extractBearerToken(r); token != "" {
				if authSvc == nil || !authSvc.IsSessionToken(token) {
					kind = domain.AuthorAgent
				}
			}
			ctx := service.WithAuthor(r.Context(), kind)
			// Which tab is writing, so the event this produces can be
			// recognised by the tab that caused it and ignored there. Opaque to
			// the server; absent for every non-browser caller.
			ctx = service.WithClientID(ctx, r.Header.Get("X-Client-Id"))
			h.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	// localOnly is the write gate for an instance with no credential configured
	// at all.
	//
	// The config table promises that an unset `api_token` means the write API is
	// disabled. It was not: `wrap` fell through to no auth, so a server started
	// with the documented defaults accepted researches, sections, documents and
	// share links from anyone who could reach the port — while `/api/health`
	// reported `write_api: false` to the operator checking whether it was safe
	// to expose.
	//
	// Refusing outright would have been the literal reading, and it would have
	// deleted the local single-binary mode: the browser never holds an
	// api_token, so with no accounts there is no credential a page can present,
	// and the web UI would have become permanently read-only for the one person
	// that mode exists for. The loopback exemption closes the hole the promise
	// is about — a reachable port — and leaves that flow alone.
	localOnly := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !auth.LocalRequest(r) {
				// "from another machine" was the first wording and it was wrong
				// for the case that hits people hardest: inside a container the
				// operator's own browser arrives from the bridge gateway, so
				// they are told their request came from somewhere it did not.
				// Say what the rule is instead of guessing where they are.
				//
				// Both settings are named, and which one they want depends on
				// what is calling: a browser cannot present an api_token.
				log.Warn("write refused: this server accepts writes from its own machine only",
					"method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "host", r.Host)
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": "the write API is disabled: this server accepts changes only from a client on the machine it runs on. Set api_token and send it as a bearer token for programmatic writes, or auth_enabled to sign in from a browser. Note that neither setting protects reads; only auth_enabled does.",
				})
				return
			}
			h.ServeHTTP(w, r)
		})
	}

	// wrap applies auth to endpoints:
	// - auth_enabled: user-based auth
	// - api_token set: legacy bearer token
	// - neither: local callers only, per localOnly above
	wrap := func(h http.Handler) http.Handler {
		if requireAuth != nil {
			return markAuthor(requireAuth(h))
		}
		if cfg.APIToken != "" {
			return markAuthor(bearerAuth(cfg.APIToken)(h))
		}
		return markAuthor(localOnly(h))
	}

	// wrapRead applies optional auth to read endpoints (user scoping when auth enabled)
	wrapRead := func(h http.Handler) http.Handler {
		if requireAuth != nil {
			return requireAuth(h)
		}
		return h
	}

	// wrapOperator serves the routes that can carry either kind of caller: a
	// person editing their team's template, or whoever runs this server writing
	// one every team will see.
	//
	// It has to be its own wrapper because the two credentials are checked by
	// different code. `wrap` sends everything through requireAuth once
	// auth_enabled is on, and requireAuth rejects the api_token — it is not a
	// JWT and not an API key, so the operator would get a 401 on a route that
	// exists for them. Here the api_token is recognised first and skips the user
	// check entirely, which is correct: the operator is not a user, has no team
	// and is in nobody's member list.
	//
	// It authorises nothing on its own. All it does is stamp the context; the
	// refusals live in TemplateService, so a second entry point cannot arrive
	// without one.
	asOperator := func(kind auth.OperatorKind, h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r.WithContext(auth.WithOperator(r.Context(), kind)))
		})
	}
	wrapOperator := func(h http.Handler) http.Handler {
		byToken := markAuthor(asOperator(auth.OperatorByToken, h))
		if requireAuth != nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if operatorCredential(cfg.APIToken, r) {
					byToken.ServeHTTP(w, r)
					return
				}
				// Not the operator: an ordinary authenticated caller, who may
				// still edit their own team's templates.
				markAuthor(requireAuth(h)).ServeHTTP(w, r)
			})
		}
		if cfg.APIToken != "" {
			return bearerAuth(cfg.APIToken)(byToken)
		}
		// Neither a token nor accounts: a local run, where there is no boundary
		// for the operator to prove themselves across. Refusing a local caller
		// here would only mean the feature cannot be tried without first
		// inventing a credential. A different kind, though — the token narrows
		// what its holder can see, and doing that here would hide a local
		// user's own team templates from them.
		//
		// `localOnly` still applies: this is a write path, and "no boundary"
		// describes the operator's own machine, not the network.
		return markAuthor(localOnly(asOperator(auth.OperatorNoBoundary, h)))
	}

	// wrapReadOperator is the read side of the same story. Without it the
	// operator could write a global template and never read one back:
	// `wrapRead` sends everything through requireAuth, which rejects the
	// api_token — so the only id they would ever hold is the one their own
	// create call returned, and `PUT`/`DELETE` need an id.
	//
	// What they see is narrowed to the global tier by TemplateService, because
	// the token proves who runs the server and not membership of any team.
	wrapReadOperator := func(h http.Handler) http.Handler {
		if requireAuth == nil {
			return wrapRead(h)
		}
		byToken := asOperator(auth.OperatorByToken, h)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if operatorCredential(cfg.APIToken, r) {
				byToken.ServeHTTP(w, r)
				return
			}
			requireAuth(h).ServeHTTP(w, r)
		})
	}

	// wrapOptional attaches a session when the request carries one and lets it
	// through when it does not. Only the invitation preview uses it: someone
	// following a link has no account yet and still has to be told what they
	// are being invited to — but if they are signed in, the answer says more.
	wrapOptional := func(h http.Handler) http.Handler {
		if optionalAuth != nil {
			return optionalAuth(h)
		}
		return h
	}

	// Every route below is registered through this. It puts the handler on the
	// mux behind the wrapper its access kind names, and puts the operation in
	// the OpenAPI document at the same moment — so the document cannot describe
	// a route that is not served, and openapi_drift_test.go fails if a route is
	// served without being described.
	rt := newRouter(routerConfig{
		Mux: mux,
		// Deliberately cfg.BaseURL and not a localhost fallback. An instance
		// deployed without `base_url` used to publish `http://localhost:8088`
		// as its server, so every consumer of the document — a codegen run, a
		// Postman import, a browsable reference's "try it" — aimed at the
		// reader's own machine. Empty means the router writes a relative
		// server instead, which is correct everywhere.
		BaseURL:     cfg.BaseURL,
		Version:     cfg.Version,
		AuthEnabled: cfg.AuthEnabled,
		APIToken:    cfg.APIToken != "",
		Wrappers: map[accessKind]func(http.Handler) http.Handler{
			accessRead:          wrapRead,
			accessWrite:         wrap,
			accessOperatorRead:  wrapReadOperator,
			accessOperatorWrite: wrapOperator,
			accessOptional:      wrapOptional,
		},
	})

	// Schemas for the entities this API returns, generated from the domain
	// structs the handlers serialise. Naming them once here is what keeps the
	// registrations below to one line of schema each.
	sResearch := rt.schemaOf(domain.Research{}, "Research")
	sMemoryItem := rt.schemaOf(domain.MemoryItem{}, "MemoryItem")
	sSection := rt.schemaOf(domain.Section{}, "Section")
	sEntry := rt.schemaOf(domain.Entry{}, "Entry")
	sSession := rt.schemaOf(domain.Session{}, "Session")
	sQuestion := rt.schemaOf(domain.Question{}, "Question")
	sTask := rt.schemaOf(domain.Task{}, "Task")
	sRoadmap := rt.schemaOf(domain.Roadmap{}, "Roadmap")
	sRoadmapNode := rt.schemaOf(domain.RoadmapNode{}, "RoadmapNode")
	sTeam := rt.schemaOf(domain.Team{}, "Team")
	sTeamMember := rt.schemaOf(domain.TeamMember{}, "TeamMember")
	sTeamInvite := rt.schemaOf(domain.TeamInvite{}, "TeamInvite")
	sAnnotation := rt.schemaOf(domain.Annotation{}, "Annotation")
	sSkill := rt.schemaOf(domain.Skill{}, "Skill")
	sTemplate := rt.schemaOf(domain.Template{}, "Template")
	sShare := rt.schemaOf(domain.Share{}, "Share")
	sUser := rt.schemaOf(domain.User{}, "User")
	sAPIKey := rt.schemaOf(domain.APIKey{}, "APIKey")
	sCrossRef := rt.schemaOf(domain.CrossRef{}, "CrossRef")
	sExternalLink := rt.schemaOf(domain.ExternalLink{}, "ExternalLink")
	sRevision := rt.schemaOf(domain.EntryRevision{}, "EntryRevision")
	sEntryUpdate := rt.schemaOf(domain.EntryUpdate{}, "EntryUpdate")
	sResume := rt.schemaOf(domain.ResearchResume{}, "ResearchResume")
	sExport := rt.schemaOf(domain.ExportData{}, "ExportData")
	sMetadataReport := rt.schemaOf(domain.FieldSchemaInfo{}, "FieldSchemaInfo")
	// Request bodies, generated from the very structs the handlers decode into.
	// Writing these by hand is what produced a document that named `type` for
	// `entry_type` and invented a `description` on a task update.
	sCreateResearch := rt.schemaOf(handlers.CreateResearchRequest{}, "CreateResearchRequest")
	sUpdateResearch := rt.schemaOf(handlers.UpdateResearchRequest{}, "UpdateResearchRequest")
	sAddMemory := rt.schemaOf(handlers.AddMemoryRequest{}, "AddMemoryRequest")
	sUpdateMemory := rt.schemaOf(handlers.UpdateMemoryRequest{}, "UpdateMemoryRequest")
	sBulkDeleteMemory := rt.schemaOf(handlers.BulkDeleteMemoryRequest{}, "BulkDeleteMemoryRequest")
	sCreateSection := rt.schemaOf(handlers.CreateSectionRequest{}, "CreateSectionRequest")
	sUpdateSection := rt.schemaOf(handlers.UpdateSectionRequest{}, "UpdateSectionRequest")
	sCreateEntry := rt.schemaOf(handlers.CreateEntryRequest{}, "CreateEntryRequest")
	sUpdateEntry := rt.schemaOf(handlers.UpdateEntryRequest{}, "UpdateEntryRequest")
	sPatchEntry := rt.schemaOf(handlers.PatchEntryRequest{}, "PatchEntryRequest")
	sCreateTask := rt.schemaOf(handlers.CreateTaskRequest{}, "CreateTaskRequest")
	sUpdateTask := rt.schemaOf(handlers.UpdateTaskRequest{}, "UpdateTaskRequest")
	sCreateSession := rt.schemaOf(handlers.CreateSessionRequest{}, "CreateSessionRequest")
	sUpdateSession := rt.schemaOf(handlers.UpdateSessionRequest{}, "UpdateSessionRequest")
	sUpdateQuestion := rt.schemaOf(handlers.UpdateQuestionRequest{}, "UpdateQuestionRequest")
	sAddQuestions := rt.schemaOf(handlers.AddQuestionsRequest{}, "AddQuestionsRequest")
	sTemplateBody := rt.schemaOf(handlers.TemplateRequest{}, "TemplateRequest")

	// Every list route answers `{data: [...], count: n}`. Declaring the count
	// once is what stopped sixteen operations from each omitting it.
	list := func(item *huma.Schema) *huma.Schema {
		return envelope(map[string]*huma.Schema{
			"data":  listOf(item),
			"count": {Type: "integer", Description: "How many entries are in `data`."},
		})
	}

	sOK := envelope(map[string]*huma.Schema{
		"status": {Type: "string", Description: "`ok` when the change was applied."},
	})
	// Most deletes answer `{"deleted": true}` rather than a status.
	sDeleted := envelope(map[string]*huma.Schema{
		"deleted": {Type: "boolean"},
	})
	sDeletionSummary := envelope(map[string]*huma.Schema{
		"sections":            {Type: "integer"},
		"entries":             {Type: "integer"},
		"sessions":            {Type: "integer"},
		"questions":           {Type: "integer"},
		"tasks":               {Type: "integer"},
		"roadmaps":            {Type: "integer"},
		"annotations":         {Type: "integer"},
		"memory":              {Type: "integer", Description: "Memory notes — the agent's working record of how the project has been conducted."},
		"shares":              {Type: "integer", Description: "Live share links, which stop working immediately."},
		"incoming_refs":       {Type: "integer", Description: "References from other projects into this one. **Not deleted** — they survive as the text that was written and stop resolving, because removing them would edit a project nobody asked to change."},
		"incoming_from_total": {Type: "integer", Description: "How many projects cite this one in total, before the cap on the list below — so a client can say \"and 79 others\" rather than \"and 9 others\"."},
		"incoming_from": {Type: "array", Description: "Which projects those references are in, capped at ten. The count is the decision; these are so it is not an abstraction.",
			Items: &huma.Schema{Type: "object", Properties: map[string]*huma.Schema{
				"code": {Type: "string"},
				"name": {Type: "string"},
			}}},
	})

	// authInfo answers "does this instance have accounts" — the first thing the
	// web UI asks, before it has anything to ask it with. It is registered
	// below, outside the block, because the honest answer when auth is off is
	// `false` and not a missing route: with the route inside the block the
	// catch-all served the SPA's own HTML with a 200, the composable read a
	// successful fetch as "accounts are on", and every page redirected to a
	// login screen that could not be used because /api/auth/login did not exist
	// either. Auth off made the whole web UI unreachable.
	authInfo := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"auth_enabled":       false,
			"allow_registration": false,
		})
	}

	// --- Auth endpoints (only when auth enabled) ---
	if cfg.AuthEnabled && authSvc != nil {
		ah := handlers.NewAuthHandler(authSvc, teamSvc, cfg.AutoLoginToken, log)
		authInfo = ah.AuthInfo

		sSession := envelope(map[string]*huma.Schema{
			"user":  sUser,
			"token": {Type: "string", Description: "A JWT. Present it as `Authorization: Bearer <token>` on every other route."},
		}, "user", "token")
		sRegistered := envelope(map[string]*huma.Schema{
			"user":  sUser,
			"token": {Type: "string"},
			"joined": envelope(map[string]*huma.Schema{
				"team_id":   {Type: "string"},
				"team_name": {Type: "string"},
				"role":      {Type: "string"},
			}),
		}, "user", "token")

		rt.route(accessPublic, op("POST", "/api/auth/register", "Register an account",
			"Creates a user and the personal team their researches will live in, and returns a session token.\n\n"+
				"Refused with 403 when `allow_registration` is off. The **first** account registered on a fresh server also adopts any researches that already exist without an owner — which is how an instance that ran without auth is migrated onto it.").
			tag("Auth").
			body("Email, password and display name.", envelope(map[string]*huma.Schema{
				"email":    {Type: "string", Format: "email"},
				"password": {Type: "string", MinLength: ptrInt(8), Description: "At least 8 characters."},
				"name":     {Type: "string"},
			}, "email", "password")).
			returns("201", "The new account and its session token. `joined` is present when the registration came from an invitation and names the team that was joined.", sRegistered).
			responds("403", "Registration is disabled on this instance.").
			responds("409", "That email already has an account.").
			build(), ah.Register)

		rt.route(accessPublic, op("POST", "/api/auth/login", "Sign in",
			"Exchanges an email and password for a JWT. The same token authenticates the REST API, the WebSocket and the MCP endpoint.").
			tag("Auth").
			body("Email and password.", envelope(map[string]*huma.Schema{
				"email":    {Type: "string", Format: "email"},
				"password": {Type: "string"},
			}, "email", "password")).
			returns("200", "The account and its session token.", sSession).
			responds("401", "Wrong email or password. The two are not told apart.").
			build(), ah.Login)

		rt.route(accessRead, op("GET", "/api/auth/me", "The signed-in account",
			"Who the presented credential belongs to, and the teams they are a member of.").
			tag("Auth").
			returns("200", "The account behind the credential, as a bare object — this route does not use the `data` envelope.", sUser).
			build(), ah.Me)

		rt.route(accessWrite, op("POST", "/api/auth/api-keys", "Create an API key",
			"Mints a long-lived credential for a script or an MCP client.\n\n"+
				"**The key itself is returned once, here, and is not recoverable.** Only its hash is stored. A write carrying an API key is recorded as made by an agent rather than a person, which is what the revision history shows.").
			tag("Auth").
			body("A name to recognise the key by later.", envelope(map[string]*huma.Schema{
				"name": {Type: "string"},
			}, "name")).
			returns("201", "The key, shown for the only time.", envelope(map[string]*huma.Schema{
				"key":        {Type: "string", Description: "The credential itself. Store it now; only its hash is kept."},
				"id":         {Type: "string"},
				"name":       {Type: "string"},
				"prefix":     {Type: "string", Description: "The first characters, so a key can be recognised in a list."},
				"created_at": {Type: "string", Format: "date-time"},
			}, "key", "id")).
			build(), ah.CreateAPIKey)

		rt.route(accessRead, op("GET", "/api/auth/api-keys", "List API keys",
			"The caller's keys, by name and prefix and when each was last used. The keys themselves are not stored and cannot be listed.").
			tag("Auth").
			returns("200", "The caller's keys, as a bare array — this route does not use the `data` envelope.", listOf(sAPIKey)).
			build(), ah.ListAPIKeys)

		rt.route(accessWrite, op("DELETE", "/api/auth/api-keys/{id}", "Revoke an API key",
			"The key stops working immediately, including on WebSocket connections it already opened — those are re-checked about once a minute and closed with code 4401.").
			tag("Auth").
			returns("200", "Revoked.", sOK).
			build(), ah.DeleteAPIKey)

	}

	rt.route(accessPublic, op("GET", "/api/auth/info", "Authentication settings",
		"What this instance expects of a client before it has one: whether accounts are on, whether registration is open, and — for a local `default_user` run — a token to log in with automatically.\n\n"+
			"This route answers whether or not accounts are configured. A client cannot ask \"are there accounts here\" only where there are.").
		tag("Auth").
		returns("200", "How this instance authenticates.", envelope(map[string]*huma.Schema{
			"auth_enabled":       {Type: "boolean"},
			"allow_registration": {Type: "boolean"},
			"auto_login_token":   {Type: "string", Description: "Present only when `default_user` is configured **and** the request came from the machine the server runs on; a JWT for that user."},
		})).
		build(), authInfo)

	// --- OAuth2 endpoints (only when auth enabled) ---
	if cfg.AuthEnabled && cfg.OAuthSvc != nil && authSvc != nil {
		oh := handlers.NewOAuthHandler(cfg.OAuthSvc, authSvc, log)

		// The OAuth metadata documents must carry absolute URLs — a client
		// reads them before it has anything to resolve a relative one against
		// — so they still fall back to localhost when nothing is configured.
		// The OpenAPI document does not; it publishes a relative server, which
		// is why the two differ here.
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = fmt.Sprintf("http://localhost:%d", cfg.Port)
		}

		sTokens := envelope(map[string]*huma.Schema{
			"access_token":  {Type: "string", Description: "Present as `Authorization: Bearer <token>`."},
			"refresh_token": {Type: "string", Description: "Redeem at this same endpoint with `grant_type=refresh_token`. Rotated on every use."},
			"token_type":    {Type: "string", Description: "Always `Bearer`."},
			"expires_in":    {Type: "integer", Description: "Seconds the access token remains valid."},
		}, "access_token", "token_type", "expires_in")

		// Standard OAuth2 paths (used by ChatGPT and other MCP clients)
		authorizeDoc := "The consent page. A `GET` renders the sign-in form; the form posts back to the same URL, " +
			"which is why the PKCE and `state` parameters are read from the query string on both methods.\n\n" +
			"On success the browser is redirected to the client's registered `redirect_uri` with `code` and `state`. " +
			"An unregistered `redirect_uri`, an unknown `client_id` or a wrong password all render the page again and " +
			"redirect nowhere — aiming an authorization code at a host of the caller's choosing is the attack this refuses."
		for _, method := range []string{"GET", "POST"} {
			rt.route(accessPublic, op(method, "/auth/authorize", "Authorization endpoint", authorizeDoc).
				tag("OAuth2").
				query("client_id", "The client, as issued by Dynamic Client Registration.").
				query("redirect_uri", "Where to send the code. Must match one the client registered, exactly.").
				query("response_type", "`code`. Nothing else is supported.").
				query("scope", "Requested scope, recorded with the grant.").
				query("state", "Opaque value echoed back on the redirect, so a client can match its own request.").
				query("code_challenge", "PKCE challenge (RFC 7636).").
				query("code_challenge_method", "`S256`. `plain` is accepted for a challenge stored that way; anything else is refused.").
				returnsFile("The sign-in page, or the page again with an error on it.", "text/html").
				responds("302", "Signed in: the browser is sent to the client's redirect_uri with the code.").
				build(), oh.Authorize)
		}

		rt.route(accessPublic, op("POST", "/auth/token", "Token endpoint",
			"Trades an authorization code for tokens, or a refresh token for a new pair.\n\n"+
				"Accepts a form body, a JSON body, or HTTP Basic for the client credentials — clients differ and all three are in use. "+
				"An authorization code is single-use: the second presentation is refused, and so is the first if another request won the race.\n\n"+
				"Access tokens last an hour. `grant_type=refresh_token` renews them and rotates the refresh token, so a refresh token that has already been spent is dead.").
			tag("OAuth2").
			bodyForm("`grant_type` plus the fields that grant needs. Accepted as a form or as JSON; the client credentials may instead be sent as HTTP Basic.", envelope(map[string]*huma.Schema{
				"grant_type":    {Type: "string", Description: "`authorization_code` or `refresh_token`."},
				"code":          {Type: "string", Description: "The authorization code. Required for `authorization_code`."},
				"refresh_token": {Type: "string", Description: "Required for `refresh_token`."},
				"client_id":     {Type: "string"},
				"client_secret": {Type: "string", Description: "May instead be sent as HTTP Basic."},
				"redirect_uri":  {Type: "string", Description: "The same one the code was issued for."},
				"code_verifier": {Type: "string", Description: "PKCE verifier. Required when the authorization carried a challenge."},
			}, "grant_type")).
			returns("200", "A fresh token pair.", sTokens).
			build(), oh.Token)

		// RFC 7591 Dynamic Client Registration
		rt.route(accessPublic, op("POST", "/auth/register", "Register a client (RFC 7591)",
			"Open Dynamic Client Registration: an external client creates its own credentials without anybody configuring them by hand. This is how ChatGPT and Claude.ai set themselves up.\n\n"+
				"A client registered this way belongs to nobody — it is not in any person's client list, and it grants nothing until somebody signs in on the consent page.").
			tag("OAuth2").
			body("The client's name and its redirect URIs.", envelope(map[string]*huma.Schema{
				"client_name":                {Type: "string"},
				"redirect_uris":              listOf(&huma.Schema{Type: "string"}),
				"grant_types":                listOf(&huma.Schema{Type: "string"}),
				"response_types":             listOf(&huma.Schema{Type: "string"}),
				"token_endpoint_auth_method": {Type: "string"},
			}, "redirect_uris")).
			returns("201", "The client, with its secret shown once.", envelope(map[string]*huma.Schema{
				"client_id":                  {Type: "string"},
				"client_secret":              {Type: "string"},
				"client_name":                {Type: "string"},
				"redirect_uris":              listOf(&huma.Schema{Type: "string"}),
				"grant_types":                listOf(&huma.Schema{Type: "string"}),
				"response_types":             listOf(&huma.Schema{Type: "string"}),
				"token_endpoint_auth_method": {Type: "string"},
			})).
			build(), oh.RegisterClient)

		// OAuth2 Authorization Server Metadata (RFC 8414)
		metadataDoc := "Where the OAuth2 endpoints are. A client reads this before it knows anything else about the server; the URLs in it are built from `base_url`, so an instance behind a proxy must set that or clients will be sent to localhost."
		for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
			rt.route(accessPublic, op("GET", path, "Authorization server metadata (RFC 8414)", metadataDoc).
				tag("OAuth2").
				returns("200", "Endpoint locations and supported grants.", envelope(map[string]*huma.Schema{
					"issuer":                                {Type: "string"},
					"authorization_endpoint":                {Type: "string"},
					"token_endpoint":                        {Type: "string"},
					"registration_endpoint":                 {Type: "string"},
					"grant_types_supported":                 listOf(&huma.Schema{Type: "string"}),
					"response_types_supported":              listOf(&huma.Schema{Type: "string"}),
					"code_challenge_methods_supported":      listOf(&huma.Schema{Type: "string"}),
					"token_endpoint_auth_methods_supported": listOf(&huma.Schema{Type: "string"}),
					"scopes_supported":                      listOf(&huma.Schema{Type: "string"}),
				})).
				build(), handlers.OAuthMetadataHandler(baseURL))
		}

		// OAuth2 Protected Resource Metadata (RFC 9728)
		rt.route(accessPublic, op("GET", "/.well-known/oauth-protected-resource", "Protected resource metadata (RFC 9728)",
			"Tells an MCP client which authorization server protects the MCP endpoint. It is the first document fetched after the `401` that starts the flow.").
			tag("OAuth2").
			returns("200", "The protected resource and its authorization server.", envelope(map[string]*huma.Schema{
				"resource":              {Type: "string"},
				"authorization_servers": listOf(&huma.Schema{Type: "string"}),
				"scopes_supported":      listOf(&huma.Schema{Type: "string"}),
			})).
			build(), handlers.OAuthProtectedResourceHandler(baseURL))
	}

	// --- Read endpoints ---
	rt.route(accessRead, op("GET", "/api/researches", "List researches",
		"Every research the caller can reach — which is every research owned by a team they belong to, not only the ones they created. A colleague's work in a shared team is theirs to see.").
		tag("Research").
		query("status", "Filter by status: `active`, `completed` or `archived`.").
		query("team", "Only researches owned by this team.").
		returns("200", "The researches the caller can reach.", list(sResearch)).
		build(), rh.List)

	rt.route(accessRead, op("GET", "/api/researches/{id}", "Get a research",
		"The research with its sections, and the active session if one is running.\n\n"+
			"Through a share link the memory and the team fields are stripped: they are working process, not the result.").
		tag("Research").
		returns("200", "The research, its sections with their document counts, and the newest session.", envelope(map[string]*huma.Schema{
			"data": envelope(map[string]*huma.Schema{
				"research": sResearch,
				// $ref rather than a bare object: `section_id` is required by the
				// next call a reader makes, and with an untyped array nothing on
				// the page said a section even has an id. The journey dead-ended
				// exactly here.
				"sections": listOf(&huma.Schema{
					Description: "A section, plus how many documents are filed under it.",
					AllOf: []*huma.Schema{
						sSection,
						{Type: "object", Properties: map[string]*huma.Schema{
							"entries_count": {Type: "integer"},
						}},
					},
				}),
				"active_session":     {Type: "object", Description: "The newest session, or null. Absent through a share link whose `sessions` flag is off — this route is not gated by the flags, so this part gates itself."},
				"active_share_count": {Type: "integer", Description: "How many links are handing this research out. Zero for anyone who could not manage them anyway, a share visitor included."},
			}),
		})).
		build(), rh.Get)

	rt.route(accessRead, op("GET", "/api/researches/{id}/sections/{sectionId}/entries", "List a section's documents",
		"The documents filed under one section, newest first.").
		tag("Documents").
		query("tag", "Only documents carrying this tag.").
		returns("200", "The section's documents.", list(sEntry)).
		build(), rh.ListSectionEntries)

	rt.route(accessRead, op("GET", "/api/researches/{id}/entries", "List every document",
		"Every document in the research, across all sections.").
		tag("Documents").
		query("tag", "Only documents carrying this tag.").
		query("session", "Only documents produced by this interview session.").
		returns("200", "The research's documents.", list(sEntry)).
		build(), rh.ListAllEntries)

	rt.route(accessRead, op("GET", "/api/researches/{id}/updates", "Documents changed since the caller last looked",
		"What has moved in this research that this particular caller has not seen. Read state is per person, so two members of a team see different answers.").
		tag("Documents").
		returns("200", "The unseen changes.", list(sEntryUpdate)).
		build(), evh.List)

	rt.route(accessRead, op("GET", "/api/researches/{id}/tags", "List tags",
		"Every tag used in the research, with how many documents carry each.").
		tag("Documents").
		returns("200", "Tags and how many documents carry each.", envelope(map[string]*huma.Schema{
			"data": listOf(envelope(map[string]*huma.Schema{
				"tag":   {Type: "string"},
				"count": {Type: "integer"},
			})),
			"count": {Type: "integer", Description: "How many distinct tags."},
		})).
		build(), rh.ListTags)

	// Deliberately not on the share sub-mux: the summary is working process.
	rt.route(accessRead, op("GET", "/api/researches/{id}/resume", "Where the work stopped",
		"A read-only summary answering \"where did we leave off\": the open tasks, the unanswered questions, the annotations waiting on a person, the documents that changed recently, and up to three suggested next actions.\n\n"+
			"Bounded on purpose — the payload is capped, and when it does not fit the previews are sacrificed before the queues, so a summary never reports four pending tasks and then lists none.\n\n"+
			"Not reachable through a share link: what is unfinished is working process.").
		tag("Research").
		queryInt("limit", "How many items per group, 1 to 15. Default 5.").
		query("session_id", "Which session to summarise, when the research has more than one running.").
		returns("200", "The summary.", envelope(map[string]*huma.Schema{"data": sResume})).
		build(), rsh.Get)

	// Constant data, but behind wrapRead all the same: it describes what this
	// installation lets a team record, which is not for anonymous readers.
	rt.route(accessRead, op("GET", "/api/metadata/schema", "Section field rules",
		"The rules a section's field declaration must follow: the type catalogue, the reserved keys and every cap. Constant for an installation, and read by anything that builds a section editor.").
		tag("Research").
		returns("200", "The field type catalogue and its limits.", list(sMetadataReport)).
		build(), rh.MetadataSchema)
	exportHandler := handlers.NewExportHandler(researchSvc, sectionSvc, entrySvc, entryRepo, sessionSvc, taskSvc, log)
	exportHandler.SetExportService(exportSvc)
	exportHandler.SetObsidianService(obsidianSvc)
	exportHandler.SetRoadmapService(roadmapSvc)
	rt.route(accessRead, op("GET", "/api/researches/{id}/export", "Export a research",
		"The whole research in one document. `format=markdown` gives a readable report; `format=obsidian` gives a zip of a linked vault, with `[[...]]` references intact.").
		tag("Export").
		query("format", "`json` (default), `markdown`, or `obsidian` for a zipped vault. `obsidian` answers `application/zip`, not JSON.").
		queryBool("sessions", "Vault only. The JSON and markdown forms always include the sessions.").
		queryBool("tasks", "Vault only. The JSON and markdown forms always include the tasks.").
		queryBool("roadmaps", "Vault only.").
		queryBool("html", "Vault only. Write `html` blocks out as HTML rather than dropping them.").
		queryBool("revisions", "Vault only. Include each document's history as notes.").
		returns("200", "The export. Not wrapped in `data` — the parts sit at the top level.", envelope(map[string]*huma.Schema{
			"research":      sResearch,
			"sections":      listOf(&huma.Schema{Type: "object", Description: "A section with its documents."}),
			"sessions":      listOf(sSession),
			"tasks":         listOf(sTask),
			"roadmap_count": {Type: "integer"},
			"markdown":      {Type: "string", Description: "The whole research as one readable document. Present for `format=markdown`."},
		})).
		returnsFile("A zipped Obsidian vault, when `format=obsidian`.", "application/zip").
		build(), exportHandler.Export)

	rt.route(accessRead, op("GET", "/api/researches/{id}/sessions/{sessionId}/export", "Export one session",
		"One interview session: its questions, its answers, and the documents it produced.").
		tag("Export").
		query("format", "`json` (default) or `markdown`.").
		returns("200", "The session export. Not wrapped in `data`.", envelope(map[string]*huma.Schema{
			"research":      sResearch,
			"session":       sSession,
			"questions":     listOf(sQuestion),
			"entries":       listOf(sEntry),
			"section_names": {Type: "object", Description: "Section id to display name, so the export reads without a second call."},
			"markdown":      {Type: "string", Description: "Present for `format=markdown`."},
		})).
		build(), exportHandler.ExportSession)

	importHandler := handlers.NewImportHandler(exportSvc, log)
	rt.route(accessRead, op("GET", "/api/researches/{id}/export/portable", "Export for re-import",
		"The same content as the JSON export, in the shape `POST /api/researches/import` reads back. This is the pair that moves a research between installations.").
		tag("Export").
		returns("200", "A portable dump, in the shape the import route reads back. Not wrapped in `data`.", envelope(map[string]*huma.Schema{
			"version":     {Type: "string", Description: "The dump format, so an older file can still be read."},
			"exported_at": {Type: "string", Format: "date-time"},
			"research":    sExport,
		})).
		build(), exportHandler.ExportPortable)
	// One document as a file. Not on the share sub-mux: whether a visitor may
	// take a document away is a separate decision, and it is not made here by
	// leaving the route off that list.
	rt.route(accessRead, op("GET", "/api/entries/{id}/markdown", "Download one document",
		"One document as a markdown file, with its metadata as front matter. Not reachable through a share link — whether a visitor may take a document away is a separate decision from whether they may read it.").
		tag("Export").
		returnsFile("The document as markdown.", "text/markdown").
		build(), exportHandler.EntryMarkdown)

	rt.route(accessWrite, op("POST", "/api/researches/import", "Import a research",
		"Creates a research from a portable dump, with new ids and freshly allocated short codes. Cross-references are rewritten to point at the new codes.").
		tag("Export").
		body("A portable export.", envelope(map[string]*huma.Schema{"data": sExport})).
		returns("201", "The research that was created, and anything the file carried that the import could not.", envelope(map[string]*huma.Schema{
			"status":      {Type: "string"},
			"research_id": {Type: "string"},
			"code":        {Type: "string"},
			"name":        {Type: "string"},
			// Absent when there is nothing to report. Present, each string names
			// one thing that did not survive — a section whose writing
			// instruction was over the 500-character limit arrives without one,
			// because it is dropped rather than truncated.
			"warnings": {Type: "array", Items: &huma.Schema{Type: "string"}},
		})).
		build(), importHandler.Import)
	// One markdown file into one section — the other half of the single-document
	// download, and a different act from the whole-research dump above. Both
	// writes, both resolved through Access on the research that owns the
	// section, and neither on the share sub-mux: a visitor reads.
	mdImportHandler := handlers.NewMarkdownImportHandler(entrySvc, log)
	rt.route(accessWrite, op("POST", "/api/sections/{id}/import/preview", "Preview a markdown import",
		"Reads a markdown file and reports what it would create, without creating it: how many documents, under what titles, and what would collide with something already there.").
		tag("Documents").
		body("The markdown to inspect.", envelope(map[string]*huma.Schema{
			"content": {Type: "string"},
		}, "content")).
		returns("200", "The documents the file would produce, and anything that would collide.", envelope(map[string]*huma.Schema{
			"data": {Type: "object", Description: "The parsed documents and any collisions."},
		})).
		build(), mdImportHandler.Preview)

	rt.route(accessWrite, op("POST", "/api/sections/{id}/import", "Import markdown into a section",
		"Applies what the preview described. The other half of the single-document download.").
		tag("Documents").
		body("The markdown to import.", envelope(map[string]*huma.Schema{
			"content": {Type: "string"},
		}, "content")).
		returns("201", "The document that was created.", envelope(map[string]*huma.Schema{"data": sEntry})).
		build(), mdImportHandler.Commit)

	rt.route(accessRead, op("GET", "/api/entries/{id}", "Get a document",
		"One document, with its blocks resolved. Provenance — who wrote it and from which session — is present for a member and absent through a share link.").
		tag("Documents").
		returns("200", "The document, with who wrote it and where the caller has got to in it.", envelope(map[string]*huma.Schema{
			"data":        sEntry,
			"author_kind": {Type: "string", Description: "`agent` or `human` — the credential that wrote the current revision. Never present through a share link."},
			"author_name": {Type: "string", Description: "Never present through a share link."},
			"revision":    {Type: "integer", Description: "The current revision number. This is the value a patch sends back as `rev`, and the one `PUT .../seen` acknowledges."},
			"revised_at":  {Type: "string", Format: "date-time"},
			"view_state":  {Type: "object", Description: "Where this caller has got to. Read state is per person, which is what drives the unseen-change badges."},
		})).
		build(), eh.Get)

	rt.route(accessRead, op("GET", "/api/researches/{id}/entries/by-code/{code}", "Resolve a document short code",
		"Turns `E3` into the document it names, within one research. This is what a `[[E3]]` reference resolves through.").
		tag("Documents").
		returns("200", "The document the code names.", envelope(map[string]*huma.Schema{"data": sEntry})).
		build(), eh.ResolveCode)

	rt.route(accessRead, op("GET", "/api/resolve/research/{code}", "Resolve a research short code",
		"Turns `R2` into the research it names. Every URL in the web UI is built from short codes, so this is how a link becomes a record.").
		tag("Research").
		returns("200", "The research the code names.", envelope(map[string]*huma.Schema{"data": sResearch})).
		build(), eh.ResolveResearchCode)
	crReadHandler := handlers.NewCrossRefHandler(crossrefRepo, entrySvc, researchSvc, access, log)
	crReadHandler.SetRoadmapService(roadmapSvc)
	rt.route(accessRead, op("GET", "/api/researches/{id}/crossrefs", "List cross-references",
		"Every `[[...]]` reference found in the research, as a graph of what points at what. Extracted on write; `POST .../crossrefs/rebuild` re-scans when they have gone stale.").
		tag("Cross-references").
		returns("200", "The references in this research.", list(sCrossRef)).
		build(), crReadHandler.ListForResearch)

	rt.route(accessRead, op("GET", "/api/entries/{id}/crossrefs", "One document's cross-references",
		"What this document points at, and what points back at it.").
		tag("Cross-references").
		returns("200", "References out of and into this document.", envelope(map[string]*huma.Schema{
			"outgoing": listOf(sCrossRef),
			"incoming": listOf(sCrossRef),
		})).
		build(), crReadHandler.GetForEntry)
	elHandler := handlers.NewExternalLinkHandler(externalLinkRepo, researchSvc, entrySvc, log)
	rt.route(accessRead, op("GET", "/api/researches/{id}/links", "List external links",
		"Every outside URL cited anywhere in the research, and which documents cite it.").
		tag("Cross-references").
		returns("200", "The external links. This route counts with `total` rather than `count`.", envelope(map[string]*huma.Schema{
			"data":  listOf(sExternalLink),
			"total": {Type: "integer"},
		})).
		build(), elHandler.ListByResearch)

	rt.route(accessRead, op("GET", "/api/entries/{id}/links", "One document's external links",
		"The outside URLs this document cites.").
		tag("Cross-references").
		returns("200", "The document's external links.", list(sExternalLink)).
		build(), elHandler.ListByEntry)

	rt.route(accessRead, op("GET", "/api/entries/{id}/related", "Documents related to this one",
		"What this document is connected to — by cross-reference, by shared tags, and by sitting in the same section.").
		tag("Documents").
		returns("200", "Related documents.", list(sEntry)).
		build(), eh.GetRelated)
	revh := handlers.NewRevisionHandler(entrySvc, sessionSvc, researchSvc, log)
	rt.route(accessRead, op("GET", "/api/entries/{id}/revisions", "A document's history",
		"Every revision of this document, newest first, each recording who wrote it — a person or an agent — and from which session. Never reachable through a share link: who edited what and when is working process.").
		tag("Revisions").
		queryBool("summary", "Omit each revision's content and return only what changed.").
		returns("200", "The revisions.", list(sRevision)).
		build(), revh.List)

	rt.route(accessRead, op("GET", "/api/entries/{id}/revisions/{revision}", "Read one revision",
		"The document as it stood at that revision.").
		tag("Revisions").
		returns("200", "The revision.", envelope(map[string]*huma.Schema{"data": sRevision})).
		build(), revh.Get)

	rt.route(accessRead, op("GET", "/api/entries/{id}/diff", "Compare two revisions",
		"What changed between two revisions of a document.").
		tag("Revisions").
		queryInt("from", "The earlier revision. Defaults to the one before `to`.").
		queryInt("to", "The later revision. Defaults to the current one.").
		returns("200", "The difference between them.", envelope(map[string]*huma.Schema{
			"data": {Type: "object", Description: "The change, block by block."},
		})).
		build(), revh.Diff)

	rt.route(accessWrite, op("POST", "/api/entries/{id}/revisions/{revision}/restore", "Restore a revision",
		"Puts an earlier revision back as the current content. This is itself a new revision, not a rewrite of history — nothing is lost.").
		tag("Revisions").
		returns("200", "The document, as restored.", envelope(map[string]*huma.Schema{"data": sEntry})).
		build(), revh.Restore)

	rt.route(accessRead, op("GET", "/api/sessions/{id}/changes", "What a session changed",
		"The documents an interview session created and modified, so a person can review a session's output as a unit.").
		tag("Revisions").
		returns("200", "What the session changed. `created` and `modified` are counts, not lists — the documents themselves are in `changes`.", envelope(map[string]*huma.Schema{
			"data": envelope(map[string]*huma.Schema{
				"session_id":    {Type: "string"},
				"session_code":  {Type: "string"},
				"session_title": {Type: "string"},
				"created":       {Type: "integer"},
				"modified":      {Type: "integer"},
				"changes":       listOf(&huma.Schema{Type: "object", Description: "One document, with whether this session created it and what changed."}),
			}),
		})).
		build(), revh.SessionChanges)
	rt.route(accessRead, op("GET", "/api/search", "Search documents",
		"Full-text search across the documents the caller can reach. Scoping to one research is the only way to find anything in a large one: tags are the sole in-research filter, and they exist only where an agent applied them.").
		tag("Documents").
		query("q", "The query. Fewer than two characters returns nothing rather than everything.").
		query("research", "Restrict the search to one research, by id or short code (`R2`). A research the caller cannot reach returns no matches rather than a refusal.").
		returns("200", "Up to 20 matching documents.", envelope(map[string]*huma.Schema{
			"entries": listOf(sEntry),
		})).
		build(), func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if len(q) < 2 {
			writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}, "researches": []any{}})
			return
		}
		// `research` scopes the search to one research, which is the only way
		// to find anything in a large one: tags are the sole in-research filter
		// and they exist only if the agent applied them.
		//
		// The value is resolved first, because every URL in the web UI is built
		// from short codes and a caller who has one in hand will send it. The
		// repository matches on the id column, so `research=R2` used to answer
		// 200 with nothing — which reads as "this research is empty" rather
		// than "you addressed it the wrong way", and is the worse of the two
		// wrong answers.
		scope := r.URL.Query().Get("research")
		if scope != "" {
			resolved, rerr := researchSvc.ResolveID(r.Context(), scope)
			if rerr != nil {
				// Unknown, or not the caller's. Either way there is nothing to
				// find, and saying which would be an oracle.
				writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}})
				return
			}
			scope = resolved
		}
		entries, err := entryRepo.SearchEntries(r.Context(), q, 20, auth.UserIDFromContext(r.Context()), scope)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		// A nil slice encodes as `null`, and a client that reads `.length` off
		// it crashes on the one case that is most likely: no matches.
		if entries == nil {
			entries = []*domain.Entry{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
	})
	rt.route(accessRead, op("GET", "/api/researches/{id}/tasks", "List tasks",
		"The research's task list — the todo list an agent keeps for itself while working.").
		tag("Tasks").
		query("status", "Filter by status: `pending`, `in_progress`, `completed`, `blocked`.").
		returns("200", "The tasks and how many are in each status.", envelope(map[string]*huma.Schema{
			"data":   listOf(sTask),
			"count":  {Type: "integer"},
			"counts": {Type: "object", Description: "How many tasks in each status."},
		})).
		build(), th.ListByResearch)

	rt.route(accessRead, op("GET", "/api/researches/{id}/sessions", "List sessions",
		"The interview sessions of this research. A research has several on purpose — an initial exploration, a deep dive, a follow-up — and each carries its own questions.").
		tag("Sessions").
		returns("200", "The sessions.", list(sSession)).
		build(), sh.ListByResearch)

	rt.route(accessRead, op("GET", "/api/researches/{id}/sessions/{sessionId}", "Get a session",
		"One session with its questions and their answers. Accepts the short code, e.g. `SS1`.").
		tag("Sessions").
		returns("200", "The session, its questions bucketed by status, and how far it has got.", envelope(map[string]*huma.Schema{
			"data": envelope(map[string]*huma.Schema{
				"session": sSession,
				"questions": {
					Type:                 "object",
					Description:          "Questions grouped by status — `pending`, `answered`, `deferred`, `skipped` — not a flat array.",
					AdditionalProperties: listOf(sQuestion),
				},
				"progress": envelope(map[string]*huma.Schema{
					"total":    {Type: "integer"},
					"answered": {Type: "integer"},
					"pending":  {Type: "integer"},
					"deferred": {Type: "integer"},
					"skipped":  {Type: "integer"},
				}),
				"entries": listOf(sEntry),
			}),
		})).
		build(), sh.Get)

	rt.route(accessRead, op("GET", "/api/researches/{id}/entries/{entryId}", "Get a document within a research",
		"The same document as `/api/entries/{id}`, addressed through the research that owns it — which is what a page built from short codes has to hand.").
		tag("Documents").
		returns("200", "The document.", envelope(map[string]*huma.Schema{"data": sEntry})).
		build(), eh.GetByResearch)

	rt.route(accessRead, op("GET", "/api/researches/{id}/roadmaps", "List roadmaps",
		"The research's roadmaps — the graphs that lay out what is planned and how the pieces connect.").
		tag("Roadmaps").
		returns("200", "The roadmaps.", list(sRoadmap)).
		build(), rmh.ListByResearch)

	rt.route(accessRead, op("GET", "/api/researches/{id}/roadmaps/{roadmapId}", "Get a roadmap within a research",
		"One roadmap with its nodes and edges.").
		tag("Roadmaps").
		returns("200", "The roadmap.", envelope(map[string]*huma.Schema{"data": sRoadmap})).
		build(), rmh.GetByResearch)

	rt.route(accessRead, op("GET", "/api/roadmaps/{id}", "Get a roadmap",
		"One roadmap. A node pointing at a task or a question carries that record's current state in `ref_data`, resolved at read time rather than stored — so a node never shows a status the entity has since left.").
		tag("Roadmaps").
		returns("200", "The roadmap, with resolved node references.", envelope(map[string]*huma.Schema{
			"data":  sRoadmap,
			"nodes": listOf(sRoadmapNode),
		})).
		build(), rmh.Get)

	// --- Graph endpoint ---
	gh := handlers.NewGraphHandler(researchSvc, sectionSvc, entrySvc, sessionSvc, taskSvc, entryRepo, crossrefRepo, access, log)
	rt.route(accessRead, op("GET", "/api/researches/{id}/graph", "The research as a graph",
		"Everything in the research as nodes and edges, which is what the mindmap view draws: sections, documents, sessions, tasks and the cross-references between them.").
		tag("Research").
		returns("200", "Nodes and edges.", envelope(map[string]*huma.Schema{
			"nodes": listOf(&huma.Schema{Type: "object"}),
			"edges": listOf(&huma.Schema{Type: "object"}),
			"available_node_types": listOf(&huma.Schema{Type: "string",
				Description: "The node types this caller could receive: always `section` and `entry`; `session` and `question` when sessions are readable; `task` when tasks are. Through a share link the include flags decide; the list never names what was withheld."}),
		})).
		build(), gh.Get)

	// --- Write endpoints ---
	wh := handlers.NewWriteHandler(researchSvc, sectionSvc, entrySvc, sessionSvc, taskSvc, log)
	crh := handlers.NewCrossRefHandler(crossrefRepo, entrySvc, researchSvc, access, log)
	crh.SetRoadmapService(roadmapSvc)

	rt.route(accessWrite, op("POST", "/api/researches", "Create a research",
		"Creates a research in the caller's team, with whatever sections were supplied, and allocates its short code (`R1`, `R2`, ...).").
		tag("Research").
		body("Name, goal, and optionally the sections to start with.", sCreateResearch).
		returns("201", "The research that was created.", envelope(map[string]*huma.Schema{
			"data": envelope(map[string]*huma.Schema{
				"research_id":      {Type: "string"},
				"code":             {Type: "string", Description: "The short code, e.g. `R1`. Usable in place of the id everywhere."},
				"name":             {Type: "string"},
				"status":           {Type: "string"},
				"sections_created": {Type: "integer"},
			}),
		})).
		build(), wh.CreateResearch)

	rt.route(accessWrite, op("PUT", "/api/researches/{id}", "Update a research",
		"Changes the fields that are sent and leaves the rest alone. `add_memory` atomically appends one note. Whole-array `memory` writes and `instruction` are rejected; use per-item memory routes and private skills.").
		tag("Research").
		body("The fields to change. An omitted field is left alone.", sUpdateResearch).
		returns("200", "The updated research.", envelope(map[string]*huma.Schema{"data": sResearch})).
		build(), wh.UpdateResearch)

	rt.route(accessRead, op("GET", "/api/researches/{id}/memory", "List memory",
		"Members may read structured working notes. Share visitors are forbidden.").
		tag("Research").
		returns("200", "The notes, with provenance and versions.", envelope(map[string]*huma.Schema{
			"data": {Type: "array", Items: sMemoryItem},
		})).build(), wh.ListMemory)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/memory", "Add memory",
		"Appends one note without replacing others. Author is determined from authentication.").
		tag("Research").body("The note to append.", sAddMemory).
		returns("201", "The created note.", envelope(map[string]*huma.Schema{"data": sMemoryItem})).
		build(), wh.AddMemory)

	rt.route(accessWrite, op("PATCH", "/api/researches/{id}/memory/{itemId}", "Edit memory",
		"Edits one note in this research and preserves its creation provenance.").
		tag("Research").body("New text and the current version.", sUpdateMemory).
		returns("204", "Updated.", nil).
		returns("409", "Stale version; reload before editing.", envelope(map[string]*huma.Schema{"error": {Type: "string"}})).
		build(), wh.UpdateMemory)

	rt.route(accessWrite, op("DELETE", "/api/researches/{id}/memory/{itemId}", "Delete memory",
		"Deletes only this note in this research. Idempotent.").
		tag("Research").returns("204", "Deleted.", nil).
		build(), wh.DeleteMemory)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/memory/bulk-delete", "Delete selected memory",
		"Deletes 1–500 explicitly selected IDs. Concurrently added notes are untouched.").
		tag("Research").body("The selected note IDs.", sBulkDeleteMemory).
		returns("204", "Deleted.", nil).
		build(), wh.BulkDeleteMemory)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/sections", "Add a section",
		"Adds a section to an existing research. A research created without sections has none until this is called.").
		tag("Research").
		body("The section to add.", sCreateSection).
		returns("201", "The section that was created.", envelope(map[string]*huma.Schema{"data": sSection})).
		build(), wh.AddSection)

	rt.route(accessWrite, op("PUT", "/api/sections/{sectionId}", "Update a section",
		"Renames a section or changes what it declares its documents must carry.").
		tag("Research").
		body("The fields to change.", sUpdateSection).
		returns("200", "The updated section.", envelope(map[string]*huma.Schema{"data": sSection})).
		build(), wh.UpdateSection)

	rt.route(accessWrite, op("DELETE", "/api/sections/{sectionId}", "Delete a section",
		"Removes a section. **Refuses with 409 while it still holds documents**, naming how many, unless `force=true` — deleting a section that silently takes five documents with it is the accident the confirmation exists to prevent, and an API that makes it a one-liner has moved the accident rather than removed it.\n\n"+
			"With `force=true` the documents go too, along with the cross-references and external links extracted from them. References *to* those documents from elsewhere stay as written and stop resolving.").
		tag("Research").
		queryBool("force", "Delete the section even though it holds documents, and delete them with it.").
		returns("200", "Deleted.", sDeleted).
		responds("409", "The section holds documents and `force` was not set. The message names how many.").
		build(), wh.DeleteSection)

	rt.route(accessRead, op("GET", "/api/researches/{id}/delete-preview", "What deleting this project would destroy",
		"Counts, per kind, what a delete would take with it, plus the references from *other* projects that would stop resolving — those survive as inert text rather than being removed, because deleting them would edit a project nobody asked to change.\n\n"+
			"A read, not a write: it is shown before the confirmation, and an editor who may not delete still needs to be told what the control they cannot use would do.").
		tag("Research").
		returns("200", "What would be destroyed.", envelope(map[string]*huma.Schema{"data": sDeletionSummary})).
		build(), wh.DeletePreview)

	rt.route(accessWrite, op("DELETE", "/api/researches/{id}", "Delete a project",
		"Destroys the project and everything under it: sections, documents and their revisions, sessions and questions, tasks, roadmaps, annotations, memory, share links and attached methodologies.\n\n"+
			"**Real deletion, not an archive.** There is no tombstone and no trash bin — the promise is that your data is a file you own, and a hidden graveyard inside that file is the opposite of it. `PUT /api/researches/{id}` with `status: \"archived\"` is the reversible path.\n\n"+
			"**Owner only.** An editor is trusted with the contents of a project and not with its existence. Someone with no access at all gets 404, because confirming the project exists is itself information.\n\n"+
			"References from other projects into this one are *not* deleted: they stop resolving and render as the text that was written.").
		tag("Research").
		returns("200", "Deleted.", sDeleted).
		responds("403", "You are not the owner of this project.").
		build(), wh.DeleteResearch)

	rt.route(accessWrite, op("POST", "/api/entries", "Create a document",
		"Files a document under a section and allocates its short code (`E1`, `E2`, ...). Any `[[...]]` references in the content are extracted and recorded.").
		tag("Documents").
		body("The document.", sCreateEntry).
		returns("201", "The document that was created. `metadata_report` is present when a key the section does not declare was dropped — silently dropping it is how a document comes back looking fine and missing a field.", envelope(map[string]*huma.Schema{
			"data": envelope(map[string]*huma.Schema{
				"entry_id":   {Type: "string"},
				"code":       {Type: "string", Description: "The short code, e.g. `E3`."},
				"title":      {Type: "string"},
				"status":     {Type: "string"},
				"entry_type": {Type: "string"},
			}),
			"metadata_report": {Type: "object"},
		})).
		build(), wh.CreateEntry)

	rt.route(accessWrite, op("PUT", "/api/entries/{id}", "Replace a document",
		"Writes new content over the whole document, leaving a revision behind. Cross-references are re-extracted.").
		tag("Documents").
		body("The new content.", sUpdateEntry).
		returns("200", "The updated document.", envelope(map[string]*huma.Schema{"data": sEntry})).
		build(), wh.UpdateEntry)

	rt.route(accessWrite, op("POST", "/api/entries/{id}/patch", "Patch a document",
		"Changes part of a document rather than replacing it — a block, a heading, a section of text. What a long document needs when only one paragraph moved.").
		tag("Documents").
		body("The operations to apply to the document's blocks.", sPatchEntry).
		returns("200", "The document after the patch.", envelope(map[string]*huma.Schema{"data": sEntry})).
		build(), wh.PatchEntry)

	rt.route(accessWrite, op("DELETE", "/api/entries/{id}", "Delete a document",
		"Removes the document and the cross-references extracted from it. References *to* it in other documents stay as written and stop resolving.").
		tag("Documents").
		returns("200", "Deleted.", sDeleted).
		build(), wh.DeleteEntry)
	// Read-state writes need only read permission: a viewer may acknowledge what
	// they have read without acquiring the right to edit team content. The
	// service enforces that distinction; `wrap` supplies the caller and tab id.
	rt.route(accessWrite, op("PUT", "/api/entries/{id}/seen", "Mark a document as read",
		"Records that this caller has seen the document at its current revision. Read state is per person: a viewer may acknowledge what they have read without gaining the right to edit it.").
		tag("Documents").
		body("Which revision the caller has read. Required — acknowledging a document without saying which version would mark a change nobody has seen.", envelope(map[string]*huma.Schema{
			"revision": {Type: "integer", Description: "The `revision` field of GET /api/entries/{id}."},
		}, "revision")).
		returns("200", "What was recorded.", envelope(map[string]*huma.Schema{
			"data": envelope(map[string]*huma.Schema{
				"entry_id": {Type: "string"},
				"revision": {Type: "integer"},
			}),
		})).
		build(), evh.MarkSeen)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/updates/seen", "Mark everything as read",
		"Clears the whole unseen list for this caller in this research.").
		tag("Documents").
		returns("200", "Recorded.", sOK).
		build(), evh.MarkAllSeen)

	rt.route(accessWrite, op("POST", "/api/tasks", "Create a task",
		"Adds an item to the research's task list.").
		tag("Tasks").
		body("The task.", sCreateTask).
		returns("201", "The task that was created.", envelope(map[string]*huma.Schema{"data": sTask})).
		build(), wh.CreateTask)

	rt.route(accessWrite, op("PUT", "/api/tasks/{id}", "Update a task",
		"Moves a task along, or records what came of it. A roadmap node pointing at this task shows the new state the next time it is read.").
		tag("Tasks").
		body("The fields to change.", sUpdateTask).
		returns("200", "The updated task.", envelope(map[string]*huma.Schema{"data": sTask})).
		build(), wh.UpdateTask)

	rt.route(accessWrite, op("DELETE", "/api/tasks/{id}", "Delete a task",
		"Removes the task. A roadmap node that pointed at it keeps its own title and stops resolving.").
		tag("Tasks").
		returns("200", "Deleted.", sDeleted).
		build(), wh.DeleteTask)

	rt.route(accessWrite, op("POST", "/api/sessions", "Start a session",
		"Opens an interview session and allocates its short code (`SS1`, `SS2`, ...). Several may exist in one research; documents written during one record which session produced them.").
		tag("Sessions").
		body("The session.", sCreateSession).
		returns("201", "The session that was created.", envelope(map[string]*huma.Schema{"data": sSession})).
		build(), wh.CreateSession)

	rt.route(accessWrite, op("PUT", "/api/sessions/{id}", "Update a session",
		"Changes the session's focus, notes or status. Notes are working process and are not served through a share link.").
		tag("Sessions").
		body("The fields to change.", sUpdateSession).
		returns("200", "The updated session.", envelope(map[string]*huma.Schema{"data": sSession})).
		build(), wh.UpdateSession)

	rt.route(accessWrite, op("DELETE", "/api/sessions/{id}", "Delete a session",
		"Removes the session and the questions asked in it.\n\n"+
			"Documents written during the session **survive**, with their link to it cleared. A finding is not an artefact of the conversation that produced it, and deleting a transcript should not delete the conclusions drawn in it.").
		tag("Sessions").
		returns("200", "Deleted.", sDeleted).
		build(), wh.DeleteSession)

	rt.route(accessWrite, op("DELETE", "/api/questions/{questionId}", "Delete a question",
		"Removes one question and any references it carried. Follow-up questions asked under it survive, with their parent link cleared, rather than disappearing with it.").
		tag("Sessions").
		returns("200", "Deleted.", sDeleted).
		build(), wh.DeleteQuestion)

	rt.route(accessWrite, op("PUT", "/api/questions/{questionId}", "Answer or edit a question",
		"Records an answer, or changes the question. Answers may carry `[[...]]` references like any other text, and they are rendered as links wherever they appear.").
		tag("Sessions").
		body("The answer, or a change of status.", sUpdateQuestion).
		returns("200", "The updated question.", envelope(map[string]*huma.Schema{"data": sQuestion})).
		build(), wh.UpdateQuestion)

	rt.route(accessWrite, op("POST", "/api/sessions/{id}/questions", "Add questions to a session",
		"Appends questions to a running session. Codes (`Q1`, `Q2`, ...) are allocated per session.").
		tag("Sessions").
		body("The questions to add.", sAddQuestions).
		returns("200", "The questions that were added.", envelope(map[string]*huma.Schema{
			"data":  listOf(sQuestion),
			"count": {Type: "integer"},
		})).
		build(), wh.AddQuestions)

	rt.route(accessWrite, op("POST", "/api/roadmaps", "Create a roadmap",
		"Creates a roadmap with its nodes and edges in one call. Nodes are given temporary ids in the request so edges can reference them before the real ids exist.").
		tag("Roadmaps").
		body("The roadmap, its nodes and its edges.", envelope(map[string]*huma.Schema{
			"research_id": {Type: "string"},
			"title":       {Type: "string"},
			"nodes":       listOf(&huma.Schema{Type: "object"}),
			"edges":       listOf(&huma.Schema{Type: "object"}),
		}, "research_id", "title")).
		returns("201", "The roadmap that was created.", envelope(map[string]*huma.Schema{"data": sRoadmap})).
		build(), rmh.Create)

	rt.route(accessWrite, op("PUT", "/api/roadmaps/{id}", "Update a roadmap",
		"Changes the roadmap itself. Nodes are edited through their own route.").
		tag("Roadmaps").
		body("The fields to change.", envelope(map[string]*huma.Schema{
			"title":       {Type: "string"},
			"description": {Type: "string"},
		})).
		returns("200", "The updated roadmap.", envelope(map[string]*huma.Schema{"data": sRoadmap})).
		build(), rmh.Update)

	rt.route(accessWrite, op("DELETE", "/api/roadmaps/{id}", "Delete a roadmap",
		"Removes the roadmap with its nodes and edges. The tasks and sessions its nodes pointed at are untouched.").
		tag("Roadmaps").
		returns("200", "Deleted.", sDeleted).
		build(), rmh.Delete)

	rt.route(accessWrite, op("PUT", "/api/roadmap-nodes/{nodeId}", "Update a roadmap node",
		"Changes one node — its title, its position, or what it points at.").
		tag("Roadmaps").
		body("The fields to change.", envelope(map[string]*huma.Schema{
			"title":    {Type: "string"},
			"status":   {Type: "string"},
			"ref_type": {Type: "string", Description: "`task`, `session`, `question` or `entry`."},
			"ref_id":   {Type: "string"},
		})).
		returns("200", "The updated node.", envelope(map[string]*huma.Schema{"data": sRoadmapNode})).
		build(), rmh.UpdateNode)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/crossrefs/rebuild", "Rebuild cross-references",
		"Re-scans every document in the research and rewrites the reference table. What to run when references have gone stale — after an import, or after codes were backfilled.").
		tag("Cross-references").
		returns("200", "How many references were found.", envelope(map[string]*huma.Schema{
			"rebuilt": {Type: "integer"},
			"status":  {Type: "string"},
		})).
		build(), crh.Rebuild)

	// --- Teams ---
	//
	// Every route here is scoped by membership inside TeamService, so wrapRead
	// and wrap do the same job they do elsewhere: put the caller in the
	// context. The one exception is the invite preview, which is reachable
	// without a session on purpose.
	rt.route(accessRead, op("GET", "/api/teams", "List teams",
		"The teams the caller belongs to, with their role in each. Everybody has at least a personal team, created at registration, which is where a solo user's researches live.").
		tag("Teams").
		returns("200", "The caller's teams.", list(sTeam)).
		build(), tmh.List)

	rt.route(accessRead, op("GET", "/api/teams/{id}", "Get a team",
		"One team, with the caller's role in it.").
		tag("Teams").
		returns("200", "The team.", envelope(map[string]*huma.Schema{"data": sTeam})).
		build(), tmh.Get)

	rt.route(accessWrite, op("POST", "/api/teams", "Create a team",
		"Creates a team with the caller as its owner.").
		tag("Teams").
		body("The team.", envelope(map[string]*huma.Schema{
			"name":        {Type: "string"},
			"description": {Type: "string"},
		}, "name")).
		returns("201", "The team that was created.", envelope(map[string]*huma.Schema{"data": sTeam})).
		build(), tmh.Create)

	rt.route(accessWrite, op("PUT", "/api/teams/{id}", "Update a team",
		"Renames a team or changes its description. Owners only, and a personal team can be renamed like any other — what makes it personal is that it cannot be deleted or left.").
		tag("Teams").
		body("The fields to change.", envelope(map[string]*huma.Schema{
			"name":        {Type: "string"},
			"description": {Type: "string"},
		})).
		returns("200", "The updated team.", envelope(map[string]*huma.Schema{"data": sTeam})).
		build(), tmh.Update)

	rt.route(accessWrite, op("DELETE", "/api/teams/{id}", "Delete a team",
		"Removes the team and everything it owns. Owners only, and a personal team cannot be deleted.").
		tag("Teams").
		returns("200", "Deleted.", sOK).
		responds("409", "A personal team, or the team still owns researches.").
		build(), tmh.Delete)

	rt.route(accessRead, op("GET", "/api/teams/{id}/members", "List members",
		"Who is in the team and with what role: `viewer` reads and exports, `editor` also writes content, `owner` also manages members and moves researches between teams.").
		tag("Teams").
		returns("200", "The members.", list(sTeamMember)).
		build(), tmh.Members)

	rt.route(accessWrite, op("PUT", "/api/teams/{id}/members/{userId}", "Change a member's role",
		"Owners only. The last owner cannot be demoted — a team with nobody who can manage it is unrecoverable.").
		tag("Teams").
		body("The new role.", envelope(map[string]*huma.Schema{
			"role": {Type: "string", Description: "`viewer`, `editor` or `owner`."},
		}, "role")).
		returns("200", "The updated membership.", envelope(map[string]*huma.Schema{"data": sTeamMember})).
		responds("409", "That would leave the team with no owner.").
		build(), tmh.UpdateMember)

	rt.route(accessWrite, op("DELETE", "/api/teams/{id}/members/{userId}", "Remove a member",
		"Owners only, and never the last owner. Anyone removed is told over the WebSocket, which is the one message that has to reach somebody who no longer qualifies to receive it.").
		tag("Teams").
		returns("200", "Removed.", sOK).
		responds("409", "That would leave the team with no owner.").
		build(), tmh.RemoveMember)

	rt.route(accessRead, op("GET", "/api/teams/{id}/invites", "List invitations",
		"The team's outstanding invitations.").
		tag("Teams").
		returns("200", "The invitations.", list(sTeamInvite)).
		build(), tmh.Invites)

	rt.route(accessWrite, op("POST", "/api/teams/{id}/invites", "Invite someone",
		"Creates an invitation and returns the token that addresses it. Owners only.").
		tag("Teams").
		body("Who to invite and as what.", envelope(map[string]*huma.Schema{
			"email": {Type: "string", Format: "email"},
			"role":  {Type: "string", Description: "`viewer`, `editor` or `owner`."},
		}, "email")).
		returns("201", "The invitation.", envelope(map[string]*huma.Schema{"data": sTeamInvite})).
		build(), tmh.CreateInvite)

	rt.route(accessWrite, op("DELETE", "/api/invites/{id}", "Revoke an invitation",
		"The link stops working immediately.").
		tag("Teams").
		returns("200", "Revoked.", sOK).
		build(), tmh.RevokeInvite)

	rt.route(accessOptional, op("GET", "/api/invites/{token}", "Preview an invitation",
		"What the link is offering. Deliberately reachable without an account, because somebody following an invitation does not have one yet — and if they are signed in, the answer says more.").
		tag("Teams").
		returns("200", "The team and role being offered.", envelope(map[string]*huma.Schema{"data": sTeamInvite})).
		build(), tmh.PreviewInvite)

	rt.route(accessWrite, op("POST", "/api/invites/{token}/accept", "Accept an invitation",
		"Joins the team at the role the invitation names. Needs an account, so the sign-up happens first.").
		tag("Teams").
		returns("200", "Joined.", envelope(map[string]*huma.Schema{"data": sTeam})).
		responds("409", "Already a member.").
		build(), tmh.AcceptInvite)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/transfer", "Move a research to another team",
		"Changes which team owns the research, and with it who can reach it. Owner of both teams.").
		tag("Teams").
		body("The destination team.", envelope(map[string]*huma.Schema{
			"team_id": {Type: "string"},
		}, "team_id")).
		returns("200", "Moved.", envelope(map[string]*huma.Schema{"data": sResearch})).
		build(), tmh.TransferResearch)

	rt.route(accessWrite, op("POST", "/api/teams/{id}/researches", "Move several researches into a team",
		"The bulk form of the transfer above.").
		tag("Teams").
		body("The researches to move.", envelope(map[string]*huma.Schema{
			"research_ids": listOf(&huma.Schema{Type: "string"}),
		}, "research_ids")).
		returns("200", "Moved.", envelope(map[string]*huma.Schema{
			"moved": {Type: "integer"},
		})).
		build(), tmh.AddResearches)

	// --- Skills ---
	//
	// A slug addresses a skill only inside a research, where the resolution
	// order is defined (private, then team, then built-in). Management is by id,
	// because a built-in and a team's fork of it share a slug on purpose.
	//
	// None of these are mounted on the shared sub-mux. A skill is a team's
	// methodology — the same class of working process as the instruction it
	// replaces, which redactForShare has always stripped — so a share link must
	// not reach one. SkillService.Load refuses a share context as well, because
	// a future route that forgot this should still fail closed.
	// Annotations. The queue is a read, everything that changes a mark is a
	// write — including closing one, which is the human decision this whole
	// feature turns on.
	rt.route(accessRead, op("GET", "/api/researches/{id}/annotations", "The annotation queue",
		"Every mark left on the research's documents — a question, a doubt, a request — and what has become of each.").
		tag("Annotations").
		query("status", "Filter by status: `open`, `answered`, `closed`.").
		query("kind", "Filter by kind.").
		query("anchor", "Filter by anchor state — `orphaned` means the passage somebody doubted has since been rewritten.").
		query("entry_id", "Only marks on this document.").
		returns("200", "The annotations and the tallies the queue is read by.", envelope(map[string]*huma.Schema{
			"data":  listOf(sAnnotation),
			"count": {Type: "integer"},
			"meta": envelope(map[string]*huma.Schema{
				"counts":    {Type: "object", Description: "How many in each status."},
				"by_anchor": {Type: "object", Description: "How many in each anchor state — `orphaned` means the passage somebody doubted has since been rewritten."},
				"by_entry":  {Type: "object"},
			}),
			"counts": {Type: "object", Description: "The same tallies as `meta.counts`, kept because the research page already reads them here."},
		})).
		build(), anh.ListByResearch)

	rt.route(accessRead, op("GET", "/api/entries/{id}/annotations", "One document's annotations",
		"The marks left on this document, with the quoted text each was anchored to.").
		tag("Annotations").
		returns("200", "The annotations on this document.", list(sAnnotation)).
		build(), anh.ListByEntry)

	rt.route(accessWrite, op("POST", "/api/entries/{id}/annotations", "Annotate a document",
		"Leaves a mark on a passage. The quoted text is stored with it, so the mark can report when the passage it was attached to has since changed.").
		tag("Annotations").
		body("The annotation.", envelope(map[string]*huma.Schema{
			"kind":  {Type: "string"},
			"body":  {Type: "string"},
			"quote": {Type: "string", Description: "The passage being marked."},
		}, "body")).
		returns("201", "The annotation.", envelope(map[string]*huma.Schema{"data": sAnnotation})).
		build(), anh.Create)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/annotations/bulk", "Annotate several documents",
		"Leaves several marks in one call — what a review pass produces.").
		tag("Annotations").
		body("The annotations.", envelope(map[string]*huma.Schema{
			"annotations": listOf(&huma.Schema{Type: "object"}),
		}, "annotations")).
		returns("200", "What was created, and what could not be anchored.", list(sAnnotation)).
		build(), anh.Bulk)

	rt.route(accessWrite, op("PUT", "/api/annotations/{id}", "Answer or close an annotation",
		"Records an answer, or closes the mark. Closing one is a human decision, which is the whole point of the queue.").
		tag("Annotations").
		body("The fields to change.", envelope(map[string]*huma.Schema{
			"status": {Type: "string", Description: "`open`, `answered` or `closed`."},
			"body":   {Type: "string"},
			"answer": {Type: "string"},
		})).
		returns("200", "The updated annotation.", envelope(map[string]*huma.Schema{"data": sAnnotation})).
		build(), anh.Update)

	rt.route(accessWrite, op("DELETE", "/api/annotations/{id}", "Delete an annotation",
		"Removes the mark and the passage it quoted. The document it was attached to is untouched, and deleting a mark is not the same as closing one — a closed annotation stays in the queue as a record that somebody decided.").
		tag("Annotations").
		returns("204", "Deleted.", nil).
		build(), anh.Delete)

	skillNote := "\n\nA skill is a team's methodology — the same class of working process as research memory — so no skill route is reachable through a share link."
	rt.route(accessRead, op("GET", "/api/researches/{id}/skills", "Skills attached to a research",
		"The skills this research works by, in resolution order: the research's own, then its team's, then the built-ins."+skillNote).
		tag("Skills").
		returns("200", "The attached skills, and the budget they are chosen against.", envelope(map[string]*huma.Schema{
			"data":   listOf(sSkill),
			"count":  {Type: "integer"},
			"cap":    {Type: "integer", Description: "How many a research may choose. Sent with the list so a client and the server cannot disagree about the limit."},
			"chosen": {Type: "integer", Description: "How many count against the cap. Lower than `count`, because the ambient product skills are on by definition and outside the budget."},
		})).
		build(), skh.ListAttached)

	rt.route(accessRead, op("GET", "/api/researches/{id}/skills/library", "Skills available to attach",
		"Everything this research could attach: its team's skills and the built-in library."+skillNote).
		tag("Skills").
		query("q", "Filter the library by name or slug.").
		returns("200", "The available skills.", list(sSkill)).
		build(), skh.Library)

	rt.route(accessRead, op("GET", "/api/researches/{id}/skills/{slug}", "Read a skill",
		"The skill a slug names inside this research. A slug is an address only within a research, because a team's fork keeps its parent's slug on purpose."+skillNote).
		tag("Skills").
		returns("200", "The skill.", envelope(map[string]*huma.Schema{"data": sSkill})).
		build(), skh.Read)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/skills", "Create a skill on a research",
		"Writes a skill that belongs to this research alone."+skillNote).
		tag("Skills").
		body("The skill.", envelope(map[string]*huma.Schema{
			"slug":    {Type: "string"},
			"name":    {Type: "string"},
			"content": {Type: "string", Description: "The methodology, as markdown."},
		}, "slug", "content")).
		returns("201", "The skill.", envelope(map[string]*huma.Schema{"data": sSkill})).
		build(), skh.Create)

	rt.route(accessWrite, op("DELETE", "/api/researches/{id}/skills/{slug}", "Detach a skill",
		"Stops this research working by that skill. The skill itself survives if it belongs to the team."+skillNote).
		tag("Skills").
		returns("204", "Detached.", nil).
		build(), skh.Detach)

	rt.route(accessWrite, op("PUT", "/api/researches/{id}/skills/{slug}", "Fork a skill into this research",
		"Takes a copy of a team or built-in skill and edits it here, leaving the original alone."+skillNote).
		tag("Skills").
		body("The changed content.", envelope(map[string]*huma.Schema{
			"name":    {Type: "string"},
			"content": {Type: "string"},
		})).
		returns("200", "The research's own copy.", envelope(map[string]*huma.Schema{"data": sSkill})).
		build(), skh.Fork)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/skills/{slug}/copy", "Copy a skill here",
		"Copies a skill into this research without attaching it to anything else."+skillNote).
		tag("Skills").
		returns("201", "The copy.", envelope(map[string]*huma.Schema{"data": sSkill})).
		build(), skh.CopyHere)

	rt.route(accessWrite, op("POST", "/api/researches/{id}/skills/{slug}/promote", "Promote a skill to the team",
		"Moves a skill that proved useful out of one research and into the team's library, where every research can reach it."+skillNote).
		tag("Skills").
		returns("200", "The promoted skill.", envelope(map[string]*huma.Schema{"data": sSkill})).
		build(), skh.Promote)

	rt.route(accessRead, op("GET", "/api/teams/{id}/skills", "A team's skills",
		"The team's own library."+skillNote).
		tag("Skills").
		returns("200", "The team's skills.", list(sSkill)).
		build(), skh.ListTeam)

	rt.route(accessWrite, op("POST", "/api/teams/{id}/skills", "Create a team skill",
		"Writes a skill every research in the team can attach."+skillNote).
		tag("Skills").
		body("The skill.", envelope(map[string]*huma.Schema{
			"slug":    {Type: "string"},
			"name":    {Type: "string"},
			"content": {Type: "string"},
		}, "slug", "content")).
		returns("201", "The skill.", envelope(map[string]*huma.Schema{"data": sSkill})).
		build(), skh.CreateTeam)

	rt.route(accessRead, op("GET", "/api/skills/{skillId}", "Read a skill by id",
		"Management is by id, because a built-in and a team's fork of it share a slug on purpose."+skillNote).
		tag("Skills").
		returns("200", "The skill.", envelope(map[string]*huma.Schema{"data": sSkill})).
		build(), skh.ReadByID)

	rt.route(accessWrite, op("PUT", "/api/skills/{skillId}", "Update a skill",
		"Edits a skill in place, by id."+skillNote).
		tag("Skills").
		body("The fields to change.", envelope(map[string]*huma.Schema{
			"name":    {Type: "string"},
			"content": {Type: "string"},
		})).
		returns("200", "The updated skill.", envelope(map[string]*huma.Schema{"data": sSkill})).
		build(), skh.Update)

	rt.route(accessWrite, op("DELETE", "/api/skills/{skillId}", "Delete a skill",
		"Removes the skill. Researches that had it attached lose it."+skillNote).
		tag("Skills").
		returns("204", "Deleted.", nil).
		build(), skh.Delete)

	// --- Templates ---
	//
	// A template is a kickoff methodology, so it belongs to nobody or to a team
	// — never to a research. That is why none of these hang off
	// /api/researches/{id}: the one that does, `templates/draft`, reads a
	// research to *propose* a template and creates nothing.
	//
	// Not on the shared sub-mux, for the same reason skills are not: a
	// template body is a team's working process.
	tmplNote := "\n\nA template is a kickoff methodology, so it belongs to nobody or to a team — never to a research. Not reachable through a share link: a template body is a team's working process."
	rt.route(accessOperatorRead, op("GET", "/api/templates", "List templates",
		"The templates the caller can reach. Presented with the instance `api_token` this is the server-wide library; presented with a person's credential it is what their teams can see."+tmplNote).
		tag("Templates").
		returns("200", "The templates.", list(sTemplate)).
		build(), tph.List)

	rt.route(accessOperatorRead, op("GET", "/api/templates/{slug}", "Read a template",
		"One template by slug, resolved in the caller's scope."+tmplNote).
		tag("Templates").
		returns("200", "The template.", envelope(map[string]*huma.Schema{"data": sTemplate})).
		build(), tph.Get)
	// Read by slug, write by id — the same split skills use, and for the same
	// reason: a team's fork keeps its parent's slug, so a slug is an address
	// only within one caller's scope. Fork is a POST because `PUT {slug}/fork`
	// and `PUT {templateId}` are ambiguous to the router and neither is more
	// specific, which it refuses to route at all.
	// The server-wide library. `wrapOperator` on all three because a global
	// template and a team's live at the same routes and are told apart by the
	// row, not by the path — see the wrapper for why the api_token cannot go
	// through `wrap`.
	rt.route(accessOperatorWrite, op("POST", "/api/templates", "Create a server-wide template",
		"Writes a template every team on this instance will see. This is the operator's route: the instance `api_token` is recognised ahead of the user check, because the operator is not a user and is in nobody's member list."+tmplNote).
		tag("Templates").
		body("The methodology itself, and when to reach for it.", sTemplateBody).
		returns("201", "The template.", envelope(map[string]*huma.Schema{"data": sTemplate})).
		responds("409", "A template with that name already exists at this tier.").
		build(), tph.CreateGlobal)

	rt.route(accessWrite, op("POST", "/api/templates/{slug}/fork", "Fork a template into a team",
		"Copies a template into the caller's team so it can be edited there. A `POST` rather than a `PUT`, because `PUT {slug}/fork` and `PUT {templateId}` are ambiguous to the router and neither is more specific."+tmplNote).
		tag("Templates").
		body("Which team to fork it into. Required — there is no personal-team fallback, because guessing which library somebody's copy belongs in is a guess that is wrong silently.", sTemplateBody).
		returns("200", "The team's copy. `forked` is `true`, which is how a client tells this from an ordinary update and knows to follow the slug to a new row.", envelope(map[string]*huma.Schema{
			"data":   sTemplate,
			"forked": {Type: "boolean"},
		})).
		build(), tph.Fork)

	rt.route(accessOperatorWrite, op("PUT", "/api/templates/{templateId}", "Update a template",
		"Edits a template by id. Read by slug, write by id — a team's fork keeps its parent's slug, so a slug addresses a template only within one caller's scope."+tmplNote).
		tag("Templates").
		body("The fields to change.", sTemplateBody).
		returns("200", "The updated template.", envelope(map[string]*huma.Schema{"data": sTemplate})).
		build(), tph.Update)

	rt.route(accessOperatorWrite, op("DELETE", "/api/templates/{templateId}", "Delete a template",
		"Removes the template. Researches already created from it are untouched."+tmplNote).
		tag("Templates").
		returns("204", "Deleted.", nil).
		build(), tph.Delete)

	rt.route(accessRead, op("GET", "/api/teams/{id}/templates", "A team's templates",
		"The team's own templates, without the server-wide library."+tmplNote).
		tag("Templates").
		returns("200", "The team's templates.", list(sTemplate)).
		build(), tph.ListTeam)

	rt.route(accessWrite, op("POST", "/api/teams/{id}/templates", "Create a team template",
		"Writes a template every research in the team can start from."+tmplNote).
		tag("Templates").
		body("The methodology itself, and when to reach for it.", sTemplateBody).
		returns("201", "The template.", envelope(map[string]*huma.Schema{"data": sTemplate})).
		build(), tph.CreateTeam)

	rt.route(accessRead, op("GET", "/api/researches/{id}/templates/draft", "Propose a template from a research",
		"Reads a research and proposes the template it would make — its sections and their field declarations. It creates nothing; this is the one template route that hangs off a research, and that is why."+tmplNote).
		tag("Templates").
		returns("200", "A skeleton to fill in, not a template to save.", envelope(map[string]*huma.Schema{
			"data": sTemplate,
			"hint": {Type: "string", Description: "What is still missing before this is worth posting."},
		})).
		build(), tph.DraftFromResearch)

	// --- Share links ---
	//
	// Both halves live in share_routes.go: the owner's management routes here on
	// the authenticated API, and the visitor's read surface behind its own
	// prefix and its own middleware. Keeping the public prefix in one file is
	// the point — it is the only place in the product where data leaves the
	// owner boundary, and it should be readable in one sitting.
	registerShareRoutes(rt, shareDeps{
		shares:   shareSvc,
		research: rh,
		entry:    eh,
		session:  sh,
		task:     th,
		roadmap:  rmh,
		crossref: crReadHandler,
		links:    elHandler,
		export:   exportHandler,
		share:    shh,
		graph:    gh,
	}, sShare)

	// Backfill short codes for all records missing them
	rt.route(accessWrite, op("POST", "/api/admin/backfill-codes", "Backfill missing short codes",
		"Allocates short codes for records that predate them. Idempotent, and safe to run twice; run `crossrefs/rebuild` afterwards so references resolve against the new codes.").
		tag("Admin").
		returns("200", "How many records were given a code.", envelope(map[string]*huma.Schema{
			"backfilled": {Type: "integer"},
			"status":     {Type: "string"},
		})).
		build(), func(w http.ResponseWriter, r *http.Request) {
		count, err := storage.BackfillCodes(r.Context(), db)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"backfilled": count, "status": "ok"})
	})

	if cfg.AuthEnabled {
		log.Info("auth: multi-user authentication enabled")
	} else if cfg.APIToken != "" {
		log.Info("write API: bearer token required")
	} else {
		// Spelled out, because the two people this bites are the ones who will
		// not guess what "local" means: a container publishes its port, so the
		// host's browser arrives from the bridge network and is refused, and a
		// reverse proxy makes every caller look either remote or — worse, if it
		// sets no forwarding header — local.
		log.Warn("write API: writes accepted only from this machine (loopback, no proxy) — set api_token or auth_enabled to accept writes from a container, a proxy or another computer; reads stay open either way")
	}

	// WebSocket
	// The hub decides delivery per event, so this only has to establish who is
	// on the other end. With auth off it takes anyone, as it always has — but
	// the origin is checked either way, because "auth off" is a single-user
	// local run, not an invitation for any page in the browser to listen in.
	if authSvc != nil {
		hub.SetTokenValidator(authSvc)
	}
	rt.undocumented("/ws", ws.HandleWebSocket(hub, cfg.BaseURL))

	// Health
	rt.route(accessPublic, op("GET", "/api/health", "Health check",
		"Whether the server is up, what it was built as, and how it is configured — which is the one thing every page can ask without a credential.").
		tag("Meta").
		returns("200", "Server status.", envelope(map[string]*huma.Schema{
			"status":       {Type: "string", Description: "Always `ok`."},
			"version":      {Type: "string"},
			"in_memory":    {Type: "boolean", Description: "True when the database is in memory and nothing survives a restart."},
			"write_api":    {Type: "boolean", Description: "Whether the server would accept a write from **this request** — not whether a credential is configured somewhere. With accounts on it is `true` and your role decides the rest. With an `api_token` and no accounts it is `true` only when this request presented that token, so a browser holding none is told `false`. With neither configured it is `true` to a caller on the server's own machine and `false` to everyone else."},
			"auth_enabled": {Type: "boolean"},
		}, "status")).
		build(), func(w http.ResponseWriter, r *http.Request) {
		// Per caller, and it has to be what this request actually presented.
		//
		// It used to report `api_token != "" || auth_enabled`, which said
		// `false` on an instance accepting anonymous writes from anywhere — the
		// one answer that would stop an operator checking. Reading the config
		// again here would repeat the mistake in the other direction: on an
		// api_token instance every browser would be told `true`, and the web UI
		// trusts this field to decide whether to render Edit at all, so it would
		// render a full set of controls that each come back 401.
		//
		// With accounts on the honest answer is "yes, writes exist here" — this
		// route carries no session to check, and the UI decides the rest from
		// the caller's role.
		writeAPI := cfg.AuthEnabled
		switch {
		case cfg.AuthEnabled:
		case cfg.APIToken != "":
			writeAPI = operatorCredential(cfg.APIToken, r)
			// A wrong token presented here is a guess, and this route answers
			// 200 either way — so without this line it is the one place an
			// attacker can test candidate tokens and read the verdict while
			// leaving no trace, when the same guess against a write logs.
			// Only a presented-and-wrong credential is logged: the ordinary
			// unauthenticated health check is every monitor in the world.
			if !writeAPI && r.Header.Get("Authorization") != "" {
				log.Warn("health: bearer token presented and rejected",
					"remote", r.RemoteAddr, "host", r.Host)
			}
		default:
			writeAPI = auth.LocalRequest(r)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "ok",
			"version":      cfg.Version,
			"in_memory":    cfg.IsInMemory,
			"write_api":    writeAPI,
			"auth_enabled": cfg.AuthEnabled,
		})
	})

	// The OpenAPI document, generated from the registrations above. Both
	// extensions serve the same document; a client that cannot read YAML asks
	// for the JSON.
	specYAML, specJSON := rt.specHandlers()
	rt.route(accessPublic, op("GET", "/api/openapi.yaml", "This document, as YAML",
		"The OpenAPI 3.1 description of this API, generated from the routes the server actually registered.").
		tag("Meta").
		returnsFile("The specification.", "application/yaml").
		respondsEmpty("304", "The document has not changed since the `ETag` in `If-None-Match` was issued. It is marshalled once at startup and cannot change while the server is up.").
		build(), specYAML)
	rt.route(accessPublic, op("GET", "/api/openapi.json", "This document, as JSON",
		"The same specification as `/api/openapi.yaml`.").
		tag("Meta").
		returnsFile("The specification.", "application/json").
		respondsEmpty("304", "The document has not changed since the `ETag` in `If-None-Match` was issued. It is marshalled once at startup and cannot change while the server is up.").
		build(), specJSON)

	// LLMs documentation
	llmsHandler := handleLLMSDocs()
	rt.route(accessPublic, op("GET", "/llms.txt", "Documentation index for assistants",
		"The entry point for an assistant with no MCP client: what this product is, and where the guides are. The guides themselves are under `/llms/`.").
		tag("Meta").
		returnsFile("The index.", "text/plain").
		build(), llmsHandler.ServeHTTP)
	rt.undocumented("GET /llms/", http.StripPrefix("/", llmsHandler))

	// Anything under /api/ that matched no route above is a 404, in JSON, on
	// every method.
	//
	// Without this the catch-all below took them, and answered in two languages
	// neither of which was the API's: a GET returned 200 with the SPA's index
	// page, so a client with a stale path — or a monitor watching a route that
	// had been renamed — saw success and a mouthful of HTML; a POST or a DELETE
	// reached the MCP handler and came back with "malformed payload: invalid
	// message version tag" or "DELETE requires an Mcp-Session-Id header",
	// describing a protocol the caller was not speaking. The 200 was the worse
	// of the two: it is how the web UI's own auth probe came to read a missing
	// route as a successful answer.
	//
	// 404 and not 405 on a method mismatch: this pattern carries no method, so
	// it matches before Go's mux reaches the point where it would collect the
	// allowed ones. The share prefix settled the same question the same way, for
	// the same reason — see `notThere` in share_routes.go.
	rt.undocumented("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such endpoint"})
	}))

	// MCP Streamable HTTP transport (used by ChatGPT, Claude.ai)
	if cfg.MCPHandler != nil {
		// This endpoint takes the same credential the REST writes take.
		//
		// `wrap` has recognised the api_token since it was introduced; this one
		// never did, so an instance configured with `api_token` and no accounts
		// — the posture the Write API section of CLAUDE.md describes — refused
		// an anonymous `POST /api/entries` with a 401 and then handed the same
		// caller every tool through `tools/call`, writes included. The gate was
		// on one of the two doors into the same services.
		//
		// The catch-all below dispatches to this same handler, so gating only
		// `/mcp` would leave the hole open on every other path.
		//
		// With no credential configured it takes the same loopback exemption
		// the REST writes take. Every tool reaches the services a write goes
		// through, so leaving this door open would have made `localOnly` on the
		// REST side decorative.
		var mcpEndpoint http.Handler
		switch {
		case requireAuth != nil:
			mcpEndpoint = requireAuth(cfg.MCPHandler)
		case cfg.APIToken != "":
			mcpEndpoint = bearerAuth(cfg.APIToken)(cfg.MCPHandler)
		default:
			mcpEndpoint = localOnly(cfg.MCPHandler)
		}
		rt.undocumented("/mcp", mcpEndpoint)
		log.Info("MCP Streamable HTTP endpoint registered at /mcp")

		// Catch-all: serve MCP for POST/DELETE with MCP headers, static frontend for everything else
		static := staticHandler()
		rt.undocumented("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Route MCP requests (POST/DELETE with JSON or MCP session header) to MCP handler
			if (r.Method == http.MethodPost || r.Method == http.MethodDelete) &&
				(r.Header.Get("Content-Type") == "application/json" || r.Header.Get("Mcp-Session-Id") != "") {
				mcpEndpoint.ServeHTTP(w, r)
				return
			}
			// GET with Accept: text/event-stream or MCP session → also MCP
			if r.Method == http.MethodGet && (r.Header.Get("Accept") == "text/event-stream" || r.Header.Get("Mcp-Session-Id") != "") {
				mcpEndpoint.ServeHTTP(w, r)
				return
			}
			static.ServeHTTP(w, r)
		}))
	} else {
		// Embedded frontend (catch-all, must be last)
		rt.undocumented("/", staticHandler())
	}

	return &Server{mux: mux, router: rt, hub: hub, port: cfg.Port, log: log}
}

// serviceTokenValidator adapts AuthService for the auth middleware.
type serviceTokenValidator struct {
	authSvc *service.AuthService
}

func (v *serviceTokenValidator) ValidateToken(r *http.Request) (*domain.User, error) {
	token := extractBearerToken(r)
	if token == "" {
		return nil, nil
	}
	return v.authSvc.ValidateToken(r.Context(), token)
}

func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		return authHeader[7:]
	}
	// Also check query param (for SSE connections)
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}

// operatorCredential answers whether this request carries the instance
// api_token, and reads the **header only**.
//
// Deliberately not extractBearerToken, which also accepts `?token=` for SSE. The
// api_token is the longest-lived and highest-privilege secret on the instance —
// it comes out of the config file and nothing rotates it — and in a query string
// it lands verbatim in the nginx access log (`deploy/nginx/mcp-research.conf`
// logs `$request`), in browser history on the GET routes, and in the `Referer`
// of any outbound link. `bearerAuth` has always read the header only; these two
// wrappers are the same credential and must not be laxer than it.
func operatorCredential(configured string, r *http.Request) bool {
	if configured == "" {
		return false
	}
	a := r.Header.Get("Authorization")
	return len(a) > 7 && a[:7] == "Bearer " && a[7:] == configured
}

// bearerAuth returns middleware that validates Authorization: Bearer <token>.
func bearerAuth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a := r.Header.Get("Authorization")
			if a == "" || len(a) < 8 || a[:7] != "Bearer " || a[7:] != token {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid or missing bearer token"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) Start(ctx context.Context) error {
	handler := corsMiddleware(s.mux)
	addr := fmt.Sprintf(":%d", s.port)
	s.log.Info("API server listening", "addr", addr)

	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	return srv.ListenAndServe()
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CORS for WebSocket upgrade
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		// X-Client-Id is not a simple header, so its presence alone makes every
		// request preflight — the GETs included. Omitting it here blocks the whole
		// cross-origin dev setup, not merely the writes.
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Client-Id")
		// Without this a cross-origin download cannot read the filename the
		// server chose — which is every request under `make frontend-dev`, where
		// the SPA is served from :3000 and the API from :8088.
		w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
