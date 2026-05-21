package config

import (
	"strings"
	"testing"
)

func TestParseYAML_Flat(t *testing.T) {
	input := `
wren.directory: /usr/src/app/etc/mdl
wren.datasource.type: DUCKDB
wren.experimental-enable-dynamic-fields: false
`
	props, err := parseYAML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseYAML error: %v", err)
	}
	if got := props["wren.directory"]; got != "/usr/src/app/etc/mdl" {
		t.Errorf("wren.directory = %q, want /usr/src/app/etc/mdl", got)
	}
	if got := props["wren.datasource.type"]; got != "DUCKDB" {
		t.Errorf("wren.datasource.type = %q, want DUCKDB", got)
	}
	if got := props["wren.experimental-enable-dynamic-fields"]; got != "false" {
		t.Errorf("wren.experimental-enable-dynamic-fields = %q, want false", got)
	}
}

func TestParseYAML_Nested(t *testing.T) {
	input := `
wren:
  directory: /usr/src/app/etc/mdl
  datasource:
    type: DUCKDB
  experimental-enable-dynamic-fields: false
duckdb:
  memory-limit: 268435456B
  max-concurrent-tasks: 10
`
	props, err := parseYAML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseYAML error: %v", err)
	}
	if got := props["wren.directory"]; got != "/usr/src/app/etc/mdl" {
		t.Errorf("wren.directory = %q, want /usr/src/app/etc/mdl", got)
	}
	if got := props["wren.datasource.type"]; got != "DUCKDB" {
		t.Errorf("wren.datasource.type = %q, want DUCKDB", got)
	}
	if got := props["duckdb.memory-limit"]; got != "268435456B" {
		t.Errorf("duckdb.memory-limit = %q, want 268435456B", got)
	}
	if got := props["duckdb.max-concurrent-tasks"]; got != "10" {
		t.Errorf("duckdb.max-concurrent-tasks = %q, want 10", got)
	}
}

func TestParseYAML_Types(t *testing.T) {
	input := `
port: 8080
enabled: true
ratio: 3.14
count: 42
empty: ~
`
	props, err := parseYAML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseYAML error: %v", err)
	}
	if got := props["port"]; got != "8080" {
		t.Errorf("port = %q, want 8080", got)
	}
	if got := props["enabled"]; got != "true" {
		t.Errorf("enabled = %q, want true", got)
	}
	if got := props["ratio"]; got != "3.14" {
		t.Errorf("ratio = %q, want 3.14", got)
	}
	if got := props["count"]; got != "42" {
		t.Errorf("count = %q, want 42", got)
	}
	if got := props["empty"]; got != "" {
		t.Errorf("empty = %q, want empty string", got)
	}
}

func TestWriteYAML(t *testing.T) {
	props := map[string]string{
		"wren.directory":     "/usr/src/app/etc/mdl",
		"wren.datasource.type": "DUCKDB",
	}
	var sb strings.Builder
	if err := writeYAML(&sb, props); err != nil {
		t.Fatalf("writeYAML error: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "wren.directory: /usr/src/app/etc/mdl") {
		t.Errorf("missing wren.directory in output:\n%s", out)
	}
	if !strings.Contains(out, "wren.datasource.type: DUCKDB") {
		t.Errorf("missing wren.datasource.type in output:\n%s", out)
	}
}

func TestConfigManager_LoadFromFile_YAML(t *testing.T) {
	cm := NewConfigManager()
	err := cm.LoadFromFile("../../etc/config.yaml")
	if err != nil {
		t.Fatalf("LoadFromFile yaml error: %v", err)
	}
	if cm.fileFormat != "yaml" {
		t.Errorf("fileFormat = %q, want yaml", cm.fileFormat)
	}
	if got, _ := cm.Get("wren.directory"); got.Value == nil || *got.Value != "/usr/src/app/etc/mdl" {
		t.Errorf("wren.directory mismatch")
	}
	if got, _ := cm.Get("wren.datasource.type"); got.Value == nil || *got.Value != "DUCKDB" {
		t.Errorf("wren.datasource.type mismatch")
	}
}

func TestConfigManager_LoadFromFile_Properties(t *testing.T) {
	cm := NewConfigManager()
	err := cm.LoadFromFile("../../etc/config.properties")
	if err != nil {
		t.Fatalf("LoadFromFile properties error: %v", err)
	}
	if cm.fileFormat != "properties" {
		t.Errorf("fileFormat = %q, want properties", cm.fileFormat)
	}
}
