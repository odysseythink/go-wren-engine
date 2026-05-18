package rewrite

import (
	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// WrenRule is the interface for SQL rewrite rules.
type WrenRule interface {
	Apply(stmt ast.Statement, ctx *analyzer.SessionContext, analyzedMDL *mdl.AnalyzedMDL) ast.Statement
}
