package analyzer

import (
	"strings"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// AnalyzeExpression runs the expression visitor. Mirrors ExpressionAnalyzer.analyze.
func AnalyzeExpression(scope *Scope, expr ast.Expression, ctx *base.SessionContext, wrenMDL *mdl.WrenMDL, analysis *Analysis) *ExpressionAnalysis {
	v := &exprVisitor{
		scope:           scope,
		ctx:             ctx,
		wrenMDL:         wrenMDL,
		analysis:        analysis,
		referenceFields: map[ast.NodeRef]*Field{},
	}
	v.process(expr)
	return newExpressionAnalysis(v.referenceFields, v.predicates, v.requireRelation)
}

type exprVisitor struct {
	scope           *Scope
	ctx             *base.SessionContext
	wrenMDL         *mdl.WrenMDL
	analysis        *Analysis
	referenceFields map[ast.NodeRef]*Field
	predicates      []*ast.ComparisonExpression
	requireRelation bool
}

func (v *exprVisitor) process(expr ast.Expression) {
	switch n := expr.(type) {
	case *ast.DereferenceExpression:
		qn := ast.GetQualifiedName(n)
		if qn != nil {
			for _, field := range v.scope.RelationType().Fields() {
				if field.CanResolve(qn) {
					v.referenceFields[ast.NodeRef{Node: n}] = field
					return
				}
			}
		}
		v.process(n.Base)

	case *ast.Identifier:
		qn := &ast.QualifiedName{Parts: []string{n.Value}, OriginalParts: []ast.Identifier{*n}}
		for _, field := range v.scope.RelationType().Fields() {
			if field.CanResolve(qn) {
				v.referenceFields[ast.NodeRef{Node: n}] = field
				return
			}
		}

	case *ast.ComparisonExpression:
		v.process(n.Left)
		v.process(n.Right)
		v.predicates = append(v.predicates, n)

	case *ast.FunctionCall:
		if strings.EqualFold(n.Name.Last(), "count") && len(n.Arguments) == 0 {
			v.requireRelation = true
			return
		}
		for _, arg := range n.Arguments {
			v.process(arg)
		}
		for _, si := range n.OrderBy {
			v.process(si.SortKey)
		}
		if n.Filter != nil {
			v.process(n.Filter)
		}
		if n.Window != nil {
			v.processWindow(n.Window)
		}

	case *ast.SubqueryExpression:
		if _, err := Analyze(v.analysis, n.Query, v.ctx, v.wrenMDL); err != nil {
			// Java throws unchecked; we silently continue to match behavior
		}

	case *ast.ExistsPredicate:
		if _, err := Analyze(v.analysis, n.Subquery, v.ctx, v.wrenMDL); err != nil {
			// silently continue
		}

	case *ast.InPredicate:
		v.process(n.Value)
		v.process(n.ValueList)

	case *ast.InListExpression:
		for _, val := range n.Values {
			v.process(val)
		}

	case *ast.BetweenPredicate:
		v.process(n.Value)
		v.process(n.Min)
		v.process(n.Max)

	case *ast.LogicalExpression:
		for _, term := range n.Terms {
			v.process(term)
		}

	case *ast.LogicalBinaryExpression:
		v.process(n.Left)
		v.process(n.Right)

	case *ast.NotExpression:
		v.process(n.Value)

	case *ast.ArithmeticBinaryExpression:
		v.process(n.Left)
		v.process(n.Right)

	case *ast.Cast:
		v.process(n.Expression)

	case *ast.CoalesceExpression:
		for _, op := range n.Operands {
			v.process(op)
		}

	case *ast.AtTimeZone:
		v.process(n.Value)
		v.process(n.TimeZone)

	case *ast.IsNullPredicate:
		v.process(n.Value)

	case *ast.LikePredicate:
		v.process(n.Value)
		v.process(n.Pattern)
		if n.Escape != nil {
			v.process(n.Escape)
		}

	case *ast.SearchedCaseExpression:
		for i := range n.WhenClauses {
			v.process(n.WhenClauses[i].Operand)
			v.process(n.WhenClauses[i].Result)
		}
		if n.DefaultValue != nil {
			v.process(n.DefaultValue)
		}

	case *ast.SimpleCaseExpression:
		v.process(n.Operand)
		for i := range n.WhenClauses {
			v.process(n.WhenClauses[i].Operand)
			v.process(n.WhenClauses[i].Result)
		}
		if n.DefaultValue != nil {
			v.process(n.DefaultValue)
		}

	case *ast.IfExpression:
		v.process(n.Condition)
		v.process(n.TrueValue)
		if n.FalseValue != nil {
			v.process(n.FalseValue)
		}

	case *ast.NullIfExpression:
		v.process(n.First)
		v.process(n.Second)

	case *ast.ExtractExpression:
		v.process(n.Expression)

	case *ast.SubscriptExpression:
		v.process(n.Base)
		v.process(n.Index)

	case *ast.Row:
		for _, item := range n.Items {
			v.process(item)
		}

	case *ast.QuantifiedComparison:
		v.process(n.Value)
		v.process(n.Subquery)

	// Leaves — no children to process
	case *ast.LongLiteral, *ast.DoubleLiteral, *ast.StringLiteral,
		*ast.BooleanLiteral, *ast.NullLiteral, *ast.GenericLiteral,
		*ast.IntervalLiteral, *ast.StarExpression:
		// nothing
	}
}

func (v *exprVisitor) processWindow(w *ast.Window) {
	for _, e := range w.PartitionBy {
		v.process(e)
	}
	for _, si := range w.OrderBy {
		v.process(si.SortKey)
	}
	if w.Frame != nil {
		if w.Frame.Start.Value != nil {
			v.process(w.Frame.Start.Value)
		}
		if w.Frame.End.Value != nil {
			v.process(w.Frame.End.Value)
		}
	}
}
