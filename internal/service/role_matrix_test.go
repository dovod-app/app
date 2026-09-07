package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
	"github.com/uptrace/bun"
)

// The ownership suite next door asks one question: can a stranger reach this?
// Teams add a second, and it is the one a single-owner model never had to
// answer — a colleague who *can* see the research still may not be allowed to
// change it.
//
// So this is a matrix, not a list: every entity, crossed with every role. A
// missed call site in the sweep from `validateResearchAccess` to
// `Access.Write` is a viewer who can write, and nothing else in the suite
// would notice.

type roleKit struct {
	db         *bun.DB
	research   *ResearchService
	section    *SectionService
	entry      *EntryService
	session    *SessionService
	task       *TaskService
	roadmap    *RoadmapService
	annotation *AnnotationService
	resume     *ResumeService
	team       *TeamService
	teamRepo   *storage.TeamRepository
	// events is what the WebSocket hub would have been handed. Delivery is
	// decided from these fields, so what is in them is a correctness question.
	events *mockNotifier
}

func newRoleKit(t *testing.T) *roleKit {
	t.Helper()
	return newRoleKitFor(t, false)
}

// newRoleKitWithAccounts wires the same services on an instance that has
// `auth_enabled` set. The matrix itself does not need the distinction — every
// case in it has a real user — but the row for a caller who is nobody does: it
// is the only role whose answer depends on which kind of instance this is.
func newRoleKitWithAccounts(t *testing.T) *roleKit {
	t.Helper()
	return newRoleKitFor(t, true)
}

func newRoleKitFor(t *testing.T, accountsEnabled bool) *roleKit {
	t.Helper()
	db := setupTestDB(t)
	log := slog.Default()
	notifier := &mockNotifier{}
	access := NewAccess(storage.NewTeamRepository(db), accountsEnabled)

	researchRepo := storage.NewResearchRepository(db)
	sectionRepo := storage.NewSectionRepository(db)
	entryRepo := storage.NewEntryRepository(db)
	sessionRepo := storage.NewSessionRepository(db)
	teamRepo := storage.NewTeamRepository(db)

	entrySvc := NewEntryService(entryRepo, sectionRepo, researchRepo, access, sessionRepo,
		storage.NewBlockRepository(db), storage.NewEntryRevisionRepository(db),
		storage.NewCrossRefRepository(db), storage.NewExternalLinkRepository(db), notifier, log)

	researchSvc := NewResearchService(researchRepo, sectionRepo, teamRepo, access, notifier, log)

	return &roleKit{
		db:       db,
		research: researchSvc,
		section:  NewSectionService(sectionRepo, entryRepo, researchRepo, access, notifier, log),
		entry:    entrySvc,
		session: NewSessionService(db, sessionRepo, storage.NewQuestionRepository(db), researchRepo,
			access, entrySvc, notifier, log),
		task: NewTaskService(storage.NewTaskRepository(db), researchRepo, access, entrySvc, notifier, log),
		annotation: NewAnnotationService(storage.NewAnnotationRepository(db), entryRepo,
			storage.NewEntryRevisionRepository(db), access, entrySvc, entrySvc, notifier, log),
		roadmap: NewRoadmapService(storage.NewRoadmapRepository(db), storage.NewRoadmapNodeRepository(db),
			storage.NewRoadmapEdgeRepository(db), researchRepo, access, notifier, log),
		resume: NewResumeService(researchSvc, sessionRepo, storage.NewTaskRepository(db),
			storage.NewQuestionRepository(db), storage.NewAnnotationRepository(db), entryRepo,
			storage.NewEntryRevisionRepository(db), access, log),
		team:     NewTeamService(teamRepo, storage.NewTeamInviteRepository(db), storage.NewUserRepository(db), researchRepo, access, notifier, log),
		teamRepo: teamRepo,
		events:   notifier,
	}
}

