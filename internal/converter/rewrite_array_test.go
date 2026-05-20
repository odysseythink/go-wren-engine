package converter_test

import (
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/converter"
)

func TestRewriteArray_SubscriptOfArrayConstructor(t *testing.T) {
	conv := &converter.DuckDBSqlConverter{}
	got, err := conv.Convert("SELECT ARRAY[1,2,3][1]", &analyzer.SessionContext{})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	// IMPORTANT: ArrayConstructor formats args with "," (no space — Java
	// Joiner.on(",")); FunctionCall args format with ", " (Java
	// Joiner.on(", ")). So after rewrite we get ", "-joined arg list.
	// Verified against Java testArray: "SELECT array_value(1, 2, 3)[1]\n\n".
	want := "SELECT array_value(1, 2, 3)[1]\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteArray_BareArrayConstructorUntouched(t *testing.T) {
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT ARRAY[1,2,3]", &analyzer.SessionContext{})
	// Bare ArrayConstructor NOT inside subscript → still ARRAY[..]
	// (note: comma without space, ArrayConstructor formatter rule).
	want := "SELECT ARRAY[1,2,3]\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteArray_NestedSubscript(t *testing.T) {
	// (ARRAY[1,2,3][1]) + 1 — only inner ArrayConstructor rewrites.
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT ARRAY[1,2,3][1] + 1", &analyzer.SessionContext{})
	want := "SELECT (array_value(1, 2, 3)[1] + 1)\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
