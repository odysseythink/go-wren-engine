package visitor

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser/ast"
)

type CountVisitor struct{ TableCount int }

func (v *CountVisitor) Visit(node ast.Node) any {
	if node == nil {
		return nil
	}
	switch n := node.(type) {
	case *ast.Table:
		return v.VisitTable(n)
	case *ast.Query:
		if n.Body != nil {
			v.Visit(n.Body)
		}
	case *ast.QuerySpecification:
		if n.From != nil {
			v.Visit(n.From)
		}
	case *ast.Join:
		if n.Left != nil {
			v.Visit(n.Left)
		}
		if n.Right != nil {
			v.Visit(n.Right)
		}
	default:
		for _, child := range node.GetChildren() {
			v.Visit(child)
		}
	}
	return nil
}

func (v *CountVisitor) VisitTable(n *ast.Table) any {
	v.TableCount++
	return nil
}

func TestCountVisitor(t *testing.T) {
	query := &ast.Query{
		Body: &ast.QuerySpecification{
			Select: &ast.Select{},
			From: &ast.Join{
				JoinType: ast.JoinTypeLeft,
				Left:     &ast.Table{Name: ast.QualifiedNameOf("orders")},
				Right:    &ast.Table{Name: ast.QualifiedNameOf("customers")},
			},
		},
	}
	v := &CountVisitor{}
	v.Visit(query)
	if v.TableCount != 2 {
		t.Errorf("expected 2 tables, got %d", v.TableCount)
	}
}
