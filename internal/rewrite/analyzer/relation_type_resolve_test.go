package analyzer

import (
	"testing"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

func TestResolveFields(t *testing.T) {
	name1 := "id"
	name2 := "name"
	f1 := &Field{columnName: "id", name: &name1, sourceColumn: &dto.Column{Name: "id"}}
	f2 := &Field{columnName: "name", name: &name2, sourceColumn: &dto.Column{Name: "name", Relationship: "r1"}}
	f3 := &Field{columnName: "age", name: nil, sourceColumn: &dto.Column{Name: "age"}}
	rt := NewRelationType([]*Field{f1, f2, f3})

	got := rt.ResolveFields(&ast.QualifiedName{Parts: []string{"id"}})
	if len(got) != 1 || got[0] != f1 {
		t.Fatalf("expected [f1], got %v", got)
	}
	got2 := rt.ResolveFields(&ast.QualifiedName{Parts: []string{"name"}})
	if len(got2) != 0 {
		t.Fatalf("expected none for relationship field, got %v", got2)
	}
}
