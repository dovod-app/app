package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dovod-app/app/internal/service"
)

// P0-4, at the surface where it was visible: an agent cites [[E20]] before E20
// exists — the normal order — and the graph drew no edge between the two
// documents while the prose rendered a live link between them.
//
// Driven through the real mux rather than the service, because the graph
// handler skips any reference with `resolved` false and that skip is the whole
// symptom.
func TestGraph_ForwardReferenceBecomesAnEdge(t *testing.T) {
	s := newShareServer(t)

	// The citing document names a code that does not exist yet.
	source, err := s.entries.Create(s.ownerCtx, service.CreateEntryRequest{
		ResearchID: s.research.ID, SectionID: s.sectionID,
		Title: "Rests on something unwritten", Content: "This rests on [[E20]].",
	})
	if err != nil {
		t.Fatalf("create citing entry: %v", err)
	}

	if n := crossrefEdges(t, s, source.ID); n != 0 {
		t.Fatalf("a reference to a document that does not exist drew %d edges", n)
	}

	// Now write the document it named.
	var target string
	for i := 0; i < 40; i++ {
		e, err := s.entries.Create(s.ownerCtx, service.CreateEntryRequest{
			ResearchID: s.research.ID, SectionID: s.sectionID, Content: "filler",
		})
		if err != nil {
			t.Fatalf("create filler: %v", err)
		}
		if e.Code == "E20" {
			target = e.ID
			break
		}
	}
	if target == "" {
		t.Fatal("never reached E20")
	}

	edges := crossrefEdgesTo(t, s, source.ID)
	if len(edges) != 1 || edges[0] != target {
		t.Fatalf("the graph still shows no edge from the citing document to E20: %v", edges)
	}
}

func crossrefEdges(t *testing.T, s *shareServer, sourceID string) int {
	t.Helper()
	return len(crossrefEdgesTo(t, s, sourceID))
}

// crossrefEdgesTo returns the targets of every crossref edge out of one node.
func crossrefEdgesTo(t *testing.T, s *shareServer, sourceID string) []string {
	t.Helper()
	// Auth is off in this fixture, so an ordinary read needs no credential.
	code, body := s.get("/api/researches/" + s.research.ID + "/graph")
	if code != http.StatusOK {
		t.Fatalf("graph: %d %s", code, body)
	}
	// The graph handler writes nodes and edges at the top level, with no data
	// envelope — unlike almost every other read in this API.
	var payload struct {
		Edges []struct {
			Source string `json:"source"`
			Target string `json:"target"`
			Type   string `json:"type"`
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode graph: %v\n%s", err, body)
	}
	var out []string
	for _, e := range payload.Edges {
		if e.Type == "crossref" && e.Source == sourceID {
			out = append(out, e.Target)
		}
	}
	return out
}
