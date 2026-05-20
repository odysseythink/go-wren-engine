package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

// RelationId identifies a relation by its source AST node (by identity).
// An anonymous RelationId (sourceNode == nil) equals only itself.
// Mirrors Java sqlrewrite.analyzer.RelationId.
type RelationId struct {
	sourceNode ast.Node
}

func RelationIdOf(sourceNode ast.Node) RelationId { return RelationId{sourceNode: sourceNode} }
func AnonymousRelationId() RelationId             { return RelationId{} }

func (r RelationId) IsAnonymous() bool    { return r.sourceNode == nil }
func (r RelationId) SourceNode() ast.Node { return r.sourceNode }
