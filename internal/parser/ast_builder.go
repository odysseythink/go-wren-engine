package parser

import (
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/wren-engine/wren/internal/parser/ast"
	generated "github.com/wren-engine/wren/internal/parser/generated"
)

// AstBuilder walks the ANTLR4 parse tree and produces Go AST nodes.
type AstBuilder struct {
	*generated.BaseSqlBaseVisitor
}

// visit is a helper that safely delegates to Accept.
func (b *AstBuilder) visit(tree antlr.ParseTree) interface{} {
	if tree == nil {
		return nil
	}
	return tree.Accept(b)
}

func (b *AstBuilder) visitStatement(tree antlr.ParseTree) ast.Statement {
	if tree == nil {
		return nil
	}
	r := tree.Accept(b)
	if r == nil {
		return nil
	}
	return r.(ast.Statement)
}

func (b *AstBuilder) visitExpression(tree antlr.ParseTree) ast.Expression {
	if tree == nil {
		return nil
	}
	r := tree.Accept(b)
	if r == nil {
		return nil
	}
	return r.(ast.Expression)
}

func (b *AstBuilder) visitRelation(tree antlr.ParseTree) ast.Relation {
	if tree == nil {
		return nil
	}
	r := tree.Accept(b)
	if r == nil {
		return nil
	}
	return r.(ast.Relation)
}

func (b *AstBuilder) visitIdentifier(tree antlr.ParseTree) *ast.Identifier {
	if tree == nil {
		return nil
	}
	r := tree.Accept(b)
	if r == nil {
		return nil
	}
	return r.(*ast.Identifier)
}

func (b *AstBuilder) visitSortItem(tree antlr.ParseTree) *ast.SortItem {
	if tree == nil {
		return nil
	}
	r := tree.Accept(b)
	if r == nil {
		return nil
	}
	return r.(*ast.SortItem)
}

func (b *AstBuilder) visitSelectItem(tree antlr.ParseTree) ast.SelectItem {
	if tree == nil {
		return nil
	}
	r := tree.Accept(b)
	if r == nil {
		return nil
	}
	return r.(ast.SelectItem)
}

func (b *AstBuilder) visitJoinCriteria(tree antlr.ParseTree) ast.JoinCriteria {
	if tree == nil {
		return nil
	}
	r := tree.Accept(b)
	if r == nil {
		return nil
	}
	return r.(ast.JoinCriteria)
}

func (b *AstBuilder) visitDataType(tree antlr.ParseTree) ast.DataType {
	if tree == nil {
		return ast.DataType{}
	}
	r := tree.Accept(b)
	if r == nil {
		return ast.DataType{}
	}
	return r.(ast.DataType)
}

func (b *AstBuilder) visitWithQuery(tree antlr.ParseTree) *ast.WithQuery {
	if tree == nil {
		return nil
	}
	r := tree.Accept(b)
	if r == nil {
		return nil
	}
	return r.(*ast.WithQuery)
}

func (b *AstBuilder) visitWindow(tree antlr.ParseTree) *ast.Window {
	if tree == nil {
		return nil
	}
	r := tree.Accept(b)
	if r == nil {
		return nil
	}
	return r.(*ast.Window)
}

// --------------------------------------------------------------------------
// Statements / Queries
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitSingleStatement(ctx *generated.SingleStatementContext) interface{} {
	return b.visitStatement(ctx.Statement())
}

func (b *AstBuilder) VisitStatementDefault(ctx *generated.StatementDefaultContext) interface{} {
	return b.visitStatement(ctx.Query())
}

func (b *AstBuilder) VisitQuery(ctx *generated.QueryContext) interface{} {
	query := &ast.Query{}
	if ctx.With() != nil {
		query.With = b.visit(ctx.With()).(*ast.With)
	}
	body := b.visitStatement(ctx.QueryNoWith())
	if qs, ok := body.(*ast.QuerySpecification); ok {
		query.Body = qs
		query.OrderBy = qs.OrderBy
		query.Limit = qs.Limit
		query.Offset = qs.Offset
		qs.OrderBy = nil
		qs.Limit = nil
		qs.Offset = nil
	} else {
		query.Body = body.(ast.QueryBody)
	}
	return query
}

