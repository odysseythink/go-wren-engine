package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestGenerateViewRewrite_ExpandsViewOnModel(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT custkey FROM useModel")
	out, err := (&GenerateViewRewrite{}).Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	if !strings.Contains(got, `"useModel"`) {
		t.Errorf("missing useModel CTE:\n%s", got)
	}
	if !strings.Contains(got, "Orders") {
		t.Errorf("view CTE body should reference Orders (model expansion is rule 3's job):\n%s", got)
	}
}

func TestGenerateViewRewrite_NestedViewTopologicalOrder(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT custkey FROM useUseMetric")
	out, err := (&GenerateViewRewrite{}).Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	idxUseMetric := strings.Index(got, `"useMetric"`)
	idxUseUseMetric := strings.Index(got, `"useUseMetric"`)
	if idxUseMetric < 0 || idxUseUseMetric < 0 {
		t.Fatalf("both useMetric and useUseMetric CTEs expected:\n%s", got)
	}
	if idxUseMetric >= idxUseUseMetric {
		t.Errorf("useMetric CTE must precede useUseMetric (risk #1):\n%s", got)
	}
}

func TestGenerateViewRewrite_PassThroughNoView(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT custkey FROM Orders")
	out, err := (&GenerateViewRewrite{}).Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// No view referenced: should be byte-identical to input (modulo formatter).
	if got, want := formatter.FormatSQL(out), formatter.FormatSQL(stmt); got != want {
		t.Errorf("non-view query changed:\n got %q\nwant %q", got, want)
	}
}
