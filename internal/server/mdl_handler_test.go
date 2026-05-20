package server_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/server"
	"github.com/wren-engine/wren/internal/service"
)

func TestPreviewEndpoint_Synthetic(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	preview := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	validation := service.NewValidationService()
	h := server.NewMDLHandler(preview, validation)

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"manifest": json.RawMessage(`{"catalog":"wren","schema":"main","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`),
		"sql":      "SELECT 1 AS a",
		"limit":    10,
	})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/mdl/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	cols := got["columns"].([]any)
	if len(cols) != 1 || cols[0].(map[string]any)["name"] != "a" {
		t.Errorf("cols: %+v", cols)
	}
}

func TestDryPlanV2_Base64Manifest(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	preview := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	h := server.NewMDLHandler(preview, service.NewValidationService())

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	manifest := `{"catalog":"wren","schema":"main","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`
	body, _ := json.Marshal(map[string]any{
		"manifestStr": base64.StdEncoding.EncodeToString([]byte(manifest)),
		"sql":         "SELECT 1",
	})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v2/mdl/dry-plan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dry-plan v2: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	out, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(out), "SELECT") {
		t.Errorf("expected planned SQL, got %q", string(out))
	}
}

func TestDryRunEndpoint_ReturnsColumnsOnly(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	preview := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	h := server.NewMDLHandler(preview, service.NewValidationService())

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"manifest": json.RawMessage(`{"catalog":"wren","schema":"main","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`),
		"sql":      "SELECT 1 AS a, 'x' AS b",
	})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/mdl/dry-run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	defer resp.Body.Close()
	var cols []map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&cols)
	if len(cols) != 2 || cols[0]["name"] != "a" || cols[1]["name"] != "b" {
		t.Errorf("cols: %+v", cols)
	}
}

func TestDryPlanEndpoint_ModelingOnlyFalse_GoesThroughConverter(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	preview := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	h := server.NewMDLHandler(preview, service.NewValidationService())

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"manifest":     json.RawMessage(`{"catalog":"wren","schema":"main","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`),
		"sql":          "SELECT COUNT(*) FROM (VALUES (1)) v(a)",
		"modelingOnly": false,
	})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/mdl/dry-plan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dry-plan: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	// modelingOnly=false → should have been lowercased by RewriteFunction.
	if !strings.Contains(string(out), "count(*)") {
		t.Errorf("expected DUCKDB-lowercased function, got %q", string(out))
	}
}