// sharedResearch sets up the shape every case here needs: an owner with a
// non-personal team, a research in it, and a second user holding `role`.
func (k *roleKit) sharedResearch(t *testing.T, role domain.TeamRole) (owner, member context.Context, research *domain.Research, section *domain.Section, teamID string) {
	t.Helper()
	ownerUser := createTestUser(t, k.db, "owner-"+string(role)+"@test.com", "Owner")
	memberUser := createTestUser(t, k.db, "member-"+string(role)+"@test.com", "Member")
	owner, member = userCtx(ownerUser), userCtx(memberUser)

	team, err := k.team.Create(owner, "Shared "+string(role))
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	teamID = team.ID
	addToTeam(t, k.db, teamID, memberUser.ID, role)

	research, sections, err := k.research.Create(owner, CreateResearchRequest{
		TeamID: teamID,
		Name:   "Shared research",
		Goal:   "Roles",
		Sections: []CreateSectionRequest{
			{Name: "s1", DisplayName: "Section one"},
		},
	})
	if err != nil {
		t.Fatalf("create research: %v", err)
	}
	return owner, member, research, sections[0], teamID
}

func TestRoles_EveryMemberCanRead(t *testing.T) {
	for _, role := range []domain.TeamRole{domain.TeamViewer, domain.TeamEditor, domain.TeamOwner} {
		t.Run(string(role), func(t *testing.T) {
			k := newRoleKit(t)
			owner, member, research, section, _ := k.sharedResearch(t, role)

			if _, err := k.entry.Create(owner, CreateEntryRequest{
				ResearchID: research.ID, SectionID: section.ID,
				Title: "Seed", Content: "body",
			}); err != nil {
				t.Fatalf("seed entry: %v", err)
			}

			reads := map[string]func() error{
				"research get": func() error { _, err := k.research.Get(member, research.ID); return err },
				"research list": func() error {
					list, err := k.research.List(member, storage.ResearchFilter{})
					if err != nil {
						return err
					}
					for _, r := range list {
						if r.ID == research.ID {
							return nil
						}
					}
					t.Fatal("shared research missing from the member's list")
					return nil
				},
				"sections":  func() error { _, err := k.section.List(member, research.ID); return err },
				"entries":   func() error { _, err := k.entry.ListByResearch(member, research.ID, storage.EntryFilter{}); return err },
				"tasks":     func() error { _, err := k.task.List(member, research.ID, storage.TaskFilter{}); return err },
				"sessions":  func() error { _, err := k.session.ListByResearch(member, research.ID); return err },
				"roadmaps":  func() error { _, err := k.roadmap.List(member, research.ID); return err },
				"team read": func() error { _, err := k.team.Members(member, researchTeam(t, k, research.ID)); return err },
			}
			for name, call := range reads {
				if err := call(); err != nil {
					t.Errorf("%s: a %s should be able to read: %v", name, role, err)
				}
			}
		})
	}
}

