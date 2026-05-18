package ast

// Expression is the base interface for all SQL expressions.
type Expression interface {
	Node
	isExpression()
}

type ComparisonOperator string

const (
	ComparisonEqual        ComparisonOperator = "="
	ComparisonNotEqual     ComparisonOperator = "<>"
	ComparisonLessThan     ComparisonOperator = "<"
	ComparisonLessEqual    ComparisonOperator = "<="
	ComparisonGreaterThan  ComparisonOperator = ">"
	ComparisonGreaterEqual ComparisonOperator = ">="
)

type ArithmeticOperator string

const (
	ArithmeticAdd      ArithmeticOperator = "+"
	ArithmeticSubtract ArithmeticOperator = "-"
	ArithmeticMultiply ArithmeticOperator = "*"
	ArithmeticDivide   ArithmeticOperator = "/"
	ArithmeticModulus  ArithmeticOperator = "%"
)

type LogicalOperator string

const (
	LogicalAnd LogicalOperator = "AND"
	LogicalOr  LogicalOperator = "OR"
)

// Identifier represents a SQL identifier.
type Identifier struct {
	BaseNode
	Value     string
	Delimited bool
}

func (i *Identifier) GetChildren() []Node { return nil }
func (i *Identifier) isExpression()       {}

// DereferenceExpression represents a.b (field access).
type DereferenceExpression struct {
	BaseNode
	Base  Expression
	Field *Identifier
}

func (d *DereferenceExpression) GetChildren() []Node {
	var children []Node
	if d.Base != nil {
		children = append(children, d.Base)
	}
	if d.Field != nil {
		children = append(children, d.Field)
	}
	return children
}

func (d *DereferenceExpression) isExpression() {}

// GetQualifiedName extracts a QualifiedName from a DereferenceExpression chain.
func GetQualifiedName(expr Expression) *QualifiedName {
	switch e := expr.(type) {
	case *Identifier:
		return &QualifiedName{Parts: []string{e.Value}, OriginalParts: []Identifier{*e}}
	case *DereferenceExpression:
		baseQN := GetQualifiedName(e.Base)
		if baseQN == nil {
			return nil
		}
		parts := append(baseQN.Parts, e.Field.Value)
		original := append(baseQN.OriginalParts, *e.Field)
		return &QualifiedName{Parts: parts, OriginalParts: original}
	}
	return nil
}

// ComparisonExpression represents a = b, a < b, etc.
type ComparisonExpression struct {
	BaseNode
	Operator ComparisonOperator
	Left     Expression
	Right    Expression
}

func (c *ComparisonExpression) GetChildren() []Node {
	return []Node{c.Left, c.Right}
}
func (c *ComparisonExpression) isExpression() {}

// ArithmeticBinaryExpression represents a + b, a * b, etc.
type ArithmeticBinaryExpression struct {
	BaseNode
	Operator ArithmeticOperator
	Left     Expression
	Right    Expression
}

func (a *ArithmeticBinaryExpression) GetChildren() []Node {
	return []Node{a.Left, a.Right}
}
func (a *ArithmeticBinaryExpression) isExpression() {}

// LogicalBinaryExpression represents a AND b, a OR b.
type LogicalBinaryExpression struct {
	BaseNode
	Operator LogicalOperator
	Left     Expression
	Right    Expression
}

func (l *LogicalBinaryExpression) GetChildren() []Node {
	return []Node{l.Left, l.Right}
}
func (l *LogicalBinaryExpression) isExpression() {}

// NotExpression represents NOT expr.
type NotExpression struct {
	BaseNode
	Value Expression
}

func (n *NotExpression) GetChildren() []Node { return []Node{n.Value} }
func (n *NotExpression) isExpression()       {}

// FunctionCall represents func_name(arg1, arg2, ...).
type FunctionCall struct {
	BaseNode
	Name        QualifiedName
	Arguments   []Expression
	OrderBy     []SortItem
	Filter      Expression
	Window      *Window
	Distinct    bool
	IgnoreNulls bool
}

func (f *FunctionCall) GetChildren() []Node {
	var children []Node
	for _, arg := range f.Arguments {
		children = append(children, arg)
	}
	for _, si := range f.OrderBy {
		children = append(children, &si)
	}
	if f.Filter != nil {
		children = append(children, f.Filter)
	}
	if f.Window != nil {
		children = append(children, f.Window)
	}
	return children
}
func (f *FunctionCall) isExpression() {}

