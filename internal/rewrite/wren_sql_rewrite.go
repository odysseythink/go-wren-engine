package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
	"github.com/wren-engine/wren/internal/rewrite/lineage"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func init() {
	mdl.SetLineageAnalyzer(func(w *mdl.WrenMDL) (interface{}, error) {
		return lineage.Analyze(w)
	})
}

type WrenSqlRewrite struct{}

// Apply expands referenced Wren models into CTEs.
func (r *WrenSqlRewrite) Apply(root ast.Statement, ctx *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	wrenMDL := analyzedMDL.WrenMDL()

	analysis := analyzer.NewAnalysis(root)
	if _, err := analyzer.Analyze(analysis, root, ctx, wrenMDL); err != nil {
		return nil, err
	}

	if ctx.EnableDynamicFields {
		return r.applyDynamic(root, analyzedMDL, analysis)
	}
	return r.applyStatic(root, analyzedMDL, analysis)
}

// applyStatic is the existing non-dynamic path (unchanged from Phase 4).
func (r *WrenSqlRewrite) applyStatic(root ast.Statement, analyzedMDL *mdl.AnalyzedMDL, analysis *analyzer.Analysis) (ast.Statement, error) {
	wrenMDL := analyzedMDL.WrenMDL()

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
			reqDesc, err := QueryDescriptorOf(req, analyzedMDL, nil)
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

// applyDynamic is the new dynamic-field path.
func (r *WrenSqlRewrite) applyDynamic(root ast.Statement, analyzedMDL *mdl.AnalyzedMDL, analysis *analyzer.Analysis) (ast.Statement, error) {
	wrenMDL := analyzedMDL.WrenMDL()
	linIface, err := analyzedMDL.DataLineage()
	if err != nil {
		return nil, fmt.Errorf("lineage: %w", err)
	}
	lin := linIface.(*lineage.Lineage)

	// Build visitedTables set from analysis.Tables (skip views).
	visitedTables := map[string]bool{}
	for _, t := range analysis.Tables() {
		if _, ok := wrenMDL.GetView(t.Table); ok {
			continue
		}
		visitedTables[t.Table] = true
	}

	// Convert collectedColumns to QualifiedName slice.
	var requiredCols []lineage.QualifiedName
	for cstn, cols := range analysis.CollectedColumns() {
		for colName := range cols {
			requiredCols = append(requiredCols, lineage.QualifiedName{
				Table:  cstn.Table,
				Column: colName,
			})
		}
	}

	// Get lineage-required fields.
	tableRequiredFields, err := lin.RequiredFields(requiredCols)
	if err != nil {
		return nil, err
	}

	// count(*) fallback: add non-calc columns for required source nodes not yet covered.
	for _, source := range analysis.RequiredSourceNodes() {
		srcName, ok := analysis.SourceNodeName(source)
		if !ok || len(srcName.Parts) == 0 {
			continue
		}
		tableName := srcName.Parts[len(srcName.Parts)-1]
		found := false
		for _, tf := range tableRequiredFields {
			if tf.Name == tableName {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if model, ok := wrenMDL.GetModel(tableName); ok {
			var cols []string
			for _, c := range model.Columns {
				if !c.IsCalculated && c.Relationship == "" {
					cols = append(cols, c.Name)
				}
			}
			tableRequiredFields = append(tableRequiredFields, lineage.TableFields{Name: tableName, Fields: cols})
		} else if metric, ok := wrenMDL.GetMetric(tableName); ok {
			var cols []string
			for _, c := range metric.GetColumns() {
				cols = append(cols, c.Name)
			}
			tableRequiredFields = append(tableRequiredFields, lineage.TableFields{Name: tableName, Fields: cols})
		}
	}

	// Build pruned descriptors.
	var descriptors []QueryDescriptor
	for _, tf := range tableRequiredFields {
		delete(visitedTables, tf.Name)
		if model, ok := wrenMDL.GetModel(tf.Name); ok {
			info, err := relationInfoOfModelWithFields(model, wrenMDL, tf.Fields)
			if err != nil {
				return nil, err
			}
			descriptors = append(descriptors, info)
		} else if metric, ok := wrenMDL.GetMetric(tf.Name); ok {
			info, err := relationInfoOfMetricWithFields(metric, wrenMDL, tf.Fields)
			if err != nil {
				return nil, err
			}
			descriptors = append(descriptors, info)
		} else if cm, ok := wrenMDL.GetCumulativeMetric(tf.Name); ok {
			info, err := cumulativeMetricInfoGet(cm, wrenMDL)
			if err != nil {
				return nil, err
			}
			descriptors = append(descriptors, info)
		}
	}

	// CTE orchestration.
	var withQueries []ast.WithQuery
	hasCumulative := false
	for _, tf := range tableRequiredFields {
		if _, ok := wrenMDL.GetCumulativeMetric(tf.Name); ok {
			hasCumulative = true
			break
		}
	}
	if hasCumulative {
		ds, err := dateSpineInfoGet(wrenMDL.GetDateSpine())
		if err != nil {
			return nil, err
		}
		withQueries = append(withQueries, getWithQuery(ds))
	}
	for _, d := range descriptors {
		withQueries = append(withQueries, getWithQuery(d))
	}
	for name := range visitedTables {
		if wrenMDL.IsObjectExist(name) {
			withQueries = append(withQueries, getWithQuery(&DummyInfo{name: name}))
		}
	}

	rewriteWith := applyWith(root, withQueries)
	return rewriteModelTables(rewriteWith, wrenMDL, analysis).(ast.Statement), nil
}

// rewriteModelTables rewrites in-MDL table references to their CTE name.
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
