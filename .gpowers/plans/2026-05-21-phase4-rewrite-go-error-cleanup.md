# Phase 4 Implementation Plan: Rewrite Go-Error Cleanup

> **For agentic workers:** REQUIRED SUB-SKILL: Use `gpowers:subagent-driven-development` (recommended) or `gpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate all 10 `go-error` cases in `baseline.json` by fixing two root causes — RC-A (jinja expressions leaking into generated SQL) and RC-B (visitor nil-pointer panics on ANTLR error-recovery trees).

**Architecture:** Wire `mdl.RenderJinja` into `WrenMDLFromManifest` so all manifest-loading paths expand jinja macros before SQL rendering. Add defensive nil-checks to `internal/parser/ast_builder.go` visitor methods so ANTLR error-recovery trees never panic. Cluster fixes by dependency: calc-field/relationship → Metric → CumulativeMetric/roll_up → View. Each cluster gets focused unit tests. End with baseline rotation.

**Tech Stack:** Go 1.26, ANTLR4, make

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/mdl/wren_mdl.go` | Modify | Call `RenderJinja` inside `WrenMDLFromManifest` |
| `internal/parser/ast_builder.go` | Modify | Add nil-checks to 12 visitor methods |
| `internal/parser/ast_builder_test.go` | Modify | Add nil-context regression test |
| `internal/rewrite/sql_rewrite_test.go` | Create | Focused tests for 10 go-error cases |
| `tools/probe-rewrite-rules.go` | Create (temp) | Per-rule format dump to verify RC-A |
| `testdata/difftest/baseline.json` | Rotate | `make difftest-accept` after all fixes |
| `docs/phase3-scoreboard.md` | Modify | Update pass-rate numbers |

---

## Task 1: RC-A Fix — Wire RenderJinja into Manifest Loading

**Files:**
- Modify: `internal/mdl/wren_mdl.go`

### Step 1: Modify `WrenMDLFromManifest`

Replace the function signature/body start:

```go
// WrenMDLFromManifest creates a WrenMDL from a Manifest.
func WrenMDLFromManifest(manifest *dto.Manifest) *WrenMDL {
	manifest = RenderJinja(manifest)
	m := &WrenMDL{
```

The rest of the function body (lines 27–60) is unchanged.

**Why this works:** `RenderJinja` mutates `manifest.Models[*].Columns[*].Expression` in-place, replacing `{{ macro(...) }}` with the macro body. The tpch manifest defines `concat` and `callConcat` macros (both with empty bodies), so jinja expressions become empty strings. `Column.GetExpression()` then falls back to `fmt.Sprintf("\"%s\"", c.Name)` — safe SQL.

### Step 2: Verify the change compiles

```bash
go build ./internal/mdl/...
```

Expected: exits 0, no output.

### Step 3: Run existing jinja tests

```bash
go test ./internal/mdl/... -run TestRenderJinja -v
```

Expected: 5 tests pass.

### Step 4: Commit

```bash
git add internal/mdl/wren_mdl.go
git commit -m "fix(p4/rc-a): wire RenderJinja into WrenMDLFromManifest"
```

---

## Task 2: RC-B Fix — Defensive Nil-Checks in ast_builder.go

**Files:**
- Modify: `internal/parser/ast_builder.go`
- Modify: `internal/parser/ast_builder_test.go`

### Step 1: Fix `VisitAliasedRelation` (lines 369–382)

Replace:

```go
func (b *AstBuilder) VisitAliasedRelation(ctx *generated.AliasedRelationContext) interface{} {
	rel := b.visit(ctx.RelationPrimary()).(ast.Relation)
	if ctx.Identifier() == nil && ctx.ColumnAliases() == nil {
		return rel
	}
	alias := b.visitIdentifier(ctx.Identifier())
	ar := &ast.AliasedRelation{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Relation: rel, Alias: alias}
	if ctx.ColumnAliases() != nil {
		for _, id := range ctx.ColumnAliases().AllIdentifier() {
			ar.ColumnNames = append(ar.ColumnNames, *b.visitIdentifier(id))
		}
	}
	return ar
}
```

