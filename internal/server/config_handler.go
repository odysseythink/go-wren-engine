package server

import (
	"encoding/json"
	"errors"
	"net/http"

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
	w.WriteHeader(http.StatusOK)
}

func (h *ConfigHandler) Patch(w http.ResponseWriter, r *http.Request) {
	var entries []patchEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	for _, e := range entries {
		value := ""
		if e.Value != nil {
			value = *e.Value
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
	}
	w.WriteHeader(http.StatusOK)
}
