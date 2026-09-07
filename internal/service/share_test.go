package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dovod-app/app/internal/auth"
	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
)

// A share link is the first time data in this product leaves the owner
// boundary by design, so these tests are written from the outside in: what can
// somebody holding a URL reach, and what happens to that URL over its life.

type shareKit struct {
	*roleKit
	shares *ShareService
	repo   *storage.ShareRepository
}

func newShareKit(t *testing.T) *shareKit {
	t.Helper()
	k := newRoleKit(t)
	repo := storage.NewShareRepository(k.db)
	return &shareKit{
		roleKit: k,
		shares:  NewShareService(repo, testAccess(k.db), k.events, slog.Default()),
		repo:    repo,
	}
}

// visit is what a request from a shared page looks like once the middleware has
// done its work: a context with a capability in it and no user at all.
func visit(t *testing.T, k *shareKit, token string) context.Context {
	t.Helper()
	share, err := k.shares.Resolve(context.Background(), token, "")
	if err != nil {
		t.Fatalf("resolve share: %v", err)
	}
	return auth.WithShare(context.Background(), Capability(share))
}

func allIncluded() domain.ShareInclude {
	return domain.ShareInclude{Sessions: true, Tasks: true, Roadmaps: true, Export: true}
}

func TestShare_TokenIsShownOnceAndStoredHashed(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, _, _ := k.sharedResearch(t, domain.TeamViewer)

	result, err := k.shares.Create(owner, research.ID, CreateShareRequest{Label: "Client review"})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	if result.Token == "" {
		t.Fatal("create returned no token")
	}

	// The plaintext must not be recoverable from anything the product will
	// hand back later: a leaked database is the whole reason for hashing it.
	stored, err := k.repo.FindByHash(context.Background(), auth.HashAPIKey(result.Token))
	if err != nil || stored == nil {
		t.Fatalf("share not findable by hash: %v", err)
	}
	listed, err := k.shares.List(owner, research.ID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 share, got %d", len(listed))
	}
	if _, err := k.repo.FindByHash(context.Background(), result.Token); err == nil {
		if s, _ := k.repo.FindByHash(context.Background(), result.Token); s != nil {
			t.Fatal("the raw token works as a lookup key: it is being stored in the clear")
		}
	}
}

