package formatter

import (
	"fmt"
	"strings"

	"github.com/wren-engine/wren/internal/parser/ast"
)

type Dialect int

const (
	DialectStandard Dialect = iota
	DialectDuckDB
	DialectPostgreSQL
)

func FormatSQL(stmt ast.Statement) string {
	return FormatSQLDialect(stmt, DialectStandard)
}

func FormatSQLDialect(stmt ast.Statement, dialect Dialect) string {
	f := &formatter{dialect: dialect}
	f.format(stmt)
	return f.builder.String()
}

type formatter struct {
	dialect Dialect
	builder strings.Builder
}

func (f *formatter) format(node ast.Node) {
	switch n := node.(type) {
	case *ast.Query:
		f.formatQuery(n)
	case *ast.QuerySpecification:
		f.formatQuerySpec(n)
	default:
		f.builder.WriteString(fmt.Sprintf("/* unhandled: %T */", n))
	}
}

func (f *formatter) formatQuery(q *ast.Query) {
	if q.With != nil {
		f.formatWith(q.With)
		f.builder.WriteString(" ")
	}
	f.format(q.Body)
	if len(q.OrderBy) > 0 {
		f.builder.WriteString(" ORDER BY ")
		for i, si := range q.OrderBy {
			if i > 0 {
				f.builder.WriteString(", ")
			}
			f.formatExpr(si.SortKey)
			if si.Ordering == ast.OrderingDesc {
				f.builder.WriteString(" DESC")
			}
		}
	}
	if q.Limit != nil {
		f.builder.WriteString(" LIMIT ")
		f.formatExpr(q.Limit)
	}
}

func (f *formatter) formatQuerySpec(qs *ast.QuerySpecification) {
	f.builder.WriteString("SELECT ")
	if qs.Select != nil && qs.Select.Distinct {
		f.builder.WriteString("DISTINCT ")
	}
	if qs.Select != nil {
		for i, item := range qs.Select.SelectItems {
			if i > 0 {
				f.builder.WriteString(", ")
			}
			f.formatSelectItem(item)
		}
	}
	if qs.From != nil {
		f.builder.WriteString(" FROM ")
		f.formatRelation(qs.From)
	}
	if qs.Where != nil {
		f.builder.WriteString(" WHERE ")
		f.formatExpr(qs.Where)
	}
	if qs.GroupBy != nil && len(qs.GroupBy.Expressions) > 0 {
		f.builder.WriteString(" GROUP BY ")
		for i, e := range qs.GroupBy.Expressions {
			if i > 0 {
				f.builder.WriteString(", ")
			}
			f.formatExpr(e)
		}
	}
}

func (f *formatter) formatWith(w *ast.With) {
	f.builder.WriteString("WITH ")
	if w.Recursive {
		f.builder.WriteString("RECURSIVE ")
	}
	for i, q := range w.Queries {
		if i > 0 {
			f.builder.WriteString(", ")
		}
		f.formatIdent(q.Name)
		f.builder.WriteString(" AS (")
		f.format(q.Query)
		f.builder.WriteString(")")
	}
}

func (f *formatter) formatSelectItem(item ast.SelectItem) {
	switch s := item.(type) {
	case *ast.SingleColumn:
		f.formatExpr(s.Expression)
		if s.Alias != nil {
			f.builder.WriteString(" AS ")
			f.formatIdent(s.Alias)
		}
	case *ast.AllColumns:
		if s.QualifiedName != nil {
			f.builder.WriteString(s.QualifiedName.String())
			f.builder.WriteString(".")
		}
		f.builder.WriteString("*")
	}
}

