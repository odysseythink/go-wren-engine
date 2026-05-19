package formatter

import (
	"fmt"

	"github.com/wren-engine/wren/internal/parser/ast"
)

// formatExpression renders an expression to SQL text. It mirrors trino's
// ExpressionFormatter — a flat (non-indented) visitor returning a string.
func formatExpression(expr ast.Expression, dialect Dialect) string {
	return (&exprFormatter{dialect: dialect}).process(expr)
}

// exprFormatter mirrors trino ExpressionFormatter.Formatter
// (an AstVisitor<String, Void>).
type exprFormatter struct {
	dialect Dialect
}

// process renders one expression node. An unhandled node is a hard error,
// mirroring Java ExpressionFormatter.visitExpression throwing.
func (e *exprFormatter) process(expr ast.Expression) string {
	switch n := expr.(type) {
	default:
		panic(fmt.Sprintf("ExpressionFormatter: not yet implemented: %T", n))
	}
}
