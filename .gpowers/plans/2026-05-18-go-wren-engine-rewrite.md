# Go Wren Engine Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite the Java wren-engine v0.9.3 as a fully Go-based semantic SQL engine with API compatibility.

**Architecture:** ANTLR4 Go target generates parser from Trino SQL grammar; a focused Go AST layer (~45 node types) feeds a visitor-based SQL rewrite engine; DuckDB/PostgreSQL connectors execute rewritten SQL; chi-based HTTP server exposes REST API identical to the Java version.

**Tech Stack:** Go 1.22+, ANTLR4 Go target, chi v5, go-duckdb, pgx/v5, gonja (Jinja2), dominikbraun/graph (DAG), gopkg.in/yaml.v3

**Source reference:** `D:\workspace\kb_work\wren-engine-0.9.3`

---

## Phase 1: Foundation — Parser & AST

### Task 1: Initialize Go Module & Project Structure

**Files:**
- Create: `go.mod`
- Create: `cmd/wren-engine/main.go`
- Create: `Makefile`

- [ ] **Step 1: Initialize Go module and create project structure**

```bash
cd D:\workspace\kb_work\go-wren-engine
go mod init github.com/wren-engine/wren
mkdir -p cmd/wren-engine internal/parser/generated internal/parser/ast internal/parser/visitor internal/parser/formatter internal/dto internal/mdl internal/rewrite internal/analyzer/decisionpoint internal/connector/duckdb internal/connector/postgres internal/converter internal/config internal/service internal/server
```

- [ ] **Step 2: Create minimal main.go**

Create `cmd/wren-engine/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("wren-engine starting...")
	os.Exit(0)
}
```

- [ ] **Step 3: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: build test clean generate

build:
	go build -o bin/wren-engine ./cmd/wren-engine

test:
	go test ./...

clean:
	rm -rf bin/

generate:
	cd internal/parser/generated && java -jar ../../../tools/antlr-4.13.2-complete.jar -Dlanguage=Go -package generated SqlBase.g4
```

- [ ] **Step 4: Verify it builds and runs**

Run: `cd D:\workspace\kb_work\go-wren-engine && go build ./cmd/wren-engine && ./wren-engine`
Expected: `wren-engine starting...`

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/ Makefile
git commit -m "feat: initialize Go module and project structure"
```

---

### Task 2: ANTLR4 Grammar Setup & Code Generation

**Files:**
- Create: `internal/parser/generated/` (ANTLR4 generated files)
- Create: `tools/` (ANTLR4 jar)

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\trino-parser\src\main\antlr4\io\trino\sql\parser\SqlBase.g4`

- [ ] **Step 1: Add ANTLR4 Go runtime dependency**

```bash
cd D:\workspace\kb_work\go-wren-engine
go get github.com/antlr4-go/antlr/v4
```

- [ ] **Step 2: Download ANTLR4 jar**

```bash
mkdir -p tools
curl -L -o tools/antlr-4.13.2-complete.jar https://www.antlr.org/download/antlr-4.13.2-complete.jar
```

- [ ] **Step 3: Copy and adapt the Trino grammar for Go target**

Copy `D:\workspace\kb_work\wren-engine-0.9.3\trino-parser\src\main\antlr4\io\trino\sql\parser\SqlBase.g4` to `internal/parser/generated/SqlBase.g4`.

The grammar requires minor adaptations for the Go target:
- Remove Java-specific `@header` and `@members` blocks
- Replace Java inline code in parser rules with empty alternatives (we handle AST building in our Go AstBuilder)
- Keep all lexer/parser rules intact for SQL syntax compatibility

- [ ] **Step 4: Generate Go parser from grammar**

```bash
cd D:\workspace\kb_work\go-wren-engine/internal/parser/generated
java -jar ../../../tools/antlr-4.13.2-complete.jar -Dlanguage=Go -package generated SqlBase.g4
```

This generates: `sql_base_lexer.go`, `sql_base_parser.go`, `sql_base_listener.go`, `sql_base_base_listener.go`, `sql_base_visitor.go`, `sql_base_base_visitor.go`

- [ ] **Step 5: Fix generated package import path and verify compile**

```bash
cd D:\workspace\kb_work\go-wren-engine
go mod tidy
go build ./internal/parser/generated
```

- [ ] **Step 6: Commit**

```bash
git add internal/parser/generated/ tools/ go.mod go.sum
git commit -m "feat: add ANTLR4 grammar and generated Go parser"
```

---

### Task 3: AST Node Types — Core Interface & Node Base

**Files:**
- Create: `internal/parser/ast/node.go`
- Create: `internal/parser/ast/node_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\trino-parser\src\main\java\io\trino\sql\tree\Node.java`

- [ ] **Step 1: Write failing test for Node interface**

Create `internal/parser/ast/node_test.go`:

```go
package ast

import "testing"

