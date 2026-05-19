package analyzer

import (
	"strings"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// AnalyzeScope walks node and records Wren objects used in FROM.
// Mirrors ScopeAnalyzer.analyze.
func AnalyzeScope(wrenMDL *mdl.WrenMDL, node ast.Node, ctx *base.SessionContext) *ScopeAnalysis {
	sa := &ScopeAnalysis{aliasedMap: map[ast.NodeRef]string{}}
	(&scopeVisitor{wrenMDL: wrenMDL, analysis: sa, ctx: ctx}).visit(node)
	return sa
}

type scopeVisitor struct {
	wrenMDL  *mdl.WrenMDL
	analysis *ScopeAnalysis
	ctx      *base.SessionContext
}

func (v *scopeVisitor) visit(node ast.Node) {
	switch n := node.(type) {
	case *ast.Table:
		if v.isBelongToWren(n.Name) {
			v.analysis.AddUsedWrenObject(n)
		}
		return
	case *ast.TableSubquery:
		return // ScopeAnalyzer.visitTableSubquery: do not descend
	case *ast.AliasedRelation:
		v.analysis.AddAliasedNode(n.Relation, n.Alias.Value)
		v.visit(n.Relation)
		return
	}
	for _, c := range node.GetChildren() {
		v.visit(c)
	}
}

// isBelongToWren checks whether the table name resolves to a Wren model/metric.
// Mirrors ScopeAnalyzer.isBelongToWren.
func (v *scopeVisitor) isBelongToWren(name ast.QualifiedName) bool {
	cstn, err := toCatalogSchemaTableName(v.ctx, name)
	if err != nil {
		return false
	}
	if !strings.EqualFold(cstn.Catalog, v.wrenMDL.Catalog()) || !strings.EqualFold(cstn.Schema, v.wrenMDL.Schema()) {
		return false
	}
	return v.wrenMDL.IsObjectExist(cstn.Table)
}
