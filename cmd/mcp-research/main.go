package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/dovod-app/app/internal/api"
	"github.com/dovod-app/app/internal/api/ws"
	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/config"
	"github.com/dovod-app/app/internal/domain"
	mcpserver "github.com/dovod-app/app/internal/mcp"
	"github.com/dovod-app/app/internal/service"
	"github.com/dovod-app/app/internal/storage"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cfg := config.Load()

	if cfg.Version {
		fmt.Printf("mcp-research %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	logLevel := parseLogLevel(cfg.LogLevel)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	db, err := storage.NewDB(cfg, log)
	if err != nil {
		log.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Backfill short codes for any records missing them (after migrations)
	if n, err := storage.BackfillCodes(context.Background(), db); err != nil {
		log.Error("failed to backfill codes", "error", err)
	} else if n > 0 {
		log.Info("backfilled short codes", "count", n)
	}

	// WebSocket hub + event notifier
	hub := ws.NewHub(log)
	events := ws.NewHubNotifier(hub)

	// Repositories
	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	questionRepo := storage.NewQuestionRepository(db)
	taskRepo := storage.NewTaskRepository(db)
	revisionRepo := storage.NewEntryRevisionRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	annotationRepo := storage.NewAnnotationRepository(db)
	externalLinkRepo := storage.NewExternalLinkRepository(db)
	roadmapRepo := storage.NewRoadmapRepository(db)
	roadmapNodeRepo := storage.NewRoadmapNodeRepository(db)
	roadmapEdgeRepo := storage.NewRoadmapEdgeRepository(db)
	teamRepo := storage.NewTeamRepository(db)
	teamInviteRepo := storage.NewTeamInviteRepository(db)
	shareRepo := storage.NewShareRepository(db)
	skillRepo := storage.NewSkillRepository(db)
	templateRepo := storage.NewTemplateRepository(db)
	userRepo := storage.NewUserRepository(db)

	// Every service asks the same guard what the caller may do, so there is one
	// place to get authorization wrong instead of eight.
	access := service.NewAccess(teamRepo)

	// The hub asks the same guard the HTTP layer does, on every event, so a
	// membership taken away stops the updates on a socket already open.
	hub.SetAuthorizer(access, cfg.AuthEnabled)

	// Events name the research by id; every URL in the web UI names it by short
	// code. The hub resolves one to the other so a page has both.
	hub.SetCodeLookup(service.NewResearchCodes(researchRepo))

	// Services
	researchSvc := service.NewResearchService(researchRepo, sectionRepo, teamRepo, access, events, log)
	sectionSvc := service.NewSectionService(sectionRepo, entryRepo, researchRepo, access, events, log)
	entrySvc := service.NewEntryService(entryRepo, sectionRepo, researchRepo, access, sessionRepo, blockRepo, revisionRepo, crossrefRepo, externalLinkRepo, events, log)
	entrySvc.SetRoadmapRepos(roadmapRepo, roadmapNodeRepo)
	entrySvc.SetTaskRepo(taskRepo)
	// So the last-resort rebuild reaches answers as well as documents.
	entrySvc.SetQuestionRepo(questionRepo)
	entrySvc.SetRevisionLimit(cfg.RevisionLimit)
	sessionSvc := service.NewSessionService(db, sessionRepo, questionRepo, researchRepo, access, entrySvc, events, log)
	taskSvc := service.NewTaskService(taskRepo, researchRepo, access, entrySvc, events, log)
	annotationSvc := service.NewAnnotationService(annotationRepo, entryRepo, revisionRepo, access, entrySvc, entrySvc, events, log)
	// An entry write now reports which marks it drifted or orphaned. Wired after
	// the service exists, and optional: without this the server behaves exactly
	// as it did before annotations.
	entrySvc.SetAnnotations(annotationRepo)
	roadmapSvc := service.NewRoadmapService(roadmapRepo, roadmapNodeRepo, roadmapEdgeRepo, researchRepo, access, events, log)
	roadmapSvc.SetRefResolvers(entryRepo, taskRepo, sessionRepo, questionRepo, sectionRepo)
	// A reference written before its target existed is the normal order, not an
	// edge case. Every service that brings a referenceable thing into being
	// tells the reference table about it, so `[[E20]]` written yesterday points
	// at E20 the moment E20 is created — rather than staying dangling until
	// somebody found the rebuild route. Wired here because EntryService owns the
	// table and is built after two of the three.
	researchSvc.SetDanglingResolver(entrySvc)
	taskSvc.SetDanglingResolver(entrySvc)
	roadmapSvc.SetDanglingResolver(entrySvc)
	exportSvc := service.NewExportService(researchSvc, sectionSvc, entrySvc, entryRepo, sessionSvc, taskSvc, roadmapSvc, log)
	// A portable dump is a move, and a move that drops the marks discards the
	// only record of what somebody did not believe.
	exportSvc.SetAnnotations(annotationRepo)
	obsidianSvc := service.NewObsidianService(researchSvc, sectionSvc, entryRepo, sessionSvc, taskSvc, roadmapSvc, revisionRepo, log)
	teamSvc := service.NewTeamService(teamRepo, teamInviteRepo, userRepo, researchRepo, events, log)
	shareSvc := service.NewShareService(shareRepo, access, events, log)
	skillSvc := service.NewSkillService(skillRepo, researchRepo, teamRepo, access, events, log)

	// The skills we ship are refreshed on every boot, so an upgrade updates
	// them. It only ever touches rows with no team and no research: a team that
	// forked one owns a separate row, and overwriting somebody's edits on
	// upgrade is the failure this whole tier exists to avoid.
	if n, err := skillSvc.LoadBuiltinSkills(context.Background()); err != nil {
		log.Error("failed to load built-in skills", "error", err)
	} else if n > 0 {
		log.Info("loaded built-in skills", "count", n)
	}

	// Templates load after skills, and not by accident: a template names the
	// skills it attaches, and the loader refuses one naming a skill that does
	// not exist. Reversing the order would fail every boot.
	templateSvc := service.NewTemplateService(templateRepo, skillRepo, teamRepo, access, log)
	templateSvc.SetSkillService(skillSvc)
	if n, err := templateSvc.LoadBuiltinTemplates(context.Background()); err != nil {
		log.Error("failed to load built-in templates", "error", err)
	} else if n > 0 {
		log.Info("loaded built-in templates", "count", n)
	}

	// A shared page watches the research update live, which is the single most
	// compelling thing this product does — a share that is a frozen snapshot
	// throws that away. The hub re-resolves the link on its own timer, so
	// revoking one closes the sockets it opened.
	hub.SetShareValidator(shareSvc)

	// Auth (optional)
	var authSvc *service.AuthService
	var oauthSvc *service.OAuthService
	var defaultUser *domain.User
	var autoLoginToken string
	if cfg.AuthEnabled {
		if cfg.JWTSecret == "" {
			cfg.JWTSecret = generateRandomSecret()
			log.Warn("no jwt_secret configured, generated random secret (will change on restart)")
		}

		apiKeyRepo := storage.NewAPIKeyRepository(db)
		oauthRepo := storage.NewOAuthRepository(db)
		jwtMgr := auth.NewJWTManager(cfg.JWTSecret, 30*24*time.Hour)

		authSvc = service.NewAuthService(userRepo, apiKeyRepo, oauthRepo, researchRepo, teamRepo, jwtMgr, cfg.AllowRegistration, log)
		oauthSvc = service.NewOAuthService(oauthRepo, log)

		// Resolve or auto-create default user
		if cfg.DefaultUser != "" {
			u, err := userRepo.FindByEmail(context.Background(), cfg.DefaultUser)
			if err != nil {
				log.Error("failed to find default user", "email", cfg.DefaultUser, "error", err)
				os.Exit(1)
			}
			if u == nil {
				// Auto-create for local development
				u, _, err = authSvc.Register(context.Background(), cfg.DefaultUser, generateRandomSecret(), "Default User")
				if err != nil {
					log.Error("failed to auto-create default user", "email", cfg.DefaultUser, "error", err)
					os.Exit(1)
				}
				log.Info("auto-created default user", "email", cfg.DefaultUser, "id", u.ID)
			}
			defaultUser = u
			// Generate auto-login token for Web UI
			if token, err := jwtMgr.Generate(u.ID); err == nil {
				autoLoginToken = token
			}
			log.Info("default user configured", "email", u.Email, "id", u.ID)
		}
	}

	// The continuation summary. It owns no entity of its own — it reads the
	// repositories the other services write — so it is built here from the
	// pieces rather than being handed a service to wrap.
	resumeSvc := service.NewResumeService(researchSvc, sessionRepo, taskRepo, questionRepo,
		annotationRepo, entryRepo, revisionRepo, access, log)

	// MCP Server
	srv := mcpserver.NewServer(researchSvc, sectionSvc, entrySvc, sessionSvc, taskSvc, roadmapSvc, exportSvc, teamSvc, skillSvc, templateSvc, annotationSvc, resumeSvc, log, version)
	srv.SetBaseURL(cfg.BaseURL)

	log.Info("mcp-research started",
		"version", version,
		"transport", cfg.Transport,
		"web_port", cfg.WebPort,
		"db", cfg.DBPath,
		"db_driver", db.Dialect().Name().String(),
		"auth_enabled", cfg.AuthEnabled,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start REST API + WebSocket server in background
	apiCfg := api.ServerConfig{
		Port:           cfg.WebPort,
		IsInMemory:     cfg.DatabaseInMemory(),
		APIToken:       cfg.APIToken,
		AuthEnabled:    cfg.AuthEnabled,
		BaseURL:        cfg.BaseURL,
		OAuthSvc:       oauthSvc,
		AutoLoginToken: autoLoginToken,
		MCPHandler:     srv.StreamableHTTPHandler(),
		Version:        version,
	}
	apiSrv := api.NewServer(apiCfg, researchSvc, sectionSvc, entrySvc, sessionSvc, taskSvc, roadmapSvc, exportSvc, obsidianSvc, teamSvc, shareSvc, skillSvc, templateSvc, annotationSvc, access, authSvc, db, entryRepo, researchRepo, crossrefRepo, externalLinkRepo, hub, log)
	go func() {
		if err := apiSrv.Start(ctx); err != nil {
			log.Error("API server error", "error", err)
		}
	}()

	// Run MCP server (blocking)
	switch cfg.Transport {
	case "sse":
		if err := srv.RunSSE(ctx, cfg.MCPPort, authSvc, cfg.APIToken, cfg.BaseURL); err != nil {
			log.Error("SSE server error", "error", err)
			os.Exit(1)
		}
	default:
		if err := srv.RunStdio(ctx, defaultUser); err != nil {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func generateRandomSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
