package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// ViewInfo is a QueryDescriptor backed by an MDL view's parsed (and
// rollup-rewritten) statement. Mirrors Java io.wren.base.sqlrewrite.ViewInfo.
type ViewInfo struct {
	name            string
	requiredObjects []string
	query           *ast.Query
}

func newViewInfo(name string, requiredObjects []string, query *ast.Query) *ViewInfo {
	return &ViewInfo{name: name, requiredObjects: requiredObjects, query: query}
}

func (v *ViewInfo) Name() string              { return v.name }
func (v *ViewInfo) RequiredObjects() []string { return v.requiredObjects }
func (v *ViewInfo) Query() *ast.Query         { return v.query }

// viewInfoGet builds a ViewInfo for an MDL view. Mirrors ViewInfo.get.
// Steps mirror Java line by line:
//  1. parseView(view.getStatement())
//  2. StatementAnalyzer.analyze on the parsed body
//  3. apply MetricRollupRewrite to the body (SQL in a view can use roll_up syntax)
//  4. requiredObjects = analysis.getWrenObjectNames() (already name-sorted)
//
// Risk #6: Go's MetricRollupRewrite.Apply (P3b 3-arg) builds a fresh internal
// Analysis from the same query tree. Node identity is preserved, so the rollup
// FunctionRelation captured during step 2 is also captured (identically) during
// the 3-arg call's internal Analyze — byte-equivalent to Java reusing analysis.
func viewInfoGet(view *dto.View, analyzedMDL *mdl.AnalyzedMDL, ctx *base.SessionContext) (*ViewInfo, error) {
	query, err := parseView(view.Statement)
	if err != nil {
		return nil, err
	}
	analysis := analyzer.NewAnalysis(query)
	if _, err := analyzer.Analyze(analysis, query, ctx, analyzedMDL.WrenMDL()); err != nil {
		return nil, err
	}
	rewritten, err := (&MetricRollupRewrite{}).Apply(query, ctx, analyzedMDL)
	if err != nil {
		return nil, err
	}
	rq, ok := rewritten.(*ast.Query)
	if !ok {
		return nil, fmt.Errorf("view %q body is not a query after rollup rewrite", view.Name)
	}
	return newViewInfo(view.Name, analysis.WrenObjectNames(), rq), nil
}
