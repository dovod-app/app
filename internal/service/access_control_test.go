package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/uptrace/bun"
)

// userCtx creates a context with the given user.
func userCtx(user *domain.User) context.Context {
	return auth.WithUser(context.Background(), user)
}

// setupTwoUsers creates two unrelated users, each with the personal team
// registration would have given them. They share no team, which is the case
// every test below is about.
func setupTwoUsers(t *testing.T, db *bun.DB) (*domain.User, *domain.User) {
	t.Helper()
	return createTestUser(t, db, "alice@test.com", "Alice"),
		createTestUser(t, db, "bob@test.com", "Bob")
}

func TestAccessControl_Research(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA := userCtx(userA)
	ctxB := userCtx(userB)

	// User A creates a research
	research, _, err := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research",
		Goal: "Testing access control",
		Sections: []CreateSectionRequest{
			{Name: "section-1", DisplayName: "Section 1"},
		},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}

	// Verify research is owned by user A
	if research.UserID != userA.ID {
		t.Fatalf("expected research.UserID=%s, got %s", userA.ID, research.UserID)
	}

	t.Run("owner can access research", func(t *testing.T) {
		r, err := researchSvc.Get(ctxA, research.ID)
		if err != nil {
			t.Fatalf("owner should access own research: %v", err)
		}
		if r.ID != research.ID {
			t.Fatal("wrong research returned")
		}
	})

	t.Run("other user cannot access research by ID", func(t *testing.T) {
		_, err := researchSvc.Get(ctxB, research.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot access research by code", func(t *testing.T) {
		_, err := researchSvc.Get(ctxB, research.Code)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("list only shows own researches", func(t *testing.T) {
		// User B creates their own research
		_, _, _ = researchSvc.Create(ctxB, CreateResearchRequest{Name: "Bob's Research", Goal: "Bob's goal"})

		listA, _ := researchSvc.List(ctxA, storage.ResearchFilter{})
		listB, _ := researchSvc.List(ctxB, storage.ResearchFilter{})

		if len(listA) != 1 {
			t.Fatalf("user A should see 1 research, got %d", len(listA))
		}
		if listA[0].Name != "Alice's Research" {
			t.Fatal("user A sees wrong research")
		}
		if len(listB) != 1 {
			t.Fatalf("user B should see 1 research, got %d", len(listB))
		}
		if listB[0].Name != "Bob's Research" {
			t.Fatal("user B sees wrong research")
		}
	})

	t.Run("other user cannot update research", func(t *testing.T) {
		newName := "Hacked!"
		_, err := researchSvc.Update(ctxB, research.ID, UpdateResearchRequest{Name: &newName})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot add section", func(t *testing.T) {
		_, err := researchSvc.AddSection(ctxB, research.ID, CreateSectionRequest{
			Name: "hacked-section", DisplayName: "Hacked",
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})
}

func TestAccessControl_Section(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	sectionSvc := NewSectionService(sectionRepo, entryRepo, researchRepo, testAccess(db), notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA := userCtx(userA)
	ctxB := userCtx(userB)

	research, sections, _ := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Test",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	section := sections[0]

	t.Run("owner can get section", func(t *testing.T) {
		s, err := sectionSvc.Get(ctxA, section.ID)
		if err != nil {
			t.Fatalf("owner should access section: %v", err)
		}
		if s.ID != section.ID {
			t.Fatal("wrong section")
		}
	})

	t.Run("other user cannot get section", func(t *testing.T) {
		_, err := sectionSvc.Get(ctxB, section.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot list sections", func(t *testing.T) {
		list, err := sectionSvc.List(ctxB, research.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) > 0 {
			t.Fatal("user B should not see sections from user A's research")
		}
	})

	t.Run("other user cannot update section", func(t *testing.T) {
		newName := "Hacked"
		_, err := sectionSvc.Update(ctxB, section.ID, UpdateSectionRequest{DisplayName: &newName})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})
}

func TestAccessControl_Entry(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA := userCtx(userA)
	ctxB := userCtx(userB)

	research, sections, _ := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Test",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	section := sections[0]

	// User A creates an entry
	entry, err := entrySvc.Create(ctxA, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  section.ID,
		Content:    "# Secret entry\n\nThis is private.",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	t.Run("other user cannot create entry in foreign research", func(t *testing.T) {
		_, err := entrySvc.Create(ctxB, CreateEntryRequest{
			ResearchID: research.ID,
			SectionID:  section.ID,
			Content:    "# Hacked",
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot get entry", func(t *testing.T) {
		_, err := entrySvc.Get(ctxB, entry.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot update entry", func(t *testing.T) {
		newTitle := "Hacked"
		_, err := entrySvc.Update(ctxB, entry.ID, UpdateEntryRequest{Title: &newTitle})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot list entries", func(t *testing.T) {
		list, err := entrySvc.List(ctxB, research.ID, section.ID, storage.EntryFilter{})
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) > 0 {
			t.Fatal("user B should not see entries from user A's research")
		}
	})

	t.Run("owner can access entry", func(t *testing.T) {
		e, err := entrySvc.Get(ctxA, entry.ID)
		if err != nil {
			t.Fatalf("owner should access entry: %v", err)
		}
		if e.ID != entry.ID {
			t.Fatal("wrong entry")
		}
	})
}

func TestAccessControl_Task(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	taskRepo := storage.NewTaskRepository(db)
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, log)
	taskSvc := NewTaskService(taskRepo, researchRepo, testAccess(db), entrySvc, notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA := userCtx(userA)
	ctxB := userCtx(userB)

	research, _, _ := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Test",
	})

	// User A creates a task
	task, err := taskSvc.Create(ctxA, CreateTaskRequest{
		ResearchID: research.ID,
		Title:      "Secret task",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	t.Run("other user cannot create task in foreign research", func(t *testing.T) {
		_, err := taskSvc.Create(ctxB, CreateTaskRequest{
			ResearchID: research.ID,
			Title:      "Hacked task",
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot get task", func(t *testing.T) {
		_, err := taskSvc.Get(ctxB, task.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot update task", func(t *testing.T) {
		newTitle := "Hacked"
		_, err := taskSvc.Update(ctxB, task.ID, UpdateTaskRequest{Title: &newTitle})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot delete task", func(t *testing.T) {
		err := taskSvc.Delete(ctxB, task.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot list tasks", func(t *testing.T) {
		list, err := taskSvc.List(ctxB, research.ID, storage.TaskFilter{})
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) > 0 {
			t.Fatal("user B should not see tasks from user A's research")
		}
	})

	t.Run("owner can access task", func(t *testing.T) {
		tk, err := taskSvc.Get(ctxA, task.ID)
		if err != nil {
			t.Fatalf("owner should access task: %v", err)
		}
		if tk.ID != task.ID {
			t.Fatal("wrong task")
		}
	})
}

func TestAccessControl_Session(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	questionRepo := storage.NewQuestionRepository(db)
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, log)
	sessionSvc := NewSessionService(db, sessionRepo, questionRepo, researchRepo, testAccess(db), entrySvc, notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA := userCtx(userA)
	ctxB := userCtx(userB)

	research, _, _ := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Test",
	})

	// User A creates a session
	session, _, err := sessionSvc.Create(ctxA, CreateSessionRequest{
		ResearchID: research.ID,
		Title:      "Interview",
		Questions: []CreateQuestionRequest{
			{Text: "What is X?", Priority: "high"},
		},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	t.Run("other user cannot create session in foreign research", func(t *testing.T) {
		_, _, err := sessionSvc.Create(ctxB, CreateSessionRequest{
			ResearchID: research.ID,
			Title:      "Hacked session",
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot get session", func(t *testing.T) {
		_, err := sessionSvc.Get(ctxB, session.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot update session", func(t *testing.T) {
		newTitle := "Hacked"
		_, err := sessionSvc.Update(ctxB, session.ID, UpdateSessionRequest{Title: &newTitle})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot list sessions", func(t *testing.T) {
		list, err := sessionSvc.ListByResearch(ctxB, research.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) > 0 {
			t.Fatal("user B should not see sessions from user A's research")
		}
	})

	t.Run("other user cannot add questions", func(t *testing.T) {
		_, err := sessionSvc.AddQuestions(ctxB, session.ID, []CreateQuestionRequest{
			{Text: "Hacked question"},
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("owner can access session", func(t *testing.T) {
		s, err := sessionSvc.Get(ctxA, session.ID)
		if err != nil {
			t.Fatalf("owner should access session: %v", err)
		}
		if s.Session.ID != session.ID {
			t.Fatal("wrong session")
		}
	})
}

func TestAccessControl_NoAuth(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)

	// Without auth context (backward compat) — should work fine
	ctx := context.Background()

	research, _, err := researchSvc.Create(ctx, CreateResearchRequest{
		Name: "Public Research", Goal: "No auth",
	})
	if err != nil {
		t.Fatalf("create research without auth: %v", err)
	}
	if research.UserID != "" {
		t.Fatalf("expected empty UserID, got %s", research.UserID)
	}

	// Should be accessible without auth
	r, err := researchSvc.Get(ctx, research.ID)
	if err != nil {
		t.Fatalf("should access research without auth: %v", err)
	}
	if r.ID != research.ID {
		t.Fatal("wrong research")
	}

	// List without auth should return all
	list, err := researchSvc.List(ctx, storage.ResearchFilter{})
	if err != nil {
		t.Fatalf("list without auth: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 research, got %d", len(list))
	}
}

// TestAccessControl_AnonymousCallerWithAccountsOn is the other half of
// TestAccessControl_NoAuth, and the two together are the whole rule: a caller
// with no identity is the owner of the instance when there are no accounts, and
// a stranger when there are.
//
// The transport that produces such a caller is stdio. It authenticates nobody —
// the client spawns the process and speaks a pipe — so `RunStdio` with
// `auth_enabled: true` and no `--default-user` put nobody in the context. The
// guard's no-caller rule then applied, and every MCP tool had owner rights over
// every research in the database, including the deletes added in #53.
func TestAccessControl_AnonymousCallerWithAccountsOn(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()
	access := testAccessWithAccounts(db)

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	teamRepo := storage.NewTeamRepository(db)
	researchSvc := NewResearchService(researchRepo, sectionRepo, teamRepo, access, notifier, log)
	sectionSvc := NewSectionService(sectionRepo, entryRepo, researchRepo, access, notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, access, nil,
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db),
		storage.NewCrossRefRepository(db), storage.NewExternalLinkRepository(db), notifier, log)

	user := createTestUser(t, db, "someone@test.com", "Someone")
	owner := userCtx(user)
	research, sections, err := researchSvc.Create(owner, CreateResearchRequest{
		Name: "Private", Goal: "g",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "Section one"}},
	})
	if err != nil {
		t.Fatalf("create research as a real user: %v", err)
	}
	if _, err := entrySvc.Create(owner, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID, Title: "Seed", Content: "body",
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// Nobody. This is exactly what an anonymous stdio session carries.
	anon := context.Background()

	// The guard itself, first: everything below is a consequence of this.
	if role, err := access.Role(anon, research.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Access.Role for an anonymous caller = (%q, %v), want ErrNotFound — "+
			"with accounts on there is no caller who is exempt", role, err)
	}
	if role := access.RoleOrEmpty(anon, research.ID); role != "" {
		t.Errorf("Access.RoleOrEmpty = %q, want empty — the UI draws a read-only screen from this", role)
	}

	refusals := map[string]func() error{
		"research get": func() error { _, err := researchSvc.Get(anon, research.ID); return err },
		"sections":     func() error { _, err := sectionSvc.List(anon, research.ID); return err },
		"entries":      func() error { _, err := entrySvc.ListByResearch(anon, research.ID, storage.EntryFilter{}); return err },
		"research update": func() error {
			_, err := researchSvc.Update(anon, research.ID, UpdateResearchRequest{Goal: ptr("theirs now")})
			return err
		},
	}
	for name, call := range refusals {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound — a stranger must not learn that this research exists", name, err)
		}
	}

	// The list does not go through the guard: with no user the member filter is
	// simply left unset, which is how "no caller" used to mean "every research
	// on the server" on this path.
	list, err := researchSvc.List(anon, storage.ResearchFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("list returned %d research(es) to an anonymous caller, want 0", len(list))
	}

	// Creating is refused too. Allowing it would file the research in the local
	// team, where the person who asked for it could not read it back.
	if _, _, err := researchSvc.Create(anon, CreateResearchRequest{Name: "Anon", Goal: "g"}); !errors.Is(err, ErrNoAuth) {
		t.Errorf("create as an anonymous caller: err = %v, want ErrNoAuth", err)
	}

	// Moving a research between teams is the highest-value write there is, and
	// TeamService held its own copy of the no-caller rule. It is unreachable
	// over HTTP — both routes are accessWrite — and there is no MCP tool for it,
	// so this test is what keeps it closed if either of those changes.
	teams := NewTeamService(teamRepo, storage.NewTeamInviteRepository(db),
		storage.NewUserRepository(db), researchRepo, access, notifier, log)
	if err := teams.TransferResearch(anon, research.ID, "team-local"); !errors.Is(err, ErrNoAuth) {
		t.Errorf("transfer as an anonymous caller: err = %v, want ErrNoAuth", err)
	}

	// And the same caller on an instance with no accounts is still the owner of
	// everything — the local single-binary mode this product started as.
	localOnly := NewResearchService(researchRepo, sectionRepo, teamRepo, testAccess(db), notifier, log)
	if _, err := localOnly.Get(anon, research.ID); err != nil {
		t.Errorf("with accounts off an anonymous caller must still read everything: %v", err)
	}
}

// TestAccessControl_AnonymousCallerAndTheTeamLibraries covers the half that
// service.Access does not: a skill or a methodology belongs to a *team*, not to
// a research, so those two services carry the no-caller rule themselves. Both
// copies said "nobody means everybody", which on an instance with accounts made
// every team's private library readable and writable from an anonymous stdio
// session.
func TestAccessControl_AnonymousCallerAndTheTeamLibraries(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()
	access := testAccessWithAccounts(db)
	teamRepo := storage.NewTeamRepository(db)
	skillRepo := storage.NewSkillRepository(db)

	skills := NewSkillService(skillRepo, storage.NewResearchRepository(db), teamRepo, access, notifier, log)
	templates := NewTemplateService(storage.NewTemplateRepository(db), skillRepo, teamRepo, access, log)
	teams := NewTeamService(teamRepo, storage.NewTeamInviteRepository(db), storage.NewUserRepository(db),
		storage.NewResearchRepository(db), access, notifier, log)

	user := createTestUser(t, db, "librarian@test.com", "Librarian")
	owner := userCtx(user)
	team, err := teams.Create(owner, "Shared")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := skills.CreateTeam(owner, team.ID, SkillInput{
		Name: "House style", Description: "How this team writes", Body: "secret",
	}); err != nil {
		t.Fatalf("seed team skill: %v", err)
	}

	anon := context.Background()
	refusals := map[string]func() error{
		"list a team's skills": func() error { _, err := skills.ListTeam(anon, team.ID); return err },
		"write a team skill": func() error {
			_, err := skills.CreateTeam(anon, team.ID, SkillInput{Name: "Mine", Description: "d", Body: "b"})
			return err
		},
		"list a team's templates": func() error { _, err := templates.ListByTeam(anon, team.ID); return err },
		"write a team template": func() error {
			_, err := templates.CreateTeam(anon, team.ID, TemplateInput{
				Name: "Mine", WhenToUse: "w", Body: "b",
			})
			return err
		},
	}
	for name, call := range refusals {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}

	// A methodology list is not scoped by a team id, so it cannot refuse — it
	// narrows instead, to what an instance-wide credential sees: the globals,
	// and nothing any team wrote.
	list, err := templates.List(anon)
	if err != nil {
		t.Fatalf("template list: %v", err)
	}
	for _, tp := range list {
		if tp.TeamID != "" {
			t.Errorf("template list gave an anonymous caller %q, which belongs to team %s", tp.Slug, tp.TeamID)
		}
	}
}

// TestAccessControl_Revisions covers the history surface added with entry
// revisions. A revision holds the entry's full text, so every one of these
// paths is a copy of the content the ownership check exists to protect — and
// two of them (diff, restore) can also mutate or describe an entry by id alone.
func TestAccessControl_Revisions(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	questionRepo := storage.NewQuestionRepository(db)

	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), sessionRepo, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, log)
	sessionSvc := NewSessionService(db, sessionRepo, questionRepo, researchRepo, testAccess(db), entrySvc, notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA := userCtx(userA)
	ctxB := userCtx(userB)

	research, sections, _ := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Test",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})

	session, _, err := sessionSvc.Create(ctxA, CreateSessionRequest{
		ResearchID: research.ID, Title: "Private session", Focus: "secrets",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	entry, err := entrySvc.Create(ctxA, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[0].ID,
		Content:    "# Secret entry\n\nThis is private.",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := entrySvc.Update(ctxA, entry.ID, UpdateEntryRequest{Content: ptr("# Secret entry\n\nStill private, now revised.")}); err != nil {
		t.Fatalf("update entry: %v", err)
	}

	t.Run("other user cannot list revisions", func(t *testing.T) {
		if _, _, err := entrySvc.History(ctxB, entry.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot read a revision", func(t *testing.T) {
		if _, err := entrySvc.Revision(ctxB, entry.ID, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot diff", func(t *testing.T) {
		if _, err := entrySvc.Diff(ctxB, entry.ID, 1, 2); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("other user cannot restore", func(t *testing.T) {
		if _, err := entrySvc.Restore(ctxB, entry.ID, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
		// And the entry is untouched.
		current, err := entrySvc.Get(ctxA, entry.ID)
		if err != nil {
			t.Fatalf("get entry: %v", err)
		}
		if !strings.Contains(current.Content, "now revised") {
			t.Fatalf("a refused restore still changed the entry: %q", current.Content)
		}
	})

	t.Run("other user cannot read session changes", func(t *testing.T) {
		if _, err := entrySvc.SessionChanges(ctxB, research.ID, session.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	// The leak this test exists for: entries.session_id is caller-supplied, and
	// the revision history resolves it to a code and a title. Pointing your own
	// entry at a session from someone else's research must not turn your own
	// history into a window onto theirs.
	t.Run("foreign session id on own entry is refused", func(t *testing.T) {
		otherResearch, otherSections, _ := researchSvc.Create(ctxB, CreateResearchRequest{
			Name: "Bob's Research", Goal: "Test",
			Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
		})
		bobSession, _, err := sessionSvc.Create(ctxB, CreateSessionRequest{
			ResearchID: otherResearch.ID, Title: "Acquisition talks with Acme", Focus: "secret",
		})
		if err != nil {
			t.Fatalf("create bob session: %v", err)
		}
		_ = otherSections

		// A writes to A's own entry, which is fully authorized — only the
		// session_id points somewhere they may not see.
		foreign := bobSession.ID
		_, err = entrySvc.Update(ctxA, entry.ID, UpdateEntryRequest{
			Content:   ptr("# Secret entry\n\nProbing."),
			SessionID: &foreign,
		})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected the write to be refused with ErrNotFound, got: %v", err)
		}

		// And even if such a row existed, the history must not resolve it.
		_, revs, err := entrySvc.History(ctxA, entry.ID)
		if err != nil {
			t.Fatalf("history: %v", err)
		}
		for _, rev := range revs {
			if rev.SessionTitle == "Acquisition talks with Acme" || rev.SessionID == bobSession.ID {
				t.Fatalf("revision r%d leaked another user's session: code=%q title=%q",
					rev.Revision, rev.SessionCode, rev.SessionTitle)
			}
		}
	})

	t.Run("session changes with a foreign session id returns nothing", func(t *testing.T) {
		otherResearch, _, _ := researchSvc.Create(ctxB, CreateResearchRequest{Name: "Bob's Other", Goal: "T"})
		bobSession, _, err := sessionSvc.Create(ctxB, CreateSessionRequest{
			ResearchID: otherResearch.ID, Title: "Bob's session", Focus: "x",
		})
		if err != nil {
			t.Fatalf("create bob session: %v", err)
		}

		// A's own research, B's session id: an empty result, not an error and
		// not another user's changes.
		changes, err := entrySvc.SessionChanges(ctxA, research.ID, bobSession.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(changes) != 0 {
			t.Fatalf("expected no changes, got %d", len(changes))
		}

		// B's research with A's session id: refused outright.
		if _, err := entrySvc.SessionChanges(ctxA, otherResearch.ID, session.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got: %v", err)
		}
	})

	t.Run("owner can read all of it", func(t *testing.T) {
		_, revs, err := entrySvc.History(ctxA, entry.ID)
		if err != nil || len(revs) != 2 {
			t.Fatalf("owner history: %d revisions, err=%v", len(revs), err)
		}
		if _, err := entrySvc.Diff(ctxA, entry.ID, 1, 2); err != nil {
			t.Fatalf("owner diff: %v", err)
		}
		changes, err := entrySvc.SessionChanges(ctxA, research.ID, session.ID)
		if err != nil {
			t.Fatalf("owner session changes: %v", err)
		}
		if len(changes) == 0 {
			t.Fatal("owner should see what the session changed")
		}
	})
}

func TestAccessControl_ObsidianVault(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	questionRepo := storage.NewQuestionRepository(db)
	taskRepo := storage.NewTaskRepository(db)
	roadmapRepo := storage.NewRoadmapRepository(db)
	roadmapNodeRepo := storage.NewRoadmapNodeRepository(db)
	roadmapEdgeRepo := storage.NewRoadmapEdgeRepository(db)
	revisionRepo := storage.NewEntryRevisionRepository(db)

	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	sectionSvc := NewSectionService(sectionRepo, entryRepo, researchRepo, testAccess(db), notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), sessionRepo, blockRepo, revisionRepo, crossrefRepo, nil, notifier, log)
	sessionSvc := NewSessionService(db, sessionRepo, questionRepo, researchRepo, testAccess(db), entrySvc, notifier, log)
	taskSvc := NewTaskService(taskRepo, researchRepo, testAccess(db), entrySvc, notifier, log)
	roadmapSvc := NewRoadmapService(roadmapRepo, roadmapNodeRepo, roadmapEdgeRepo, researchRepo, testAccess(db), notifier, log)
	obsidianSvc := NewObsidianService(researchSvc, sectionSvc, entryRepo, sessionSvc, taskSvc, roadmapSvc, revisionRepo, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA, ctxB := userCtx(userA), userCtx(userB)

	research, sections, _ := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Test",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if _, err := entrySvc.Create(ctxA, CreateEntryRequest{
		ResearchID: research.ID,
		SectionID:  sections[0].ID,
		Title:      "Secret",
		Content:    "This is private.",
	}); err != nil {
		t.Fatalf("create entry: %v", err)
	}

	t.Run("the owner gets a vault", func(t *testing.T) {
		v, err := obsidianSvc.Vault(ctxA, research.ID, DefaultVaultOptions())
		if err != nil {
			t.Fatalf("owner cannot export: %v", err)
		}
		if len(v.Files) == 0 {
			t.Fatal("owner's vault is empty")
		}
	})

	// An export is the widest read in the product: one call returns every entry,
	// session, question, task and roadmap at once. If ownership fails anywhere,
	// it fails here first.
	t.Run("another user gets nothing", func(t *testing.T) {
		for _, id := range []string{research.ID, research.Code} {
			v, err := obsidianSvc.Vault(ctxB, id, DefaultVaultOptions())
			if err == nil {
				t.Fatalf("exported another user's research by %q: %d files", id, len(v.Files))
			}
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("by %q: got %v, want ErrNotFound — a 403 would confirm the research exists", id, err)
			}
		}
	})
}

// TestAccessControl_ListQuestions covers the read path that had no owner check:
// question_list passes a caller-supplied session id straight to the service, so
// anyone holding another user's session UUID could read its questions and their
// answers.
func TestAccessControl_ListQuestions(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	questionRepo := storage.NewQuestionRepository(db)

	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), sessionRepo, blockRepo, storage.NewEntryRevisionRepository(db), crossrefRepo, nil, notifier, log)
	sessionSvc := NewSessionService(db, sessionRepo, questionRepo, researchRepo, testAccess(db), entrySvc, notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA, ctxB := userCtx(userA), userCtx(userB)

	research, _, _ := researchSvc.Create(ctxA, CreateResearchRequest{Name: "Alice's Research", Goal: "Test"})
	session, _, err := sessionSvc.Create(ctxA, CreateSessionRequest{
		ResearchID: research.ID,
		Title:      "Private session",
		Questions:  []CreateQuestionRequest{{Text: "ALICE PRIVATE QUESTION", Priority: domain.PriorityHigh}},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	t.Run("the owner reads their questions", func(t *testing.T) {
		qs, err := sessionSvc.ListQuestions(ctxA, session.ID, storage.QuestionFilter{})
		if err != nil || len(qs) != 1 {
			t.Fatalf("owner got %d questions, err %v", len(qs), err)
		}
	})

	t.Run("another user holding the session id gets nothing", func(t *testing.T) {
		qs, err := sessionSvc.ListQuestions(ctxB, session.ID, storage.QuestionFilter{})
		if err == nil {
			t.Fatalf("read another user's questions: %d returned", len(qs))
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
		if len(qs) != 0 {
			t.Errorf("a refused read still returned %d questions", len(qs))
		}
	})
}

// Skills are the newest thing a research owns, and the only entity whose write
// methods are addressed by id as well as by slug — so a stranger holding an id
// is a shape none of the tests above cover. Everything here must be
// indistinguishable from a research and a skill that do not exist.
func TestAccessControl_Skill(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()
	access := testAccess(db)

	researchRepo := storage.NewResearchRepository(db)
	teamRepo := storage.NewTeamRepository(db)
	skillRepo := storage.NewSkillRepository(db)
	researchSvc := NewResearchService(researchRepo, storage.NewSectionRepository(db), teamRepo, access, notifier, log)
	skillSvc := NewSkillService(skillRepo, researchRepo, teamRepo, access, notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA, ctxB := userCtx(userA), userCtx(userB)

	research, _, err := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Testing access control",
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}
	private, err := skillSvc.CreatePrivate(ctxA, research.ID, SkillInput{
		Name:        "House rule",
		Description: "Use when writing anything into Alice's research.",
		Body:        "Alice's methodology, which Bob must not read.",
	})
	if err != nil {
		t.Fatalf("create private skill: %v", err)
	}
	aliceTeam, err := researchSvc.Get(ctxA, research.ID)
	if err != nil {
		t.Fatalf("read research: %v", err)
	}
	teamSkill, err := skillSvc.CreateTeam(ctxA, aliceTeam.TeamID, SkillInput{
		Name:        "Team method",
		Description: "Use when Alice's team needs one way of doing this.",
		Body:        "Alice's team's methodology.",
	})
	if err != nil {
		t.Fatalf("create team skill: %v", err)
	}

	// Bob's own research, so the "point a stolen slug at somewhere I can write"
	// cases have somewhere to be pointed.
	bobs, _, err := researchSvc.Create(ctxB, CreateResearchRequest{Name: "Bob's Research", Goal: "His own"})
	if err != nil {
		t.Fatalf("create bob's research: %v", err)
	}

	notFound := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: got %v, want ErrNotFound — anything else confirms the row exists", name, err)
		}
	}

	t.Run("a stranger reads nothing", func(t *testing.T) {
		_, err := skillSvc.ListAttached(ctxB, research.ID)
		notFound("ListAttached", err)
		_, err = skillSvc.ListLibrary(ctxB, research.ID, "")
		notFound("ListLibrary", err)
		_, err = skillSvc.Load(ctxB, research.ID, private.Slug)
		notFound("Load", err)
		_, err = skillSvc.ResolveSlug(ctxB, research.ID, private.Slug)
		notFound("ResolveSlug", err)
		_, err = skillSvc.ListTeam(ctxB, aliceTeam.TeamID)
		notFound("ListTeam", err)
		if idx := skillSvc.Index(ctxB, research.ID); len(idx) != 0 {
			t.Errorf("Index returned %d skills to a stranger", len(idx))
		}
	})

	t.Run("a stranger holding an id reads nothing", func(t *testing.T) {
		// The id is the address Update and Delete take, and skill_list now
		// hands ids out — so a stranger holding one is a real shape, not a
		// hypothetical.
		for name, id := range map[string]string{"private": private.ID, "team": teamSkill.ID} {
			sk, err := skillSvc.Read(ctxB, id)
			notFound("Read "+name, err)
			if sk != nil {
				t.Errorf("Read %s returned a skill to a stranger", name)
			}
		}
	})

	t.Run("a stranger writes nothing", func(t *testing.T) {
		_, err := skillSvc.Attach(ctxB, research.ID, private.Slug, false)
		notFound("Attach", err)
		notFound("Detach", skillSvc.Detach(ctxB, research.ID, private.Slug))
		_, err = skillSvc.CreatePrivate(ctxB, research.ID, SkillInput{
			Name: "Bob's rule", Description: "Use when Bob wants in.", Body: "x",
		})
		notFound("CreatePrivate", err)
		_, err = skillSvc.CreateTeam(ctxB, aliceTeam.TeamID, SkillInput{
			Name: "Bob's method", Description: "Use when Bob wants in.", Body: "x",
		})
		notFound("CreateTeam", err)
		_, err = skillSvc.CopyHere(ctxB, research.ID, private.Slug)
		notFound("CopyHere", err)
		_, err = skillSvc.Promote(ctxB, research.ID, private.Slug)
		notFound("Promote", err)
		for name, id := range map[string]string{"private": private.ID, "team": teamSkill.ID} {
			_, err := skillSvc.Update(ctxB, id, SkillInput{
				Name: "Rewritten", Description: "Use when Bob has taken over.", Body: "mine now",
			})
			notFound("Update "+name, err)
			notFound("Delete "+name, skillSvc.Delete(ctxB, id))
		}
	})

	t.Run("a stolen slug pointed at the stranger's own research finds nothing", func(t *testing.T) {
		// Bob has full write here, so the only thing standing between him and
		// Alice's methodology is that the slug does not resolve in his scope.
		for _, slug := range []string{private.Slug, teamSkill.Slug} {
			_, err := skillSvc.Load(ctxB, bobs.ID, slug)
			notFound("Load "+slug, err)
			_, err = skillSvc.Attach(ctxB, bobs.ID, slug, false)
			notFound("Attach "+slug, err)
			_, err = skillSvc.CopyHere(ctxB, bobs.ID, slug)
			notFound("CopyHere "+slug, err)
		}
	})

	t.Run("Alice still has everything she started with", func(t *testing.T) {
		sk, err := skillSvc.Load(ctxA, research.ID, private.Slug)
		if err != nil {
			t.Fatalf("owner load: %v", err)
		}
		if !strings.Contains(sk.Body, "Bob must not read") {
			t.Errorf("body = %q, want the original", sk.Body)
		}
	})
}

// A list of what did NOT resolve is a list of what did.
//
// The import preview reports unresolvable `[[...]]` codes, and short codes are
// global: resolving them reaches every research on the instance. Reporting the
// raw resolution bit turned a file full of `[[R1]] [[R1:E1]] [[R1:E2]]…`,
// uploaded into the caller's own section, into an enumeration of somebody
// else's code space. VisibleCrossRefs exists to close this and the preview was
// the one consumer that skipped it.
func TestAccessControl_ImportPreviewDoesNotOracleForeignCodes(t *testing.T) {
	db := setupTestDB(t)
	log := slog.Default()
	access := testAccess(db)
	notifier := &mockNotifier{}

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), access, notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, access, storage.NewSessionRepository(db),
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db),
		storage.NewCrossRefRepository(db), storage.NewExternalLinkRepository(db), notifier, log)

	alice, mallory := setupTwoUsers(t, db)
	ctxA, ctxM := userCtx(alice), userCtx(mallory)

	hers, herSections, err := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Private",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create alice's research: %v", err)
	}
	secret, err := entrySvc.Create(ctxA, CreateEntryRequest{
		ResearchID: hers.ID, SectionID: herSections[0].ID,
		Title: "Private findings", Content: "Nothing Mallory may see.",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	// Mallory's own research, where she legitimately has write access.
	mine, mySections, err := researchSvc.Create(ctxM, CreateResearchRequest{
		Name: "Mallory's Research", Goal: "Probing",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create mallory's research: %v", err)
	}
	_ = mine

	probe := "# probe\n\n" +
		"[[" + hers.Code + "]] " +
		"[[" + hers.Code + ":" + secret.Code + "]] " +
		"[[R9999]] [[R9999:E1]]\n"
	got, err := entrySvc.PreviewMarkdownImport(ctxM, mySections[0].ID, "probe.md", []byte(probe))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	reported := map[string]bool{}
	for _, r := range got.UnresolvedRefs {
		reported[r.Ref] = true
	}
	// Everything Mallory cannot read must come back as unresolved, exactly as
	// unresolved as the codes that genuinely name nothing.
	for _, ref := range []string{hers.Code, hers.Code + ":" + secret.Code, "R9999", "R9999:E1"} {
		if !reported[ref] {
			t.Errorf("%q was not reported as unresolved, so its existence leaked", ref)
		}
	}

	// And the owner still gets the truth: her own codes resolve.
	ownerView, err := entrySvc.PreviewMarkdownImport(ctxA, herSections[0].ID, "probe.md", []byte(probe))
	if err != nil {
		t.Fatalf("owner preview: %v", err)
	}
	for _, r := range ownerView.UnresolvedRefs {
		if r.Ref == hers.Code || r.Ref == hers.Code+":"+secret.Code {
			t.Errorf("the owner's own %q was reported unresolved", r.Ref)
		}
	}
}

// A mark is addressed by a bare id, with no research in the URL, so the refusal
// it returns is the only thing a stranger learns from. Both halves matter: they
// must not reach it, and they must not be told which of "no such mark" and "not
// yours" it was.
func TestAccessControl_Annotation(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	annotationRepo := storage.NewAnnotationRepository(db)
	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), nil,
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db),
		storage.NewCrossRefRepository(db), nil, notifier, log)
	annSvc := NewAnnotationService(annotationRepo, entryRepo, storage.NewEntryRevisionRepository(db),
		testAccess(db), entrySvc, entrySvc, notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA, ctxB := userCtx(userA), userCtx(userB)

	research, sections, err := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Test",
		Sections: []CreateSectionRequest{{Name: "s", DisplayName: "S"}},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}
	entry, err := entrySvc.Create(ctxA, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Title: "Doc", Content: "Costs fall by 40 percent.",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	mark, err := annSvc.Create(ctxA, CreateAnnotationRequest{
		EntryID: entry.ID, Quote: domain.Quote{Exact: "Costs fall"}, Kind: domain.AnnotationVerify,
	})
	if err != nil {
		t.Fatalf("create annotation: %v", err)
	}

	t.Run("B reaches nothing of A's", func(t *testing.T) {
		if _, err := annSvc.Get(ctxB, mark.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get = %v, want ErrNotFound", err)
		}
		if _, err := annSvc.ListByEntry(ctxB, entry.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("ListByEntry = %v, want ErrNotFound", err)
		}
		if _, err := annSvc.ListByResearch(ctxB, research.ID, storage.AnnotationFilter{}); !errors.Is(err, ErrNotFound) {
			t.Errorf("ListByResearch = %v, want ErrNotFound", err)
		}
		if _, err := annSvc.Counts(ctxB, research.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("Counts = %v, want ErrNotFound", err)
		}
		if _, err := annSvc.Create(ctxB, CreateAnnotationRequest{
			EntryID: entry.ID, Quote: domain.Quote{Exact: "Costs fall"}, Kind: domain.AnnotationDig,
		}); !errors.Is(err, ErrNotFound) {
			t.Errorf("Create = %v, want ErrNotFound", err)
		}
		if _, err := annSvc.Update(ctxB, mark.ID, UpdateAnnotationRequest{Body: ptr("mine now")}); !errors.Is(err, ErrNotFound) {
			t.Errorf("Update = %v, want ErrNotFound", err)
		}
		if _, err := annSvc.Answer(ctxB, mark.ID, AnswerAnnotationRequest{Resolution: "x"}); !errors.Is(err, ErrNotFound) {
			t.Errorf("Answer = %v, want ErrNotFound", err)
		}
		if err := annSvc.Delete(ctxB, mark.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("Delete = %v, want ErrNotFound", err)
		}
	})

	// The refusal must not distinguish a real id from an invented one, or a
	// guessed uuid becomes a confirmed one — and Bulk answers up to sixty at a
	// time with a message per row.
	t.Run("a real id and an invented one refuse identically", func(t *testing.T) {
		_, real := annSvc.Get(ctxB, mark.ID)
		_, fake := annSvc.Get(ctxB, "00000000-0000-0000-0000-000000000000")
		if real.Error() != fake.Error() {
			t.Errorf("refusals differ: %q vs %q", real, fake)
		}
	})

	t.Run("bulk refuses rows outside its research", func(t *testing.T) {
		other, _, err := researchSvc.Create(ctxB, CreateResearchRequest{Name: "Bob's", Goal: "T"})
		if err != nil {
			t.Fatalf("create research: %v", err)
		}
		if _, err := annSvc.Bulk(ctxB, other.ID, []string{mark.ID}, domain.AnnotationClosed, ""); err != nil {
			t.Fatalf("bulk: %v", err)
		}
		fresh, err := annotationRepo.FindByID(context.Background(), mark.ID)
		if err != nil || fresh == nil {
			t.Fatalf("read back: %v", err)
		}
		if fresh.Status != domain.AnnotationOpen {
			t.Errorf("status = %q, want open — a mark was closed through another research's route", fresh.Status)
		}
	})

	t.Run("A still reaches their own", func(t *testing.T) {
		got, err := annSvc.Get(ctxA, mark.ID)
		if err != nil || got == nil {
			t.Fatalf("owner refused their own mark: %v", err)
		}
	})
}

// TestAccessControl_EntryMarkdownExport covers the second whole-body renderer.
//
// `GET /api/entries/{id}/markdown` returns a complete document by entry id, and
// with `task_ref` it now reads a SECOND table — the research's tasks — to turn
// references into titled rows. The repository read has no check of its own; it
// is safe only because `s.Get` has already asked `Access.Read` about the same
// research. Pin that here, so moving the read above the check fails a test
// rather than a share link.
func TestAccessControl_EntryMarkdownExport(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	blockRepo := storage.NewBlockRepository(db)
	crossrefRepo := storage.NewCrossRefRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	revisionRepo := storage.NewEntryRevisionRepository(db)
	taskRepo := storage.NewTaskRepository(db)

	researchSvc := NewResearchService(researchRepo, sectionRepo, storage.NewTeamRepository(db), testAccess(db), notifier, log)
	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, testAccess(db), sessionRepo, blockRepo, revisionRepo, crossrefRepo, nil, notifier, log)
	entrySvc.SetTaskRepo(taskRepo)
	taskSvc := NewTaskService(taskRepo, researchRepo, testAccess(db), entrySvc, notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA, ctxB := userCtx(userA), userCtx(userB)

	research, sections, _ := researchSvc.Create(ctxA, CreateResearchRequest{
		Name: "Alice's Research", Goal: "Test",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	task, err := taskSvc.Create(ctxA, CreateTaskRequest{ResearchID: research.ID, Title: "Alice's private task"})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	entry, err := entrySvc.Create(ctxA, CreateEntryRequest{
		ResearchID: research.ID, SectionID: sections[0].ID,
		Type: domain.EntryBlocks, Title: "Plan",
		Content: `{"version":1,"blocks":[{"type":"task_ref","data":{"tasks":["` + task.Code + `"]}}]}`,
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	t.Run("the owner gets the file, with the task resolved", func(t *testing.T) {
		f, err := entrySvc.MarkdownExport(ctxA, entry.ID)
		if err != nil {
			t.Fatalf("owner cannot export: %v", err)
		}
		if !strings.Contains(f.Content, "Alice's private task") {
			t.Errorf("the task_ref did not resolve for its owner:\n%s", f.Content)
		}
	})

	t.Run("another user gets nothing", func(t *testing.T) {
		f, err := entrySvc.MarkdownExport(ctxB, entry.ID)
		if err == nil {
			t.Fatalf("exported another user's document:\n%s", f.Content)
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound — a 403 would confirm the entry exists", err)
		}
	})

	t.Run("a share visitor gets nothing", func(t *testing.T) {
		// Refused at the top of MarkdownExport rather than only by the route
		// being absent from the shared sub-mux — the file is a second way to a
		// whole document, and provenance is never shared.
		visitor := auth.WithShare(context.Background(), &auth.Share{ID: "s1", ResearchID: research.ID})
		if f, err := entrySvc.MarkdownExport(visitor, entry.ID); err == nil {
			t.Fatalf("a share link downloaded a document file:\n%s", f.Content)
		} else if !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})
}

// Roadmap ids, node ids and the node ids inside edges and parent links are all
// handed to the service bare. Each must stop at the research boundary.
func TestAccessControl_Roadmap(t *testing.T) {
	db := setupTestDB(t)
	notifier := &mockNotifier{}
	log := slog.Default()
	researchRepo := storage.NewResearchRepository(db)
	researchSvc := NewResearchService(researchRepo, storage.NewSectionRepository(db), storage.NewTeamRepository(db), testAccess(db), notifier, log)
	roadmapSvc := NewRoadmapService(storage.NewRoadmapRepository(db), storage.NewRoadmapNodeRepository(db),
		storage.NewRoadmapEdgeRepository(db), researchRepo, testAccess(db), notifier, log)

	userA, userB := setupTwoUsers(t, db)
	ctxA, ctxB := userCtx(userA), userCtx(userB)

	researchA, _, _ := researchSvc.Create(ctxA, CreateResearchRequest{Name: "Alice's Research", Goal: "Test"})
	researchB, _, _ := researchSvc.Create(ctxB, CreateResearchRequest{Name: "Bob's Research", Goal: "Test"})

	rmA, err := roadmapSvc.Create(ctxA, CreateRoadmapRequest{ResearchID: researchA.ID, Title: "Alice's plan",
		Nodes: []CreateRoadmapNodeRequest{{TempID: "a", Title: "Secret step"}}})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	nodeA := rmA.Nodes[0].ID
	rmB, err := roadmapSvc.Create(ctxB, CreateRoadmapRequest{ResearchID: researchB.ID, Title: "Bob's plan",
		Nodes: []CreateRoadmapNodeRequest{{TempID: "b", Title: "Own step"}}})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	nodeB := rmB.Nodes[0].ID

	refused := map[string]func() error{
		"get":  func() error { _, err := roadmapSvc.Get(ctxB, rmA.ID); return err },
		"list": func() error { _, err := roadmapSvc.List(ctxB, researchA.ID); return err },
		"update": func() error {
			_, err := roadmapSvc.Update(ctxB, rmA.ID, UpdateRoadmapRequest{Title: ptr("Hacked")})
			return err
		},
		"delete": func() error { return roadmapSvc.Delete(ctxB, rmA.ID) },
		"add nodes": func() error {
			_, err := roadmapSvc.AddNodes(ctxB, rmA.ID, []CreateRoadmapNodeRequest{{Title: "X"}}, nil)
			return err
		},
		"update node": func() error {
			_, err := roadmapSvc.UpdateNode(ctxB, nodeA, UpdateRoadmapNodeRequest{Title: ptr("Hacked")})
			return err
		},
		"remove nodes": func() error { return roadmapSvc.RemoveNodes(ctxB, rmA.ID, []string{nodeA}) },
		"remove a foreign node through one's own roadmap": func() error {
			return roadmapSvc.RemoveNodes(ctxB, rmB.ID, []string{nodeA})
		},
		"edge from one's own roadmap to a foreign node": func() error {
			_, err := roadmapSvc.AddNodes(ctxB, rmB.ID, nil, []CreateRoadmapEdgeRequest{{SourceNodeRef: nodeB, TargetNodeRef: nodeA}})
			return err
		},
		"foreign node as parent": func() error {
			_, err := roadmapSvc.UpdateNode(ctxB, nodeB, UpdateRoadmapNodeRequest{ParentID: ptr(nodeA)})
			return err
		},
	}
	for name, call := range refused {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: expected ErrNotFound, got %v", name, err)
		}
	}

	got, err := roadmapSvc.Get(ctxA, rmA.ID)
	if err != nil {
		t.Fatalf("owner get: %v", err)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Title != "Secret step" || len(got.Edges) != 0 {
		t.Errorf("Alice's roadmap was changed by refused calls: %+v", got)
	}
}
