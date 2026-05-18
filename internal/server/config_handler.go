package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/config"
)

// ConfigHandler handles configuration endpoints.
type ConfigHandler struct {
	configMgr *config.ConfigManager
}

// ConfigEntry represents a single config entry.
type ConfigEntry struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// NewConfigHandler creates a new ConfigHandler.
func NewConfigHandler(configMgr *config.ConfigManager) *ConfigHandler {
	return &ConfigHandler{configMgr: configMgr}
}

// RegisterRoutes registers config routes.
func (h *ConfigHandler) RegisterRoutes(r chi.Router) {
	r.Get("/v1/config", h.GetAll)
	r.Get("/v1/config/{configName}", h.Get)
	r.Delete("/v1/config", h.DeleteAll)
	r.Patch("/v1/config", h.Patch)
}

func (h *ConfigHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	cfg := h.configMgr.Get()
	entries := []ConfigEntry{
		{Name: "server.port", Value: cfg.Server.Port},
		{Name: "wren.mdl_directory", Value: cfg.Wren.MDLDirectory},
		{Name: "wren.datasource_type", Value: cfg.Wren.DatasourceType},
		{Name: "wren.enable_dynamic_fields", Value: cfg.Wren.EnableDynamicFields},
	}
	json.NewEncoder(w).Encode(entries)
}

func (h *ConfigHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "configName")
	cfg := h.configMgr.Get()
	var value any
	switch name {
	case "server.port":
		value = cfg.Server.Port
	case "wren.mdl_directory":
		value = cfg.Wren.MDLDirectory
	case "wren.datasource_type":
		value = cfg.Wren.DatasourceType
	default:
		WriteError(w, &WrenError{Code: 65536, Type: NotFound, Message: "config not found: " + name})
		return
	}
	json.NewEncoder(w).Encode(ConfigEntry{Name: name, Value: value})
}

func (h *ConfigHandler) DeleteAll(w http.ResponseWriter, r *http.Request) {
	// Reset to defaults
	*h.configMgr = *config.NewConfigManager()
	w.WriteHeader(http.StatusOK)
}

func (h *ConfigHandler) Patch(w http.ResponseWriter, r *http.Request) {
	var entries []ConfigEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	// TODO: Apply config updates
	w.WriteHeader(http.StatusOK)
}
