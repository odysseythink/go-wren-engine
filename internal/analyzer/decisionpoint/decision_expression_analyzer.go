package decisionpoint

import (
	"fmt"
	"github.com/wren-engine/wren/internal/parser/ast"
)

const (
	IncludeFunctionCall          = "includeFunctionCall"
	IncludeMathematicalOperation = "includeMathematicalOperation"
)

var DefaultAnalysis = DecisionExpressionAnalysis{IncludeFunctionCall: false, IncludeMathematicalOperation: false}

type DecisionExpressionAnalysis struct {
	IncludeFunctionCall          bool
	IncludeMathematicalOperation bool
}

func (d DecisionExpressionAnalysis) ToMap() map[string]string {
	return map[string]string{
		IncludeFunctionCall:          fmt.Sprintf("%t", d.IncludeFunctionCall),
		IncludeMathematicalOperation: fmt.Sprintf("%t", d.IncludeMathematicalOperation),
	}
}

func AnalyzeDecisionExpression(expr ast.Expression) DecisionExpressionAnalysis {
	v := &decExprVisitor{}
	v.walk(expr)
	return DecisionExpressionAnalysis{IncludeFunctionCall: v.includeFunctionCall, IncludeMathematicalOperation: v.includeMathematicalOperation}
}

type decExprVisitor struct {
	includeFunctionCall          bool
	includeMathematicalOperation bool
}

func (v *decExprVisitor) walk(node ast.Node) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.FunctionCall:
		v.includeFunctionCall = true
		for _, child := range node.GetChildren() {
			v.walk(child)
		}
	case *ast.ArithmeticBinaryExpression:
		v.includeMathematicalOperation = true
		v.walk(n.Left)
		v.walk(n.Right)
	case *ast.ComparisonExpression:
		v.includeMathematicalOperation = true
		v.walk(n.Left)
		v.walk(n.Right)
	default:
		for _, child := range node.GetChildren() {
			v.walk(child)
		}
	}
}
