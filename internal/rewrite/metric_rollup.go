package rewrite

import (
	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// MetricRollupRewrite handles cumulative metric time-grain rollups.
type MetricRollupRewrite struct{}

func (r *MetricRollupRewrite) Apply(stmt ast.Statement, ctx *analyzer.SessionContext, analyzedMDL *mdl.AnalyzedMDL) ast.Statement {
	// TODO: Implement metric rollup
	return stmt
}
