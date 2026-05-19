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
	case *ast.Select:
		f.visitSelect(n, indent)
	case *ast.SingleColumn:
		f.visitSingleColumn(n)
	case *ast.AllColumns:
		f.visitAllColumns(n)
	case *ast.QuerySpecification:
		f.visitQuerySpecification(n, indent)
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

// visitSelect mirrors trino SqlFormatter.visitSelect — note the one-column vs
// multi-column layout fork.
func (f *formatter) visitSelect(n *ast.Select, indent int) {
	f.append(indent, "SELECT")
	if n.Distinct {
		f.builder.WriteString(" DISTINCT")
	}
	if len(n.SelectItems) > 1 {
		for i, item := range n.SelectItems {
			f.builder.WriteString("\n")
			f.builder.WriteString(f.indentString(indent))
			if i == 0 {
				f.builder.WriteString("  ")
			} else {
				f.builder.WriteString(", ")
			}
			f.process(item, indent)
		}
	} else {
		f.builder.WriteString(" ")
		f.process(n.SelectItems[0], indent)
	}
	f.builder.WriteString("\n")
}

// visitSingleColumn mirrors trino SqlFormatter.visitSingleColumn.
func (f *formatter) visitSingleColumn(n *ast.SingleColumn) {
	f.builder.WriteString(formatExpression(n.Expression, f.dialect))
	if n.Alias != nil {
		f.builder.WriteString(" ")
		f.builder.WriteString(formatExpression(n.Alias, f.dialect))
	}
}

// visitAllColumns mirrors trino SqlFormatter.visitAllColumns ("t.*" / "*").
func (f *formatter) visitAllColumns(n *ast.AllColumns) {
	if n.QualifiedName != nil {
		f.builder.WriteString(formatName(*n.QualifiedName, f.dialect))
		f.builder.WriteString(".")
	}
	f.builder.WriteString("*")
}

// visitQuerySpecification mirrors trino SqlFormatter.visitQuerySpecification.
func (f *formatter) visitQuerySpecification(n *ast.QuerySpecification, indent int) {
	f.process(n.Select, indent)

	if n.From != nil {
		f.append(indent, "FROM")
		f.builder.WriteString("\n")
		f.append(indent, "  ")
		f.process(n.From, indent)
	}
	f.builder.WriteString("\n")

	if n.Where != nil {
		f.append(indent, "WHERE "+formatExpression(n.Where, f.dialect))
		f.builder.WriteString("\n")
	}
	if n.GroupBy != nil {
		f.append(indent, "GROUP BY "+f.formatGroupBy(n.GroupBy))
		f.builder.WriteString("\n")
	}
	if n.Having != nil {
		f.append(indent, "HAVING "+formatExpression(n.Having, f.dialect))
		f.builder.WriteString("\n")
	}

	f.appendOrderBy(n.OrderBy, indent)
	f.appendOffset(n.Offset, indent)
	f.appendLimit(n.Limit, indent)
}

// appendOrderBy emits an ORDER BY line when items is non-empty. Mirrors trino
// SqlFormatter.visitOrderBy.
func (f *formatter) appendOrderBy(items []ast.SortItem, indent int) {
	if len(items) == 0 {
		return
	}
	ef := &exprFormatter{dialect: f.dialect}
	f.append(indent, ef.formatOrderBy(items))
	f.builder.WriteString("\n")
}

// appendOffset emits an OFFSET line. Mirrors trino SqlFormatter.visitOffset
// (DEFAULT dialect: an extra "ROWS" line follows).
func (f *formatter) appendOffset(offset ast.Expression, indent int) {
	if offset == nil {
		return
	}
	f.append(indent, "OFFSET "+formatExpression(offset, f.dialect))
	f.builder.WriteString("\n")
	f.append(indent, "ROWS\n")
}

// appendLimit emits a LIMIT line. Mirrors trino SqlFormatter.visitLimit.
func (f *formatter) appendLimit(limit ast.Expression, indent int) {
	if limit == nil {
		return
	}
	f.append(indent, "LIMIT "+formatExpression(limit, f.dialect))
	f.builder.WriteString("\n")
}

// formatGroupBy renders the grouping elements. Mirrors trino formatGroupBy.
// The Go AST GroupBy carries Expressions plus a Sets flag (true for GROUPING
// SETS); Cube/Rollup are not in the P2 corpus.
func (f *formatter) formatGroupBy(g *ast.GroupBy) string {
	ef := &exprFormatter{dialect: f.dialect}
	if !g.Sets {
		parts := make([]string, len(g.Expressions))
		for i, x := range g.Expressions {
			parts[i] = ef.process(x)
		}
		return strings.Join(parts, ", ")
	}
	// GROUPING SETS: each element is itself a parenthesized list. The Go AST
	// models each set as a *Row of its grouping columns.
	sets := make([]string, len(g.Expressions))
	for i, x := range g.Expressions {
		if r, ok := x.(*ast.Row); ok {
			cols := make([]string, len(r.Items))
			for j, it := range r.Items {
				cols[j] = ef.process(it)
			}
			sets[i] = "(" + strings.Join(cols, ", ") + ")"
		} else {
			sets[i] = "(" + ef.process(x) + ")"
		}
	}
	return "GROUPING SETS (" + strings.Join(sets, ", ") + ")"
}
