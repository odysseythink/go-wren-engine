package analyzer

import (
	"github.com/wren-engine/wren/internal/dto"
)

// Analysis tracks tables, models, metrics, views, and columns referenced in a query.
type Analysis struct {
	Tables            []string
	Models            []string
	Metrics           []string
	CumulativeMetrics []string
	Views             []string
	CollectedColumns  []dto.Column
}

// AddTable adds a table reference.
func (a *Analysis) AddTable(name string) {
	a.Tables = append(a.Tables, name)
}

// AddModel adds a model reference.
func (a *Analysis) AddModel(name string) {
	a.Models = append(a.Models, name)
}

// AddMetric adds a metric reference.
func (a *Analysis) AddMetric(name string) {
	a.Metrics = append(a.Metrics, name)
}

// AddCumulativeMetric adds a cumulative metric reference.
func (a *Analysis) AddCumulativeMetric(name string) {
	a.CumulativeMetrics = append(a.CumulativeMetrics, name)
}

// AddView adds a view reference.
func (a *Analysis) AddView(name string) {
	a.Views = append(a.Views, name)
}

// AddColumn adds a collected column.
func (a *Analysis) AddColumn(col dto.Column) {
	a.CollectedColumns = append(a.CollectedColumns, col)
}
