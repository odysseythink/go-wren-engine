package rewrite

import (
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// WrenRule is one SQL-rewrite rule. Mirrors Java io.wren.base.sqlrewrite.WrenRule.
// Apply receives a freshly parsed statement and returns the rewritten statement.
// A non-nil error mirrors Java throwing IllegalArgumentException.
type WrenRule interface {
	Apply(root ast.Statement, sessionContext *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error)
}
