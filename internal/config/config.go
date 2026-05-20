package config

import (
	"os"
	"sort"
	"strconv"
)

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
	port    int               // bootstrap-only, never in /v1/config
	configs map[string]string // mirrors Java configs map
	static  map[string]bool   // mirrors Java staticConfigs set
}

// NewConfigManager creates a ConfigManager with Java-compatible defaults.
func NewConfigManager() *ConfigManager {
	cm := &ConfigManager{
		port:    8080,
		configs: map[string]string{},
		static:  map[string]bool{},
	}
	// initConfig(key, value, requiredReload, isStatic) — mirror Java's table.
	cm.initConfig("wren.directory", "etc/mdl", false, true)
	cm.initConfig("wren.datasource.type", "DUCKDB", true, false)
	cm.initConfig("wren.experimental-enable-dynamic-fields", "true", false, false)
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

func (cm *ConfigManager) initConfig(key, value string, _requiredReload, isStatic bool) {
	cm.configs[key] = value
	if isStatic {
		cm.static[key] = true
	}
}

// Port returns the bootstrap HTTP port. Not surfaced via /v1/config.
func (cm *ConfigManager) Port() int { return cm.port }

// Get returns the entry for a name; ok=false if unknown.
func (cm *ConfigManager) Get(name string) (ConfigEntry, bool) {
	v, ok := cm.configs[name]
	if !ok {
		return ConfigEntry{}, false
	}
	return ConfigEntry{Name: name, Value: stringValue(v)}, true
}

// All returns all entries sorted by name (risk #3).
func (cm *ConfigManager) All() []ConfigEntry {
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
	if cm.static[name] {
		return nil // silent skip
	}
	if _, ok := cm.configs[name]; !ok {
		return ErrUnknownConfigKey{Key: name}
	}
	cm.configs[name] = value
	return nil
}

// Reset wipes config back to defaults (Java DELETE /v1/config behaviour).
func (cm *ConfigManager) Reset() {
	port := cm.port
	*cm = *NewConfigManager()
	cm.port = port
}

// ErrUnknownConfigKey is returned by Set when the key isn't a recognised Java key.
type ErrUnknownConfigKey struct{ Key string }

func (e ErrUnknownConfigKey) Error() string { return "Config not found: " + e.Key }

// LoadFromEnv overrides defaults with environment variables.
func (cm *ConfigManager) LoadFromEnv() {
	envMap := map[string]string{
		"WREN_DIRECTORY":                         "wren.directory",
		"WREN_DATASOURCE_TYPE":                   "wren.datasource.type",
		"WREN_EXPERIMENTAL_ENABLE_DYNAMIC_FIELDS": "wren.experimental-enable-dynamic-fields",
		"WREN_DUCKDB_MEMORY_LIMIT":               "duckdb.memory-limit",
		"WREN_DUCKDB_HOME_DIRECTORY":             "duckdb.home-directory",
		"WREN_DUCKDB_TEMP_DIRECTORY":             "duckdb.temp-directory",
		"WREN_DUCKDB_MAX_CONCURRENT_TASKS":       "duckdb.max-concurrent-tasks",
		"WREN_DUCKDB_MAX_CACHE_QUERY_TIMEOUT":    "duckdb.max-cache-query-timeout",
		"WREN_DUCKDB_CACHE_TASK_RETRY_DELAY":     "duckdb.cache-task-retry-delay",
		"WREN_DUCKDB_CONNECTOR_INIT_SQL_PATH":    "duckdb.connector.init-sql-path",
		"WREN_DUCKDB_CONNECTOR_SESSION_SQL_PATH": "duckdb.connector.session-sql-path",
	}
	for env, key := range envMap {
		if v := os.Getenv(env); v != "" {
			cm.configs[key] = v
		}
	}
	if v := os.Getenv("WREN_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cm.port = p
		}
	}
}

// EnableDynamicFields is a typed convenience used by PreviewService.
func (cm *ConfigManager) EnableDynamicFields() bool {
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
