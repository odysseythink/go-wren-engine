package ast_test

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

func TestArrayConstructor_ParseAndFormatDefault(t *testing.T) {
	stmt, err := parser.ParseSQL("SELECT ARRAY[1, 2, 3]")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := formatter.FormatSQL(stmt)
	want := "SELECT ARRAY[1,2,3]\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}

	// Tree shape spot-check: outer Query → QuerySpecification → Select →
	// SingleColumn → ArrayConstructor{Values: [LongLiteral 1, 2, 3]}.
	q, ok := stmt.(*ast.Query)
	if !ok {
		t.Fatalf("not a Query: %T", stmt)
	}
	qs := q.Body.(*ast.QuerySpecification)
	sc := qs.Select.SelectItems[0].(*ast.SingleColumn)
	ac, ok := sc.Expression.(*ast.ArrayConstructor)
	if !ok {
		t.Fatalf("not ArrayConstructor: %T", sc.Expression)
	}
	if len(ac.Values) != 3 {
		t.Errorf("expected 3 values, got %d", len(ac.Values))
	}
}

func TestArrayConstructor_Subscript(t *testing.T) {
	stmt, err := parser.ParseSQL("SELECT ARRAY[1, 2, 3][1]")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	sc := qs.Select.SelectItems[0].(*ast.SingleColumn)
	sub, ok := sc.Expression.(*ast.SubscriptExpression)
	if !ok {
		t.Fatalf("not SubscriptExpression: %T", sc.Expression)
	}
	if _, ok := sub.Base.(*ast.ArrayConstructor); !ok {
		t.Fatalf("subscript base not ArrayConstructor: %T", sub.Base)
	}
}
