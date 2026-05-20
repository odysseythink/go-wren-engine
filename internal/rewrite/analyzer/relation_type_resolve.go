package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

func (rt *RelationType) ResolveFields(name *ast.QualifiedName) []*Field {
	var out []*Field
	for _, f := range rt.fields {
		if f.sourceColumn != nil && f.sourceColumn.Relationship != "" {
			continue
		}
		if f.CanResolve(name) {
			out = append(out, f)
		}
	}
	return out
}
