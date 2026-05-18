package ast

// NodeLocation represents a position in the source SQL text.
type NodeLocation struct {
	Line         int
	CharPosition int
}

// Node is the base interface for all AST nodes.
type Node interface {
	GetChildren() []Node
	GetLocation() *NodeLocation
}

// BaseNode provides a default implementation for Node.
type BaseNode struct {
	Location *NodeLocation
}

func (n *BaseNode) GetLocation() *NodeLocation {
	return n.Location
}

// NodeRef is a reference to a Node, used for identity comparison in maps.
type NodeRef struct {
	Node Node
}