func (b *AstBuilder) VisitQueryNoWith(ctx *generated.QueryNoWithContext) interface{} {
	qs := b.visitStatement(ctx.QueryTerm()).(*ast.QuerySpecification)
	for _, si := range ctx.AllSortItem() {
		qs.OrderBy = append(qs.OrderBy, *b.visitSortItem(si))
	}
	if ctx.LIMIT() != nil {
		if ctx.LimitRowCount() != nil && ctx.LimitRowCount().RowCount() != nil {
			qs.Limit = b.visitExpression(ctx.LimitRowCount().RowCount())
		} else if len(ctx.AllRowCount()) > 0 {
			qs.Limit = b.visitExpression(ctx.RowCount(0))
		}
	}
	if ctx.OFFSET() != nil && len(ctx.AllRowCount()) > 0 {
		if ctx.LIMIT() != nil {
			qs.Offset = b.visitExpression(ctx.RowCount(len(ctx.AllRowCount()) - 1))
		} else {
			qs.Offset = b.visitExpression(ctx.RowCount(0))
		}
	}
	return qs
}

func (b *AstBuilder) VisitQueryTermDefault(ctx *generated.QueryTermDefaultContext) interface{} {
	return b.visit(ctx.QueryPrimary())
}

func (b *AstBuilder) VisitQueryPrimaryDefault(ctx *generated.QueryPrimaryDefaultContext) interface{} {
	return b.visit(ctx.QuerySpecification())
}

func (b *AstBuilder) VisitSubquery(ctx *generated.SubqueryContext) interface{} {
	return b.visit(ctx.QueryNoWith())
}

func (b *AstBuilder) VisitQuerySpecification(ctx *generated.QuerySpecificationContext) interface{} {
	qs := &ast.QuerySpecification{}
	selectNode := &ast.Select{}
	if ctx.SetQuantifier() != nil {
		selectNode.Distinct = ctx.SetQuantifier().DISTINCT() != nil
	}
	for _, item := range ctx.AllSelectItem() {
		selectNode.SelectItems = append(selectNode.SelectItems, b.visitSelectItem(item))
	}
	qs.Select = selectNode
	if ctx.FROM() != nil && len(ctx.AllRelation()) > 0 {
		if len(ctx.AllRelation()) == 1 {
			qs.From = b.visitRelation(ctx.Relation(0))
		} else {
			join := &ast.Join{JoinType: ast.JoinTypeImplicit, Left: b.visitRelation(ctx.Relation(0)), Right: b.visitRelation(ctx.Relation(1))}
			for i := 2; i < len(ctx.AllRelation()); i++ {
				join = &ast.Join{JoinType: ast.JoinTypeImplicit, Left: join, Right: b.visitRelation(ctx.Relation(i))}
			}
			qs.From = join
		}
	}
	if ctx.GetWhere() != nil {
		qs.Where = b.visitExpression(ctx.GetWhere())
	}
	if ctx.GROUP() != nil && ctx.GroupBy() != nil {
		qs.GroupBy = b.visit(ctx.GroupBy()).(*ast.GroupBy)
	}
	if ctx.GetHaving() != nil {
		qs.Having = b.visitExpression(ctx.GetHaving())
	}
	return qs
}

// --------------------------------------------------------------------------
// WITH / CTEs
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitWith(ctx *generated.WithContext) interface{} {
	with := &ast.With{Recursive: ctx.RECURSIVE() != nil}
	for _, nq := range ctx.AllNamedQuery() {
		with.Queries = append(with.Queries, *b.visitWithQuery(nq))
	}
	return with
}

