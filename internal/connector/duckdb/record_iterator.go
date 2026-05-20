package duckdb

import (
	"database/sql"
	"fmt"

	"github.com/wren-engine/wren/internal/connector"
)

// newRecordIterator: full implementation lands in P4 slice 3 (task 7).
// Slice 0 stub keeps the package compiling for smoke testing.
func newRecordIterator(rows *sql.Rows) (connector.RecordIterator, error) {
	rows.Close()
	return nil, fmt.Errorf("duckdb record iterator: pending slice 3")
}
