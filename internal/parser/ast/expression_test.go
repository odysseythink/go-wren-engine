package ast

import "testing"

func TestQualifiedName(t *testing.T) {
	qn := QualifiedNameOf("catalog", "schema", "table")
	if qn.String() != "catalog.schema.table" {
		t.Errorf("QualifiedName string wrong: %s", qn.String())
	}
	if len(qn.Parts) != 3 {
		t.Errorf("QualifiedName should have 3 parts, got %d", len(qn.Parts))
	}
}

func TestComparisonExpression(t *testing.T) {
	comp := &ComparisonExpression{
		Operator: ComparisonEqual,
		Left:     &Identifier{Value: "a"},
		Right:    &LongLiteral{Value: 1},
	}
	children := comp.GetChildren()
	if len(children) != 2 {
		t.Errorf("ComparisonExpression should have 2 children, got %d", len(children))
	}
}

func TestFunctionCall(t *testing.T) {
	fn := &FunctionCall{
		Name:      QualifiedNameOf("count"),
		Arguments: []Expression{&StarExpression{}},
	}
	children := fn.GetChildren()
	if len(children) != 1 {
		t.Errorf("FunctionCall should have 1 child, got %d", len(children))
	}
}