With:

```go
func (b *AstBuilder) VisitAliasedRelation(ctx *generated.AliasedRelationContext) interface{} {
	var rel ast.Relation
	if ctx.RelationPrimary() != nil {
		rel = b.visit(ctx.RelationPrimary()).(ast.Relation)
	}
	if ctx.Identifier() == nil && ctx.ColumnAliases() == nil {
		return rel
	}
	var alias *ast.Identifier
	if ctx.Identifier() != nil {
		alias = b.visitIdentifier(ctx.Identifier())
	}
	ar := &ast.AliasedRelation{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Relation: rel, Alias: alias}
	if ctx.ColumnAliases() != nil {
		for _, id := range ctx.ColumnAliases().AllIdentifier() {
			if id != nil {
				ar.ColumnNames = append(ar.ColumnNames, *b.visitIdentifier(id))
			}
		}
	}
	return ar
}
```

### Step 2: Fix `VisitTableName` (lines 384–386)

Replace:

```go
func (b *AstBuilder) VisitTableName(ctx *generated.TableNameContext) interface{} {
	return &ast.Table{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Name: b.visit(ctx.QualifiedName()).(ast.QualifiedName)}
}
```

With:

```go
func (b *AstBuilder) VisitTableName(ctx *generated.TableNameContext) interface{} {
	var qn ast.QualifiedName
	if ctx.QualifiedName() != nil {
		qn = b.visit(ctx.QualifiedName()).(ast.QualifiedName)
	}
	return &ast.Table{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Name: qn}
}
```

### Step 3: Fix `VisitSubqueryRelation` (lines 388–390)

Replace:

```go
func (b *AstBuilder) VisitSubqueryRelation(ctx *generated.SubqueryRelationContext) interface{} {
	return &ast.TableSubquery{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Query: b.visitStatement(ctx.Query())}
}
```

With:

```go
func (b *AstBuilder) VisitSubqueryRelation(ctx *generated.SubqueryRelationContext) interface{} {
	var query ast.Statement
	if ctx.Query() != nil {
		query = b.visitStatement(ctx.Query())
	}
	return &ast.TableSubquery{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Query: query}
}
```

### Step 4: Fix `VisitParenthesizedRelation` (lines 400–402)

Replace:

```go
func (b *AstBuilder) VisitParenthesizedRelation(ctx *generated.ParenthesizedRelationContext) interface{} {
	return b.visitRelation(ctx.Relation())
}
```

With:

```go
func (b *AstBuilder) VisitParenthesizedRelation(ctx *generated.ParenthesizedRelationContext) interface{} {
	if ctx.Relation() == nil {
		return nil
	}
	return b.visitRelation(ctx.Relation())
}
```

### Step 5: Fix `VisitColumnReference` (lines 646–648)

Replace:

```go
func (b *AstBuilder) VisitColumnReference(ctx *generated.ColumnReferenceContext) interface{} {
	return b.visitIdentifier(ctx.Identifier())
}
```

With:

```go
func (b *AstBuilder) VisitColumnReference(ctx *generated.ColumnReferenceContext) interface{} {
	if ctx.Identifier() == nil {
		return nil
	}
	return b.visitIdentifier(ctx.Identifier())
}
```

### Step 6: Fix `VisitDereference` (lines 650–652)

Replace:

```go
func (b *AstBuilder) VisitDereference(ctx *generated.DereferenceContext) interface{} {
	return &ast.DereferenceExpression{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Base: b.visitExpression(ctx.GetBase()), Field: b.visitIdentifier(ctx.GetFieldName())}
}
```

With:

```go
func (b *AstBuilder) VisitDereference(ctx *generated.DereferenceContext) interface{} {
	var baseExpr ast.Expression
	if ctx.GetBase() != nil {
		baseExpr = b.visitExpression(ctx.GetBase())
	}
	var field *ast.Identifier
	if ctx.GetFieldName() != nil {
		field = b.visitIdentifier(ctx.GetFieldName())
	}
	return &ast.DereferenceExpression{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Base: baseExpr, Field: field}
}
```

