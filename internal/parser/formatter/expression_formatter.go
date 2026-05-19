package formatter

import (
	"fmt"
	"strconv"
	"strings"

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
	if expr == nil {
		return ""
	}
	switch n := expr.(type) {
	case *ast.Identifier:
		if !n.Delimited {
			return n.Value
		}
		return formatIdentifier(n.Value, e.dialect)
	case *ast.LongLiteral:
		return strconv.FormatInt(n.Value, 10)
	case *ast.DoubleLiteral:
		return formatDouble(n.Value, e.dialect)
	case *ast.StringLiteral:
		return formatStringLiteral(n.Value)
	case *ast.GenericLiteral:
		return n.Type + " " + formatStringLiteral(n.Value)
	case *ast.BooleanLiteral:
		if n.Value {
			return "true"
		}
		return "false"
	case *ast.NullLiteral:
		return "null"
	case *ast.ComparisonExpression:
		return e.formatBinary(string(n.Operator), n.Left, n.Right)
	case *ast.ArithmeticBinaryExpression:
		return e.formatBinary(string(n.Operator), n.Left, n.Right)
	case *ast.LogicalExpression:
		parts := make([]string, len(n.Terms))
		for i, t := range n.Terms {
			parts[i] = e.process(t)
		}
		return "(" + strings.Join(parts, " "+string(n.Operator)+" ") + ")"
	case *ast.NotExpression:
		return "(NOT " + e.process(n.Value) + ")"
	case *ast.FunctionCall:
		return e.formatFunctionCall(n)
	case *ast.Cast:
		kind := "CAST"
		if n.Safe {
			kind = "TRY_CAST"
		}
		return kind + "(" + e.process(n.Expression) + " AS " + e.formatType(&n.Type) + ")"
	case *ast.SearchedCaseExpression:
		return e.formatSearchedCase(n)
	case *ast.SimpleCaseExpression:
		return e.formatSimpleCase(n)
	case *ast.WhenClause:
		return "WHEN " + e.process(n.Operand) + " THEN " + e.process(n.Result)
	case *ast.IfExpression:
		out := "IF(" + e.process(n.Condition) + ", " + e.process(n.TrueValue)
		if n.FalseValue != nil {
			out += ", " + e.process(n.FalseValue)
		}
		return out + ")"
	case *ast.NullIfExpression:
		return "NULLIF(" + e.process(n.First) + ", " + e.process(n.Second) + ")"
	case *ast.CoalesceExpression:
		return "COALESCE(" + e.joinExpressions(n.Operands) + ")"
	case *ast.InPredicate:
		out := "(" + e.process(n.Value) + " IN " + e.process(n.ValueList) + ")"
		if n.Not {
			out = "(NOT " + out + ")"
		}
		return out
	case *ast.InListExpression:
		return "(" + e.joinExpressions(n.Values) + ")"
	case *ast.BetweenPredicate:
		out := "(" + e.process(n.Value) + " BETWEEN " +
			e.process(n.Min) + " AND " + e.process(n.Max) + ")"
		if n.Not {
			out = "(NOT " + out + ")"
		}
		return out
	case *ast.LikePredicate:
		out := "(" + e.process(n.Value) + " LIKE " + e.process(n.Pattern)
		if n.Escape != nil {
			out += " ESCAPE " + e.process(n.Escape)
		}
		out += ")"
		if n.Not {
			out = "(NOT " + out + ")"
		}
		return out
	case *ast.IsNullPredicate:
		if n.Not {
			return "(" + e.process(n.Value) + " IS NOT NULL)"
		}
		return "(" + e.process(n.Value) + " IS NULL)"
	case *ast.ExtractExpression:
		return "EXTRACT(" + n.Field + " FROM " + e.process(n.Expression) + ")"
	case *ast.SubscriptExpression:
		return e.process(n.Base) + "[" + e.process(n.Index) + "]"
	case *ast.Row:
		return "ROW (" + e.joinExpressions(n.Items) + ")"
	case *ast.AtTimeZone:
		return e.process(n.Value) + " AT TIME ZONE " + e.process(n.TimeZone)
	case *ast.DereferenceExpression:
		base := e.process(n.Base)
		if n.Field == nil {
			return base + ".*"
		}
		return base + "." + e.process(n.Field)
	case *ast.SubqueryExpression:
		return "(" + FormatSQLDialect(n.Query, e.dialect) + ")"
	case *ast.ExistsPredicate:
		return "(EXISTS (" + FormatSQLDialect(n.Subquery, e.dialect) + "))"
	case *ast.QuantifiedComparison:
		return "(" + e.process(n.Value) + " " + string(n.Operator) + " " +
			n.Quantifier + " (" + FormatSQLDialect(n.Subquery.(*ast.SubqueryExpression).Query, e.dialect) + "))"
	case *ast.StarExpression:
		return "*"
	case *ast.IntervalLiteral:
		return e.formatInterval(n)
	default:
		panic(fmt.Sprintf("ExpressionFormatter: not yet implemented: %T", n))
	}
}