func TestNodeLocation(t *testing.T) {
	loc := NodeLocation{Line: 1, CharPosition: 5}
	if loc.Line != 1 || loc.CharPosition != 5 {
		t.Errorf("NodeLocation not set correctly: %+v", loc)
	}
}

func TestNodeInterface(t *testing.T) {
	var _ Node = (*Query)(nil)
	var _ Node = (*Table)(nil)
	var _ Node = (*Identifier)(nil)
	var _ Node = (*LongLiteral)(nil)
	var _ Node = (*StringLiteral)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/parser/ast/`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement Node interface and base types**

Create `internal/parser/ast/node.go`:

```go
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
```

- [ ] **Step 4: Run test — still fails because Query, Table, etc. not defined. Expected. Continue to next tasks.**

- [ ] **Step 5: Commit**

```bash
git add internal/parser/ast/
git commit -m "feat: add Node interface and base types for AST layer"
```

---

### Task 4: AST Node Types — Statements

**Files:**
- Create: `internal/parser/ast/statement.go`
- Create: `internal/parser/ast/statement_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\trino-parser\src\main\java\io\trino\sql\tree\Statement.java`, `Query.java`, `QuerySpecification.java`

- [ ] **Step 1: Write failing test for Statement types**

Create `internal/parser/ast/statement_test.go`:

```go
package ast

import "testing"

func TestQueryNode(t *testing.T) {
	q := &Query{
		BaseNode: BaseNode{Location: &NodeLocation{Line: 1}},
	}
	if q.GetLocation().Line != 1 {
		t.Errorf("Query location not preserved")
	}
}

func TestQuerySpecificationNode(t *testing.T) {
	qs := &QuerySpecification{
		Select: &Select{},
	}
	if qs.Select == nil {
		t.Error("QuerySpecification Select should not be nil")
	}
}
```

- [ ] **Step 2: Implement Statement AST types**

Create `internal/parser/ast/statement.go`:

```go
package ast

// Statement is the root interface for all SQL statements.
type Statement interface {
	Node
	isStatement()
}

// Query represents a SELECT statement (possibly with WITH clause).
type Query struct {
	BaseNode
	With     *With
	Body     QueryBody
	OrderBy  []SortItem
	Limit    Expression
	Offset   Expression
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
		children = append(children, si)
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
	Select   *Select
	From     Relation
	Where    Expression
	GroupBy  *GroupBy
	Having   Expression
	OrderBy  []SortItem
	Limit    Expression
	Offset   Expression
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
		children = append(children, si)
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
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test ./internal/parser/ast/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/parser/ast/statement.go internal/parser/ast/statement_test.go
git commit -m "feat: add Statement AST types (Query, QuerySpecification)"
```

---

### Task 5: AST Node Types — Expressions & QualifiedName

**Files:**
- Create: `internal/parser/ast/expression.go`
- Create: `internal/parser/ast/qualified_name.go`
- Create: `internal/parser/ast/expression_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\trino-parser\src\main\java\io\trino\sql\tree\{Expression,DereferenceExpression,Identifier,Literal,ComparisonExpression,FunctionCall,Cast}.java`

- [ ] **Step 1: Write failing test for Expression types**

Create `internal/parser/ast/expression_test.go`:

```go
package ast

import "testing"

func TestQualifiedName(t *testing.T) {
	qn := QualifiedNameOf("catalog", "schema", "table")
	if qn.String() != "catalog.schema.table" {
		t.Errorf("QualifiedName string wrong: %s", qn.String())
	}
	if len(qn.Parts) != 3 {
		t.Errorf("QualifiedName should have 3 parts, got %d", len(qn.Parts))
	}
}

func TestComparisonExpression(t *testing.T) {
	comp := &ComparisonExpression{
		Operator: ComparisonEqual,
		Left:     &Identifier{Value: "a"},
		Right:    &LongLiteral{Value: 1},
	}
	children := comp.GetChildren()
	if len(children) != 2 {
		t.Errorf("ComparisonExpression should have 2 children, got %d", len(children))
	}
}

func TestFunctionCall(t *testing.T) {
	fn := &FunctionCall{
		Name:      QualifiedNameOf("count"),
		Arguments: []Expression{&StarExpression{}},
	}
	children := fn.GetChildren()
	if len(children) != 1 {
		t.Errorf("FunctionCall should have 1 child, got %d", len(children))
	}
}
```

- [ ] **Step 2: Implement QualifiedName**

Create `internal/parser/ast/qualified_name.go`:

```go
package ast

import "strings"

// QualifiedName represents a dot-separated name (e.g., catalog.schema.table).
type QualifiedName struct {
	Parts         []string
	OriginalParts []Identifier
}

func QualifiedNameOf(parts ...string) QualifiedName {
	original := make([]Identifier, len(parts))
	for i, p := range parts {
		original[i] = Identifier{Value: p}
	}
	return QualifiedName{Parts: parts, OriginalParts: original}
}

func (q QualifiedName) String() string {
	return strings.Join(q.Parts, ".")
}

func (q QualifiedName) HasPrefix(prefix QualifiedName) bool {
	if len(prefix.Parts) > len(q.Parts) {
		return false
	}
	for i, p := range prefix.Parts {
		if p != q.Parts[i] {
			return false
		}
	}
	return true
}

func (q QualifiedName) Suffix(n int) QualifiedName {
	return QualifiedName{
		Parts:         q.Parts[n:],
		OriginalParts: q.OriginalParts[n:],
	}
}

func (q QualifiedName) Last() string {
	if len(q.Parts) == 0 {
		return ""
	}
	return q.Parts[len(q.Parts)-1]
}
```

- [ ] **Step 3: Implement Expression AST types**

Create `internal/parser/ast/expression.go`:

```go
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
		children = append(children, si)
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/parser/ast/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/parser/ast/expression.go internal/parser/ast/qualified_name.go internal/parser/ast/expression_test.go
git commit -m "feat: add Expression AST types and QualifiedName"
```

---

### Task 6: AST Node Types — Relations

**Files:**
- Create: `internal/parser/ast/relation.go`
- Create: `internal/parser/ast/relation_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\trino-parser\src\main\java\io\trino\sql\tree\{Table,AliasedRelation,Join}.java`

- [ ] **Step 1: Write failing test**

Create `internal/parser/ast/relation_test.go`:

```go
package ast

import "testing"

func TestTableNode(t *testing.T) {
	tbl := &Table{Name: QualifiedNameOf("catalog", "schema", "orders")}
	if tbl.Name.String() != "catalog.schema.orders" {
		t.Errorf("Table name not correct: %s", tbl.Name.String())
	}
}

func TestJoinNode(t *testing.T) {
	join := &Join{
		JoinType: JoinTypeLeft,
		Left:     &Table{Name: QualifiedNameOf("orders")},
		Right:    &Table{Name: QualifiedNameOf("customers")},
		Criteria: &JoinOn{Expression: &ComparisonExpression{}},
	}
	children := join.GetChildren()
	if len(children) != 3 {
		t.Errorf("Join should have 3 children, got %d", len(children))
	}
}
```

- [ ] **Step 2: Implement Relation AST types**

Create `internal/parser/ast/relation.go`:

```go
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
```

- [ ] **Step 3: Run test**

Run: `go test ./internal/parser/ast/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/parser/ast/relation.go internal/parser/ast/relation_test.go
git commit -m "feat: add Relation AST types (Table, Join, AliasedRelation)"
```

---

### Task 7: AST Node Types — Query Parts & Types

**Files:**
- Create: `internal/parser/ast/query.go`
- Create: `internal/parser/ast/types.go`
- Create: `internal/parser/ast/query_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\trino-parser\src\main\java\io\trino\sql\tree\{Select,With,WithQuery,SortItem,Window,DataType,ColumnDefinition}.java`

- [ ] **Step 1: Write test for query parts**

Create `internal/parser/ast/query_test.go`:

```go
package ast

import "testing"

func TestWithQuery(t *testing.T) {
	wq := &WithQuery{
		Name:  &Identifier{Value: "cte"},
		Query: &Query{},
	}
	children := wq.GetChildren()
	if len(children) != 2 {
		t.Errorf("WithQuery should have 2 children, got %d", len(children))
	}
}

func TestSelect(t *testing.T) {
	sel := &Select{
		Distinct: true,
		SelectItems: []SelectItem{
			&SingleColumn{Expression: &Identifier{Value: "a"}},
			&AllColumns{},
		},
	}
	children := sel.GetChildren()
	if len(children) != 2 {
		t.Errorf("Select should have 2 children, got %d", len(children))
	}
}
```

- [ ] **Step 2: Implement query part types and DataType/ColumnDefinition**

Create `internal/parser/ast/query.go`:

```go
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
		children = append(children, si)
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
```

Create `internal/parser/ast/types.go`:

```go
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
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/parser/ast/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/parser/ast/query.go internal/parser/ast/types.go internal/parser/ast/query_test.go
git commit -m "feat: add Query part, Window, and DataType AST types"
```

---

### Task 8: AST Visitor Pattern

**Files:**
- Create: `internal/parser/visitor/visitor.go`
- Create: `internal/parser/visitor/visitor_test.go`

- [ ] **Step 1: Write failing test for visitor**

Create `internal/parser/visitor/visitor_test.go`:

```go
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
```

- [ ] **Step 2: Implement visitor**

Create `internal/parser/visitor/visitor.go`:

```go
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
```

- [ ] **Step 3: Run test**

Run: `go test ./internal/parser/visitor/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/parser/visitor/
git commit -m "feat: add AST visitor pattern with BaseVisitor and test"
```

---

### Task 9: Parser Public API & SQL Formatter

**Files:**
- Create: `internal/parser/parser.go`
- Create: `internal/parser/formatter/formatter.go`
- Create: `internal/parser/formatter/formatter_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\trino-parser\src\main\java\io\trino\sql\parser\SqlParser.java`, `D:\workspace\kb_work\wren-engine-0.9.3\trino-parser\src\main\java\io\trino\sql\SqlFormatter.java`

- [ ] **Step 1: Implement parser API**

Create `internal/parser/parser.go`:

```go
package parser

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/wren-engine/wren/internal/parser/ast"
	generated "github.com/wren-engine/wren/internal/parser/generated"
)

// ParseSQL parses a SQL string into an AST Statement.
func ParseSQL(sql string) (ast.Statement, error) {
	lexer := generated.NewSqlBaseLexer(antlr.NewInputStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, 0)
	p := generated.NewSqlBaseParser(stream)
	tree := p.Statements()

	builder := &AstBuilder{}
	result := builder.Visit(tree)
	if result == nil {
		return nil, fmt.Errorf("failed to parse SQL: %s", sql)
	}
	stmt, ok := result.(ast.Statement)
	if !ok {
		return nil, fmt.Errorf("parse result is not a Statement: %T", result)
	}
	return stmt, nil
}

// ParseExpression parses a SQL expression into an AST Expression.
func ParseExpression(sql string) (ast.Expression, error) {
	lexer := generated.NewSqlBaseLexer(antlr.NewInputStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, 0)
	p := generated.NewSqlBaseParser(stream)
	tree := p.Expression()

	builder := &AstBuilder{}
	result := builder.Visit(tree)
	if result == nil {
		return nil, fmt.Errorf("failed to parse expression: %s", sql)
	}
	expr, ok := result.(ast.Expression)
	if !ok {
		return nil, fmt.Errorf("parse result is not an Expression: %T", result)
	}
	return expr, nil
}

// AstBuilder walks the ANTLR4 parse tree and produces Go AST nodes.
type AstBuilder struct{}

// Visit dispatches to the appropriate method based on the parse tree node type.
func (b *AstBuilder) Visit(tree antlr.ParseTree) any {
	return nil // Will be filled in incrementally as grammar rules are mapped
}
```

- [ ] **Step 2: Implement SQL formatter**

Create `internal/parser/formatter/formatter.go`:

```go
package formatter

import (
	"fmt"
	"strings"

	"github.com/wren-engine/wren/internal/parser/ast"
)

type Dialect int

const (
	DialectStandard Dialect = iota
	DialectDuckDB
	DialectPostgreSQL
)

func FormatSQL(stmt ast.Statement) string {
	return FormatSQLDialect(stmt, DialectStandard)
}

func FormatSQLDialect(stmt ast.Statement, dialect Dialect) string {
	f := &formatter{dialect: dialect}
	f.format(stmt)
	return f.builder.String()
}

type formatter struct {
	dialect Dialect
	builder strings.Builder
}

func (f *formatter) format(node ast.Node) {
	switch n := node.(type) {
	case *ast.Query:
		f.formatQuery(n)
	case *ast.QuerySpecification:
		f.formatQuerySpec(n)
	default:
		f.builder.WriteString(fmt.Sprintf("/* unhandled: %T */", n))
	}
}

func (f *formatter) formatQuery(q *ast.Query) {
	if q.With != nil {
		f.formatWith(q.With)
		f.builder.WriteString(" ")
	}
	f.format(q.Body)
	if len(q.OrderBy) > 0 {
		f.builder.WriteString(" ORDER BY ")
		for i, si := range q.OrderBy {
			if i > 0 {
				f.builder.WriteString(", ")
			}
			f.formatExpr(si.SortKey)
			if si.Ordering == ast.OrderingDesc {
				f.builder.WriteString(" DESC")
			}
		}
	}
	if q.Limit != nil {
		f.builder.WriteString(" LIMIT ")
		f.formatExpr(q.Limit)
	}
}

func (f *formatter) formatQuerySpec(qs *ast.QuerySpecification) {
	f.builder.WriteString("SELECT ")
	if qs.Select != nil && qs.Select.Distinct {
		f.builder.WriteString("DISTINCT ")
	}
	if qs.Select != nil {
		for i, item := range qs.Select.SelectItems {
			if i > 0 {
				f.builder.WriteString(", ")
			}
			f.formatSelectItem(item)
		}
	}
	if qs.From != nil {
		f.builder.WriteString(" FROM ")
		f.formatRelation(qs.From)
	}
	if qs.Where != nil {
		f.builder.WriteString(" WHERE ")
		f.formatExpr(qs.Where)
	}
	if qs.GroupBy != nil && len(qs.GroupBy.Expressions) > 0 {
		f.builder.WriteString(" GROUP BY ")
		for i, e := range qs.GroupBy.Expressions {
			if i > 0 {
				f.builder.WriteString(", ")
			}
			f.formatExpr(e)
		}
	}
}

func (f *formatter) formatWith(w *ast.With) {
	f.builder.WriteString("WITH ")
	if w.Recursive {
		f.builder.WriteString("RECURSIVE ")
	}
	for i, q := range w.Queries {
		if i > 0 {
			f.builder.WriteString(", ")
		}
		f.formatIdent(q.Name)
		f.builder.WriteString(" AS (")
		f.format(q.Query)
		f.builder.WriteString(")")
	}
}

func (f *formatter) formatSelectItem(item ast.SelectItem) {
	switch s := item.(type) {
	case *ast.SingleColumn:
		f.formatExpr(s.Expression)
		if s.Alias != nil {
			f.builder.WriteString(" AS ")
			f.formatIdent(s.Alias)
		}
	case *ast.AllColumns:
		if s.QualifiedName != nil {
			f.builder.WriteString(s.QualifiedName.String())
			f.builder.WriteString(".")
		}
		f.builder.WriteString("*")
	}
}

func (f *formatter) formatRelation(r ast.Relation) {
	switch n := r.(type) {
	case *ast.Table:
		f.builder.WriteString(n.Name.String())
		if n.Alias != nil {
			f.builder.WriteString(" AS ")
			f.formatIdent(n.Alias)
		}
	case *ast.AliasedRelation:
		f.formatRelation(n.Relation)
		f.builder.WriteString(" AS ")
		f.formatIdent(n.Alias)
	case *ast.Join:
		f.formatRelation(n.Left)
		switch n.JoinType {
		case ast.JoinTypeInner:
			f.builder.WriteString(" JOIN ")
		case ast.JoinTypeLeft:
			f.builder.WriteString(" LEFT JOIN ")
		case ast.JoinTypeRight:
			f.builder.WriteString(" RIGHT JOIN ")
		case ast.JoinTypeFull:
			f.builder.WriteString(" FULL JOIN ")
		case ast.JoinTypeCross:
			f.builder.WriteString(" CROSS JOIN ")
		case ast.JoinTypeImplicit:
			f.builder.WriteString(", ")
		}
		f.formatRelation(n.Right)
		if n.Criteria != nil {
			switch c := n.Criteria.(type) {
			case *ast.JoinOn:
				f.builder.WriteString(" ON ")
				f.formatExpr(c.Expression)
			case *ast.JoinUsing:
				f.builder.WriteString(" USING (")
				for i, col := range c.Columns {
					if i > 0 {
						f.builder.WriteString(", ")
					}
					f.builder.WriteString(col.Value)
				}
				f.builder.WriteString(")")
			}
		}
	default:
		f.builder.WriteString(fmt.Sprintf("/* unhandled relation: %T */", n))
	}
}

func (f *formatter) formatExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		f.formatIdent(e)
	case *ast.LongLiteral:
		fmt.Fprintf(&f.builder, "%d", e.Value)
	case *ast.StringLiteral:
		fmt.Fprintf(&f.builder, "'%s'", e.Value)
	case *ast.DoubleLiteral:
		fmt.Fprintf(&f.builder, "%g", e.Value)
	case *ast.BooleanLiteral:
		if e.Value {
			f.builder.WriteString("TRUE")
		} else {
			f.builder.WriteString("FALSE")
		}
	case *ast.NullLiteral:
		f.builder.WriteString("NULL")
	case *ast.DereferenceExpression:
		f.formatExpr(e.Base)
		f.builder.WriteString(".")
		f.formatIdent(e.Field)
	case *ast.ComparisonExpression:
		f.formatExpr(e.Left)
		fmt.Fprintf(&f.builder, " %s ", e.Operator)
		f.formatExpr(e.Right)
	case *ast.ArithmeticBinaryExpression:
		f.formatExpr(e.Left)
		fmt.Fprintf(&f.builder, " %s ", e.Operator)
		f.formatExpr(e.Right)
	case *ast.LogicalBinaryExpression:
		f.formatExpr(e.Left)
		fmt.Fprintf(&f.builder, " %s ", e.Operator)
		f.formatExpr(e.Right)
	case *ast.FunctionCall:
		f.builder.WriteString(e.Name.String())
		f.builder.WriteString("(")
		if e.Distinct {
			f.builder.WriteString("DISTINCT ")
		}
		for i, arg := range e.Arguments {
			if i > 0 {
				f.builder.WriteString(", ")
			}
			f.formatExpr(arg)
		}
		f.builder.WriteString(")")
	case *ast.Cast:
		f.builder.WriteString("CAST(")
		f.formatExpr(e.Expression)
		f.builder.WriteString(" AS ")
		f.builder.WriteString(e.Type.Name)
		f.builder.WriteString(")")
	case *ast.StarExpression:
		f.builder.WriteString("*")
	case *ast.IsNotNullPredicate:
		f.formatExpr(e.Value)
		if e.Not {
			f.builder.WriteString(" IS NOT NULL")
		} else {
			f.builder.WriteString(" IS NULL")
		}
	default:
		fmt.Fprintf(&f.builder, "/* unhandled expr: %T */", e)
	}
}

