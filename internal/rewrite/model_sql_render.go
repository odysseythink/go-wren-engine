package rewrite

import (
	"fmt"
	"strings"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// modelSqlRender mirrors Java ModelSqlRender.
type modelSqlRender struct {
	relationableSqlRender
	requiredFields map[string]bool
}

func newModelSqlRender(model *dto.Model, wrenMDL *mdl.WrenMDL) (*modelSqlRender, error) {
	requiredFields := map[string]bool{}
	for i := range model.Columns {
		requiredFields[model.Columns[i].Name] = true
	}
	refSql, err := initRefSql(model)
	if err != nil {
		return nil, err
	}
	return &modelSqlRender{
		relationableSqlRender: relationableSqlRender{
			relationable:               model,
			mdl:                        wrenMDL,
			refSql:                     refSql,
			requiredObjects:            map[string]bool{},
			selectItems:                []string{},
			calculatedRequiredRelationshipInfos: []*calculatedFieldRelationshipInfo{},
			calculatedScopeSelectItems: newOrderedMap(),
		},
		requiredFields: requiredFields,
	}, nil
}

func initRefSql(model *dto.Model) (string, error) {
	if model.RefSql != "" {
		return "(" + model.RefSql + ")", nil
	}
	// Use model.BaseObject directly (not GetBaseObject) because GetBaseObject
	// returns model.Name for refSql models, creating a self-dependency cycle.
	if model.BaseObject != "" {
		return fmt.Sprintf(`(SELECT * FROM "%s")`, model.BaseObject), nil
	}
	if model.TableReference != nil {
		return model.TableReference.ToQualifiedName(), nil
	}
	return "", fmt.Errorf("cannot get reference sql from model %s", model.Name)
}

func (r *modelSqlRender) render() (*RelationInfo, error) {
	model := r.relationable
	if len(model.Columns) == 0 {
		q, err := parseQuery(r.refSql)
		if err != nil {
			return nil, err
		}
		return newRelationInfo(model.Name, nil, q), nil
	}

	// First loop: columns with no relationship and no expression
	for i := range model.Columns {
		col := &model.Columns[i]
		if col.Relationship == "" && col.Expression == "" {
			r.selectItems = append(r.selectItems, r.getSelectItemsExpression(col, ""))
			r.calculatedScopeSelectItems.put(col.Name, fmt.Sprintf(`"%s"."%s"`, model.Name, col.Name))
		}
	}

	// Second loop: columns with no relationship and with expression
	for i := range model.Columns {
		col := &model.Columns[i]
		if col.Relationship == "" && col.Expression != "" {
			r.collectRelationship(col, model)
		}
	}

	baseModelSql := r.getBaseModelSql(model)
	calculatedFieldsWithoutRelationship := r.getModelSubQuerySelectItemsExpression()
	calculatedSubQuery := fmt.Sprintf("(SELECT %s FROM (%s) AS \"%s\") AS \"%s\"\n",
		calculatedFieldsWithoutRelationship, baseModelSql, model.Name, model.Name)

	tableJoinsSql := calculatedSubQuery
	if len(r.calculatedRequiredRelationshipInfos) > 0 {
		joins := r.getCalculatedSubQuery(model, r.calculatedRequiredRelationshipInfos)
		for _, info := range joins {
			tableJoinsSql += fmt.Sprintf("LEFT JOIN (%s) AS \"%s\" ON %s", info.sql, info.subqueryAlias, info.joinCriteria)
		}
	}
	tableJoinsSql += "\n"

	querySQL := r.getQuerySql(strings.Join(r.selectItems, ", "), tableJoinsSql)
	q, err := parseQuery(querySQL)
	if err != nil {
		return nil, fmt.Errorf("render model %q: %w", model.Name, err)
	}
	return newRelationInfo(model.Name, sortedKeys(r.requiredObjects), q), nil
}

func (r *modelSqlRender) getQuerySql(selectItemsSql, tableJoinsSql string) string {
	return fmt.Sprintf("SELECT %s FROM %s", selectItemsSql, tableJoinsSql)
}

func (r *modelSqlRender) getModelSubQuerySelectItemsExpression() string {
	entries := r.calculatedScopeSelectItems.entries()
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf("%s AS \"%s\"", e.V, e.K)
	}
	return strings.Join(parts, ", ")
}

