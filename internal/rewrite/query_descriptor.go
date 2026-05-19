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
// Mirrors QueryDescriptor.of (P3a: models only).
func QueryDescriptorOf(name string, analyzedMDL *mdl.AnalyzedMDL, ctx *base.SessionContext) (QueryDescriptor, error) {
	wrenMDL := analyzedMDL.WrenMDL()
	if model, ok := wrenMDL.GetModel(name); ok {
		return relationInfoOfModel(model, wrenMDL)
	}
	if _, ok := wrenMDL.GetMetric(name); ok {
		return nil, fmt.Errorf("metric %q requires P3b", name)
	}
	if _, ok := wrenMDL.GetCumulativeMetric(name); ok {
		return nil, fmt.Errorf("cumulative metric %q requires P3b", name)
	}
	if _, ok := wrenMDL.GetView(name); ok {
		return nil, fmt.Errorf("view %q requires P3c", name)
	}
	return nil, fmt.Errorf("%s not found in wren mdl", name)
}
