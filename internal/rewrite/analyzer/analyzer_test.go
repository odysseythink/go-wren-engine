package analyzer

import (
	"os"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func loadTPCH(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	raw, err := os.ReadFile("../../../testdata/difftest/cases/tpch/mdl.json")
	if err != nil {
		t.Fatalf("read mdl: %v", err)
	}
	m, err := mdl.WrenMDLFromJSON(string(raw))
	if err != nil {
		t.Fatalf("parse mdl: %v", err)
	}
	return m
}

func TestAnalyze_IdentifiesModels(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT partkey, name FROM Part")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := a.Models(); len(got) != 1 || got[0].Name != "Part" {
		t.Fatalf("Models() = %v, want [Part]", got)
	}
}

func TestAnalyze_RawTableNotModel(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT l_orderkey FROM lineitem")
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(a.Models()) != 0 {
		t.Fatalf("Models() = %v, want []", a.Models())
	}
}

func TestAnalyze_WithCTENotModel(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL(`WITH Part AS (SELECT 1 x) SELECT x FROM Part`)
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(a.Models()) != 0 {
		t.Fatalf("CTE shadowing model: Models() = %v, want []", a.Models())
	}
}
