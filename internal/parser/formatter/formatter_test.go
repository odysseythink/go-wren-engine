package formatter

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser/ast"
)

func TestFormatSimpleQuery(t *testing.T) {
	query := &ast.Query{
		Body: &ast.QuerySpecification{
			Select: &ast.Select{
				SelectItems: []ast.SelectItem{
					&ast.SingleColumn{Expression: &ast.LongLiteral{Value: 1}},
				},
			},
		},
	}
	result := FormatSQL(query)
	if result != "SELECT 1" {
		t.Errorf("Expected 'SELECT 1', got '%s'", result)
	}
}

func TestFormatTableQuery(t *testing.T) {
	query := &ast.Query{
		Body: &ast.QuerySpecification{
			Select: &ast.Select{
				SelectItems: []ast.SelectItem{
					&ast.SingleColumn{Expression: &ast.Identifier{Value: "a"}},
					&ast.SingleColumn{Expression: &ast.Identifier{Value: "b"}},
				},
			},
			From: &ast.Table{Name: ast.QualifiedNameOf("orders")},
			Where: &ast.ComparisonExpression{
				Operator: ast.ComparisonGreaterThan,
				Left:     &ast.Identifier{Value: "c"},
				Right:    &ast.LongLiteral{Value: 5},
			},
		},
	}
	result := FormatSQL(query)
	expected := "SELECT a, b FROM orders WHERE c > 5"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}
