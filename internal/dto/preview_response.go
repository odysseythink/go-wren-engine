package dto

// PreviewColumn mirrors Java io.wren.base.Column. The type is upper-case
// (Java forces it via toUpperCase(Locale.ROOT) at construction).
type PreviewColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// PreviewResponse is the JSON envelope returned by /v1/mdl/preview,
// /v1/mdl/dry-run, and /v1/data-source/duckdb/query. Mirrors Java
// io.wren.main.web.dto.QueryResultDto.
type PreviewResponse struct {
	Columns []PreviewColumn `json:"columns"`
	Data    [][]any         `json:"data"`
}
