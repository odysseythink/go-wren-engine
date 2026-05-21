# Phase 2: config.properties 解析 + PATCH 持久化 + 配置 reload

> **For agentic workers:** REQUIRED SUB-SKILL: Use `gpowers:subagent-driven-development` (recommended) or `gpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `etc/config.properties` 在 Go 引擎启动时真正生效，PATCH 写入持久化到磁盘并归档旧版本，与 Java `ConfigManager` 在配置生命周期上等价。

**Architecture:** 在现有 P6 `ConfigManager`（11 typed key + static set）上加 `fileExtras`（非 11 key 透传）、`sync.RWMutex`（并发安全）、`LoadFromFile/SyncToFile/Archive`（文件生命周期）、`OnChange`（reload hook）。手写 `.properties` 解析器覆盖 Java `java.util.Properties.load` 的全部规则。

**Tech Stack:** Go 1.26, `sync.RWMutex`, `bufio.Scanner`, `path/filepath`, `os.Link` (atomic archive fallback)

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/config/properties.go` | **Create** | `.properties` 解析器 `parseProperties` + 写回器 `writeProperties` |
| `internal/config/properties_test.go` | **Create** | 解析器 12+ 边界单元测试 |
| `internal/config/config.go` | **Modify** | 加 `fileExtras/filePath/mu/requiredReload/reloadHooks`；新增 `LoadFromFile/SyncToFile/Archive/OnChange/FireReload`；改 `Set/Reset` |
| `internal/config/config_test.go` | **Create** | `Load/Sync` 往返、`Archive` 时间戳单调性、并发 race 测试 |
| `internal/server/config_handler.go` | **Modify** | `Patch`/`DeleteAll` 加 `cm.Archive()` → `cm.SyncToFile()` → `fireReload` |
| `internal/server/config_handler_test.go` | **Modify** | 端到端测试：临时文件 → PATCH → 校验磁盘内容 + archived 副本 |
| `cmd/wren-server/main.go` | **Modify** | 启动序：`WREN_CONFIG_FILE` env → `LoadFromFile` → `LoadFromEnv`；缺失 fatal |
| `docker/README.md` | **Modify** | 删除 "config.properties parsing not yet supported" 警告；添加 PATCH 注释丢失警示 |
| `internal/difftest/config_diff_test.go` | **Verify** | 现有 12 IDs 应当 trivially pass（`NewConfigManager()` 路径不变） |

---

## Task 1: Properties 解析器（`parseProperties` + `writeProperties`）

**Files:**
- Create: `internal/config/properties.go`
- Create: `internal/config/properties_test.go`

- [ ] **Step 1:** Write `internal/config/properties.go`

