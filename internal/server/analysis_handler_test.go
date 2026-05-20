package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAnalyzeSQLV2(t *testing.T) {
	h := NewAnalysisHandler()
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// base64 of {"catalog":"test","schema":"test","models":[]}
	reqBody, _ := json.Marshal(map[string]string{
		"manifestStr": "eyJjYXRhbG9nIjoidGVzdCIsInNjaGVtYSI6InRlc3QiLCJtb2RlbHMiOltdfQ==",
		"sql":         "SELECT 1",
	})
	// GET with body — matches Java JAX-RS @GET and the production capture flow.
	req := httptest.NewRequest(http.MethodGet, "/v2/analysis/sql", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 query analysis, got %d", len(result))
	}
}