### Step 7: Fix `VisitCast` (lines 681–683)

Replace:

```go
func (b *AstBuilder) VisitCast(ctx *generated.CastContext) interface{} {
	return &ast.Cast{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Expression: b.visitExpression(ctx.Expression()), Type: b.visitDataType(ctx.Type_()), Safe: ctx.TRY_CAST() != nil}
}
```

With:

```go
func (b *AstBuilder) VisitCast(ctx *generated.CastContext) interface{} {
	var expr ast.Expression
	if ctx.Expression() != nil {
		expr = b.visitExpression(ctx.Expression())
	}
	var dt ast.DataType
	if ctx.Type_() != nil {
		dt = b.visitDataType(ctx.Type_())
	}
	return &ast.Cast{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Expression: expr, Type: dt, Safe: ctx.TRY_CAST() != nil}
}
```

### Step 8: Fix `VisitExists` (lines 728–730)

Replace:

```go
func (b *AstBuilder) VisitExists(ctx *generated.ExistsContext) interface{} {
	return &ast.ExistsPredicate{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Subquery: b.visitStatement(ctx.Query())}
}
```

With:

```go
func (b *AstBuilder) VisitExists(ctx *generated.ExistsContext) interface{} {
	var query ast.Statement
	if ctx.Query() != nil {
		query = b.visitStatement(ctx.Query())
	}
	return &ast.ExistsPredicate{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Subquery: query}
}
```

### Step 9: Fix `VisitParenthesizedExpression` (lines 732–734)

Replace:

```go
func (b *AstBuilder) VisitParenthesizedExpression(ctx *generated.ParenthesizedExpressionContext) interface{} {
	return b.visitExpression(ctx.Expression())
}
```

With:

```go
func (b *AstBuilder) VisitParenthesizedExpression(ctx *generated.ParenthesizedExpressionContext) interface{} {
	if ctx.Expression() == nil {
		return nil
	}
	return b.visitExpression(ctx.Expression())
}
```

### Step 10: Fix `VisitLateral` (lines 949–951)

Replace:

```go
func (b *AstBuilder) VisitLateral(ctx *generated.LateralContext) interface{} {
	return &ast.Lateral{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Query: b.visitStatement(ctx.Query())}
}
```

With:

```go
func (b *AstBuilder) VisitLateral(ctx *generated.LateralContext) interface{} {
	var query ast.Statement
	if ctx.Query() != nil {
		query = b.visitStatement(ctx.Query())
	}
	return &ast.Lateral{BaseNode: ast.BaseNode{Location: locOf(ctx)}, Query: query}
}
```

### Step 11: Fix `VisitPatternRecognition` (lines 945–947)

Replace:

```go
func (b *AstBuilder) VisitPatternRecognition(ctx *generated.PatternRecognitionContext) interface{} {
	return b.visit(ctx.AliasedRelation())
}
```

With:

```go
func (b *AstBuilder) VisitPatternRecognition(ctx *generated.PatternRecognitionContext) interface{} {
	if ctx.AliasedRelation() == nil {
		return nil
	}
	return b.visit(ctx.AliasedRelation())
}
```

### Step 12: Fix `VisitFunctionRelation` (lines 953–964)

Replace:

```go
func (b *AstBuilder) VisitFunctionRelation(ctx *generated.FunctionRelationContext) interface{} {
	fe := ctx.FunctionExpression()
	name := b.visit(fe.QualifiedName()).(ast.QualifiedName)
	var args []ast.Expression
	for _, expr := range fe.AllExpression() {
		args = append(args, b.visitExpression(expr))
	}
	return &ast.FunctionRelation{BaseNode: ast.BaseNode{Location: locOf(ctx)},
		Name:      name,
		Arguments: args,
	}
}
```

With:

