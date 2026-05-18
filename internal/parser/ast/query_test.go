package ast

import "testing"

func TestWithQuery(t *testing.T) {
	wq := &WithQuery{
		Name:  &Identifier{Value: "cte"},
		Query: &Query{},
	}
	children := wq.GetChildren()
	if len(children) != 2 {
		t.Errorf("WithQuery should have 2 children, got %d", len(children))
	}
}

func TestSelect(t *testing.T) {
	sel := &Select{
		Distinct: true,
		SelectItems: []SelectItem{
			&SingleColumn{Expression: &Identifier{Value: "a"}},
			&AllColumns{},
		},
	}
	children := sel.GetChildren()
	if len(children) != 2 {
		t.Errorf("Select should have 2 children, got %d", len(children))
	}
}