func TestRoles_ViewerCannotWriteAnything(t *testing.T) {
	k := newRoleKit(t)
	owner, viewer, research, section, _ := k.sharedResearch(t, domain.TeamViewer)

	entry, err := k.entry.Create(owner, CreateEntryRequest{
		ResearchID: research.ID, SectionID: section.ID, Title: "Seed", Content: "body",
	})
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	sess, _, err := k.session.Create(owner, CreateSessionRequest{
		ResearchID: research.ID, Title: "S", Focus: "F",
		Questions: []CreateQuestionRequest{{Text: "Q?"}},
	})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	mark, err := k.annotation.Create(owner, CreateAnnotationRequest{
		EntryID: entry.ID, Quote: domain.Quote{Exact: "body"}, Kind: domain.AnnotationVerify,
	})
	if err != nil {
		t.Fatalf("seed annotation: %v", err)
	}

	task, err := k.task.Create(owner, CreateTaskRequest{ResearchID: research.ID, Title: "T"})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	rm, err := k.roadmap.Create(owner, CreateRoadmapRequest{ResearchID: research.ID, Title: "RM"})
	if err != nil {
		t.Fatalf("seed roadmap: %v", err)
	}
	questions, err := k.session.ListQuestions(owner, sess.ID, storage.QuestionFilter{})
	if err != nil || len(questions) == 0 {
		t.Fatalf("seed questions: %v", err)
	}

	writes := map[string]func(context.Context) error{
		"research update": func(ctx context.Context) error {
			_, err := k.research.Update(ctx, research.ID, UpdateResearchRequest{Name: ptr("Renamed")})
			return err
		},
		"add section": func(ctx context.Context) error {
			_, err := k.research.AddSection(ctx, research.ID, CreateSectionRequest{Name: "s2", DisplayName: "S2"})
			return err
		},
		"section update": func(ctx context.Context) error {
			_, err := k.section.Update(ctx, section.ID, UpdateSectionRequest{DisplayName: ptr("Renamed")})
			return err
		},
		"entry create": func(ctx context.Context) error {
			_, err := k.entry.Create(ctx, CreateEntryRequest{
				ResearchID: research.ID, SectionID: section.ID, Title: "New", Content: "x",
			})
			return err
		},
		"entry update": func(ctx context.Context) error {
			_, err := k.entry.Update(ctx, entry.ID, UpdateEntryRequest{Title: ptr("Renamed")})
			return err
		},
		"entry delete": func(ctx context.Context) error { return k.entry.Delete(ctx, entry.ID) },
		"entry patch": func(ctx context.Context) error {
			_, err := k.entry.PatchBlocks(ctx, entry.ID, PatchBlocksRequest{})
			return err
		},
		"crossref rebuild": func(ctx context.Context) error {
			_, err := k.entry.RebuildCrossRefs(ctx, research.ID)
			return err
		},
		// Both halves of the markdown import. The preview writes nothing, and it
		// is still a write for permission purposes: it is reachable only from a
		// section the caller may add a document to, and telling a viewer what
		// their file would have become is offering them a control they do not
		// have.
		"import preview": func(ctx context.Context) error {
			_, err := k.entry.PreviewMarkdownImport(ctx, section.ID, "note.md", []byte("# Hi\n\nBody.\n"))
			return err
		},
		"import commit": func(ctx context.Context) error {
			_, err := k.entry.ImportMarkdown(ctx, ImportEntryRequest{
				SectionID: section.ID, Title: "Imported", Body: "Body.",
			})
			return err
		},
		"session create": func(ctx context.Context) error {
			_, _, err := k.session.Create(ctx, CreateSessionRequest{ResearchID: research.ID, Title: "S2"})
			return err
		},
		"session update": func(ctx context.Context) error {
			_, err := k.session.Update(ctx, sess.ID, UpdateSessionRequest{Title: ptr("Renamed")})
			return err
		},
		"question add": func(ctx context.Context) error {
			_, err := k.session.AddQuestions(ctx, sess.ID, []CreateQuestionRequest{{Text: "Another?"}})
			return err
		},
		"question update": func(ctx context.Context) error {
			answer := "an answer"
			_, err := k.session.UpdateQuestion(ctx, questions[0].ID, nil, &answer)
			return err
		},
		"task create": func(ctx context.Context) error {
			_, err := k.task.Create(ctx, CreateTaskRequest{ResearchID: research.ID, Title: "T2"})
			return err
		},
		"task update": func(ctx context.Context) error {
			_, err := k.task.Update(ctx, task.ID, UpdateTaskRequest{Title: ptr("Renamed")})
			return err
		},
		"task delete": func(ctx context.Context) error { return k.task.Delete(ctx, task.ID) },
		// Marking a sentence is authoring, not reading: a viewer may see what
		// others doubted and may not add to it.
		"annotation create": func(ctx context.Context) error {
			_, err := k.annotation.Create(ctx, CreateAnnotationRequest{
				EntryID: entry.ID, Quote: domain.Quote{Exact: "body"}, Kind: domain.AnnotationVerify,
			})
			return err
		},
		"annotation update": func(ctx context.Context) error {
			_, err := k.annotation.Update(ctx, mark.ID, UpdateAnnotationRequest{Body: ptr("changed")})
			return err
		},
		"annotation answer": func(ctx context.Context) error {
			_, err := k.annotation.Answer(ctx, mark.ID, AnswerAnnotationRequest{Resolution: "done"})
			return err
		},
		"annotation delete": func(ctx context.Context) error { return k.annotation.Delete(ctx, mark.ID) },
		"roadmap create": func(ctx context.Context) error {
			_, err := k.roadmap.Create(ctx, CreateRoadmapRequest{ResearchID: research.ID, Title: "RM2"})
			return err
		},
		"roadmap update": func(ctx context.Context) error {
			_, err := k.roadmap.Update(ctx, rm.ID, UpdateRoadmapRequest{Title: ptr("Renamed")})
			return err
		},
		"roadmap add nodes": func(ctx context.Context) error {
			_, err := k.roadmap.AddNodes(ctx, rm.ID, []CreateRoadmapNodeRequest{{Title: "N"}}, nil)
			return err
		},
		"roadmap update node": func(ctx context.Context) error {
			rmWithNode, err := k.roadmap.AddNodes(owner, rm.ID, []CreateRoadmapNodeRequest{{Title: "To rename"}}, nil)
			if err != nil {
				return err
			}
			for _, n := range rmWithNode.Nodes {
				if n.Title == "To rename" {
					_, err := k.roadmap.UpdateNode(ctx, n.ID, UpdateRoadmapNodeRequest{Title: ptr("Renamed")})
					return err
				}
			}
			return errors.New("seeded node not returned")
		},
		"roadmap remove nodes": func(ctx context.Context) error {
			// The node is created by the owner so that a viewer's refusal comes
			// from the removal, not from an earlier add.
			rmWithNode, err := k.roadmap.AddNodes(owner, rm.ID, []CreateRoadmapNodeRequest{{Title: "To remove"}}, nil)
			if err != nil {
				return err
			}
			for _, n := range rmWithNode.Nodes {
				if n.Title == "To remove" {
					return k.roadmap.RemoveNodes(ctx, rm.ID, []string{n.ID})
				}
			}
			return errors.New("seeded node not returned")
		},
		"roadmap delete": func(ctx context.Context) error { return k.roadmap.Delete(ctx, rm.ID) },
	}

	for name, call := range writes {
		if err := call(viewer); !errors.Is(err, ErrForbidden) {
			t.Errorf("%s: a viewer must be refused with ErrForbidden, got %v", name, err)
		}
	}
}

