package rewrite

import (
	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// EnumRewrite replaces enum column comparisons with IN expressions.
type EnumRewrite struct{}

func (r *EnumRewrite) Apply(stmt ast.Statement, ctx *analyzer.SessionContext, analyzedMDL *mdl.AnalyzedMDL) ast.Statement {
	// TODO: Implement enum rewrite
	return stmt
}
