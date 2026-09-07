package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dovod-app/app/internal/domain"
)

func TestMemoryRoutes_CRUDConflictAndLegacyRejection(t *testing.T) {
	s := newShareServer(t)
	base := "/api/researches/" + s.research.Code + "/memory"
	request := func(method, path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		// No api_token and no accounts: writes are taken from a client on the
		// server's own machine only. httptest defaults RemoteAddr to 192.0.2.1
		// and Host to example.com, and auth.LocalRequest reads both.
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = "localhost:8088"
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: %d, want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	var initial struct {
		Data domain.Memory `json:"data"`
	}
	if err := json.Unmarshal(request("GET", base, nil, 200).Body.Bytes(), &initial); err != nil || len(initial.Data) != 1 {
		t.Fatalf("initial memory: %+v %v", initial.Data, err)
	}
	w := request("POST", base, map[string]any{"text": "from browser", "session_id": s.sessionID, "author": "agent"}, http.StatusCreated)
	var result struct {
		Data domain.MemoryItem `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	item := result.Data
	if item.Author != "user" || item.SessionID != s.sessionID || item.ID == "" {
		t.Fatalf("HTTP attribution: %+v", item)
	}
	request("PATCH", base+"/"+item.ID, map[string]any{"text": "edited", "version": 1}, 204)
	request("PATCH", base+"/"+item.ID, map[string]any{"text": "stale", "version": 1}, 409)
	request("PATCH", base+"/"+item.ID, map[string]any{"text": "unversioned"}, 400)
	request("POST", base, map[string]any{"text": "  "}, 400)
	request("POST", base+"/bulk-delete", map[string]any{"ids": []string{}}, 400)
	request("GET", base, nil, 200)
	request("DELETE", base+"/"+item.ID, nil, 204)
	request("POST", base, map[string]any{"text": "keep the later append"}, 201)
	request("POST", base+"/bulk-delete", map[string]any{"ids": []string{initial.Data[0].ID, item.ID}}, 204)
	w = request("GET", base, nil, 200)
	var listed struct {
		Data domain.Memory `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Text != "keep the later append" {
		t.Fatalf("bulk delete lost an unselected note: %+v", listed.Data)
	}
	request("PATCH", base+"/"+item.ID, map[string]any{"text": "deleted", "version": 2}, 404)
	request("PUT", "/api/researches/"+s.research.ID, map[string]any{"memory": []string{"stale snapshot"}}, 400)
	request("PUT", "/api/researches/"+s.research.ID, map[string]any{"instruction": "old client"}, 400)
	request("PUT", "/api/researches/"+s.research.ID, map[string]any{"memory": nil}, 400)
}
