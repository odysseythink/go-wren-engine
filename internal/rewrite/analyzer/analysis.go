package analyzer

import (
	"sort"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// Analysis carries everything StatementAnalyzer discovers about a statement.
// Mirrors Java sqlrewrite.analyzer.Analysis.
type Analysis struct {
	root ast.Statement

	scopes              map[ast.NodeRef]*Scope
	tables              []CatalogSchemaTableName
	models              []*dto.Model
	metrics             []*dto.Metric
	cumulativeMetrics   []*dto.CumulativeMetric
	views               []*dto.View
	metricRollups       map[ast.NodeRef]*MetricRollupInfo
	collectedColumns    map[CatalogSchemaTableName]map[string]bool
	referenceFields     map[ast.NodeRef]*Field
	requiredSourceNodes map[ast.NodeRef]ast.Node
	sourceNodeNames     map[ast.NodeRef]ast.QualifiedName
}

func NewAnalysis(root ast.Statement) *Analysis {
	return &Analysis{
		root:                root,
		scopes:              map[ast.NodeRef]*Scope{},
		collectedColumns:    map[CatalogSchemaTableName]map[string]bool{},
		referenceFields:     map[ast.NodeRef]*Field{},
		requiredSourceNodes: map[ast.NodeRef]ast.Node{},
		sourceNodeNames:     map[ast.NodeRef]ast.QualifiedName{},
		metricRollups:       map[ast.NodeRef]*MetricRollupInfo{},
	}
}

func (a *Analysis) Root() ast.Statement { return a.root }

// AddTable adds a table, de-duplicating. Mirrors Analysis.addTable.
func (a *Analysis) AddTable(t CatalogSchemaTableName) {
	for _, x := range a.tables {
		if x == t {
			return
		}
	}
	a.tables = append(a.tables, t)
}
func (a *Analysis) Tables() []CatalogSchemaTableName { return a.tables }

// AddModels merges models, then sorts by name for deterministic CTE ordering.
func (a *Analysis) AddModels(models []*dto.Model) {
	seen := map[string]bool{}
	for _, m := range a.models {
		seen[m.Name] = true
	}
	for _, m := range models {
		if !seen[m.Name] {
			seen[m.Name] = true
			a.models = append(a.models, m)
		}
	}
	sort.Slice(a.models, func(i, j int) bool { return a.models[i].Name < a.models[j].Name })
}
func (a *Analysis) Models() []*dto.Model { return a.models }

// AddMetrics merges metrics, sorting by name.
func (a *Analysis) AddMetrics(metrics []*dto.Metric) {
	seen := map[string]bool{}
	for _, m := range a.metrics {
		seen[m.Name] = true
	}
	for _, m := range metrics {
		if !seen[m.Name] {
			seen[m.Name] = true
			a.metrics = append(a.metrics, m)
		}
	}
	sort.Slice(a.metrics, func(i, j int) bool { return a.metrics[i].Name < a.metrics[j].Name })
}
func (a *Analysis) Metrics() []*dto.Metric { return a.metrics }

// AddCumulativeMetrics merges cumulative metrics, sorting by name.
func (a *Analysis) AddCumulativeMetrics(cms []*dto.CumulativeMetric) {
	seen := map[string]bool{}
	for _, m := range a.cumulativeMetrics {
		seen[m.Name] = true
	}
	for _, m := range cms {
		if !seen[m.Name] {
			seen[m.Name] = true
			a.cumulativeMetrics = append(a.cumulativeMetrics, m)
		}
	}
	sort.Slice(a.cumulativeMetrics, func(i, j int) bool { return a.cumulativeMetrics[i].Name < a.cumulativeMetrics[j].Name })
}
func (a *Analysis) CumulativeMetrics() []*dto.CumulativeMetric { return a.cumulativeMetrics }

// AddViews merges views, sorting by name.
func (a *Analysis) AddViews(views []*dto.View) {
	seen := map[string]bool{}
	for _, v := range a.views {
		seen[v.Name] = true
	}
	for _, v := range views {
		if !seen[v.Name] {
			seen[v.Name] = true
			a.views = append(a.views, v)
		}
	}
	sort.Slice(a.views, func(i, j int) bool { return a.views[i].Name < a.views[j].Name })
}
func (a *Analysis) Views() []*dto.View { return a.views }

// SetScope / TryGetScope / GetScope mirror the scope map accessors.
func (a *Analysis) SetScope(n ast.Node, s *Scope) { a.scopes[ast.NodeRef{Node: n}] = s }
func (a *Analysis) TryGetScope(n ast.Node) (*Scope, bool) {
	s, ok := a.scopes[ast.NodeRef{Node: n}]
	return s, ok
}

// AddSourceNodeName / SourceNodeName key by node identity.
func (a *Analysis) AddSourceNodeName(n ast.Node, name ast.QualifiedName) {
	a.sourceNodeNames[ast.NodeRef{Node: n}] = name
}
func (a *Analysis) SourceNodeName(n ast.Node) (ast.QualifiedName, bool) {
	name, ok := a.sourceNodeNames[ast.NodeRef{Node: n}]
	return name, ok
}

// AddCollectedColumns adds fields to the collected columns map.
func (a *Analysis) AddCollectedColumns(fields []*Field) {
	for _, f := range fields {
		tn := f.TableName()
		if a.collectedColumns[tn] == nil {
			a.collectedColumns[tn] = map[string]bool{}
		}
		a.collectedColumns[tn][f.ColumnName()] = true
	}
}

// CollectedColumns returns the collected columns map.
func (a *Analysis) CollectedColumns() map[CatalogSchemaTableName]map[string]bool {
	return a.collectedColumns
}

// AddReferenceField adds a reference field mapping.
func (a *Analysis) AddReferenceField(n ast.Node, f *Field) {
	a.referenceFields[ast.NodeRef{Node: n}] = f
}

// ReferenceField looks up a reference field by node.
func (a *Analysis) ReferenceField(n ast.Node) (*Field, bool) {
	f, ok := a.referenceFields[ast.NodeRef{Node: n}]
	return f, ok
}

// AddRequiredSourceNode records a required source node.
func (a *Analysis) AddRequiredSourceNode(n ast.Node, source ast.Node) {
	a.requiredSourceNodes[ast.NodeRef{Node: n}] = source
}

// RequiredSourceNode looks up a required source node.
func (a *Analysis) RequiredSourceNode(n ast.Node) (ast.Node, bool) {
	s, ok := a.requiredSourceNodes[ast.NodeRef{Node: n}]
	return s, ok
}

// RequiredSourceNodes returns the full required-source-node map.
func (a *Analysis) RequiredSourceNodes() map[ast.NodeRef]ast.Node {
	return a.requiredSourceNodes
}

// AddMetricRollups registers a roll_up node's info, keyed by node identity.
// Mirrors Analysis.addMetricRollups.
func (a *Analysis) AddMetricRollups(n ast.Node, info *MetricRollupInfo) {
	a.metricRollups[ast.NodeRef{Node: n}] = info
}

// MetricRollups returns the node-ref -> info map. Mirrors Analysis.getMetricRollups.
func (a *Analysis) MetricRollups() map[ast.NodeRef]*MetricRollupInfo {
	return a.metricRollups
}

// GetMetricRollup looks up a roll_up node's info by identity.
func (a *Analysis) GetMetricRollup(n ast.Node) (*MetricRollupInfo, bool) {
	info, ok := a.metricRollups[ast.NodeRef{Node: n}]
	return info, ok
}

// WrenObjectNames returns the union of model / metric / cumulative metric / view
// names referenced by this analysis, sorted by name (deterministic).
// Mirrors Java Analysis.getWrenObjectNames.
func (a *Analysis) WrenObjectNames() []string {
	set := map[string]bool{}
	for _, m := range a.models {
		set[m.Name] = true
	}
	for _, m := range a.metrics {
		set[m.Name] = true
	}
	for _, c := range a.cumulativeMetrics {
		set[c.Name] = true
	}
	for _, v := range a.views {
		set[v.Name] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