func (b *AstBuilder) VisitNamedQuery(ctx *generated.NamedQueryContext) interface{} {
	wq := &ast.WithQuery{Name: b.visitIdentifier(ctx.GetName()), Query: b.visitStatement(ctx.Query())}
	if ctx.ColumnAliases() != nil {
		for _, id := range ctx.ColumnAliases().AllIdentifier() {
			wq.ColumnNames = append(wq.ColumnNames, *b.visitIdentifier(id))
		}
	}
	return wq
}

// --------------------------------------------------------------------------
// SELECT items
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitSelectSingle(ctx *generated.SelectSingleContext) interface{} {
	sc := &ast.SingleColumn{Expression: b.visitExpression(ctx.Expression())}
	if ctx.AS() != nil || ctx.Identifier() != nil {
		sc.Alias = b.visitIdentifier(ctx.Identifier())
	}
	return sc
}

func (b *AstBuilder) VisitSelectAll(ctx *generated.SelectAllContext) interface{} {
	ac := &ast.AllColumns{}
	if ctx.PrimaryExpression() != nil {
		expr := b.visitExpression(ctx.PrimaryExpression())
		qn := ast.GetQualifiedName(expr)
		if qn != nil {
			ac.QualifiedName = qn
		}
	}
	return ac
}

// --------------------------------------------------------------------------
// Relations / FROM
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitRelationDefault(ctx *generated.RelationDefaultContext) interface{} {
	return b.visit(ctx.SampledRelation())
}

func (b *AstBuilder) VisitJoinRelation(ctx *generated.JoinRelationContext) interface{} {
	join := &ast.Join{}
	join.Left = b.visitRelation(ctx.GetLeft())
	if ctx.GetRight() != nil {
		join.Right = b.visit(ctx.GetRight()).(ast.Relation)
	} else if ctx.GetRightRelation() != nil {
		join.Right = b.visitRelation(ctx.GetRightRelation())
	}
	if ctx.CROSS() != nil {
		join.JoinType = ast.JoinTypeCross
	} else if ctx.NATURAL() != nil {
		join.JoinType = ast.JoinTypeInner
	} else if ctx.JoinType() != nil {
		join.JoinType = b.VisitJoinType(ctx.JoinType().(*generated.JoinTypeContext)).(ast.JoinType)
	} else {
		join.JoinType = ast.JoinTypeInner
	}
	if ctx.JoinCriteria() != nil {
		join.Criteria = b.visitJoinCriteria(ctx.JoinCriteria())
	}
	return join
}

func (b *AstBuilder) VisitJoinType(ctx *generated.JoinTypeContext) interface{} {
	if ctx.LEFT() != nil {
		return ast.JoinTypeLeft
	}
	if ctx.RIGHT() != nil {
		return ast.JoinTypeRight
	}
	if ctx.FULL() != nil {
		return ast.JoinTypeFull
	}
	return ast.JoinTypeInner
}

func (b *AstBuilder) VisitJoinCriteria(ctx *generated.JoinCriteriaContext) interface{} {
	if ctx.ON() != nil {
		return &ast.JoinOn{Expression: b.visitExpression(ctx.BooleanExpression())}
	}
	if ctx.USING() != nil {
		using := &ast.JoinUsing{}
		for _, id := range ctx.AllIdentifier() {
			using.Columns = append(using.Columns, *b.visitIdentifier(id))
		}
		return using
	}
	return &ast.NaturalJoin{}
}

func (b *AstBuilder) VisitAliasedRelation(ctx *generated.AliasedRelationContext) interface{} {
	rel := b.visit(ctx.RelationPrimary()).(ast.Relation)
	if ctx.Identifier() == nil && ctx.ColumnAliases() == nil {
		return rel
	}
	alias := b.visitIdentifier(ctx.Identifier())
	ar := &ast.AliasedRelation{Relation: rel, Alias: alias}
	if ctx.ColumnAliases() != nil {
		for _, id := range ctx.ColumnAliases().AllIdentifier() {
			ar.ColumnNames = append(ar.ColumnNames, *b.visitIdentifier(id))
		}
	}
	return ar
}

func (b *AstBuilder) VisitTableName(ctx *generated.TableNameContext) interface{} {
	return &ast.Table{Name: b.visit(ctx.QualifiedName()).(ast.QualifiedName)}
}

