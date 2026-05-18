package visitor

import "github.com/wren-engine/wren/internal/parser/ast"

// Visitor is the interface for walking and transforming AST nodes.
type Visitor interface {
	Visit(node ast.Node) any
}

// BaseVisitor provides default traversal that visits all children.
type BaseVisitor struct{}

func (v *BaseVisitor) Visit(node ast.Node) any {
	if node == nil {
		return nil
	}
	for _, child := range node.GetChildren() {
		v.Visit(child)
	}
	return nil
}