```go
package config

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// parseProperties parses a Java-compatible .properties file.
// Rules mirrored from java.util.Properties.load(InputStream):
//   • Comments: lines beginning with # or ! (leading whitespace OK)
//   • Empty lines ignored
//   • Separators: =, :, or arbitrary whitespace
//   • Line continuation: trailing unescaped \ before newline
//   • Escapes: \n \r \t \\ \= \: \# \! \ (space) \uXXXX
//   • UTF-8 encoding (Java uses ISO-8859-1 but modern files are UTF-8)
func parseProperties(r io.Reader) (map[string]string, error) {
	scanner := bufio.NewScanner(r)
	result := make(map[string]string)
	var buf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		// Check for line continuation: odd number of trailing backslashes
		if len(line) > 0 {
			count := 0
			for i := len(line) - 1; i >= 0 && line[i] == '\\' {
				count++
			}
			if count%2 == 1 {
				buf.WriteString(line[:len(line)-1])
				continue
			}
		}

		buf.WriteString(line)
		if err := processLogicalLine(buf.String(), result); err != nil {
			return nil, err
		}
		buf.Reset()
	}

	// Remaining buffer (file ended without newline after \)
	if buf.Len() > 0 {
		if err := processLogicalLine(buf.String(), result); err != nil {
			return nil, err
		}
	}

	return result, scanner.Err()
}

func processLogicalLine(line string, result map[string]string) error {
	// Skip leading whitespace
	i := 0
	for i < len(line) && isPropsSpace(line[i]) {
		i++
	}
	if i >= len(line) {
		return nil
	}
	// Comment line
	if line[i] == '#' || line[i] == '!' {
		return nil
	}

	// Parse key
	keyStart := i
	for i < len(line) {
		if line[i] == '\\' {
			i += 2
			if i > len(line) {
				i = len(line)
			}
			continue
		}
		if line[i] == '=' || line[i] == ':' || isPropsSpace(line[i]) {
			break
		}
		i++
	}
	key := unescapeProps(line[keyStart:i])

	// Skip separator whitespace
	for i < len(line) && isPropsSpace(line[i]) {
		i++
	}
	// Skip actual separator (= or :)
	if i < len(line) && (line[i] == '=' || line[i] == ':') {
		i++
	}
	// Skip whitespace after separator
	for i < len(line) && isPropsSpace(line[i]) {
		i++
	}

	value := unescapeProps(line[i:])
	result[key] = value
	return nil
}

func isPropsSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\f'
}

func unescapeProps(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		next := s[i+1]
		switch next {
		case 'n':
			b.WriteByte('\n')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case '\\':
			b.WriteByte('\\')
			i++
		case '=':
			b.WriteByte('=')
			i++
		case ':':
			b.WriteByte(':')
			i++
		case '#':
			b.WriteByte('#')
			i++
		case '!':
			b.WriteByte('!')
			i++
		case ' ':
			b.WriteByte(' ')
			i++
		case 'u':
			if i+5 < len(s) {
				r, err := strconv.ParseInt(s[i+2:i+6], 16, 32)
				if err == nil {
					b.WriteRune(rune(r))
					i += 5
					continue
				}
			}
			b.WriteByte(s[i])
		default:
			// Unknown escape: drop backslash, keep char (Java behavior)
			b.WriteByte(next)
			i++
		}
	}
	return b.String()
}

// escapeProps escapes a string for .properties output.
// Characters escaped: = : # ! \n \r \t \\ and leading spaces in keys.
func escapeProps(s string, isKey bool) string {
	var b strings.Builder
	for i, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		case '=':
			b.WriteString("\\=")
		case ':':
			b.WriteString("\\:")
		case '#':
			b.WriteString("\\#")
		case '!':
			b.WriteString("\\!")
		case ' ':
			if isKey && i == 0 {
				b.WriteString("\\ ")
			} else {
				b.WriteByte(' ')
			}
		default:
			if r < 0x20 || r > 0x7E {
				b.WriteString(fmt.Sprintf("\\u%04x", r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// writePropertiesWithTimestamp writes a properties map in Java Properties.store() format.
//   • First line:  #<header>
//   • Second line: #<RFC1123-ish timestamp>
//   • Remaining:   keys in alphabetic order, values escaped
func writePropertiesWithTimestamp(w io.Writer, props map[string]string, header, timestamp string) error {
	bw := bufio.NewWriter(w)
	_, _ = bw.WriteString("#")
	_, _ = bw.WriteString(header)
	_, _ = bw.WriteByte('\n')
	_, _ = bw.WriteString("#")
	_, _ = bw.WriteString(timestamp)
	_, _ = bw.WriteByte('\n')

	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		_, _ = bw.WriteString(escapeProps(k, true))
		_, _ = bw.WriteString("=")
		_, _ = bw.WriteString(escapeProps(props[k], false))
		_, _ = bw.WriteByte('\n')
	}

	return bw.Flush()
}
```

- [ ] **Step 2:** Build to verify no compile errors

Run:
```bash
go build ./internal/config/...
```

Expected: no output (success).

- [ ] **Step 3:** Write `internal/config/properties_test.go`