func (b *AstBuilder) VisitSubqueryRelation(ctx *generated.SubqueryRelationContext) interface{} {
	return &ast.TableSubquery{Query: b.visitStatement(ctx.Query())}
}

func (b *AstBuilder) VisitUnnest(ctx *generated.UnnestContext) interface{} {
	unnest := &ast.Unnest{Ordinality: ctx.ORDINALITY() != nil}
	for _, expr := range ctx.AllExpression() {
		unnest.Expressions = append(unnest.Expressions, b.visitExpression(expr))
	}
	return unnest
}

func (b *AstBuilder) VisitParenthesizedRelation(ctx *generated.ParenthesizedRelationContext) interface{} {
	return b.visitRelation(ctx.Relation())
}

// --------------------------------------------------------------------------
// Boolean Expressions
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitPredicated(ctx *generated.PredicatedContext) interface{} {
	value := b.visitExpression(ctx.ValueExpression())
	if ctx.Predicate() == nil {
		return value
	}
	predResult := b.visit(ctx.Predicate())
	if predResult == nil {
		return value
	}
	switch pred := predResult.(type) {
	case *ast.ComparisonExpression:
		pred.Left = value
		return pred
	case *ast.BetweenPredicate:
		pred.Value = value
		return pred
	case *ast.InPredicate:
		pred.Value = value
		return pred
	case *ast.LikePredicate:
		pred.Value = value
		return pred
	case *ast.IsNullPredicate:
		pred.Value = value
		return pred
	default:
		return value
	}
}

func (b *AstBuilder) VisitLogicalNot(ctx *generated.LogicalNotContext) interface{} {
	return &ast.NotExpression{Value: b.visitExpression(ctx.BooleanExpression())}
}

func (b *AstBuilder) VisitOr(ctx *generated.OrContext) interface{} {
	exprs := ctx.AllBooleanExpression()
	left := b.visitExpression(exprs[0])
	for i := 1; i < len(exprs); i++ {
		left = &ast.LogicalBinaryExpression{Operator: ast.LogicalOr, Left: left, Right: b.visitExpression(exprs[i])}
	}
	return left
}

func (b *AstBuilder) VisitAnd(ctx *generated.AndContext) interface{} {
	exprs := ctx.AllBooleanExpression()
	left := b.visitExpression(exprs[0])
	for i := 1; i < len(exprs); i++ {
		left = &ast.LogicalBinaryExpression{Operator: ast.LogicalAnd, Left: left, Right: b.visitExpression(exprs[i])}
	}
	return left
}

// --------------------------------------------------------------------------
// Predicates
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitComparison(ctx *generated.ComparisonContext) interface{} {
	op := ast.ComparisonEqual
	if ctx.ComparisonOperator() != nil {
		text := ctx.ComparisonOperator().GetText()
		switch text {
		case "=":
			op = ast.ComparisonEqual
		case "<>", "!=":
			op = ast.ComparisonNotEqual
		case "<":
			op = ast.ComparisonLessThan
		case "<=":
			op = ast.ComparisonLessEqual
		case ">":
			op = ast.ComparisonGreaterThan
		case ">=":
			op = ast.ComparisonGreaterEqual
		}
	} else if ctx.OPERATOR() != nil {
		text := ctx.OPERATOR().GetText()
		switch text {
		case "=":
			op = ast.ComparisonEqual
		case "<>", "!=":
			op = ast.ComparisonNotEqual
		case "<":
			op = ast.ComparisonLessThan
		case "<=":
			op = ast.ComparisonLessEqual
		case ">":
			op = ast.ComparisonGreaterThan
		case ">=":
			op = ast.ComparisonGreaterEqual
		}
	}
	return &ast.ComparisonExpression{Operator: op, Right: b.visitExpression(ctx.GetRight())}
}