```go
func (b *AstBuilder) VisitFunctionRelation(ctx *generated.FunctionRelationContext) interface{} {
	fe := ctx.FunctionExpression()
	if fe == nil {
		return nil
	}
	var name ast.QualifiedName
	if fe.QualifiedName() != nil {
		name = b.visit(fe.QualifiedName()).(ast.QualifiedName)
	}
	var args []ast.Expression
	for _, expr := range fe.AllExpression() {
		if expr != nil {
			args = append(args, b.visitExpression(expr))
		}
	}
	return &ast.FunctionRelation{BaseNode: ast.BaseNode{Location: locOf(ctx)},
		Name:      name,
		Arguments: args,
	}
}
```

### Step 13: Add nil-context regression test

Append to `internal/parser/ast_builder_test.go`:

```go
func TestAstBuilder_NilContextDoesNotPanic(t *testing.T) {
	b := &AstBuilder{}
	// Each of these should return nil (or a zero-value node) instead of panicking.
	tests := []struct {
		name string
		fn   func()
	}{
		{"VisitAliasedRelation nil", func() { b.VisitAliasedRelation(nil) }},
		{"VisitTableName nil", func() { b.VisitTableName(nil) }},
		{"VisitSubqueryRelation nil", func() { b.VisitSubqueryRelation(nil) }},
		{"VisitParenthesizedRelation nil", func() { b.VisitParenthesizedRelation(nil) }},
		{"VisitColumnReference nil", func() { b.VisitColumnReference(nil) }},
		{"VisitDereference nil", func() { b.VisitDereference(nil) }},
		{"VisitCast nil", func() { b.VisitCast(nil) }},
		{"VisitExists nil", func() { b.VisitExists(nil) }},
		{"VisitParenthesizedExpression nil", func() { b.VisitParenthesizedExpression(nil) }},
		{"VisitLateral nil", func() { b.VisitLateral(nil) }},
		{"VisitPatternRecognition nil", func() { b.VisitPatternRecognition(nil) }},
		{"VisitFunctionRelation nil", func() { b.VisitFunctionRelation(nil) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic: %v", r)
				}
			}()
			 tc.fn()
		})
	}
}
```

### Step 14: Build and run parser tests

```bash
go test ./internal/parser/... -run TestAstBuilder_NilContextDoesNotPanic -v
go test ./internal/parser/... -count=1
```

Expected: all parser tests pass, nil-context test passes with 12 sub-tests.

### Step 15: Commit

```bash
git add internal/parser/ast_builder.go internal/parser/ast_builder_test.go
git commit -m "fix(p4/rc-b): defensive nil-checks in ast_builder visitor methods"
```

---

## Task 3: Probe Tool — Verify RC-A is Eliminated

**Files:**
- Create: `tools/probe-rewrite-rules.go`

### Step 1: Write probe script

```go
//go:build ignore

// Command probe-rewrite-rules runs each rewrite rule step-by-step for a given
// case and prints the formatted SQL after each rule. Used to verify RC-A
// (jinja / unparsed expressions leaking into generated SQL) is fixed.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/difftest"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"
	"github.com/wren-engine/wren/internal/rewrite"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("usage: go run tools/probe-rewrite-rules.go <case-id>\n  e.g. go run tools/probe-rewrite-rules.go tpch/m_orders")
	}
	caseID := os.Args[1]

	cases, err := difftest.LoadCorpus("testdata/difftest/cases")
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	var c difftest.Case
	for _, candidate := range cases {
		if candidate.ID() == caseID {
			c = candidate
			break
		}
	}
	if c.ID() == "" {
		log.Fatalf("case %q not found", caseID)
	}

	var manifest dto.Manifest
	if err := json.Unmarshal(c.ManifestJSON, &manifest); err != nil {
		log.Fatalf("unmarshal manifest: %v", err)
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)

	fmt.Printf("=== Case: %s ===\nSQL: %s\n\n", c.ID(), c.SQL)

	statement, err := parser.ParseSQL(c.SQL)
	if err != nil {
		log.Fatalf("initial parse: %v", err)
	}

	for _, rule := range rewrite.AllRules {
		formatted := formatter.FormatSQL(statement)
		fmt.Printf("--- BEFORE %T ---\n%s\n", rule, formatted)

		if strings.Contains(formatted, "{") || strings.Contains(formatted, "}") {
			fmt.Println(">>> WARNING: contains '{' or '}' <<<")
		}

		reparsed, err := parser.ParseSQL(formatted)
		if err != nil {
			fmt.Printf(">>> RE-PARSE ERROR: %v <<<\n", err)
			break
		}

		statement, err = rule.Apply(reparsed, ctx, analyzed)
		if err != nil {
			fmt.Printf(">>> APPLY ERROR: %v <<<\n", err)
			break
		}
		fmt.Printf("--- AFTER %T ---\n%s\n\n", rule, formatter.FormatSQL(statement))
	}

	final := formatter.FormatSQL(statement)
	fmt.Printf("=== FINAL ===\n%s\n", final)
	if strings.Contains(final, "{") || strings.Contains(final, "}") {
		fmt.Println(">>> WARNING: final output contains '{' or '}' <<<")
		os.Exit(1)
	}
	fmt.Println("OK: no '{' or '}' in output")
}
```

