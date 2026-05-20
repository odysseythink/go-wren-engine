package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

// Scope represents a query scope with its relation type and parent.
// Mirrors analyzer.Scope.
type Scope struct {
	parent            *Scope
	relationId        RelationId
	relationType      *RelationType
	isDataSourceScope bool
	namedQueries      map[string]*ast.WithQuery
}

// ScopeBuilder builds a Scope. Mirrors Scope.builder().
type ScopeBuilder struct {
	parent            *Scope
	relationId        RelationId
	relationType      *RelationType
	isDataSourceScope bool
	namedQueries      map[string]*ast.WithQuery
}

// ScopeBuilderWithParent creates a ScopeBuilder with a parent scope.
func ScopeBuilderWithParent(parent *Scope) *ScopeBuilder {
	return &ScopeBuilder{parent: parent}
}

// RelationId sets the relation id.
func (b *ScopeBuilder) RelationId(id RelationId) *ScopeBuilder {
	b.relationId = id
	return b
}

// RelationType sets the relation type.
func (b *ScopeBuilder) RelationType(rt *RelationType) *ScopeBuilder {
	b.relationType = rt
	return b
}

// IsDataSourceScope sets the data-source flag.
func (b *ScopeBuilder) IsDataSourceScope(v bool) *ScopeBuilder {
	b.isDataSourceScope = v
	return b
}

// NamedQueries sets the named queries map.
func (b *ScopeBuilder) NamedQueries(q map[string]*ast.WithQuery) *ScopeBuilder {
	b.namedQueries = q
	return b
}

// Build creates the Scope.
func (b *ScopeBuilder) Build() *Scope {
	rt := b.relationType
	if rt == nil {
		rt = NewRelationType(nil)
	}
	return &Scope{
		parent:            b.parent,
		relationId:        b.relationId,
		relationType:      rt,
		isDataSourceScope: b.isDataSourceScope,
		namedQueries:      b.namedQueries,
	}
}

// Parent returns the parent scope.
func (s *Scope) Parent() *Scope { return s.parent }

// RelationId returns the scope's relation id.
func (s *Scope) RelationId() RelationId { return s.relationId }

// RelationType returns the scope's relation type.
func (s *Scope) RelationType() *RelationType { return s.relationType }

// GetNamedQuery looks up a named query by traversing parent scopes.
// Mirrors Scope.getNamedQuery.
func (s *Scope) GetNamedQuery(name string) *ast.WithQuery {
	for scope := s; scope != nil; scope = scope.parent {
		if scope.namedQueries != nil {
			if q, ok := scope.namedQueries[name]; ok {
				return q
			}
		}
	}
	return nil
}
