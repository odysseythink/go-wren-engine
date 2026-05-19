package analyzer

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestAnalyze_IdentifiesMetric(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT customer, totalprice FROM Revenue")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := a.Metrics(); len(got) != 1 || got[0].Name != "Revenue" {
		t.Fatalf("Metrics() = %v, want [Revenue]", got)
	}
	if len(a.Models()) != 0 {
		t.Fatalf("Models() = %v, want [] (Revenue is a metric, not a model)", a.Models())
	}
}

func TestAnalyze_IdentifiesCumulativeMetric(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT orderdate, totalprice FROM WeeklyRevenue")
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := a.CumulativeMetrics(); len(got) != 1 || got[0].Name != "WeeklyRevenue" {
		t.Fatalf("CumulativeMetrics() = %v, want [WeeklyRevenue]", got)
	}
}

func TestAnalyze_CapturesRollup(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT * FROM roll_up(Revenue, orderdate, YEAR)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := len(a.MetricRollups()); got != 1 {
		t.Fatalf("MetricRollups() has %d entries, want 1", got)
	}
	for _, info := range a.MetricRollups() {
		if info.Metric.Name != "Revenue" || info.TimeUnit != "YEAR" {
			t.Fatalf("rollup info = %+v, want metric=Revenue unit=YEAR", info)
		}
	}
}
