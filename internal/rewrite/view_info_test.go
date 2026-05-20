package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/formatter"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestViewInfo_GetOnModel(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	view, _ := wrenMDL.GetView("useModel")
	info, err := viewInfoGet(view, mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("viewInfoGet: %v", err)
	}
	if info.Name() != "useModel" {
		t.Fatalf("Name() = %q, want useModel", info.Name())
	}
	// useModel = "select * from Orders" → required = {Orders}
	req := info.RequiredObjects()
	if len(req) != 1 || req[0] != "Orders" {
		t.Fatalf("RequiredObjects() = %v, want [Orders]", req)
	}
	got := formatter.FormatSQL(info.Query())
	if !strings.Contains(got, "Orders") {
		t.Errorf("view query missing Orders:\n%s", got)
	}
}

func TestViewInfo_GetOnMetric(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	view, _ := wrenMDL.GetView("useMetric")
	info, err := viewInfoGet(view, mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("viewInfoGet: %v", err)
	}
	// useMetric = "select * from Revenue" → required = {Revenue}
	req := info.RequiredObjects()
	if len(req) != 1 || req[0] != "Revenue" {
		t.Fatalf("RequiredObjects() = %v, want [Revenue]", req)
	}
}

func TestViewInfo_GetNested(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	view, _ := wrenMDL.GetView("useUseMetric")
	info, err := viewInfoGet(view, mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("viewInfoGet: %v", err)
	}
	// useUseMetric = "select * from useMetric" → required = {useMetric}
	req := info.RequiredObjects()
	if len(req) != 1 || req[0] != "useMetric" {
		t.Fatalf("RequiredObjects() = %v, want [useMetric]", req)
	}
}

func TestViewInfo_GetRollupExpanded(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	view, _ := wrenMDL.GetView("useMetricRollUp")
	info, err := viewInfoGet(view, mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("viewInfoGet: %v", err)
	}
	got := formatter.FormatSQL(info.Query())
	// MetricRollupRewrite should have replaced roll_up(...) with a subquery
	if strings.Contains(got, "roll_up") {
		t.Errorf("roll_up not replaced in view body:\n%s", got)
	}
	if !strings.Contains(got, "DATE_TRUNC") {
		t.Errorf("rollup subquery missing DATE_TRUNC:\n%s", got)
	}
}

func TestQueryDescriptorOf_View(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	d, err := QueryDescriptorOf("useModel", mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("QueryDescriptorOf(useModel): %v", err)
	}
	if d.Name() != "useModel" {
		t.Fatalf("Name() = %q, want useModel", d.Name())
	}
	if _, ok := d.(*ViewInfo); !ok {
		t.Fatalf("expected *ViewInfo, got %T", d)
	}
}
