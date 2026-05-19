package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// StatementAnalysis holds the results of analyzing a SQL statement.
type StatementAnalysis struct {
	Tables            []string
	Models            []*dto.Model
	Metrics           []*dto.Metric
	CumulativeMetrics []*dto.CumulativeMetric
	Views             []*dto.View
}

// AnalyzeStatement walks the AST and collects referenced Wren objects.
func AnalyzeStatement(stmt ast.Statement, ctx *analyzer.SessionContext, mdl *mdl.WrenMDL) *StatementAnalysis {
	a := &StatementAnalysis{}
	collectTables(stmt, a, ctx, mdl)
	return a
}

func collectTables(node ast.Node, a *StatementAnalysis, ctx *analyzer.SessionContext, wrenMDL *mdl.WrenMDL) {
	if node == nil {
		return
	}

	// Check if this node is a Table reference
	if table, ok := node.(*ast.Table); ok {
		name := table.Name.String()
		a.Tables = append(a.Tables, name)

		// Check if table name matches a Wren object
		if model, found := wrenMDL.GetModel(name); found {
			a.Models = append(a.Models, model)
		}
		if metric, found := wrenMDL.GetMetric(name); found {
			a.Metrics = append(a.Metrics, metric)
		}
		if cm, found := wrenMDL.GetCumulativeMetric(name); found {
			a.CumulativeMetrics = append(a.CumulativeMetrics, cm)
		}
		if view, found := wrenMDL.GetView(name); found {
			a.Views = append(a.Views, view)
		}
	}

	// Recurse into children
	for _, child := range node.GetChildren() {
		collectTables(child, a, ctx, wrenMDL)
	}
}

// BuildModelDescriptor creates a QueryDescriptor for a model.
func BuildModelDescriptor(model *dto.Model) *QueryDescriptor {
	sql := model.RefSql
	if sql == "" {
		sql = fmt.Sprintf("SELECT * FROM %s", model.Name)
	}
	return &QueryDescriptor{
		Name:     model.Name,
		SQL:      sql,
		Requires: nil,
	}
}

// BuildMetricDescriptor creates a QueryDescriptor for a metric.
func BuildMetricDescriptor(metric *dto.Metric) *QueryDescriptor {
	return &QueryDescriptor{
		Name:     metric.Name,
		SQL:      metric.BaseObject,
		Requires: nil,
	}
}

// BuildCumulativeMetricDescriptor creates a QueryDescriptor for a cumulative metric.
func BuildCumulativeMetricDescriptor(cm *dto.CumulativeMetric) *QueryDescriptor {
	return &QueryDescriptor{
		Name:     cm.Name,
		SQL:      cm.BaseObject,
		Requires: nil,
	}
}

// BuildViewDescriptor creates a QueryDescriptor for a view.
func BuildViewDescriptor(view *dto.View) *QueryDescriptor {
	return &QueryDescriptor{
		Name:     view.Name,
		SQL:      view.Statement,
		Requires: nil,
	}
}
