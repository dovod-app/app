package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/dovod-app/app/internal/domain"
	"github.com/dovod-app/app/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var slugRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func isValidSlug(s string) bool {
	return slugRegexp.MatchString(s)
}

func successResult(data any) (*mcp.CallToolResult, any, error) {
	text, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("marshal response: %v", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: sanitizeUTF8(string(text))},
		},
	}, nil, nil
}

// sanitizeUTF8 removes U+FFFD replacement characters and any invalid UTF-8 sequences.
func sanitizeUTF8(s string) string {
	// Fast path: if no replacement chars and valid UTF-8, return as-is
	if !strings.ContainsRune(s, utf8.RuneError) && utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			// Skip invalid byte
			i++
			continue
		}
		if r == utf8.RuneError {
			// Skip U+FFFD replacement character
			i += size
			continue
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

func errorResult(msg string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, nil, nil
}

func validationErrorResult(errs []string) (*mcp.CallToolResult, any, error) {
	return errorResult("Validation errors:\n- " + strings.Join(errs, "\n- "))
}

// derefStr returns the string value of a *string, or empty string if nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefFloat64 returns the float64 value of a *float64, or 0 if nil.
func derefFloat64(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func nodeFromInput(n RoadmapCreateNode) service.CreateRoadmapNodeRequest {
	return service.CreateRoadmapNodeRequest{
		TempID:      n.TempID,
		Title:       n.Title,
		Description: derefStr(n.Description),
		NodeType:    derefStr(n.NodeType),
		Status:      derefStr(n.Status),
		PositionX:   derefFloat64(n.PositionX),
		PositionY:   derefFloat64(n.PositionY),
		ParentID:    derefStr(n.ParentID),
		RefType:     derefStr(n.RefType),
		RefID:       derefStr(n.RefID),
		Metadata:    derefStr(n.Metadata),
		Stage:       derefStr(n.Stage),
		NodeDate:    derefStr(n.NodeDate),
		NodeEndDate: derefStr(n.NodeEndDate),
	}
}

func edgeFromInput(e RoadmapCreateEdge) service.CreateRoadmapEdgeRequest {
	return service.CreateRoadmapEdgeRequest{
		SourceNodeRef: e.SourceNodeRef,
		TargetNodeRef: e.TargetNodeRef,
		Label:         derefStr(e.Label),
		EdgeType:      derefStr(e.EdgeType),
	}
}

// derefMap reads an optional map input. Optional fields are pointers here so a
// client sending null does not fail schema validation, and a nil pointer has to
// mean "not mentioned" rather than "empty".
func derefMap(m *map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	return *m
}

// addMetadataReport tells a writer what happened to the metadata it sent.
//
// It is attached to a response and never stored, for the same reason
// BlockSaveReport is: a key that was dropped and a required field left empty
// are losses the author cannot see from the entry it gets back. Nothing here
// failed the write — an agent mid-interview that is refused loses the answers a
// person already gave — so saying what happened is the only correction channel
// there is.
func addMetadataReport(out map[string]any, entry *domain.Entry) {
	r := entry.MetaReport
	if r == nil {
		return
	}
	report := map[string]any{"spec_version": r.SpecVersion}
	if len(r.Stored) > 0 {
		report["stored"] = r.Stored
	}
	if len(r.UnknownKeys) > 0 {
		report["unknown_keys"] = r.UnknownKeys
	}
	if len(r.InvalidValues) > 0 {
		report["invalid_values"] = r.InvalidValues
	}
	if len(r.MissingRequired) > 0 {
		report["missing_required"] = r.MissingRequired
		report["hint"] = "Required fields are still unanswered. The metadata map replaces what is stored, so send the values you want to keep alongside the new ones; a null value records an explicit unknown, which is the honest answer when you do not know."
	}
	out["metadata_report"] = report
}

// addAnnotationReport tells a writer which marks its write moved or destroyed.
//
// The same contract as addMetadataReport and BlockSaveReport: attached to the
// response, never stored. This one matters most of the three, because the loss
// it reports is somebody else's: a person marked a sentence as needing a source
// and the write just deleted it. Without this the agent finds out never, and
// the reader finds out from a queue full of orphans nobody can explain.
func addAnnotationReport(out map[string]any, entry *domain.Entry) {
	r := entry.AnnReport
	if r == nil || r.Empty() {
		return
	}
	report := map[string]any{}
	if len(r.Drifted) > 0 {
		report["annotations_drifted"] = r.Drifted
	}
	if len(r.Orphaned) > 0 {
		report["annotations_orphaned"] = r.Orphaned
	}
	report["hint"] = "Text somebody had marked changed or is gone. Say so when you answer those marks — a doubt quietly buried by a rewrite is the thing this reports. annotation_list(research_id, entry_id) shows where they stand now."
	out["annotation_report"] = report
}

// derefInt reads an optional int input. Absent means "not mentioned", which for
// an offset or a limit is not the same as zero.
func derefInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

// derefBool reads an optional boolean. Absent and false mean the same thing for
// every flag this package has: the safe default is always "no".
func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
