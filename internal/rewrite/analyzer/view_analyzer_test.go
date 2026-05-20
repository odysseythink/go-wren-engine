package analyzer

import (
	"sort"
	"testing"

	"github.com/wren-engine/wren/internal/parser"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestAnalyze_IdentifiesView(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT * FROM useModel")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	views := a.Views()
	if len(views) != 1 || views[0].Name != "useModel" {
		t.Fatalf("Views() = %v, want [useModel]", views)
	}
}

func TestAnalyze_WrenObjectNames(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	// useUseMetric references useMetric (a view) inside its body, but the body
	// isn't analyzed here — this test only checks top-level reference: the
	// outer query references useMetric directly.
	stmt, _ := parser.ParseSQL("SELECT * FROM useMetric")
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	got := a.WrenObjectNames()
	want := []string{"useMetric"} // useMetric is the only top-level wren object
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("WrenObjectNames() = %v, want %v", got, want)
	}
	// also verify it's sorted: insert a model + view together
	stmt2, _ := parser.ParseSQL("SELECT * FROM Orders, useModel")
	a2 := NewAnalysis(stmt2)
	if _, err := Analyze(a2, stmt2, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	got2 := a2.WrenObjectNames()
	want2 := []string{"Orders", "useModel"}
	sort.Strings(want2) // already sorted
	if len(got2) != len(want2) || got2[0] != want2[0] || got2[1] != want2[1] {
		t.Fatalf("WrenObjectNames() = %v, want %v", got2, want2)
	}
}