func (b *AstBuilder) VisitBetween(ctx *generated.BetweenContext) interface{} {
	values := ctx.AllValueExpression()
	return &ast.BetweenPredicate{
		Min: values[len(values)-2].Accept(b).(ast.Expression),
		Max: values[len(values)-1].Accept(b).(ast.Expression),
		Not: ctx.NOT() != nil,
	}
}

func (b *AstBuilder) VisitInList(ctx *generated.InListContext) interface{} {
	list := &ast.InListExpression{}
	for _, expr := range ctx.AllExpression() {
		list.Values = append(list.Values, b.visitExpression(expr))
	}
	return &ast.InPredicate{ValueList: list, Not: ctx.NOT() != nil}
}

func (b *AstBuilder) VisitInSubquery(ctx *generated.InSubqueryContext) interface{} {
	return &ast.InPredicate{ValueList: &ast.SubqueryExpression{Query: b.visitStatement(ctx.Query())}, Not: ctx.NOT() != nil}
}

func (b *AstBuilder) VisitLike(ctx *generated.LikeContext) interface{} {
	like := &ast.LikePredicate{Pattern: b.visitExpression(ctx.GetPattern()), Not: ctx.NOT() != nil}
	if ctx.GetEscape() != nil {
		like.Escape = b.visitExpression(ctx.GetEscape())
	}
	return like
}

func (b *AstBuilder) VisitNullPredicate(ctx *generated.NullPredicateContext) interface{} {
	return &ast.IsNullPredicate{Not: ctx.NOT() != nil}
}

func (b *AstBuilder) VisitDistinctFrom(ctx *generated.DistinctFromContext) interface{} {
	return &ast.ComparisonExpression{Operator: ast.ComparisonNotEqual, Right: b.visitExpression(ctx.GetRight())}
}

func (b *AstBuilder) VisitQuantifiedComparison(ctx *generated.QuantifiedComparisonContext) interface{} {
	op := ast.ComparisonEqual
	if ctx.ComparisonOperator() != nil {
		text := ctx.ComparisonOperator().GetText()
		switch text {
		case "=":
			op = ast.ComparisonEqual
		case "<>", "!=":
			op = ast.ComparisonNotEqual
		case "<":
			op = ast.ComparisonLessThan
		case "<=":
			op = ast.ComparisonLessEqual
		case ">":
			op = ast.ComparisonGreaterThan
		case ">=":
			op = ast.ComparisonGreaterEqual
		}
	}
	return &ast.ComparisonExpression{Operator: op, Right: &ast.SubqueryExpression{Query: b.visitStatement(ctx.Query())}}
}

// --------------------------------------------------------------------------
// Value Expressions
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitValueExpressionDefault(ctx *generated.ValueExpressionDefaultContext) interface{} {
	return b.visit(ctx.PrimaryExpression())
}

func (b *AstBuilder) VisitArithmeticBinary(ctx *generated.ArithmeticBinaryContext) interface{} {
	op := ast.ArithmeticAdd
	token := ctx.GetOperator()
	if token != nil {
		switch token.GetTokenType() {
		case generated.SqlBaseParserPLUS:
			op = ast.ArithmeticAdd
		case generated.SqlBaseParserMINUS:
			op = ast.ArithmeticSubtract
		case generated.SqlBaseParserASTERISK:
			op = ast.ArithmeticMultiply
		case generated.SqlBaseParserSLASH:
			op = ast.ArithmeticDivide
		case generated.SqlBaseParserPERCENT:
			op = ast.ArithmeticModulus
		}
	}
	values := ctx.AllValueExpression()
	return &ast.ArithmeticBinaryExpression{Operator: op, Left: b.visitExpression(values[0]), Right: b.visitExpression(values[1])}
}

func (b *AstBuilder) VisitArithmeticUnary(ctx *generated.ArithmeticUnaryContext) interface{} {
	value := b.visitExpression(ctx.ValueExpression())
	if ctx.PLUS() != nil {
		return value
	}
	if ctx.MINUS() != nil {
		return &ast.ArithmeticBinaryExpression{Operator: ast.ArithmeticSubtract, Left: &ast.LongLiteral{Value: 0}, Right: value}
	}
	return value
}

