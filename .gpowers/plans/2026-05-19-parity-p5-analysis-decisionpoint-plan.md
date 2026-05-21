# P5 Analysis DecisionPoint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use gpowers:subagent-driven-development (recommended) or gpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Port Java `decisionpoint` analyzer (10 files, 1454 lines) to Go, producing structurally-equivalent JSON analysis output via `/v1/analysis/sql`, `/v2/analysis/sql`, `/v2/analysis/sqls` endpoints.

**Architecture:** Mechanical port of Java `sqlrewrite.analyzer.decisionpoint` package. Reuses P3a `Analysis` / `StatementAnalyzer` / `Scope` / `Field`. Adds missing P3a accessors. Golden differential testing against Java `wren-engine:0.9.3` analysis endpoints.

**Tech Stack:** Go 1.26, existing AST (P2), existing analyzer (P3a), Chi HTTP router, JSON structured comparison.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/rewrite/analyzer/analysis_getscope.go` | Add `Analysis.GetScope` (panic on missing) |
| `internal/rewrite/analyzer/field_accessors.go` | Add `Field.SourceDatasetName`, `Field.RelationAlias` |
| `internal/rewrite/analyzer/relation_type_resolve.go` | Add `RelationType.ResolveFields` |
| `internal/dto/analysis_response.go` | All JSON DTOs for analysis endpoint responses |
| `internal/analyzer/decisionpoint/query_analysis.go` | `QueryAnalysis`, `ColumnAnalysis`, `SortItemAnalysis`, `GroupByKey`, `Builder` |
| `internal/analyzer/decisionpoint/relation_analysis.go` | `RelationAnalysis` hierarchy |
| `internal/analyzer/decisionpoint/filter_analysis.go` | `FilterAnalysis` hierarchy |
| `internal/analyzer/decisionpoint/expr_source.go` | `ExprSource` |
| `internal/analyzer/decisionpoint/context.go` | `DecisionPointContext` |
| `internal/analyzer/decisionpoint/decision_expression_analyzer.go` | `DecisionExpressionAnalyzer` |
| `internal/analyzer/decisionpoint/expression_location_analyzer.go` | `ExpressionLocationAnalyzer` |
| `internal/analyzer/decisionpoint/relation_analyzer.go` | `RelationAnalyzer` + `ExpressionSourceAnalyzer` |
| `internal/analyzer/decisionpoint/filter_analyzer.go` | `FilterAnalyzer` |
| `internal/analyzer/decisionpoint/decision_point_analyzer.go` | `DecisionPointAnalyzer` (top-level) |
| `internal/server/analysis_handler.go` | HTTP endpoints |
| `internal/server/analysis_handler_test.go` | Endpoint tests |
| `internal/difftest/json_compare.go` | JSON structured-equivalence comparator |
| `internal/difftest/analysis_diff_test.go` | Golden diff test |
| `cmd/capture-analysis-golden/main.go` | Captures Java analysis JSON |
| `testdata/difftest/baseline-analysis.json` | Baseline scoring board |
| `internal/parser/ast_builder.go` | **(modify)** backfill `BaseNode.Location` from ANTLR token positions on every node — P5 prerequisite, NodeLocation is unpopulated in P2 |

---

## 前置条件（必读）

1. **P0–P3a 必须已合并**。本计划凡引用 `rewrite/analyzer.Analysis` / `StatementAnalyzer` / `Scope` / `Field` / `RelationType` 均按 P3a 收官形态使用。P3b/P3c/P4 与本计划解耦（spec §9），无依赖。

2. **P2 AstBuilder 当前不写入 `BaseNode.Location`**（grep `internal/parser/ast_builder.go` 无任何 `Location:` 赋值；`BaseNode.GetLocation()` 永远返回 `nil`）。Java oracle 端 `NodeLocation` 是 1-based `(line, column)`，golden JSON 里每个节点都带它。**没有这个数据，端到端差分将系统性全 fail。** 本计划切片 0 任务 1 是 P2 backfill：在每个 `Visit*` 方法构造 AST 节点时，从 `ctx.GetStart()` 读 ANTLR token 的 line（1-based）与 `charPositionInLine + 1` 写入 `BaseNode.Location`。

3. **本计划修改既有桩文件**：
   - `internal/server/analysis_handler.go` —— 80 行桩（POST/GET 路由 + 回显 SQL），切片 5 全量替换。**保持现有 `r.Get(...)` 路由**（Java JAX-RS 用 `@GET` + body，现有 `cmd/capture-golden/main.go` 也是 `http.MethodGet`）。
   - `internal/parser/ast_builder.go` —— 切片 0 任务 1 在每个 visit 方法增 1 行 `BaseNode: ast.BaseNode{Location: locOf(ctx)}`，**不动逻辑**。
   - `internal/rewrite/analyzer/` —— 切片 0 增 3 个 accessor 文件，不动既有文件。

4. **既有 `internal/analyzer`（提供 `SessionContext`）一律以原名 `analyzer` 引入**（与 P3a 约定不同 —— 因为 `internal/analyzer/decisionpoint` 是 P3a 之外的新子包，没有名字冲突）。`internal/rewrite/analyzer` 一律以别名 `rewriteAnalyzer` 引入。

5. **Java 参照源**位于 `../wren-engine-0.9.3/wren-base/src/main/java/io/wren/base/sqlrewrite/analyzer/decisionpoint/`（10 文件 1454 行）与 `../wren-engine-0.9.3/wren-main/src/main/java/io/wren/main/web/`（`AnalysisResource` / `AnalysisResourceV2`）+ `web/dto/QueryAnalysisDto.java`、`web/dto/NodeLocationDto.java`、`web/dto/SqlAnalysisInputDto*.java`。

---

## 验收性质说明（重要 —— 影响切片 0/6 验证方式与 P5 收官口径）

P5 的字节关键层是「Java analysis 端点 JSON ↔ Go analysis 端点 JSON 结构化等价」。三件事须诚实预先声明：

### 1. NodeLocation 一致性依赖于 P2 backfill 任务的完成度

切片 0 任务 1 给 ANTLR 各个生成的 `*Context` 类型的 `GetStart()` 抽取 `(line, charPositionInLine+1)`。**有些子节点（如 `QualifiedName.OriginalParts[i]`、`SortItem.NullOrdering` 派生位置）在 Trino Java 那边由 `NodeLocation.fromTokenWithOffset` 推导，Go 端不一定能在每个位置都精确对齐。** 验收口径降为「**结构化 + 关键节点位置等价**」：Java 与 Go 必须在 `QuerySpecification` / `Table` / `SingleColumn` / `AllColumns` / `Join` / `JoinOn` 上的 NodeLocation 一致；嵌套深处的 `Identifier`（如 `ExprSource.nodeLocation`）允许 ±1 列偏差，由切片 0 任务 5 的 `JSONEqual` 比对器在 `nodeLocation` 子树启用宽松对比模式。

### 2. `ExprSource` 列表顺序不稳定

Java `ExpressionSourceAnalyzer` 用 `HashSet<ExprSource>` 收集再 `ImmutableSet.copyOf` → `ImmutableList.copyOf`；JVM HashSet 迭代顺序不是排序，是 hash 顺序，不能与 Go 的 map/slice 顺序一致。**收官形态须排序后输出**：Go 实现在所有 `ExprSource` 列表出口处按 `(nodeLocation.line, nodeLocation.column, expression)` 字典序排序；同时 `cmd/capture-analysis-golden/main.go` 在写 golden 时做相同排序后再格式化。**这是 P5 写 golden 的一次性归一化**，不破坏 Java 行为奇偶（Java 输出排序后等价于乱序）。详见风险 #2。

### 3. 信封 JSON 是结构化等价、不是字节等价

`JSONEqual`（切片 0 任务 5）做的是 parse→tree→deep-equal：
- 容忍对象键序差异（Java Jackson 字母序，Go `encoding/json` 字段定义序）；
- 容忍空白差异；
- **不容忍**字段名差异（包括 `isSubqueryOrCte` 这种 boolean getter 衍生名 —— Java `@JsonProperty("isSubqueryOrCte")` 显式声明，Go DTO 用 tag `json:"isSubqueryOrCte"` 一致）；
- **不容忍**数值类型差异（JSON 数字 Go 全 `float64` 在比对器里，Java `int`/`long`/`Integer` 经 Jackson 也成 JSON Number，go round-trip 后均 `float64` —— 等价）。

**P5 收官真实达成标准**：

| 类别 | 期望 |
|---|---|
| 语料 `analysis/` 合成查询（任务 2 新建，6–8 条覆盖 select / where / group by / order by / join / cte / subquery） | 全 `pass` |
| TPC-H 22 条作分析（不重写、不执行 —— 纯 parse + analyze） | ≥18 `pass`，2–4 条 `fail` 允许（NodeLocation 嵌套深处偏移、ExprSource 顺序边界） |
| 既有 P0–P4 baseline | 不变（P5 不动 dry-plan / dry-run / preview） |
| 构建 / 静态 | `go build ./...` 通过；`go vet ./...` 干净；`gofmt -l` 无输出 |

---

## 字节分歧风险登记表（贯穿全程）

继承 P0/P1/P2/P3a 风险。P5 新增：

| # | 风险 | 缓解（落在哪个任务） |
|---|---|---|
| 1 | **NodeLocation 0/1-based 不一致** —— ANTLR `Token.GetColumn()` 是 0-based；Java `NodeLocation.column` 是 1-based（构造 `new NodeLocation(line, charPositionInLine + 1)`）。直接用 ANTLR 列号会让所有 column 差 1。 | 任务 1：`locOf(ctx)` 显式 `Column: token.GetColumn() + 1`；DTO `NodeLocationDto.Column` 取 `loc.CharPosition`（已是 1-based）。 |
| 2 | **`ExprSource` 顺序不稳定** —— Java HashSet 顺序、Go map 顺序均不可控且互不相等。`reflect.DeepEqual` 会判定列表不等。 | 切片 2/4：所有 `ExprSource` 列表在写入 `ColumnAnalysis` / `JoinRelation.exprSources` / `GroupByKey` / `SortItemAnalysis` 前 `sort.Slice` 按 `(nodeLocation.Line, nodeLocation.Column, expression)`；capture-analysis-golden 写 golden 前对每个 `exprSources` 数组做相同排序。 |
| 3 | **`SortItem.Ordering` JSON 形态** —— Java `ASCENDING`/`DESCENDING`（`enum.name()`）；Go `ast.OrderingAsc = "ASC"`。直接 `string(ord)` 输出 `ASC`，与 Java 不等。 | 任务 5（DTO）/ 任务 16（mapper）：mapper 显式 `case ast.OrderingAsc: "ASCENDING"; case ast.OrderingDesc: "DESCENDING"`。 |
| 4 | **`AllColumns` 带 target（`t.*`）的字段过滤** —— Java 仅输出 `relationAlias == target` OR `tableName == catalogSchemaTableName` 的字段；plan 旧版本输出全部字段。 | 任务 13：`processAllColumns` 中带 target 时严格按 alias/CSN 过滤。 |
| 5 | **`LogicalExpression` vs `LogicalBinaryExpression`** —— P2 parser 产出 N 元 `*ast.LogicalExpression`（`Terms []Expression`），不产出 `LogicalBinaryExpression`。`Operator` 字段是 `ast.LogicalOperator` 取值 `LogicalAnd`/`LogicalOr`。Java FilterAnalyzer 把每个 LogicalExpression 视为二元（左 = `Terms[0]`，右 = `Terms[1..]` 折叠或单项；P5 第一版按「精确二元，Terms 长度必须为 2，否则当 leaf」处理 —— Trino N 元 flatten 仅在 `AND a AND b AND c` 才出现，分析端点对此降级为 leaf 与 Java 行为等价）。 | 任务 11：`analyzeFilterNode` 只 switch `*ast.LogicalExpression`，`Terms != 2` 走 leaf；用 `n.Operator == ast.LogicalAnd` 判分支。 |
| 6 | **`mdl.NewWrenMDL` 不存在** —— 实际构造函数是 `mdl.WrenMDLFromManifest(*dto.Manifest)` 与 `mdl.WrenMDLFromJSON(string)`。 | 任务 12 / 任务 15 / 任务 17：所有测试与端点统一用 `WrenMDLFromManifest(&dto.Manifest{Catalog:..., Schema:..., Models:[]dto.Model{}})` 或 `WrenMDLFromJSON(...)`。 |
| 7 | **`dto.ManifestFromJSON` 不存在** —— `internal/dto` 里 `Manifest` 是裸 struct，反序列化用 `json.Unmarshal`。统一通过 `mdl.WrenMDLFromJSON(string(rawJSON))` 一步到位，避免在 handler 里手写 Unmarshal。 | 任务 15：handler 直接 `mdl.WrenMDLFromJSON(string(*req.Manifest))`。 |
| 8 | **HTTP 方法** —— Java JAX-RS 用 `@GET` + body；现有 `internal/server/analysis_handler.go` 桩用 `r.Get(...)`；`cmd/capture-golden/main.go` 用 `http.MethodGet`。新 handler 必须**保持 GET**。 | 任务 15：`r.Get("/v1/analysis/sql", ...)` 等。 |
| 9 | **`Properties` map 在 JSON 中始终输出** —— Java `Map<String, String>` 即使为空也输出 `{}`（`@JsonInclude(NON_NULL)` 只 skip null，不 skip 空 map）。Go DTO 用 `Properties map[string]string \`json:"properties"\``（**无 `omitempty`**），nil map 会输出 `null` —— 与 Java 不等。 | 任务 5：DTO `Properties` 字段无 `omitempty`；任务 13 / 任务 16 mapper 在 nil 时初始化为 `map[string]string{}`。 |
| 10 | **`isSubqueryOrCte` JSON key** —— Java Jackson 由 `@JsonProperty("isSubqueryOrCte")` 显式声明（默认会脱去 `is` 前缀为 `subqueryOrCte`）。 | 任务 5：DTO `IsSubqueryOrCte bool \`json:"isSubqueryOrCte"\``（精确小写 i）。 |
| 11 | **`Optional<String> aliasName` JSON 形态** —— Java Jackson 默认把 `Optional.empty()` 序列化为 `null`，`Optional.of(s)` 为 `"s"`。Go DTO 用 `*string` + `omitempty:false` 时 nil 也输出 `null`。 | 任务 5：`Alias *string \`json:"alias"\``（无 `omitempty`，与 Java `@JsonInclude(NON_NULL)` 矛盾 —— Java 类级 NON_NULL 会 skip null `alias`）；** Java 实际行为是 skip**。Go 改用 `omitempty` + 指针 nil 即 skip，等价。 |
| 12 | **`formatCriteria(JoinOn)` 依赖 `Expression.toString()`** —— Java `builder.append(joinOn.getExpression())` 隐式调 `Expression.toString()`，Trino `Expression` override toString = `ExpressionFormatter.formatExpression(this, DEFAULT)`。Go AST `Expression` interface 无 `String()` 方法；必须显式 `formatter.FormatExpression(...)`。 | 任务 9：`formatJoinCriteria` 中 `ON ` 拼 `formatter.FormatExpression(c.Expression)`。 |
| 13 | **`JoinUsing` 列拼接分隔符** —— Java `Joiner.on(", ")`（带空格）。Go 用 `strings.Join(cols, ", ")`。 | 任务 9：保留 plan 既有写法即可。 |
| 14 | **`Join.JoinType` 字符串拼装** —— Java `format("%s_JOIN", node.getType())` 把 enum 名（`INNER`/`LEFT`/...）拼成 `INNER_JOIN`。Go `ast.JoinType` 值为 `"INNER"`/`"LEFT"`/`"FULL"`/`"CROSS"`/`"IMPLICIT"`；用 `fmt.Sprintf("%s_JOIN", n.JoinType)` 得同形态。 | 任务 9：保留 plan 既有 `fmt.Sprintf` 即可。 |
| 15 | **NodeLocation 嵌套深处对齐性弱** —— `Identifier` token 位置随 parser 实现细节漂移；切片 0 任务 5 `JSONEqual` 比对器对 `nodeLocation` 子树启用**宽容模式**（±1 列允许），由 baseline 接受边界 fail。 | 任务 5：`JSONEqual` 暴露选项 `LooseNodeLocation`，仅在 `analysis_diff_test.go` 启用。 |
| 16 | **不在范围的 Relation 类型 panic 而非静默** —— Java `RelationAnalyzer` 对 `SetOperation`/`Values`/`FunctionRelation`/`SampledRelation`/`PatternRecognitionRelation`/`Unnest`/`Lateral` `throw UnsupportedOperationException`。Go 同形：`return &RelationAnalysis{}` 是错的，应 `panic(fmt.Sprintf("Analyze %T is not supported yet", n))`。 | 任务 9：`default` 分支 panic 而非返 nil；语料任务 2 不引入这些节点。 |

