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
	case *ast.Table:
		f.builder.WriteString(formatName(n.Name, f.dialect))
	case *ast.AliasedRelation:
		f.visitAliasedRelation(n, indent)
	case *ast.TableSubquery:
		f.visitTableSubquery(n, indent)
	case *ast.Join:
		f.visitJoin(n, indent)
	case *ast.Unnest:
		f.visitUnnest(n)
	case *ast.Lateral:
		f.visitLateral(n, indent)
	case *ast.SampledRelation:
		f.visitSampledRelation(n, indent)
	case *ast.FunctionRelation:
		f.visitFunctionRelation(n)
	case *ast.Values:
		f.visitValues(n, indent)
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

// visitAliasedRelation mirrors trino SqlFormatter.visitAliasedRelation.
func (f *formatter) visitAliasedRelation(n *ast.AliasedRelation, indent int) {
	f.processRelationSuffix(n.Relation, indent)
	f.builder.WriteString(" ")
	f.builder.WriteString(formatExpression(n.Alias, f.dialect))
	f.appendAliasColumns(n.ColumnNames)
}

// processRelationSuffix wraps a relation in "( ... )" when it needs grouping
// (aliased / sampled / join), otherwise processes it directly. Mirrors trino
// SqlFormatter.processRelationSuffix.
func (f *formatter) processRelationSuffix(relation ast.Relation, indent int) {
	switch relation.(type) {
	case *ast.AliasedRelation, *ast.SampledRelation, *ast.Join:
		f.builder.WriteString("( ")
		f.process(relation, indent+1)
		f.append(indent, ")")
	default:
		f.process(relation, indent)
	}
}

// visitTableSubquery mirrors trino SqlFormatter.visitTableSubquery.
func (f *formatter) visitTableSubquery(n *ast.TableSubquery, indent int) {
	f.builder.WriteString("(\n")
	f.process(n.Query, indent+1)
	f.append(indent, ") ")
}

// appendAliasColumns appends " (col, col, ...)" when columns is non-empty.
// Mirrors trino SqlFormatter.appendAliasColumns.
func (f *formatter) appendAliasColumns(columns []ast.Identifier) {
	if len(columns) == 0 {
		return
	}
	parts := make([]string, len(columns))
	for i := range columns {
		parts[i] = formatExpression(&columns[i], f.dialect)
	}
	f.builder.WriteString(" (")
	f.builder.WriteString(strings.Join(parts, ", "))
	f.builder.WriteString(")")
}

// visitJoin mirrors trino SqlFormatter.visitJoin.
func (f *formatter) visitJoin(n *ast.Join, indent int) {
	joinType := string(n.JoinType) // CROSS / INNER / LEFT / RIGHT / FULL
	if _, natural := n.Criteria.(*ast.NaturalJoin); natural {
		joinType = "NATURAL " + joinType
	}

	f.process(n.Left, indent)
	f.builder.WriteString("\n")
	if n.JoinType == ast.JoinTypeImplicit {
		f.append(indent, ", ")
	} else {
		f.append(indent, joinType)
		f.builder.WriteString(" JOIN ")
	}
	f.process(n.Right, indent)

	if n.JoinType == ast.JoinTypeCross || n.JoinType == ast.JoinTypeImplicit {
		return
	}
	switch c := n.Criteria.(type) {
	case *ast.JoinUsing:
		cols := make([]string, len(c.Columns))
		for i := range c.Columns {
			cols[i] = c.Columns[i].Value
		}
		f.builder.WriteString(" USING (")
		f.builder.WriteString(strings.Join(cols, ", "))
		f.builder.WriteString(")")
	case *ast.JoinOn:
		f.builder.WriteString(" ON ")
		f.builder.WriteString(formatExpression(c.Expression, f.dialect))
	case *ast.NaturalJoin, nil:
		// no criteria suffix
	default:
		panic(fmt.Sprintf("SqlFormatter: unknown join criteria: %T", c))
	}
}

// visitUnnest mirrors trino SqlFormatter.visitUnnest (DEFAULT dialect).
func (f *formatter) visitUnnest(n *ast.Unnest) {
	parts := make([]string, len(n.Expressions))
	for i, x := range n.Expressions {
		parts[i] = formatExpression(x, f.dialect)
	}
	f.builder.WriteString("UNNEST(")
	f.builder.WriteString(strings.Join(parts, ", "))
	f.builder.WriteString(")")
	if n.Ordinality {
		f.builder.WriteString(" WITH ORDINALITY")
	}
}

// visitLateral mirrors trino SqlFormatter.visitLateral.
func (f *formatter) visitLateral(n *ast.Lateral, indent int) {
	f.append(indent, "LATERAL (")
	f.process(n.Query, indent+1)
	f.append(indent, ")")
}

// visitSampledRelation mirrors trino SqlFormatter.visitSampledRelation.
func (f *formatter) visitSampledRelation(n *ast.SampledRelation, indent int) {
	f.processRelationSuffix(n.Relation, indent)
	f.builder.WriteString(" TABLESAMPLE ")
	f.builder.WriteString(n.SampleType)
	f.builder.WriteString(" (")
	f.builder.WriteString(formatExpression(n.SamplePercentage, f.dialect))
	f.builder.WriteString(")")
}

// visitFunctionRelation mirrors trino SqlFormatter.visitFunctionRelation.
func (f *formatter) visitFunctionRelation(n *ast.FunctionRelation) {
	parts := make([]string, len(n.Arguments))
	for i, a := range n.Arguments {
		parts[i] = formatExpression(a, f.dialect)
	}
	f.builder.WriteString(formatName(n.Name, f.dialect))
	f.builder.WriteString("(")
	f.builder.WriteString(strings.Join(parts, ", "))
	f.builder.WriteString(")")
}

// visitValues mirrors trino SqlFormatter.visitValues: each row on its own line.
func (f *formatter) visitValues(n *ast.Values, indent int) {
	f.builder.WriteString(" VALUES ")
	for i, row := range n.Rows {
		f.builder.WriteString("\n")
		f.builder.WriteString(f.indentString(indent))
		if i == 0 {
			f.builder.WriteString("  ")
		} else {
			f.builder.WriteString(", ")
		}
		if len(row) == 1 {
			if r, ok := row[0].(*ast.Row); ok {
				f.builder.WriteString(formatExpression(r, f.dialect))
				continue
			}
		}
		parts := make([]string, len(row))
		for j, x := range row {
			parts[j] = formatExpression(x, f.dialect)
		}
		f.builder.WriteString("(")
		f.builder.WriteString(strings.Join(parts, ", "))
		f.builder.WriteString(")")
	}
	f.builder.WriteString("\n")
}
