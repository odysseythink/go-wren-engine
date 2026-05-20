package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

type FilterType string

const (
	FilterTypeAnd  FilterType = "AND"
	FilterTypeOr   FilterType = "OR"
	FilterTypeExpr FilterType = "EXPR"
)

type FilterAnalysis struct {
	Type         FilterType
	NodeLocation *ast.NodeLocation
	Left         *FilterAnalysis
	Right        *FilterAnalysis
	Node         string
	ExprSources  []ExprSource
}

func NewLogicalAnalysis(typ FilterType, left, right *FilterAnalysis, loc *ast.NodeLocation) *FilterAnalysis {
	return &FilterAnalysis{Type: typ, Left: left, Right: right, NodeLocation: loc}
}
func NewExpressionAnalysis(node string, loc *ast.NodeLocation, exprSources []ExprSource) *FilterAnalysis {
	return &FilterAnalysis{Type: FilterTypeExpr, Node: node, NodeLocation: loc, ExprSources: exprSources}
}