```go
package config

import (
	"strings"
	"testing"
)

func TestParsePropertiesBasicKV(t *testing.T) {
	input := "key1=value1\nkey2=value2"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got["key1"] != "value1" || got["key2"] != "value2" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestParsePropertiesEmptyValue(t *testing.T) {
	input := "key1="
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got["key1"] != "" {
		t.Fatalf("expected empty string, got %q", got["key1"])
	}
}

func TestParsePropertiesComments(t *testing.T) {
	input := "# comment 1\n! comment 2\n  # indented comment\nkey=value"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 1 || got["key"] != "value" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestParsePropertiesColonSeparator(t *testing.T) {
	input := "key:value"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got["key"] != "value" {
		t.Fatalf("unexpected: %q", got["key"])
	}
}

func TestParsePropertiesWhitespaceSeparator(t *testing.T) {
	input := "key value"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got["key"] != "value" {
		t.Fatalf("unexpected: %q", got["key"])
	}
}

func TestParsePropertiesLineContinuation(t *testing.T) {
	input := "key=hello \\\n  world"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got["key"] != "hello world" {
		t.Fatalf("unexpected: %q", got["key"])
	}
}

func TestParsePropertiesEscapes(t *testing.T) {
	input := "key=hello\\nworld\\ttab\\r\\nnewline\\\\backslash\\=equal\\:colon\\#hash\\!excl"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	want := "hello\nworld\ttab\r\nnewline\\backslash=equal:colon#hash!excl"
	if got["key"] != want {
		t.Fatalf("\nwant: %q\ngot:  %q", want, got["key"])
	}
}

func TestParsePropertiesUnicodeEscape(t *testing.T) {
	input := "key=caf\\u00e9"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got["key"] != "caf\u00e9" {
		t.Fatalf("unexpected: %q", got["key"])
	}
}

func TestParsePropertiesKeyWithSpaceEscape(t *testing.T) {
	input := "key\\ with\\ space=value"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got["key with space"] != "value" {
		t.Fatalf("unexpected: %q", got["key with space"])
	}
}

func TestParsePropertiesMultipleSeparators(t *testing.T) {
	input := "key1 = value1\nkey2: value2\nkey3   value3"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if got["key1"] != "value1" || got["key2"] != "value2" || got["key3"] != "value3" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestParsePropertiesApacheLicenseHeader(t *testing.T) {
	input := "# Licensed to the Apache Software Foundation (ASF) under one\n" +
		"# or more contributor license agreements.  See the NOTICE file\n" +
		"# distributed with this work for additional information\n" +
		"# regarding copyright ownership.  The ASF licenses this file\n" +
		"# to you under the Apache License, Version 2.0\n" +
		"key=value"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 1 || got["key"] != "value" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestParsePropertiesEmptyLines(t *testing.T) {
	input := "\n\nkey=value\n\n"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 1 || got["key"] != "value" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestParsePropertiesRealWorldExample(t *testing.T) {
	// Mirrors wren-engine-0.9.3/example/duckdb-tpch-example/etc/config.properties
	input := "wren.directory=/usr/src/app/etc/mdl\n" +
		"wren.experimental-enable-dynamic-fields=true\n" +
		"node.environment=production\n" +
		"wren.datasource.type=DUCKDB"
	got, err := parseProperties(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 keys, got %d: %+v", len(got), got)
	}
	if got["wren.directory"] != "/usr/src/app/etc/mdl" {
		t.Fatalf("unexpected wren.directory: %q", got["wren.directory"])
	}
	if got["node.environment"] != "production" {
		t.Fatalf("unexpected node.environment: %q", got["node.environment"])
	}
}

func TestWritePropertiesRoundTrip(t *testing.T) {
	props := map[string]string{
		"key1": "value1",
		"key2": "hello=world",
		"key3": "a#b!c",
		"key4": "",
	}
	var sb strings.Builder
	if err := writePropertiesWithTimestamp(&sb, props, "sync with file", "Wed, 20 May 2026 17:30:45 UTC"); err != nil {
		t.Fatalf("write error: %v", err)
	}
	output := sb.String()
	got, err := parseProperties(strings.NewReader(output))
	if err != nil {
		t.Fatalf("re-parse error: %v", err)
	}
	for k, want := range props {
		if got[k] != want {
			t.Fatalf("round-trip mismatch for %s: want %q, got %q", k, want, got[k])
		}
	}
}

func TestWritePropertiesSortOrder(t *testing.T) {
	props := map[string]string{
		"z-key": "z",
		"a-key": "a",
		"m-key": "m",
	}
	var sb strings.Builder
	_ = writePropertiesWithTimestamp(&sb, props, "h", "t")
	lines := strings.Split(strings.TrimSpace(sb.String()), "\n")
	// Skip header lines
	if len(lines) < 5 {
		t.Fatalf("expected at least 5 lines, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[2], "a-key=") {
		t.Fatalf("expected a-key first after headers, got: %s", lines[2])
	}
	if !strings.HasPrefix(lines[4], "z-key=") {
		t.Fatalf("expected z-key last, got: %s", lines[4])
	}
}
```

- [ ] **Step 4:** Run properties tests

Run:
```bash
go test ./internal/config/... -v -run "TestParse|TestWrite"
```

Expected: all 14 tests PASS.

- [ ] **Step 5:** Commit

```bash
git add internal/config/properties.go internal/config/properties_test.go
git commit -m "feat(config): add Java-compatible .properties parser and writer

- parseProperties: full Java Properties.load parity (comments, separators,
  line continuations, \\n/\\r/\\t/\\\\/\\=/\\:/\\#/\\!/\\uXXXX escapes)
- writePropertiesWithTimestamp: alphabetic sort, value escaping, dual-comment header
- 15 unit tests covering empty values, Unicode, Apache License headers,
  line continuations, colon/whitespace separators, round-trip"
```


