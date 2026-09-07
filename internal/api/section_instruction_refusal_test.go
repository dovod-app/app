package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The issue's first acceptance criterion is that an over-long instruction is
// "refused with a field error", modelled on skills.description — which answers
// with `field` beside `error`.
//
// A sentence in an `error` key alone is not that. The section settings card has
// two editors on one card, a field spec and an instruction, and a refusal that
// does not name which one it refused makes the client guess.
func TestSectionInstruction_RefusalIsAFieldError(t *testing.T) {
	s := newShareServer(t)

	body := `{"instruction":"` + strings.Repeat("я", 501) + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/sections/"+s.sectionID, strings.NewReader(body))
	// A local caller: the fixture runs with no credential configured, where
	// only a loopback request may write. httptest defaults RemoteAddr to
	// 192.0.2.1 and Host to example.com, and auth.LocalRequest reads both.
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "localhost:8088"
	req.Header.Set("Content-Type", "application/json")
	s.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over-long instruction: got %d, want 400\n%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Error string `json:"error"`
		Field string `json:"field"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode refusal: %v\n%s", err, rec.Body.String())
	}
	if payload.Field != "instruction" {
		t.Errorf(`refusal named field %q, want "instruction"`, payload.Field)
	}
	// The editor renders `e.data.error`, so a field error that dropped the
	// sentence would leave the writer told nothing.
	if !strings.Contains(payload.Error, "500") {
		t.Errorf("the refusal does not say what the limit is: %q", payload.Error)
	}

	// A refusal on one field must not become the shape of every refusal: a
	// section that does not exist is still a 404 with no field.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/sections/does-not-exist", strings.NewReader(`{"display_name":"x"}`))
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "localhost:8088"
	req.Header.Set("Content-Type", "application/json")
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown section: got %d, want 404", rec.Code)
	}
}
