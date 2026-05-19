package rewrite

import (
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// rewriteRelationship replaces relationship dereferences in expr with the
// dereference expression that points at the joined model. Mirrors
// RelationshipRewriter.rewrite.
func rewriteRelationship(infos []*analyzer.ExpressionRelationshipInfo, expr ast.Expression) ast.Expression {
	replacements := map[string]ast.Expression{}
	for _, info := range infos {
		replacements[info.QualifiedName().String()] = toDereferenceExpression(info)
	}
	return RewriteNode(expr, func(n ast.Node) (ast.Node, bool) {
		d, ok := n.(*ast.DereferenceExpression)
		if !ok {
			return nil, false // descend
		}
		if qn := ast.GetQualifiedName(d); qn != nil {
			if r, found := replacements[qn.String()]; found {
				return r, true
			}
		}
		return d, true // matched a dereference but no replacement: stop, unchanged
	}).(ast.Expression)
}

// toDereferenceExpression builds "<lastModel>.<remainingParts...>" as a
// delimited dereference chain. Mirrors RelationshipRewriter.toDereferenceExpression.
func toDereferenceExpression(info *analyzer.ExpressionRelationshipInfo) ast.Expression {
	rels := info.Relationships()
	base := rels[len(rels)-1].Models[1]
	parts := []ast.Identifier{{Value: base, Delimited: true}}
	for _, p := range info.RemainingParts() {
		parts = append(parts, ast.Identifier{Value: p, Delimited: true})
	}
	return dereferenceFrom(parts)
}