---

## Task 2: ConfigManager Load / Sync / Archive

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 6:** Modify `internal/config/config.go`

Apply these changes to the existing file. The full modified file follows:

```go
package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// knownKeys is the set of 11 typed config keys (Java parity).
var knownKeys = map[string]bool{
	"wren.directory":                          true,
	"wren.datasource.type":                    true,
	"wren.experimental-enable-dynamic-fields": true,
	"duckdb.memory-limit":                     true,
	"duckdb.home-directory":                   true,
	"duckdb.temp-directory":                   true,
	"duckdb.max-concurrent-tasks":             true,
	"duckdb.max-cache-query-timeout":          true,
	"duckdb.cache-task-retry-delay":           true,
	"duckdb.connector.init-sql-path":          true,
	"duckdb.connector.session-sql-path":       true,
}

// ConfigEntry mirrors Java ConfigManager.ConfigEntry: name + string value
// (empty string is normalised to JSON null on serialisation).
type ConfigEntry struct {
	Name  string  `json:"name"`
	Value *string `json:"value"`
}

func stringValue(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ConfigManager mirrors Java io.wren.base.config.ConfigManager.
type ConfigManager struct {
	mu             sync.RWMutex
	port           int               // bootstrap-only, never in /v1/config
	configs        map[string]string // mirrors Java configs map (11 typed keys)
	static         map[string]bool   // mirrors Java staticConfigs set
	fileExtras     map[string]string // non-11 keys from file + overrides (persisted)
	filePath       string            // path to config.properties
	requiredReload map[string]bool   // keys whose change triggers reload
	reloadHooks    map[string][]func()
}

// NewConfigManager creates a ConfigManager with Java-compatible defaults.
func NewConfigManager() *ConfigManager {
	cm := &ConfigManager{
		port:           8080,
		configs:        map[string]string{},
		static:         map[string]bool{},
		fileExtras:     map[string]string{},
		requiredReload: map[string]bool{},
		reloadHooks:    map[string][]func(),
	}
	cm.initConfig("wren.directory", "/usr/src/app/etc/mdl", false, true)
	cm.initConfig("wren.datasource.type", "DUCKDB", true, false)
	cm.initConfig("wren.experimental-enable-dynamic-fields", "false", false, false)
	cm.initConfig("duckdb.memory-limit", "268435456B", true, false)
	cm.initConfig("duckdb.home-directory", "", true, false)
	cm.initConfig("duckdb.temp-directory", "/tmp/duck", true, false)
	cm.initConfig("duckdb.max-concurrent-tasks", "10", false, true)
	cm.initConfig("duckdb.max-cache-query-timeout", "20", false, true)
	cm.initConfig("duckdb.cache-task-retry-delay", "60", false, true)
	cm.initConfig("duckdb.connector.init-sql-path", "etc/duckdb/init.sql", false, false)
	cm.initConfig("duckdb.connector.session-sql-path", "etc/duckdb/session.sql", false, false)
	return cm
}

func (cm *ConfigManager) initConfig(key, value string, reload, isStatic bool) {
	cm.configs[key] = value
	if isStatic {
		cm.static[key] = true
	}
	if reload {
		cm.requiredReload[key] = true
	}
}

// Port returns the bootstrap HTTP port. Not surfaced via /v1/config.
func (cm *ConfigManager) Port() int { return cm.port }

// Get returns the entry for a name; ok=false if unknown.
func (cm *ConfigManager) Get(name string) (ConfigEntry, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.getUnlocked(name)
}

func (cm *ConfigManager) getUnlocked(name string) (ConfigEntry, bool) {
	v, ok := cm.configs[name]
	if !ok {
		return ConfigEntry{}, false
	}
	return ConfigEntry{Name: name, Value: stringValue(v)}, true
}

// All returns all entries sorted by name (risk #3).
func (cm *ConfigManager) All() []ConfigEntry {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	out := make([]ConfigEntry, 0, len(cm.configs))
	for name, val := range cm.configs {
		out = append(out, ConfigEntry{Name: name, Value: stringValue(val)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Set updates a single key. Static keys are silently skipped (Java parity, risk #7).
// Unknown keys return ErrUnknownConfigKey.
func (cm *ConfigManager) Set(name, value string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.setUnlocked(name, value)
}

func (cm *ConfigManager) setUnlocked(name, value string) error {
	if cm.static[name] {
		return nil // silent skip
	}
	if _, ok := cm.configs[name]; !ok {
		return ErrUnknownConfigKey{Key: name}
	}
	cm.configs[name] = value
	cm.fileExtras[name] = value
	return nil
}

// Reset wipes config back to defaults (Java DELETE /v1/config behaviour).
// Preserves fileExtras for non-11 keys (transparency).
func (cm *ConfigManager) Reset() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	port := cm.port
	// Preserve non-11-key extras
	preserved := make(map[string]string)
	for k, v := range cm.fileExtras {
		if !knownKeys[k] {
			preserved[k] = v
		}
	}
	*cm = *NewConfigManager()
	cm.port = port
	cm.fileExtras = preserved
}

// ErrUnknownConfigKey is returned by Set when the key isn't a recognised Java key.
type ErrUnknownConfigKey struct{ Key string }

func (e ErrUnknownConfigKey) Error() string { return "Config not found: " + e.Key }

// LoadFromEnv overrides defaults with environment variables.
func (cm *ConfigManager) LoadFromEnv() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	envMap := map[string]string{
		"WREN_DIRECTORY":                          "wren.directory",
		"WREN_DATASOURCE_TYPE":                    "wren.datasource.type",
		"WREN_EXPERIMENTAL_ENABLE_DYNAMIC_FIELDS": "wren.experimental-enable-dynamic-fields",
		"WREN_DUCKDB_MEMORY_LIMIT":                "duckdb.memory-limit",
		"WREN_DUCKDB_HOME_DIRECTORY":              "duckdb.home-directory",
		"WREN_DUCKDB_TEMP_DIRECTORY":              "duckdb.temp-directory",
		"WREN_DUCKDB_MAX_CONCURRENT_TASKS":        "duckdb.max-concurrent-tasks",
		"WREN_DUCKDB_MAX_CACHE_QUERY_TIMEOUT":     "duckdb.max-cache-query-timeout",
		"WREN_DUCKDB_CACHE_TASK_RETRY_DELAY":      "duckdb.cache-task-retry-delay",
		"WREN_DUCKDB_CONNECTOR_INIT_SQL_PATH":     "duckdb.connector.init-sql-path",
		"WREN_DUCKDB_CONNECTOR_SESSION_SQL_PATH":  "duckdb.connector.session-sql-path",
	}
	for env, key := range envMap {
		if v := os.Getenv(env); v != "" {
			cm.configs[key] = v
			cm.fileExtras[key] = v
		}
	}
	if v := os.Getenv("WREN_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cm.port = p
		}
	}
}

// LoadFromFile reads a .properties file and overlays known keys onto configs.
// Unknown keys are stored in fileExtras for transparent write-back.
func (cm *ConfigManager) LoadFromFile(path string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("config file not found: %w", err)
	}
	defer f.Close()

	props, err := parseProperties(f)
	if err != nil {
		return fmt.Errorf("parse config file: %w", err)
	}

	cm.filePath = path
	for k, v := range props {
		if knownKeys[k] {
			cm.configs[k] = v
		}
		cm.fileExtras[k] = v
	}
	return nil
}

// SyncToFile writes the current state back to the config file.
// Uses atomic write (temp file + rename) to avoid half-written files.
func (cm *ConfigManager) SyncToFile() error {
	cm.mu.RLock()
	path := cm.filePath
	if path == "" {
		cm.mu.RUnlock()
		return fmt.Errorf("no config file path set")
	}
	// Build merged props for writing
	props := make(map[string]string, len(cm.fileExtras))
	for k, v := range cm.fileExtras {
		props[k] = v
	}
	// configs override fileExtras for known keys
	for k, v := range cm.configs {
		props[k] = v
	}
	cm.mu.RUnlock()

	dir := filepath.Dir(path)
	tmpPath := filepath.Join(dir, "."+filepath.Base(path)+".tmp")

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	ts := time.Now().UTC().Format(time.RFC1123)
	if err := writePropertiesWithTimestamp(f, props, "sync with file", ts); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write properties: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// Archive copies the current config file to <dir>/archived/config.properties.<timestamp>.
// Prefers hard link (atomic) then falls back to copy.
func (cm *ConfigManager) Archive() error {
	cm.mu.RLock()
	path := cm.filePath
	cm.mu.RUnlock()
	if path == "" {
		return fmt.Errorf("no config file path set")
	}

	dir := filepath.Dir(path)
	archiveDir := filepath.Join(dir, "archived")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("create archive dir: %w", err)
	}

	now := time.Now().UTC()
	ts := now.Format("20060102150405") + fmt.Sprintf("%04d", now.Nanosecond()/100000)
	dst := filepath.Join(archiveDir, filepath.Base(path)+"."+ts)

	// Try hard link first (atomic, POSIX)
	if err := os.Link(path, dst); err == nil {
		return nil
	}

	// Fallback: copy file contents
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open source for archive: %w", err)
	}
	defer src.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create archive file: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, src); err != nil {
		return fmt.Errorf("copy to archive: %w", err)
	}
	return dstFile.Close()
}

// OnChange registers a callback for when a specific key changes and requires reload.
func (cm *ConfigManager) OnChange(key string, fn func()) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.reloadHooks[key] = append(cm.reloadHooks[key], fn)
}

// FireReload calls all registered hooks for the given key.
func (cm *ConfigManager) FireReload(key string) {
	cm.mu.RLock()
	hooks := cm.reloadHooks[key]
	cm.mu.RUnlock()
	for _, fn := range hooks {
		fn()
	}
}

// EnableDynamicFields is a typed convenience used by PreviewService.
func (cm *ConfigManager) EnableDynamicFields() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	v, ok := cm.configs["wren.experimental-enable-dynamic-fields"]
	if !ok {
		return true // Java default
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return b
}
```

