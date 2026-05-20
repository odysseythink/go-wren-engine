package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// MetricRollupRewrite replaces roll_up(...) FunctionRelations with a metric
// sub-query. Mirrors Java io.wren.base.sqlrewrite.MetricRollupRewrite.
type MetricRollupRewrite struct{}

// Apply rewrites every roll_up FunctionRelation captured by the analyzer.
// Mirrors MetricRollupRewrite.apply + the inner Rewriter.visitFunctionRelation.
func (r *MetricRollupRewrite) Apply(root ast.Statement, ctx *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	analysis := analyzer.NewAnalysis(root)
	if _, err := analyzer.Analyze(analysis, root, ctx, analyzedMDL.WrenMDL()); err != nil {
		return nil, err
	}
	var rewriteErr error
	out := RewriteNode(root, func(n ast.Node) (ast.Node, bool) {
		fr, ok := n.(*ast.FunctionRelation)
		if !ok {
			return nil, false // descend
		}
		info, found := analysis.GetMetricRollup(fr)
		if !found {
			// every roll_up node is captured + syntax-checked in StatementAnalyzer;
			// reaching here means an unsupported FunctionRelation. Mirrors Java throw.
			rewriteErr = fmt.Errorf("MetricRollup node is not replaced")
			return fr, true
		}
		query, err := parseMetricRollupSql(info)
		if err != nil {
			rewriteErr = err
			return fr, true
		}
		return &ast.AliasedRelation{
			Relation: &ast.TableSubquery{Query: query},
			Alias:    &ast.Identifier{Value: info.Metric.Name}, // non-delimited (risk #6)
		}, true
	})
	if rewriteErr != nil {
		return nil, rewriteErr
	}
	return out.(ast.Statement), nil
}
