package parser

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser/ast"
)

func assertNoError(t *testing.T, err error) {
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseSelect1(t *testing.T) {
	stmt, err := ParseSQL("SELECT 1")
	assertNoError(t, err)
	q, ok := stmt.(*ast.Query)
	if !ok {
		t.Fatalf("expected *ast.Query, got %T", stmt)
	}
	qs, ok := q.Body.(*ast.QuerySpecification)
	if !ok {
		t.Fatalf("expected *ast.QuerySpecification, got %T", q.Body)
	}
	if len(qs.Select.SelectItems) != 1 {
		t.Fatalf("expected 1 select item, got %d", len(qs.Select.SelectItems))
	}
	sc, ok := qs.Select.SelectItems[0].(*ast.SingleColumn)
	if !ok {
		t.Fatalf("expected *ast.SingleColumn, got %T", qs.Select.SelectItems[0])
	}
	lit, ok := sc.Expression.(*ast.LongLiteral)
	if !ok {
		t.Fatalf("expected *ast.LongLiteral, got %T", sc.Expression)
	}
	if lit.Value != 1 {
		t.Fatalf("expected 1, got %d", lit.Value)
	}
}

func TestParseSelectColumnsFromTable(t *testing.T) {
	stmt, err := ParseSQL("SELECT a, b FROM t")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	if len(qs.Select.SelectItems) != 2 {
		t.Fatalf("expected 2 select items, got %d", len(qs.Select.SelectItems))
	}
	table, ok := qs.From.(*ast.Table)
	if !ok {
		t.Fatalf("expected *ast.Table, got %T", qs.From)
	}
	if table.Name.String() != "t" {
		t.Fatalf("expected table name 't', got '%s'", table.Name.String())
	}
}

func TestParseSelectStarWithWhere(t *testing.T) {
	stmt, err := ParseSQL("SELECT * FROM t WHERE x > 5")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	_, ok := qs.Select.SelectItems[0].(*ast.AllColumns)
	if !ok {
		t.Fatalf("expected *ast.AllColumns, got %T", qs.Select.SelectItems[0])
	}
	cmp, ok := qs.Where.(*ast.ComparisonExpression)
	if !ok {
		t.Fatalf("expected *ast.ComparisonExpression, got %T", qs.Where)
	}
	if cmp.Operator != ast.ComparisonGreaterThan {
		t.Fatalf("expected >, got %s", cmp.Operator)
	}
}

func TestParseSelectAliasWithJoin(t *testing.T) {
	stmt, err := ParseSQL("SELECT a AS alias FROM t1 JOIN t2 ON t1.id = t2.id")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	sc := qs.Select.SelectItems[0].(*ast.SingleColumn)
	if sc.Alias == nil || sc.Alias.Value != "alias" {
		t.Fatalf("expected alias 'alias', got %v", sc.Alias)
	}
	join, ok := qs.From.(*ast.Join)
	if !ok {
		t.Fatalf("expected *ast.Join, got %T", qs.From)
	}
	if join.JoinType != ast.JoinTypeInner {
		t.Fatalf("expected INNER join, got %s", join.JoinType)
	}
}

func TestParseSelectGroupByHaving(t *testing.T) {
	stmt, err := ParseSQL("SELECT COUNT(*) FROM t GROUP BY a HAVING COUNT(*) > 1")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	if qs.GroupBy == nil || len(qs.GroupBy.Expressions) != 1 {
		t.Fatalf("expected 1 group by expression, got %v", qs.GroupBy)
	}
	if qs.Having == nil {
		t.Fatalf("expected HAVING clause")
	}
}

func TestParseSelectOrderByLimitOffset(t *testing.T) {
	stmt, err := ParseSQL("SELECT a FROM t ORDER BY a DESC LIMIT 10 OFFSET 5")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	if len(q.OrderBy) != 1 {
		t.Fatalf("expected 1 order by, got %d", len(q.OrderBy))
	}
	if q.OrderBy[0].Ordering != ast.OrderingDesc {
		t.Fatalf("expected DESC, got %s", q.OrderBy[0].Ordering)
	}
	if q.Limit == nil {
		t.Fatalf("expected LIMIT")
	}
	if q.Offset == nil {
		t.Fatalf("expected OFFSET")
	}
}

func TestParseWithCTE(t *testing.T) {
	stmt, err := ParseSQL("WITH cte AS (SELECT 1) SELECT * FROM cte")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	if q.With == nil || len(q.With.Queries) != 1 {
		t.Fatalf("expected 1 CTE, got %v", q.With)
	}
	if q.With.Queries[0].Name.Value != "cte" {
		t.Fatalf("expected CTE name 'cte', got '%s'", q.With.Queries[0].Name.Value)
	}
}

func TestParseInList(t *testing.T) {
	stmt, err := ParseSQL("SELECT a FROM t WHERE a IN (1, 2, 3)")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	inPred, ok := qs.Where.(*ast.InPredicate)
	if !ok {
		t.Fatalf("expected *ast.InPredicate, got %T", qs.Where)
	}
	if inPred.Not {
		t.Fatalf("expected NOT=false")
	}
	inList, ok := inPred.ValueList.(*ast.InListExpression)
	if !ok {
		t.Fatalf("expected *ast.InListExpression, got %T", inPred.ValueList)
	}
	if len(inList.Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(inList.Values))
	}
}

