package rewrite

import (
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// WrenSqlRewrite expands Wren models/relationships into CTE queries.
// Mirrors Java io.wren.base.sqlrewrite.WrenSqlRewrite (non-dynamic-field path).
type WrenSqlRewrite struct{}

// Apply is a pass-through until task 20 wires the real engine.
func (r *WrenSqlRewrite) Apply(root ast.Statement, sessionContext *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	return root, nil
}
