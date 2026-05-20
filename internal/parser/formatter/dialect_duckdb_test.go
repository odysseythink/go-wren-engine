package formatter_test

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

// roundtrip parses a SQL then re-renders it under DialectDuckDB. Both Java
// and Go normalize via parse→format, so byte equivalence requires both
// formatter outputs to match.
func roundtrip(t *testing.T, in string) string {
	t.Helper()
	stmt, err := parser.ParseSQL(in)
	if err != nil {
		t.Fatalf("parse: %v\nSQL: %s", err, in)
	}
	return formatter.FormatSQLDialect(stmt, formatter.DialectDuckDB)
}

func TestDuckDB_DoubleLiteral(t *testing.T) {
	cases := []struct{ in, want string }{
		// Java Double.toString boundaries: [1e-3, 1e7) is decimal; outside is scientific.
		// Verified by running Java in ../wren-engine-0.9.3 REPL with String.valueOf.
		{"SELECT 1.5", "SELECT 1.5\n\n"},
		{"SELECT 0.001", "SELECT 0.001\n\n"},          // 1e-3 boundary inclusive → still decimal
		{"SELECT 0.0001", "SELECT 1.0E-4\n\n"},        // < 1e-3 → scientific
		{"SELECT 9999999.9", "SELECT 9999999.9\n\n"},   // < 1e7 → decimal
		{"SELECT 1.0E10", "SELECT 1.0E10\n\n"},        // ≥ 1e7 → scientific
		{"SELECT -2.5E-5", "SELECT -2.5E-5\n\n"},       // negative + scientific
	}
	for _, c := range cases {
		if got := roundtrip(t, c.in); got != c.want {
			t.Errorf("in=%q\n got=%q\nwant=%q", c.in, got, c.want)
		}
	}
}

func TestDuckDB_IntervalLiteral(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SELECT INTERVAL '7' DAY", "SELECT INTERVAL '7' DAY\n\n"},
		{"SELECT INTERVAL -'7' DAY", "SELECT INTERVAL '-7' DAY\n\n"},
		{"SELECT INTERVAL '1' YEAR TO MONTH", "SELECT INTERVAL '1' YEAR\n\n"}, // DuckDB drops TO subfield
	}
	for _, c := range cases {
		if got := roundtrip(t, c.in); got != c.want {
			t.Errorf("in=%q\n got=%q\nwant=%q", c.in, got, c.want)
		}
	}
}

func TestDuckDB_ArrayType(t *testing.T) {
	if got := roundtrip(t, "SELECT CAST(x AS ARRAY(INTEGER)) FROM t"); got != "SELECT CAST(x AS INTEGER[])\nFROM\n  t\n" {
		t.Errorf("ARRAY type → DUCKDB suffix: got %q", got)
	}
}

func TestDuckDB_ArrayConstructor_Subscript(t *testing.T) {
	// Pre-RewriteArray: ARRAY[1,2,3][1] formatter still emits ARRAY[..].
	// RewriteArray (任务 5) is what converts to array_value(...). The
	// formatter alone must not.
	if got := roundtrip(t, "SELECT ARRAY[1,2,3][1]"); got != "SELECT ARRAY[1,2,3][1]\n\n" {
		t.Errorf("formatter alone must not rewrite ArrayConstructor: got %q", got)
	}
}

func TestDuckDB_GenerateTimestampArray_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for GENERATE_TIMESTAMP_ARRAY under DUCKDB")
		}
	}()
	_ = roundtrip(t, "SELECT GENERATE_TIMESTAMP_ARRAY(t1, t2, INTERVAL '1' HOUR)")
}
