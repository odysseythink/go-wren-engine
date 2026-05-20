package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/config"
)

func newConfigRouter() (*config.ConfigManager, chi.Router) {
	cm := config.NewConfigManager()
	h := NewConfigHandler(cm)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return cm, r
}

func TestConfigGetAll(t *testing.T) {
	_, r := newConfigRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var entries []config.ConfigEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 11 {
		t.Fatalf("expected 11 entries, got %d", len(entries))
	}
	// Sorted by name: "duckdb.cache-task-retry-delay" precedes everything.
	if entries[0].Name != "duckdb.cache-task-retry-delay" {
		t.Fatalf("unexpected first entry: %s", entries[0].Name)
	}
}

func TestConfigGetSingleStringValue(t *testing.T) {
	_, r := newConfigRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/config/wren.datasource.type", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var entry config.ConfigEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry.Name != "wren.datasource.type" || entry.Value == nil || *entry.Value != "DUCKDB" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestConfigGetEmptyValueSerializesNull(t *testing.T) {
	_, r := newConfigRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/config/duckdb.home-directory", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Empty value MUST serialize as "value":null (Java parity, risk #5).
	if !bytes.Contains(w.Body.Bytes(), []byte(`"value":null`)) {
		t.Fatalf("expected value:null, got %s", w.Body.String())
	}
}

func TestConfigGetNotFound(t *testing.T) {
	_, r := newConfigRouter()
	req := httptest.NewRequest(http.MethodGet, "/v1/config/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestConfigPatchUpdatesNonStatic(t *testing.T) {
	cm, r := newConfigRouter()
	body, _ := json.Marshal([]map[string]any{
		{"name": "wren.datasource.type", "value": "POSTGRES"},
	})
	req := httptest.NewRequest(http.MethodPatch, "/v1/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	v, _ := cm.Get("wren.datasource.type")
	if v.Value == nil || *v.Value != "POSTGRES" {
		t.Fatalf("expected POSTGRES, got %+v", v)
	}
}

// Risk #7: static keys are silently skipped by Java.
func TestConfigPatchSilentlySkipsStaticKey(t *testing.T) {
	cm, r := newConfigRouter()
	body, _ := json.Marshal([]map[string]any{
		{"name": "duckdb.max-concurrent-tasks", "value": "99"},
	})
	req := httptest.NewRequest(http.MethodPatch, "/v1/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	v, _ := cm.Get("duckdb.max-concurrent-tasks")
	if v.Value == nil || *v.Value != "10" {
		t.Fatalf("static key was modified: got %+v", v)
	}
}

func TestConfigPatchUnknownKey(t *testing.T) {
	_, r := newConfigRouter()
	body, _ := json.Marshal([]map[string]any{{"name": "unknown", "value": "x"}})
	req := httptest.NewRequest(http.MethodPatch, "/v1/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestConfigDeleteResets(t *testing.T) {
	cm, r := newConfigRouter()
	_ = cm.Set("wren.datasource.type", "POSTGRES")

	req := httptest.NewRequest(http.MethodDelete, "/v1/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	v, _ := cm.Get("wren.datasource.type")
	if v.Value == nil || *v.Value != "DUCKDB" {
		t.Fatalf("expected reset to DUCKDB, got %+v", v)
	}
}

func TestConfigPatch_PersistsToDisk(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	if err := os.WriteFile(path, []byte("wren.datasource.type=DUCKDB\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cm := config.NewConfigManager()
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	h := NewConfigHandler(cm)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	body, _ := json.Marshal([]map[string]any{
		{"name": "wren.datasource.type", "value": "POSTGRES"},
	})
	req := httptest.NewRequest(http.MethodPatch, "/v1/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte("wren.datasource.type=POSTGRES")) {
		t.Fatalf("file does not contain PATCHed value:\n%s", string(data))
	}

	archiveDir := filepath.Join(tmpDir, "archived")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("ReadDir archived: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived file, got %d", len(entries))
	}
}

func TestConfigPatch_StaticKey_NoArchiveNoSync(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	if err := os.WriteFile(path, []byte("duckdb.max-concurrent-tasks=10\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cm := config.NewConfigManager()
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	h := NewConfigHandler(cm)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	body, _ := json.Marshal([]map[string]any{
		{"name": "duckdb.max-concurrent-tasks", "value": "99"},
	})
	req := httptest.NewRequest(http.MethodPatch, "/v1/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	archiveDir := filepath.Join(tmpDir, "archived")
	_, err := os.Stat(archiveDir)
	if err == nil {
		t.Fatalf("archive dir should not exist for static-key PATCH")
	}
}

func TestConfigDelete_PersistsToDisk(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	if err := os.WriteFile(path, []byte("wren.datasource.type=POSTGRES\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cm := config.NewConfigManager()
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	h := NewConfigHandler(cm)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodDelete, "/v1/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte("wren.datasource.type=DUCKDB")) {
		t.Fatalf("file should contain default DUCKDB after DELETE:\n%s", string(data))
	}

	archiveDir := filepath.Join(tmpDir, "archived")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("ReadDir archived: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived file, got %d", len(entries))
	}
}