func TestParseBetween(t *testing.T) {
	stmt, err := ParseSQL("SELECT a FROM t WHERE a BETWEEN 1 AND 10")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	between, ok := qs.Where.(*ast.BetweenPredicate)
	if !ok {
		t.Fatalf("expected *ast.BetweenPredicate, got %T", qs.Where)
	}
	if between.Not {
		t.Fatalf("expected NOT=false")
	}
}

func TestParseLike(t *testing.T) {
	stmt, err := ParseSQL("SELECT a FROM t WHERE a LIKE '%test%'")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	like, ok := qs.Where.(*ast.LikePredicate)
	if !ok {
		t.Fatalf("expected *ast.LikePredicate, got %T", qs.Where)
	}
	if like.Not {
		t.Fatalf("expected NOT=false")
	}
}

func TestParseIsNull(t *testing.T) {
	stmt, err := ParseSQL("SELECT a FROM t WHERE a IS NULL")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	isNull, ok := qs.Where.(*ast.IsNullPredicate)
	if !ok {
		t.Fatalf("expected *ast.IsNullPredicate, got %T", qs.Where)
	}
	if isNull.Not {
		t.Fatalf("expected NOT=false")
	}
}

func TestParseLogicalExpressions(t *testing.T) {
	stmt, err := ParseSQL("SELECT NOT a, a AND b, a OR b FROM t")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	if len(qs.Select.SelectItems) != 3 {
		t.Fatalf("expected 3 select items, got %d", len(qs.Select.SelectItems))
	}
	_, ok := qs.Select.SelectItems[0].(*ast.SingleColumn).Expression.(*ast.NotExpression)
	if !ok {
		t.Fatalf("expected *ast.NotExpression for first item")
	}
	logicalAnd, ok := qs.Select.SelectItems[1].(*ast.SingleColumn).Expression.(*ast.LogicalExpression)
	if !ok {
		t.Fatalf("expected *ast.LogicalExpression for second item")
	}
	if logicalAnd.Operator != ast.LogicalAnd {
		t.Fatalf("expected LogicalAnd, got %s", logicalAnd.Operator)
	}
	if len(logicalAnd.Terms) != 2 {
		t.Fatalf("expected 2 terms, got %d", len(logicalAnd.Terms))
	}
	logicalOr, ok := qs.Select.SelectItems[2].(*ast.SingleColumn).Expression.(*ast.LogicalExpression)
	if !ok {
		t.Fatalf("expected *ast.LogicalExpression for third item")
	}
	if logicalOr.Operator != ast.LogicalOr {
		t.Fatalf("expected LogicalOr, got %s", logicalOr.Operator)
	}
	if len(logicalOr.Terms) != 2 {
		t.Fatalf("expected 2 terms, got %d", len(logicalOr.Terms))
	}
}

func TestParseArithmetic(t *testing.T) {
	stmt, err := ParseSQL("SELECT a + b * c FROM t")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	add, ok := qs.Select.SelectItems[0].(*ast.SingleColumn).Expression.(*ast.ArithmeticBinaryExpression)
	if !ok {
		t.Fatalf("expected *ast.ArithmeticBinaryExpression, got %T", qs.Select.SelectItems[0].(*ast.SingleColumn).Expression)
	}
	if add.Operator != ast.ArithmeticAdd {
		t.Fatalf("expected +, got %s", add.Operator)
	}
}

func TestParseCast(t *testing.T) {
	stmt, err := ParseSQL("SELECT CAST(x AS INTEGER) FROM t")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	cast, ok := qs.Select.SelectItems[0].(*ast.SingleColumn).Expression.(*ast.Cast)
	if !ok {
		t.Fatalf("expected *ast.Cast, got %T", qs.Select.SelectItems[0].(*ast.SingleColumn).Expression)
	}
	if cast.Type.Name != "INTEGER" {
		t.Fatalf("expected INTEGER, got %s", cast.Type.Name)
	}
}

func TestParseWindowFunction(t *testing.T) {
	stmt, err := ParseSQL("SELECT func(a, b) OVER (PARTITION BY c) FROM t")
	assertNoError(t, err)
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	fc, ok := qs.Select.SelectItems[0].(*ast.SingleColumn).Expression.(*ast.FunctionCall)
	if !ok {
		t.Fatalf("expected *ast.FunctionCall, got %T", qs.Select.SelectItems[0].(*ast.SingleColumn).Expression)
	}
	if fc.Window == nil {
		t.Fatalf("expected window")
	}
	if len(fc.Window.PartitionBy) != 1 {
		t.Fatalf("expected 1 partition by, got %d", len(fc.Window.PartitionBy))
	}
}
