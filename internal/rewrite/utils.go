package rewrite

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// parseSQL parses a statement. Mirrors Java Utils.parseSql.
func parseSQL(sql string) (ast.Statement, error) {
	return parser.ParseSQL(sql)
}

// parseExpression parses a standalone expression. Mirrors Java Utils.parseExpression.
func parseExpression(sql string) (ast.Expression, error) {
	return parser.ParseExpression(sql)
}

// parseQuery parses sql and asserts the result is a *ast.Query.
// Mirrors Java Utils.parseQuery.
func parseQuery(sql string) (*ast.Query, error) {
	stmt, err := parseSQL(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %s: %w", sql, err)
	}
	q, ok := stmt.(*ast.Query)
	if !ok {
		return nil, fmt.Errorf("not a query: %s", sql)
	}
	return q, nil
}

// checkArgument returns a formatted error when cond is false.
// Mirrors Java com.google.common.base.Preconditions.checkArgument.
func checkArgument(cond bool, format string, args ...any) error {
	if cond {
		return nil
	}
	return fmt.Errorf(format, args...)
}

// contains reports whether s holds v. Shared helper used by the graph and the
// SqlRender tests.
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// qualifiedConditionString parses a relationship condition and delimits all
// identifiers. Mirrors Java Relationship.qualifiedCondition.
func qualifiedConditionString(condition string) (string, error) {
	expr, err := parseExpression(condition)
	if err != nil {
		return "", fmt.Errorf("parse condition %q: %w", condition, err)
	}
	rewritten := RewriteNode(expr, func(n ast.Node) (ast.Node, bool) {
		if id, ok := n.(*ast.Identifier); ok && !id.Delimited {
			return &ast.Identifier{Value: id.Value, Delimited: true}, true
		}
		return nil, false
	}).(ast.Expression)
	return formatter.FormatExpression(rewritten), nil
}

// hasPrefixParts reports whether qn starts with the given prefix parts.
func hasPrefixParts(qn ast.QualifiedName, prefix ...string) bool {
	if len(prefix) > len(qn.Parts) {
		return false
	}
	for i, p := range prefix {
		if !strings.EqualFold(p, qn.Parts[i]) {
			return false
		}
	}
	return true
}

// dereferenceFrom builds a DereferenceExpression chain (or Identifier) from
// a slice of identifiers. Mirrors trino DereferenceExpression.from.
func dereferenceFrom(parts []ast.Identifier) ast.Expression {
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		id := parts[0]
		return &id
	}
	result := &ast.DereferenceExpression{
		Base:  dereferenceFrom(parts[:len(parts)-1]),
		Field: &parts[len(parts)-1],
	}
	return result
}

// sortedKeys returns the keys of set in ascending order — used to turn a
// requiredObjects set into a deterministic slice.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// getWindowType resolves the SQL type of a cumulative metric's window column.
// Mirrors Java Utils.getWindowType.
func getWindowType(cm *dto.CumulativeMetric, wrenMDL *mdl.WrenMDL) (string, error) {
	if model, ok := wrenMDL.GetModel(cm.BaseObject); ok {
		for _, c := range model.Columns {
			if c.Name == cm.Window.RefColumn {
				return c.Type, nil
			}
		}
		return "", fmt.Errorf("window type not found in %s", cm.BaseObject)
	}
	if metric, ok := wrenMDL.GetMetric(cm.BaseObject); ok {
		for _, c := range metric.GetColumns() {
			if c.Name == cm.Window.RefColumn {
				return c.Type, nil
			}
		}
		return "", fmt.Errorf("window type not found in %s", cm.BaseObject)
	}
	if base, ok := wrenMDL.GetCumulativeMetric(cm.BaseObject); ok {
		if base.Window.Name == cm.Window.RefColumn {
			return getWindowType(base, wrenMDL)
		}
		return "", fmt.Errorf("window ref column %s not found in base cumulative metric %s", cm.Window.RefColumn, cm.BaseObject)
	}
	return "", fmt.Errorf("window type not found in %s", cm.BaseObject)
}