func (f *formatter) formatIdent(id *ast.Identifier) {
	if id == nil {
		return
	}
	if id.Delimited {
		fmt.Fprintf(&f.builder, `"%s"`, id.Value)
	} else {
		f.builder.WriteString(id.Value)
	}
}
```

- [ ] **Step 3: Write formatter test**

Create `internal/parser/formatter/formatter_test.go`:

```go
package formatter

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser/ast"
)

func TestFormatSimpleQuery(t *testing.T) {
	query := &ast.Query{
		Body: &ast.QuerySpecification{
			Select: &ast.Select{
				SelectItems: []ast.SelectItem{
					&ast.SingleColumn{Expression: &ast.LongLiteral{Value: 1}},
				},
			},
		},
	}
	result := FormatSQL(query)
	if result != "SELECT 1" {
		t.Errorf("Expected 'SELECT 1', got '%s'", result)
	}
}

func TestFormatTableQuery(t *testing.T) {
	query := &ast.Query{
		Body: &ast.QuerySpecification{
			Select: &ast.Select{
				SelectItems: []ast.SelectItem{
					&ast.SingleColumn{Expression: &ast.Identifier{Value: "a"}},
					&ast.SingleColumn{Expression: &ast.Identifier{Value: "b"}},
				},
			},
			From: &ast.Table{Name: ast.QualifiedNameOf("orders")},
			Where: &ast.ComparisonExpression{
				Operator: ast.ComparisonGreaterThan,
				Left:     &ast.Identifier{Value: "c"},
				Right:    &ast.LongLiteral{Value: 5},
			},
		},
	}
	result := FormatSQL(query)
	expected := "SELECT a, b FROM orders WHERE c > 5"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/parser/formatter/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/parser/parser.go internal/parser/formatter/
git commit -m "feat: add parser public API and SQL formatter"
```

---

## Phase 2: Core MDL & Rewrite Engine

### Task 10: DTO Structs — All MDL Types

**Files:**
- Create: `internal/dto/manifest.go`
- Create: `internal/dto/model.go`
- Create: `internal/dto/column.go`
- Create: `internal/dto/relationship.go`
- Create: `internal/dto/enum.go`
- Create: `internal/dto/metric.go`
- Create: `internal/dto/view.go`
- Create: `internal/dto/macro.go`
- Create: `internal/dto/date_spine.go`
- Create: `internal/dto/common.go`
- Create: `internal/dto/manifest_test.go`

**Reference:** All files in `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\dto\`

- [ ] **Step 1: Write failing test for Manifest JSON deserialization**

Create `internal/dto/manifest_test.go`:

```go
package dto

import (
	"encoding/json"
	"testing"
)

func TestManifestJSONRoundTrip(t *testing.T) {
	original := &Manifest{
		Catalog: "wren",
		Schema:  "public",
		Models: []Model{
			{Name: "orders", RefSql: strPtr("SELECT * FROM raw_orders"), Columns: []Column{{Name: "id", Type: "INTEGER"}}},
		},
		Relationships: []Relationship{
			{Name: "orders_customer", Models: []string{"orders", "customers"}, JoinType: JoinTypeManyToMany, Condition: "orders.customer_id = customers.id"},
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var parsed Manifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if parsed.Catalog != "wren" {
		t.Errorf("Catalog mismatch: got %s", parsed.Catalog)
	}
	if len(parsed.Models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(parsed.Models))
	}
}

func TestManifestEmptyDefaults(t *testing.T) {
	jsonStr := `{"catalog": "c", "schema": "s"}`
	var m Manifest
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if m.Models == nil {
		t.Error("Models should default to empty slice")
	}
}

func strPtr(s string) *string { return &s }
```

- [ ] **Step 2: Implement all DTO structs** (see spec section 4.1 for exact field definitions)

Implement all files: `common.go`, `column.go`, `model.go`, `relationship.go`, `enum.go`, `metric.go`, `view.go`, `macro.go`, `date_spine.go`, `manifest.go`

Key points:
- All JSON tags match Java `@JsonProperty` names exactly
- `Manifest.UnmarshalJSON` provides default empty slices (not nil)
- `Column.GetExpression()` returns expression or `"name"` as default (matches Java)
- `JoinType` is a string enum
- `Relationship` has `ManySideSortKeys` with nested `SortKey` struct

- [ ] **Step 3: Run test**

Run: `go test ./internal/dto/`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/dto/
git commit -m "feat: add all MDL DTO structs with JSON serialization"
```

---

### Task 11: WrenMDL, AnalyzedMDL & Jinja Rendering

**Files:**
- Create: `internal/mdl/wren_mdl.go`
- Create: `internal/mdl/wren_mdl_test.go`
- Create: `internal/mdl/analyzed_mdl.go`
- Create: `internal/mdl/jinja.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\WrenMDL.java`, `wren-base\src\main\java\io\wren\base\jinjava\`

- [ ] **Step 1: Add gonja dependency**

```bash
go get github.com/noirbizarre/gonja/v2
```

- [ ] **Step 2: Write test for WrenMDL** (test Model lookup, JSON parsing, IsObjectExist)

- [ ] **Step 3: Implement WrenMDL** — index models/metrics/relationships/views/enums by name, provide Get/List methods

- [ ] **Step 4: Implement AnalyzedMDL** — wraps WrenMDL + WrenDataLineage

- [ ] **Step 5: Implement Jinja rendering** — uses gonja to process macro tags + column expressions

- [ ] **Step 6: Run test and commit**

```bash
go test ./internal/mdl/
git add internal/mdl/
git commit -m "feat: add WrenMDL, AnalyzedMDL, and Jinja macro rendering"
```

---

### Task 12: Analysis Struct & SessionContext

**Files:**
- Create: `internal/analyzer/analysis.go`
- Create: `internal/analyzer/session_context.go`
- Create: `internal/analyzer/analysis_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\sqlrewrite\analyzer\Analysis.java`, `wren-base\src\main\java\io\wren\base\SessionContext.java`

- [ ] **Step 1: Write test and implement SessionContext and Analysis**

SessionContext: catalog, schema, enableDynamicFields
Analysis: tracks Tables, Models, Metrics, CumulativeMetrics, Views, CollectedColumns with Add/Get methods

- [ ] **Step 2: Run test and commit**

---

### Task 13: WrenPlanner & WrenRule Interface

**Files:**
- Create: `internal/rewrite/rule.go`
- Create: `internal/rewrite/planner.go`
- Create: `internal/rewrite/planner_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\sqlrewrite\WrenPlanner.java`, `WrenRule.java`

- [ ] **Step 1: Implement WrenRule interface and WrenPlanner**

WrenRule.Apply(stmt, ctx, analyzedMDL) → Statement
WrenPlanner.Rewrite(sql, ctx, analyzedMDL) → string (format→reparse between each rule)

- [ ] **Step 2: Run test and commit**

---

### Task 14: Rewrite Rules — GenerateViewRewrite, WithRewriter

**Files:**
- Create: `internal/rewrite/view_rewrite.go`
- Create: `internal/rewrite/with_rewriter.go`
- Create: `internal/rewrite/view_rewrite_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\sqlrewrite\GenerateViewRewrite.java`, `WithRewriter.java`

- [ ] **Step 1: Implement WithRewriter** — prepends CTEs to a Query's WITH clause

- [ ] **Step 2: Implement GenerateViewRewrite** — analyzes view references, builds CTEs, topologically sorts view dependencies

- [ ] **Step 3: Run test and commit**

---

### Task 15: Rewrite Rules — WrenSqlRewrite (Core)

**Files:**
- Create: `internal/rewrite/wren_sql.go`
- Create: `internal/rewrite/wren_sql_test.go`
- Create: `internal/rewrite/descriptor.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\sqlrewrite\WrenSqlRewrite.java`, `QueryDescriptor.java`, `RelationInfo.java`, `ModelSqlRender.java`, `MetricSqlRender.java`

- [ ] **Step 1: Implement QueryDescriptor** — describes a CTE to be generated (name, SQL, required objects)

- [ ] **Step 2: Implement WrenSqlRewrite** — the main rule: analyze→build descriptors→topological sort→add CTEs→replace table references

- [ ] **Step 3: Implement ModelSqlRender** — generates SELECT ... FROM (refSql) or baseObject

- [ ] **Step 4: Run test and commit**

---

### Task 16: Rewrite Rules — MetricRollupRewrite, EnumRewrite

**Files:**
- Create: `internal/rewrite/metric_rollup.go`
- Create: `internal/rewrite/enum_rewrite.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\sqlrewrite\MetricRollupRewrite.java`, `EnumRewrite.java`

- [ ] **Step 1: Implement MetricRollupRewrite** — handles cumulative metric time-grain rollups

- [ ] **Step 2: Implement EnumRewrite** — replaces enum column comparisons with IN expressions

- [ ] **Step 3: Register all rules in WrenPlanner AllRules slice**

- [ ] **Step 4: Run test and commit**

---

## Phase 3: Connectors & Converters

### Task 17: Connector Interface & DuckDB Connector

**Files:**
- Create: `internal/connector/connector.go`
- Create: `internal/connector/duckdb/connector.go`
- Create: `internal/connector/duckdb/connector_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\client\duckdb\DuckdbClient.java`

- [ ] **Step 1: Add go-duckdb dependency**

```bash
go get github.com/marcboeker/go-duckdb
```

- [ ] **Step 2: Implement connector interface** (Client, Metadata, RecordIterator)

- [ ] **Step 3: Implement DuckDB connector** (sql.DB pool, init SQL, session SQL, DirectQuery, DescribeQuery, DirectDDL)

- [ ] **Step 4: Run test and commit**

---

### Task 18: PostgreSQL Connector

**Files:**
- Create: `internal/connector/postgres/connector.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\config\PostgresConfig.java`

- [ ] **Step 1: Add pgx dependency**

```bash
go get github.com/jackc/pgx/v5
```

- [ ] **Step 2: Implement PostgreSQL connector** (pgxpool, DirectQuery, DescribeQuery, DirectDDL)

- [ ] **Step 3: Verify compile and commit**

---

### Task 19: SQL Dialect Converters

**Files:**
- Create: `internal/converter/converter.go`
- Create: `internal/converter/duckdb.go`
- Create: `internal/converter/duckdb_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-main\src\main\java\io\wren\main\connector\duckdb\DuckDBSqlConverter.java`

- [ ] **Step 1: Implement SqlConverter interface**

- [ ] **Step 2: Implement DuckDBSqlConverter** — parse SQL, apply RewriteArray and RewriteFunction, format with DuckDB dialect

- [ ] **Step 3: Run test and commit**

---

## Phase 4: Services & Server

### Task 20: Configuration Management

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\config\WrenConfig.java`

- [ ] **Step 1: Add yaml dependency**

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Implement Config structs** (ServerConfig, WrenConfig, DuckDBConfig, PostgresConfig)

- [ ] **Step 3: Implement ConfigManager** (Get/Set, LoadConfig from YAML)

- [ ] **Step 4: Run test and commit**

---

### Task 21: Error Handling

**Files:**
- Create: `internal/server/errors.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-base\src\main\java\io\wren\base\WrenException.java`

- [ ] **Step 1: Implement WrenError** — Code, Type, Message, Cause; ErrorType enum; WriteError HTTP mapper matching Java ErrorMessageDto format

- [ ] **Step 2: Verify compile and commit**

---

### Task 22: PreviewService & ValidationService

**Files:**
- Create: `internal/service/preview.go`
- Create: `internal/service/validation.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-main\src\main\java\io\wren\main\PreviewService.java`

- [ ] **Step 1: Implement PreviewService** — Preview, DryPlan, DryRun using WrenPlanner + SqlConverter + Metadata

- [ ] **Step 2: Implement ValidationService** — rule registry, Validate dispatch, ColumnIsValid rule

- [ ] **Step 3: Verify compile and commit**

---

### Task 23: HTTP Server & API Handlers

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/mdl_handler.go`
- Create: `internal/server/analysis_handler.go`
- Create: `internal/server/duckdb_handler.go`
- Create: `internal/server/config_handler.go`
- Update: `cmd/wren-engine/main.go`

**Reference:** All resource files in `D:\workspace\kb_work\wren-engine-0.9.3\wren-main\src\main\java\io\wren\main\web\`

- [ ] **Step 1: Add chi dependency**

```bash
go get github.com/go-chi/chi/v5
```

- [ ] **Step 2: Implement Server** — chi router, middleware, route registration

- [ ] **Step 3: Implement MDL handler** — /v1/mdl/preview, /v1/mdl/dry-plan, /v1/mdl/dry-run, /v1/mdl/validate/{ruleName}, /v2/mdl/dry-plan

- [ ] **Step 4: Implement Analysis handler** — /v1/analysis/sql, /v2/analysis/sql, /v2/analysis/sqls

- [ ] **Step 5: Implement DuckDB handler** — /v1/data-source/duckdb/query, init-sql GET/PUT/PATCH, session-sql GET/PUT/PATCH

- [ ] **Step 6: Implement Config handler** — /v1/config GET/GET/{name}/DELETE/PATCH

- [ ] **Step 7: Update main.go** — load config, create connectors, create services, start server

- [ ] **Step 8: Run integration test and commit**

---

## Phase 5: Integration & Testing

### Task 24: End-to-End Integration Test

**Files:**
- Create: `internal/server/server_test.go`

**Reference:** `D:\workspace\kb_work\wren-engine-0.9.3\wren-tests\src\test\java\io\wren\testing\TestMDLResource.java`

- [ ] **Step 1: Write integration test** — start server, POST /v1/mdl/preview with test manifest, verify QueryResultDto response

- [ ] **Step 2: Write dry-plan test** — verify /v1/mdl/dry-plan returns rewritten SQL

- [ ] **Step 3: Write DuckDB query test** — verify /v1/data-source/duckdb/query works

- [ ] **Step 4: Run all tests and commit**

---

### Task 25: Docker Build & Final Polish

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yaml`

- [ ] **Step 1: Create Dockerfile** — multi-stage build (builder → runner), Go 1.22, copy binary, expose 8080

- [ ] **Step 2: Create docker-compose.yaml** — wren-engine service with DuckDB, volume for etc/mdl

- [ ] **Step 3: Verify Docker build**

```bash
docker build -t go-wren-engine .
```

- [ ] **Step 4: Commit**

```bash
git add Dockerfile docker-compose.yaml
git commit -m "feat: add Docker build and docker-compose"
```
