package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AnalysisHandler handles analysis endpoints.
type AnalysisHandler struct{}

// NewAnalysisHandler creates a new AnalysisHandler.
func NewAnalysisHandler() *AnalysisHandler {
	return &AnalysisHandler{}
}

// SqlAnalysisInputDto represents SQL analysis input.
type SqlAnalysisInputDto struct {
	Manifest *json.RawMessage `json:"manifest"`
	SQL      string           `json:"sql"`
}

// SqlAnalysisInputDtoV2 represents v2 SQL analysis input.
type SqlAnalysisInputDtoV2 struct {
	ManifestStr string `json:"manifestStr"`
	SQL         string `json:"sql"`
}

// SqlAnalysisInputBatchDto represents batch SQL analysis input.
type SqlAnalysisInputBatchDto struct {
	Manifest *json.RawMessage `json:"manifest"`
	SQLs     []string         `json:"sqls"`
}

// QueryAnalysisDto represents query analysis output.
type QueryAnalysisDto struct {
	SQL string `json:"sql"`
}

// RegisterRoutes registers analysis routes.
func (h *AnalysisHandler) RegisterRoutes(r chi.Router) {
	r.Get("/v1/analysis/sql", h.AnalyzeSQL)
	r.Get("/v2/analysis/sql", h.AnalyzeSQLV2)
	r.Get("/v2/analysis/sqls", h.AnalyzeSQLs)
}

func (h *AnalysisHandler) AnalyzeSQL(w http.ResponseWriter, r *http.Request) {
	var req SqlAnalysisInputDto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	// TODO: Implement analysis
	json.NewEncoder(w).Encode([]QueryAnalysisDto{{SQL: req.SQL}})
}

func (h *AnalysisHandler) AnalyzeSQLV2(w http.ResponseWriter, r *http.Request) {
	var req SqlAnalysisInputDtoV2
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	// TODO: Implement analysis
	json.NewEncoder(w).Encode([]QueryAnalysisDto{{SQL: req.SQL}})
}

func (h *AnalysisHandler) AnalyzeSQLs(w http.ResponseWriter, r *http.Request) {
	var req SqlAnalysisInputBatchDto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	// TODO: Implement batch analysis
	result := make([][]QueryAnalysisDto, len(req.SQLs))
	for i, sql := range req.SQLs {
		result[i] = []QueryAnalysisDto{{SQL: sql}}
	}
	json.NewEncoder(w).Encode(result)
}
