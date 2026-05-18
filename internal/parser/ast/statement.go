package ast

// Statement is the root interface for all SQL statements.
type Statement interface {
	Node
	isStatement()
}

// Query represents a SELECT statement (possibly with WITH clause).
type Query struct {
	BaseNode
	With    *With
	Body    QueryBody
	OrderBy []SortItem
	Limit   Expression
	Offset  Expression
}

func (q *Query) GetChildren() []Node {
	var children []Node
	if q.With != nil {
		children = append(children, q.With)
	}
	if q.Body != nil {
		children = append(children, q.Body)
	}
	for _, si := range q.OrderBy {
		children = append(children, &si)
	}
	if q.Limit != nil {
		children = append(children, q.Limit)
	}
	return children
}

func (q *Query) isStatement() {}

// QueryBody is satisfied by QuerySpecification and SetOperation.
type QueryBody interface {
	Node
	isQueryBody()
}

// QuerySpecification represents a simple SELECT ... FROM ... WHERE ... query.
type QuerySpecification struct {
	BaseNode
	Select  *Select
	From    Relation
	Where   Expression
	GroupBy *GroupBy
	Having  Expression
	OrderBy []SortItem
	Limit   Expression
	Offset  Expression
}

func (qs *QuerySpecification) GetChildren() []Node {
	var children []Node
	if qs.Select != nil {
		children = append(children, qs.Select)
	}
	if qs.From != nil {
		children = append(children, qs.From)
	}
	if qs.Where != nil {
		children = append(children, qs.Where)
	}
	if qs.GroupBy != nil {
		children = append(children, qs.GroupBy)
	}
	for _, si := range qs.OrderBy {
		children = append(children, &si)
	}
	if qs.Limit != nil {
		children = append(children, qs.Limit)
	}
	return children
}

func (qs *QuerySpecification) isQueryBody() {}
func (qs *QuerySpecification) isStatement() {}

// GroupBy represents a GROUP BY clause.
type GroupBy struct {
	BaseNode
	Expressions []Expression
	Sets        bool
}

func (g *GroupBy) GetChildren() []Node {
	children := make([]Node, len(g.Expressions))
	for i, e := range g.Expressions {
		children[i] = e
	}
	return children
}
