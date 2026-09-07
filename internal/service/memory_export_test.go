package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/storage"
)

func TestMemoryExport_LegacyJSONAndSessionRoundTrip(t *testing.T) {
	ctx := context.Background()
	exporter, research, _, _, sessions, _, _ := setupExportService(t)
	var legacy domain.ExportData
	if err := json.Unmarshal([]byte(`{"version":1,"research":{"name":"Legacy","status":"active","instruction":"exact ? '\n日本語","memory":["same","same",""]}}`), &legacy); err != nil {
		t.Fatal(err)
	}
	r, _, err := exporter.Import(ctx, &legacy, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Memory) != 3 || r.Memory[0].Author != "unknown" || r.Memory[0].CreatedAt != nil || r.Memory[0].ID == r.Memory[1].ID {
		t.Fatalf("legacy notes: %+v", r.Memory)
	}
	session, _, err := sessions.Create(ctx, CreateSessionRequest{ResearchID: r.ID, Title: "Source session"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := research.AddMemory(WithAuthor(ctx, domain.AuthorHuman), r.ID, "with provenance", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	dump, err := exporter.Export(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dump.Version != 2 || dump.Research.Instruction != "" || len(dump.Research.PrivateSkills) != 1 || dump.Research.PrivateSkills[0].Body != legacy.Research.Instruction {
		t.Fatalf("portable process: %+v", dump)
	}
	raw, err := json.Marshal(dump)
	if err != nil {
		t.Fatal(err)
	}
	var decoded domain.ExportData
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	// A forged local session ID must not override portable session linkage.
	decoded.Research.Memory[3].SessionID = session.ID
	for i := 0; i < 2; i++ {
		imported, _, err := exporter.Import(ctx, &decoded, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(imported.Memory) != 4 {
			t.Fatalf("notes: %+v", imported.Memory)
		}
		got := imported.Memory[3]
		if got.ID == note.ID || got.SessionID == note.SessionID || got.SessionID == "" || got.Author != "user" || got.CreatedAt == nil || !got.CreatedAt.Equal(*note.CreatedAt) {
			t.Fatalf("remapped provenance: %+v original %+v", got, note)
		}
		ss, err := sessions.Get(ctx, got.SessionID)
		if err != nil || ss.Session.ResearchID != imported.ID {
			t.Fatalf("session points outside import: %+v %v", ss, err)
		}
		again, err := exporter.Export(ctx, imported.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(dump.Research.Memory, again.Research.Memory) || !reflect.DeepEqual(dump.Research.PrivateSkills, again.Research.PrivateSkills) {
			t.Fatal("portable memory/skills changed on round-trip")
		}
	}
}

func TestMemoryExport_UnlinkedNotesStayUnlinkedWithUnnamedSessions(t *testing.T) {
	for _, payload := range []string{
		`{"version":1,"research":{"name":"Legacy","status":"active","sessions":[{"title":"Unnamed session","status":"active"}],"memory":["Unattributed note"]}}`,
		`{"version":2,"research":{"name":"Structured","status":"active","sessions":[{"title":"Unnamed session","status":"active"}],"memory":[{"text":"Unattributed note","author":"unknown","session_id":"foreign-instance-id"}]}}`,
	} {
		var data domain.ExportData
		if err := json.Unmarshal([]byte(payload), &data); err != nil {
			t.Fatal(err)
		}
		exporter, _, _, _, _, _, _ := setupExportService(t)
		imported, _, err := exporter.Import(context.Background(), &data, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(imported.Memory) != 1 || imported.Memory[0].SessionID != "" || imported.Memory[0].SessionCode != "" {
			t.Fatalf("v%d import invented session provenance: %+v", data.Version, imported.Memory)
		}
	}
}

func TestMemoryExport_InvalidProcessRejectedBeforeCreatingResearch(t *testing.T) {
	for _, process := range []domain.ExportResearch{
		{Memory: domain.Memory{{Text: "x", Author: "invented"}}},
		{Memory: domain.Memory{{Text: "x", Author: "agent", SessionCode: "SS404"}}},
		{PrivateSkills: []domain.ExportPrivateSkill{{Slug: "same", Name: "First"}, {Slug: "same", Name: "Second"}}},
	} {
		exporter, research, _, _, _, _, _ := setupExportService(t)
		process.Name, process.Status = "Invalid", domain.ResearchActive
		_, _, err := exporter.Import(context.Background(), &domain.ExportData{Version: 2, Research: process}, "")
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("invalid process accepted: %v", err)
		}
		list, err := research.researches.FindAll(context.Background(), storage.ResearchFilter{})
		if err != nil || len(list) != 0 {
			t.Fatalf("invalid import left partial research: %d %v", len(list), err)
		}
	}
}
