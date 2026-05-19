package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// EnumRewrite replaces enum dereferences (enumName.valueName) with string literals.
type EnumRewrite struct{}

func (r *EnumRewrite) Apply(stmt ast.Statement, ctx *analyzer.SessionContext, analyzedMDL *mdl.AnalyzedMDL) ast.Statement {
	return rewriteEnums(stmt, analyzedMDL.WrenMDL()).(ast.Statement)
}

func rewriteEnums(node ast.Node, wrenMDL *mdl.WrenMDL) ast.Node {
	if node == nil {
		return nil
	}

	switch n := node.(type) {
	case *ast.DereferenceExpression:
		qn := ast.GetQualifiedName(n)
		if qn != nil && len(qn.Parts) == 2 {
			enumName := qn.Parts[0]
			valueName := qn.Parts[1]
			if enumDef, found := wrenMDL.GetEnumDefinition(enumName); found {
				if value := enumDef.ValueOf(valueName); value != nil {
					val := value.GetValue()
					if val == "" {
						val = valueName
					}
					return &ast.StringLiteral{Value: val}
				}
				panic(fmt.Sprintf("Enum value '%s' not found in enum '%s'", valueName, enumName))
			}
		}
		// Not an enum, recurse into children
		n.Base = rewriteEnums(n.Base, wrenMDL).(ast.Expression)
		return n
	case *ast.Query:
		if n.With != nil {
			n.With = rewriteEnums(n.With, wrenMDL).(*ast.With)
		}
		if n.Body != nil {
			n.Body = rewriteEnums(n.Body, wrenMDL).(ast.QueryBody)
		}
		for i := range n.OrderBy {
			n.OrderBy[i].SortKey = rewriteEnums(n.OrderBy[i].SortKey, wrenMDL).(ast.Expression)
		}
		if n.Limit != nil {
			n.Limit = rewriteEnums(n.Limit, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.QuerySpecification:
		if n.Select != nil {
			for i, item := range n.Select.SelectItems {
				n.Select.SelectItems[i] = rewriteSelectItem(item, wrenMDL)
			}
		}
		if n.From != nil {
			n.From = rewriteEnums(n.From, wrenMDL).(ast.Relation)
		}
		if n.Where != nil {
			n.Where = rewriteEnums(n.Where, wrenMDL).(ast.Expression)
		}
		if n.GroupBy != nil {
			for i, expr := range n.GroupBy.Expressions {
				n.GroupBy.Expressions[i] = rewriteEnums(expr, wrenMDL).(ast.Expression)
			}
		}
		if n.Having != nil {
			n.Having = rewriteEnums(n.Having, wrenMDL).(ast.Expression)
		}
		for i := range n.OrderBy {
			n.OrderBy[i].SortKey = rewriteEnums(n.OrderBy[i].SortKey, wrenMDL).(ast.Expression)
		}
		if n.Limit != nil {
			n.Limit = rewriteEnums(n.Limit, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.With:
		for i := range n.Queries {
			n.Queries[i].Query = rewriteEnums(n.Queries[i].Query, wrenMDL).(ast.Statement)
		}
		return n
	case *ast.Table:
		return n
	case *ast.Join:
		n.Left = rewriteEnums(n.Left, wrenMDL).(ast.Relation)
		n.Right = rewriteEnums(n.Right, wrenMDL).(ast.Relation)
		if n.Criteria != nil {
			switch c := n.Criteria.(type) {
			case *ast.JoinOn:
				c.Expression = rewriteEnums(c.Expression, wrenMDL).(ast.Expression)
			}
		}
		return n
	case *ast.AliasedRelation:
		n.Relation = rewriteEnums(n.Relation, wrenMDL).(ast.Relation)
		return n
	case *ast.TableSubquery:
		n.Query = rewriteEnums(n.Query, wrenMDL).(ast.Statement)
		return n
	case *ast.ComparisonExpression:
		n.Left = rewriteEnums(n.Left, wrenMDL).(ast.Expression)
		n.Right = rewriteEnums(n.Right, wrenMDL).(ast.Expression)
		return n
	case *ast.LogicalBinaryExpression:
		n.Left = rewriteEnums(n.Left, wrenMDL).(ast.Expression)
		n.Right = rewriteEnums(n.Right, wrenMDL).(ast.Expression)
		return n
	case *ast.NotExpression:
		n.Value = rewriteEnums(n.Value, wrenMDL).(ast.Expression)
		return n
	case *ast.ArithmeticBinaryExpression:
		n.Left = rewriteEnums(n.Left, wrenMDL).(ast.Expression)
		n.Right = rewriteEnums(n.Right, wrenMDL).(ast.Expression)
		return n
	case *ast.FunctionCall:
		for i, arg := range n.Arguments {
			n.Arguments[i] = rewriteEnums(arg, wrenMDL).(ast.Expression)
		}
		if n.Filter != nil {
			n.Filter = rewriteEnums(n.Filter, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.Cast:
		n.Expression = rewriteEnums(n.Expression, wrenMDL).(ast.Expression)
		return n
	case *ast.InPredicate:
		n.Value = rewriteEnums(n.Value, wrenMDL).(ast.Expression)
		n.ValueList = rewriteEnums(n.ValueList, wrenMDL).(ast.Expression)
		return n
	case *ast.InListExpression:
		for i, v := range n.Values {
			n.Values[i] = rewriteEnums(v, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.BetweenPredicate:
		n.Value = rewriteEnums(n.Value, wrenMDL).(ast.Expression)
		n.Min = rewriteEnums(n.Min, wrenMDL).(ast.Expression)
		n.Max = rewriteEnums(n.Max, wrenMDL).(ast.Expression)
		return n
	case *ast.LikePredicate:
		n.Value = rewriteEnums(n.Value, wrenMDL).(ast.Expression)
		n.Pattern = rewriteEnums(n.Pattern, wrenMDL).(ast.Expression)
		if n.Escape != nil {
			n.Escape = rewriteEnums(n.Escape, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.IsNullPredicate:
		n.Value = rewriteEnums(n.Value, wrenMDL).(ast.Expression)
		return n
	case *ast.SubqueryExpression:
		n.Query = rewriteEnums(n.Query, wrenMDL).(ast.Statement)
		return n
	case *ast.AtTimeZone:
		n.Value = rewriteEnums(n.Value, wrenMDL).(ast.Expression)
		n.TimeZone = rewriteEnums(n.TimeZone, wrenMDL).(ast.Expression)
		return n
	case *ast.CoalesceExpression:
		for i, o := range n.Operands {
			n.Operands[i] = rewriteEnums(o, wrenMDL).(ast.Expression)
		}
		return n
	default:
		// For leaf nodes (Identifier, literals, etc.), return as-is
		return node
	}
}

func rewriteSelectItem(item ast.SelectItem, wrenMDL *mdl.WrenMDL) ast.SelectItem {
	switch s := item.(type) {
	case *ast.SingleColumn:
		s.Expression = rewriteEnums(s.Expression, wrenMDL).(ast.Expression)
		return s
	case *ast.AllColumns:
		return s
	}
	return item
}
