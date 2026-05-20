package converter_test

import (
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/converter"
)

func TestRewriteFunction_PgToDuckDBMapping(t *testing.T) {
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT generate_array(1, 10)", &analyzer.SessionContext{})
	want := "SELECT generate_series(1, 10)\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteFunction_LowercaseEvenIfNoMapping(t *testing.T) {
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT COUNT(*) FROM t", &analyzer.SessionContext{})
	// COUNT lowercased to count even though no PG→DuckDB mapping fires.
	want := "SELECT count(*)\nFROM\n  t\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteFunction_QualifiedNameUsesSuffix(t *testing.T) {
	// Mapping looks up suffix (last segment), not full qualified name.
	// generate_array → generate_series. some.schema.generate_array → generate_series (suffix-only match — Java behavior).
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT some.schema.generate_array(1)", &analyzer.SessionContext{})
	want := "SELECT generate_series(1)\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteFunction_NestedFunctionCalls(t *testing.T) {
	// Outer + inner both lowercased.
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT SUM(COUNT(*))", &analyzer.SessionContext{})
	want := "SELECT sum(count(*))\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