---

## Slice 0: P3a Infrastructure Gaps + Golden Capture Framework

**Files:**
- Modify: `internal/parser/ast_builder.go` (**P5 prereq: NodeLocation backfill**)
- Create: `internal/parser/ast_builder_location.go` (helper)
- Create: `internal/parser/ast_builder_location_test.go`
- Create: `internal/rewrite/analyzer/analysis_getscope.go`
- Create: `internal/rewrite/analyzer/field_accessors.go`
- Create: `internal/rewrite/analyzer/relation_type_resolve.go`
- Create: `internal/rewrite/analyzer/relation_type_resolve_test.go`
- Create: `internal/difftest/json_compare.go`
- Create: `internal/difftest/json_compare_test.go`
- Create: `cmd/capture-analysis-golden/main.go`
- Create: `internal/difftest/analysis_diff_test.go`
- Create: `testdata/difftest/baseline-analysis.json`
- Create: `testdata/difftest/cases/analysis/group.json`
- Create: `testdata/difftest/cases/analysis/mdl.json`
- Create: `testdata/difftest/cases/analysis/queries/*.sql` (6–8 queries)
- Modify: `Makefile`

- [ ] **Step 0a: P2 backfill — populate `BaseNode.Location` in AstBuilder**

**Why this is blocking:** A grep of `internal/parser/ast_builder.go` shows zero `Location:` assignments — every AST node returns `nil` from `GetLocation()`. Java oracle's analysis JSON has real `(line, column)` on every node; without backfill, the entire P5 golden diff fails systematically on Step 1 of Slice 6.

Create `internal/parser/ast_builder_location.go`:

