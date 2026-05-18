package ast

import "testing"

func TestTableNode(t *testing.T) {
	tbl := &Table{Name: QualifiedNameOf("catalog", "schema", "orders")}
	if tbl.Name.String() != "catalog.schema.orders" {
		t.Errorf("Table name not correct: %s", tbl.Name.String())
	}
}

func TestJoinNode(t *testing.T) {
	join := &Join{
		JoinType: JoinTypeLeft,
		Left:     &Table{Name: QualifiedNameOf("orders")},
		Right:    &Table{Name: QualifiedNameOf("customers")},
		Criteria: &JoinOn{Expression: &ComparisonExpression{}},
	}
	children := join.GetChildren()
	if len(children) != 3 {
		t.Errorf("Join should have 3 children, got %d", len(children))
	}
}
