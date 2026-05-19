package rewrite

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// metricSqlRender renders a Metric into a CTE query. Mirrors Java
// io.wren.base.sqlrewrite.MetricSqlRender (extends RelationableSqlRender).
type metricSqlRender struct {
	relationable     *dto.Metric
	mdl              *mdl.WrenMDL
	refSql           string
	requiredObjects  map[string]bool
	selectItems      []string
	calculatedRequiredRelationshipInfos []*calculatedFieldRelationshipInfo
	calculatedScopeSelectItems          *orderedMap
	requiredDims     map[string]bool
	requiredMeasures map[string]bool
}

// newMetricSqlRender mirrors the 2-arg MetricSqlRender(Metric, WrenMDL) ctor.
func newMetricSqlRender(metric *dto.Metric, wrenMDL *mdl.WrenMDL) *metricSqlRender {
	r := &metricSqlRender{
		relationable:               metric,
		mdl:                        wrenMDL,
		requiredObjects:            map[string]bool{},
		calculatedScopeSelectItems: newOrderedMap(),
		requiredDims:               map[string]bool{},
		requiredMeasures:           map[string]bool{},
	}
	r.refSql = r.initRefSql()
	if metric.BaseObject != "" {
		r.requiredObjects[metric.BaseObject] = true
	}
	for _, c := range metric.Dimension {
		r.requiredDims[c.Name] = true
	}
	for _, c := range metric.Measure {
		r.requiredMeasures[c.Name] = true
	}
	return r
}

// initRefSql mirrors MetricSqlRender.initRefSql (:81-84).
func (r *metricSqlRender) initRefSql() string {
	return fmt.Sprintf(`SELECT * FROM "%s"`, r.relationable.BaseObject)
}

// isRequiredColumn mirrors MetricSqlRender.isRequiredColumn (:286-289).
func (r *metricSqlRender) isRequiredColumn(name string) bool {
	return r.requiredDims[name] || r.requiredMeasures[name]
}

// addCountAllIfNeeded mirrors MetricSqlRender.addCountAllIfNeeded (:293-298).
// Appends to the struct field selectItems (as the Java field does). Inert on the
// non-dynamic path (metric measures are always present), kept for fidelity.
func (r *metricSqlRender) addCountAllIfNeeded() {
	if len(r.requiredMeasures) == 0 {
		r.selectItems = append(r.selectItems, "COUNT(*) AS _count_filler")
	}
}

// render dispatches on the metric's base object. Mirrors MetricSqlRender.render().
func (r *metricSqlRender) render() (*RelationInfo, error) {
	base := r.relationable.BaseObject
	if model, ok := r.mdl.GetModel(base); ok {
		return r.renderOnModel(model)
	}
	if metric, ok := r.mdl.GetMetric(base); ok {
		return r.renderBasedOnMetric(metric.Name)
	}
	if cm, ok := r.mdl.GetCumulativeMetric(base); ok {
		return r.renderBasedOnMetric(cm.Name)
	}
	return nil, fmt.Errorf("invalid metric, cannot render metric sql")
}

// renderBasedOnMetric handles metric-on-metric / metric-on-cumulative.
// Mirrors MetricSqlRender.renderBasedOnMetric. NOTE: Java joins a *local*
// selectItems while addCountAllIfNeeded mutates the field — a latent quirk that
// is inert here (measures always present). Ported 1:1: local slice + field append.
func (r *metricSqlRender) renderBasedOnMetric(metricName string) (*RelationInfo, error) {
	var selectItems []string
	for _, c := range r.relationable.GetColumns() {
		if !r.isRequiredColumn(c.Name) {
			continue
		}
		selectItems = append(selectItems, fmt.Sprintf(`%s AS "%s"`, c.GetExpression(), c.Name))
	}
	r.addCountAllIfNeeded()
	sql := r.getQuerySql(strings.Join(selectItems, ", "), metricName)
	query, err := parseQuery(sql)
	if err != nil {
		return nil, fmt.Errorf("render metric %q: %w", r.relationable.Name, err)
	}
	return newRelationInfo(r.relationable.Name, []string{metricName}, query), nil
}

