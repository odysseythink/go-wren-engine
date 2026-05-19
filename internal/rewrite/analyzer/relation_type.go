package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

// RelationType is an ordered list of Fields. Mirrors Java analyzer.RelationType.
type RelationType struct {
	fields []*Field
}

func NewRelationType(fields []*Field) *RelationType { return &RelationType{fields: fields} }

func (rt *RelationType) Fields() []*Field { return rt.fields }

// ResolveAnyField returns the first non-relationship field that resolves name.
// Mirrors RelationType.resolveAnyField.
func (rt *RelationType) ResolveAnyField(name *ast.QualifiedName) *Field {
	for _, f := range rt.fields {
		if f.sourceColumn != nil && f.sourceColumn.Relationship != "" {
			continue
		}
		if f.CanResolve(name) {
			return f
		}
	}
	return nil
}

// JoinWith concatenates the fields of two RelationTypes. Mirrors joinWith.
func (rt *RelationType) JoinWith(other *RelationType) *RelationType {
	return NewRelationType(append(append([]*Field{}, rt.fields...), other.fields...))
}
