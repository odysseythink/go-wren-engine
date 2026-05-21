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
	fileExtras     map[string]string // all keys from file (11-key + non-11-key) for transparent write-back
	filePath       string            // path to config file
	fileFormat     string            // "properties" or "yaml"
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
		reloadHooks:    map[string][]func(){},
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
	preserved := make(map[string]string)
	for k, v := range cm.fileExtras {
		if !knownKeys[k] {
			preserved[k] = v
		}
	}
	filePath := cm.filePath
	newCm := NewConfigManager()
	cm.port = newCm.port
	cm.configs = newCm.configs
	cm.static = newCm.static
	cm.fileExtras = preserved
	cm.filePath = filePath
	cm.requiredReload = newCm.requiredReload
	cm.port = port
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

	var props map[string]string
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".yaml" || ext == ".yml" {
		props, err = parseYAML(f)
		cm.fileFormat = "yaml"
	} else {
		props, err = parseProperties(f)
		cm.fileFormat = "properties"
	}
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
	format := cm.fileFormat
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

	var writeErr error
	if format == "yaml" {
		writeErr = writeYAML(f, props)
	} else {
		ts := time.Now().UTC().Format(time.RFC1123)
		writeErr = writePropertiesWithTimestamp(f, props, "sync with file", ts)
	}
	if writeErr != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write config: %w", writeErr)
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
