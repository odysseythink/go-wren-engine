package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoadFromFile_OverridesDefaults(t *testing.T) {
	cm := NewConfigManager()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	content := "wren.datasource.type=POSTGRES\nduckdb.memory-limit=512MB\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	ent, _ := cm.Get("wren.datasource.type")
	if ent.Value == nil || *ent.Value != "POSTGRES" {
		t.Fatalf("expected POSTGRES, got %+v", ent)
	}
	ent2, _ := cm.Get("duckdb.memory-limit")
	if ent2.Value == nil || *ent2.Value != "512MB" {
		t.Fatalf("expected 512MB, got %+v", ent2)
	}
}

func TestLoadFromFile_UnknownKeyInFileExtras(t *testing.T) {
	cm := NewConfigManager()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	content := "node.environment=production\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	all := cm.All()
	for _, e := range all {
		if e.Name == "node.environment" {
			t.Fatalf("node.environment should not be in All()")
		}
	}
}

func TestSyncToFile_RoundTrip(t *testing.T) {
	cm := NewConfigManager()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")

	if err := os.WriteFile(path, []byte("wren.datasource.type=POSTGRES\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	_ = cm.Set("wren.datasource.type", "MYSQL")

	if err := cm.SyncToFile(); err != nil {
		t.Fatalf("SyncToFile: %v", err)
	}

	cm2 := NewConfigManager()
	if err := cm2.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile round 2: %v", err)
	}
	ent, _ := cm2.Get("wren.datasource.type")
	if ent.Value == nil || *ent.Value != "MYSQL" {
		t.Fatalf("expected MYSQL after round-trip, got %+v", ent)
	}
}

func TestSyncToFile_AtomicWrite(t *testing.T) {
	cm := NewConfigManager()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	if err := os.WriteFile(path, []byte("key=old\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	_ = cm.Set("wren.datasource.type", "POSTGRES")

	if err := cm.SyncToFile(); err != nil {
		t.Fatalf("SyncToFile: %v", err)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestArchive_CreatesArchivedCopy(t *testing.T) {
	cm := NewConfigManager()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	if err := os.WriteFile(path, []byte("key=original\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if err := cm.Archive(); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	archiveDir := filepath.Join(tmpDir, "archived")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("ReadDir archived: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived file, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "config.properties.") {
		t.Fatalf("unexpected archive name: %s", entries[0].Name())
	}
}

func TestArchive_MultipleCallsMonotonic(t *testing.T) {
	cm := NewConfigManager()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	if err := os.WriteFile(path, []byte("k=v\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	if err := cm.Archive(); err != nil {
		t.Fatalf("Archive 1: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := cm.Archive(); err != nil {
		t.Fatalf("Archive 2: %v", err)
	}

	archiveDir := filepath.Join(tmpDir, "archived")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 archived files, got %d", len(entries))
	}
	name1 := entries[0].Name()
	name2 := entries[1].Name()
	if name1 >= name2 {
		t.Fatalf("timestamps not monotonic: %s vs %s", name1, name2)
	}
}

func TestReset_PreservesUnknownExtras(t *testing.T) {
	cm := NewConfigManager()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	content := "wren.datasource.type=POSTGRES\nnode.environment=prod\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	cm.Reset()

	ent, _ := cm.Get("wren.datasource.type")
	if ent.Value == nil || *ent.Value != "DUCKDB" {
		t.Fatalf("expected DUCKDB after reset, got %+v", ent)
	}

	if err := cm.SyncToFile(); err != nil {
		t.Fatalf("SyncToFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "node.environment=prod") {
		t.Fatalf("node.environment should be preserved after reset, got:\n%s", string(data))
	}
}

func TestConcurrentSet(t *testing.T) {
	cm := NewConfigManager()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.properties")
	if err := os.WriteFile(path, []byte("wren.datasource.type=DUCKDB\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(v string) {
			_ = cm.Set("wren.datasource.type", v)
			done <- true
		}("VAL" + strconv.Itoa(i))
	}
	for i := 0; i < 100; i++ {
		<-done
	}

	ent, _ := cm.Get("wren.datasource.type")
	if ent.Value == nil {
		t.Fatalf("expected non-nil value")
	}
}

func TestLoadFromFile_MissingFile(t *testing.T) {
	cm := NewConfigManager()
	err := cm.LoadFromFile("/nonexistent/path/config.properties")
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
