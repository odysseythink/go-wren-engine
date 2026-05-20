package rewrite

import "github.com/wren-engine/wren/internal/parser/ast"

// getWithQuery wraps a descriptor as a WITH query with a delimited CTE name.
// Mirrors WithRewriter.getWithQuery (Java new Identifier(name, true)). Risk #7.
func getWithQuery(d QueryDescriptor) ast.WithQuery {
	return ast.WithQuery{
		Name:  &ast.Identifier{Value: d.Name(), Delimited: true},
		Query: d.Query(),
	}
}

// applyWith prepends withQueries to root's WITH clause. Model CTEs must come
// first (Java Stream.concat(withQueries, with.getQueries())). Mirrors
// WithRewriter.visitQuery; only the root Query is affected.
func applyWith(root ast.Statement, withQueries []ast.WithQuery) ast.Statement {
	q, ok := root.(*ast.Query)
	if !ok {
		return root
	}
	out := *q // body identity preserved — do NOT RewriteNode the body (risk #5)
	switch {
	case q.With != nil:
		merged := append(append([]ast.WithQuery{}, withQueries...), q.With.Queries...)
		out.With = &ast.With{Recursive: q.With.Recursive, Queries: merged}
	case len(withQueries) > 0:
		out.With = &ast.With{Recursive: false, Queries: withQueries}
	default:
		out.With = nil
	}
	return &out
}