// A section instruction is read by everyone in the team and written only by
// somebody who may write content. It is a convention for the documents, not a
// setting about the team, so it does not need an owner.
// TestRoles_AnonymousIsNotARoleWhenAccountsAreOn adds the row the matrix never
// had: a caller with no identity at all.
//
// Every other case here holds a real user, so nothing above would notice that
// "no user" was its own, most privileged role — reads and writes both, on every
// research on the server. That is what an anonymous stdio session was, and the
// deletes make the consequence permanent.
func TestRoles_AnonymousIsNotARoleWhenAccountsAreOn(t *testing.T) {
	k := newRoleKitWithAccounts(t)
	owner, _, research, section, _ := k.sharedResearch(t, domain.TeamViewer)

	entry, err := k.entry.Create(owner, CreateEntryRequest{
		ResearchID: research.ID, SectionID: section.ID, Title: "Seed", Content: "body",
	})
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	task, err := k.task.Create(owner, CreateTaskRequest{ResearchID: research.ID, Title: "Seed task"})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}

	anon := context.Background()
	ops := map[string]func() error{
		"research get": func() error { _, err := k.research.Get(anon, research.ID); return err },
		"research update": func() error {
			_, err := k.research.Update(anon, research.ID, UpdateResearchRequest{Goal: ptr("mine")})
			return err
		},
		"sections": func() error { _, err := k.section.List(anon, research.ID); return err },
		"section update": func() error {
			_, err := k.section.Update(anon, section.ID, UpdateSectionRequest{DisplayName: ptr("Renamed")})
			return err
		},
		"entries":    func() error { _, err := k.entry.ListByResearch(anon, research.ID, storage.EntryFilter{}); return err },
		"entry read": func() error { _, err := k.entry.Get(anon, entry.ID); return err },
		"entry create": func() error {
			_, err := k.entry.Create(anon, CreateEntryRequest{ResearchID: research.ID, SectionID: section.ID, Title: "T", Content: "c"})
			return err
		},
		"tasks": func() error { _, err := k.task.List(anon, research.ID, storage.TaskFilter{}); return err },
		"task update": func() error {
			_, err := k.task.Update(anon, task.ID, UpdateTaskRequest{Title: ptr("mine")})
			return err
		},
		"sessions": func() error { _, err := k.session.ListByResearch(anon, research.ID); return err },
		"roadmaps": func() error { _, err := k.roadmap.List(anon, research.ID); return err },
		"resume":   func() error { _, err := k.resume.Get(anon, research.ID, ResumeRequest{}); return err },
	}
	for name, call := range ops {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound — an anonymous caller is a stranger once accounts exist", name, err)
		}
	}

	// The list is the one read that answers without consulting the guard.
	list, err := k.research.List(anon, storage.ResearchFilter{})
	if err != nil {
		t.Fatalf("research list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("research list gave an anonymous caller %d research(es), want 0", len(list))
	}
}

