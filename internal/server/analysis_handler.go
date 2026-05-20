package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/analyzer/decisionpoint"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
)

// AnalysisHandler implements /v1/analysis/sql, /v2/analysis/sql,
// /v2/analysis/sqls. Mirrors Java AnalysisResource / AnalysisResourceV2.
type AnalysisHandler struct{}

func NewAnalysisHandler() *AnalysisHandler { return &AnalysisHandler{} }

// SqlAnalysisInputDto is the v1 body. The manifest is inline JSON (Java
// SqlAnalysisInputDto.manifest: Manifest).
type SqlAnalysisInputDto struct {
	Manifest *json.RawMessage `json:"manifest"`
	SQL      string           `json:"sql"`
}

// SqlAnalysisInputDtoV2 is the v2 single-SQL body. The manifest is
// base64-encoded JSON (Java SqlAnalysisInputDtoV2.manifestStr: String).
type SqlAnalysisInputDtoV2 struct {
	ManifestStr string `json:"manifestStr"`
	SQL         string `json:"sql"`
}

// SqlAnalysisInputBatchDto is the v2 batch body.
type SqlAnalysisInputBatchDto struct {
	ManifestStr string   `json:"manifestStr"`
	SQLs        []string `json:"sqls"`
}

// RegisterRoutes uses GET to match Java JAX-RS @GET and the existing capture
// tool's http.MethodGet. Chi accepts GET with a request body.
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
	if req.Manifest == nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "Manifest is required"})
		return
	}
	// dto.ManifestFromJSON doesn't exist; mdl.WrenMDLFromJSON parses + builds
	// in one call. Inline-manifest v1 path: raw JSON bytes from the body.
	wrenMDL, err := mdl.WrenMDLFromJSON(string(*req.Manifest))
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	result, err := analyzeSQL(req.SQL, wrenMDL)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *AnalysisHandler) AnalyzeSQLV2(w http.ResponseWriter, r *http.Request) {
	var req SqlAnalysisInputDtoV2
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
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	wrenMDL, err := mdl.WrenMDLFromJSON(string(manifestJSON))
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	result, err := analyzeSQL(req.SQL, wrenMDL)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *AnalysisHandler) AnalyzeSQLs(w http.ResponseWriter, r *http.Request) {
	var req SqlAnalysisInputBatchDto
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
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	wrenMDL, err := mdl.WrenMDLFromJSON(string(manifestJSON))
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	result := make([][]dto.QueryAnalysisDto, len(req.SQLs))
	for i, sql := range req.SQLs {
		analyses, err := analyzeSQL(sql, wrenMDL)
		if err != nil {
			WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
			return
		}
		result[i] = analyses
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// analyzeSQL is the shared core: parse → analyze → DTO. The single DTO
// conversion path lives in package decisionpoint (slice 6 step 1, dto_converter.go).
func analyzeSQL(sql string, wrenMDL *mdl.WrenMDL) ([]dto.QueryAnalysisDto, error) {
	stmt, err := parser.ParseSQL(sql)
	if err != nil {
		return nil, err
	}
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	analyses := decisionpoint.Analyze(stmt, ctx, wrenMDL)
	out := make([]dto.QueryAnalysisDto, len(analyses))
	for i, a := range analyses {
		out[i] = a.ToDto() // method receiver defined in slice 6's dto_converter.go
	}
	return out, nil
}
