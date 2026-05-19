package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// WrenSqlRewrite is the main rewrite rule for model/metric expansion.
type WrenSqlRewrite struct{}

func (r *WrenSqlRewrite) Apply(stmt ast.Statement, ctx *analyzer.SessionContext, analyzedMDL *mdl.AnalyzedMDL) ast.Statement {
	analysis := AnalyzeStatement(stmt, ctx, analyzedMDL.WrenMDL())
	if len(analysis.Models) == 0 && len(analysis.Metrics) == 0 && len(analysis.CumulativeMetrics) == 0 {
		return stmt
	}

	// Build descriptors for all referenced objects
	descriptors := make(map[string]*QueryDescriptor)
	graph := make(map[string][]string)

	for _, model := range analysis.Models {
		desc := BuildModelDescriptor(model)
		descriptors[model.Name] = desc
		// Model dependencies: if RefSql references other models, parse and find them
		addDependencies(desc, graph, analyzedMDL.WrenMDL())
	}

	for _, metric := range analysis.Metrics {
		desc := BuildMetricDescriptor(metric)
		descriptors[metric.Name] = desc
		// Metric depends on its base object
		if metric.BaseObject != "" {
			graph[metric.Name] = append(graph[metric.Name], metric.BaseObject)
		}
		addDependencies(desc, graph, analyzedMDL.WrenMDL())
	}

	for _, cm := range analysis.CumulativeMetrics {
		desc := BuildCumulativeMetricDescriptor(cm)
		descriptors[cm.Name] = desc
		// Cumulative metric depends on base object and date_spine
		if cm.BaseObject != "" {
			graph[cm.Name] = append(graph[cm.Name], cm.BaseObject)
		}
		addDependencies(desc, graph, analyzedMDL.WrenMDL())
	}

	// Topological sort
	order, err := topologicalSort(graph)
	if err != nil {
		panic(fmt.Sprintf("model/metric dependency cycle: %v", err))
	}

	// Build CTEs
	var ctes []ast.WithQuery
	for _, name := range order {
		desc := descriptors[name]
		parsed, err := parser.ParseSQL(desc.SQL)
		if err != nil {
			panic(fmt.Sprintf("failed to parse descriptor SQL for %s: %v", name, err))
		}
		ctes = append(ctes, ast.WithQuery{
			Name:  &ast.Identifier{Value: name},
			Query: parsed,
		})
	}

	// Prepend CTEs to query
	query, ok := stmt.(*ast.Query)
	if !ok {
		qs, ok := stmt.(*ast.QuerySpecification)
		if !ok {
			return stmt
		}
		query = &ast.Query{Body: qs}
	}
	PrependCTEs(query, ctes)

	// Replace table references with CTE names and remove catalog/schema prefixes
	return rewriteWrenTables(query, analyzedMDL.WrenMDL()).(ast.Statement)
}

func addDependencies(desc *QueryDescriptor, graph map[string][]string, wrenMDL *mdl.WrenMDL) {
	// Parse the descriptor SQL to find referenced tables that are Wren objects
	parsed, err := parser.ParseSQL(desc.SQL)
	if err != nil {
		return
	}
	analysis := AnalyzeStatement(parsed, nil, wrenMDL)
	for _, table := range analysis.Tables {
		if table != desc.Name {
			graph[desc.Name] = append(graph[desc.Name], table)
		}
	}
}