// getCumulativeMetricSql builds the cumulative-metric CTE SQL.
// Mirrors Java Utils.getCumulativeMetricSql. Template kept verbatim.
func getCumulativeMetricSql(cm *dto.CumulativeMetric, wrenMDL *mdl.WrenMDL) (string, error) {
	windowType, err := getWindowType(cm, wrenMDL)
	if err != nil {
		return "", err
	}
	const pattern = `select 
  metric_time as %s,
  %s(distinct measure_field) as %s
from 
  (
    select 
      date_trunc('%s', d.metric_time) as metric_time,
      measure_field
    from 
      (%s) d 
      left join (
        select 
          measure_field,
          metric_time
        from (%s) sub1
        where 
          metric_time >= cast('%s' as %s) 
          and metric_time <= cast('%s' as %s)
      ) sub2 on (
        sub2.metric_time <= d.metric_time 
        and sub2.metric_time > %s
      )
    where 
      d.metric_time >= cast('%s' as %s)  
      and d.metric_time <= cast('%s' as %s)   
  ) sub3 
group by 1
order by 1
`
	castingDateSpine := fmt.Sprintf(`select cast(metric_time as %s) as metric_time from "%s"`, windowType, dateSpineName)
	windowRange := fmt.Sprintf("d.metric_time - %s", cm.Window.TimeUnit.IntervalExpression())
	selectFromModel := fmt.Sprintf("select %s as measure_field, %s as metric_time from %s",
		cm.Measure.RefColumn, cm.Window.RefColumn, cm.BaseObject)
	return fmt.Sprintf(pattern,
		cm.Window.Name,
		cm.Measure.Operator,
		cm.Measure.Name,
		string(cm.Window.TimeUnit),
		castingDateSpine,
		selectFromModel,
		cm.Window.Start, windowType,
		cm.Window.End, windowType,
		windowRange,
		cm.Window.Start, windowType,
		cm.Window.End, windowType), nil
}

// parseCumulativeMetricSql builds and parses the cumulative-metric CTE query.
// Mirrors Java Utils.parseCumulativeMetricSql.
func parseCumulativeMetricSql(cm *dto.CumulativeMetric, wrenMDL *mdl.WrenMDL) (*ast.Query, error) {
	sql, err := getCumulativeMetricSql(cm, wrenMDL)
	if err != nil {
		return nil, err
	}
	q, err := parseQuery(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cumulative metric sql for %q: %w", cm.Name, err)
	}
	return q, nil
}

// createDateSpineQuery builds the date-spine CTE query. Mirrors Java
// Utils.createDateSpineQuery (uses the BigQuery GENERATE_TIMESTAMP_ARRAY form).
func createDateSpineQuery(ds dto.DateSpine) (*ast.Query, error) {
	sql := fmt.Sprintf(
		`SELECT * FROM UNNEST(GENERATE_TIMESTAMP_ARRAY(TIMESTAMP '%s', TIMESTAMP '%s', %s)) t(metric_time)`,
		ds.Start, ds.End, ds.Unit.IntervalExpression())
	q, err := parseQuery(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse date spine query: %w", err)
	}
	return q, nil
}

// getMetricRollupSql builds the SQL for a roll_up(...) sub-query.
// Mirrors Java Utils.getMetricRollupSql.
func getMetricRollupSql(info *analyzer.MetricRollupInfo) string {
	metric := info.Metric
	timeGrain := fmt.Sprintf(`DATE_TRUNC('%s', %s) "%s"`,
		string(info.TimeUnit), info.TimeGrain.RefColumn, info.TimeGrain.Name)
	selectItems := []string{timeGrain}
	for _, c := range metric.Dimension {
		selectItems = append(selectItems, fmt.Sprintf(`%s AS "%s"`, c.GetExpression(), c.Name))
	}
	for _, c := range metric.Measure {
		selectItems = append(selectItems, fmt.Sprintf(`%s AS "%s"`, c.GetExpression(), c.Name))
	}
	n := len(selectItems) - len(metric.Measure)
	ordinals := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ordinals = append(ordinals, strconv.Itoa(i))
	}
	return fmt.Sprintf(`SELECT %s FROM "%s" GROUP BY %s`,
		strings.Join(selectItems, ","), metric.BaseObject, strings.Join(ordinals, ","))
}

// parseMetricRollupSql builds and parses the roll_up sub-query.
// Mirrors Java Utils.parseMetricRollupSql.
func parseMetricRollupSql(info *analyzer.MetricRollupInfo) (*ast.Query, error) {
	q, err := parseQuery(getMetricRollupSql(info))
	if err != nil {
		return nil, fmt.Errorf("failed to parse metric rollup sql for %q: %w", info.Metric.Name, err)
	}
	return q, nil
}