### Step 2: Run probe against m_orders

```bash
go run tools/probe-rewrite-rules.go tpch/m_orders
```

Expected:
- No `>>> WARNING: contains '{' or '}' <<<`
- No `>>> RE-PARSE ERROR <<<`
- Final output ends with `OK: no '{' or '}' in output`

If warnings appear, the RC-A fix in Task 1 is incomplete — investigate which rule emits `{`.

### Step 3: Run probe against all 10 cases

```bash
for case in tpch/m_orders tpch/met_customer_revenue tpch/met_daily tpch/met_revenue tpch/met_rollup tpch/met_weekly tpch/v_use_metric tpch/v_use_model tpch/v_use_nested tpch/v_use_rollup; do
  echo "=== $case ==="
  go run tools/probe-rewrite-rules.go "$case" 2>&1 | tail -3
done
```

Expected: all 10 cases print `OK: no '{' or '}' in output`.

### Step 4: Commit probe tool

```bash
git add tools/probe-rewrite-rules.go
git commit -m "chore(p4): add probe-rewrite-rules debug script"
```

---

## Task 4: Focused Tests — Cluster 1 (Calc Field / Relationship)

**Files:**
- Create: `internal/rewrite/sql_rewrite_test.go`

### Step 1: Create test file with tpch manifest helper

```go
package rewrite

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func loadTpchManifest(t *testing.T) *dto.Manifest {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/difftest/cases/tpch/mdl.json")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest dto.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return &manifest
}

func mustRewrite(t *testing.T, sql string, manifest *dto.Manifest) string {
	t.Helper()
	wrenMDL := mdl.WrenMDLFromManifest(manifest)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	got, err := Rewrite(sql, ctx, analyzed)
	if err != nil {
		t.Fatalf("Rewrite(%q): %v", sql, err)
	}
	// Defensive: output must be re-parsable as valid SQL.
	if _, err := parser.ParseSQL(got); err != nil {
		t.Fatalf("re-parse failed: %v\noutput:\n%s", err, got)
	}
	return got
}

// TestRewrite_m_orders covers the simplest model case (Orders with calc field).
func TestRewrite_m_orders(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select orderkey, totalprice from Orders", manifest)
	t.Logf("output:\n%s", got)
	// After fix, this should produce valid SQL containing CTEs for Orders.
	// Exact golden comparison is deferred to difftest baseline rotation.
}

// TestRewrite_met_customer_revenue covers Metric → Model with calc field.
func TestRewrite_met_customer_revenue(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select custkey, totalprice from CustomerRevenue", manifest)
	t.Logf("output:\n%s", got)
}

// TestRewrite_met_revenue covers basic Metric expansion.
func TestRewrite_met_revenue(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select customer, totalprice from Revenue", manifest)
	t.Logf("output:\n%s", got)
}
```

### Step 2: Run cluster 1 tests

```bash
go test ./internal/rewrite/... -run 'TestRewrite_m_orders|TestRewrite_met_customer_revenue|TestRewrite_met_revenue' -v
```

Expected: 3 tests pass, no panic, output is re-parsable SQL.

### Step 3: Run full difftest regression

