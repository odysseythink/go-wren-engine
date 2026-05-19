package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

// ScopeAnalysis holds the result of ScopeAnalyzer.walk.
// Mirrors analyzer.ScopeAnalysis.
type ScopeAnalysis struct {
	usedWrenObjects []Relation
	aliasedMap      map[ast.NodeRef]string
}

// Relation identifies a used Wren object with its optional alias.
type Relation struct {
	Name  string
	Alias string
}

// UsedWrenObjects returns the list of used Wren objects.
func (s *ScopeAnalysis) UsedWrenObjects() []Relation { return s.usedWrenObjects }

// AddUsedWrenObject records a table as a used Wren object.
func (s *ScopeAnalysis) AddUsedWrenObject(t *ast.Table) {
	name := t.Name.Last()
	s.usedWrenObjects = append(s.usedWrenObjects, Relation{Name: name})
}

// AddAliasedNode records an aliased relation.
func (s *ScopeAnalysis) AddAliasedNode(node ast.Node, alias string) {
	s.aliasedMap[ast.NodeRef{Node: node}] = alias
}
