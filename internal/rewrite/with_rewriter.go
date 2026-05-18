package rewrite

import "github.com/wren-engine/wren/internal/parser/ast"

// PrependCTEs prepends CTEs to a Query's WITH clause.
func PrependCTEs(query *ast.Query, ctes []ast.WithQuery) {
	if query.With == nil {
		query.With = &ast.With{}
	}
	query.With.Queries = append(ctes, query.With.Queries...)
}