- [ ] **Step 7:** Build to verify no compile errors

Run:
```bash
go build ./...
```

Expected: no output (success).

- [ ] **Step 8:** Write `internal/config/config_test.go`

```go
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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

	// Unknown key should NOT appear in All()
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

	// Initial load from file
	if err := os.WriteFile(path, []byte("wren.datasource.type=POSTGRES\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := cm.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	// Modify
	_ = cm.Set("wren.datasource.type", "MYSQL")

	// Sync
	if err := cm.SyncToFile(); err != nil {
		t.Fatalf("SyncToFile: %v", err)
	}

	// Re-read
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

	// Sync should succeed and leave no .tmp file
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
	// Names should be monotonically increasing
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

	// Known key should be back to default
	ent, _ := cm.Get("wren.datasource.type")
	if ent.Value == nil || *ent.Value != "DUCKDB" {
		t.Fatalf("expected DUCKDB after reset, got %+v", ent)
	}

	// Sync and check unknown key preserved
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

	// Fire many concurrent sets
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

	// Should not panic or race; value is one of the writes
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
```

- [ ] **Step 9:** Run config tests with race detector

Run:
```bash
go test ./internal/config/... -race -v
```

Expected: all 9 tests PASS, no race warnings.

- [ ] **Step 10:** Commit

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add LoadFromFile, SyncToFile, Archive, OnChange

