package rewrite

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// CumulativeMetricInfo is a QueryDescriptor for a cumulative metric.
// Mirrors Java io.wren.base.sqlrewrite.CumulativeMetricInfo.
type CumulativeMetricInfo struct {
	name            string
	requiredObjects []string
	query           *ast.Query
}

// cumulativeMetricInfoGet builds a CumulativeMetricInfo. Mirrors CumulativeMetricInfo.get.
func cumulativeMetricInfoGet(cm *dto.CumulativeMetric, wrenMDL *mdl.WrenMDL) (*CumulativeMetricInfo, error) {
	query, err := parseCumulativeMetricSql(cm, wrenMDL)
	if err != nil {
		return nil, err
	}
	req := sortedKeys(map[string]bool{cm.BaseObject: true, dateSpineName: true})
	return &CumulativeMetricInfo{name: cm.Name, requiredObjects: req, query: query}, nil
}

func (c *CumulativeMetricInfo) Name() string              { return c.name }
func (c *CumulativeMetricInfo) RequiredObjects() []string { return c.requiredObjects }
func (c *CumulativeMetricInfo) Query() *ast.Query         { return c.query }
