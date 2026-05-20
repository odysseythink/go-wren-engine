package decisionpoint

import (
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// AnalyzeFilter parses the top-level logical structure of a WHERE expression.
// Mirrors Java FilterAnalyzer.analyze: only top-level AND/OR pairs become
// LogicalAnalysis; nested or non-binary logical exprs collapse to a leaf
// ExpressionAnalysis formatted via FormatExpression.
func AnalyzeFilter(expr ast.Expression, scope *rewriteAnalyzer.Scope) *FilterAnalysis {
	return analyzeFilterNode(expr, nil, scope)
}

func analyzeFilterNode(node ast.Node, parent ast.Node, scope *rewriteAnalyzer.Scope) *FilterAnalysis {
	if node == nil {
		return nil
	}
	if le, ok := node.(*ast.LogicalExpression); ok {
		// Only the top-level OR a Logical-nested Logical can split. The
		// parent==nil branch matches Java's first invocation (no parent).
		// The parent-is-Logical branch matches recursive descent through
		// AND/OR. Anything else (e.g., Logical under a Comparison) is a leaf.
		if parent == nil || isLogical(parent) {
			if len(le.Terms) == 2 {
				typ := FilterTypeAnd
				if le.Operator == ast.LogicalOr {
					typ = FilterTypeOr
				}
				return NewLogicalAnalysis(
					typ,
					analyzeFilterNode(le.Terms[0], node, scope),
					analyzeFilterNode(le.Terms[1], node, scope),
					le.GetLocation(),
				)
			}
			// N-ary (>2) flatten: Trino parser collapses `a AND b AND c`
			// into one LogicalExpression with 3 Terms; Java FilterAnalyzer
			// only handles binary, so >2 falls back to a single leaf. The
			// formatted output preserves the original SQL form.
		}
		return leafFilter(le, scope)
	}
	return leafFilter(node, scope)
}

func isLogical(node ast.Node) bool {
	_, ok := node.(*ast.LogicalExpression)
	return ok
}

func leafFilter(node ast.Node, scope *rewriteAnalyzer.Scope) *FilterAnalysis {
	expr := node.(ast.Expression)
	exprStr := formatter.FormatExpression(expr)
	exprSources := expressionSourceAnalyze(expr, scope)
	return NewExpressionAnalysis(exprStr, node.GetLocation(), exprSources)
}
