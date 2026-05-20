package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

func ExpressionLocationAnalyze(node ast.Node) *ast.NodeLocation {
	if node == nil {
		return nil
	}
	v := &exprLocVisitor{}
	v.walk(node)
	return v.loc
}

type exprLocVisitor struct {
	loc *ast.NodeLocation
}

func (v *exprLocVisitor) walk(node ast.Node) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.ComparisonExpression:
		v.walk(n.Left)
	case *ast.ArithmeticBinaryExpression:
		v.walk(n.Left)
	default:
		v.loc = node.GetLocation()
	}
}