// formatIdentifier quotes a delimited identifier. trino formatIdentifier:
// DEFAULT/DUCKDB/POSTGRES all wrap in double quotes and double any embedded
// double quote.
func formatIdentifier(s string, dialect Dialect) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// formatStringLiteral wraps s in single quotes, doubling any embedded quote.
// Mirrors trino ExpressionFormatter.formatStringLiteral.
func formatStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// formatDouble renders a double literal. The DUCKDB dialect (filled in by P4)
// uses String.valueOf; the DEFAULT dialect uses Java's
// DecimalFormat("0.###################E0###") — normalized scientific notation
// with one integer digit, up to 19 fractional digits, and a sign-on-negative
// exponent with no leading zeros.
func formatDouble(v float64, dialect Dialect) string {
	// strconv 'E' with precision -1 gives the shortest round-tripping mantissa
	// (<=17 significant digits for float64, within DecimalFormat's 19-# limit).
	// Go emits e.g. "6E-02" / "1E+02"; Java emits "6E-2" / "1E2": strip the
	// '+' and any leading zeros from the exponent.
	s := strconv.FormatFloat(v, 'E', -1, 64)
	mantissa, exp, ok := strings.Cut(s, "E")
	if !ok {
		return s
	}
	sign := ""
	if strings.HasPrefix(exp, "-") {
		sign = "-"
		exp = exp[1:]
	} else if strings.HasPrefix(exp, "+") {
		exp = exp[1:]
	}
	exp = strings.TrimLeft(exp, "0")
	if exp == "" {
		exp = "0"
	}
	return mantissa + "E" + sign + exp
}

// formatType renders a data type. Mirrors trino ExpressionFormatter
// .visitGenericDataType: NAME optionally followed by (arg, arg, ...).
func (e *exprFormatter) formatType(t *ast.DataType) string {
	result := t.Name
	if len(t.Parameters) == 0 {
		return result
	}
	parts := make([]string, len(t.Parameters))
	for i, p := range t.Parameters {
		parts[i] = e.formatTypeParameter(p)
	}
	return result + "(" + strings.Join(parts, ", ") + ")"
}

// formatTypeParameter renders one type parameter — a nested type or a number.
func (e *exprFormatter) formatTypeParameter(p ast.DataTypeParameter) string {
	switch tp := p.(type) {
	case *ast.TypeParameter:
		return e.formatType(&tp.Type)
	case *ast.NumericParameter:
		return tp.Value
	default:
		panic(fmt.Sprintf("ExpressionFormatter: not yet implemented type param: %T", tp))
	}
}

// formatBinary renders a binary expression fully parenthesized:
// '(' left ' ' op ' ' right ')'. Mirrors trino formatBinaryExpression.
func (e *exprFormatter) formatBinary(op string, left, right ast.Expression) string {
	return "(" + e.process(left) + " " + op + " " + e.process(right) + ")"
}

// formatFunctionCall mirrors trino ExpressionFormatter.visitFunctionCall for
// the DEFAULT dialect (BigQuery/DuckDB special-casing is out of P2 scope).
func (e *exprFormatter) formatFunctionCall(n *ast.FunctionCall) string {
	arguments := e.joinExpressions(n.Arguments)
	if len(n.Arguments) == 0 && strings.EqualFold(n.Name.Last(), "count") {
		arguments = "*"
	}
	if n.Distinct {
		arguments = "DISTINCT " + arguments
	}

	var b strings.Builder
	b.WriteString(formatName(n.Name, e.dialect))
	b.WriteString("(")
	b.WriteString(arguments)
	if len(n.OrderBy) > 0 {
		b.WriteString(" ")
		b.WriteString(e.formatOrderBy(n.OrderBy))
	}
	b.WriteString(")")

	if n.IgnoreNulls {
		b.WriteString(" IGNORE NULLS")
	}
	if n.Filter != nil {
		b.WriteString(" FILTER (WHERE ")
		b.WriteString(e.process(n.Filter))
		b.WriteString(")")
	}
	if n.Window != nil {
		b.WriteString(" OVER ")
		b.WriteString(e.formatWindowSpec(n.Window))
	}
	return b.String()
}

// joinExpressions renders a comma-separated list of expressions.
func (e *exprFormatter) joinExpressions(exprs []ast.Expression) string {
	parts := make([]string, len(exprs))
	for i, x := range exprs {
		parts[i] = e.process(x)
	}
	return strings.Join(parts, ", ")
}

