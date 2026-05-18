package ast

import "testing"

func TestQueryNode(t *testing.T) {
	q := &Query{
		BaseNode: BaseNode{Location: &NodeLocation{Line: 1}},
	}
	if q.GetLocation().Line != 1 {
		t.Errorf("Query location not preserved")
	}
}

func TestQuerySpecificationNode(t *testing.T) {
	qs := &QuerySpecification{
		Select: &Select{},
	}
	if qs.Select == nil {
		t.Error("QuerySpecification Select should not be nil")
	}
}
