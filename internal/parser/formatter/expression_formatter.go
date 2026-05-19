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
