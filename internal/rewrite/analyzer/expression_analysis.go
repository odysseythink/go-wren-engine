package analyzer

import (
	"github.com/wren-engine/wren/internal/parser/ast"
)

// ExpressionAnalysis holds the result of analyzing an expression.
// Mirrors analyzer.ExpressionAnalysis.
type ExpressionAnalysis struct {
	referencedFields map[ast.NodeRef]*Field
	collectedFields  []*Field
	predicates       []*ast.ComparisonExpression
	requireRelation  bool
}

func newExpressionAnalysis(
	referencedFields map[ast.NodeRef]*Field,
	predicates []*ast.ComparisonExpression,
	requireRelation bool,
) *ExpressionAnalysis {
	return &ExpressionAnalysis{
		referencedFields: referencedFields,
		predicates:       predicates,
		requireRelation:  requireRelation,
	}
}

func (e *ExpressionAnalysis) ReferencedFields() map[ast.NodeRef]*Field { return e.referencedFields }
func (e *ExpressionAnalysis) Predicates() []*ast.ComparisonExpression  { return e.predicates }
func (e *ExpressionAnalysis) RequireRelation() bool                    { return e.requireRelation }
