package ast

// DataType represents a SQL data type.
type DataType struct {
	BaseNode
	Name       string
	Parameters []DataTypeParameter
}

func (d *DataType) GetChildren() []Node {
	children := make([]Node, len(d.Parameters))
	for i, p := range d.Parameters {
		children[i] = p
	}
	return children
}

// DataTypeParameter is satisfied by TypeParameter and NumericParameter.
type DataTypeParameter interface {
	Node
	isDataTypeParameter()
}

// TypeParameter wraps a DataType as a parameter.
type TypeParameter struct {
	BaseNode
	Type DataType
}

func (t *TypeParameter) GetChildren() []Node        { return []Node{&t.Type} }
func (t *TypeParameter) isDataTypeParameter() {}

// NumericParameter wraps a number as a type parameter.
type NumericParameter struct {
	BaseNode
	Value string
}

func (n *NumericParameter) GetChildren() []Node        { return nil }
func (n *NumericParameter) isDataTypeParameter() {}

// ColumnDefinition represents a column definition in CREATE TABLE.
type ColumnDefinition struct {
	BaseNode
	Name    Identifier
	Type    DataType
	NotNull bool
}

func (c *ColumnDefinition) GetChildren() []Node { return []Node{&c.Name, &c.Type} }
