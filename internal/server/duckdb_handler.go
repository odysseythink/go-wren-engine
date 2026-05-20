package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/dto"
)

// DuckDBHandler handles DuckDB data source endpoints.
type DuckDBHandler struct {
	metadata *duckdb.Metadata
}

// NewDuckDBHandler creates a new DuckDBHandler.
func NewDuckDBHandler(m *duckdb.Metadata) *DuckDBHandler {
	return &DuckDBHandler{metadata: m}
}

// RegisterRoutes registers DuckDB routes.
func (h *DuckDBHandler) RegisterRoutes(r chi.Router) {
	r.Post("/v1/data-source/duckdb/query", h.Query)
	r.Get("/v1/data-source/duckdb/settings/init-sql", h.GetInitSQL)
	r.Put("/v1/data-source/duckdb/settings/init-sql", h.SetInitSQL)
	r.Patch("/v1/data-source/duckdb/settings/init-sql", h.PatchInitSQL)
	r.Get("/v1/data-source/duckdb/settings/session-sql", h.GetSessionSQL)
	r.Put("/v1/data-source/duckdb/settings/session-sql", h.SetSessionSQL)
	r.Patch("/v1/data-source/duckdb/settings/session-sql", h.PatchSessionSQL)
}

func (h *DuckDBHandler) Query(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	sql := string(body)
	result, err := h.metadata.DirectQuery(r.Context(), sql, nil)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	defer result.Close()

	cols := result.Columns()
	pcols := make([]dto.PreviewColumn, len(cols))
	for i, c := range cols {
		pcols[i] = dto.PreviewColumn{Name: c.Name, Type: c.Type}
	}
	var data [][]any
	for result.Next() {
		data = append(data, result.Get())
	}
	json.NewEncoder(w).Encode(dto.PreviewResponse{Columns: pcols, Data: data})
}

func (h *DuckDBHandler) GetInitSQL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(h.metadata.InitSQL()))
}

func (h *DuckDBHandler) SetInitSQL(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := h.metadata.SetInitSQL(r.Context(), string(body)); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *DuckDBHandler) PatchInitSQL(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := h.metadata.AppendInitSQL(r.Context(), string(body)); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *DuckDBHandler) GetSessionSQL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(h.metadata.SessionSQL()))
}

func (h *DuckDBHandler) SetSessionSQL(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := h.metadata.SetSessionSQL(r.Context(), string(body)); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *DuckDBHandler) PatchSessionSQL(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := h.metadata.AppendSessionSQL(r.Context(), string(body)); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}
