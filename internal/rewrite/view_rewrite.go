package rewrite

import (
	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// GenerateViewRewrite expands View references into CTEs.
type GenerateViewRewrite struct{}

func (r *GenerateViewRewrite) Apply(stmt ast.Statement, ctx *analyzer.SessionContext, analyzedMDL *mdl.AnalyzedMDL) ast.Statement {
	// TODO: Implement view expansion
	return stmt
}
