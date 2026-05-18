package ast

// Select represents the SELECT clause.
type Select struct {
	BaseNode
	Distinct    bool
	SelectItems []SelectItem
}

func (s *Select) GetChildren() []Node {
	children := make([]Node, len(s.SelectItems))
	for i, item := range s.SelectItems {
		children[i] = item
	}
	return children
}

// SelectItem is satisfied by SingleColumn and AllColumns.
type SelectItem interface {
	Node
	isSelectItem()
}

// SingleColumn represents a single column in SELECT.
type SingleColumn struct {
	BaseNode
	Expression Expression
	Alias      *Identifier
}

func (s *SingleColumn) GetChildren() []Node {
	if s.Alias != nil {
		return []Node{s.Expression, s.Alias}
	}
	return []Node{s.Expression}
}
func (s *SingleColumn) isSelectItem() {}

// AllColumns represents SELECT * or SELECT t.*.
type AllColumns struct {
	BaseNode
	QualifiedName *QualifiedName
}

func (a *AllColumns) GetChildren() []Node { return nil }
func (a *AllColumns) isSelectItem()       {}

// With represents the WITH clause.
type With struct {
	BaseNode
	Recursive bool
	Queries   []WithQuery
}

func (w *With) GetChildren() []Node {
	children := make([]Node, len(w.Queries))
	for i, q := range w.Queries {
		children[i] = &q
	}
	return children
}

// WithQuery represents a single CTE.
type WithQuery struct {
	BaseNode
	Name        *Identifier
	Query       Statement
	ColumnNames []Identifier
}

func (w *WithQuery) GetChildren() []Node {
	var children []Node
	if w.Name != nil {
		children = append(children, w.Name)
	}
	if w.Query != nil {
		children = append(children, w.Query)
	}
	return children
}

// SortItem represents ORDER BY expr [ASC|DESC].
type SortItem struct {
	BaseNode
	SortKey      Expression
	Ordering     Ordering
	NullOrdering NullOrdering
}

type Ordering string

const (
	OrderingAsc  Ordering = "ASC"
	OrderingDesc Ordering = "DESC"
)

type NullOrdering string

const (
	NullOrderingFirst       NullOrdering = "FIRST"
	NullOrderingLast        NullOrdering = "LAST"
	NullOrderingUnspecified NullOrdering = "UNSPECIFIED"
)

func (s *SortItem) GetChildren() []Node { return []Node{s.SortKey} }

// Window represents OVER (...).
type Window struct {
	BaseNode
	PartitionBy []Expression
	OrderBy     []SortItem
	Frame       *WindowFrame
}

func (w *Window) GetChildren() []Node {
	var children []Node
	for _, e := range w.PartitionBy {
		children = append(children, e)
	}
	for _, si := range w.OrderBy {
		children = append(children, &si)
	}
	if w.Frame != nil {
		children = append(children, w.Frame)
	}
	return children
}

// WindowFrame represents ROWS/RANGE BETWEEN ... AND ...
type WindowFrame struct {
	BaseNode
	Type  FrameType
	Start FrameBound
	End   FrameBound
}

type FrameType string

const (
	FrameTypeRows  FrameType = "ROWS"
	FrameTypeRange FrameType = "RANGE"
)

type FrameBound struct {
	BaseNode
	Type  BoundType
	Value Expression
}

type BoundType string

const (
	BoundTypeUnboundedPreceding BoundType = "UNBOUNDED_PRECEDING"
	BoundTypeUnboundedFollowing BoundType = "UNBOUNDED_FOLLOWING"
	BoundTypePreceding          BoundType = "PRECEDING"
	BoundTypeFollowing          BoundType = "FOLLOWING"
	BoundTypeCurrentRow         BoundType = "CURRENT_ROW"
)

func (w *WindowFrame) GetChildren() []Node {
	var children []Node
	if w.Start.Value != nil {
		children = append(children, w.Start.Value)
	}
	if w.End.Value != nil {
		children = append(children, w.End.Value)
	}
	return children
}