func (b *AstBuilder) VisitConcatenation(ctx *generated.ConcatenationContext) interface{} {
	return &ast.FunctionCall{Name: ast.QualifiedNameOf("concat"), Arguments: []ast.Expression{b.visitExpression(ctx.GetLeft()), b.visitExpression(ctx.GetRight())}}
}

func (b *AstBuilder) VisitAtTimeZone(ctx *generated.AtTimeZoneContext) interface{} {
	return &ast.AtTimeZone{Value: b.visitExpression(ctx.ValueExpression()), TimeZone: b.visit(ctx.TimeZoneSpecifier()).(ast.Expression)}
}

// --------------------------------------------------------------------------
// Primary Expressions
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitColumnReference(ctx *generated.ColumnReferenceContext) interface{} {
	return b.visitIdentifier(ctx.Identifier())
}

func (b *AstBuilder) VisitDereference(ctx *generated.DereferenceContext) interface{} {
	return &ast.DereferenceExpression{Base: b.visitExpression(ctx.GetBase()), Field: b.visitIdentifier(ctx.GetFieldName())}
}

func (b *AstBuilder) VisitFunctionCall(ctx *generated.FunctionCallContext) interface{} {
	fc := &ast.FunctionCall{Name: b.visit(ctx.QualifiedName()).(ast.QualifiedName)}
	if ctx.ASTERISK() != nil {
		fc.Arguments = append(fc.Arguments, &ast.StarExpression{})
	} else {
		for _, expr := range ctx.AllExpression() {
			fc.Arguments = append(fc.Arguments, b.visitExpression(expr))
		}
	}
	if ctx.SetQuantifier() != nil {
		fc.Distinct = ctx.SetQuantifier().DISTINCT() != nil
	}
	for _, si := range ctx.AllSortItem() {
		fc.OrderBy = append(fc.OrderBy, *b.visitSortItem(si))
	}
	if ctx.Filter() != nil {
		fc.Filter = b.visitExpression(ctx.Filter().BooleanExpression())
	}
	if ctx.Over() != nil {
		fc.Window = b.visitWindow(ctx.Over())
	}
	if ctx.NullTreatment() != nil {
		fc.IgnoreNulls = ctx.NullTreatment().IGNORE() != nil
	}
	return fc
}

func (b *AstBuilder) VisitCast(ctx *generated.CastContext) interface{} {
	return &ast.Cast{Expression: b.visitExpression(ctx.Expression()), Type: b.visitDataType(ctx.Type_()), Safe: ctx.TRY_CAST() != nil}
}

func (b *AstBuilder) VisitStringLiteral(ctx *generated.StringLiteralContext) interface{} {
	text := ctx.GetText()
	if len(text) >= 2 {
		if (text[0] == '\'' && text[len(text)-1] == '\'') || (text[0] == '"' && text[len(text)-1] == '"') {
			text = text[1 : len(text)-1]
		}
	}
	return &ast.StringLiteral{Value: text}
}

func (b *AstBuilder) VisitNumericLiteral(ctx *generated.NumericLiteralContext) interface{} {
	text := ctx.GetText()
	if strings.Contains(text, ".") || strings.Contains(text, "e") || strings.Contains(text, "E") {
		val, err := strconv.ParseFloat(text, 64)
		if err == nil {
			return &ast.DoubleLiteral{Value: val}
		}
	}
	val, err := strconv.ParseInt(text, 10, 64)
	if err == nil {
		return &ast.LongLiteral{Value: val}
	}
	valF, _ := strconv.ParseFloat(text, 64)
	return &ast.DoubleLiteral{Value: valF}
}

func (b *AstBuilder) VisitBooleanLiteral(ctx *generated.BooleanLiteralContext) interface{} {
	if ctx.BooleanValue().TRUE() != nil {
		return &ast.BooleanLiteral{Value: true}
	}
	return &ast.BooleanLiteral{Value: false}
}