func rewriteWrenTables(node ast.Node, wrenMDL *mdl.WrenMDL) ast.Node {
	if node == nil {
		return nil
	}

	switch n := node.(type) {
	case *ast.Table:
		name := n.Name.String()
		// If this table refers to a Wren object, strip catalog/schema and use just the name
		if _, found := wrenMDL.GetModel(name); found {
			return &ast.Table{Name: ast.QualifiedNameOf(n.Name.Last())}
		}
		if _, found := wrenMDL.GetMetric(name); found {
			return &ast.Table{Name: ast.QualifiedNameOf(n.Name.Last())}
		}
		if _, found := wrenMDL.GetCumulativeMetric(name); found {
			return &ast.Table{Name: ast.QualifiedNameOf(n.Name.Last())}
		}
		return n
	case *ast.DereferenceExpression:
		// Remove catalog/schema prefixes from dereferences if they match Wren catalog/schema
		qn := ast.GetQualifiedName(n)
		if qn != nil && len(qn.Parts) >= 2 {
			catalog := wrenMDL.Catalog()
			schema := wrenMDL.Schema()
			if catalog != "" && schema != "" {
				if len(qn.Parts) >= 3 && qn.Parts[0] == catalog && qn.Parts[1] == schema {
					// catalog.schema.table.column -> table.column
					newParts := qn.Parts[2:]
					return dereferenceFromQualifiedName(ast.QualifiedNameOf(newParts...))
				}
			}
			if schema != "" && qn.Parts[0] == schema {
				// schema.table.column -> table.column
				newParts := qn.Parts[1:]
				return dereferenceFromQualifiedName(ast.QualifiedNameOf(newParts...))
			}
		}
		// Recurse into base
		n.Base = rewriteWrenTables(n.Base, wrenMDL).(ast.Expression)
		return n
	case *ast.Query:
		if n.With != nil {
			n.With = rewriteWrenTables(n.With, wrenMDL).(*ast.With)
		}
		if n.Body != nil {
			n.Body = rewriteWrenTables(n.Body, wrenMDL).(ast.QueryBody)
		}
		for i := range n.OrderBy {
			n.OrderBy[i].SortKey = rewriteWrenTables(n.OrderBy[i].SortKey, wrenMDL).(ast.Expression)
		}
		if n.Limit != nil {
			n.Limit = rewriteWrenTables(n.Limit, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.QuerySpecification:
		if n.Select != nil {
			for i, item := range n.Select.SelectItems {
				n.Select.SelectItems[i] = rewriteWrenSelectItem(item, wrenMDL)
			}
		}
		if n.From != nil {
			n.From = rewriteWrenTables(n.From, wrenMDL).(ast.Relation)
		}
		if n.Where != nil {
			n.Where = rewriteWrenTables(n.Where, wrenMDL).(ast.Expression)
		}
		if n.GroupBy != nil {
			for i, expr := range n.GroupBy.Expressions {
				n.GroupBy.Expressions[i] = rewriteWrenTables(expr, wrenMDL).(ast.Expression)
			}
		}
		if n.Having != nil {
			n.Having = rewriteWrenTables(n.Having, wrenMDL).(ast.Expression)
		}
		for i := range n.OrderBy {
			n.OrderBy[i].SortKey = rewriteWrenTables(n.OrderBy[i].SortKey, wrenMDL).(ast.Expression)
		}
		if n.Limit != nil {
			n.Limit = rewriteWrenTables(n.Limit, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.With:
		for i := range n.Queries {
			n.Queries[i].Query = rewriteWrenTables(n.Queries[i].Query, wrenMDL).(ast.Statement)
		}
		return n
	case *ast.Join:
		n.Left = rewriteWrenTables(n.Left, wrenMDL).(ast.Relation)
		n.Right = rewriteWrenTables(n.Right, wrenMDL).(ast.Relation)
		if n.Criteria != nil {
			switch c := n.Criteria.(type) {
			case *ast.JoinOn:
				c.Expression = rewriteWrenTables(c.Expression, wrenMDL).(ast.Expression)
			}
		}
		return n
	case *ast.AliasedRelation:
		n.Relation = rewriteWrenTables(n.Relation, wrenMDL).(ast.Relation)
		return n
	case *ast.TableSubquery:
		n.Query = rewriteWrenTables(n.Query, wrenMDL).(ast.Statement)
		return n
	case *ast.ComparisonExpression:
		n.Left = rewriteWrenTables(n.Left, wrenMDL).(ast.Expression)
		n.Right = rewriteWrenTables(n.Right, wrenMDL).(ast.Expression)
		return n
	case *ast.LogicalBinaryExpression:
		n.Left = rewriteWrenTables(n.Left, wrenMDL).(ast.Expression)
		n.Right = rewriteWrenTables(n.Right, wrenMDL).(ast.Expression)
		return n
	case *ast.NotExpression:
		n.Value = rewriteWrenTables(n.Value, wrenMDL).(ast.Expression)
		return n
	case *ast.ArithmeticBinaryExpression:
		n.Left = rewriteWrenTables(n.Left, wrenMDL).(ast.Expression)
		n.Right = rewriteWrenTables(n.Right, wrenMDL).(ast.Expression)
		return n
	case *ast.FunctionCall:
		for i, arg := range n.Arguments {
			n.Arguments[i] = rewriteWrenTables(arg, wrenMDL).(ast.Expression)
		}
		if n.Filter != nil {
			n.Filter = rewriteWrenTables(n.Filter, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.Cast:
		n.Expression = rewriteWrenTables(n.Expression, wrenMDL).(ast.Expression)
		return n
	case *ast.InPredicate:
		n.Value = rewriteWrenTables(n.Value, wrenMDL).(ast.Expression)
		n.ValueList = rewriteWrenTables(n.ValueList, wrenMDL).(ast.Expression)
		return n
	case *ast.InListExpression:
		for i, v := range n.Values {
			n.Values[i] = rewriteWrenTables(v, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.BetweenPredicate:
		n.Value = rewriteWrenTables(n.Value, wrenMDL).(ast.Expression)
		n.Min = rewriteWrenTables(n.Min, wrenMDL).(ast.Expression)
		n.Max = rewriteWrenTables(n.Max, wrenMDL).(ast.Expression)
		return n
	case *ast.LikePredicate:
		n.Value = rewriteWrenTables(n.Value, wrenMDL).(ast.Expression)
		n.Pattern = rewriteWrenTables(n.Pattern, wrenMDL).(ast.Expression)
		if n.Escape != nil {
			n.Escape = rewriteWrenTables(n.Escape, wrenMDL).(ast.Expression)
		}
		return n
	case *ast.IsNullPredicate:
		n.Value = rewriteWrenTables(n.Value, wrenMDL).(ast.Expression)
		return n
	case *ast.SubqueryExpression:
		n.Query = rewriteWrenTables(n.Query, wrenMDL).(ast.Statement)
		return n
	case *ast.AtTimeZone:
		n.Value = rewriteWrenTables(n.Value, wrenMDL).(ast.Expression)
		n.TimeZone = rewriteWrenTables(n.TimeZone, wrenMDL).(ast.Expression)
		return n
	case *ast.CoalesceExpression:
		for i, o := range n.Operands {
			n.Operands[i] = rewriteWrenTables(o, wrenMDL).(ast.Expression)
		}
		return n
	default:
		return node
	}
}

func rewriteWrenSelectItem(item ast.SelectItem, wrenMDL *mdl.WrenMDL) ast.SelectItem {
	switch s := item.(type) {
	case *ast.SingleColumn:
		s.Expression = rewriteWrenTables(s.Expression, wrenMDL).(ast.Expression)
		return s
	case *ast.AllColumns:
		return s
	}
	return item
}

// dereferenceFromQualifiedName builds a DereferenceExpression tree from a QualifiedName.
// e.g., QualifiedNameOf("a", "b", "c") -> DereferenceExpression{Base: DereferenceExpression{Base: Identifier("a"), Field: Identifier("b")}, Field: Identifier("c")}
func dereferenceFromQualifiedName(qn ast.QualifiedName) ast.Expression {
	if len(qn.Parts) == 0 {
		return nil
	}
	if len(qn.Parts) == 1 {
		return &ast.Identifier{Value: qn.Parts[0]}
	}
	result := &ast.DereferenceExpression{
		Base:  &ast.Identifier{Value: qn.Parts[0]},
		Field: &ast.Identifier{Value: qn.Parts[1]},
	}
	for i := 2; i < len(qn.Parts); i++ {
		result = &ast.DereferenceExpression{
			Base:  result,
			Field: &ast.Identifier{Value: qn.Parts[i]},
		}
	}
	return result
}