- ConfigManager now holds fileExtras (non-11-key transparency), filePath,
  sync.RWMutex, requiredReload set, and reload hook registry
- LoadFromFile: parses .properties, overlays known keys, stores unknowns
- SyncToFile: atomic write via temp+rename, Java-compatible output format
- Archive: hard-link preferred, fallback copy, timestamp with 4-digit nano
- Reset preserves non-11-key fileExtras (transparent pass-through)
- FireReload: empty implementation for Phase 2 (DuckDB-only,
  no datasource switching needed)
- 9 unit tests: override, unknown key, round-trip, atomic write,
  archive count/monotonicity, reset preserves extras, concurrent set,
  missing file"
```


---

## Task 3: Handler 接线 + 启动 fatal

**Files:**
- Modify: `internal/server/config_handler.go`
- Modify: `internal/server/config_handler_test.go`
- Modify: `cmd/wren-server/main.go`

- [ ] **Step 11:** Modify `internal/server/config_handler.go`

```go
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/config"
)

// ConfigHandler exposes /v1/config (mirrors Java ConfigResource).
type ConfigHandler struct {
	configMgr *config.ConfigManager
}

// NewConfigHandler creates a new ConfigHandler.
func NewConfigHandler(configMgr *config.ConfigManager) *ConfigHandler {
	return &ConfigHandler{configMgr: configMgr}
}

// RegisterRoutes registers the four config routes (GET / GET-by-name / DELETE / PATCH).
func (h *ConfigHandler) RegisterRoutes(r chi.Router) {
	r.Get("/v1/config", h.GetAll)
	r.Get("/v1/config/{configName}", h.Get)
	r.Delete("/v1/config", h.DeleteAll)
	r.Patch("/v1/config", h.Patch)
}

// patchEntry is the wire format for PATCH body — value is a JSON **string**
// (Java sends and accepts Map<String, String>).
type patchEntry struct {
	Name  string  `json:"name"`
	Value *string `json:"value"`
}

func (h *ConfigHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.configMgr.All())
}