```bash
go test ./internal/difftest/... -run TestDifferential -count=1 2>&1 | tail -20
```

Expected:
- 10 go-error cases now show `fail` (not `go-error`) — RC-B prevents panic
- 35 original pass cases still `pass` — 0 regression

### Step 4: Commit

```bash
git add internal/rewrite/sql_rewrite_test.go
git commit -m "test(p4/cluster-1): focused tests for calc-field/model/metric cases"
```

---

## Task 5: Focused Tests — Cluster 2 (Metric)

**Files:**
- Modify: `internal/rewrite/sql_rewrite_test.go`

### Step 1: Append metric-focused tests

```go
// TestRewrite_met_daily covers CumulativeMetric with date_spine.
func TestRewrite_met_daily(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select customer, date, totalprice from CustomerDailyRevenue", manifest)
	t.Logf("output:\n%s", got)
}

// TestRewrite_met_weekly covers CumulativeMetric with week grouping.
func TestRewrite_met_weekly(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select orderdate, totalprice from WeeklyRevenue", manifest)
	t.Logf("output:\n%s", got)
}

// TestRewrite_met_rollup covers roll_up function replacement.
func TestRewrite_met_rollup(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select * from roll_up(Revenue, orderdate, YEAR)", manifest)
	t.Logf("output:\n%s", got)
}
```

### Step 2: Run cluster 2 tests

```bash
go test ./internal/rewrite/... -run 'TestRewrite_met_daily|TestRewrite_met_weekly|TestRewrite_met_rollup' -v
```

Expected: 3 tests pass. `met_rollup` may fail with error (not panic) if `MetricRollupRewrite` has its own bug — this is acceptable for Phase 4 as long as it's not `go-error`.

### Step 3: Run full difftest regression

```bash
go test ./internal/difftest/... -run TestDifferential -count=1 2>&1 | tail -20
```

Expected: 0 regression among original 35 pass cases.

### Step 4: Commit

```bash
git add internal/rewrite/sql_rewrite_test.go
git commit -m "test(p4/cluster-2): focused tests for cumulative-metric and rollup cases"
```

---

## Task 6: Focused Tests — Cluster 3 (View)

**Files:**
- Modify: `internal/rewrite/sql_rewrite_test.go`

### Step 1: Append view-focused tests

```go
// TestRewrite_v_use_model covers View → Model.
func TestRewrite_v_use_model(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select * from useModel", manifest)
	t.Logf("output:\n%s", got)
}

// TestRewrite_v_use_metric covers View → Metric.
func TestRewrite_v_use_metric(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select * from useMetric", manifest)
	t.Logf("output:\n%s", got)
}

// TestRewrite_v_use_nested covers View → View → Metric nesting.
func TestRewrite_v_use_nested(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select * from useUseMetric", manifest)
	t.Logf("output:\n%s", got)
}

// TestRewrite_v_use_rollup covers View → CumulativeMetric with roll_up.
func TestRewrite_v_use_rollup(t *testing.T) {
	manifest := loadTpchManifest(t)
	got := mustRewrite(t, "select * from useMetricRollUp", manifest)
	t.Logf("output:\n%s", got)
}
```

### Step 2: Run cluster 3 tests

```bash
go test ./internal/rewrite/... -run 'TestRewrite_v_use_' -v
```

Expected: 4 tests pass (or fail with error, not panic).

### Step 3: Run full difftest regression

```bash
go test ./internal/difftest/... -run TestDifferential -count=1 2>&1 | tail -20
```

Expected: 0 regression.

### Step 4: Commit

```bash
git add internal/rewrite/sql_rewrite_test.go
git commit -m "test(p4/cluster-3): focused tests for view-rewrite cases"
```

---

## Task 7: Full Regression + Race Check

**Files:** (no file changes)

### Step 1: Run all rewrite tests

```bash
go test ./internal/rewrite/... -race -count=1
```

Expected: pass, no race warnings.

### Step 2: Run all parser tests

```bash
go test ./internal/parser/... -race -count=1
```

Expected: pass, no race warnings.

