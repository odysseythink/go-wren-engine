package analyzer

import (
	"fmt"
	"strings"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// Analyze runs the statement analyzer. Mirrors StatementAnalyzer.analyze.
func Analyze(analysis *Analysis, statement ast.Statement, ctx *base.SessionContext, wrenMDL *mdl.WrenMDL) (*Scope, error) {
	v := &stmtVisitor{ctx: ctx, analysis: analysis, wrenMDL: wrenMDL}
	queryScope, err := v.process(statement, nil)
	if err != nil {
		return nil, err
	}
	// Add directly referenced models to analysis
	var models []*dto.Model
	for _, model := range wrenMDL.ListModels() {
		for _, t := range analysis.Tables() {
			if t.Catalog == wrenMDL.Catalog() && t.Schema == wrenMDL.Schema() && t.Table == model.Name {
				models = append(models, model)
				break
			}
		}
	}
	analysis.AddModels(models)

	// metrics referenced as plain tables
	var metrics []*dto.Metric
	for _, t := range analysis.Tables() {
		if t.Catalog == wrenMDL.Catalog() && t.Schema == wrenMDL.Schema() {
			if m, ok := wrenMDL.GetMetric(t.Table); ok {
				metrics = append(metrics, m)
			}
		}
	}
	// a metric must not appear both as a table and as a rollup target
	rollupMetrics := map[string]bool{}
	for _, info := range analysis.MetricRollups() {
		rollupMetrics[info.Metric.Name] = true
	}
	for _, m := range metrics {
		if rollupMetrics[m.Name] {
			return nil, fmt.Errorf("duplicate metrics in metrics and metric rollups")
		}
	}
	analysis.AddMetrics(metrics)

	var cumulativeMetrics []*dto.CumulativeMetric
	for _, t := range analysis.Tables() {
		if t.Catalog == wrenMDL.Catalog() && t.Schema == wrenMDL.Schema() {
			if cm, ok := wrenMDL.GetCumulativeMetric(t.Table); ok {
				cumulativeMetrics = append(cumulativeMetrics, cm)
			}
		}
	}
	analysis.AddCumulativeMetrics(cumulativeMetrics)

	// views referenced as plain tables
	var views []*dto.View
	for _, t := range analysis.Tables() {
		if t.Catalog == wrenMDL.Catalog() && t.Schema == wrenMDL.Schema() {
			if v, ok := wrenMDL.GetView(t.Table); ok {
				views = append(views, v)
			}
		}
	}
	analysis.AddViews(views)

	return queryScope, nil
}

type stmtVisitor struct {
	ctx      *base.SessionContext
	analysis *Analysis
	wrenMDL  *mdl.WrenMDL
}

func (v *stmtVisitor) process(node ast.Node, scope *Scope) (*Scope, error) {
	switch n := node.(type) {
	case *ast.Query:
		return v.visitQuery(n, scope)
	case *ast.QuerySpecification:
		return v.visitQuerySpecification(n, scope)
	case *ast.Table:
		return v.visitTable(n, scope)
	case *ast.Join:
		return v.visitJoin(n, scope)
	case *ast.AliasedRelation:
		return v.visitAliasedRelation(n, scope)
	case *ast.TableSubquery:
		return v.visitTableSubquery(n, scope)
	case *ast.SetOperation:
		return v.visitSetOperation(n, scope)
	case *ast.Values:
		return v.visitValues(n, scope)
	case *ast.Unnest:
		return v.visitUnnest(n, scope)
	case *ast.FunctionRelation:
		return v.visitFunctionRelation(n, scope)
	case *ast.Lateral:
		return v.visitLateral(n, scope)
	default:
		return nil, fmt.Errorf("unsupported node type in StatementAnalyzer: %T", node)
	}
}

func (v *stmtVisitor) visitQuery(n *ast.Query, scope *Scope) (*Scope, error) {
	queryScope, err := v.createScopeForQuery(n, scope)
	if err != nil {
		return nil, err
	}
	if n.With != nil {
		if err := v.analyzeWith(n.With, queryScope); err != nil {
			return nil, err
		}
	}
	bodyScope, err := v.process(n.Body, queryScope)
	if err != nil {
		return nil, err
	}
	// order by / limit / offset are analyzed against bodyScope
	for _, si := range n.OrderBy {
		v.analyzeExpression(bodyScope, si.SortKey)
	}
	if n.Limit != nil {
		v.analyzeExpression(bodyScope, n.Limit)
	}
	if n.Offset != nil {
		v.analyzeExpression(bodyScope, n.Offset)
	}
	return v.createAndAssignScope(n, bodyScope), nil
}

func (v *stmtVisitor) visitQuerySpecification(n *ast.QuerySpecification, scope *Scope) (*Scope, error) {
	fromScope, err := v.analyzeFrom(n.From, scope)
	if err != nil {
		return nil, err
	}
	if err := v.analyzeSelect(n.Select, fromScope); err != nil {
		return nil, err
	}
	if n.Where != nil {
		v.analyzeExpression(fromScope, n.Where)
	}
	if n.GroupBy != nil {
		for _, e := range n.GroupBy.Expressions {
			v.analyzeExpression(fromScope, e)
		}
	}
	if n.Having != nil {
		v.analyzeExpression(fromScope, n.Having)
	}
	for _, si := range n.OrderBy {
		v.analyzeExpression(fromScope, si.SortKey)
	}
	if n.Limit != nil {
		v.analyzeExpression(fromScope, n.Limit)
	}
	if n.Offset != nil {
		v.analyzeExpression(fromScope, n.Offset)
	}
	return v.createAndAssignScope(n, fromScope), nil
}

func (v *stmtVisitor) visitTable(n *ast.Table, scope *Scope) (*Scope, error) {
	// Check if shadowed by a WITH CTE
	if scope != nil {
		last := n.Name.Last()
		if q := scope.GetNamedQuery(last); q != nil {
			return v.createScopeForCommonTableExpression(q, scope), nil
		}
	}

	cstn, err := toCatalogSchemaTableName(v.ctx, n.Name)
	if err != nil {
		return nil, err
	}
	v.analysis.AddTable(cstn)

	var rt *RelationType
	// If catalog+schema match MDL, register source node name and collect fields
	if cstn.Catalog == v.wrenMDL.Catalog() && cstn.Schema == v.wrenMDL.Schema() {
		v.analysis.AddSourceNodeName(n, ast.QualifiedNameOf(cstn.Table))
		model, ok := v.wrenMDL.GetModel(cstn.Table)
		if ok {
			v.collectFieldFromMDL(n, model)
			var fields []*Field
			for i := range model.Columns {
				col := &model.Columns[i]
				name := col.Name
				fields = append(fields, &Field{
					tableName:         CatalogSchemaTableName{Catalog: v.wrenMDL.Catalog(), Schema: v.wrenMDL.Schema(), Table: model.Name},
					columnName:        name,
					name:              &name,
					sourceDatasetName: &model.Name,
					sourceColumn:      col,
				})
			}
			rt = NewRelationType(fields)
		} else if metric, ok := v.wrenMDL.GetMetric(cstn.Table); ok {
			var fields []*Field
			for i := range metric.Dimension {
				col := &metric.Dimension[i]
				name := col.Name
				fields = append(fields, &Field{
					tableName:         CatalogSchemaTableName{Catalog: v.wrenMDL.Catalog(), Schema: v.wrenMDL.Schema(), Table: metric.Name},
					columnName:        name,
					name:              &name,
					sourceDatasetName: &metric.Name,
					sourceColumn:      col,
				})
			}
			for i := range metric.Measure {
				col := &metric.Measure[i]
				name := col.Name
				fields = append(fields, &Field{
					tableName:         CatalogSchemaTableName{Catalog: v.wrenMDL.Catalog(), Schema: v.wrenMDL.Schema(), Table: metric.Name},
					columnName:        name,
					name:              &name,
					sourceDatasetName: &metric.Name,
					sourceColumn:      col,
				})
			}
			v.analysis.AddCollectedColumns(fields)
			rt = NewRelationType(fields)
		} else if cm, ok := v.wrenMDL.GetCumulativeMetric(cstn.Table); ok {
			var fields []*Field
			windowCol := cm.Window.ToColumn()
			winName := windowCol.Name
			fields = append(fields, &Field{
				tableName:         CatalogSchemaTableName{Catalog: v.wrenMDL.Catalog(), Schema: v.wrenMDL.Schema(), Table: cm.Name},
				columnName:        winName,
				name:              &winName,
				sourceDatasetName: &cm.Name,
				sourceColumn:      &windowCol,
			})
			measureCol := cm.Measure.ToColumn()
			measName := measureCol.Name
			fields = append(fields, &Field{
				tableName:         CatalogSchemaTableName{Catalog: v.wrenMDL.Catalog(), Schema: v.wrenMDL.Schema(), Table: cm.Name},
				columnName:        measName,
				name:              &measName,
				sourceDatasetName: &cm.Name,
				sourceColumn:      &measureCol,
			})
			v.analysis.AddCollectedColumns(fields)
			rt = NewRelationType(fields)
		}
	}

	return v.createAndAssignScope(n, ScopeBuilderWithParent(scope).RelationId(RelationIdOf(n)).RelationType(rt).Build()), nil
}

func (v *stmtVisitor) visitJoin(n *ast.Join, scope *Scope) (*Scope, error) {
	leftScope, err := v.process(n.Left, scope)
	if err != nil {
		return nil, err
	}
	rightScope, err := v.process(n.Right, scope)
	if err != nil {
		return nil, err
	}
	joinedType := leftScope.RelationType().JoinWith(rightScope.RelationType())
	joinedScope := ScopeBuilderWithParent(scope).RelationType(joinedType).Build()
	if n.Criteria != nil {
		switch c := n.Criteria.(type) {
		case *ast.JoinOn:
			v.analyzeExpression(joinedScope, c.Expression)
		}
	}
	return v.createAndAssignScope(n, joinedScope), nil
}

func (v *stmtVisitor) visitAliasedRelation(n *ast.AliasedRelation, scope *Scope) (*Scope, error) {
	relationScope, err := v.process(n.Relation, scope)
	if err != nil {
		return nil, err
	}
	if n.Alias != nil {
		alias := ast.QualifiedNameOf(n.Alias.Value)
		fields := relationScope.RelationType().Fields()
		newFields := make([]*Field, len(fields))
		for i, f := range fields {
			newFields[i] = &Field{
				relationAlias:     &alias,
				tableName:         f.tableName,
				columnName:        f.columnName,
				name:              f.name,
				sourceDatasetName: f.sourceDatasetName,
				sourceColumn:      f.sourceColumn,
			}
		}
		return v.createAndAssignScope(n, ScopeBuilderWithParent(scope).RelationType(NewRelationType(newFields)).Build()), nil
	}
	return v.createAndAssignScope(n, relationScope), nil
}

func (v *stmtVisitor) visitTableSubquery(n *ast.TableSubquery, scope *Scope) (*Scope, error) {
	subqueryScope, err := v.process(n.Query, scope)
	if err != nil {
		return nil, err
	}
	return v.createAndAssignScope(n, subqueryScope), nil
}

func (v *stmtVisitor) visitSetOperation(n *ast.SetOperation, scope *Scope) (*Scope, error) {
	var resultType *RelationType
	for _, rel := range n.Relations {
		relScope, err := v.process(rel, scope)
		if err != nil {
			return nil, err
		}
		if resultType == nil {
			resultType = relScope.RelationType()
		} else {
			resultType = resultType.JoinWith(relScope.RelationType())
		}
	}
	if resultType == nil {
		resultType = NewRelationType(nil)
	}
	return v.createAndAssignScope(n, ScopeBuilderWithParent(scope).RelationType(resultType).Build()), nil
}

func (v *stmtVisitor) visitValues(n *ast.Values, scope *Scope) (*Scope, error) {
	return v.createAndAssignScope(n, ScopeBuilderWithParent(scope).Build()), nil
}

func (v *stmtVisitor) visitUnnest(n *ast.Unnest, scope *Scope) (*Scope, error) {
	for _, e := range n.Expressions {
		v.analyzeExpression(scope, e)
	}
	return v.createAndAssignScope(n, ScopeBuilderWithParent(scope).Build()), nil
}

func (v *stmtVisitor) visitFunctionRelation(n *ast.FunctionRelation, scope *Scope) (*Scope, error) {
	if strings.EqualFold(n.Name.String(), "roll_up") {
		args := n.Arguments
		if len(args) != 3 {
			return nil, fmt.Errorf("rollup function should have 3 arguments")
		}
		tableName := ast.GetQualifiedName(args[0])
		if tableName == nil {
			return nil, fmt.Errorf("'%v' cannot be resolved", args[0])
		}
		timeId, ok1 := args[1].(*ast.Identifier)
		if !ok1 {
			return nil, fmt.Errorf("'%v' cannot be resolved", args[1])
		}
		unitId, ok2 := args[2].(*ast.Identifier)
		if !ok2 {
			return nil, fmt.Errorf("'%v' cannot be resolved", args[2])
		}
		cstn, err := toCatalogSchemaTableName(v.ctx, *tableName)
		if err != nil {
			return nil, err
		}
		var metric *dto.Metric
		if cstn.Catalog == v.wrenMDL.Catalog() && cstn.Schema == v.wrenMDL.Schema() {
			if m, found := v.wrenMDL.GetMetric(cstn.Table); found {
				metric = m
			}
		}
		if metric == nil {
			return nil, fmt.Errorf("Metric not found: %s.%s.%s", cstn.Catalog, cstn.Schema, cstn.Table)
		}
		timeGrain, found := metric.GetTimeGrain(timeId.Value)
		if !found {
			return nil, fmt.Errorf("Time column not found in metric: %s", timeId.Value)
		}
		unit, err := dto.ParseTimeUnit(unitId.Value)
		if err != nil {
			return nil, err
		}
		v.analysis.AddMetricRollups(n, &MetricRollupInfo{Metric: metric, TimeGrain: timeGrain, TimeUnit: unit})
		return v.createAndAssignScope(n, ScopeBuilderWithParent(scope).Build()), nil
	}
	for _, arg := range n.Arguments {
		v.analyzeExpression(scope, arg)
	}
	return v.createAndAssignScope(n, ScopeBuilderWithParent(scope).Build()), nil
}

func (v *stmtVisitor) visitLateral(n *ast.Lateral, scope *Scope) (*Scope, error) {
	_, err := v.process(n.Query, scope)
	if err != nil {
		return nil, err
	}
	return v.createAndAssignScope(n, ScopeBuilderWithParent(scope).Build()), nil
}

func (v *stmtVisitor) analyzeWith(w *ast.With, scope *Scope) error {
	namedQueries := map[string]*ast.WithQuery{}
	for i := range w.Queries {
		q := &w.Queries[i]
		namedQueries[q.Name.Value] = q
		// Recursively analyze CTE bodies so model references inside WITH
		// clauses are detected and expanded ( mirrors Java StatementAnalyzer ).
		if _, err := v.process(q.Query, scope); err != nil {
			return err
		}
	}
	scope.namedQueries = namedQueries
	return nil
}

func (v *stmtVisitor) analyzeFrom(from ast.Relation, scope *Scope) (*Scope, error) {
	if from == nil {
		return ScopeBuilderWithParent(scope).Build(), nil
	}
	return v.process(from, scope)
}

func (v *stmtVisitor) analyzeSelect(sel *ast.Select, scope *Scope) error {
	if sel == nil {
		return nil
	}
	for _, item := range sel.SelectItems {
		switch n := item.(type) {
		case *ast.SingleColumn:
			if err := v.analyzeSelectSingleColumn(n, scope); err != nil {
				return err
			}
		case *ast.AllColumns:
			// nothing
		}
	}
	return nil
}

func (v *stmtVisitor) analyzeSelectSingleColumn(col *ast.SingleColumn, scope *Scope) error {
	exprAnalysis := AnalyzeExpression(scope, col.Expression, v.ctx, v.wrenMDL, v.analysis)
	if exprAnalysis.RequireRelation() {
		source := scope.RelationId().SourceNode()
		if source == nil {
			return fmt.Errorf("count(*) requires a source relation")
		}
		v.analysis.AddRequiredSourceNode(col.Expression, source)
	}
	return nil
}

func (v *stmtVisitor) analyzeExpression(scope *Scope, expr ast.Expression) {
	if expr == nil {
		return
	}
	AnalyzeExpression(scope, expr, v.ctx, v.wrenMDL, v.analysis)
}

func (v *stmtVisitor) collectFieldFromMDL(table *ast.Table, model *dto.Model) {
	var fields []*Field
	for i := range model.Columns {
		col := &model.Columns[i]
		name := col.Name
		fields = append(fields, &Field{
			tableName:         CatalogSchemaTableName{Catalog: v.wrenMDL.Catalog(), Schema: v.wrenMDL.Schema(), Table: model.Name},
			columnName:        name,
			name:              &name,
			sourceDatasetName: &model.Name,
			sourceColumn:      col,
		})
	}
	v.analysis.AddCollectedColumns(fields)
}

func (v *stmtVisitor) createScopeForQuery(query *ast.Query, parent *Scope) (*Scope, error) {
	return ScopeBuilderWithParent(parent).Build(), nil
}

func (v *stmtVisitor) createScopeForCommonTableExpression(q *ast.WithQuery, parent *Scope) *Scope {
	return ScopeBuilderWithParent(parent).Build()
}

func (v *stmtVisitor) createAndAssignScope(node ast.Node, scope *Scope) *Scope {
	v.analysis.SetScope(node, scope)
	return scope
}