func (b *AstBuilder) VisitNullLiteral(ctx *generated.NullLiteralContext) interface{} {
	return &ast.NullLiteral{}
}

func (b *AstBuilder) VisitSubqueryExpression(ctx *generated.SubqueryExpressionContext) interface{} {
	return &ast.SubqueryExpression{Query: b.visitStatement(ctx.Query())}
}

func (b *AstBuilder) VisitExists(ctx *generated.ExistsContext) interface{} {
	return &ast.FunctionCall{Name: ast.QualifiedNameOf("exists"), Arguments: []ast.Expression{&ast.SubqueryExpression{Query: b.visitStatement(ctx.Query())}}}
}

func (b *AstBuilder) VisitParenthesizedExpression(ctx *generated.ParenthesizedExpressionContext) interface{} {
	return b.visitExpression(ctx.Expression())
}

// --------------------------------------------------------------------------
// Identifiers
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitUnquotedIdentifier(ctx *generated.UnquotedIdentifierContext) interface{} {
	text := ctx.GetText()
	if ctx.IDENTIFIER() != nil {
		text = ctx.IDENTIFIER().GetText()
	}
	return &ast.Identifier{Value: text, Delimited: false}
}

func (b *AstBuilder) VisitQuotedIdentifier(ctx *generated.QuotedIdentifierContext) interface{} {
	text := ctx.QUOTED_IDENTIFIER().GetText()
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = text[1 : len(text)-1]
	}
	return &ast.Identifier{Value: text, Delimited: true}
}

func (b *AstBuilder) VisitBackQuotedIdentifier(ctx *generated.BackQuotedIdentifierContext) interface{} {
	text := ctx.BACKQUOTED_IDENTIFIER().GetText()
	if len(text) >= 2 && text[0] == '`' && text[len(text)-1] == '`' {
		text = text[1 : len(text)-1]
	}
	return &ast.Identifier{Value: text, Delimited: true}
}

func (b *AstBuilder) VisitDigitIdentifier(ctx *generated.DigitIdentifierContext) interface{} {
	return &ast.Identifier{Value: ctx.DIGIT_IDENTIFIER().GetText(), Delimited: false}
}

func (b *AstBuilder) VisitQualifiedName(ctx *generated.QualifiedNameContext) interface{} {
	var parts []string
	for _, id := range ctx.AllIdentifier() {
		ident := b.visitIdentifier(id)
		if ident != nil {
			parts = append(parts, ident.Value)
		}
	}
	return ast.QualifiedNameOf(parts...)
}

// --------------------------------------------------------------------------
// SortItem
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitSortItem(ctx *generated.SortItemContext) interface{} {
	si := &ast.SortItem{SortKey: b.visitExpression(ctx.Expression()), Ordering: ast.OrderingAsc}
	if ctx.DESC() != nil {
		si.Ordering = ast.OrderingDesc
	}
	if ctx.NULLS() != nil {
		if ctx.FIRST() != nil {
			si.NullOrdering = ast.NullOrderingFirst
		}
		if ctx.LAST() != nil {
			si.NullOrdering = ast.NullOrderingLast
		}
	}
	return si
}

// --------------------------------------------------------------------------
// GroupBy
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitGroupBy(ctx *generated.GroupByContext) interface{} {
	gb := &ast.GroupBy{}
	for _, ge := range ctx.AllGroupingElement() {
		result := b.visit(ge)
		if result == nil {
			continue
		}
		switch v := result.(type) {
		case *ast.GroupBy:
			gb.Expressions = append(gb.Expressions, v.Expressions...)
		case []ast.Expression:
			gb.Expressions = append(gb.Expressions, v...)
		case ast.Expression:
			gb.Expressions = append(gb.Expressions, v)
		}
	}
	return gb
}

func (b *AstBuilder) VisitGroupingSet(ctx *generated.GroupingSetContext) interface{} {
	var exprs []ast.Expression
	for _, expr := range ctx.AllExpression() {
		exprs = append(exprs, b.visitExpression(expr))
	}
	return exprs
}