### Step 3: Run full difftest suite

```bash
go test ./internal/difftest/... -count=1 2>&1 | tail -30
```

Expected:
- 0 `go-error` cases in rewrite baseline
- 35 original pass cases still pass
- Any remaining non-pass cases show `fail` (with token-diff detail), not `go-error`

### Step 4: Build and vet

```bash
go build ./...
go vet ./...
```

Expected: both exit 0 with no warnings.

### Step 5: Commit (if any fixes needed during regression)

If regression check revealed any new issues, fix them and commit. Otherwise no commit needed.

---

## Task 8: Baseline Rotation

**Files:**
- Rotate: `testdata/difftest/baseline.json`

### Step 1: Backup and rotate baseline

```bash
cp testdata/difftest/baseline.json testdata/difftest/baseline.json.bak.phase4
go test ./internal/difftest/... -run TestDifferential -difftest.accept -count=1
```

### Step 2: Verify rotation result

```bash
diff -u testdata/difftest/baseline.json.bak.phase4 testdata/difftest/baseline.json | head -60
```

Expected diff:
- 10 lines changed: `"tpch/m_orders": "go-error"` → `"tpch/m_orders": "pass"` (or `"fail"`)
- No original pass case changed to `go-error` or `fail`
- `tpch/1` and `tpch/4` still `oracle-error-permanent`

### Step 3: Commit

```bash
git add testdata/difftest/baseline.json
git commit -m "chore(p4): rotate baseline — rewrite layer 0 go-error"
```

---

## Task 9: Update Scoreboard

**Files:**
- Modify: `docs/phase3-scoreboard.md`

### Step 1: Update rewrite section

Replace the Rewrite Baseline table with updated numbers from the rotated baseline. Run:

```bash
go test ./internal/difftest/... -run TestDifferential -count=1 2>&1 | grep -E "差分计分板|pass|fail|go-error|oracle-error"
```

Update the markdown table:

```markdown
## Rewrite Baseline (`modelingOnly=group-config`)

| Status | Count | Notes |
|--------|-------|-------|
| pass | N | |
| fail | M | Token-level diffs against Java golden |
| go-error | 0 | Phase 4 complete — no panics |
| oracle-error-permanent | 2 | tpch/1, tpch/4 |
| **Drop-in pass rate** | **N/(N+M) = X%** | |
```

Remove the go-error Inventory section (or mark as resolved).

### Step 2: Commit

```bash
git add docs/phase3-scoreboard.md
git commit -m "docs(p4): update scoreboard — rewrite layer 0 go-error"
```

### Step 3: Delete probe tool

```bash
rm tools/probe-rewrite-rules.go
git rm tools/probe-rewrite-rules.go
git commit -m "chore(p4): remove temporary probe-rewrite-rules script"
```

---

## Self-Review Checklist

### 1. Spec Coverage

| Design Requirement | Task |
|---|---|
| RC-A fix — jinja expressions no longer leak `{` | Task 1 |
| RC-B fix — visitor nil-checks | Task 2 |
| Probe tool to verify RC-A | Task 3 |
| Focused unit tests for 10 cases | Tasks 4–6 |
| Full regression check | Task 7 |
| Baseline rotation | Task 8 |
| Scoreboard update | Task 9 |

**Gap:** None. All design requirements map to a task.

### 2. Placeholder Scan

- No "TBD" in code blocks.
- No "implement later" or "fill in details".
- All function signatures, file paths, and commands are exact.

### 3. Type Consistency

- `visit` helper returns `interface{}`; nil-check pattern is `if ctx.XXX() != nil { ... }` consistently across all 12 methods.
- `RenderJinja` signature matches: `func RenderJinja(manifest *dto.Manifest) *dto.Manifest`.
- `mustRewrite` helper uses same pattern as `goRewrite` in difftest.

---

## Execution Handoff

**Plan complete and saved to `.gpowers/plans/2026-05-21-phase4-rewrite-go-error-cleanup.md`.**

Two execution options:

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using `gpowers:executing-plans`, batch execution with checkpoints.

**Which approach?**
