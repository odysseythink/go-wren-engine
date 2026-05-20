package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// QueryDescriptor describes one CTE to be generated. Mirrors Java QueryDescriptor.
type QueryDescriptor interface {
	Name() string
	RequiredObjects() []string
	Query() *ast.Query
}

// QueryDescriptorOf builds a descriptor for the named object.
// Mirrors Java QueryDescriptor.of.
func QueryDescriptorOf(name string, analyzedMDL *mdl.AnalyzedMDL, ctx *base.SessionContext) (QueryDescriptor, error) {
	wrenMDL := analyzedMDL.WrenMDL()
	if model, ok := wrenMDL.GetModel(name); ok {
		return relationInfoOfModel(model, wrenMDL)
	}
	if metric, ok := wrenMDL.GetMetric(name); ok {
		return relationInfoOfMetric(metric, wrenMDL)
	}
	if cm, ok := wrenMDL.GetCumulativeMetric(name); ok {
		return cumulativeMetricInfoGet(cm, wrenMDL)
	}
	if view, ok := wrenMDL.GetView(name); ok {
		return viewInfoGet(view, analyzedMDL, ctx)
	}
	if name == dateSpineName {
		return dateSpineInfoGet(wrenMDL.GetDateSpine())
	}
	return nil, fmt.Errorf("%s not found in wren mdl", name)
}