func TestShare_ReachesOnlyItsOwnResearch(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, section, teamID := k.sharedResearch(t, domain.TeamViewer)

	other, _, err := k.research.Create(owner, CreateResearchRequest{
		TeamID: teamID, Name: "Not shared", Goal: "Secret",
	})
	if err != nil {
		t.Fatalf("create second research: %v", err)
	}
	if _, err := k.entry.Create(owner, CreateEntryRequest{
		ResearchID: research.ID, SectionID: section.ID, Title: "Seed", Content: "body",
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	result, err := k.shares.Create(owner, research.ID, CreateShareRequest{Include: allIncluded()})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	ctx := visit(t, k, result.Token)

	// The shared research reads.
	if _, err := k.research.Get(ctx, research.ID); err != nil {
		t.Fatalf("share cannot read its own research: %v", err)
	}
	if _, err := k.entry.ListByResearch(ctx, research.ID, storage.EntryFilter{}); err != nil {
		t.Fatalf("share cannot read its own entries: %v", err)
	}

	// Everything else is not merely refused — it is not found, because saying
	// "forbidden" would confirm the research exists.
	elsewhere := map[string]func() error{
		"research get": func() error { _, err := k.research.Get(ctx, other.ID); return err },
		"sections":     func() error { _, err := k.section.List(ctx, other.ID); return err },
		"entries":      func() error { _, err := k.entry.ListByResearch(ctx, other.ID, storage.EntryFilter{}); return err },
		"tasks":        func() error { _, err := k.task.List(ctx, other.ID, storage.TaskFilter{}); return err },
		"sessions":     func() error { _, err := k.session.ListByResearch(ctx, other.ID); return err },
		"roadmaps":     func() error { _, err := k.roadmap.List(ctx, other.ID); return err },
	}
	for name, call := range elsewhere {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: a share reached another research (%v)", name, err)
		}
	}

	// And a share has no list. Without the guard the filter would be left
	// unset — there is no user to scope by — and the answer would be every
	// research on the server.
	list, err := k.research.List(ctx, storage.ResearchFilter{})
	if err != nil {
		t.Fatalf("list under share: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("a share listed %d researches; it should list none", len(list))
	}
}

func TestShare_EveryWriteIsRefused(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, section, _ := k.sharedResearch(t, domain.TeamViewer)
	entry, err := k.entry.Create(owner, CreateEntryRequest{
		ResearchID: research.ID, SectionID: section.ID, Title: "Seed", Content: "body",
	})
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	result, err := k.shares.Create(owner, research.ID, CreateShareRequest{Include: allIncluded()})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	ctx := visit(t, k, result.Token)

	writes := map[string]func() error{
		"research update": func() error {
			_, err := k.research.Update(ctx, research.ID, UpdateResearchRequest{Name: ptr("hacked")})
			return err
		},
		"add section": func() error {
			_, err := k.research.AddSection(ctx, research.ID, CreateSectionRequest{Name: "new"})
			return err
		},
		"entry create": func() error {
			_, err := k.entry.Create(ctx, CreateEntryRequest{
				ResearchID: research.ID, SectionID: section.ID, Title: "x", Content: "y",
			})
			return err
		},
		"entry update": func() error {
			_, err := k.entry.Update(ctx, entry.ID, UpdateEntryRequest{Title: ptr("hacked")})
			return err
		},
		"entry delete": func() error { return k.entry.Delete(ctx, entry.ID) },
		"task create": func() error {
			_, err := k.task.Create(ctx, CreateTaskRequest{ResearchID: research.ID, Title: "x"})
			return err
		},
		"session create": func() error {
			_, _, err := k.session.Create(ctx, CreateSessionRequest{ResearchID: research.ID, Title: "x"})
			return err
		},
		"roadmap create": func() error {
			_, err := k.roadmap.Create(ctx, CreateRoadmapRequest{ResearchID: research.ID, Title: "x"})
			return err
		},
		"share create": func() error {
			_, err := k.shares.Create(ctx, research.ID, CreateShareRequest{})
			return err
		},
	}
	for name, call := range writes {
		if err := call(); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: a share link performed a write, or failed for the wrong reason (%v)", name, err)
		}
	}
}

func TestShare_InstructionAndMemoryNeverAppear(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, _, _ := k.sharedResearch(t, domain.TeamViewer)

	if err := k.research.researches.ImportProcess(owner, research.ID, "Do not tell the client we are guessing", domain.Memory{{Text: "the budget is a fiction", Author: "unknown"}}, nil); err != nil {
		t.Fatalf("set instruction and memory: %v", err)
	}

	result, err := k.shares.Create(owner, research.ID, CreateShareRequest{Include: allIncluded()})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	ctx := visit(t, k, result.Token)

	got, err := k.research.Get(ctx, research.ID)
	if err != nil {
		t.Fatalf("read shared research: %v", err)
	}
	if len(got.Memory) != 0 {
		t.Errorf("memory leaked to a share visitor: %v", got.Memory)
	}
	if got.TeamID != "" || got.TeamName != "" || got.UserID != "" {
		t.Errorf("the team behind the research leaked: team=%q/%q user=%q", got.TeamID, got.TeamName, got.UserID)
	}
	if got.Role != domain.TeamViewer {
		t.Errorf("a share resolved to role %q; the UI draws editing controls for anything else", got.Role)
	}

	// The owner still sees both. Redaction is per reader, not a deletion.
	own, err := k.research.Get(owner, research.ID)
	if err != nil {
		t.Fatalf("owner read: %v", err)
	}
	if len(own.Memory) == 0 {
		t.Fatal("redaction reached the owner's own read")
	}
	skills, err := k.research.researches.ExportPrivateSkills(owner, own.ID)
	if err != nil || len(skills) != 1 || skills[0].Body != "Do not tell the client we are guessing" {
		t.Fatalf("owner private instruction: %+v %v", skills, err)
	}
}

// How a team writes is working process, like the research's memory beside it.
// A visitor holding a read-only link to the findings was never handed the
// conventions those findings were written under.
func TestShare_SectionInstructionNeverAppears(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, section, _ := k.sharedResearch(t, domain.TeamViewer)

	const instruction = "Name the producing service. State the consumer."
	if _, err := k.section.Update(owner, section.ID, UpdateSectionRequest{
		Instruction: ptr(instruction),
	}); err != nil {
		t.Fatalf("set instruction: %v", err)
	}
	if _, err := k.entry.Create(owner, CreateEntryRequest{
		ResearchID: research.ID, SectionID: section.ID, Title: "Seed", Content: "body",
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	result, err := k.shares.Create(owner, research.ID, CreateShareRequest{Include: allIncluded()})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	ctx := visit(t, k, result.Token)

	// The section page: the list the shared research view is drawn from.
	list, err := k.section.List(ctx, research.ID)
	if err != nil {
		t.Fatalf("list sections: %v", err)
	}
	for _, sec := range list {
		if sec.Instruction != "" {
			t.Errorf("section %s leaked its instruction to a share visitor: %q", sec.Name, sec.Instruction)
		}
	}

	// And the single-section read, which is a separate entry point.
	one, err := k.section.Get(ctx, section.ID)
	if err != nil {
		t.Fatalf("get section: %v", err)
	}
	if one.Instruction != "" {
		t.Errorf("a direct section read leaked the instruction: %q", one.Instruction)
	}

	// The Obsidian vault. It carries a README per section, so a change that
	// starts writing the instruction into one has to fail here rather than ship.
	obsidian := NewObsidianService(k.research, k.section, storage.NewEntryRepository(k.db),
		k.session, k.task, k.roadmap, storage.NewEntryRevisionRepository(k.db), slog.Default())
	vault, err := obsidian.Vault(ctx, research.ID, DefaultVaultOptions())
	if err != nil {
		t.Fatalf("build vault: %v", err)
	}
	for _, f := range vault.Files {
		if strings.Contains(string(f.Content), instruction) {
			t.Errorf("the vault file %s carries the section instruction", f.Path)
		}
	}

	// Redaction is per reader. The owner still has their own convention.
	own, err := k.section.Get(owner, section.ID)
	if err != nil {
		t.Fatalf("owner read: %v", err)
	}
	if own.Instruction != instruction {
		t.Fatalf("redaction reached the owner's own read: %q", own.Instruction)
	}
}

func TestShare_RevokedExpiredAndUnknownAreIndistinguishable(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, _, _ := k.sharedResearch(t, domain.TeamViewer)
	ctx := context.Background()

	live, err := k.shares.Create(owner, research.ID, CreateShareRequest{})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	if err := k.shares.Revoke(owner, live.Share.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	past := -1
	expired, err := k.shares.Create(owner, research.ID, CreateShareRequest{ExpiresInDays: &past})
	if err != nil {
		t.Fatalf("create expiring share: %v", err)
	}
	// ExpiresInDays clamps to at least one day, so age the row directly: what is
	// under test is the reading of expiry, not the arithmetic that set it.
	if _, err := k.db.Exec(`UPDATE shares SET expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Hour).Format(time.DateTime), expired.Share.ID); err != nil {
		t.Fatalf("age the share: %v", err)
	}

	for name, token := range map[string]string{
		"revoked": live.Token,
		"expired": expired.Token,
		"unknown": "mrs_0000000000000000000000000000000000000000000000000000000000000000",
		"empty":   "",
	} {
		if _, err := k.shares.Resolve(ctx, token, ""); !errors.Is(err, ErrShareUnavailable) {
			t.Errorf("%s token: got %v, want the one uniform refusal", name, err)
		}
	}
}

func TestShare_RevocationSurvivesNothingAndTakesEffectAtOnce(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, _, _ := k.sharedResearch(t, domain.TeamViewer)

	result, err := k.shares.Create(owner, research.ID, CreateShareRequest{})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}

	// It works before, including from a context with nobody in it — a link
	// outliving its creator's session is the point of the feature.
	if _, err := k.shares.Resolve(context.Background(), result.Token, ""); err != nil {
		t.Fatalf("share does not work while live: %v", err)
	}
	if err := k.shares.Revoke(owner, result.Share.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := k.shares.Resolve(context.Background(), result.Token, ""); !errors.Is(err, ErrShareUnavailable) {
		t.Fatalf("a revoked link still resolves: %v", err)
	}
	// Revoking twice is reported, not silently repeated.
	if err := k.shares.Revoke(owner, result.Share.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revoke: got %v, want ErrNotFound", err)
	}
	// The row stays in the list. "Who did I give this to, and did I take it
	// back" is the question the list answers.
	listed, err := k.shares.List(owner, research.ID)
	if err != nil || len(listed) != 1 || listed[0].RevokedAt == nil {
		t.Fatalf("revoked share missing or not marked in the list: %v %+v", err, listed)
	}
}

func TestShare_Password(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, _, _ := k.sharedResearch(t, domain.TeamViewer)
	ctx := context.Background()

	if _, err := k.shares.Create(owner, research.ID, CreateShareRequest{Password: "abc"}); !errors.Is(err, ErrSharePassword) {
		t.Errorf("a three-character password was accepted: %v", err)
	}

	result, err := k.shares.Create(owner, research.ID, CreateShareRequest{Password: "correct horse"})
	if err != nil {
		t.Fatalf("create protected share: %v", err)
	}
	if !result.Share.HasPassword {
		t.Error("a protected share does not report itself as protected")
	}

	// Locked is deliberately not the uniform 404: whoever holds a working URL
	// already knows the link exists, and a password prompt that renders as
	// "not found" is unusable.
	if _, err := k.shares.Resolve(ctx, result.Token, ""); !errors.Is(err, ErrShareLocked) {
		t.Errorf("protected share resolved without a password: %v", err)
	}
	if _, err := k.shares.Unlock(ctx, result.Token, "wrong"); !errors.Is(err, ErrShareLocked) {
		t.Errorf("the wrong password unlocked the share: %v", err)
	}

	unlock, err := k.shares.Unlock(ctx, result.Token, "correct horse")
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if unlock == "" {
		t.Fatal("unlock returned nothing to present on the next request")
	}
	if _, err := k.shares.Resolve(ctx, result.Token, unlock); err != nil {
		t.Fatalf("the unlock value does not open the share: %v", err)
	}
	if _, err := k.shares.Resolve(ctx, result.Token, "guessed"); !errors.Is(err, ErrShareLocked) {
		t.Error("an arbitrary unlock value opened the share")
	}

	// Revoking a protected link kills it whether or not the visitor unlocked it.
	if err := k.shares.Revoke(owner, result.Share.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := k.shares.Resolve(ctx, result.Token, unlock); !errors.Is(err, ErrShareUnavailable) {
		t.Errorf("an unlocked visitor survived revocation: %v", err)
	}
}

func TestShare_OnlyWritersMayCreateOrManage(t *testing.T) {
	for _, tc := range []struct {
		role    domain.TeamRole
		allowed bool
	}{
		{domain.TeamViewer, false},
		{domain.TeamEditor, true},
		{domain.TeamOwner, true},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			k := newShareKit(t)
			owner, member, research, _, _ := k.sharedResearch(t, tc.role)

			result, err := k.shares.Create(member, research.ID, CreateShareRequest{})
			if tc.allowed {
				if err != nil {
					t.Fatalf("a %s could not create a share: %v", tc.role, err)
				}
			} else if !errors.Is(err, ErrForbidden) {
				t.Fatalf("a %s created a share: %v", tc.role, err)
			}

			if !tc.allowed {
				// A viewer may not see the management surface either — the pair
				// "hand out a link" and "take it back" belongs to one audience.
				seed, err := k.shares.Create(owner, research.ID, CreateShareRequest{})
				if err != nil {
					t.Fatalf("seed share: %v", err)
				}
				if _, err := k.shares.List(member, research.ID); !errors.Is(err, ErrForbidden) {
					t.Errorf("a viewer listed shares: %v", err)
				}
				if err := k.shares.Revoke(member, seed.Share.ID); !errors.Is(err, ErrForbidden) {
					t.Errorf("a viewer revoked a share: %v", err)
				}
				return
			}
			_ = result
		})
	}
}

func TestShare_StrangerCannotCreateOrRevoke(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, _, _ := k.sharedResearch(t, domain.TeamViewer)
	stranger := userCtx(createTestUser(t, k.db, "stranger@test.com", "Stranger"))

	if _, err := k.shares.Create(stranger, research.ID, CreateShareRequest{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a stranger creating a share got %v, want ErrNotFound", err)
	}
	seed, err := k.shares.Create(owner, research.ID, CreateShareRequest{})
	if err != nil {
		t.Fatalf("seed share: %v", err)
	}
	if err := k.shares.Revoke(stranger, seed.Share.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a stranger revoking a share got %v, want ErrNotFound", err)
	}
	if _, err := k.shares.List(stranger, research.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a stranger listing shares got %v, want ErrNotFound", err)
	}
}

func TestShare_CrossReferencesOutOfScopeRenderInert(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, section, teamID := k.sharedResearch(t, domain.TeamViewer)

	other, otherSections, err := k.research.Create(owner, CreateResearchRequest{
		TeamID: teamID, Name: "Other", Goal: "Secret",
		Sections: []CreateSectionRequest{{Name: "s1", DisplayName: "S1"}},
	})
	if err != nil {
		t.Fatalf("create other research: %v", err)
	}
	target, err := k.entry.Create(owner, CreateEntryRequest{
		ResearchID: other.ID, SectionID: otherSections[0].ID, Title: "Target", Content: "secret",
	})
	if err != nil {
		t.Fatalf("create target entry: %v", err)
	}
	if _, err := k.entry.Create(owner, CreateEntryRequest{
		ResearchID: research.ID, SectionID: section.ID, Title: "Source",
		Content: "see [[" + other.Code + ":" + target.Code + "]] for detail",
	}); err != nil {
		t.Fatalf("create source entry: %v", err)
	}

	result, err := k.shares.Create(owner, research.ID, CreateShareRequest{Include: allIncluded()})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	ctx := visit(t, k, result.Token)

	refs, err := storage.NewCrossRefRepository(k.db).FindByResearch(context.Background(), research.ID)
	if err != nil {
		t.Fatalf("read crossrefs: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("the cross-reference was never extracted; the test proves nothing")
	}

	access := testAccess(k.db)
	for _, ref := range access.VisibleCrossRefs(ctx, refs) {
		if ref.TargetResearchID == "" && ref.TargetEntryID == "" && !ref.Resolved {
			continue
		}
		if ref.TargetResearchID != research.ID && ref.TargetResearchID != "" {
			t.Errorf("a share saw a reference into another research: %+v", ref)
		}
		if ref.Resolved && ref.TargetEntryID == target.ID {
			t.Errorf("a reference out of the shared research resolved: %+v", ref)
		}
	}
}

func TestShare_DiesWithItsResearch(t *testing.T) {
	k := newShareKit(t)
	owner, _, research, _, _ := k.sharedResearch(t, domain.TeamViewer)

	result, err := k.shares.Create(owner, research.ID, CreateShareRequest{})
	if err != nil {
		t.Fatalf("create share: %v", err)
	}
	if _, err := k.db.Exec(`DELETE FROM researches WHERE id = ?`, research.ID); err != nil {
		t.Fatalf("delete research: %v", err)
	}
	if _, err := k.shares.Resolve(context.Background(), result.Token, ""); !errors.Is(err, ErrShareUnavailable) {
		t.Errorf("a share outlived its research: %v", err)
	}
}

func TestShare_UpdateTakesWriteAccessAndALiveLink(t *testing.T) {
	k := newShareKit(t)
	owner, viewer, research, _, _ := k.sharedResearch(t, domain.TeamViewer)
	seed, err := k.shares.Create(owner, research.ID, CreateShareRequest{Label: "  Client review  "})
	if err != nil {
		t.Fatalf("seed share: %v", err)
	}
	widen := UpdateShareRequest{Include: &domain.ShareInclude{Sessions: true, Tasks: true, Roadmaps: true, Export: true}}

	// A viewer may read the research through the team, but changing what a
	// public link exposes is the same class of act as issuing one.
	if _, err := k.shares.Update(viewer, seed.Share.ID, widen); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer widened a share: %v", err)
	}
	// A stranger learns nothing, not even that the id is real.
	stranger := userCtx(createTestUser(t, k.db, "stranger-update@test.com", "Stranger"))
	if _, err := k.shares.Update(stranger, seed.Share.ID, widen); !errors.Is(err, ErrNotFound) {
		t.Errorf("a stranger updating a share got %v, want ErrNotFound", err)
	}
	// And the share visitor holding the link least of all.
	if _, err := k.shares.Update(visit(t, k, seed.Token), seed.Share.ID, widen); err == nil {
		t.Error("a visitor widened the link they were handed")
	}

	// The owner may, and the same token now carries the new flags.
	label := "  Website demo  "
	updated, err := k.shares.Update(owner, seed.Share.ID, UpdateShareRequest{Label: &label, Include: widen.Include})
	if err != nil {
		t.Fatalf("owner update: %v", err)
	}
	if updated.Label != "Website demo" || !updated.Include.Sessions || !updated.Include.Tasks {
		t.Errorf("update stored %+v", updated)
	}
	resolved, err := k.shares.Resolve(context.Background(), seed.Token, "")
	if err != nil || !resolved.Include.Sessions {
		t.Errorf("the token does not carry the widened flags: %+v %v", resolved, err)
	}

	// Label alone leaves the flags where they are; include alone leaves the
	// label. Neither is a patch of the other.
	only := "Renamed"
	if got, err := k.shares.Update(owner, seed.Share.ID, UpdateShareRequest{Label: &only}); err != nil || !got.Include.Sessions || got.Label != "Renamed" {
		t.Errorf("label-only update: %+v %v", got, err)
	}
	if got, err := k.shares.Update(owner, seed.Share.ID, UpdateShareRequest{Include: &domain.ShareInclude{}}); err != nil || got.Include.Sessions || got.Label != "Renamed" {
		t.Errorf("include-only update: %+v %v", got, err)
	}

	// An expired link is dead, and the owner is told so rather than handed a
	// silent success on a row nobody can open.
	if _, err := k.db.NewUpdate().Table("shares").Set("expires_at=?", "2000-01-01 00:00:00").
		Where("id=?", seed.Share.ID).Exec(context.Background()); err != nil {
		t.Fatalf("expire share: %v", err)
	}
	if _, err := k.shares.Update(owner, seed.Share.ID, widen); !errors.Is(err, ErrShareInactive) || !errors.Is(err, ErrConflict) {
		t.Errorf("updating an expired link got %v, want ErrShareInactive (a conflict)", err)
	}

	// So is a revoked one.
	second, err := k.shares.Create(owner, research.ID, CreateShareRequest{})
	if err != nil {
		t.Fatalf("second share: %v", err)
	}
	if err := k.shares.Revoke(owner, second.Share.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := k.shares.Update(owner, second.Share.ID, widen); !errors.Is(err, ErrShareInactive) {
		t.Errorf("updating a revoked link got %v, want ErrShareInactive", err)
	}
	if _, err := k.shares.Resolve(context.Background(), second.Token, ""); !errors.Is(err, ErrShareUnavailable) {
		t.Errorf("a revoked link came back after an update attempt: %v", err)
	}
}