func (r *modelSqlRender) getSelectItemsExpression(column *dto.Column, relationalBase string) string {
	if relationalBase != "" {
		return fmt.Sprintf(`"%s"."%s" AS "%s"`, relationalBase, column.Name, column.Name)
	}
	return fmt.Sprintf(`"%s"."%s" AS "%s"`, r.relationable.Name, column.Name, column.Name)
}

func (r *modelSqlRender) collectRelationship(column *dto.Column, baseModel *dto.Model) {
	expr, err := parser.ParseExpression(column.GetExpression())
	if err != nil {
		// If expression can't be parsed (e.g., Jinja), fall back to treating as normal
		r.selectItems = append(r.selectItems, r.getSelectItemsExpression(column, ""))
		r.calculatedScopeSelectItems.put(column.Name, column.GetExpression())
		return
	}
	infos, err := analyzer.GetRelationships(expr, r.mdl, baseModel)
	if err != nil {
		// Fall back
		r.selectItems = append(r.selectItems, r.getSelectItemsExpression(column, ""))
		r.calculatedScopeSelectItems.put(column.Name, column.GetExpression())
		return
	}

	if column.IsCalculated {
		if len(infos) > 0 {
			if !r.requiredFields[column.Name] {
				return
			}
			info := newCalculatedFieldRelationshipInfo(column, infos)
			r.calculatedRequiredRelationshipInfos = append(r.calculatedRequiredRelationshipInfos, info)

			for _, ei := range infos {
				for _, rel := range ei.Relationships() {
					for _, mName := range rel.Models {
						if mName != baseModel.Name {
							r.requiredObjects[mName] = true
						}
					}
				}
			}

			if info.isAggregated {
				r.selectItems = append(r.selectItems, r.getSelectItemsExpression(column, info.alias()))
			} else {
				r.selectItems = append(r.selectItems, r.getSelectItemsExpression(column, getRelationableAlias(baseModel.Name)))
			}
			return
		}
		// calculated field without relationship
		r.selectItems = append(r.selectItems, r.getSelectItemsExpression(column, ""))
		r.calculatedScopeSelectItems.put(column.Name, column.GetExpression())
		return
	}

	// normal column with expression
	r.selectItems = append(r.selectItems, r.getSelectItemsExpression(column, ""))
	r.calculatedScopeSelectItems.put(column.Name, fmt.Sprintf(`"%s"."%s"`, baseModel.Name, column.Name))
}

func (r *modelSqlRender) getCalculatedSubQuery(baseModel *dto.Model, relationshipInfos []*calculatedFieldRelationshipInfo) []subQueryJoinInfo {
	var result []subQueryJoinInfo
	result = append(result, r.getToOneRelationshipsQuery(baseModel, relationshipInfos)...)
	result = append(result, r.getToManyRelationshipsQuery(baseModel, relationshipInfos)...)
	return result
}

