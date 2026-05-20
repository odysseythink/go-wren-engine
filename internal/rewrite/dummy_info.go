package rewrite

import "github.com/wren-engine/wren/internal/parser/ast"

// DummyInfo is a QueryDescriptor whose CTE body is "SELECT 1".
// Mirrors Java DummyInfo.
type DummyInfo struct{ name string }

func NewDummyInfo(name string) *DummyInfo      { return &DummyInfo{name: name} }
func (d *DummyInfo) Name() string              { return d.name }
func (d *DummyInfo) RequiredObjects() []string { return nil }
func (d *DummyInfo) Query() *ast.Query {
	q, err := parseQuery("select 1")
	if err != nil {
		panic(err) // "select 1" is always valid
	}
	return q
}
