package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// GenerateViewRewrite expands view references into CTEs.
type GenerateViewRewrite struct{}

func (r *GenerateViewRewrite) Apply(stmt ast.Statement, ctx *analyzer.SessionContext, analyzedMDL *mdl.AnalyzedMDL) ast.Statement {
	analysis := AnalyzeStatement(stmt, ctx, analyzedMDL.WrenMDL())
	if len(analysis.Views) == 0 {
		return stmt
	}

	// Build view descriptors and dependency graph
	descriptors := make(map[string]*QueryDescriptor)
	graph := make(map[string][]string)
	visited := make(map[string]bool)

	for _, view := range analysis.Views {
		buildViewGraph(view, descriptors, graph, visited, analyzedMDL.WrenMDL())
	}

	// Topological sort
	order, err := topologicalSort(graph)
	if err != nil {
		panic(fmt.Sprintf("view dependency cycle: %v", err))
	}

	// Build CTEs
	var ctes []ast.WithQuery
	for _, name := range order {
		desc := descriptors[name]
		parsed, err := parseSQL(desc.SQL)
		if err != nil {
			panic(fmt.Sprintf("failed to parse view %s: %v", name, err))
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

	// Replace view table references with CTE names
	replaceViewTables(query, descriptors)

	return query
}

func buildViewGraph(view *dto.View, descriptors map[string]*QueryDescriptor, graph map[string][]string, visited map[string]bool, wrenMDL *mdl.WrenMDL) {
	if visited[view.Name] {
		return
	}
	visited[view.Name] = true

	desc := BuildViewDescriptor(view)
	descriptors[view.Name] = desc

	// Parse view SQL to find its dependencies
	parsed, err := parseSQL(view.Statement)
	if err != nil {
		return
	}
	va := AnalyzeStatement(parsed, nil, wrenMDL)
	for _, depView := range va.Views {
		graph[view.Name] = append(graph[view.Name], depView.Name)
		buildViewGraph(depView, descriptors, graph, visited, wrenMDL)
	}
}

func topologicalSort(graph map[string][]string) ([]string, error) {
	inDegree := make(map[string]int)
	for node := range graph {
		if inDegree[node] == 0 {
			inDegree[node] = 0
		}
	}
	for _, deps := range graph {
		for _, dep := range deps {
			inDegree[dep]++
		}
	}

	var queue []string
	for node, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, node)
		}
	}

	var result []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		for _, dep := range graph[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(result) != len(inDegree) {
		return nil, fmt.Errorf("cycle detected")
	}

	// Reverse for correct dependency order (dependents first)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}

func replaceViewTables(node ast.Node, descriptors map[string]*QueryDescriptor) ast.Node {
	if node == nil {
		return nil
	}

	switch n := node.(type) {
	case *ast.Table:
		if _, ok := descriptors[n.Name.String()]; ok {
			// Replace with just the name (no catalog/schema since it's now a CTE)
			return &ast.Table{Name: ast.QualifiedNameOf(n.Name.Last())}
		}
		return n
	case *ast.Query:
		if n.With != nil {
			for i := range n.With.Queries {
				n.With.Queries[i].Query = replaceViewTables(n.With.Queries[i].Query, descriptors).(ast.Statement)
			}
		}
		if n.Body != nil {
			n.Body = replaceViewTables(n.Body, descriptors).(ast.QueryBody)
		}
		return n
	case *ast.QuerySpecification:
		if n.From != nil {
			n.From = replaceViewTables(n.From, descriptors).(ast.Relation)
		}
		if n.Where != nil {
			n.Where = replaceViewTables(n.Where, descriptors).(ast.Expression)
		}
		if n.Having != nil {
			n.Having = replaceViewTables(n.Having, descriptors).(ast.Expression)
		}
		for i := range n.Select.SelectItems {
			n.Select.SelectItems[i] = replaceViewTablesInSelectItem(n.Select.SelectItems[i], descriptors)
		}
		return n
	case *ast.Join:
		n.Left = replaceViewTables(n.Left, descriptors).(ast.Relation)
		n.Right = replaceViewTables(n.Right, descriptors).(ast.Relation)
		if n.Criteria != nil {
			switch c := n.Criteria.(type) {
			case *ast.JoinOn:
				c.Expression = replaceViewTables(c.Expression, descriptors).(ast.Expression)
			}
		}
		return n
	case *ast.AliasedRelation:
		n.Relation = replaceViewTables(n.Relation, descriptors).(ast.Relation)
		return n
	case *ast.TableSubquery:
		n.Query = replaceViewTables(n.Query, descriptors).(ast.Statement)
		return n
	case *ast.ComparisonExpression:
		n.Left = replaceViewTables(n.Left, descriptors).(ast.Expression)
		n.Right = replaceViewTables(n.Right, descriptors).(ast.Expression)
		return n
	case *ast.LogicalBinaryExpression:
		n.Left = replaceViewTables(n.Left, descriptors).(ast.Expression)
		n.Right = replaceViewTables(n.Right, descriptors).(ast.Expression)
		return n
	case *ast.NotExpression:
		n.Value = replaceViewTables(n.Value, descriptors).(ast.Expression)
		return n
	case *ast.FunctionCall:
		for i, arg := range n.Arguments {
			n.Arguments[i] = replaceViewTables(arg, descriptors).(ast.Expression)
		}
		return n
	default:
		return node
	}
}

func replaceViewTablesInSelectItem(item ast.SelectItem, descriptors map[string]*QueryDescriptor) ast.SelectItem {
	switch s := item.(type) {
	case *ast.SingleColumn:
		s.Expression = replaceViewTables(s.Expression, descriptors).(ast.Expression)
		return s
	}
	return item
}

func parseSQL(sql string) (ast.Statement, error) {
	return parser.ParseSQL(sql)
}