// formatOrderBy renders "ORDER BY <items>". Mirrors trino formatOrderBy.
func (e *exprFormatter) formatOrderBy(items []ast.SortItem) string {
	return "ORDER BY " + e.formatSortItems(items)
}

// formatSortItems renders a comma-separated list of sort items.
func (e *exprFormatter) formatSortItems(items []ast.SortItem) string {
	parts := make([]string, len(items))
	for i := range items {
		parts[i] = e.formatSortItem(&items[i])
	}
	return strings.Join(parts, ", ")
}

// formatSortItem renders "<key> ASC|DESC [NULLS FIRST|LAST]". Mirrors trino
// sortItemFormatterFunction.
func (e *exprFormatter) formatSortItem(s *ast.SortItem) string {
	out := e.process(s.SortKey)
	if s.Ordering == ast.OrderingDesc {
		out += " DESC"
	} else {
		out += " ASC"
	}
	switch s.NullOrdering {
	case ast.NullOrderingFirst:
		out += " NULLS FIRST"
	case ast.NullOrderingLast:
		out += " NULLS LAST"
	}
	return out
}

// formatWindowSpec renders a window specification "( ... )". Mirrors trino
// formatWindowSpecification + formatFrame.
func (e *exprFormatter) formatWindowSpec(w *ast.Window) string {
	var parts []string
	if len(w.PartitionBy) > 0 {
		parts = append(parts, "PARTITION BY "+e.joinExpressions(w.PartitionBy))
	}
	if len(w.OrderBy) > 0 {
		parts = append(parts, e.formatOrderBy(w.OrderBy))
	}
	if w.Frame != nil {
		parts = append(parts, e.formatFrame(w.Frame))
	}
	return "(" + strings.Join(parts, " ") + ")"
}

// formatFrame renders a window frame. Mirrors trino formatFrame for the
// ROWS/RANGE BETWEEN ... AND ... shapes the corpus exercises.
func (e *exprFormatter) formatFrame(f *ast.WindowFrame) string {
	out := string(f.Type) + " "
	if f.End.Type != "" {
		return out + "BETWEEN " + e.formatFrameBound(&f.Start) +
			" AND " + e.formatFrameBound(&f.End)
	}
	return out + e.formatFrameBound(&f.Start)
}

// formatFrameBound renders one frame bound. Mirrors trino formatFrameBound.
func (e *exprFormatter) formatFrameBound(b *ast.FrameBound) string {
	switch b.Type {
	case ast.BoundTypeUnboundedPreceding:
		return "UNBOUNDED PRECEDING"
	case ast.BoundTypePreceding:
		return e.process(b.Value) + " PRECEDING"
	case ast.BoundTypeCurrentRow:
		return "CURRENT ROW"
	case ast.BoundTypeFollowing:
		return e.process(b.Value) + " FOLLOWING"
	case ast.BoundTypeUnboundedFollowing:
		return "UNBOUNDED FOLLOWING"
	default:
		panic(fmt.Sprintf("ExpressionFormatter: unhandled frame bound: %q", b.Type))
	}
}

// formatSearchedCase renders "(CASE WHEN ... [ELSE ...] END)". Mirrors trino
// visitSearchedCaseExpression — the whole CASE is wrapped in parentheses.
func (e *exprFormatter) formatSearchedCase(n *ast.SearchedCaseExpression) string {
	parts := []string{"CASE"}
	for i := range n.WhenClauses {
		parts = append(parts, e.process(&n.WhenClauses[i]))
	}
	if n.DefaultValue != nil {
		parts = append(parts, "ELSE", e.process(n.DefaultValue))
	}
	parts = append(parts, "END")
	return "(" + strings.Join(parts, " ") + ")"
}

// formatSimpleCase renders "(CASE operand WHEN ... [ELSE ...] END)". Mirrors
// trino visitSimpleCaseExpression.
func (e *exprFormatter) formatSimpleCase(n *ast.SimpleCaseExpression) string {
	parts := []string{"CASE", e.process(n.Operand)}
	for i := range n.WhenClauses {
		parts = append(parts, e.process(&n.WhenClauses[i]))
	}
	if n.DefaultValue != nil {
		parts = append(parts, "ELSE", e.process(n.DefaultValue))
	}
	parts = append(parts, "END")
	return "(" + strings.Join(parts, " ") + ")"
}

// formatInterval renders an interval literal. Mirrors trino
// ExpressionFormatter.visitIntervalLiteral.
func (e *exprFormatter) formatInterval(n *ast.IntervalLiteral) string {
	result := "INTERVAL "
	if n.Sign != "" {
		result += n.Sign
	}
	result += " " + formatStringLiteral(n.Value) + " " + strings.ToUpper(n.From)
	if n.To != "" {
		result += " TO " + strings.ToUpper(n.To)
	}
	return result
}
