package rewrite

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

// parseSQL parses a statement. Mirrors Java Utils.parseSql.
func parseSQL(sql string) (ast.Statement, error) {
	return parser.ParseSQL(sql)
}

// parseExpression parses a standalone expression. Mirrors Java Utils.parseExpression.
func parseExpression(sql string) (ast.Expression, error) {
	return parser.ParseExpression(sql)
}

// parseQuery parses sql and asserts the result is a *ast.Query.
// Mirrors Java Utils.parseQuery.
func parseQuery(sql string) (*ast.Query, error) {
	stmt, err := parseSQL(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %s: %w", sql, err)
	}
	q, ok := stmt.(*ast.Query)
	if !ok {
		return nil, fmt.Errorf("not a query: %s", sql)
	}
	return q, nil
}

// checkArgument returns a formatted error when cond is false.
// Mirrors Java com.google.common.base.Preconditions.checkArgument.
func checkArgument(cond bool, format string, args ...any) error {
	if cond {
		return nil
	}
	return fmt.Errorf(format, args...)
}

// contains reports whether s holds v. Shared helper used by the graph and the
// SqlRender tests.
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// qualifiedConditionString parses a relationship condition and delimits all
// identifiers. Mirrors Java Relationship.qualifiedCondition.
func qualifiedConditionString(condition string) (string, error) {
	expr, err := parseExpression(condition)
	if err != nil {
		return "", fmt.Errorf("parse condition %q: %w", condition, err)
	}
	rewritten := RewriteNode(expr, func(n ast.Node) (ast.Node, bool) {
		if id, ok := n.(*ast.Identifier); ok && !id.Delimited {
			return &ast.Identifier{Value: id.Value, Delimited: true}, true
		}
		return nil, false
	}).(ast.Expression)
	return formatter.FormatExpression(rewritten), nil
}

// hasPrefixParts reports whether qn starts with the given prefix parts.
func hasPrefixParts(qn ast.QualifiedName, prefix ...string) bool {
	if len(prefix) > len(qn.Parts) {
		return false
	}
	for i, p := range prefix {
		if !strings.EqualFold(p, qn.Parts[i]) {
			return false
		}
	}
	return true
}

// dereferenceFrom builds a DereferenceExpression chain (or Identifier) from
// a slice of identifiers. Mirrors trino DereferenceExpression.from.
func dereferenceFrom(parts []ast.Identifier) ast.Expression {
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		id := parts[0]
		return &id
	}
	result := &ast.DereferenceExpression{
		Base:  dereferenceFrom(parts[:len(parts)-1]),
		Field: &parts[len(parts)-1],
	}
	return result
}

// sortedKeys returns the keys of set in ascending order — used to turn a
// requiredObjects set into a deterministic slice (risk #1).
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
