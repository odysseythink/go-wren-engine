package rewrite

import (
	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// WrenSqlRewrite is the main rewrite rule for model/metric expansion.
type WrenSqlRewrite struct{}

func (r *WrenSqlRewrite) Apply(stmt ast.Statement, ctx *analyzer.SessionContext, analyzedMDL *mdl.AnalyzedMDL) ast.Statement {
	// TODO: Implement main SQL rewrite
	return stmt
}
