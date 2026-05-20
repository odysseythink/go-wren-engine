package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// GenerateViewRewrite expands MDL view references into WITH CTEs.
// Mirrors Java io.wren.base.sqlrewrite.GenerateViewRewrite.
type GenerateViewRewrite struct{}

// Apply runs StatementAnalyzer, then turns every directly-referenced view (and
// every nested-view dependency) into a WITH CTE, topologically ordered so a
// view is defined after the views it references. Mirrors GenerateViewRewrite.apply.
//
// Unlike WrenSqlRewrite this rule does NOT rewrite Table references — it just
// prepends view CTEs; the original "FROM useModel" then resolves to the CTE.
func (r *GenerateViewRewrite) Apply(root ast.Statement, ctx *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	wrenMDL := analyzedMDL.WrenMDL()

	analysis := analyzer.NewAnalysis(root)
	if _, err := analyzer.Analyze(analysis, root, ctx, wrenMDL); err != nil {
		return nil, err
	}

	// seed: directly-referenced views (name-sorted by Analysis.AddViews)
	var viewDescriptors []QueryDescriptor
	for _, view := range analysis.Views() {
		info, err := viewInfoGet(view, analyzedMDL, ctx)
		if err != nil {
			return nil, err
		}
		viewDescriptors = append(viewDescriptors, info)
	}
	if len(viewDescriptors) == 0 {
		return root, nil // no view referenced: pass through
	}

	// DAG: vertices = view names (only); edges = requiredView -> view
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
		for _, req := range d.RequiredObjects() { // name-sorted by WrenObjectNames
			if _, isView := wrenMDL.GetView(req); !isView {
				continue // risk #8: only views become graph vertices/edges here
			}
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
	for _, d := range viewDescriptors {
		if err := addToGraph(d); err != nil {
			return nil, err
		}
	}

	order, err := topoSort(vertices, edges)
	if err != nil {
		return nil, fmt.Errorf("found cycle in view: %w", err)
	}

	var withQueries []ast.WithQuery
	for _, name := range order {
		d, ok := descriptorMap[name]
		if !ok {
			return nil, fmt.Errorf("%s not found in query descriptors", name)
		}
		withQueries = append(withQueries, getWithQuery(d)) // risk #7: delimited CTE name
	}

	return applyWith(root, withQueries), nil // risk #2: applyWith prepends; rule 3 WrenSqlRewrite later prepends model CTEs
}
