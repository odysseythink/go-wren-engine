package decisionpoint

import rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"

type DecisionPointContext struct {
	builder         *QueryAnalysisBuilder
	scope           *rewriteAnalyzer.Scope
	isSubqueryOrCte bool
}

func WithSubqueryOrCte(ctx *DecisionPointContext, v bool) *DecisionPointContext {
	if ctx == nil {
		return &DecisionPointContext{isSubqueryOrCte: v}
	}
	return &DecisionPointContext{builder: ctx.builder, scope: ctx.scope, isSubqueryOrCte: v}
}

func IsSubqueryOrCte(ctx *DecisionPointContext) bool {
	if ctx == nil {
		return false
	}
	return ctx.isSubqueryOrCte
}

func (c *DecisionPointContext) Builder() *QueryAnalysisBuilder { return c.builder }
func (c *DecisionPointContext) Scope() *rewriteAnalyzer.Scope  { return c.scope }
