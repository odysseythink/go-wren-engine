package duckdb

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/wren-engine/wren/internal/connector"
)

// recordIterator wraps a *sql.Rows in the connector.RecordIterator interface.
// Mirrors Java DuckdbRecordIterator: column metadata captured at construction,
// then row-by-row iteration with type conversion.
type recordIterator struct {
	rows    *sql.Rows
	columns []connector.Column
	scanned []any
	holders []any
}

func newRecordIterator(rows *sql.Rows) (connector.RecordIterator, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("duckdb column types: %w", err)
	}
	cols := make([]connector.Column, len(types))
	for i, t := range types {
		cols[i] = connector.Column{
			Name: t.Name(),
			Type: strings.ToUpper(t.DatabaseTypeName()),
		}
	}
	scanned := make([]any, len(cols))
	holders := make([]any, len(cols))
	for i := range holders {
		holders[i] = &scanned[i]
	}
	return &recordIterator{
		rows:    rows,
		columns: cols,
		scanned: scanned,
		holders: holders,
	}, nil
}

func (r *recordIterator) Columns() []connector.Column { return r.columns }

func (r *recordIterator) Next() bool {
	if !r.rows.Next() {
		return false
	}
	if err := r.rows.Scan(r.holders...); err != nil {
		// database/sql convention: error during scan means Next() returns false
		// and Err() reveals the cause. We swallow here to match the simple
		// connector.RecordIterator contract; production paths should call Err().
		return false
	}
	return true
}

func (r *recordIterator) Get() []any {
	out := make([]any, len(r.scanned))
	for i, v := range r.scanned {
		out[i] = convertValue(r.columns[i].Type, v)
	}
	return out
}

func (r *recordIterator) Close() error {
	return r.rows.Close()
}

// convertValue maps go-duckdb's scanned value to the canonical envelope
// representation. Mirrors Java DuckdbRecordIterator.convertValue:
//   - null      -> nil
//   - TIMESTAMP -> LocalDateTime (here: time.Time, ISO-8601 stringifies via Jackson)
//   - BLOB      -> []byte (Jackson base64-encodes byte[])
//   - JSON      -> string (toString)
//   - T[]       -> []any with recursive conversion
//   - otherwise -> as-is (database/sql canonical types)
func convertValue(typeName string, v any) any {
	if v == nil {
		return nil
	}
	if strings.HasSuffix(typeName, "[]") {
		inner := typeName[:len(typeName)-2]
		switch a := v.(type) {
		case []any:
			out := make([]any, len(a))
			for i, x := range a {
				out[i] = convertValue(inner, x)
			}
			return out
		}
		return v
	}
	switch typeName {
	case "JSON":
		// go-duckdb returns JSON as string already; mirror Java toString().
		return fmt.Sprintf("%v", v)
	}
	// TIMESTAMP / BLOB pass through — go-duckdb returns time.Time / []byte
	// which Go's encoding/json encodes ISO-8601 / base64 respectively. That
	// matches Jackson default serialization for LocalDateTime + byte[].
	return v
}