func TestRoles_SectionInstructionIsReadByEveryoneAndWrittenByWriters(t *testing.T) {
	const instruction = "Name the producing service. State the consumer."
	for _, tc := range []struct {
		role     domain.TeamRole
		mayWrite bool
	}{
		{domain.TeamViewer, false},
		{domain.TeamEditor, true},
		{domain.TeamOwner, true},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			k := newRoleKit(t)
			owner, member, research, section, _ := k.sharedResearch(t, tc.role)

			if _, err := k.section.Update(owner, section.ID, UpdateSectionRequest{
				Instruction: ptr(instruction),
			}); err != nil {
				t.Fatalf("seed instruction: %v", err)
			}

			// Reading it is the point of the feature: the member has to see the
			// same rule the agent writing here does.
			list, err := k.section.List(member, research.ID)
			if err != nil || len(list) == 0 {
				t.Fatalf("a %s could not list sections: %v", tc.role, err)
			}
			if list[0].Instruction != instruction {
				t.Errorf("a %s reads the section instruction as %q", tc.role, list[0].Instruction)
			}

			_, err = k.section.Update(member, section.ID, UpdateSectionRequest{
				Instruction: ptr("Rewritten by a " + string(tc.role)),
			})
			if tc.mayWrite && err != nil {
				t.Fatalf("a %s should be able to write the instruction: %v", tc.role, err)
			}
			if !tc.mayWrite && !errors.Is(err, ErrForbidden) {
				t.Fatalf("a %s wrote the section instruction (err=%v)", tc.role, err)
			}

			after, err := k.section.Get(owner, section.ID)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if !tc.mayWrite && after.Instruction != instruction {
				t.Errorf("a viewer's refused write still landed: %q", after.Instruction)
			}
		})
	}
}

func TestRoles_EditorCanWriteButNotAdminister(t *testing.T) {
	k := newRoleKit(t)
	_, editor, research, section, teamID := k.sharedResearch(t, domain.TeamEditor)

	if _, err := k.entry.Create(editor, CreateEntryRequest{
		ResearchID: research.ID, SectionID: section.ID, Title: "Editor's entry", Content: "body",
	}); err != nil {
		t.Fatalf("an editor must be able to write: %v", err)
	}
	if _, err := k.task.Create(editor, CreateTaskRequest{ResearchID: research.ID, Title: "T"}); err != nil {
		t.Fatalf("an editor must be able to create a task: %v", err)
	}
	if _, err := k.research.Update(editor, research.ID, UpdateResearchRequest{Goal: ptr("New goal")}); err != nil {
		t.Fatalf("an editor must be able to update the research: %v", err)
	}

	// Managing the team is where an editor stops.
	other := createTestUser(t, k.db, "outsider@test.com", "Outsider")
	if _, err := k.team.CreateInvite(editor, teamID, "someone@test.com", domain.TeamViewer); !errors.Is(err, ErrForbidden) {
		t.Errorf("an editor must not invite: %v", err)
	}
	if err := k.team.UpdateRole(editor, teamID, other.ID, domain.TeamEditor); !errors.Is(err, ErrForbidden) {
		t.Errorf("an editor must not change roles: %v", err)
	}
	if err := k.team.Delete(editor, teamID); !errors.Is(err, ErrForbidden) {
		t.Errorf("an editor must not delete the team: %v", err)
	}
	if err := k.team.TransferResearch(editor, research.ID, teamID); !errors.Is(err, ErrForbidden) {
		t.Errorf("an editor must not move the research: %v", err)
	}
}

