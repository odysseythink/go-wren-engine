package rewrite

import (
	"os"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// loadTPCHForRewrite loads the TPC-H MDL for rewrite-package tests.
// internal/rewrite is two levels below the repo root.
func loadTPCHForRewrite(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/difftest/cases/tpch/mdl.json")
	if err != nil {
		t.Fatalf("read mdl: %v", err)
	}
	m, err := mdl.WrenMDLFromJSON(string(raw))
	if err != nil {
		t.Fatalf("parse mdl: %v", err)
	}
	return m
}

func TestGetRelationships_CalculatedField(t *testing.T) {
	wrenMDL := loadTPCHForRewrite(t)
	orders, _ := wrenMDL.GetModel("Orders")
	expr, err := parser.ParseExpression("customer.nation.name")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	infos, err := analyzer.GetRelationships(expr, wrenMDL, orders)
	if err != nil {
		t.Fatalf("GetRelationships: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("got %d infos, want 1", len(infos))
	}
	rels := infos[0].Relationships()
	if len(rels) != 2 {
		t.Fatalf("got %d relationships, want 2 (OrdersCustomer, CustomerNation)", len(rels))
	}
	// remainingParts should be ["name"]
	if got := infos[0].RemainingParts(); len(got) != 1 || got[0] != "name" {
		t.Fatalf("RemainingParts = %v, want [name]", got)
	}
}