func (f *formatter) formatRelation(r ast.Relation) {
	switch n := r.(type) {
	case *ast.Table:
		f.builder.WriteString(n.Name.String())
		if n.Alias != nil {
			f.builder.WriteString(" AS ")
			f.formatIdent(n.Alias)
		}
	case *ast.AliasedRelation:
		f.formatRelation(n.Relation)
		f.builder.WriteString(" AS ")
		f.formatIdent(n.Alias)
	case *ast.Join:
		f.formatRelation(n.Left)
		switch n.JoinType {
		case ast.JoinTypeInner:
			f.builder.WriteString(" JOIN ")
		case ast.JoinTypeLeft:
			f.builder.WriteString(" LEFT JOIN ")
		case ast.JoinTypeRight:
			f.builder.WriteString(" RIGHT JOIN ")
		case ast.JoinTypeFull:
			f.builder.WriteString(" FULL JOIN ")
		case ast.JoinTypeCross:
			f.builder.WriteString(" CROSS JOIN ")
		case ast.JoinTypeImplicit:
			f.builder.WriteString(", ")
		}
		f.formatRelation(n.Right)
		if n.Criteria != nil {
			switch c := n.Criteria.(type) {
			case *ast.JoinOn:
				f.builder.WriteString(" ON ")
				f.formatExpr(c.Expression)
			case *ast.JoinUsing:
				f.builder.WriteString(" USING (")
				for i, col := range c.Columns {
					if i > 0 {
						f.builder.WriteString(", ")
					}
					f.builder.WriteString(col.Value)
				}
				f.builder.WriteString(")")
			}
		}
	default:
		f.builder.WriteString(fmt.Sprintf("/* unhandled relation: %T */", n))
	}
}

func (f *formatter) formatExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		f.formatIdent(e)
	case *ast.LongLiteral:
		fmt.Fprintf(&f.builder, "%d", e.Value)
	case *ast.StringLiteral:
		fmt.Fprintf(&f.builder, "'%s'", e.Value)
	case *ast.DoubleLiteral:
		fmt.Fprintf(&f.builder, "%g", e.Value)
	case *ast.BooleanLiteral:
		if e.Value {
			f.builder.WriteString("TRUE")
		} else {
			f.builder.WriteString("FALSE")
		}
	case *ast.NullLiteral:
		f.builder.WriteString("NULL")
	case *ast.DereferenceExpression:
		f.formatExpr(e.Base)
		f.builder.WriteString(".")
		f.formatIdent(e.Field)
	case *ast.ComparisonExpression:
		f.formatExpr(e.Left)
		fmt.Fprintf(&f.builder, " %s ", e.Operator)
		f.formatExpr(e.Right)
	case *ast.ArithmeticBinaryExpression:
		f.formatExpr(e.Left)
		fmt.Fprintf(&f.builder, " %s ", e.Operator)
		f.formatExpr(e.Right)
	case *ast.LogicalBinaryExpression:
		f.formatExpr(e.Left)
		fmt.Fprintf(&f.builder, " %s ", e.Operator)
		f.formatExpr(e.Right)
	case *ast.FunctionCall:
		f.builder.WriteString(e.Name.String())
		f.builder.WriteString("(")
		if e.Distinct {
			f.builder.WriteString("DISTINCT ")
		}
		for i, arg := range e.Arguments {
			if i > 0 {
				f.builder.WriteString(", ")
			}
			f.formatExpr(arg)
		}
		f.builder.WriteString(")")
	case *ast.Cast:
		f.builder.WriteString("CAST(")
		f.formatExpr(e.Expression)
		f.builder.WriteString(" AS ")
		f.builder.WriteString(e.Type.Name)
		f.builder.WriteString(")")
	case *ast.StarExpression:
		f.builder.WriteString("*")
	case *ast.IsNullPredicate:
		f.formatExpr(e.Value)
		if e.Not {
			f.builder.WriteString(" IS NOT NULL")
		} else {
			f.builder.WriteString(" IS NULL")
		}
	default:
		fmt.Fprintf(&f.builder, "/* unhandled expr: %T */", e)
	}
}

func (f *formatter) formatIdent(id *ast.Identifier) {
	if id == nil {
		return
	}
	if id.Delimited {
		fmt.Fprintf(&f.builder, `"%s"`, id.Value)
	} else {
		f.builder.WriteString(id.Value)
	}
}
