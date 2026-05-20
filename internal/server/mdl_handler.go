package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/service"
)

// MDLHandler handles MDL-related endpoints.
type MDLHandler struct {
	previewService    *service.PreviewService
	validationService *service.ValidationService
}

// NewMDLHandler creates a new MDLHandler.
func NewMDLHandler(preview *service.PreviewService, validation *service.ValidationService) *MDLHandler {
	return &MDLHandler{previewService: preview, validationService: validation}
}

// PreviewDto represents a preview request.
type PreviewDto struct {
	Manifest *dto.Manifest `json:"manifest"`
	SQL      string        `json:"sql"`
	Limit    *int64        `json:"limit,omitempty"`
}

// DryPlanDto represents a dry-plan request.
type DryPlanDto struct {
	Manifest     *dto.Manifest `json:"manifest"`
	SQL          string        `json:"sql"`
	ModelingOnly bool          `json:"modelingOnly"`
}

// DryPlanDtoV2 represents a v2 dry-plan request.
type DryPlanDtoV2 struct {
	ManifestStr string `json:"manifestStr"`
	SQL         string `json:"sql"`
}

// ValidateDto represents a validate request.
type ValidateDto struct {
	Manifest   *dto.Manifest  `json:"manifest"`
	Parameters map[string]any `json:"parameters"`
}

// RegisterRoutes registers MDL routes.
func (h *MDLHandler) RegisterRoutes(r chi.Router) {
	r.Get("/v1/mdl/preview", h.Preview)
	r.Get("/v1/mdl/dry-plan", h.DryPlan)
	r.Get("/v1/mdl/dry-run", h.DryRun)
	r.Post("/v1/mdl/validate/{ruleName}", h.Validate)
	r.Get("/v2/mdl/dry-plan", h.DryPlanV2)
}

func (h *MDLHandler) Preview(w http.ResponseWriter, r *http.Request) {
	var req PreviewDto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	wrenMDL := mdl.WrenMDLFromManifest(req.Manifest)
	result, err := h.previewService.Preview(r.Context(), wrenMDL, req.SQL, 100)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (h *MDLHandler) DryPlan(w http.ResponseWriter, r *http.Request) {
	var req DryPlanDto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	wrenMDL := mdl.WrenMDLFromManifest(req.Manifest)
	result, err := h.previewService.DryPlan(r.Context(), wrenMDL, req.SQL, req.ModelingOnly)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(result))
}

func (h *MDLHandler) DryRun(w http.ResponseWriter, r *http.Request) {
	var req PreviewDto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	wrenMDL := mdl.WrenMDLFromManifest(req.Manifest)
	result, err := h.previewService.DryRun(r.Context(), wrenMDL, req.SQL)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (h *MDLHandler) Validate(w http.ResponseWriter, r *http.Request) {
	ruleName := chi.URLParam(r, "ruleName")
	var req ValidateDto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	wrenMDL := mdl.WrenMDLFromManifest(req.Manifest)
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	result, err := h.validationService.Validate(r.Context(), ruleName, req.Parameters, analyzed)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (h *MDLHandler) DryPlanV2(w http.ResponseWriter, r *http.Request) {
	var req DryPlanDtoV2
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	if req.ManifestStr == "" {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "Manifest is required"})
		return
	}
	manifestJSON, err := base64.StdEncoding.DecodeString(req.ManifestStr)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "base64 decode: " + err.Error()})
		return
	}
	wrenMDL, err := mdl.WrenMDLFromJSON(string(manifestJSON))
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	// V2 dry-plan: Java forces modelingOnly=true regardless of request (see MDLResourceV2.dryPlan).
	result, err := h.previewService.DryPlan(r.Context(), wrenMDL, req.SQL, true)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(result))
}
