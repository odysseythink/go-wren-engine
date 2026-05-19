// Package formatter renders an AST back to SQL text. It is a faithful port of
// trino's SqlFormatter (statement/query/relation visits, indentation) and
// ExpressionFormatter (expression visits). The byte output is the engine's
// external contract: WrenPlanner re-parses and re-formats between every
// rewrite rule, so P2 requires byte-for-byte parity with Java.
package formatter

import (
	"fmt"
	"strings"

	"github.com/wren-engine/wren/internal/parser/ast"
)

// Dialect selects dialect-specific formatting. P2 targets DialectStandard
// (trino SqlFormatter.Dialect.DEFAULT); the DuckDB branch is filled in by P4.
type Dialect int

const (
	DialectStandard Dialect = iota
	DialectDuckDB
	DialectPostgreSQL
)

// indentUnit mirrors trino SqlFormatter.INDENT — three spaces.
const indentUnit = "   "

// FormatSQL renders stmt to SQL text using the DEFAULT dialect.
func FormatSQL(stmt ast.Statement) string {
	return FormatSQLDialect(stmt, DialectStandard)
}

// FormatSQLDialect renders stmt to SQL text using the given dialect.
func FormatSQLDialect(stmt ast.Statement, dialect Dialect) string {
	f := &formatter{dialect: dialect}
	f.process(stmt, 0)
	return f.builder.String()
}

// formatter is the indented statement/query/relation visitor. It mirrors
// trino SqlFormatter.Formatter (an AstVisitor<Void, Integer>).
type formatter struct {
	dialect Dialect
	builder strings.Builder
}

// process dispatches one statement/query/relation/clause node. Expressions are
// not handled here — they go through formatExpression. An unhandled node is a
// hard error, mirroring Java SqlFormatter.visitNode throwing.
func (f *formatter) process(node ast.Node, indent int) {
	switch n := node.(type) {
	default:
		panic(fmt.Sprintf("SqlFormatter: not yet implemented: %T", n))
	}
}

// append writes indentString(indent) followed by s. Mirrors Java
// Formatter.append(int, String).
func (f *formatter) append(indent int, s string) {
	f.builder.WriteString(f.indentString(indent))
	f.builder.WriteString(s)
}

// indentString returns the indent prefix for the given depth.
func (f *formatter) indentString(indent int) string {
	return strings.Repeat(indentUnit, indent)
}

// formatName joins a qualified name with dots, each part rendered as an
// expression. Mirrors trino SqlFormatter.formatName.
func formatName(name ast.QualifiedName, dialect Dialect) string {
	parts := make([]string, len(name.OriginalParts))
	for i := range name.OriginalParts {
		parts[i] = formatExpression(&name.OriginalParts[i], dialect)
	}
	return strings.Join(parts, ".")
}