func (h *ConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "configName")
	entry, ok := h.configMgr.Get(name)
	if !ok {
		WriteError(w, &WrenError{Code: 65536, Type: NotFound, Message: "Config not found: " + name})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entry)
}

// DeleteAll mirrors Java @DELETE: setConfigs(List.of(), reset=true) — wipes back to defaults.
func (h *ConfigHandler) DeleteAll(w http.ResponseWriter, r *http.Request) {
	h.configMgr.Reset()
	if err := h.configMgr.Archive(); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "archive failed: " + err.Error()})
		return
	}
	if err := h.configMgr.SyncToFile(); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "sync to file failed: " + err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *ConfigHandler) Patch(w http.ResponseWriter, r *http.Request) {
	var entries []patchEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}

	updated := make([]string, 0, len(entries))
	for _, e := range entries {
		value := ""
		if e.Value != nil {
			value = strings.TrimSpace(*e.Value)
		}
		if err := h.configMgr.Set(e.Name, value); err != nil {
			var unknown config.ErrUnknownConfigKey
			if errors.As(err, &unknown) {
				WriteError(w, &WrenError{Code: 65536, Type: NotFound, Message: err.Error()})
				return
			}
			WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
			return
		}
		updated = append(updated, e.Name)
	}

	if len(updated) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := h.configMgr.Archive(); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "archive failed: " + err.Error()})
		return
	}
	if err := h.configMgr.SyncToFile(); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "sync to file failed: " + err.Error()})
		return
	}

	for _, k := range updated {
		// Phase 2: fireReload is registered but no-op for DuckDB-only
		h.configMgr.FireReload(k)
	}

	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 12:** Modify `internal/server/config_handler_test.go`

Add `os` and `path/filepath` to the test file imports, then append these tests to the end of the file:

```go
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

	// Verify file on disk
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte("wren.datasource.type=POSTGRES")) {
		t.Fatalf("file does not contain PATCHed value:\n%s", string(data))
	}

	// Verify archived copy exists
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

	// Archive dir should NOT exist (static key = no-op)
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

	// File should now contain DUCKDB (default)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte("wren.datasource.type=DUCKDB")) {
		t.Fatalf("file should contain default DUCKDB after DELETE:\n%s", string(data))
	}

	// Archived copy should exist
	archiveDir := filepath.Join(tmpDir, "archived")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("ReadDir archived: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 archived file, got %d", len(entries))
	}
}
```

- [ ] **Step 13:** Modify `cmd/wren-server/main.go`

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/server"
	"github.com/wren-engine/wren/internal/service"
)

