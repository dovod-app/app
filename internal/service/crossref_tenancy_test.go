package service

import (
	"testing"
)

// The forward-reference tests next door run one owner with two researches,
// which proves the code scoping but not the tenancy: the same person may read
// both, so nothing there fails if the boundary is drawn in the wrong place.
// These run two users who share no team.

// Bob creating his own E20 must not resolve Alice's `[[E20]]`. Entry codes
// repeat across researches — every research has an E20 eventually — so a match
// that ignored the source research would point one team's sentence at another
// team's document, permanently: the `resolved=0` guard then refuses to repair
// the row when Alice's own E20 finally appears.
func TestForwardRefs_TwoTenants_EntryCodeStaysHome(t *testing.T) {
	k := newForwardKit(t)
	alice, bob := setupTwoUsers(t, k.db)
	ctxA, ctxB := userCtx(alice), userCtx(bob)

	aid, asid := k.research1(t, ctxA, "Alice's research")
	src := k.citing(t, ctxA, aid, asid, "This rests on [[E20]].", "E20")

	bid, bsid := k.research1(t, ctxB, "Bob's research")
	k.fillTo(t, ctxB, bid, bsid, "E20")

	if k.resolvedRef(t, ctxA, aid, src, "E20") {
		t.Fatal("Bob creating E20 in his own research resolved Alice's [[E20]]")
	}

	// And Alice's own E20 still repairs it, so the scoping is a boundary rather
	// than a switch that turned the feature off.
	own := k.fillTo(t, ctxA, aid, asid, "E20")
	if !k.resolvedRef(t, ctxA, aid, src, "E20") {
		t.Fatal("Alice's own E20 did not resolve her [[E20]]")
	}
	if entryID, _, _, _ := k.refRow(t, ctxA, aid, src, "E20"); entryID != own {
		t.Fatalf("resolved to %s, want Alice's own entry %s", entryID, own)
	}
}

// The same for `[[RM1]]`. Roadmap codes are allocated per research, so every
// research that has a roadmap has an RM1 — this is the case the review caught
// against the first version of the resolver, which matched them globally.
func TestForwardRefs_TwoTenants_RoadmapCodeStaysHome(t *testing.T) {
	k := newForwardKit(t)
	alice, bob := setupTwoUsers(t, k.db)
	ctxA, ctxB := userCtx(alice), userCtx(bob)

	aid, asid := k.research1(t, ctxA, "Alice's research")
	src := k.citing(t, ctxA, aid, asid, "The plan is [[RM1]].", "RM1")

	bid, _ := k.research1(t, ctxB, "Bob's research")
	if _, err := k.roadmap.Create(ctxB, CreateRoadmapRequest{
		ResearchID: bid, Title: "Bob's plan",
	}); err != nil {
		t.Fatalf("create Bob's roadmap: %v", err)
	}

	if k.resolvedRef(t, ctxA, aid, src, "RM1") {
		t.Fatal("Bob creating RM1 in his own research resolved Alice's [[RM1]]")
	}

	own, err := k.roadmap.Create(ctxA, CreateRoadmapRequest{ResearchID: aid, Title: "Alice's plan"})
	if err != nil {
		t.Fatalf("create Alice's roadmap: %v", err)
	}
	if !k.resolvedRef(t, ctxA, aid, src, "RM1") {
		t.Fatal("Alice's own RM1 did not resolve her [[RM1]]")
	}
	if _, _, roadmapID, _ := k.refRow(t, ctxA, aid, src, "RM1"); roadmapID != own.ID {
		t.Fatalf("resolved to roadmap %s, want Alice's own %s", roadmapID, own.ID)
	}
	_ = asid
}

// The repair is announced to the research that holds the *source*, which for a
// cross-research reference is not the one that just gained the target.
//
// Alice writes `[[R<bob>:E20]]`. When Bob creates E20 the row moves — it is
// Alice's page that changed and must repaint. Announcing it on Bob's research
// instead told the wrong tenant that somebody out of sight cites them, and left
// the one page that actually changed stale until it was reloaded by hand.
func TestForwardRefs_ResolvedEventNamesTheSourceResearch(t *testing.T) {
	k := newForwardKit(t)
	alice, bob := setupTwoUsers(t, k.db)
	ctxA, ctxB := userCtx(alice), userCtx(bob)

	aid, asid := k.research1(t, ctxA, "Alice's research")
	bid, bsid := k.research1(t, ctxB, "Bob's research")

	bobResearch, err := k.research.Get(ctxB, bid)
	if err != nil {
		t.Fatalf("read Bob's research: %v", err)
	}
	ref := bobResearch.Code + ":E20"
	k.citing(t, ctxA, aid, asid, "Bob has [["+ref+"]].", ref)

	k.notifier.reset()
	k.fillTo(t, ctxB, bid, bsid, "E20")

	var named []string
	for _, e := range k.notifier.events {
		if e.Type == "crossrefs.resolved" {
			named = append(named, e.ResearchID)
		}
	}
	if len(named) != 1 {
		t.Fatalf("crossrefs.resolved fired %d times, want exactly one: %v", len(named), named)
	}
	if named[0] == bid {
		t.Fatal("the repair was announced on Bob's research, whose rows did not move")
	}
	if named[0] != aid {
		t.Fatalf("announced on %s, want Alice's research %s", named[0], aid)
	}
}

// Creating something nothing points at announces nothing. Without this the
// event is indistinguishable from "a document was created", and a research with
// an empty reference table would repaint its graph on every write.
func TestForwardRefs_NothingRepairedAnnouncesNothing(t *testing.T) {
	k := newForwardKit(t)
	alice := createTestUser(t, k.db, "quiet@test.com", "Quiet")
	ctx := userCtx(alice)

	rid, sid := k.research1(t, ctx, "Quiet research")
	k.notifier.reset()
	k.fillTo(t, ctx, rid, sid, "E3")

	for _, e := range k.notifier.events {
		if e.Type == "crossrefs.resolved" {
			t.Fatalf("crossrefs.resolved fired with nothing to repair (research %s)", e.ResearchID)
		}
	}
}
