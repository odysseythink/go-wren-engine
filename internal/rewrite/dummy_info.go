package rewrite

import "github.com/wren-engine/wren/internal/parser/ast"

// DummyInfo is a QueryDescriptor that emits a constant CTE.
// Used by the dynamic branch for unvisited tables.
// Mirrors Java DummyInfo.
type DummyInfo struct {
	name string
}

func (d *DummyInfo) Name() string              { return d.name }
func (d *DummyInfo) RequiredObjects() []string { return nil }
func (d *DummyInfo) Query() *ast.Query {
	q, _ := parseQuery("SELECT 1")
	return q
}