// StarExpression represents * or t.* in SELECT.
type StarExpression struct {
	BaseNode
	QualifiedName *QualifiedName
}

func (s *StarExpression) GetChildren() []Node { return nil }
func (s *StarExpression) isExpression()       {}

// Cast represents CAST(expr AS type).
type Cast struct {
	BaseNode
	Expression Expression
	Type       DataType
	Safe       bool
}

func (c *Cast) GetChildren() []Node { return []Node{c.Expression} }
func (c *Cast) isExpression()       {}

// CoalesceExpression represents COALESCE(a, b, ...).
type CoalesceExpression struct {
	BaseNode
	Operands []Expression
}

func (c *CoalesceExpression) GetChildren() []Node {
	children := make([]Node, len(c.Operands))
	for i, o := range c.Operands {
		children[i] = o
	}
	return children
}
func (c *CoalesceExpression) isExpression() {}

// InPredicate represents expr IN (values).
type InPredicate struct {
	BaseNode
	Value     Expression
	ValueList Expression
	Not       bool
}

func (i *InPredicate) GetChildren() []Node { return []Node{i.Value, i.ValueList} }
func (i *InPredicate) isExpression()       {}

// InListExpression represents (1, 2, 3) in IN predicate.
type InListExpression struct {
	BaseNode
	Values []Expression
}

func (i *InListExpression) GetChildren() []Node {
	children := make([]Node, len(i.Values))
	for j, v := range i.Values {
		children[j] = v
	}
	return children
}
func (i *InListExpression) isExpression() {}

// BetweenPredicate represents a BETWEEN b AND c.
type BetweenPredicate struct {
	BaseNode
	Value Expression
	Min   Expression
	Max   Expression
	Not   bool
}

func (b *BetweenPredicate) GetChildren() []Node { return []Node{b.Value, b.Min, b.Max} }
func (b *BetweenPredicate) isExpression()       {}

// SubqueryExpression represents a subquery used as an expression.
type SubqueryExpression struct {
	BaseNode
	Query Statement
}

func (s *SubqueryExpression) GetChildren() []Node { return []Node{s.Query} }
func (s *SubqueryExpression) isExpression()       {}

// AtTimeZone represents expr AT TIME ZONE 'zone'.
type AtTimeZone struct {
	BaseNode
	Value    Expression
	TimeZone Expression
}

func (a *AtTimeZone) GetChildren() []Node { return []Node{a.Value, a.TimeZone} }
func (a *AtTimeZone) isExpression()       {}

// IsNullPredicate represents expr IS NULL.
type IsNullPredicate struct {
	BaseNode
	Value Expression
	Not   bool
}

func (i *IsNullPredicate) GetChildren() []Node { return []Node{i.Value} }
func (i *IsNullPredicate) isExpression()       {}

// LikePredicate represents expr LIKE pattern.
type LikePredicate struct {
	BaseNode
	Value   Expression
	Pattern Expression
	Escape  Expression
	Not     bool
}

func (l *LikePredicate) GetChildren() []Node {
	children := []Node{l.Value, l.Pattern}
	if l.Escape != nil {
		children = append(children, l.Escape)
	}
	return children
}
func (l *LikePredicate) isExpression() {}

// --- Literal types ---

type LongLiteral struct {
	BaseNode
	Value int64
}

func (l *LongLiteral) GetChildren() []Node { return nil }
func (l *LongLiteral) isExpression()       {}

type DoubleLiteral struct {
	BaseNode
	Value float64
}

func (d *DoubleLiteral) GetChildren() []Node { return nil }
func (d *DoubleLiteral) isExpression()       {}

type StringLiteral struct {
	BaseNode
	Value string
}

func (s *StringLiteral) GetChildren() []Node { return nil }
func (s *StringLiteral) isExpression()       {}

type BooleanLiteral struct {
	BaseNode
	Value bool
}

func (b *BooleanLiteral) GetChildren() []Node { return nil }
func (b *BooleanLiteral) isExpression()       {}

type NullLiteral struct{ BaseNode }

func (n *NullLiteral) GetChildren() []Node { return nil }
func (n *NullLiteral) isExpression()       {}

type GenericLiteral struct {
	BaseNode
	Type  string
	Value string
}

func (g *GenericLiteral) GetChildren() []Node { return nil }
func (g *GenericLiteral) isExpression()       {}