func (r *modelSqlRender) getToOneRelationshipsQuery(baseModel *dto.Model, relationshipInfos []*calculatedFieldRelationshipInfo) []subQueryJoinInfo {
	var toOneInfos []*calculatedFieldRelationshipInfo
	for _, info := range relationshipInfos {
		if !info.isAggregated {
			toOneInfos = append(toOneInfos, info)
		}
	}
	if len(toOneInfos) == 0 {
		return nil
	}

	var exprs []string
	for _, info := range toOneInfos {
		expr, _ := parseExpression(info.column.GetExpression())
		rewritten := rewriteRelationship(info.expressionRelationship, expr)
		exprs = append(exprs, fmt.Sprintf("%s AS \"%s\"", formatter.FormatExpression(rewritten), info.alias()))
	}
	requiredExpressions := strings.Join(exprs, ", ")

	// Collect unique required relationships
	seenRels := map[string]*dto.Relationship{}
	for _, info := range toOneInfos {
		for _, ei := range info.expressionRelationship {
			for _, rel := range ei.Relationships() {
				seenRels[rel.Name] = rel
			}
		}
	}
	var requiredRelationships []*dto.Relationship
	for _, rel := range seenRels {
		requiredRelationships = append(requiredRelationships, rel)
	}

	var joinClauses []string
	for _, rel := range requiredRelationships {
		cond, err := qualifiedConditionString(rel.Condition)
		if err != nil {
			cond = rel.Condition
		}
		joinClauses = append(joinClauses, fmt.Sprintf(` LEFT JOIN "%s" ON %s`, rel.Models[1], cond))
	}

	tableJoins := fmt.Sprintf("(%s) AS \"%s\"%s",
		r.getBaseModelSql(baseModel), baseModel.Name, strings.Join(joinClauses, ""))

	joinCriteria := fmt.Sprintf(`"%s"."%s" = "%s"."%s"`, baseModel.Name, baseModel.PrimaryKey, getRelationableAlias(baseModel.Name), baseModel.PrimaryKey)

	sql := fmt.Sprintf("SELECT \"%s\".\"%s\", %s FROM (%s)",
		baseModel.Name, baseModel.PrimaryKey, requiredExpressions, tableJoins)

	return []subQueryJoinInfo{{
		sql:           sql,
		subqueryAlias: getRelationableAlias(baseModel.Name),
		joinCriteria:  joinCriteria,
	}}
}

func (r *modelSqlRender) getToManyRelationshipsQuery(baseModel *dto.Model, relationshipInfos []*calculatedFieldRelationshipInfo) []subQueryJoinInfo {
	var result []subQueryJoinInfo
	for _, info := range relationshipInfos {
		if !info.isAggregated {
			continue
		}
		expr, _ := parseExpression(info.column.GetExpression())
		rewritten := rewriteRelationship(info.expressionRelationship, expr)
		requiredExpressions := fmt.Sprintf("%s AS \"%s\"", formatter.FormatExpression(rewritten), info.alias())

		seenRels := map[string]*dto.Relationship{}
		for _, ei := range info.expressionRelationship {
			for _, rel := range ei.Relationships() {
				seenRels[rel.Name] = rel
			}
		}
		var joinClauses []string
		for _, rel := range seenRels {
			cond, err := qualifiedConditionString(rel.Condition)
			if err != nil {
				cond = rel.Condition
			}
			joinClauses = append(joinClauses, fmt.Sprintf(` LEFT JOIN "%s" ON %s`, rel.Models[1], cond))
		}

		tableJoins := fmt.Sprintf("(%s) AS \"%s\"%s",
			r.getBaseModelSql(baseModel), baseModel.Name, strings.Join(joinClauses, ""))

		joinCriteria := fmt.Sprintf(`"%s"."%s" = "%s"."%s"`, baseModel.Name, baseModel.PrimaryKey, info.alias(), baseModel.PrimaryKey)

		sql := fmt.Sprintf("SELECT \"%s\".\"%s\", %s FROM (%s) GROUP BY 1",
			baseModel.Name, baseModel.PrimaryKey, requiredExpressions, tableJoins)

		result = append(result, subQueryJoinInfo{
			sql:           sql,
			subqueryAlias: info.alias(),
			joinCriteria:  joinCriteria,
		})
	}
	return result
}

func (r *modelSqlRender) getBaseModelSql(model *dto.Model) string {
	var cols []string
	for i := range model.Columns {
		col := &model.Columns[i]
		if !col.IsCalculated && col.Relationship == "" {
			cols = append(cols, fmt.Sprintf("%s AS \"%s\"", col.GetExpression(), col.Name))
		}
	}
	if len(cols) == 0 {
		// All columns are calculated or relationships; select primary key so
		// JOINs and subqueries still have an anchor column.
		cols = append(cols, fmt.Sprintf(`"%s"."%s"`, model.Name, model.PrimaryKey))
	}
	return fmt.Sprintf("SELECT %s FROM %s AS \"%s\"", strings.Join(cols, ", "), r.refSql, model.Name)
}