func (b *AstBuilder) VisitSingleGroupingSet(ctx *generated.SingleGroupingSetContext) interface{} {
	return b.visit(ctx.GroupingSet())
}

// --------------------------------------------------------------------------
// Types
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitGenericType(ctx *generated.GenericTypeContext) interface{} {
	dt := ast.DataType{Name: b.visitIdentifier(ctx.Identifier()).Value}
	for _, tp := range ctx.AllTypeParameter() {
		result := b.visit(tp)
		if result != nil {
			dt.Parameters = append(dt.Parameters, result.(ast.DataTypeParameter))
		}
	}
	return dt
}

func (b *AstBuilder) VisitTypeParameter(ctx *generated.TypeParameterContext) interface{} {
	return ast.TypeParameter{Type: b.visitDataType(ctx.Type_())}
}

// --------------------------------------------------------------------------
// Window
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitOver(ctx *generated.OverContext) interface{} {
	win := &ast.Window{}
	if ctx.WindowSpecification() != nil {
		ws := ctx.WindowSpecification()
		for _, expr := range ws.AllExpression() {
			win.PartitionBy = append(win.PartitionBy, b.visitExpression(expr))
		}
		for _, si := range ws.AllSortItem() {
			win.OrderBy = append(win.OrderBy, *b.visitSortItem(si))
		}
		if ws.WindowFrame() != nil {
			win.Frame = b.visit(ws.WindowFrame()).(*ast.WindowFrame)
		}
	}
	return win
}

func (b *AstBuilder) VisitWindowFrame(ctx *generated.WindowFrameContext) interface{} {
	frame := &ast.WindowFrame{}
	if ctx.FrameExtent() != nil {
		result := b.visit(ctx.FrameExtent())
		if result != nil {
			if f, ok := result.(*ast.WindowFrame); ok {
				return f
			}
		}
	}
	return frame
}

func (b *AstBuilder) VisitUnboundedFrame(ctx *generated.UnboundedFrameContext) interface{} {
	bound := ast.FrameBound{}
	if ctx.PRECEDING() != nil {
		bound.Type = ast.BoundTypeUnboundedPreceding
	}
	if ctx.FOLLOWING() != nil {
		bound.Type = ast.BoundTypeUnboundedFollowing
	}
	return bound
}

func (b *AstBuilder) VisitBoundedFrame(ctx *generated.BoundedFrameContext) interface{} {
	bound := ast.FrameBound{Value: b.visitExpression(ctx.Expression())}
	if ctx.PRECEDING() != nil {
		bound.Type = ast.BoundTypePreceding
	}
	if ctx.FOLLOWING() != nil {
		bound.Type = ast.BoundTypeFollowing
	}
	return bound
}

func (b *AstBuilder) VisitCurrentRowBound(ctx *generated.CurrentRowBoundContext) interface{} {
	return ast.FrameBound{Type: ast.BoundTypeCurrentRow}
}

// --------------------------------------------------------------------------
// Entry points
// --------------------------------------------------------------------------

func (b *AstBuilder) VisitStandaloneExpression(ctx *generated.StandaloneExpressionContext) interface{} {
	return b.visitExpression(ctx.Expression())
}

func (b *AstBuilder) VisitExpression(ctx *generated.ExpressionContext) interface{} {
	return b.visitExpression(ctx.BooleanExpression())
}

func (b *AstBuilder) VisitRowCount(ctx *generated.RowCountContext) interface{} {
	if ctx.INTEGER_VALUE() != nil {
		val, _ := strconv.ParseInt(ctx.INTEGER_VALUE().GetText(), 10, 64)
		return &ast.LongLiteral{Value: val}
	}
	return nil
}

func (b *AstBuilder) VisitPatternRecognition(ctx *generated.PatternRecognitionContext) interface{} {
	return b.visit(ctx.AliasedRelation())
}

func (b *AstBuilder) VisitSampledRelation(ctx *generated.SampledRelationContext) interface{} {
	return b.visit(ctx.PatternRecognition())
}