// getQuerySql mirrors MetricSqlRender.getQuerySql.
func (r *metricSqlRender) getQuerySql(selectItemsSql, tableJoinsSql string) string {
	if len(r.requiredDims) == 0 {
		return fmt.Sprintf("SELECT %s FROM %s", selectItemsSql, tableJoinsSql)
	}
	ordinals := make([]string, 0, len(r.requiredDims))
	for i := 1; i <= len(r.requiredDims); i++ {
		ordinals = append(ordinals, strconv.Itoa(i))
	}
	return fmt.Sprintf("SELECT %s FROM %s GROUP BY %s", selectItemsSql, tableJoinsSql, strings.Join(ordinals, ","))
}

// getModelSubQuerySelectItemsExpression mirrors the MetricSqlRender override
// (:132-137): metric model sub-query always projects "*".
func (r *metricSqlRender) getModelSubQuerySelectItemsExpression() string {
	return "*"
}

// awareModelStr mirrors MetricSqlRender.awareModel(String, Model).
func (r *metricSqlRender) awareModelStr(expression string, baseModel *dto.Model) (string, error) {
	expr, err := parseExpression(expression)
	if err != nil {
		return "", err
	}
	return formatter.FormatExpression(r.awareModelExpr(expr, baseModel)), nil
}

// awareModelExpr mirrors MetricSqlRender.awareModel(Expression, Model): a bare
// identifier matching a base-model column (case-insensitive) becomes
// "<model>"."<col>".
func (r *metricSqlRender) awareModelExpr(expression ast.Expression, baseModel *dto.Model) ast.Expression {
	return RewriteNode(expression, func(n ast.Node) (ast.Node, bool) {
		id, ok := n.(*ast.Identifier)
		if !ok {
			return nil, false // descend
		}
		for _, c := range baseModel.Columns {
			if strings.EqualFold(c.Name, id.Value) {
				return dereferenceFrom([]ast.Identifier{
					{Value: baseModel.Name, Delimited: true},
					{Value: id.Value, Delimited: true},
				}), true
			}
		}
		return id, true
	}).(ast.Expression)
}

// getSelectItemsExpression mirrors MetricSqlRender.getSelectItemsExpression.
func (r *metricSqlRender) getSelectItemsExpression(column dto.Column, relationableBase string, hasRelationableBase bool) (string, error) {
	isMeasure := false
	for _, m := range r.relationable.Measure {
		if m.Name == column.Name {
			isMeasure = true
			break
		}
	}
	baseModel, ok := r.mdl.GetModel(r.relationable.BaseObject)
	if !ok {
		return "", fmt.Errorf("cannot find model %s", r.relationable.BaseObject)
	}
	expr, err := parseExpression(column.GetExpression())
	if err != nil {
		return "", err
	}
	relInfos, err := analyzer.GetRelationships(expr, r.mdl, baseModel)
	if err != nil {
		return "", err
	}
	if len(relInfos) > 0 && hasRelationableBase {
		newExpr := relationshipAware(relInfos, relationableBase, expr)
		return fmt.Sprintf(`%s AS "%s"`, formatter.FormatExpression(newExpr), column.Name), nil
	}
	if isMeasure {
		if column.Expression == "" {
			return "", fmt.Errorf("measure column must have expression")
		}
		am, err := r.awareModelStr(column.Expression, baseModel)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`%s AS "%s"`, am, column.Name), nil
	}
	am, err := r.awareModelStr(column.GetExpression(), baseModel)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`%s AS "%s"`, am, column.Name), nil
}

// collectRelationship mirrors MetricSqlRender.collectRelationship.
func (r *metricSqlRender) collectRelationship(column dto.Column, baseModel *dto.Model) error {
	if !r.isRequiredColumn(column.Name) {
		return nil
	}
	expr, err := parseExpression(column.GetExpression())
	if err != nil {
		return err
	}
	relInfos, err := analyzer.GetRelationships(expr, r.mdl, baseModel)
	if err != nil {
		return err
	}
	if len(relInfos) > 0 {
		col := column
		r.calculatedRequiredRelationshipInfos = append(
			r.calculatedRequiredRelationshipInfos, newCalculatedFieldRelationshipInfo(&col, relInfos))
		for _, info := range relInfos {
			for _, rel := range info.Relationships() {
				for _, mn := range rel.Models {
					if mn != baseModel.Name {
						r.requiredObjects[mn] = true
					}
				}
			}
		}
		item, err := r.getSelectItemsExpression(column, getRelationableAlias(baseModel.Name), true)
		if err != nil {
			return err
		}
		r.selectItems = append(r.selectItems, item)
		return nil
	}
	item, err := r.getSelectItemsExpression(column, "", false)
	if err != nil {
		return err
	}
	r.selectItems = append(r.selectItems, item)
	r.calculatedScopeSelectItems.put(column.Name, column.GetExpression())
	return nil
}

