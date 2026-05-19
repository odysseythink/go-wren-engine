package ast

// Relation is the base interface for FROM clause items.
type Relation interface {
	Node
	isRelation()
}

type JoinType string

const (
	JoinTypeCross    JoinType = "CROSS"
	JoinTypeInner    JoinType = "INNER"
	JoinTypeLeft     JoinType = "LEFT"
	JoinTypeRight    JoinType = "RIGHT"
	JoinTypeFull     JoinType = "FULL"
	JoinTypeImplicit JoinType = "IMPLICIT"
)

// Table represents a table reference.
type Table struct {
	BaseNode
	Name  QualifiedName
	Alias *Identifier
}

func (t *Table) GetChildren() []Node { return nil }
func (t *Table) isRelation()         {}

// AliasedRelation wraps a relation with an alias.
type AliasedRelation struct {
	BaseNode
	Relation    Relation
	Alias       *Identifier
	ColumnNames []Identifier
}

func (a *AliasedRelation) GetChildren() []Node { return []Node{a.Relation} }
func (a *AliasedRelation) isRelation()         {}

// JoinCriteria is the base interface for JOIN ON/USING.
type JoinCriteria interface {
	Node
	isJoinCriteria()
}

// JoinOn represents a JOIN ON condition.
type JoinOn struct {
	BaseNode
	Expression Expression
}

func (j *JoinOn) GetChildren() []Node {
	if j.Expression != nil {
		return []Node{j.Expression}
	}
	return nil
}
func (j *JoinOn) isJoinCriteria() {}

// JoinUsing represents a JOIN USING clause.
type JoinUsing struct {
	BaseNode
	Columns []Identifier
}

func (j *JoinUsing) GetChildren() []Node {
	children := make([]Node, len(j.Columns))
	for i := range j.Columns {
		children[i] = &j.Columns[i]
	}
	return children
}
func (j *JoinUsing) isJoinCriteria() {}

// NaturalJoin represents NATURAL JOIN.
type NaturalJoin struct{ BaseNode }

func (n *NaturalJoin) GetChildren() []Node { return nil }
func (n *NaturalJoin) isJoinCriteria()     {}

// Join represents a JOIN relation.
type Join struct {
	BaseNode
	JoinType JoinType
	Left     Relation
	Right    Relation
	Criteria JoinCriteria
}

func (j *Join) GetChildren() []Node {
	var children []Node
	if j.Left != nil {
		children = append(children, j.Left)
	}
	if j.Right != nil {
		children = append(children, j.Right)
	}
	if j.Criteria != nil {
		children = append(children, j.Criteria)
	}
	return children
}
func (j *Join) isRelation() {}

// TableSubquery represents a subquery in the FROM clause.
type TableSubquery struct {
	BaseNode
	Query Statement
}

func (t *TableSubquery) GetChildren() []Node { return []Node{t.Query} }
func (t *TableSubquery) isRelation()         {}

// Unnest represents UNNEST(...).
type Unnest struct {
	BaseNode
	Expressions []Expression
	Ordinality  bool
}

func (u *Unnest) GetChildren() []Node {
	children := make([]Node, len(u.Expressions))
	for i, e := range u.Expressions {
		children[i] = e
	}
	return children
}
func (u *Unnest) isRelation() {}

// Values represents VALUES (...), (...).
type Values struct {
	BaseNode
	Rows [][]Expression
}

func (v *Values) GetChildren() []Node {
	var children []Node
	for _, row := range v.Rows {
		for _, expr := range row {
			children = append(children, expr)
		}
	}
	return children
}
func (v *Values) isRelation() {}
func (v *Values) isQueryBody() {}
func (v *Values) isStatement() {}

// Lateral represents LATERAL (subquery) in a FROM clause.
type Lateral struct {
	BaseNode
	Query Statement
}

func (l *Lateral) GetChildren() []Node { return []Node{l.Query} }
func (l *Lateral) isRelation()         {}

// SampledRelation represents <relation> TABLESAMPLE <type> (<percentage>).
type SampledRelation struct {
	BaseNode
	Relation         Relation
	SampleType       string // "BERNOULLI" | "SYSTEM"
	SamplePercentage Expression
}

func (s *SampledRelation) GetChildren() []Node { return []Node{s.Relation} }
func (s *SampledRelation) isRelation()         {}

// FunctionRelation represents a table function invocation name(args...) in FROM.
type FunctionRelation struct {
	BaseNode
	Name      QualifiedName
	Arguments []Expression
}

func (f *FunctionRelation) GetChildren() []Node {
	children := make([]Node, len(f.Arguments))
	for i, a := range f.Arguments {
		children[i] = a
	}
	return children
}
func (f *FunctionRelation) isRelation() {}
