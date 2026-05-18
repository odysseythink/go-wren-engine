package config

import (
	"os"
	"strconv"
)

// ServerConfig holds server configuration.
type ServerConfig struct {
	Port int `yaml:"port"`
}

// WrenConfig holds Wren-specific configuration.
type WrenConfig struct {
	MDLDirectory        string `yaml:"mdl_directory"`
	DatasourceType      string `yaml:"datasource_type"`
	EnableDynamicFields bool   `yaml:"enable_dynamic_fields"`
}

// DuckDBConfig holds DuckDB configuration.
type DuckDBConfig struct {
	MaxConcurrentTasks int    `yaml:"max_concurrent_tasks"`
	MemoryLimit        string `yaml:"memory_limit"`
	TempDirectory      string `yaml:"temp_directory"`
	HomeDirectory      string `yaml:"home_directory"`
}

// PostgresConfig holds PostgreSQL configuration.
type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// Config is the top-level configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Wren     WrenConfig     `yaml:"wren"`
	DuckDB   DuckDBConfig   `yaml:"duckdb"`
	Postgres PostgresConfig `yaml:"postgres"`
}

// ConfigManager manages configuration.
type ConfigManager struct {
	config Config
}

// NewConfigManager creates a new ConfigManager with defaults.
func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		config: Config{
			Server: ServerConfig{Port: 8080},
			Wren: WrenConfig{
				MDLDirectory:        "etc/mdl",
				DatasourceType:      "duckdb",
				EnableDynamicFields: true,
			},
			DuckDB: DuckDBConfig{
				MaxConcurrentTasks: 4,
				MemoryLimit:        "4GB",
				TempDirectory:      "/tmp/wren",
				HomeDirectory:      ".",
			},
			Postgres: PostgresConfig{
				Host:     "localhost",
				Port:     5432,
				Database: "wren",
				User:     "wren",
			},
		},
	}
}

// Get returns the current config.
func (cm *ConfigManager) Get() Config {
	return cm.config
}

// LoadFromEnv overrides config with environment variables.
func (cm *ConfigManager) LoadFromEnv() {
	if v := os.Getenv("WREN_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cm.config.Server.Port = port
		}
	}
	if v := os.Getenv("WREN_MDL_DIRECTORY"); v != "" {
		cm.config.Wren.MDLDirectory = v
	}
	if v := os.Getenv("WREN_DATASOURCE_TYPE"); v != "" {
		cm.config.Wren.DatasourceType = v
	}
	if v := os.Getenv("WREN_DUCKDB_MEMORY_LIMIT"); v != "" {
		cm.config.DuckDB.MemoryLimit = v
	}
	if v := os.Getenv("WREN_PG_HOST"); v != "" {
		cm.config.Postgres.Host = v
	}
	if v := os.Getenv("WREN_PG_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cm.config.Postgres.Port = port
		}
	}
	if v := os.Getenv("WREN_PG_DATABASE"); v != "" {
		cm.config.Postgres.Database = v
	}
	if v := os.Getenv("WREN_PG_USER"); v != "" {
		cm.config.Postgres.User = v
	}
	if v := os.Getenv("WREN_PG_PASSWORD"); v != "" {
		cm.config.Postgres.Password = v
	}
}
