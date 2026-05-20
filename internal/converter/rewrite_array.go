package converter

import (
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite"
)

// RewriteArray converts an ArrayConstructor that appears as the base of a
// SubscriptExpression into a FunctionCall to array_value(...). DuckDB rejects
// ARRAY[1,2,3][1] but accepts array_value(1,2,3)[1]. Mirrors Java
// io.wren.main.sql.duckdb.RewriteArray.
type RewriteArray struct{}

func (RewriteArray) Apply(root ast.Statement) ast.Statement {
	out := rewrite.RewriteNode(root, func(n ast.Node) (ast.Node, bool) {
		sub, ok := n.(*ast.SubscriptExpression)
		if !ok {
			return nil, false
		}
		ac, ok := sub.Base.(*ast.ArrayConstructor)
		if !ok {
			return nil, false
		}
		// Replace the SubscriptExpression's base with a FunctionCall(array_value, ...)
		// — but the rest of the SubscriptExpression (the Index) must still be
		// recursively rewritten, so we descend the index expression through
		// RewriteNode by re-emitting Base as the replacement and letting the
		// engine descend Index normally.
		newBase := &ast.FunctionCall{
			Name:      ast.QualifiedNameOf("array_value"),
			Arguments: ac.Values,
		}
		repl := &ast.SubscriptExpression{
			BaseNode: sub.BaseNode,
			Base:     newBase,
			Index:    sub.Index,
		}
		return repl, true
	})
	return out.(ast.Statement)
}