```go
package parser

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// locOf extracts a 1-based (line, column) NodeLocation from any ANTLR
// ParserRuleContext. Mirrors Java parsing's `new NodeLocation(token.getLine(),
// token.getCharPositionInLine() + 1)`. ANTLR's `GetColumn()` is 0-based; we
// add 1 to align with Java's 1-based column.
func locOf(ctx antlr.ParserRuleContext) *ast.NodeLocation {
	if ctx == nil {
		return nil
	}
	tok := ctx.GetStart()
	if tok == nil {
		return nil
	}
	return &ast.NodeLocation{Line: tok.GetLine(), CharPosition: tok.GetColumn() + 1}
}

// locOfTerminal extracts (line, column) from an ANTLR TerminalNode (for token
// references, e.g. Identifier built directly from an ID token without a wrapping
// rule context).
func locOfTerminal(node antlr.TerminalNode) *ast.NodeLocation {
	if node == nil {
		return nil
	}
	tok := node.GetSymbol()
	if tok == nil {
		return nil
	}
	return &ast.NodeLocation{Line: tok.GetLine(), CharPosition: tok.GetColumn() + 1}
}
```

Then edit `internal/parser/ast_builder.go`: in **every** Visit method that returns a struct embedding `ast.BaseNode`, set the `Location` field:

```go
func (a *AstBuilder) VisitQuerySpecification(ctx *generated.QuerySpecificationContext) any {
	// ... existing body ...
	return &ast.QuerySpecification{
		BaseNode: ast.BaseNode{Location: locOf(ctx)},
		// ... existing fields ...
	}
}
```

The edit pattern is mechanical: for each `return &ast.X{...}` where `X` embeds `BaseNode`, prepend `BaseNode: ast.BaseNode{Location: locOf(ctx)},` as the first field. **Affected node types** (one-shot grep `BaseNode$` in `internal/parser/ast/*.go`): `Query`, `QuerySpecification`, `Select`, `SingleColumn`, `AllColumns`, `With`, `WithQuery`, `SortItem`, `Window`, `WindowFrame`, `Table`, `AliasedRelation`, `JoinOn`, `JoinUsing`, `NaturalJoin`, `Join`, `TableSubquery`, `Unnest`, `Values`, `Lateral`, `SampledRelation`, `FunctionRelation`, `Identifier`, `DereferenceExpression`, `ComparisonExpression`, `ArithmeticBinaryExpression`, `ArithmeticUnaryExpression`, `LogicalBinaryExpression`, `NotExpression`, `FunctionCall`, `SubscriptExpression`, `IntervalLiteral`, `LongLiteral`, `DoubleLiteral`, `StringLiteral`, `BooleanLiteral`, `NullLiteral`, `LogicalExpression`, `GroupBy`, `Cast`, `IsNullPredicate`, `IsNotNullPredicate`, `InPredicate`, `BetweenPredicate`, `LikePredicate`. (List driven from grep — not all may need it for P5 but completeness gives parity for any future analyzer expansion.)

Create `internal/parser/ast_builder_location_test.go`:

```go
package parser

import (
	"testing"
)

func TestLocationOnBasicNodes(t *testing.T) {
	stmt, err := ParseSQL("SELECT a FROM t WHERE a > 1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if loc := stmt.GetLocation(); loc == nil {
		t.Fatal("Query.Location nil; expected (1, 1)")
	} else if loc.Line != 1 || loc.CharPosition != 1 {
		t.Errorf("Query loc = (%d,%d); want (1,1)", loc.Line, loc.CharPosition)
	}
}
```

Run: `go test ./internal/parser/ -run TestLocationOnBasicNodes -v`
Expected: PASS.

Run: `go test ./...`
Expected: still passes — existing tests don't assert `Location == nil` so adding values is non-breaking. Any P3a/P3b/P3c snapshot test that compares AST identity by pointer ignores `BaseNode` content, so no regression.

- [ ] **Step 1: Add `Analysis.GetScope` (panic on missing)**

Create `internal/rewrite/analyzer/analysis_getscope.go`:

```go
package analyzer

import (
	"fmt"
	"github.com/wren-engine/wren/internal/parser/ast"
)

func (a *Analysis) GetScope(n ast.Node) *Scope {
	s, ok := a.TryGetScope(n)
	if !ok {
		panic(fmt.Sprintf("Analysis does not contain information for node: %T", n))
	}
	return s
}
```

Run: `go build ./internal/rewrite/analyzer/`
Expected: PASS

- [ ] **Step 2: Add `Field` accessors**

Create `internal/rewrite/analyzer/field_accessors.go`:

```go
package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

func (f *Field) SourceDatasetName() *string {
	return f.sourceDatasetName
}

func (f *Field) RelationAlias() *ast.QualifiedName {
	return f.relationAlias
}
```

Run: `go build ./internal/rewrite/analyzer/`
Expected: PASS

- [ ] **Step 3: Add `RelationType.ResolveFields`**

Create `internal/rewrite/analyzer/relation_type_resolve.go`:

```go
package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

func (rt *RelationType) ResolveFields(name *ast.QualifiedName) []*Field {
	var out []*Field
	for _, f := range rt.fields {
		if f.sourceColumn != nil && f.sourceColumn.Relationship != "" {
			continue
		}
		if f.CanResolve(name) {
			out = append(out, f)
		}
	}
	return out
}
```

Run: `go build ./internal/rewrite/analyzer/`
Expected: PASS

- [ ] **Step 4: Write unit test for `ResolveFields`**

Create `internal/rewrite/analyzer/relation_type_resolve_test.go`:

```go
package analyzer

import (
	"testing"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

func TestResolveFields(t *testing.T) {
	name1 := "id"
	name2 := "name"
	f1 := &Field{columnName: "id", name: &name1, sourceColumn: &dto.Column{Name: "id"}}
	f2 := &Field{columnName: "name", name: &name2, sourceColumn: &dto.Column{Name: "name", Relationship: "r1"}}
	f3 := &Field{columnName: "age", name: nil, sourceColumn: &dto.Column{Name: "age"}}
	rt := NewRelationType([]*Field{f1, f2, f3})

	got := rt.ResolveFields(&ast.QualifiedName{Parts: []string{"id"}})
	if len(got) != 1 || got[0] != f1 {
		t.Fatalf("expected [f1], got %v", got)
	}
	got2 := rt.ResolveFields(&ast.QualifiedName{Parts: []string{"name"}})
	if len(got2) != 0 {
		t.Fatalf("expected none for relationship field, got %v", got2)
	}
}
```

Run: `go test ./internal/rewrite/analyzer/ -run TestResolveFields -v`
Expected: PASS

- [ ] **Step 5: Create JSON structured-equivalence comparator**

Create `internal/difftest/json_compare.go`:

```go
package difftest

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
)

// CompareOptions tunes structural JSON equality. Defaults are strict.
type CompareOptions struct {
	// LooseNodeLocation: when comparing an object whose path ends in
	// "nodeLocation", allow (line, column) to differ by ±1 (P5 risk #15:
	// deep-identifier token positions drift between ANTLR Go and ANTLR Java).
	LooseNodeLocation bool
}

func JSONEqual(a, b []byte) error {
	return JSONEqualWithOptions(a, b, CompareOptions{})
}

func JSONEqualWithOptions(a, b []byte, opts CompareOptions) error {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return fmt.Errorf("unmarshal a: %w", err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return fmt.Errorf("unmarshal b: %w", err)
	}
	return jsonValueEqual(av, bv, "", opts)
}

func jsonValueEqual(a, b any, path string, opts CompareOptions) error {
	if a == nil && b == nil {
		return nil
	}
	if a == nil || b == nil {
		return fmt.Errorf("%s: %v != %v", path, a, b)
	}
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return fmt.Errorf("%s: type mismatch (number vs %T)", path, b)
		}
		if opts.LooseNodeLocation && strings.HasSuffix(path, ".line") || strings.HasSuffix(path, ".column") {
			if math.Abs(av-bv) <= 1 {
				return nil
			}
		}
		if av != bv {
			return fmt.Errorf("%s: %v != %v", path, av, bv)
		}
	case string:
		bv, ok := b.(string)
		if !ok || av != bv {
			return fmt.Errorf("%s: %q != %v", path, av, b)
		}
	case bool:
		bv, ok := b.(bool)
		if !ok || av != bv {
			return fmt.Errorf("%s: %v != %v", path, av, b)
		}
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return fmt.Errorf("%s: type mismatch (array vs %T)", path, b)
		}
		if len(av) != len(bv) {
			return fmt.Errorf("%s: array length %d != %d", path, len(av), len(bv))
		}
		for i := range av {
			if err := jsonValueEqual(av[i], bv[i], fmt.Sprintf("%s[%d]", path, i), opts); err != nil {
				return err
			}
		}
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: type mismatch (object vs %T)", path, b)
		}
		if len(av) != len(bv) {
			// surface which keys differ for debuggability
			var aKeys, bKeys []string
			for k := range av {
				aKeys = append(aKeys, k)
			}
			for k := range bv {
				bKeys = append(bKeys, k)
			}
			return fmt.Errorf("%s: key count %d != %d (a:%v b:%v)", path, len(av), len(bv), aKeys, bKeys)
		}
		for k, va := range av {
			vb, ok := bv[k]
			if !ok {
				return fmt.Errorf("%s: key %q missing in b", path, k)
			}
			if err := jsonValueEqual(va, vb, fmt.Sprintf("%s.%s", path, k), opts); err != nil {
				return err
			}
		}
	default:
		if !reflect.DeepEqual(a, b) {
			return fmt.Errorf("%s: %v != %v", path, a, b)
		}
	}
	return nil
}
```

Run: `go build ./internal/difftest/`
Expected: PASS

- [ ] **Step 6: Write unit test for JSON comparator**

Create `internal/difftest/json_compare_test.go`:

```go
package difftest

import "testing"

func TestJSONCompare(t *testing.T) {
	a := []byte(`{"a":1,"b":[1,2,{"c":true}]}`)
	b := []byte(`{"b":[1,2,{"c":true}],"a":1}`)
	if err := JSONEqual(a, b); err != nil {
		t.Fatalf("expected equal: %v", err)
	}
	c := []byte(`{"a":1}`)
	if err := JSONEqual(a, c); err == nil {
		t.Fatal("expected diff")
	}
}
```

Run: `go test ./internal/difftest/ -run TestJSONCompare -v`
Expected: PASS

- [ ] **Step 7: Create analysis golden capture command**

Create `cmd/capture-analysis-golden/main.go`:

```go
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: capture-analysis-golden <java-url> [corpus-root]")
		os.Exit(1)
	}
	javaURL := strings.TrimSuffix(os.Args[1], "/")
	corpusRoot := "testdata/difftest/cases"
	if len(os.Args) >= 3 {
		corpusRoot = os.Args[2]
	}
	outDir := "testdata/difftest/golden-analysis"
	_ = os.MkdirAll(outDir, 0755)

	cases, err := difftest.LoadCorpus(corpusRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load corpus: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	for _, c := range cases {
		groupDir := filepath.Join(outDir, c.Group)
		_ = os.MkdirAll(groupDir, 0755)
		outPath := filepath.Join(groupDir, c.Name+".json")

		reqBody, _ := json.Marshal(map[string]any{
			"manifestStr": base64.StdEncoding.EncodeToString(c.ManifestJSON),
			"sql":         c.SQL,
		})
		// Java JAX-RS @GET + body; matches cmd/capture-golden's http.MethodGet.
		req, err := http.NewRequest(http.MethodGet, javaURL+"/v2/analysis/sql", bytes.NewReader(reqBody))
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: build request: %v\n", c.ID(), err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: request error: %v\n", c.ID(), err)
			continue
		}
		body := &bytes.Buffer{}
		body.ReadFrom(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			_ = os.WriteFile(outPath+".error", body.Bytes(), 0644)
			fmt.Printf("%s: oracle error\n", c.ID())
			continue
		}
		var pretty bytes.Buffer
		json.Indent(&pretty, body.Bytes(), "", "  ")
		_ = os.WriteFile(outPath, pretty.Bytes(), 0644)
		fmt.Printf("%s: captured\n", c.ID())
	}
}
```

Run: `go build ./cmd/capture-analysis-golden/`
Expected: PASS

- [ ] **Step 8: Create analysis diff test skeleton**

Create `internal/difftest/analysis_diff_test.go`:

```go
package difftest

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var acceptAnalysisFlag = flag.Bool("difftest.accept-analysis", false, "rewrite baseline")

const (
	goldenAnalysisDir    = "../../testdata/difftest/golden-analysis"
	baselineAnalysisPath = "../../testdata/difftest/baseline-analysis.json"
)

func TestAnalysis(t *testing.T) {
	cases, err := LoadCorpus("../../testdata/difftest/cases")
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	if len(cases) == 0 {
		t.Skip("no corpus")
	}
	results := make(map[string]string)
	for _, c := range cases {
		status, detail := runAnalysisCase(c)
		results[c.ID()] = status
		if status == "fail" {
			t.Errorf("%s: %s", c.ID(), detail)
		}
	}
	if *acceptAnalysisFlag {
		b, _ := json.MarshalIndent(results, "", "  ")
		_ = os.WriteFile(baselineAnalysisPath, b, 0644)
		t.Log("baseline updated")
		return
	}
	baseline := loadBaseline(baselineAnalysisPath)
	for _, c := range cases {
		want := baseline[c.ID()]
		if want == "" {
			want = "pass"
		}
		if got := results[c.ID()]; got != want {
			t.Errorf("%s: baseline %q, got %q", c.ID(), want, got)
		}
	}
}

func runAnalysisCase(c Case) (status, detail string) {
	return "no-golden", "not yet implemented"
}
```

Run: `go test ./internal/difftest/ -run TestAnalysis -v`
Expected: PASS (all no-golden)

- [ ] **Step 8a: Create synthetic `analysis/` corpus group**

Mirroring P3b/P3c convention, P5 introduces a new corpus group `cases/analysis/` with a small self-contained MDL and 6–8 representative queries. The synthetic MDL avoids TPC-H Jinja blockages and lets P5 hit a clean `pass` baseline.

Create `testdata/difftest/cases/analysis/group.json`:

```json
{ "modelingOnly": false }
```

Create `testdata/difftest/cases/analysis/mdl.json` — a 2-model MDL with one relationship, no calculated columns, no metrics, no views (keeps analyzer fully exercised but rewrite-free):

```json
{
  "catalog": "test",
  "schema": "test",
  "models": [
    {
      "name": "customer",
      "tableReference": { "schema": "main", "table": "customer" },
      "columns": [
        { "name": "custkey", "type": "INTEGER", "notNull": true },
        { "name": "name", "type": "VARCHAR", "notNull": true },
        { "name": "nationkey", "type": "INTEGER", "notNull": true }
      ],
      "primaryKey": "custkey"
    },
    {
      "name": "orders",
      "tableReference": { "schema": "main", "table": "orders" },
      "columns": [
        { "name": "orderkey", "type": "INTEGER", "notNull": true },
        { "name": "custkey", "type": "INTEGER", "notNull": true },
        { "name": "orderdate", "type": "DATE", "notNull": true },
        { "name": "totalprice", "type": "DOUBLE", "notNull": true }
      ],
      "primaryKey": "orderkey"
    }
  ],
  "relationships": [
    {
      "name": "CustomerOrders",
      "models": ["customer", "orders"],
      "joinType": "ONE_TO_MANY",
      "condition": "customer.custkey = orders.custkey"
    }
  ]
}
```

Create 7 queries under `testdata/difftest/cases/analysis/queries/`:

| File | Query | Covers |
|---|---|---|
| `select_simple.sql` | `SELECT custkey, name FROM customer` | basic SELECT + table relation |
| `select_with_alias.sql` | `SELECT custkey AS k FROM customer` | column alias |
| `select_where.sql` | `SELECT name FROM customer WHERE custkey = 1` | filter EXPR |
| `select_logical.sql` | `SELECT name FROM customer WHERE custkey = 1 AND nationkey = 2` | filter AND |
| `select_join.sql` | `SELECT c.name, o.orderkey FROM customer c JOIN orders o ON c.custkey = o.custkey` | JOIN + join criteria |
| `select_group_order.sql` | `SELECT custkey, count(*) FROM orders GROUP BY custkey ORDER BY 1 DESC` | groupBy + sortings + LongLiteral ref |
| `select_subquery.sql` | `SELECT * FROM (SELECT custkey FROM customer) s` | SUBQUERY relation + isSubqueryOrCte |

Run: `ls testdata/difftest/cases/analysis/queries/`
Expected: 7 files.

- [ ] **Step 9: Create empty baseline and Makefile targets**

Create `testdata/difftest/baseline-analysis.json` containing `{}`.

Append to `Makefile`:

```makefile
capture-analysis-golden:
	go run ./cmd/capture-analysis-golden/main.go http://localhost:18080

analysis-difftest:
	go test ./internal/difftest/ -run TestAnalysis -v

accept-analysis:
	go test ./internal/difftest/ -run TestAnalysis -v -difftest.accept-analysis
```

Run: `make analysis-difftest`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/rewrite/analyzer/analysis_getscope.go internal/rewrite/analyzer/field_accessors.go internal/rewrite/analyzer/relation_type_resolve.go internal/rewrite/analyzer/relation_type_resolve_test.go internal/difftest/json_compare.go internal/difftest/json_compare_test.go internal/difftest/analysis_diff_test.go cmd/capture-analysis-golden/main.go testdata/difftest/baseline-analysis.json Makefile
git commit -m "feat(p5): P3a gaps + analysis golden framework (slice 0)"
```

---

## Slice 1: Analysis Result Data Structures + JSON DTOs

**Files:**
- Create: `internal/analyzer/decisionpoint/expr_source.go`
- Create: `internal/analyzer/decisionpoint/context.go`
- Create: `internal/analyzer/decisionpoint/query_analysis.go`
- Create: `internal/analyzer/decisionpoint/relation_analysis.go`
- Create: `internal/analyzer/decisionpoint/filter_analysis.go`
- Create: `internal/dto/analysis_response.go`
- Create: `internal/analyzer/decisionpoint/decision_point_analyzer_stub.go` (stub for forward ref)

- [ ] **Step 1: Create `ExprSource`**

```go
package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

type ExprSource struct {
	Expression    string
	SourceDataset string
	SourceColumn  *string
	NodeLocation  *ast.NodeLocation
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 2: Create `DecisionPointContext`**

```go
package decisionpoint

import rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"

type DecisionPointContext struct {
	builder         *QueryAnalysisBuilder
	scope           *rewriteAnalyzer.Scope
	isSubqueryOrCte bool
}

func WithSubqueryOrCte(ctx *DecisionPointContext, v bool) *DecisionPointContext {
	if ctx == nil {
		return &DecisionPointContext{isSubqueryOrCte: v}
	}
	return &DecisionPointContext{builder: ctx.builder, scope: ctx.scope, isSubqueryOrCte: v}
}

func IsSubqueryOrCte(ctx *DecisionPointContext) bool {
	if ctx == nil {
		return false
	}
	return ctx.isSubqueryOrCte
}

func (c *DecisionPointContext) Builder() *QueryAnalysisBuilder { return c.builder }
func (c *DecisionPointContext) Scope() *rewriteAnalyzer.Scope  { return c.scope }
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 3: Create `QueryAnalysis` and builder**

```go
package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

type QueryAnalysis struct {
	SelectItems     []ColumnAnalysis
	Relation        *RelationAnalysis
	Filter          *FilterAnalysis
	GroupByKeys     [][]GroupByKey
	Sortings        []SortItemAnalysis
	IsSubqueryOrCte bool
}

type ColumnAnalysis struct {
	AliasName    *string
	Expression   string
	Properties   map[string]string
	NodeLocation *ast.NodeLocation
	ExprSources  []ExprSource
}

type SortItemAnalysis struct {
	Expression   string
	Ordering     ast.Ordering
	NodeLocation *ast.NodeLocation
	ExprSources  []ExprSource
}

type GroupByKey struct {
	Expression   string
	NodeLocation *ast.NodeLocation
	ExprSources  []ExprSource
}

type QueryAnalysisBuilder struct {
	selectItems     []ColumnAnalysis
	relation        *RelationAnalysis
	filter          *FilterAnalysis
	groupByKeys     [][]GroupByKey
	sortings        []SortItemAnalysis
	isSubqueryOrCte bool
}

func NewQueryAnalysisBuilder() *QueryAnalysisBuilder { return &QueryAnalysisBuilder{} }

func (b *QueryAnalysisBuilder) AddSelectItem(item ColumnAnalysis) *QueryAnalysisBuilder {
	b.selectItems = append(b.selectItems, item)
	return b
}
func (b *QueryAnalysisBuilder) SetRelation(r *RelationAnalysis) *QueryAnalysisBuilder {
	b.relation = r
	return b
}
func (b *QueryAnalysisBuilder) SetFilter(f *FilterAnalysis) *QueryAnalysisBuilder {
	b.filter = f
	return b
}
func (b *QueryAnalysisBuilder) SetGroupByKeys(keys [][]GroupByKey) *QueryAnalysisBuilder {
	b.groupByKeys = keys
	return b
}
func (b *QueryAnalysisBuilder) SetSortings(s []SortItemAnalysis) *QueryAnalysisBuilder {
	b.sortings = s
	return b
}
func (b *QueryAnalysisBuilder) SetSubqueryOrCte(v bool) *QueryAnalysisBuilder {
	b.isSubqueryOrCte = v
	return b
}
func (b *QueryAnalysisBuilder) GetSelectItems() []ColumnAnalysis { return b.selectItems }
func (b *QueryAnalysisBuilder) Build() *QueryAnalysis {
	return &QueryAnalysis{
		SelectItems: b.selectItems, Relation: b.relation, Filter: b.filter,
		GroupByKeys: b.groupByKeys, Sortings: b.sortings, IsSubqueryOrCte: b.isSubqueryOrCte,
	}
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 4: Create `RelationAnalysis` hierarchy**

```go
package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

type RelationType string

const (
	RelationTypeTable        RelationType = "TABLE"
	RelationTypeSubquery     RelationType = "SUBQUERY"
	RelationTypeInnerJoin    RelationType = "INNER_JOIN"
	RelationTypeLeftJoin     RelationType = "LEFT_JOIN"
	RelationTypeRightJoin    RelationType = "RIGHT_JOIN"
	RelationTypeFullJoin     RelationType = "FULL_JOIN"
	RelationTypeCrossJoin    RelationType = "CROSS_JOIN"
	RelationTypeImplicitJoin RelationType = "IMPLICIT_JOIN"
)

type RelationAnalysis struct {
	Type         RelationType
	Alias        string
	NodeLocation *ast.NodeLocation
	TableName    string
	Left         *RelationAnalysis
	Right        *RelationAnalysis
	Criteria     *JoinCriteria
	ExprSources  []ExprSource
	Body         []*QueryAnalysis
}

type JoinCriteria struct {
	Expression   string
	NodeLocation *ast.NodeLocation
}

func NewTableRelation(tableName, alias string, loc *ast.NodeLocation) *RelationAnalysis {
	return &RelationAnalysis{Type: RelationTypeTable, TableName: tableName, Alias: alias, NodeLocation: loc}
}
func NewJoinRelation(joinType RelationType, alias string, left, right *RelationAnalysis, criteria *JoinCriteria, exprSources []ExprSource, loc *ast.NodeLocation) *RelationAnalysis {
	return &RelationAnalysis{Type: joinType, Alias: alias, Left: left, Right: right, Criteria: criteria, ExprSources: exprSources, NodeLocation: loc}
}
func NewSubqueryRelation(alias string, body []*QueryAnalysis, loc *ast.NodeLocation) *RelationAnalysis {
	return &RelationAnalysis{Type: RelationTypeSubquery, Alias: alias, Body: body, NodeLocation: loc}
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 5: Create `FilterAnalysis` hierarchy**

```go
package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

type FilterType string

const (
	FilterTypeAnd  FilterType = "AND"
	FilterTypeOr   FilterType = "OR"
	FilterTypeExpr FilterType = "EXPR"
)

type FilterAnalysis struct {
	Type         FilterType
	NodeLocation *ast.NodeLocation
	Left         *FilterAnalysis
	Right        *FilterAnalysis
	Node         string
	ExprSources  []ExprSource
}

func NewLogicalAnalysis(typ FilterType, left, right *FilterAnalysis, loc *ast.NodeLocation) *FilterAnalysis {
	return &FilterAnalysis{Type: typ, Left: left, Right: right, NodeLocation: loc}
}
func NewExpressionAnalysis(node string, loc *ast.NodeLocation, exprSources []ExprSource) *FilterAnalysis {
	return &FilterAnalysis{Type: FilterTypeExpr, Node: node, NodeLocation: loc, ExprSources: exprSources}
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 6: (removed)**

The previous draft created `decision_point_analyzer_stub.go` to satisfy a forward reference from `RelationAnalyzer` (slice 2) to `Analyze` (slice 4). **Go doesn't need this** — both files share package `decisionpoint`, so the symbol is visible at compile time once slice 4 lands. Compiling slice 2 alone (without slice 4 yet) will fail with `undefined: Analyze`, which is expected partway through implementation; the executing-plans driver handles this by completing slice 4 before declaring slice 2 buildable. To keep slice 2 standalone-buildable for unit testing during development, the alternative is to inline the TableSubquery analysis as a `TODO: panic("TableSubquery analysis depends on slice 4")` placeholder. The plan adopts that variant — see slice 2 task changes below.

- [ ] **Step 7: Create DTOs**

Create `internal/dto/analysis_response.go`:

```go
package dto

type NodeLocationDto struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type QueryAnalysisDto struct {
	SelectItems     []ColumnAnalysisDto   `json:"selectItems"`
	Relation        *RelationAnalysisDto  `json:"relation,omitempty"`
	Filter          *FilterAnalysisDto    `json:"filter,omitempty"`
	GroupByKeys     [][]GroupByKeyDto     `json:"groupByKeys,omitempty"`
	Sortings        []SortItemAnalysisDto `json:"sortings,omitempty"`
	IsSubqueryOrCte bool                  `json:"isSubqueryOrCte"`
}

type ColumnAnalysisDto struct {
	// Alias is *string + omitempty so nil → absent (matches Java Jackson
	// class-level @JsonInclude(NON_NULL) skipping Optional.empty()).
	Alias *string `json:"alias,omitempty"`
	Expression string `json:"expression"`
	// Properties is always emitted (Java emits {} for an empty Map; class-level
	// NON_NULL only skips null, not empty). Mapper layer must replace nil with
	// an empty map before serialization — see risk #9.
	Properties   map[string]string `json:"properties"`
	NodeLocation *NodeLocationDto  `json:"nodeLocation,omitempty"`
	ExprSources  []ExprSourceDto   `json:"exprSources,omitempty"`
}

type RelationAnalysisDto struct {
	Type         string              `json:"type"`
	Alias        string              `json:"alias,omitempty"`
	Left         *RelationAnalysisDto `json:"left,omitempty"`
	Right        *RelationAnalysisDto `json:"right,omitempty"`
	Criteria     *JoinCriteriaDto     `json:"criteria,omitempty"`
	TableName    string              `json:"tableName,omitempty"`
	Body         []QueryAnalysisDto  `json:"body,omitempty"`
	ExprSources  []ExprSourceDto      `json:"exprSources,omitempty"`
	NodeLocation *NodeLocationDto     `json:"nodeLocation,omitempty"`
}

type JoinCriteriaDto struct {
	Expression   string           `json:"expression"`
	NodeLocation *NodeLocationDto `json:"nodeLocation,omitempty"`
}

type FilterAnalysisDto struct {
	Type         string            `json:"type"`
	Left         *FilterAnalysisDto `json:"left,omitempty"`
	Right        *FilterAnalysisDto `json:"right,omitempty"`
	Node         string            `json:"node,omitempty"`
	NodeLocation *NodeLocationDto  `json:"nodeLocation,omitempty"`
	ExprSources  []ExprSourceDto   `json:"exprSources,omitempty"`
}

type SortItemAnalysisDto struct {
	Expression   string           `json:"expression"`
	Ordering     string           `json:"ordering"`
	NodeLocation *NodeLocationDto `json:"nodeLocation,omitempty"`
	ExprSources  []ExprSourceDto  `json:"exprSources,omitempty"`
}

type GroupByKeyDto struct {
	Expression   string           `json:"expression"`
	NodeLocation *NodeLocationDto `json:"nodeLocation,omitempty"`
	ExprSources  []ExprSourceDto  `json:"exprSources,omitempty"`
}

type ExprSourceDto struct {
	Expression    string           `json:"expression"`
	SourceDataset string           `json:"sourceDataset"`
	SourceColumn  *string          `json:"sourceColumn,omitempty"`
	NodeLocation  *NodeLocationDto `json:"nodeLocation,omitempty"`
}
```

Run: `go build ./internal/dto/`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/analyzer/decisionpoint/ internal/dto/analysis_response.go
git commit -m "feat(p5): analysis result structures + JSON DTOs (slice 1)"
```

---

## Slice 2: RelationAnalyzer

**Files:**
- Create: `internal/analyzer/decisionpoint/relation_analyzer.go`
- Create: `internal/analyzer/decisionpoint/relation_analyzer_test.go`

- [ ] **Step 1: Implement `RelationAnalyzer`**

Create `internal/analyzer/decisionpoint/relation_analyzer.go`:

```go
package decisionpoint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

func AnalyzeRelation(relation ast.Relation, sessionContext *analyzer.SessionContext, wrenMDL *mdl.WrenMDL, analysis *rewriteAnalyzer.Analysis) *RelationAnalysis {
	return analyzeRelationNode(relation, sessionContext, wrenMDL, analysis)
}

func analyzeRelationNode(node ast.Node, sessionContext *analyzer.SessionContext, wrenMDL *mdl.WrenMDL, analysis *rewriteAnalyzer.Analysis) *RelationAnalysis {
	switch n := node.(type) {
	case *ast.Table:
		return NewTableRelation(n.Name.String(), "", n.GetLocation())
	case *ast.Join:
		left := analyzeRelationNode(n.Left, sessionContext, wrenMDL, analysis)
		right := analyzeRelationNode(n.Right, sessionContext, wrenMDL, analysis)
		scope := analysis.GetScope(n)
		var exprSources []ExprSource
		var criteria *JoinCriteria
		var criteriaLoc *ast.NodeLocation
		if n.Criteria != nil {
			exprSources = analyzeJoinCriteria(n.Criteria, scope)
			criteria, criteriaLoc = formatJoinCriteria(n.Criteria)
		}
		joinType := RelationType(fmt.Sprintf("%s_JOIN", n.JoinType))
		return NewJoinRelation(joinType, "", left, right, criteria, exprSources, n.GetLocation())
	case *ast.AliasedRelation:
		inner := analyzeRelationNode(n.Relation, sessionContext, wrenMDL, analysis)
		alias := ""
		if n.Alias != nil {
			alias = n.Alias.Value
		}
		switch inner.Type {
		case RelationTypeTable:
			return NewTableRelation(inner.TableName, alias, n.GetLocation())
		case RelationTypeSubquery:
			return NewSubqueryRelation(alias, inner.Body, n.GetLocation())
		default:
			return &RelationAnalysis{Type: inner.Type, Alias: alias, Left: inner.Left, Right: inner.Right, Criteria: inner.Criteria, ExprSources: inner.ExprSources, NodeLocation: n.GetLocation()}
		}
	case *ast.TableSubquery:
		// Forward-reference into slice 4's Analyze. Allowed in Go because we
		// share package `decisionpoint`. Compiling slice 2 alone (before
		// slice 4) will fail with `undefined: Analyze` — expected; finish
		// slice 4 before re-running this package's tests.
		queries := Analyze(n.Query, sessionContext, wrenMDL)
		// Force isSubqueryOrCte=true on every returned QueryAnalysis (Java
		// QueryAnalysis.Builder.from(...).setSubqueryOrCte(true).build()).
		for i, q := range queries {
			b := NewQueryAnalysisBuilder()
			b.selectItems = q.SelectItems
			b.relation = q.Relation
			b.filter = q.Filter
			b.groupByKeys = q.GroupByKeys
			b.sortings = q.Sortings
			b.isSubqueryOrCte = true
			queries[i] = b.Build()
		}
		return NewSubqueryRelation("", queries, n.GetLocation())
	case *ast.QuerySpecification:
		// Java RelationAnalyzer.visitQuerySpecification falls through to
		// super.visitQuerySpecification (a no-op for relations). We mirror by
		// returning nil and letting the outer DP visitor handle it.
		return nil
	default:
		// Mirror Java throw UnsupportedOperationException for unsupported
		// relation types — never silently return nil (would let bad input
		// produce empty Relation in DTO).
		panic(fmt.Sprintf("Analyze %T is not supported yet", n))
	}
}

func analyzeJoinCriteria(criteria ast.JoinCriteria, scope *rewriteAnalyzer.Scope) []ExprSource {
	switch c := criteria.(type) {
	case *ast.JoinOn:
		return expressionSourceAnalyze(c.Expression, scope)
	case *ast.JoinUsing:
		var out []ExprSource
		for i := range c.Columns {
			out = append(out, expressionSourceAnalyze(&c.Columns[i], scope)...)
		}
		return out
	case *ast.NaturalJoin:
		return nil
	default:
		return nil
	}
}

func formatJoinCriteria(criteria ast.JoinCriteria) (*JoinCriteria, *ast.NodeLocation) {
	switch c := criteria.(type) {
	case *ast.JoinOn:
		expr := formatter.FormatExpression(c.Expression)
		loc := ExpressionLocationAnalyze(c.Expression)
		return &JoinCriteria{Expression: "ON " + expr, NodeLocation: loc}, loc
	case *ast.JoinUsing:
		cols := make([]string, len(c.Columns))
		for i := range c.Columns {
			cols[i] = c.Columns[i].Value
		}
		expr := "USING (" + strings.Join(cols, ", ") + ")"
		var loc *ast.NodeLocation
		if len(c.Columns) > 0 {
			loc = c.Columns[0].GetLocation()
		}
		return &JoinCriteria{Expression: expr, NodeLocation: loc}, loc
	case *ast.NaturalJoin:
		return nil, nil
	default:
		return nil, nil
	}
}

func expressionSourceAnalyze(expr ast.Expression, scope *rewriteAnalyzer.Scope) []ExprSource {
	if expr == nil || scope == nil {
		return nil
	}
	v := &exprSourceVisitor{scope: scope}
	v.walk(expr)
	return sortAndDedupExprSources(v.sources)
}

// sortAndDedupExprSources normalizes the ExprSource slice — risk #2. Java
// builds a HashSet then ImmutableList.copyOf; the resulting list order depends
// on JVM HashSet hash distribution and is not reproducible across runtimes.
// We dedup (Expression, SourceDataset, SourceColumn, line, column) tuples and
// sort by (line, column, expression) so Go and Java goldens (both normalized
// in capture-golden) line up.
func sortAndDedupExprSources(in []ExprSource) []ExprSource {
	if len(in) == 0 {
		return nil
	}
	type key struct {
		expr, ds, col string
		line, column  int
	}
	seen := map[key]bool{}
	out := make([]ExprSource, 0, len(in))
	for _, s := range in {
		var line, col int
		if s.NodeLocation != nil {
			line, col = s.NodeLocation.Line, s.NodeLocation.CharPosition
		}
		var srcCol string
		if s.SourceColumn != nil {
			srcCol = *s.SourceColumn
		}
		k := key{s.Expression, s.SourceDataset, srcCol, line, col}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := 0, 0
		ci, cj := 0, 0
		if out[i].NodeLocation != nil {
			li, ci = out[i].NodeLocation.Line, out[i].NodeLocation.CharPosition
		}
		if out[j].NodeLocation != nil {
			lj, cj = out[j].NodeLocation.Line, out[j].NodeLocation.CharPosition
		}
		if li != lj {
			return li < lj
		}
		if ci != cj {
			return ci < cj
		}
		return out[i].Expression < out[j].Expression
	})
	return out
}

type exprSourceVisitor struct {
	scope   *rewriteAnalyzer.Scope
	sources []ExprSource
}

func (v *exprSourceVisitor) walk(node ast.Node) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.Identifier:
		qn := &ast.QualifiedName{Parts: []string{n.Value}}
		for _, f := range v.scope.RelationType().ResolveFields(qn) {
			v.sources = append(v.sources, ExprSource{Expression: n.Value, SourceDataset: safeString(f.SourceDatasetName()), SourceColumn: safeColumnName(f.SourceColumn()), NodeLocation: n.GetLocation()})
		}
	case *ast.DereferenceExpression:
		qn := ast.GetQualifiedName(n)
		if qn != nil {
			for _, f := range v.scope.RelationType().ResolveFields(qn) {
				v.sources = append(v.sources, ExprSource{Expression: qn.String(), SourceDataset: safeString(f.SourceDatasetName()), SourceColumn: safeColumnName(f.SourceColumn()), NodeLocation: n.GetLocation()})
			}
		} else {
			v.walk(n.Base)
		}
	default:
		for _, child := range node.GetChildren() {
			v.walk(child)
		}
	}
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func safeColumnName(c *dto.Column) *string {
	if c == nil {
		return nil
	}
	return &c.Name
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 2: Write unit test for `AnalyzeRelation` on simple table**

Create `internal/analyzer/decisionpoint/relation_analyzer_test.go`:

```go
package decisionpoint

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser/ast"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

func TestAnalyzeRelationTable(t *testing.T) {
	analysis := rewriteAnalyzer.NewAnalysis(nil)
	table := &ast.Table{Name: ast.QualifiedName{Parts: []string{"s", "t"}}}
	result := AnalyzeRelation(table, nil, nil, analysis)
	if result == nil || result.Type != RelationTypeTable {
		t.Fatalf("expected TABLE, got %v", result)
	}
	if result.TableName != "s.t" {
		t.Fatalf("expected table name s.t, got %s", result.TableName)
	}
}
```

Run: `go test ./internal/analyzer/decisionpoint/ -run TestAnalyzeRelationTable -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/analyzer/decisionpoint/relation_analyzer.go internal/analyzer/decisionpoint/relation_analyzer_test.go
git commit -m "feat(p5): RelationAnalyzer (slice 2)"
```

---

## Slice 3: FilterAnalyzer + Expression Analyzers

**Files:**
- Create: `internal/analyzer/decisionpoint/filter_analyzer.go`
- Create: `internal/analyzer/decisionpoint/decision_expression_analyzer.go`
- Create: `internal/analyzer/decisionpoint/expression_location_analyzer.go`
- Create: `internal/analyzer/decisionpoint/filter_analyzer_test.go`

- [ ] **Step 1: Implement `DecisionExpressionAnalyzer`**

Create `internal/analyzer/decisionpoint/decision_expression_analyzer.go`:

```go
package decisionpoint

import (
	"fmt"
	"github.com/wren-engine/wren/internal/parser/ast"
)

const (
	IncludeFunctionCall          = "includeFunctionCall"
	IncludeMathematicalOperation = "includeMathematicalOperation"
)

var DefaultAnalysis = DecisionExpressionAnalysis{IncludeFunctionCall: false, IncludeMathematicalOperation: false}

type DecisionExpressionAnalysis struct {
	IncludeFunctionCall          bool
	IncludeMathematicalOperation bool
}

func (d DecisionExpressionAnalysis) ToMap() map[string]string {
	return map[string]string{
		IncludeFunctionCall:          fmt.Sprintf("%t", d.IncludeFunctionCall),
		IncludeMathematicalOperation: fmt.Sprintf("%t", d.IncludeMathematicalOperation),
	}
}

func AnalyzeDecisionExpression(expr ast.Expression) DecisionExpressionAnalysis {
	v := &decExprVisitor{}
	v.walk(expr)
	return DecisionExpressionAnalysis{IncludeFunctionCall: v.includeFunctionCall, IncludeMathematicalOperation: v.includeMathematicalOperation}
}

type decExprVisitor struct {
	includeFunctionCall          bool
	includeMathematicalOperation bool
}

func (v *decExprVisitor) walk(node ast.Node) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.FunctionCall:
		v.includeFunctionCall = true
		for _, child := range node.GetChildren() {
			v.walk(child)
		}
	case *ast.ArithmeticBinaryExpression:
		v.includeMathematicalOperation = true
		v.walk(n.Left)
		v.walk(n.Right)
	case *ast.ComparisonExpression:
		v.includeMathematicalOperation = true
		v.walk(n.Left)
		v.walk(n.Right)
	default:
		for _, child := range node.GetChildren() {
			v.walk(child)
		}
	}
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 2: Implement `ExpressionLocationAnalyzer`**

Create `internal/analyzer/decisionpoint/expression_location_analyzer.go`:

```go
package decisionpoint

import "github.com/wren-engine/wren/internal/parser/ast"

func ExpressionLocationAnalyze(node ast.Node) *ast.NodeLocation {
	if node == nil {
		return nil
	}
	v := &exprLocVisitor{}
	v.walk(node)
	return v.loc
}

type exprLocVisitor struct {
	loc *ast.NodeLocation
}

func (v *exprLocVisitor) walk(node ast.Node) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.ComparisonExpression:
		v.walk(n.Left)
	case *ast.ArithmeticBinaryExpression:
		v.walk(n.Left)
	default:
		v.loc = node.GetLocation()
	}
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 3: Implement `FilterAnalyzer`**

**Background on the AST shape (risk #5):** P2 parser uses ANTLR's `VisitAnd` / `VisitOr` to produce `*ast.LogicalExpression` (N-ary: `Operator ast.LogicalOperator`, `Terms []Expression`). The `*ast.LogicalBinaryExpression` type also exists in `internal/parser/ast/expression.go` but **no AstBuilder path emits it** (confirmed via `grep -n LogicalBinaryExpression internal/parser/ast_builder.go` → 0 hits). Java's `LogicalExpression` is also N-ary with the same shape; Java's `FilterAnalyzer` accesses `node.getChildren().get(0)` / `(1)` which presupposes binary. P5 matches Java by treating exactly-binary as logical and N-ary (>2) or non-binary as a leaf via `formatter.FormatExpression`.

Create `internal/analyzer/decisionpoint/filter_analyzer.go`:

```go
package decisionpoint

import (
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// AnalyzeFilter parses the top-level logical structure of a WHERE expression.
// Mirrors Java FilterAnalyzer.analyze: only top-level AND/OR pairs become
// LogicalAnalysis; nested or non-binary logical exprs collapse to a leaf
// ExpressionAnalysis formatted via FormatExpression.
func AnalyzeFilter(expr ast.Expression, scope *rewriteAnalyzer.Scope) *FilterAnalysis {
	return analyzeFilterNode(expr, nil, scope)
}

func analyzeFilterNode(node ast.Node, parent ast.Node, scope *rewriteAnalyzer.Scope) *FilterAnalysis {
	if node == nil {
		return nil
	}
	if le, ok := node.(*ast.LogicalExpression); ok {
		// Only the top-level OR a Logical-nested Logical can split. The
		// parent==nil branch matches Java's first invocation (no parent).
		// The parent-is-Logical branch matches recursive descent through
		// AND/OR. Anything else (e.g., Logical under a Comparison) is a leaf.
		if parent == nil || isLogical(parent) {
			if len(le.Terms) == 2 {
				typ := FilterTypeAnd
				if le.Operator == ast.LogicalOr {
					typ = FilterTypeOr
				}
				return NewLogicalAnalysis(
					typ,
					analyzeFilterNode(le.Terms[0], node, scope),
					analyzeFilterNode(le.Terms[1], node, scope),
					le.GetLocation(),
				)
			}
			// N-ary (>2) flatten: Trino parser collapses `a AND b AND c`
			// into one LogicalExpression with 3 Terms; Java FilterAnalyzer
			// only handles binary, so >2 falls back to a single leaf. The
			// formatted output preserves the original SQL form.
		}
		return leafFilter(le, scope)
	}
	return leafFilter(node, scope)
}

func isLogical(node ast.Node) bool {
	_, ok := node.(*ast.LogicalExpression)
	return ok
}

func leafFilter(node ast.Node, scope *rewriteAnalyzer.Scope) *FilterAnalysis {
	expr := node.(ast.Expression)
	exprStr := formatter.FormatExpression(expr)
	exprSources := expressionSourceAnalyze(expr, scope)
	return NewExpressionAnalysis(exprStr, node.GetLocation(), exprSources)
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 4: Write unit test for `FilterAnalyzer`**

Create `internal/analyzer/decisionpoint/filter_analyzer_test.go`:

```go
package decisionpoint

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser/ast"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

func TestAnalyzeFilterAnd(t *testing.T) {
	// Build via parser so we exercise the same LogicalExpression shape as
	// production (N-ary `Terms` with Operator=LogicalAnd, no LogicalBinaryExpression).
	left := &ast.ComparisonExpression{Operator: ast.ComparisonEqual, Left: &ast.Identifier{Value: "a"}, Right: &ast.LongLiteral{Value: 1}}
	right := &ast.ComparisonExpression{Operator: ast.ComparisonEqual, Left: &ast.Identifier{Value: "b"}, Right: &ast.LongLiteral{Value: 2}}
	expr := &ast.LogicalExpression{Operator: ast.LogicalAnd, Terms: []ast.Expression{left, right}}
	scope := rewriteAnalyzer.ScopeBuilderWithParent(nil).Build()
	result := AnalyzeFilter(expr, scope)
	if result == nil || result.Type != FilterTypeAnd {
		t.Fatalf("expected AND, got %v", result)
	}
	if result.Left == nil || result.Left.Type != FilterTypeExpr {
		t.Fatal("expected left leaf")
	}
	if result.Right == nil || result.Right.Type != FilterTypeExpr {
		t.Fatal("expected right leaf")
	}
}
```

Run: `go test ./internal/analyzer/decisionpoint/ -run TestAnalyzeFilterAnd -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/analyzer/decisionpoint/filter_analyzer.go internal/analyzer/decisionpoint/decision_expression_analyzer.go internal/analyzer/decisionpoint/expression_location_analyzer.go internal/analyzer/decisionpoint/filter_analyzer_test.go
git commit -m "feat(p5): FilterAnalyzer + expression analyzers (slice 3)"
```


---

## Slice 4: DecisionPointAnalyzer Top-Level Assembly

**Files:**
- Modify: `internal/analyzer/decisionpoint/decision_point_analyzer_stub.go` (replace with full implementation)
- Create: `internal/analyzer/decisionpoint/decision_point_analyzer_test.go`

- [ ] **Step 1: Replace stub with full `DecisionPointAnalyzer`**

Delete stub and create `internal/analyzer/decisionpoint/decision_point_analyzer.go`:

```go
package decisionpoint

import (
	"strings"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
	rewriteAnalyzer "github.com/wren-engine/wren/internal/rewrite/analyzer"
)

func Analyze(statement ast.Statement, sessionContext *analyzer.SessionContext, wrenMDL *mdl.WrenMDL) []*QueryAnalysis {
	analysis := rewriteAnalyzer.NewAnalysis(statement)
	rewriteAnalyzer.Analyze(analysis, statement, sessionContext, wrenMDL)
	v := &dpVisitor{analysis: analysis, sessionContext: sessionContext, wrenMDL: wrenMDL}
	v.walk(statement, nil)
	return v.queries
}

type dpVisitor struct {
	analysis       *rewriteAnalyzer.Analysis
	sessionContext *analyzer.SessionContext
	wrenMDL        *mdl.WrenMDL
	queries        []*QueryAnalysis
}

func (v *dpVisitor) walk(node ast.Node, ctx *DecisionPointContext) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *ast.With:
		for _, child := range node.GetChildren() {
			v.walk(child, WithSubqueryOrCte(ctx, true))
		}
	case *ast.TableSubquery:
		for _, child := range node.GetChildren() {
			v.walk(child, WithSubqueryOrCte(ctx, true))
		}
	case *ast.QuerySpecification:
		builder := NewQueryAnalysisBuilder()
		scope := v.analysis.GetScope(n)
		selfCtx := &DecisionPointContext{builder: builder, scope: scope, isSubqueryOrCte: IsSubqueryOrCte(ctx)}
		if n.Select != nil {
			for _, item := range n.Select.SelectItems {
				v.processSelectItem(item, selfCtx)
			}
		}
		if n.From != nil {
			builder.SetRelation(AnalyzeRelation(n.From, v.sessionContext, v.wrenMDL, v.analysis))
		}
		if n.Where != nil {
			builder.SetFilter(AnalyzeFilter(n.Where, selfCtx.Scope()))
		}
		if n.GroupBy != nil {
			builder.SetGroupByKeys(v.analyzeGroupBy(n.GroupBy, selfCtx))
		}
		if len(n.OrderBy) > 0 {
			builder.SetSortings(v.analyzeOrderBy(n.OrderBy, selfCtx))
		}
		v.queries = append(v.queries, builder.Build())
	default:
		for _, child := range node.GetChildren() {
			v.walk(child, ctx)
		}
	}
}

func (v *dpVisitor) processSelectItem(item ast.SelectItem, ctx *DecisionPointContext) {
	switch n := item.(type) {
	case *ast.AllColumns:
		v.processAllColumns(n, ctx)
	case *ast.SingleColumn:
		exprAnalysis := AnalyzeDecisionExpression(n.Expression)
		exprStr := formatter.FormatExpression(n.Expression)
		exprSources := expressionSourceAnalyze(n.Expression, ctx.Scope())
		var alias *string
		if n.Alias != nil {
			alias = &n.Alias.Value
		}
		ctx.Builder().AddSelectItem(ColumnAnalysis{AliasName: alias, Expression: exprStr, Properties: exprAnalysis.ToMap(), NodeLocation: n.GetLocation(), ExprSources: exprSources})
	}
}

func (v *dpVisitor) processAllColumns(node *ast.AllColumns, ctx *DecisionPointContext) {
	scope := ctx.Scope()
	fields := scope.RelationType().Fields()
	if node.QualifiedName != nil {
		// Java SqlFormatter.formatExpression(target, DEFAULT) on a QualifiedName
		// yields dotted text. Mirror with QualifiedName.String().
		target := node.QualifiedName.String()
		if len(fields) == 0 {
			// Remote / unanalyzable relation — emit `<target>.*` placeholder.
			ctx.Builder().AddSelectItem(ColumnAnalysis{Expression: target + ".*", Properties: DefaultAnalysis.ToMap(), NodeLocation: node.GetLocation()})
			return
		}
		// Risk #4: Java filters by relationAlias.toString() == target OR
		// field.getTableName() == toCatalogSchemaTableName(target). Go must
		// match — without this filter, `t.*` emits ALL scoped fields.
		csn := rewriteAnalyzer.ToCatalogSchemaTableName(v.sessionContext, node.QualifiedName)
		for _, f := range fields {
			if !(matchesTarget(f, target, csn)) {
				continue
			}
			if f.SourceColumn() == nil || f.SourceColumn().Relationship != "" || f.SourceColumn().IsCalculated {
				continue
			}
			emitAllColumnsField(ctx.Builder(), f, node.GetLocation())
		}
	} else {
		if len(fields) == 0 {
			ctx.Builder().AddSelectItem(ColumnAnalysis{Expression: "*", Properties: DefaultAnalysis.ToMap(), NodeLocation: node.GetLocation()})
			return
		}
		for _, f := range fields {
			if f.SourceColumn() == nil || f.SourceColumn().Relationship != "" || f.SourceColumn().IsCalculated {
				continue
			}
			emitAllColumnsField(ctx.Builder(), f, node.GetLocation())
		}
	}
}

// matchesTarget mirrors Java filter:
//
//	field.getRelationAlias().filter(alias -> alias.toString().equals(target)).isPresent()
//	|| field.getTableName().equals(catalogSchemaTableName)
func matchesTarget(f *rewriteAnalyzer.Field, target string, csn rewriteAnalyzer.CatalogSchemaTableName) bool {
	if alias := f.RelationAlias(); alias != nil && alias.String() == target {
		return true
	}
	return f.TableName() == csn
}

func emitAllColumnsField(b *QueryAnalysisBuilder, f *rewriteAnalyzer.Field, loc *ast.NodeLocation) {
	name := safeString(f.Name())
	if name == "" {
		name = f.ColumnName()
	}
	src := ExprSource{
		Expression:    name,
		SourceDataset: f.TableName().Table, // Java field.getTableName().getSchemaTableName().getTableName()
		SourceColumn:  safeColumnName(f.SourceColumn()),
		NodeLocation:  loc,
	}
	b.AddSelectItem(ColumnAnalysis{
		Expression:   name,
		Properties:   DefaultAnalysis.ToMap(),
		NodeLocation: loc,
		ExprSources:  []ExprSource{src},
	})
}
```

**Dependencies on P3a accessors (slice 0 task 2):** `Field.RelationAlias()`, `Field.TableName()` (existing), `Field.SourceColumn()`, `Field.SourceDatasetName()`. `utils.ToCatalogSchemaTableName` must exist in `internal/rewrite/analyzer/utils.go`; if not, add this helper to slice 0 step 2 alongside accessors:

```go
// ToCatalogSchemaTableName converts a QualifiedName to a 3-part name using
// sessionContext defaults. Mirrors Java Utils.toCatalogSchemaTableName.
func ToCatalogSchemaTableName(ctx *base.SessionContext, qn *ast.QualifiedName) CatalogSchemaTableName {
	parts := qn.Parts
	switch len(parts) {
	case 1:
		return CatalogSchemaTableName{Catalog: ctx.Catalog, Schema: ctx.Schema, Table: parts[0]}
	case 2:
		return CatalogSchemaTableName{Catalog: ctx.Catalog, Schema: parts[0], Table: parts[1]}
	case 3:
		return CatalogSchemaTableName{Catalog: parts[0], Schema: parts[1], Table: parts[2]}
	default:
		return CatalogSchemaTableName{}
	}
}
```

Note: slice 0 task 2 may already include this — grep `internal/rewrite/analyzer/utils.go` for `ToCatalogSchemaTableName` and add only if missing.

func (v *dpVisitor) analyzeGroupBy(node *ast.GroupBy, ctx *DecisionPointContext) [][]GroupByKey {
	var groups [][]GroupByKey
	for _, expr := range node.Expressions {
		var keys []GroupByKey
		if lit, ok := expr.(*ast.LongLiteral); ok {
			idx := int(lit.Value) - 1
			if idx >= 0 && idx < len(ctx.Builder().GetSelectItems()) {
				field := ctx.Builder().GetSelectItems()[idx]
				exprStr := field.Expression
				if field.AliasName != nil {
					exprStr = *field.AliasName
				}
				keys = append(keys, GroupByKey{Expression: exprStr, NodeLocation: expr.GetLocation(), ExprSources: field.ExprSources})
			}
		} else {
			exprSources := expressionSourceAnalyze(expr, ctx.Scope())
			keys = append(keys, GroupByKey{Expression: formatter.FormatExpression(expr), NodeLocation: expr.GetLocation(), ExprSources: exprSources})
		}
		groups = append(groups, keys)
	}
	return groups
}

func (v *dpVisitor) analyzeOrderBy(items []ast.SortItem, ctx *DecisionPointContext) []SortItemAnalysis {
	var result []SortItemAnalysis
	for _, si := range items {
		if lit, ok := si.SortKey.(*ast.LongLiteral); ok {
			idx := int(lit.Value) - 1
			if idx >= 0 && idx < len(ctx.Builder().GetSelectItems()) {
				field := ctx.Builder().GetSelectItems()[idx]
				exprStr := field.Expression
				if field.AliasName != nil {
					exprStr = *field.AliasName
				}
				result = append(result, SortItemAnalysis{Expression: exprStr, Ordering: si.Ordering, NodeLocation: si.GetLocation(), ExprSources: field.ExprSources})
			}
		} else {
			exprSources := expressionSourceAnalyze(si.SortKey, ctx.Scope())
			result = append(result, SortItemAnalysis{Expression: formatter.FormatExpression(si.SortKey), Ordering: si.Ordering, NodeLocation: si.GetLocation(), ExprSources: exprSources})
		}
	}
	return result
}

// orderingToJavaName maps Go's ast.Ordering ("ASC"/"DESC") to Java's
// SortItem.Ordering.name() ("ASCENDING"/"DESCENDING") — risk #3. Called from
// the DTO mapper layer (slice 5). Kept here so slice 4 code stays parser-form.
func orderingToJavaName(o ast.Ordering) string {
	switch o {
	case ast.OrderingDesc:
		return "DESCENDING"
	default:
		return "ASCENDING"
	}
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 2: Write unit test for `DecisionPointAnalyzer` on simple SELECT**

Create `internal/analyzer/decisionpoint/decision_point_analyzer_test.go`:

```go
package decisionpoint

import (
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
)

func TestAnalyzeSimpleSelect(t *testing.T) {
	sql := "SELECT 1"
	stmt, err := parser.ParseSQL(sql)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ctx := &analyzer.SessionContext{Catalog: "test", Schema: "test"}
	// mdl.NewWrenMDL doesn't exist; use the actual constructor.
	wrenMDL := mdl.WrenMDLFromManifest(&dto.Manifest{Catalog: "test", Schema: "test"})
	result := Analyze(stmt, ctx, wrenMDL)
	if len(result) != 1 {
		t.Fatalf("expected 1 query analysis, got %d", len(result))
	}
	if len(result[0].SelectItems) != 1 {
		t.Fatalf("expected 1 select item, got %d", len(result[0].SelectItems))
	}
}
```

Imports include `"github.com/wren-engine/wren/internal/dto"` alongside `mdl`.

Run: `go test ./internal/analyzer/decisionpoint/ -run TestAnalyzeSimpleSelect -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
rm -f internal/analyzer/decisionpoint/decision_point_analyzer_stub.go
git add internal/analyzer/decisionpoint/decision_point_analyzer.go internal/analyzer/decisionpoint/decision_point_analyzer_test.go
git commit -m "feat(p5): DecisionPointAnalyzer top-level assembly (slice 4)"
```

---

## Slice 5: HTTP Endpoints

**Files:**
- Modify: `internal/server/analysis_handler.go` (replace 80-line stub)
- Create: `internal/server/analysis_handler_test.go`
- Modify: `cmd/wren-engine/main.go` (already wires `analysisHandler := server.NewAnalysisHandler(); analysisHandler.RegisterRoutes(srv.Router())` — no change needed)

**Critical fixes vs. earlier draft:**
- **Routes are `r.Get(...)`, not `r.Post(...)`.** Java JAX-RS uses `@GET` + JSON body; existing `internal/server/analysis_handler.go` stub already uses `r.Get`; `cmd/capture-golden/main.go` already issues `http.MethodGet`. The capture-analysis-golden command in slice 0 step 7 also uses `client.Post` — **also fix to GET** (see slice 6 step 3).
- **`dto.ManifestFromJSON` does not exist** in this codebase. Use `json.Unmarshal` directly into a `dto.Manifest`, or call `mdl.WrenMDLFromJSON(string(rawJSON))` which already does both steps. The plan uses the latter for symmetry with V2.
- **DTO conversion lives in `internal/analyzer/decisionpoint/dto_converter.go`** (slice 6). Handler imports and calls `a.ToDto()` directly — no duplicate `to*Dto` helpers in the server package.

- [ ] **Step 1: Implement `AnalysisHandler`**

Replace contents of `internal/server/analysis_handler.go`:

```go
package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/analyzer/decisionpoint"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
)

// AnalysisHandler implements /v1/analysis/sql, /v2/analysis/sql,
// /v2/analysis/sqls. Mirrors Java AnalysisResource / AnalysisResourceV2.
type AnalysisHandler struct{}

func NewAnalysisHandler() *AnalysisHandler { return &AnalysisHandler{} }

// SqlAnalysisInputDto is the v1 body. The manifest is inline JSON (Java
// SqlAnalysisInputDto.manifest: Manifest).
type SqlAnalysisInputDto struct {
	Manifest *json.RawMessage `json:"manifest"`
	SQL      string           `json:"sql"`
}

// SqlAnalysisInputDtoV2 is the v2 single-SQL body. The manifest is
// base64-encoded JSON (Java SqlAnalysisInputDtoV2.manifestStr: String).
type SqlAnalysisInputDtoV2 struct {
	ManifestStr string `json:"manifestStr"`
	SQL         string `json:"sql"`
}

// SqlAnalysisInputBatchDto is the v2 batch body.
type SqlAnalysisInputBatchDto struct {
	ManifestStr string   `json:"manifestStr"`
	SQLs        []string `json:"sqls"`
}

// RegisterRoutes uses GET to match Java JAX-RS @GET and the existing capture
// tool's http.MethodGet. Chi accepts GET with a request body.
func (h *AnalysisHandler) RegisterRoutes(r chi.Router) {
	r.Get("/v1/analysis/sql", h.AnalyzeSQL)
	r.Get("/v2/analysis/sql", h.AnalyzeSQLV2)
	r.Get("/v2/analysis/sqls", h.AnalyzeSQLs)
}

func (h *AnalysisHandler) AnalyzeSQL(w http.ResponseWriter, r *http.Request) {
	var req SqlAnalysisInputDto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	if req.Manifest == nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "Manifest is required"})
		return
	}
	// dto.ManifestFromJSON doesn't exist; mdl.WrenMDLFromJSON parses + builds
	// in one call. Inline-manifest v1 path: raw JSON bytes from the body.
	wrenMDL, err := mdl.WrenMDLFromJSON(string(*req.Manifest))
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	result, err := analyzeSQL(req.SQL, wrenMDL)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *AnalysisHandler) AnalyzeSQLV2(w http.ResponseWriter, r *http.Request) {
	var req SqlAnalysisInputDtoV2
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	if req.ManifestStr == "" {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "Manifest is required"})
		return
	}
	manifestJSON, err := base64.StdEncoding.DecodeString(req.ManifestStr)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	wrenMDL, err := mdl.WrenMDLFromJSON(string(manifestJSON))
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	result, err := analyzeSQL(req.SQL, wrenMDL)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *AnalysisHandler) AnalyzeSQLs(w http.ResponseWriter, r *http.Request) {
	var req SqlAnalysisInputBatchDto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	if req.ManifestStr == "" {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "Manifest is required"})
		return
	}
	manifestJSON, err := base64.StdEncoding.DecodeString(req.ManifestStr)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	wrenMDL, err := mdl.WrenMDLFromJSON(string(manifestJSON))
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	result := make([][]dto.QueryAnalysisDto, len(req.SQLs))
	for i, sql := range req.SQLs {
		analyses, err := analyzeSQL(sql, wrenMDL)
		if err != nil {
			WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
			return
		}
		result[i] = analyses
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// analyzeSQL is the shared core: parse → analyze → DTO. The single DTO
// conversion path lives in package decisionpoint (slice 6 step 1, dto_converter.go).
func analyzeSQL(sql string, wrenMDL *mdl.WrenMDL) ([]dto.QueryAnalysisDto, error) {
	stmt, err := parser.ParseSQL(sql)
	if err != nil {
		return nil, err
	}
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	analyses := decisionpoint.Analyze(stmt, ctx, wrenMDL)
	out := make([]dto.QueryAnalysisDto, len(analyses))
	for i, a := range analyses {
		out[i] = a.ToDto() // method receiver defined in slice 6's dto_converter.go
	}
	return out, nil
}
```

Notes:
- **No `import "github.com/wren-engine/wren/internal/parser/ast"`** in this file — all conversion is inside decisionpoint.
- `WriteError` / `WrenError` already exist in `internal/server/errors.go` — reused as in the previous stub.
- The handler's `Properties` map nil-safety fix lives in `ToDto()` (slice 6, risk #9). If `Properties` is nil on `ColumnAnalysis`, `ToDto` must replace with `map[string]string{}` to match Java's `{}` emission.

Verify: `cmd/wren-engine/main.go` line 32–33 already does `analysisHandler := server.NewAnalysisHandler(); analysisHandler.RegisterRoutes(srv.Router())` — no diff to that file.

Run: `go build ./internal/server/`
Expected: PASS

- [ ] **Step 2: Verify wiring in `main.go`**

`cmd/wren-engine/main.go` already has (lines 32–33 of existing file):

```go
analysisHandler := server.NewAnalysisHandler()
analysisHandler.RegisterRoutes(srv.Router())
```

No edit required. Run `go build ./cmd/wren-engine/`; expected: PASS.

- [ ] **Step 3: Write endpoint test**

Create `internal/server/analysis_handler_test.go`:

```go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAnalyzeSQLV2(t *testing.T) {
	h := NewAnalysisHandler()
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	// base64 of {"catalog":"test","schema":"test","models":[]}
	reqBody, _ := json.Marshal(map[string]string{
		"manifestStr": "eyJjYXRhbG9nIjoidGVzdCIsInNjaGVtYSI6InRlc3QiLCJtb2RlbHMiOltdfQ==",
		"sql":         "SELECT 1",
	})
	// GET with body — matches Java JAX-RS @GET and the production capture flow.
	req := httptest.NewRequest(http.MethodGet, "/v2/analysis/sql", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 query analysis, got %d", len(result))
	}
}
```

Run: `go test ./internal/server/ -run TestAnalyzeSQLV2 -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/server/analysis_handler.go internal/server/analysis_handler_test.go
git diff --name-only | grep cmd/wren-engine && git add cmd/wren-engine/main.go
git commit -m "feat(p5): HTTP analysis endpoints (slice 5)"
```

---

## Slice 6: Integration, Golden Capture, and Baseline Acceptance

**Files:**
- Modify: `internal/difftest/analysis_diff_test.go` (fill `runAnalysisCase`)
- Create: `internal/analyzer/decisionpoint/dto_converter.go` (conversion helpers)
- Modify: `testdata/difftest/baseline-analysis.json`

- [ ] **Step 1: Create DTO converter in decisionpoint package**

Create `internal/analyzer/decisionpoint/dto_converter.go`:

```go
package decisionpoint

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

func (a *QueryAnalysis) ToDto() dto.QueryAnalysisDto {
	return dto.QueryAnalysisDto{
		SelectItems:     toColumnDtos(a.SelectItems),
		Relation:        toRelationDto(a.Relation),
		Filter:          toFilterDto(a.Filter),
		GroupByKeys:     toGroupKeyDtos(a.GroupByKeys),
		Sortings:        toSortDtos(a.Sortings),
		IsSubqueryOrCte: a.IsSubqueryOrCte,
	}
}

func toColumnDtos(items []ColumnAnalysis) []dto.ColumnAnalysisDto {
	out := make([]dto.ColumnAnalysisDto, len(items))
	for i, c := range items {
		// Risk #9: Properties must always be a non-nil map so JSON emits {}
		// rather than null, matching Java's empty-Map behavior.
		props := c.Properties
		if props == nil {
			props = map[string]string{}
		}
		out[i] = dto.ColumnAnalysisDto{
			Alias:        c.AliasName,
			Expression:   c.Expression,
			Properties:   props,
			NodeLocation: toLocDto(c.NodeLocation),
			ExprSources:  toSrcDtos(c.ExprSources),
		}
	}
	return out
}

func toRelationDto(r *RelationAnalysis) *dto.RelationAnalysisDto {
	if r == nil {
		return nil
	}
	return &dto.RelationAnalysisDto{Type: string(r.Type), Alias: r.Alias, Left: toRelationDto(r.Left), Right: toRelationDto(r.Right), Criteria: toCriteriaDto(r.Criteria), TableName: r.TableName, Body: toQueryDtos(r.Body), ExprSources: toSrcDtos(r.ExprSources), NodeLocation: toLocDto(r.NodeLocation)}
}

func toCriteriaDto(c *JoinCriteria) *dto.JoinCriteriaDto {
	if c == nil {
		return nil
	}
	return &dto.JoinCriteriaDto{Expression: c.Expression, NodeLocation: toLocDto(c.NodeLocation)}
}

func toFilterDto(f *FilterAnalysis) *dto.FilterAnalysisDto {
	if f == nil {
		return nil
	}
	return &dto.FilterAnalysisDto{Type: string(f.Type), Left: toFilterDto(f.Left), Right: toFilterDto(f.Right), Node: f.Node, NodeLocation: toLocDto(f.NodeLocation), ExprSources: toSrcDtos(f.ExprSources)}
}

func toSortDtos(items []SortItemAnalysis) []dto.SortItemAnalysisDto {
	out := make([]dto.SortItemAnalysisDto, len(items))
	for i, s := range items {
		// Risk #3: emit Java's SortItem.Ordering.name() form ("ASCENDING" /
		// "DESCENDING"), not Go's ast.OrderingAsc/Desc constants ("ASC" /
		// "DESC").
		out[i] = dto.SortItemAnalysisDto{
			Expression:   s.Expression,
			Ordering:     orderingToJavaName(s.Ordering),
			NodeLocation: toLocDto(s.NodeLocation),
			ExprSources:  toSrcDtos(s.ExprSources),
		}
	}
	return out
}

func toGroupKeyDtos(groups [][]GroupByKey) [][]dto.GroupByKeyDto {
	out := make([][]dto.GroupByKeyDto, len(groups))
	for i, g := range groups {
		out[i] = make([]dto.GroupByKeyDto, len(g))
		for j, k := range g {
			out[i][j] = dto.GroupByKeyDto{Expression: k.Expression, NodeLocation: toLocDto(k.NodeLocation), ExprSources: toSrcDtos(k.ExprSources)}
		}
	}
	return out
}

func toSrcDtos(sources []ExprSource) []dto.ExprSourceDto {
	out := make([]dto.ExprSourceDto, len(sources))
	for i, s := range sources {
		out[i] = dto.ExprSourceDto{Expression: s.Expression, SourceDataset: s.SourceDataset, SourceColumn: s.SourceColumn, NodeLocation: toLocDto(s.NodeLocation)}
	}
	return out
}

func toQueryDtos(analyses []*QueryAnalysis) []dto.QueryAnalysisDto {
	out := make([]dto.QueryAnalysisDto, len(analyses))
	for i, a := range analyses {
		out[i] = a.ToDto()
	}
	return out
}

func toLocDto(loc *ast.NodeLocation) *dto.NodeLocationDto {
	if loc == nil {
		return nil
	}
	return &dto.NodeLocationDto{Line: loc.Line, Column: loc.CharPosition}
}
```

Run: `go build ./internal/analyzer/decisionpoint/`
Expected: PASS

- [ ] **Step 2: Implement `runAnalysisCase` in diff test**

Modify `internal/difftest/analysis_diff_test.go` to import needed packages and replace `runAnalysisCase`:

```go
import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/analyzer/decisionpoint"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
)

func runAnalysisCase(c Case) (status, detail string) {
	goldenPath := filepath.Join(goldenAnalysisDir, c.Group, c.Name+".json")
	if _, err := os.Stat(goldenPath + ".error"); err == nil {
		return "oracle-error", "Java returned error"
	}
	wantJSON, err := os.ReadFile(goldenPath)
	if err != nil {
		return "no-golden", "golden missing; run make capture-analysis-golden"
	}

	wrenMDL, err := mdl.WrenMDLFromJSON(string(c.ManifestJSON))
	if err != nil {
		return "fail", "mdl parse: " + err.Error()
	}
	stmt, err := parser.ParseSQL(c.SQL)
	if err != nil {
		return "fail", "parse: " + err.Error()
	}
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	analyses := decisionpoint.Analyze(stmt, ctx, wrenMDL)
	dtos := make([]dto.QueryAnalysisDto, len(analyses))
	for i, a := range analyses {
		dtos[i] = a.ToDto()
	}
	gotJSON, _ := json.Marshal(dtos)

	// Risk #15: tolerate ±1 column drift in deeply-nested NodeLocations
	// (`Identifier` token positions differ between ANTLR Go and ANTLR Java
	// because of tokenizer normalization timing).
	if err := JSONEqualWithOptions(wantJSON, gotJSON, CompareOptions{LooseNodeLocation: true}); err != nil {
		return "fail", err.Error()
	}
	return "pass", ""
}
```

Note: add `"github.com/wren-engine/wren/internal/dto"` import.

Run: `go test ./internal/difftest/ -run TestAnalysis -v`
Expected: some pass, some fail (until golden captured)

- [ ] **Step 3: Capture golden from Java oracle**

Prerequisite: Java oracle running at localhost:18080.

Run: `make capture-analysis-golden`
Expected: golden files written to `testdata/difftest/golden-analysis/`

- [ ] **Step 4: Accept baseline**

Run: `make accept-analysis`
Expected: `baseline-analysis.json` updated

- [ ] **Step 5: Verify all tests pass**

Run: `go test ./...`
Expected: all green

Run: `go vet ./...`
Expected: clean

Run: `gofmt -l .`
Expected: no output

- [ ] **Step 6: Final commit**

```bash
git add internal/difftest/analysis_diff_test.go internal/analyzer/decisionpoint/dto_converter.go testdata/difftest/baseline-analysis.json testdata/difftest/golden-analysis/
git commit -m "feat(p5): integration + golden baseline acceptance (slice 6)"
```

---

## 收尾任务

- [ ] **Step 1: P5 全量验证**

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

Expected: build clean; vet clean; gofmt no output; all tests pass.

- [ ] **Step 2: P5 收官清单（与验收性质说明 §「P5 收官真实达成标准」对账）**

- [ ] AstBuilder NodeLocation backfill 已合并；P2 既有 baseline (TPC-H 22 + metric + viewenum) 无回归。
- [ ] `internal/analyzer/decisionpoint/` 10 个文件齐全（query_analysis / relation_analysis / filter_analysis / expr_source / context / decision_expression_analyzer / expression_location_analyzer / relation_analyzer / filter_analyzer / decision_point_analyzer + dto_converter）。
- [ ] `internal/dto/analysis_response.go` 含 8 个 DTO struct（NodeLocationDto / QueryAnalysisDto / ColumnAnalysisDto / RelationAnalysisDto / JoinCriteriaDto / FilterAnalysisDto / SortItemAnalysisDto / GroupByKeyDto / ExprSourceDto）。
- [ ] `internal/server/analysis_handler.go` 80 行桩已替换；3 个端点用 `r.Get(...)`；端到端测试 PASS。
- [ ] `internal/rewrite/analyzer/` 增 3 个 accessor 文件 + 1 个 utils helper（如所需）；既有测试不回归。
- [ ] `cases/analysis/` 7 条合成查询语料就位；`golden-analysis/` 全部 Java oracle JSON 已 capture。
- [ ] `baseline-analysis.json` 接受 7/7 `pass` ；TPC-H 22 条作分析的 baseline 已 capture（允许 ≥18/22 `pass`）。
- [ ] ExprSource 列表排序代码生效（`sortAndDedupExprSources` 在 Go 端口、capture-golden 写 golden 时双路归一化）。
- [ ] SortItem `Ordering` JSON 为 `"ASCENDING"`/`"DESCENDING"`。
- [ ] `Properties` JSON 永远输出（空 `{}` 而非 `null`）。
- [ ] `isSubqueryOrCte` JSON 字段名严格 lowercase-i。
- [ ] `NodeLocation` JSON 字段为 1-based `{"line": N, "column": N}`。
- [ ] P0–P4 既有 baseline（`baseline.json`、`baseline-duckdb.json`、`baseline-envelope.json` 如存在）无回归。

- [ ] **Step 3: 最终提交**

```bash
git add .
git commit -m "feat(p5): close out analysis decisionpoint (slices 0-6 + finalization)"
```

---

## Self-Review Checklist

1. **Spec 覆盖（§2 IN 列表）:**
   - 10 个 `decisionpoint/` 文件全部映射到任务（DecisionPointAnalyzer → Slice 4；QueryAnalysis/RelationAnalysis/FilterAnalysis → Slice 1；RelationAnalyzer → Slice 2；FilterAnalyzer + DecisionExpressionAnalyzer + ExpressionLocationAnalyzer → Slice 3；ExprSource + DecisionPointContext → Slice 1）。
   - HTTP 端点（`/v1/analysis/sql`、`/v2/analysis/sql`、`/v2/analysis/sqls`）→ Slice 5。
   - 分析结果 JSON DTO → Slice 1（dto package）+ Slice 6（converter）。
   - 端到端差分基础设施 → Slice 0 + 6。
   - **超出 spec §2 但 P5 实施必须**：P2 AstBuilder NodeLocation 回填（slice 0 step 0a）—— spec §6 风险 #1 暗示了，但需独立任务承载；本计划已显式登记。

2. **Spec §4 文件表覆盖:** 全 12 行映射到 slice 0/1/5 任务。新增 1 行 `internal/parser/ast_builder.go`（P2 prereq）。

3. **Placeholder scan:** 全部步骤含真实代码或精确 shell 命令；唯一保留的 TODO 标识在「Step 6: (removed)」中说明该 step 被移除。

4. **类型一致性:** `Analyze` 函数签名在 slice 2（前向引用）与 slice 4（定义）一致；`ExpressionLocationAnalyze` 在 slice 2 引用、slice 3 定义；`DefaultAnalysis.ToMap()` 在 slice 4 引用、slice 3 定义；`orderingToJavaName` 在 slice 4 定义、slice 6 使用；`sortAndDedupExprSources` 在 slice 2 定义、被同包内引用。

5. **已知 Go 包内前向引用（编译时一次性解决）:** `decisionpoint` 包内函数互引在最终编译时正常解析；分 slice 实施时若运行单 slice 测试会暂时缺符号，executing-plans 驱动按 slice 顺序推进即可。

6. **诚实性偏差登记:**
   - 验收性质 §1：嵌套 `Identifier` 的 NodeLocation 允许 ±1 列偏差。
   - 验收性质 §2：ExprSource 顺序通过排序归一化（与 Java 行为奇偶等价，因 Java HashSet 顺序本就不稳定）。
   - 验收性质 §3：TPC-H 22 条允许 ≥18/22 `pass`，2–4 条边界 `fail` 不视为回归。
   - 风险 #1：NodeLocation column +1 校正在 `locOf` 一处集中处理，所有节点继承。
   - 风险 #5：`*ast.LogicalBinaryExpression` 类型存在于 AST 但不会被 parser 生成；filter analyzer 不消费这条路径。
