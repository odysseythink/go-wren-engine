package lineage

import (
	"fmt"
	"sort"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// QualifiedName identifies a column in a table / model / metric.
type QualifiedName struct {
	Table  string
	Column string
}

func (q QualifiedName) String() string { return q.Table + "." + q.Column }

// TableFields is an ordered entry returned by RequiredFields.
type TableFields struct {
	Name   string
	Fields []string
}

// Lineage tracks column-level dependencies across the MDL.
// Mirrors Java WrenDataLineage.
type Lineage struct {
	mdl           *mdl.WrenMDL
	sourceColumns map[QualifiedName]map[QualifiedName]bool // direct one-hop sources
}

// Analyze builds a Lineage from a WrenMDL.
func Analyze(wrenMDL *mdl.WrenMDL) (*Lineage, error) {
	l := &Lineage{
		mdl:           wrenMDL,
		sourceColumns: make(map[QualifiedName]map[QualifiedName]bool),
	}
	if err := l.collectSourceColumns(); err != nil {
		return nil, err
	}
	return l, nil
}

// SourceColumns returns the direct (one-hop) source columns for a given column.
func (l *Lineage) SourceColumns(qn QualifiedName) map[string][]string {
	sources, ok := l.sourceColumns[qn]
	if !ok {
		return nil
	}
	out := make(map[string][]string)
	for src := range sources {
		out[src.Table] = append(out[src.Table], src.Column)
	}
	for k := range out {
		sort.Strings(out[k])
		seen := map[string]bool{}
		dedup := make([]string, 0, len(out[k]))
		for _, v := range out[k] {
			if !seen[v] {
				seen[v] = true
				dedup = append(dedup, v)
			}
		}
		out[k] = dedup
	}
	return out
}

// RequiredFields recursively collects all source tables and columns needed for
// the given columns, topologically sorted by dependency order.
func (l *Lineage) RequiredFields(columns []QualifiedName) ([]TableFields, error) {
	g := newDAG()
	for _, col := range columns {
		if err := l.collectRequiredFields(col, g); err != nil {
			return nil, err
		}
	}
	return g.topoSort()
}

// collectRequiredFields recurses through sourceColumns building the DAG.
func (l *Lineage) collectRequiredFields(qn QualifiedName, g *dag) error {
	g.addVertex(qn.Table, qn.Column)
	sources, ok := l.sourceColumns[qn]
	if !ok {
		return nil // silent skip for unknown columns (mirrors Java fallback)
	}
	for src := range sources {
		g.addVertex(src.Table, src.Column)
		if src == qn {
			continue // self-reference, no edge or recursion needed
		}
		if src.Table != qn.Table {
			if err := g.addEdge(src.Table, qn.Table); err != nil {
				return err
			}
		}
		if err := l.collectRequiredFields(src, g); err != nil {
			return err
		}
	}
	return nil
}

// collectSourceColumns populates sourceColumns for every model/metric/cumulative-metric column.
func (l *Lineage) collectSourceColumns() error {
	for _, model := range l.mdl.ListModels() {
		for i := range model.Columns {
			col := &model.Columns[i]
			qn := QualifiedName{Table: model.Name, Column: col.Name}
			sources, err := l.getModelColumnSources(model, col)
			if err != nil {
				return fmt.Errorf("model %q column %q: %w", model.Name, col.Name, err)
			}
			l.sourceColumns[qn] = sources
		}
	}
	manifest := l.mdl.Manifest()
	for i := range manifest.Metrics {
		metric := &manifest.Metrics[i]
		for _, col := range metric.GetColumns() {
			qn := QualifiedName{Table: metric.Name, Column: col.Name}
			sources, err := l.getMetricColumnSources(metric, &col)
			if err != nil {
				return fmt.Errorf("metric %q column %q: %w", metric.Name, col.Name, err)
			}
			l.sourceColumns[qn] = sources
		}
	}
	for i := range manifest.CumulativeMetrics {
		cm := &manifest.CumulativeMetrics[i]
		// Measure pseudo-column maps to baseObject.measure.refColumn
		l.sourceColumns[QualifiedName{Table: cm.Name, Column: cm.Measure.Name}] = map[QualifiedName]bool{
			{Table: cm.BaseObject, Column: cm.Measure.RefColumn}: true,
		}
		// Window pseudo-column maps to baseObject.window.refColumn
		l.sourceColumns[QualifiedName{Table: cm.Name, Column: cm.Window.Name}] = map[QualifiedName]bool{
			{Table: cm.BaseObject, Column: cm.Window.RefColumn}: true,
		}
	}
	return nil
}

// getModelColumnSources analyses a model column expression for direct source columns.
func (l *Lineage) getModelColumnSources(model *dto.Model, col *dto.Column) (map[QualifiedName]bool, error) {
	if col.Expression == "" {
		return map[QualifiedName]bool{{Table: model.Name, Column: col.Name}: true}, nil
	}
	expr, err := parser.ParseExpression(col.Expression)
	if err != nil {
		// Unparseable (e.g. jinja) — conservative fallback to self.
		return map[QualifiedName]bool{{Table: model.Name, Column: col.Name}: true}, nil
	}
	sources := make(map[QualifiedName]bool)
	collectColumnReferences(expr, func(e ast.Expression) {
		if qn, ok := l.resolveDereference(model, e); ok {
			sources[qn] = true
		}
	})
	if len(sources) == 0 {
		return map[QualifiedName]bool{{Table: model.Name, Column: col.Name}: true}, nil
	}
	return sources, nil
}

// getMetricColumnSources analyses a metric column expression for direct source columns.
func (l *Lineage) getMetricColumnSources(metric *dto.Metric, col *dto.Column) (map[QualifiedName]bool, error) {
	if col.Expression == "" {
		return map[QualifiedName]bool{{Table: metric.Name, Column: col.Name}: true}, nil
	}
	expr, err := parser.ParseExpression(col.Expression)
	if err != nil {
		return map[QualifiedName]bool{{Table: metric.Name, Column: col.Name}: true}, nil
	}
	// Determine the effective base model for this metric's expressions.
	baseModel, ok := l.mdl.GetModel(metric.BaseObject)
	if !ok {
		// Metric-on-metric: resolve the base chain until we hit a model.
		// For lineage we conservatively treat the metric itself as the source.
		return map[QualifiedName]bool{{Table: metric.Name, Column: col.Name}: true}, nil
	}
	sources := make(map[QualifiedName]bool)
	collectColumnReferences(expr, func(e ast.Expression) {
		if qn, ok := l.resolveDereference(baseModel, e); ok {
			sources[qn] = true
		}
	})
	if len(sources) == 0 {
		return map[QualifiedName]bool{{Table: metric.Name, Column: col.Name}: true}, nil
	}
	return sources, nil
}

// resolveDereference maps an expression to its source QualifiedName.
func (l *Lineage) resolveDereference(startModel *dto.Model, expr ast.Expression) (QualifiedName, bool) {
	qn := ast.GetQualifiedName(expr)
	if qn == nil || len(qn.Parts) == 0 {
		return QualifiedName{}, false
	}
	currentModel := startModel
	for i, part := range qn.Parts {
		relCol, ok := mdl.GetRelationshipColumn(currentModel, part)
		if !ok {
			// part is not a relationship → it names a concrete column.
			if i == 0 {
				return QualifiedName{Table: startModel.Name, Column: part}, true
			}
			return QualifiedName{Table: currentModel.Name, Column: part}, true
		}
		nextModel, ok := l.mdl.GetModel(relCol.Type)
		if !ok {
			return QualifiedName{}, false
		}
		currentModel = nextModel
	}
	// All parts were relationships — return the last model + last part as fallback.
	return QualifiedName{Table: currentModel.Name, Column: qn.Parts[len(qn.Parts)-1]}, true
}

// collectColumnReferences walks the AST and invokes fn for every expression that
// may reference a column (Identifier or DereferenceExpression).
func collectColumnReferences(node ast.Node, fn func(ast.Expression)) {
	if node == nil {
		return
	}
	switch e := node.(type) {
	case *ast.Identifier:
		fn(e)
	case *ast.DereferenceExpression:
		fn(e)
		return // don't recurse into children of a DereferenceExpression
	}
	for _, child := range node.GetChildren() {
		collectColumnReferences(child, fn)
	}
}

// dag is a small directed-acyclic graph used by RequiredFields.
type dag struct {
	vertices map[string]map[string]bool // table -> column set
	edges    map[string]map[string]bool // from -> to
}

func newDAG() *dag {
	return &dag{
		vertices: make(map[string]map[string]bool),
		edges:    make(map[string]map[string]bool),
	}
}

func (d *dag) addVertex(name string, columns ...string) {
	if d.vertices[name] == nil {
		d.vertices[name] = make(map[string]bool)
	}
	for _, c := range columns {
		d.vertices[name][c] = true
	}
}

func (d *dag) addEdge(from, to string) error {
	if from == to {
		return nil
	}
	if d.edges[from] == nil {
		d.edges[from] = make(map[string]bool)
	}
	// Cycle guard: reject if to can already reach from.
	if d.hasPath(to, from) {
		return fmt.Errorf("cycle detected: %s -> %s", from, to)
	}
	d.edges[from][to] = true
	return nil
}

func (d *dag) hasPath(from, to string) bool {
	visited := map[string]bool{}
	var dfs func(string) bool
	dfs = func(n string) bool {
		if n == to {
			return true
		}
		if visited[n] {
			return false
		}
		visited[n] = true
		for next := range d.edges[n] {
			if dfs(next) {
				return true
			}
		}
		return false
	}
	return dfs(from)
}

func (d *dag) topoSort() ([]TableFields, error) {
	inDegree := map[string]int{}
	for v := range d.vertices {
		inDegree[v] = 0
	}
	for from := range d.edges {
		for to := range d.edges[from] {
			inDegree[to]++
		}
	}
	var queue []string
	for v, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, v)
		}
	}
	sort.Strings(queue)

	var result []TableFields
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		cols := make([]string, 0, len(d.vertices[v]))
		for c := range d.vertices[v] {
			cols = append(cols, c)
		}
		sort.Strings(cols)
		result = append(result, TableFields{Name: v, Fields: cols})
		for next := range d.edges[v] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
		sort.Strings(queue)
	}
	if len(result) != len(d.vertices) {
		return nil, fmt.Errorf("cycle detected in dependency graph")
	}
	return result, nil
}
