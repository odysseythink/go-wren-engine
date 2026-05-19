package rewrite

import "testing"

func TestTopoSort_Linear(t *testing.T) {
	order, err := topoSort([]string{"A", "B"}, [][2]string{{"B", "A"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "B" || order[1] != "A" {
		t.Fatalf("order = %v, want [B A]", order)
	}
}

func TestTopoSort_Cycle(t *testing.T) {
	_, err := topoSort([]string{"A", "B"}, [][2]string{{"A", "B"}, {"B", "A"}})
	if err == nil {
		t.Fatal("want cycle error, got nil")
	}
}

func TestTopoSort_TieBreakLexical(t *testing.T) {
	order, _ := topoSort([]string{"C", "A", "B"}, nil)
	if order[0] != "A" || order[1] != "B" || order[2] != "C" {
		t.Fatalf("order = %v, want [A B C]", order)
	}
}
