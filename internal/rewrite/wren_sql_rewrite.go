package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"

	base "github.com/wren-engine/wren/internal/analyzer"
)

type WrenSqlRewrite struct{}

// Apply expands referenced Wren models into CTEs (non-dynamic-field path).
// Mirrors Java WrenSqlRewrite.apply.
func (r *WrenSqlRewrite) Apply(root ast.Statement, ctx *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	wrenMDL := analyzedMDL.WrenMDL()

	analysis := analyzer.NewAnalysis(root)
	if _, err := analyzer.Analyze(analysis, root, ctx, wrenMDL); err != nil {
		return nil, err
	}

	// non-dynamic path: model + metric + cumulative descriptors
	var allDescriptors []QueryDescriptor
	for _, model := range analysis.Models() {
		info, err := relationInfoOfModel(model, wrenMDL)
		if err != nil {
			return nil, err
		}
		allDescriptors = append(allDescriptors, info)
	}
	for _, metric := range analysis.Metrics() {
		info, err := relationInfoOfMetric(metric, wrenMDL)
		if err != nil {
			return nil, err
		}
		allDescriptors = append(allDescriptors, info)
	}
	for _, cm := range analysis.CumulativeMetrics() {
		info, err := cumulativeMetricInfoGet(cm, wrenMDL)
		if err != nil {
			return nil, err
		}
		allDescriptors = append(allDescriptors, info)
	}
	if len(allDescriptors) == 0 {
		return root, nil
	}

	descriptorMap := map[string]QueryDescriptor{}
	var vertices []string
	var edges [][2]string
	seen := map[string]bool{}
	var addToGraph func(d QueryDescriptor) error
	addToGraph = func(d QueryDescriptor) error {
		if !contains(vertices, d.Name()) {
			vertices = append(vertices, d.Name())
		}
		descriptorMap[d.Name()] = d
		for _, req := range d.RequiredObjects() {
			if !contains(vertices, req) {
				vertices = append(vertices, req)
			}
			edges = append(edges, [2]string{req, d.Name()})
			if seen[req] {
				continue
			}
			seen[req] = true
			reqDesc, err := QueryDescriptorOf(req, analyzedMDL, ctx)
			if err != nil {
				return err
			}
			if err := addToGraph(reqDesc); err != nil {
				return err
			}
		}
		return nil
	}
	for _, d := range allDescriptors {
		if err := addToGraph(d); err != nil {
			return nil, err
		}
	}

	order, err := topoSort(vertices, edges)
	if err != nil {
		return nil, err
	}

	var withQueries []ast.WithQuery
	for _, name := range order {
		d, ok := descriptorMap[name]
		if !ok {
			return nil, fmt.Errorf("%s not found in query descriptors", name)
		}
		withQueries = append(withQueries, getWithQuery(d))
	}

	rewriteWith := applyWith(root, withQueries)
	return rewriteModelTables(rewriteWith, wrenMDL, analysis).(ast.Statement), nil
}

// rewriteModelTables rewrites in-MDL table references to their CTE name and
// strips catalog/schema prefixes. Mirrors WrenSqlRewrite.Rewriter.
func rewriteModelTables(node ast.Node, wrenMDL *mdl.WrenMDL, analysis *analyzer.Analysis) ast.Node {
	return RewriteNode(node, func(n ast.Node) (ast.Node, bool) {
		switch x := n.(type) {
		case *ast.Table:
			if _, ok := analysis.SourceNodeName(x); ok {
				last := x.Name.OriginalParts[len(x.Name.OriginalParts)-1]
				return &ast.Table{Name: ast.QualifiedName{
					Parts:         []string{last.Value},
					OriginalParts: []ast.Identifier{last},
				}}, true
			}
			return x, true
		case *ast.DereferenceExpression:
			return stripCatalogSchema(x, wrenMDL), true
		}
		return nil, false
	})
}

// stripCatalogSchema removes a leading catalog.schema (or schema) prefix from a
// dereference. Mirrors Rewriter.visitDereferenceExpression.
func stripCatalogSchema(d *ast.DereferenceExpression, wrenMDL *mdl.WrenMDL) ast.Expression {
	qn := ast.GetQualifiedName(d)
	if qn == nil || wrenMDL.Catalog() == "" || wrenMDL.Schema() == "" {
		return d
	}
	if hasPrefixParts(*qn, wrenMDL.Catalog(), wrenMDL.Schema()) {
		return dereferenceFrom(qn.OriginalParts[2:])
	}
	if hasPrefixParts(*qn, wrenMDL.Schema()) {
		return dereferenceFrom(qn.OriginalParts[1:])
	}
	return d
}
