package service

import (
	"context"
	"log/slog"
	"testing"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/dovod-app/app/internal/testdb"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

func setupTestDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := storage.NewDB(testdb.Config(t), slog.Default())
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// mockNotifier records events for assertions.
type mockNotifier struct {
	events []Event
}

func (m *mockNotifier) Notify(e Event) {
	m.events = append(m.events, e)
}

func (m *mockNotifier) reset() { m.events = nil }

func (m *mockNotifier) hasEvent(eventType string) bool {
	for _, e := range m.events {
		if e.Type == eventType {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }

// testAccess builds the guard every service is handed, for an instance with no
// accounts. Tests construct services by hand, so this is the one place the
// wiring lives for them.
func testAccess(db *bun.DB) *Access {
	return NewAccess(storage.NewTeamRepository(db), false)
}

// testAccessWithAccounts is the same guard on an instance that has `auth_enabled`
// set. The difference is only visible to a caller who is nobody: with accounts
// off that is the local single-binary owner, and with accounts on it is a
// stranger.
func testAccessWithAccounts(db *bun.DB) *Access {
	return NewAccess(storage.NewTeamRepository(db), true)
}

// createTestUser makes a user with the personal team registration would have
// given them. Without the team the account cannot create a research at all, so
// a test that skips it is testing a state the product never produces.
func createTestUser(t *testing.T, db *bun.DB, email, name string) *domain.User {
	t.Helper()
	ctx := context.Background()

	user := &domain.User{ID: uuid.New().String(), Email: email, PasswordHash: "x", Name: name}
	if err := storage.NewUserRepository(db).Create(ctx, user); err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	team := &domain.Team{ID: uuid.New().String(), Name: name, Personal: true, CreatedBy: user.ID}
	if err := storage.NewTeamRepository(db).CreateWithOwner(ctx, team, user.ID); err != nil {
		t.Fatalf("create personal team for %s: %v", email, err)
	}
	return user
}

// addToTeam puts a user in an existing team with a role, which is how a test
// builds the shared-team cases the ownership model never had.
func addToTeam(t *testing.T, db *bun.DB, teamID, userID string, role domain.TeamRole) {
	t.Helper()
	if err := storage.NewTeamRepository(db).AddMember(context.Background(), teamID, userID, role, ""); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

// createTestResearch is a helper that creates a research record for FK constraints.
func createTestResearch(t *testing.T, db *bun.DB) *domain.Research {
	t.Helper()
	ctx := context.Background()
	repo := storage.NewResearchRepository(db)
	svc := NewResearchService(repo, storage.NewSectionRepository(db), storage.NewTeamRepository(db), testAccess(db), &mockNotifier{}, slog.Default())
	r, _, err := svc.Create(ctx, CreateResearchRequest{
		Name:        "Test Research",
		Description: "A test research project",
		Goal:        "Testing",
	})
	if err != nil {
		t.Fatalf("create test research: %v", err)
	}
	return r
}

// createTestResearchWithSection creates a research with one section.
func createTestResearchWithSection(t *testing.T, db *bun.DB) (*domain.Research, *domain.Section) {
	t.Helper()
	ctx := context.Background()
	repo := storage.NewResearchRepository(db)
	svc := NewResearchService(repo, storage.NewSectionRepository(db), storage.NewTeamRepository(db), testAccess(db), &mockNotifier{}, slog.Default())
	r, sections, err := svc.Create(ctx, CreateResearchRequest{
		Name:        "Test Research",
		Description: "A test research project",
		Goal:        "Testing",
		Sections: []CreateSectionRequest{
			{Name: "section-1", DisplayName: "Section One", Description: "First section", Position: 1},
		},
	})
	if err != nil {
		t.Fatalf("create test research with section: %v", err)
	}
	return r, sections[0]
}
