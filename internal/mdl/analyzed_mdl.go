package mdl

import "sync"

// AnalyzedMDL wraps WrenMDL with computed lineage information.
type AnalyzedMDL struct {
	wrenMDL    *WrenMDL
	lineageOnce sync.Once
	lineage    interface{}
	lineageErr error
}

// NewAnalyzedMDL creates an AnalyzedMDL from a WrenMDL.
func NewAnalyzedMDL(wrenMDL *WrenMDL) *AnalyzedMDL {
	return &AnalyzedMDL{wrenMDL: wrenMDL}
}

// WrenMDL returns the underlying WrenMDL.
func (a *AnalyzedMDL) WrenMDL() *WrenMDL {
	return a.wrenMDL
}

// DataLineage returns the lazily-computed lineage graph.
// The concrete return type is *lineage.Lineage; callers should type-assert.
func (a *AnalyzedMDL) DataLineage() (interface{}, error) {
	a.lineageOnce.Do(func() {
		if lineageAnalyzer != nil {
			a.lineage, a.lineageErr = lineageAnalyzer(a.wrenMDL)
		}
	})
	return a.lineage, a.lineageErr
}

// lineageAnalyzer is injected by the rewrite package to avoid an import cycle.
var lineageAnalyzer func(*WrenMDL) (interface{}, error)

// SetLineageAnalyzer injects the lineage analyzer function.
func SetLineageAnalyzer(f func(*WrenMDL) (interface{}, error)) {
	lineageAnalyzer = f
}
