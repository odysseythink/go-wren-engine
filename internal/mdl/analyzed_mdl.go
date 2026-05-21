package mdl

// AnalyzedMDL wraps WrenMDL with computed lineage information.
type AnalyzedMDL struct {
	wrenMDL *WrenMDL
}

// NewAnalyzedMDL creates an AnalyzedMDL from a WrenMDL.
func NewAnalyzedMDL(wrenMDL *WrenMDL) *AnalyzedMDL {
	return &AnalyzedMDL{wrenMDL: wrenMDL}
}

// WrenMDL returns the underlying WrenMDL.
func (a *AnalyzedMDL) WrenMDL() *WrenMDL {
	return a.wrenMDL
}