func TestRoles_RemovedMemberLosesEverything(t *testing.T) {
	k := newRoleKit(t)
	owner, member, research, _, teamID := k.sharedResearch(t, domain.TeamEditor)

	memberUser := createTestUser(t, k.db, "second@test.com", "Second")
	addToTeam(t, k.db, teamID, memberUser.ID, domain.TeamOwner)

	if _, err := k.research.Get(member, research.ID); err != nil {
		t.Fatalf("member should start with access: %v", err)
	}

	var removed string
	members, err := k.team.Members(owner, teamID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	for _, m := range members {
		if m.Email == "member-editor@test.com" {
			removed = m.UserID
		}
	}
	if removed == "" {
		t.Fatal("did not find the member to remove")
	}
	if err := k.team.RemoveMember(owner, teamID, removed); err != nil {
		t.Fatalf("remove member: %v", err)
	}

	// Not "forbidden" — gone. Someone outside the team must not learn the
	// research is there.
	if _, err := k.research.Get(member, research.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("a removed member must get ErrNotFound, got %v", err)
	}
	list, err := k.research.List(member, storage.ResearchFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range list {
		if r.ID == research.ID {
			t.Error("a removed member still sees the research in their list")
		}
	}
}

// researchTeam is the team a research belongs to, read back through the
// service so the test asserts on what callers actually get.
func researchTeam(t *testing.T, k *roleKit, researchID string) string {
	t.Helper()
	r, err := storage.NewResearchRepository(k.db).FindByID(context.Background(), researchID)
	if err != nil || r == nil {
		t.Fatalf("find research: %v", err)
	}
	return r.TeamID
}

// The continuation summary is a read, so a viewer gets all of it — and gets
// `can_write: false`, which is what the web UI reads to decide whether to draw
// a control. A viewer refused outright would be a regression in the other
// direction: they may see what is outstanding, they simply may not act on it.
func TestRoles_ResumeReadsForEveryMemberAndSaysWhoMayWrite(t *testing.T) {
	for _, tc := range []struct {
		role     domain.TeamRole
		canWrite bool
	}{
		{domain.TeamViewer, false},
		{domain.TeamEditor, true},
		{domain.TeamOwner, true},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			k := newRoleKit(t)
			owner, member, research, _, _ := k.sharedResearch(t, tc.role)

			if _, err := k.task.Create(owner, CreateTaskRequest{ResearchID: research.ID, Title: "Something open"}); err != nil {
				t.Fatalf("seed task: %v", err)
			}

			out, err := k.resume.Get(member, research.ID, ResumeRequest{})
			if err != nil {
				t.Fatalf("a %s must be able to read the summary: %v", tc.role, err)
			}
			if out.Research.Role != tc.role {
				t.Errorf("role = %q, want %q", out.Research.Role, tc.role)
			}
			if out.Research.CanWrite != tc.canWrite {
				t.Errorf("can_write = %v, want %v for a %s", out.Research.CanWrite, tc.canWrite, tc.role)
			}
			// The work itself is the team's, so every role sees the same queue.
			if out.Work.Pending.Total == nil || *out.Work.Pending.Total != 1 {
				t.Errorf("pending total = %v, want 1", out.Work.Pending.Total)
			}
		})
	}
}

// Somebody in no team at all is told the research does not exist, not that they
// may not read it — the same answer an absent id gets.
func TestRoles_ResumeHidesAResearchFromANonMember(t *testing.T) {
	k := newRoleKit(t)
	owner, _, research, _, _ := k.sharedResearch(t, domain.TeamViewer)
	stranger := userCtx(createTestUser(t, k.db, "stranger-resume@test.com", "Stranger"))

	if _, err := k.resume.Get(owner, research.ID, ResumeRequest{}); err != nil {
		t.Fatalf("owner resume: %v", err)
	}
	if _, err := k.resume.Get(stranger, research.ID, ResumeRequest{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger resume = %v, want ErrNotFound", err)
	}
}
