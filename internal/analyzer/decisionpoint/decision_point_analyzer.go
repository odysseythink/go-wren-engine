package decisionpoint

import (
	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

func Analyze(statement ast.Statement, sessionContext *analyzer.SessionContext, wrenMDL *mdl.WrenMDL) []*QueryAnalysis {
	analysis := rewriteAnalyzer.NewAnalysis(statement)
	rewriteAnalyzer.Analyze(analysis, statement, sessionContext, wrenMDL)
	v := &dpVisitor{analysis: analysis, sessionContext: sessionContext, wrenMDL: wrenMDL}
	v.walk(statement, nil)
	return v.queries
}

type dpVisitor struct {
	analysis       *rewriteAnalyzer.Analysis
	sessionContext *analyzer.SessionContext
	wrenMDL        *mdl.WrenMDL
	queries        []*QueryAnalysis
}

func (v *dpVisitor) walk(node ast.Node, ctx *DecisionPointContext) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.With:
		for _, child := range node.GetChildren() {
			v.walk(child, WithSubqueryOrCte(ctx, true))
		}
	case *ast.TableSubquery:
		for _, child := range node.GetChildren() {
			v.walk(child, WithSubqueryOrCte(ctx, true))
		}
	case *ast.QuerySpecification:
		builder := NewQueryAnalysisBuilder()
		scope := v.analysis.GetScope(n)
		selfCtx := &DecisionPointContext{builder: builder, scope: scope, isSubqueryOrCte: IsSubqueryOrCte(ctx)}
		if n.Select != nil {
			for _, item := range n.Select.SelectItems {
				v.processSelectItem(item, selfCtx)
			}
		}
		if n.From != nil {
			builder.SetRelation(AnalyzeRelation(n.From, v.sessionContext, v.wrenMDL, v.analysis))
		}
		if n.Where != nil {
			builder.SetFilter(AnalyzeFilter(n.Where, selfCtx.Scope()))
		}
		if n.GroupBy != nil {
			builder.SetGroupByKeys(v.analyzeGroupBy(n.GroupBy, selfCtx))
		}
		if len(n.OrderBy) > 0 {
			builder.SetSortings(v.analyzeOrderBy(n.OrderBy, selfCtx))
		}
		v.queries = append(v.queries, builder.Build())
	default:
		for _, child := range node.GetChildren() {
			v.walk(child, ctx)
		}
	}
}

func (v *dpVisitor) processSelectItem(item ast.SelectItem, ctx *DecisionPointContext) {
	switch n := item.(type) {
	case *ast.AllColumns:
		v.processAllColumns(n, ctx)
	case *ast.SingleColumn:
		exprAnalysis := AnalyzeDecisionExpression(n.Expression)
		exprStr := formatter.FormatExpression(n.Expression)
		exprSources := expressionSourceAnalyze(n.Expression, ctx.Scope())
		var alias *string
		if n.Alias != nil {
			alias = &n.Alias.Value
		}
		ctx.Builder().AddSelectItem(ColumnAnalysis{AliasName: alias, Expression: exprStr, Properties: exprAnalysis.ToMap(), NodeLocation: n.GetLocation(), ExprSources: exprSources})
	}
}

func (v *dpVisitor) processAllColumns(node *ast.AllColumns, ctx *DecisionPointContext) {
	scope := ctx.Scope()
	fields := scope.RelationType().Fields()
	if node.QualifiedName != nil {
		// Java SqlFormatter.formatExpression(target, DEFAULT) on a QualifiedName
		// yields dotted text. Mirror with QualifiedName.String().
		target := node.QualifiedName.String()
		if len(fields) == 0 {
			// Remote / unanalyzable relation — emit `<target>.*` placeholder.
			ctx.Builder().AddSelectItem(ColumnAnalysis{Expression: target + ".*", Properties: DefaultAnalysis.ToMap(), NodeLocation: node.GetLocation()})
			return
		}
		// Risk #4: Java filters by relationAlias.toString() == target OR
		// field.getTableName() == toCatalogSchemaTableName(target). Go must
		// match — without this filter, `t.*` emits ALL scoped fields.
		csn := rewriteAnalyzer.ToCatalogSchemaTableName(v.sessionContext, node.QualifiedName)
		for _, f := range fields {
			if !(matchesTarget(f, target, csn)) {
				continue
			}
			if f.SourceColumn() == nil || f.SourceColumn().Relationship != "" || f.SourceColumn().IsCalculated {
				continue
			}
			emitAllColumnsField(ctx.Builder(), f, node.GetLocation())
		}
	} else {
		if len(fields) == 0 {
			ctx.Builder().AddSelectItem(ColumnAnalysis{Expression: "*", Properties: DefaultAnalysis.ToMap(), NodeLocation: node.GetLocation()})
			return
		}
		for _, f := range fields {
			if f.SourceColumn() == nil || f.SourceColumn().Relationship != "" || f.SourceColumn().IsCalculated {
				continue
			}
			emitAllColumnsField(ctx.Builder(), f, node.GetLocation())
		}
	}
}