// getCalculatedSubQuery mirrors MetricSqlRender.getCalculatedSubQuery.
func (r *metricSqlRender) getCalculatedSubQuery(baseModel *dto.Model, infos []*calculatedFieldRelationshipInfo) ([]subQueryJoinInfo, error) {
	if len(infos) == 0 {
		return nil, nil
	}
	var reqExprs []string
	seenExpr := map[string]bool{}
	for _, ci := range infos {
		for _, eri := range ci.expressionRelationship {
			s := formatter.FormatExpression(toDereferenceExpression(eri))
			if !seenExpr[s] {
				seenExpr[s] = true
				reqExprs = append(reqExprs, s)
			}
		}
	}
	var reqRels []*dto.Relationship
	seenRel := map[string]bool{}
	for _, ci := range infos {
		for _, eri := range ci.expressionRelationship {
			for _, rel := range eri.Relationships() {
				if !seenRel[rel.Name] {
					seenRel[rel.Name] = true
					reqRels = append(reqRels, rel)
				}
			}
		}
	}
	joins := ""
	for _, rel := range reqRels {
		cond, err := qualifiedConditionString(rel.Condition)
		if err != nil {
			return nil, err
		}
		joins += fmt.Sprintf("LEFT JOIN \"%s\" ON %s\n", rel.Models[1], cond)
	}
	tableJoins := fmt.Sprintf("\"%s\"\n%s", baseModel.Name, joins)
	alias := getRelationableAlias(baseModel.Name)
	joinCriteria := fmt.Sprintf(`"%s"."%s" = "%s"."%s"`,
		baseModel.Name, baseModel.PrimaryKey, alias, baseModel.PrimaryKey)
	sql := fmt.Sprintf("SELECT \"%s\".\"%s\", %s FROM (%s)",
		baseModel.Name, baseModel.PrimaryKey, strings.Join(reqExprs, ", "), tableJoins)
	return []subQueryJoinInfo{{sql: sql, subqueryAlias: alias, joinCriteria: joinCriteria}}, nil
}

// renderOnModel mirrors MetricSqlRender.render(Model baseModel).
func (r *metricSqlRender) renderOnModel(baseModel *dto.Model) (*RelationInfo, error) {
	// non-relationship, no-expression, required columns
	for _, c := range r.relationable.GetColumns() {
		if c.Relationship == "" && c.Expression == "" && r.isRequiredColumn(c.Name) {
			item, err := r.getSelectItemsExpression(c, "", false)
			if err != nil {
				return nil, err
			}
			r.selectItems = append(r.selectItems, item)
		}
	}
	// non-relationship, with-expression columns
	for _, c := range r.relationable.GetColumns() {
		if c.Relationship == "" && c.Expression != "" {
			if err := r.collectRelationship(c, baseModel); err != nil {
				return nil, err
			}
		}
	}
	r.addCountAllIfNeeded()

	modelSubQuery := fmt.Sprintf(`(SELECT %s FROM (%s) AS "%s") AS "%s"`,
		r.getModelSubQuerySelectItemsExpression(), r.refSql, baseModel.Name, baseModel.Name)

	tableJoinsSql := modelSubQuery
	if len(r.calculatedRequiredRelationshipInfos) > 0 {
		subs, err := r.getCalculatedSubQuery(baseModel, r.calculatedRequiredRelationshipInfos)
		if err != nil {
			return nil, err
		}
		for _, info := range subs {
			tableJoinsSql += fmt.Sprintf("\nLEFT JOIN (%s) AS \"%s\" ON %s", info.sql, info.subqueryAlias, info.joinCriteria)
		}
	}
	tableJoinsSql += "\n"

	query, err := parseQuery(r.getQuerySql(strings.Join(r.selectItems, ", "), tableJoinsSql))
	if err != nil {
		return nil, fmt.Errorf("render metric %q: %w", r.relationable.Name, err)
	}
	return newRelationInfo(r.relationable.Name, sortedKeys(r.requiredObjects), query), nil
}

// relationInfoOfMetric renders a metric into a RelationInfo. Mirrors
// RelationInfo.get(Relationable, WrenMDL) for the Metric case.
func relationInfoOfMetric(metric *dto.Metric, wrenMDL *mdl.WrenMDL) (*RelationInfo, error) {
	return newMetricSqlRender(metric, wrenMDL).render()
}
