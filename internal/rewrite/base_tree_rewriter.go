package rewrite

import "github.com/wren-engine/wren/internal/parser/ast"

// RewriteHook inspects a node. Returning (replacement, true) substitutes the
// node and stops descent; returning (_, false) makes RewriteNode rebuild the
// node from its rewritten children. Mirrors a Java BaseRewriter visitX override.
type RewriteHook func(node ast.Node) (ast.Node, bool)

// RewriteNode returns node with hook applied throughout the tree. Every
// container node is rebuilt from RewriteNode-recursed children; leaves and
// unknown nodes are returned unchanged (mirrors Java BaseTreeRewriter.visitNode).
func RewriteNode(node ast.Node, hook RewriteHook) ast.Node {
	if node == nil {
		return nil
	}
	if r, ok := hook(node); ok {
		return r
	}
	switch n := node.(type) {

	case *ast.Query:
		out := *n
		if n.With != nil {
			out.With = RewriteNode(n.With, hook).(*ast.With)
		}
		if n.Body != nil {
			out.Body = RewriteNode(n.Body, hook).(ast.QueryBody)
		}
		out.OrderBy = rewriteSortItems(n.OrderBy, hook)
		out.Limit = rewriteExpr(n.Limit, hook)
		out.Offset = rewriteExpr(n.Offset, hook)
		return &out

	case *ast.QuerySpecification:
		out := *n
		if n.Select != nil {
			out.Select = RewriteNode(n.Select, hook).(*ast.Select)
		}
		if n.From != nil {
			out.From = RewriteNode(n.From, hook).(ast.Relation)
		}
		out.Where = rewriteExpr(n.Where, hook)
		if n.GroupBy != nil {
			out.GroupBy = RewriteNode(n.GroupBy, hook).(*ast.GroupBy)
		}
		out.Having = rewriteExpr(n.Having, hook)
		out.OrderBy = rewriteSortItems(n.OrderBy, hook)
		out.Limit = rewriteExpr(n.Limit, hook)
		out.Offset = rewriteExpr(n.Offset, hook)
		return &out

	case *ast.SetOperation:
		out := *n
		out.Relations = make([]ast.Relation, len(n.Relations))
		for i, r := range n.Relations {
			out.Relations[i] = RewriteNode(r, hook).(ast.Relation)
		}
		return &out

	case *ast.Select:
		out := *n
		out.SelectItems = make([]ast.SelectItem, len(n.SelectItems))
		for i, item := range n.SelectItems {
			out.SelectItems[i] = RewriteNode(item, hook).(ast.SelectItem)
		}
		return &out

	case *ast.SingleColumn:
		out := *n
		out.Expression = rewriteExpr(n.Expression, hook)
		// Alias is intentionally NOT rewritten (mirrors Java BaseTreeRewriter.visitSingleColumn)
		return &out

	case *ast.AllColumns:
		return n // leaf

	case *ast.With:
		out := *n
		out.Queries = make([]ast.WithQuery, len(n.Queries))
		for i, q := range n.Queries {
			wq := RewriteNode(&q, hook).(*ast.WithQuery)
			out.Queries[i] = *wq
		}
		return &out

	case *ast.WithQuery:
		out := *n
		if n.Query != nil {
			out.Query = RewriteNode(n.Query, hook).(ast.Statement)
		}
		return &out

	case *ast.GroupBy:
		out := *n
		out.Expressions = rewriteExprs(n.Expressions, hook)
		return &out

	case *ast.SortItem:
		out := *n
		out.SortKey = rewriteExpr(n.SortKey, hook)
		return &out

	case *ast.Window:
		out := *n
		out.PartitionBy = rewriteExprs(n.PartitionBy, hook)
		out.OrderBy = rewriteSortItems(n.OrderBy, hook)
		if n.Frame != nil {
			out.Frame = RewriteNode(n.Frame, hook).(*ast.WindowFrame)
		}
		return &out

	case *ast.WindowFrame:
		out := *n
		out.Start = ast.FrameBound{
			Type:  n.Start.Type,
			Value: rewriteExpr(n.Start.Value, hook),
		}
		out.End = ast.FrameBound{
			Type:  n.End.Type,
			Value: rewriteExpr(n.End.Value, hook),
		}
		return &out

	// --- Relations ---
	case *ast.Table:
		return n // leaf relation

	case *ast.AliasedRelation:
		out := *n
		out.Relation = RewriteNode(n.Relation, hook).(ast.Relation)
		return &out

	case *ast.Join:
		out := *n
		out.Left = RewriteNode(n.Left, hook).(ast.Relation)
		out.Right = RewriteNode(n.Right, hook).(ast.Relation)
		if n.Criteria != nil {
			out.Criteria = RewriteNode(n.Criteria, hook).(ast.JoinCriteria)
		}
		return &out

	case *ast.JoinOn:
		out := *n
		out.Expression = rewriteExpr(n.Expression, hook)
		return &out

	case *ast.JoinUsing:
		return n // leaf (column names are identifiers, not rewritten)

	case *ast.NaturalJoin:
		return n // leaf

	case *ast.TableSubquery:
		out := *n
		out.Query = RewriteNode(n.Query, hook).(ast.Statement)
		return &out

	case *ast.Unnest:
		out := *n
		out.Expressions = rewriteExprs(n.Expressions, hook)
		return &out

	case *ast.Values:
		out := *n
		out.Rows = make([][]ast.Expression, len(n.Rows))
		for i, row := range n.Rows {
			out.Rows[i] = rewriteExprs(row, hook)
		}
		return &out

	case *ast.Lateral:
		out := *n
		out.Query = RewriteNode(n.Query, hook).(ast.Statement)
		return &out

	case *ast.SampledRelation:
		out := *n
		out.Relation = RewriteNode(n.Relation, hook).(ast.Relation)
		out.SamplePercentage = rewriteExpr(n.SamplePercentage, hook)
		return &out

	case *ast.FunctionRelation:
		out := *n
		out.Arguments = rewriteExprs(n.Arguments, hook)
		return &out

	// --- Expressions ---
	case *ast.ComparisonExpression:
		out := *n
		out.Left = rewriteExpr(n.Left, hook)
		out.Right = rewriteExpr(n.Right, hook)
		return &out

	case *ast.ArithmeticBinaryExpression:
		out := *n
		out.Left = rewriteExpr(n.Left, hook)
		out.Right = rewriteExpr(n.Right, hook)
		return &out

	case *ast.LogicalBinaryExpression:
		out := *n
		out.Left = rewriteExpr(n.Left, hook)
		out.Right = rewriteExpr(n.Right, hook)
		return &out

	case *ast.LogicalExpression:
		out := *n
		out.Terms = rewriteExprs(n.Terms, hook)
		return &out

	case *ast.NotExpression:
		out := *n
		out.Value = rewriteExpr(n.Value, hook)
		return &out

	case *ast.FunctionCall:
		out := *n
		out.Arguments = rewriteExprs(n.Arguments, hook)
		out.OrderBy = rewriteSortItems(n.OrderBy, hook)
		out.Filter = rewriteExpr(n.Filter, hook)
		if n.Window != nil {
			out.Window = RewriteNode(n.Window, hook).(*ast.Window)
		}
		return &out

	case *ast.Cast:
		out := *n
		out.Expression = rewriteExpr(n.Expression, hook)
		return &out

	case *ast.CoalesceExpression:
		out := *n
		out.Operands = rewriteExprs(n.Operands, hook)
		return &out

	case *ast.InPredicate:
		out := *n
		out.Value = rewriteExpr(n.Value, hook)
		out.ValueList = rewriteExpr(n.ValueList, hook)
		return &out

	case *ast.InListExpression:
		out := *n
		out.Values = rewriteExprs(n.Values, hook)
		return &out

	case *ast.BetweenPredicate:
		out := *n
		out.Value = rewriteExpr(n.Value, hook)
		out.Min = rewriteExpr(n.Min, hook)
		out.Max = rewriteExpr(n.Max, hook)
		return &out

	case *ast.SubqueryExpression:
		out := *n
		out.Query = RewriteNode(n.Query, hook).(ast.Statement)
		return &out

	case *ast.AtTimeZone:
		out := *n
		out.Value = rewriteExpr(n.Value, hook)
		out.TimeZone = rewriteExpr(n.TimeZone, hook)
		return &out

	case *ast.IsNullPredicate:
		out := *n
		out.Value = rewriteExpr(n.Value, hook)
		return &out

	case *ast.LikePredicate:
		out := *n
		out.Value = rewriteExpr(n.Value, hook)
		out.Pattern = rewriteExpr(n.Pattern, hook)
		out.Escape = rewriteExpr(n.Escape, hook)
		return &out

	case *ast.SearchedCaseExpression:
		out := *n
		out.WhenClauses = make([]ast.WhenClause, len(n.WhenClauses))
		for i, wc := range n.WhenClauses {
			out.WhenClauses[i] = *RewriteNode(&wc, hook).(*ast.WhenClause)
		}
		out.DefaultValue = rewriteExpr(n.DefaultValue, hook)
		return &out

	case *ast.SimpleCaseExpression:
		out := *n
		out.Operand = rewriteExpr(n.Operand, hook)
		out.WhenClauses = make([]ast.WhenClause, len(n.WhenClauses))
		for i, wc := range n.WhenClauses {
			out.WhenClauses[i] = *RewriteNode(&wc, hook).(*ast.WhenClause)
		}
		out.DefaultValue = rewriteExpr(n.DefaultValue, hook)
		return &out

	case *ast.WhenClause:
		out := *n
		out.Operand = rewriteExpr(n.Operand, hook)
		out.Result = rewriteExpr(n.Result, hook)
		return &out

	case *ast.IfExpression:
		out := *n
		out.Condition = rewriteExpr(n.Condition, hook)
		out.TrueValue = rewriteExpr(n.TrueValue, hook)
		out.FalseValue = rewriteExpr(n.FalseValue, hook)
		return &out

	case *ast.NullIfExpression:
		out := *n
		out.First = rewriteExpr(n.First, hook)
		out.Second = rewriteExpr(n.Second, hook)
		return &out

	case *ast.ExtractExpression:
		out := *n
		out.Expression = rewriteExpr(n.Expression, hook)
		return &out

	case *ast.SubscriptExpression:
		out := *n
		out.Base = rewriteExpr(n.Base, hook)
		out.Index = rewriteExpr(n.Index, hook)
		return &out

	case *ast.Row:
		out := *n
		out.Items = rewriteExprs(n.Items, hook)
		return &out

	case *ast.ExistsPredicate:
		out := *n
		out.Subquery = RewriteNode(n.Subquery, hook).(ast.Statement)
		return &out

	case *ast.QuantifiedComparison:
		out := *n
		out.Value = rewriteExpr(n.Value, hook)
		out.Subquery = rewriteExpr(n.Subquery, hook)
		return &out

	case *ast.DereferenceExpression:
		out := *n
		out.Base = rewriteExpr(n.Base, hook)
		return &out

	// --- Literals and leaves ---
	case *ast.Identifier:
		return n
	case *ast.StarExpression:
		return n
	case *ast.LongLiteral:
		return n
	case *ast.DoubleLiteral:
		return n
	case *ast.StringLiteral:
		return n
	case *ast.BooleanLiteral:
		return n
	case *ast.NullLiteral:
		return n
	case *ast.GenericLiteral:
		return n
	case *ast.IntervalLiteral:
		return n

	// --- Types ---
	case *ast.DataType:
		return n
	case *ast.TypeParameter:
		return n
	case *ast.NumericParameter:
		return n
	case *ast.ColumnDefinition:
		return n

	default:
		return node
	}
}

// rewriteExpr is a nil-safe RewriteNode for expressions.
func rewriteExpr(e ast.Expression, hook RewriteHook) ast.Expression {
	if e == nil {
		return nil
	}
	return RewriteNode(e, hook).(ast.Expression)
}

// rewriteExprs rewrites every expression in a slice (returns a fresh slice).
func rewriteExprs(es []ast.Expression, hook RewriteHook) []ast.Expression {
	if es == nil {
		return nil
	}
	out := make([]ast.Expression, len(es))
	for i, e := range es {
		out[i] = rewriteExpr(e, hook)
	}
	return out
}

// rewriteSortItems rewrites the sort key of every SortItem.
func rewriteSortItems(items []ast.SortItem, hook RewriteHook) []ast.SortItem {
	if items == nil {
		return nil
	}
	out := make([]ast.SortItem, len(items))
	for i, it := range items {
		out[i] = it
		out[i].SortKey = rewriteExpr(it.SortKey, hook)
	}
	return out
}
