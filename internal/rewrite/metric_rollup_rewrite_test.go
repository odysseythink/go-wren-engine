package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestMetricRollupRewrite_ReplacesRollup(t *testing.T) {
	wrenMDL := metricMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT * FROM roll_up(Revenue, orderdate, YEAR)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rule := &MetricRollupRewrite{}
	out, err := rule.Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	for _, want := range []string{`DATE_TRUNC('YEAR'`, `"Orders"`, "GROUP BY 1, 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten rollup missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "roll_up") {
		t.Errorf("roll_up not replaced:\n%s", got)
	}
}

func TestMetricRollupRewrite_PassThroughNoRollup(t *testing.T) {
	wrenMDL := metricMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT custkey FROM Revenue")
	rule := &MetricRollupRewrite{}
	out, err := rule.Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := formatter.FormatSQL(out); got != formatter.FormatSQL(stmt) {
		t.Errorf("non-rollup query changed: got %q", got)
	}
}