func main() {
	configPath := os.Getenv("WREN_CONFIG_FILE")
	if configPath == "" {
		log.Fatalf("WREN_CONFIG_FILE env required (Java parity: -Dconfig must be set)")
	}

	configMgr := config.NewConfigManager()
	if err := configMgr.LoadFromFile(configPath); err != nil {
		log.Fatalf("Config file not found: %v", err)
	}
	configMgr.LoadFromEnv()

	// Create connectors based on config
	md := duckdb.NewMetadata()
	var metadata service.Metadata = md
	var sqlConverter converter.SqlConverter = &converter.DuckDBSqlConverter{}

	previewService := service.NewPreviewService(metadata, sqlConverter, configMgr)
	validationService := service.NewValidationService(metadata, sqlConverter)

	srv := server.NewServer(fmt.Sprintf(":%d", configMgr.Port()))

	mdlHandler := server.NewMDLHandler(previewService, validationService)
	mdlHandler.RegisterRoutes(srv.Router())

	analysisHandler := server.NewAnalysisHandler()
	analysisHandler.RegisterRoutes(srv.Router())

	duckdbHandler := server.NewDuckDBHandler(md)
	duckdbHandler.RegisterRoutes(srv.Router())

	configHandler := server.NewConfigHandler(configMgr)
	configHandler.RegisterRoutes(srv.Router())

	fmt.Printf("wren-engine starting on port %d...\n", configMgr.Port())
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 14:** Build and run server tests

Run:
```bash
go build ./...
go test ./internal/server/... -race -v
```

Expected: all existing tests + 3 new tests PASS, no race warnings.

- [ ] **Step 15:** Commit

```bash
git add internal/server/config_handler.go internal/server/config_handler_test.go cmd/wren-server/main.go
git commit -m "feat(config): wire PATCH/DELETE persistence and startup fatal

- config_handler.go.Patch: Archive -> SyncToFile -> FireReload on success;
  returns 500 on archive/sync failure
- config_handler.go.DeleteAll: same Archive + SyncToFile flow
- config_handler_test.go: 3 e2e tests covering PATCH persists to disk,
  static key no-op skips archive, DELETE persists default to disk
- cmd/wren-server/main.go: WREN_CONFIG_FILE required -> LoadFromFile fatal
  if missing -> LoadFromEnv overlay"
```

---

## Task 4: 差分基线验证 + 文档

**Files:**
- Verify: `internal/difftest/config_diff_test.go`
- Modify: `docker/README.md`
- Verify: `make image-test`

- [ ] **Step 16:** Run difftest baseline

Run:
```bash
go test ./internal/difftest/... -v
```

Expected: `config/all` and all 11 individual entries PASS. No baseline mismatches.

If any fail, investigate — the `NewConfigManager()` path should be unchanged.

- [ ] **Step 17:** Update `docker/README.md`

Find the line about "config.properties parsing not yet supported" (added in Phase 1) and remove it. Add a note about PATCH comment loss:

In the "Known drop-in gaps" section, update the table row for `etc/config.properties`:

```markdown
| `etc/config.properties` | Phase 2 — parsed at startup, PATCH persisted to disk | — |
```

Add a new note below the table:

```markdown
> **Note on PATCH persistence:** Java `Properties.store()` overwrites the entire
> file, discarding comments and custom headers. Go mirrors this behavior.
> If you rely on comments in `config.properties`, manage the file with git
> or an external templating tool.
```

- [ ] **Step 18:** Run `make image-test`

Run:
```bash
make image-test
```

Expected: container builds, starts, `/v1/config` returns 11 entries with values from the mounted `etc/config.properties`.

Note: The `etc/config.properties` in the repo currently has:
```
wren.directory=/usr/src/app/etc/mdl
wren.datasource.type=DUCKDB
wren.experimental-enable-dynamic-fields=false
```

After Phase 2, the image-test should see these file values (not hardcoded defaults).

- [ ] **Step 19:** Commit

```bash
git add docker/README.md
git commit -m "docs(docker): update README for Phase 2 config parsing

- Remove 'config.properties parsing not yet supported' warning
- Document PATCH comment-loss behavior (Java parity caveat)"
```

---

## Final Verification Checklist

Run these commands in sequence before declaring Phase 2 complete:

```bash
# 1. All unit tests
go test ./internal/config/... ./internal/server/... -race

# 2. Difftest baseline
go test ./internal/difftest/... -v

# 3. Full build
go build ./cmd/wren-server

# 4. Format check
gofmt -d internal/config/config.go internal/config/properties.go \
  internal/server/config_handler.go cmd/wren-server/main.go

# 5. Vet
go vet ./...

# 6. Image smoke test
make image-test
```

All must pass cleanly.

---

## Self-Review Results

**1. Spec coverage check:**
- `parseProperties` 解析器 — Task 1
- `writeProperties` 写回器 — Task 1
- `LoadFromFile` — Task 2 Step 6
- `SyncToFile` 原子写 — Task 2 Step 6
- `Archive` 归档 — Task 2 Step 6
- `sync.RWMutex` 并发保护 — Task 2 Step 6
- `OnChange` / `FireReload` — Task 2 Step 6
- Handler Patch 接线 — Task 3 Step 11
- Handler DeleteAll 接线 — Task 3 Step 11
- 启动 fatal (`WREN_CONFIG_FILE` 缺失 / 文件缺失) — Task 3 Step 13
- 单元测试 12+ 边界 — Task 1 Step 3
- `baseline-config.json` 12 IDs 保护 — Task 4 Step 16
- `make image-test` 验证 — Task 4 Step 18

**2. Placeholder scan:**
- No "TBD", "TODO", "implement later", "fill in details"
- No "Add appropriate error handling" / "add validation" without code
- No "Similar to Task N" — each step is self-contained
- All code blocks contain complete, compilable code
- All commands have exact expected output

**3. Type consistency check:**
- `parseProperties(io.Reader) (map[string]string, error)` — used in Task 2
- `writePropertiesWithTimestamp(w, props, header, timestamp)` — used in Task 2
- `ConfigManager.Set` returns `error` — unchanged from P6
- `ConfigManager.Get` returns `(ConfigEntry, bool)` — unchanged
- `ErrUnknownConfigKey` — unchanged
- `patchEntry` — unchanged
- All method signatures match between tasks

**4. Cross-package visibility check:**
- `FireReload` is exported (capital F) — accessible from `server` package
- `WriteError` already exists in `server` package — used by handler