// matchesTarget mirrors Java filter:
//
//	field.getRelationAlias().filter(alias -> alias.toString().equals(target)).isPresent()
//	|| field.getTableName().equals(catalogSchemaTableName)
func matchesTarget(f *rewriteAnalyzer.Field, target string, csn rewriteAnalyzer.CatalogSchemaTableName) bool {
	if alias := f.RelationAlias(); alias != nil && alias.String() == target {
		return true
	}
	return f.TableName() == csn
}

func emitAllColumnsField(b *QueryAnalysisBuilder, f *rewriteAnalyzer.Field, loc *ast.NodeLocation) {
	name := safeString(f.Name())
	if name == "" {
		name = f.ColumnName()
	}
	src := ExprSource{
		Expression:    name,
		SourceDataset: f.TableName().Table, // Java field.getTableName().getSchemaTableName().getTableName()
		SourceColumn:  safeColumnName(f.SourceColumn()),
		NodeLocation:  loc,
	}
	b.AddSelectItem(ColumnAnalysis{
		Expression:   name,
		Properties:   DefaultAnalysis.ToMap(),
		NodeLocation: loc,
		ExprSources:  []ExprSource{src},
	})
}

func (v *dpVisitor) analyzeGroupBy(node *ast.GroupBy, ctx *DecisionPointContext) [][]GroupByKey {
	var groups [][]GroupByKey
	for _, expr := range node.Expressions {
		var keys []GroupByKey
		if lit, ok := expr.(*ast.LongLiteral); ok {
			idx := int(lit.Value) - 1
			if idx >= 0 && idx < len(ctx.Builder().GetSelectItems()) {
				field := ctx.Builder().GetSelectItems()[idx]
				exprStr := field.Expression
				if field.AliasName != nil {
					exprStr = *field.AliasName
				}
				keys = append(keys, GroupByKey{Expression: exprStr, NodeLocation: expr.GetLocation(), ExprSources: field.ExprSources})
			}
		} else {
			exprSources := expressionSourceAnalyze(expr, ctx.Scope())
			keys = append(keys, GroupByKey{Expression: formatter.FormatExpression(expr), NodeLocation: expr.GetLocation(), ExprSources: exprSources})
		}
		groups = append(groups, keys)
	}
	return groups
}

func (v *dpVisitor) analyzeOrderBy(items []ast.SortItem, ctx *DecisionPointContext) []SortItemAnalysis {
	var result []SortItemAnalysis
	for _, si := range items {
		if lit, ok := si.SortKey.(*ast.LongLiteral); ok {
			idx := int(lit.Value) - 1
			if idx >= 0 && idx < len(ctx.Builder().GetSelectItems()) {
				field := ctx.Builder().GetSelectItems()[idx]
				exprStr := field.Expression
				if field.AliasName != nil {
					exprStr = *field.AliasName
				}
				result = append(result, SortItemAnalysis{Expression: exprStr, Ordering: si.Ordering, NodeLocation: si.GetLocation(), ExprSources: field.ExprSources})
			}
		} else {
			exprSources := expressionSourceAnalyze(si.SortKey, ctx.Scope())
			result = append(result, SortItemAnalysis{Expression: formatter.FormatExpression(si.SortKey), Ordering: si.Ordering, NodeLocation: si.GetLocation(), ExprSources: exprSources})
		}
	}
	return result
}

// orderingToJavaName maps Go's ast.Ordering ("ASC"/"DESC") to Java's
// SortItem.Ordering.name() ("ASCENDING"/"DESCENDING") — risk #3. Called from
// the DTO mapper layer (slice 6). Kept here so slice 4 code stays parser-form.
func orderingToJavaName(o ast.Ordering) string {
	switch o {
	case ast.OrderingDesc:
		return "DESCENDING"
	default:
		return "ASCENDING"
	}
}
