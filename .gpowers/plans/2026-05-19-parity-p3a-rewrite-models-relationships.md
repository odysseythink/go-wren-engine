# P3a 核心重写引擎（模型与关系）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use gpowers:subagent-driven-development (recommended) or gpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 干净机械移植 Java `wren-engine:0.9.3` 的 `WrenSqlRewrite` 非动态字段路径（模型 + 关系 + 计算列），让 Go 引擎对「引用 Wren 模型」的查询产出与 Java 逐字节一致的重写后 SQL。

**Architecture:** 整体丢弃现有 `internal/rewrite/`（901 行惯用法近似实现），按 Java `wren-base/sqlrewrite` 的结构 1:1 重建：编排器 `WrenPlanner` → 4 条规则（P3a 仅 `WrenSqlRewrite` 真实实现，其余透传桩）→ `WrenSqlRewrite` 内部用 `StatementAnalyzer` 识别模型、用 `ModelSqlRender` 把每个模型渲染成 CTE 查询、用 DAG 拓扑序排列 CTE、用 `WithRewriter` 前置 CTE、用 `Rewriter` 把表引用改写为 CTE 名。由外向内分 6 个切片，每切片自成一体并以 P1 差分测试 + baseline 计分板棘轮推进。

**Tech Stack:** Go 1.x；`internal/parser`（P2 已字节对齐的 parser/formatter）；`internal/parser/ast`（P2 已与 trino 对齐的 AST）；`internal/difftest`（P1 golden-snapshot 差分框架）；`internal/dto` / `internal/mdl`（MDL 数据模型）。

---

## 前置条件（必读）

1. **P2 必须已完成并合并。** P3a 的全部输出都经 P2 的 `formatter.FormatSQL` 渲染为字节，且 P3a 的树遍历依赖 P2 对齐后的 AST 节点集（`LogicalExpression` N 元、`SearchedCaseExpression`/`SimpleCaseExpression`/`WhenClause`/`IfExpression`/`NullIfExpression`/`ExtractExpression`/`SubscriptExpression`/`Row`/`ExistsPredicate`/`QuantifiedComparison`、关系节点 `Lateral`/`SampledRelation`/`FunctionRelation`）。本计划中凡引用 AST 节点，均指 **P2 合并后**的 `internal/parser/ast/` 节点集。若执行 P3a 时某节点名与 P2 实际命名不符，以 P2 为准并在对应任务内对齐。

2. **Java 参照源**位于 `../wren-engine-0.9.3/wren-base/src/main/java/io/wren/base/sqlrewrite/`（含 `analyzer/` 子目录）。本计划凡写「移植 `X.java:NN-MM`」均指该路径下的文件。机械移植 = 逐方法对照，控制流与命名一一对应。

3. **包命名约定（全程一致）：**
   - 新建主包 `internal/rewrite/`（package `rewrite`，替换被丢弃的旧包）。
   - 新建子包 `internal/rewrite/analyzer/`（package `analyzer`，对应 Java `sqlrewrite/analyzer/`）。
   - 既有 `internal/analyzer`（提供 `SessionContext`）在所有文件中一律以别名 `base` 导入：`import base "github.com/wren-engine/wren/internal/analyzer"` → `base.SessionContext`。
   - `internal/rewrite` 内引用子包用自然名 `analyzer`：`import "github.com/wren-engine/wren/internal/rewrite/analyzer"` → `analyzer.Analysis`。

## 验收性质说明（重要 — 影响切片 4/5 的验证方式）

**当前 22 条 TPC-H 语料查询全部引用裸表**（小写 `lineitem`/`orders`/`customer`/`part`/`partsupp`/`supplier`/`nation`/`region`），而 MDL 中的 Wren 模型是首字母大写的 `Orders`/`Customer`/`Lineitem`/`Part`/`Nation`。因此 **22 条标准查询都不会触发模型展开**，其 golden 等于纯 `SqlFormatter` 透传输出 —— 它们验证的是 P2 的 formatter 奇偶性，**无法验证 P3a 的核心**（模型 CTE 展开）。

因此本计划在 **任务 4** 新增「引用模型」的查询进 `cases/tpch/queries/`，用 `make capture-golden` 冻结真实 Java golden，作为切片 4/5 的端到端字节奇偶证据。

MDL 体检结论（任务 4、18 据此选用例）：

| 模型 | Jinja 列 | 关系计算列 | P3a 可净渲染 |
|---|---|---|---|
| `Part`（partkey, name） | 无 | 无 | ✅ 完全干净 |
| `Lineitem` | 无 | 无（`orderkey_linenumber` 是无关系的计算列） | ✅ 完全干净 |
| `Nation` | 无 | 无（rel 列 region/customer/supplier 在基础渲染中被过滤） | ✅ 完全干净 |
| `Customer` | `custkey_name`、`custkey_call_concat` 含 `{{ }}` | — | ❌ Jinja，待 P6 |
| `Orders` | 无 | `nation_name = customer.nation.name`，渲染时 LEFT JOIN `Customer` → 传递性拉入 Customer CTE → Jinja | ❌ 传递性 Jinja，待 P6 |

所以 P3a 端到端可转绿的模型查询只能引用 `Part`/`Lineitem`/`Nation`；引用 `Orders`/`Customer` 的查询在 baseline 记为已知失败（`go-error`，待 P6 的 Jinja 层）。**关系遍历计算列的 join 渲染**（`getToOneRelationshipsQuery`/`getToManyRelationshipsQuery`）因 MDL 中唯一的关系计算列 `Orders.nation_name` 被 Jinja 阻断，改由**任务 18 的合成 MDL 单测**覆盖（不依赖 Docker）。

## 字节分歧风险登记表（贯穿全程，执行时逐条核对）

| # | 风险 | 缓解（落在哪个任务） |
|---|---|---|
| 1 | **CTE 顺序（最高风险）** —— Java 用 JGraphT DAG 拓扑迭代决定 WITH CTE 顺序，喂入集合在 Java 是 `HashSet`（按 `Model.hashCode` 排序）。Go 必须确定性。 | 任务 19：拓扑排序的 seed 顺序与平局规则收敛到**单一可调函数**（初值：按名字字典序）。任务 21：以多模型 golden 逆向校验 CTE 顺序，不符则只调该函数。 |
| 2 | Map/Set 迭代序 —— `calculatedScopeSelectItems` 是 `LinkedHashMap`（保插入序）。 | 任务 15：用有序结构（`[]kv` 或 keys 切片 + map），禁用裸 `map` 遍历产出 SQL。 |
| 3 | 字符串模板逐 token —— `ModelSqlRender` 的 `format()` 模板结果会被 `parseQuery` 再解析；模板须 token 一致（空白可不同，但关键字/标识符/标点不可错）。 | 任务 16：逐字复刻 Java 模板字符串。 |
| 4 | CTE 拼接位序 —— 模型 CTE 必须排在用户原有 WITH 之前。 | 任务 14：`applyWith` 用 `append(modelCTEs, userCTEs...)`。 |
| 5 | `Rewriter` 改写条件 —— `visitTable` 仅当 `analysis` 有该 Table 的 sourceNodeName 才改写；`visitDereferenceExpression` 按精确前缀剥 catalog/schema。 | 任务 20：`SourceNodeName` 按 `NodeRef` 身份键查；`WithRewriter` 必须保留 query body 的对象身份。 |
| 6 | CTE 名为带引号标识符 —— Java `new Identifier(name, true)`。 | 任务 14：`getWithQuery` 造 `&ast.Identifier{Value: name, Delimited: true}`。 |
| 7 | 规则间重解析 —— 每轮规则间 `parseSql(formatSql(...))`，依赖 P2 formatter 幂等。 | 任务 1：`Rewrite` 编排循环精确复刻；P2 已保证幂等。 |
| 8 | 动态字段默认关 —— golden 须以 `enable-dynamic-fields=false` 捕获。 | 任务 3：已确认 `tools/oracle-etc/config.properties` 含 `wren.experimental-enable-dynamic-fields=false`，无需重捕获。 |
| 9 | `baseObject` 原始字段 vs 计算值 —— Java `RelationableSqlRender` 用 **原始** `baseObject` 字段（refSql 模型为 null）；Go `dto.Model.GetBaseObject()` 是会返回模型名的**计算方法**，误用会造成模型自依赖成环。 | 任务 15：一律用原始字段 `model.BaseObject`，**不要**调 `GetBaseObject()`。 |

---

# 切片 0 — 透传脊柱

目标：丢弃旧包，建新包骨架，4 条规则全透传，差分测试编译通过、baseline 无回归；新增模型语料并冻结 golden。

## 任务 1：丢弃旧包，建编排器骨架

**Files:**
- Delete: `internal/rewrite/analyzer.go` `descriptor.go` `enum_rewrite.go` `metric_rollup.go` `planner.go` `rule.go` `view_rewrite.go` `with_rewriter.go` `wren_sql.go` `planner_test.go`
- Create: `internal/rewrite/rule.go`
- Create: `internal/rewrite/utils.go`
- Create: `internal/rewrite/planner.go`

- [ ] **Step 1: 删除旧包全部文件**

```bash
cd /Users/ranwei/workspace/go_work/wren-rewrite/go-wren-engine
git rm internal/rewrite/analyzer.go internal/rewrite/descriptor.go \
  internal/rewrite/enum_rewrite.go internal/rewrite/metric_rollup.go \
  internal/rewrite/planner.go internal/rewrite/rule.go \
  internal/rewrite/view_rewrite.go internal/rewrite/with_rewriter.go \
  internal/rewrite/wren_sql.go internal/rewrite/planner_test.go
```

- [ ] **Step 2: 写 `rule.go` —— `WrenRule` 接口**

对应 Java `WrenRule.java`。Java 接口有两个 `apply` 重载（第二个带 `Analysis` 参数，供规则间复用分析结果）；P3a 不复用，只保留单参版本，但返回值加 `error`（对应 Java 抛 `IllegalArgumentException`，见设计 §7「不静默吞错」）。

```go
package rewrite

import (
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// WrenRule is one SQL-rewrite rule. Mirrors Java io.wren.base.sqlrewrite.WrenRule.
// Apply receives a freshly parsed statement and returns the rewritten statement.
// A non-nil error mirrors Java throwing IllegalArgumentException.
type WrenRule interface {
	Apply(root ast.Statement, sessionContext *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error)
}
```

- [ ] **Step 3: 写 `utils.go` —— 解析包装 + checkArgument**

对应 Java `sqlrewrite/Utils.java` 的 `parseSql`/`parseExpression`/`parseQuery` 与 `io.wren.base.Utils.checkArgument`。Java 用 `ParsingOptions(AS_DOUBLE)`；P2 后 Go 的 `parser.ParseSQL` 已内建十进制按 double 解析，故直接包装即可。

```go
package rewrite

import (
	"fmt"
	"sort"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// parseSQL parses a statement. Mirrors Java Utils.parseSql.
func parseSQL(sql string) (ast.Statement, error) {
	return parser.ParseSQL(sql)
}

// parseExpression parses a standalone expression. Mirrors Java Utils.parseExpression.
func parseExpression(sql string) (ast.Expression, error) {
	return parser.ParseExpression(sql)
}

// parseQuery parses sql and asserts the result is a *ast.Query.
// Mirrors Java Utils.parseQuery.
func parseQuery(sql string) (*ast.Query, error) {
	stmt, err := parseSQL(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %s: %w", sql, err)
	}
	q, ok := stmt.(*ast.Query)
	if !ok {
		return nil, fmt.Errorf("not a query: %s", sql)
	}
	return q, nil
}

// checkArgument returns a formatted error when cond is false.
// Mirrors Java com.google.common.base.Preconditions.checkArgument.
func checkArgument(cond bool, format string, args ...any) error {
	if cond {
		return nil
	}
	return fmt.Errorf(format, args...)
}

// contains reports whether s holds v. Shared helper used by the graph and the
// SqlRender tests.
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// sortedKeys returns the keys of set in ascending order — used to turn a
// requiredObjects set into a deterministic slice (risk #1).
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

注：`contains`/`sortedKeys` 在本任务后续若干任务才被引用 —— Go 允许未使用的包级函数，提前定义不影响编译。`dereferenceFrom`/`hasPrefixParts`/`qualifiedConditionString` 由任务 13/16/20 增量追加进本文件。

- [ ] **Step 4: 写 `planner.go` —— 编排循环**

对应 Java `WrenPlanner.java:46-54`。规则间 `parseSql(formatSql(...))` 必须逐字复刻（风险 #7）。`Rewrite` 名称保留 —— `internal/difftest` 已调用 `rewrite.Rewrite`。

```go
package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/formatter"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// AllRules is the ordered rule pipeline. Mirrors Java WrenPlanner.ALL_RULES.
var AllRules = []WrenRule{
	&GenerateViewRewrite{},
	&MetricRollupRewrite{},
	&WrenSqlRewrite{},
	&EnumRewrite{},
}

// Rewrite applies every rule in order, re-parsing the formatted SQL between
// rules so rules cannot interfere. Mirrors Java WrenPlanner.rewrite.
func Rewrite(sql string, sessionContext *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (string, error) {
	statement, err := parseSQL(sql)
	if err != nil {
		return "", err
	}
	for _, rule := range AllRules {
		reparsed, err := parseSQL(formatter.FormatSQL(statement))
		if err != nil {
			return "", fmt.Errorf("re-parse before %T: %w", rule, err)
		}
		statement, err = rule.Apply(reparsed, sessionContext, analyzedMDL)
		if err != nil {
			return "", err
		}
	}
	return formatter.FormatSQL(statement), nil
}
```

- [ ] **Step 5: 暂不编译（规则类型未定义）；进入任务 2 后统一编译**

## 任务 2：四条规则透传桩

**Files:**
- Create: `internal/rewrite/passthrough_rules.go`
- Create: `internal/rewrite/wren_sql_rewrite.go`
- Test: `internal/rewrite/planner_test.go`

- [ ] **Step 1: 写 `passthrough_rules.go` —— 三条非 P3a 规则的透传桩**

对应 Java `GenerateViewRewrite`/`MetricRollupRewrite`/`EnumRewrite`。P3a 范围外（设计 §2「其余 3 条规则做透传桩」），`Apply` 原样返回。

```go
package rewrite

import (
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// GenerateViewRewrite is a P3a pass-through stub for Java GenerateViewRewrite.
// Real implementation lands in P3c.
type GenerateViewRewrite struct{}

func (r *GenerateViewRewrite) Apply(root ast.Statement, _ *base.SessionContext, _ *mdl.AnalyzedMDL) (ast.Statement, error) {
	return root, nil
}

// MetricRollupRewrite is a P3a pass-through stub for Java MetricRollupRewrite.
// Real implementation lands in P3b.
type MetricRollupRewrite struct{}

func (r *MetricRollupRewrite) Apply(root ast.Statement, _ *base.SessionContext, _ *mdl.AnalyzedMDL) (ast.Statement, error) {
	return root, nil
}

// EnumRewrite is a P3a pass-through stub for Java EnumRewrite.
// Real implementation lands in P3c.
type EnumRewrite struct{}

func (r *EnumRewrite) Apply(root ast.Statement, _ *base.SessionContext, _ *mdl.AnalyzedMDL) (ast.Statement, error) {
	return root, nil
}
```

- [ ] **Step 2: 写 `wren_sql_rewrite.go` —— `WrenSqlRewrite` 透传桩**

切片 5 任务 20 将填充真实实现；切片 0 先透传。

```go
package rewrite

import (
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// WrenSqlRewrite expands Wren models/relationships into CTE queries.
// Mirrors Java io.wren.base.sqlrewrite.WrenSqlRewrite (non-dynamic-field path).
type WrenSqlRewrite struct{}

// Apply is a pass-through until task 20 wires the real engine.
func (r *WrenSqlRewrite) Apply(root ast.Statement, sessionContext *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	return root, nil
}
```

- [ ] **Step 3: 写 `planner_test.go` —— 无模型查询透传**

```go
package rewrite

import (
	"testing"

	"github.com/wren-engine/wren/internal/mdl"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestRewritePassThrough(t *testing.T) {
	ctx := &base.SessionContext{Catalog: "wren", Schema: "public"}
	analyzed := mdl.NewAnalyzedMDL(mdl.WrenMDLFromManifest(&dtoEmptyManifest))
	got, err := Rewrite("SELECT 1", ctx, analyzed)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if got != "SELECT 1" {
		t.Fatalf("got %q, want %q", got, "SELECT 1")
	}
}
```

`dtoEmptyManifest` 在测试文件顶部声明：`var dtoEmptyManifest = dto.Manifest{Catalog: "wren", Schema: "public"}`（import `github.com/wren-engine/wren/internal/dto`）。

- [ ] **Step 4: 编译并运行**

Run: `go build ./... && go test ./internal/rewrite/...`
Expected: PASS（`SELECT 1` 经 P2 formatter → `SELECT 1`）。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/
git commit -m "refactor(p3a): discard old rewrite pkg, add pass-through spine

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 3：差分测试接线 + 基线确认

**Files:**
- Read-only 检查: `internal/difftest/difftest_test.go`、`tools/oracle-etc/config.properties`
- Modify: `testdata/difftest/baseline.json`（经 `make difftest-accept`）

- [ ] **Step 1: 确认 difftest 仍编译**

`internal/difftest/difftest_test.go` 调用 `rewrite.Rewrite(c.SQL, ctx, analyzed)` —— 签名与任务 1 保持一致（`(string, *base.SessionContext, *mdl.AnalyzedMDL) (string, error)`），无需改动 difftest。

Run: `go build ./... && go vet ./...`
Expected: 无输出。

- [ ] **Step 2: 确认风险 #8 —— 动态字段默认关**

Run: `grep dynamic tools/oracle-etc/config.properties`
Expected: `wren.experimental-enable-dynamic-fields=false`

确认 golden 以非动态路径捕获 —— P3a 实现的正是非动态路径，假设成立，无需重捕获。

- [ ] **Step 3: 运行差分测试，记录当前状态**

Run: `make difftest`
Expected: 与现有 baseline 一致、无回归（透传桩下，22 条查询的状态由 P2 formatter 决定）。

- [ ] **Step 4: 接受 baseline**

若任务 1–2 改动使个别 case 状态变化（如旧包某 `go-error` 因透传变 `fail`），运行：

Run: `make difftest-accept`
然后 `git diff testdata/difftest/baseline.json` 核对变化合理（只允许朝「旧包 bug 消失」方向）。

- [ ] **Step 5: Commit**

```bash
git add testdata/difftest/baseline.json
git commit -m "test(p3a): rebaseline difftest after rewrite-pkg reset

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 4：新增「引用模型」语料并冻结 golden

**Files:**
- Create: `testdata/difftest/cases/tpch/queries/m_part.sql`
- Create: `testdata/difftest/cases/tpch/queries/m_lineitem.sql`
- Create: `testdata/difftest/cases/tpch/queries/m_lineitem_calc.sql`
- Create: `testdata/difftest/cases/tpch/queries/m_nation.sql`
- Create: `testdata/difftest/cases/tpch/queries/m_join.sql`
- Create: `testdata/difftest/cases/tpch/queries/m_orders.sql`
- Generated: `testdata/difftest/golden/tpch/m_*.sql`（经 `make capture-golden`）
- Modify: `testdata/difftest/baseline.json`

说明：这些查询与 `cases/tpch/queries/` 内既有 22 条共用同一 `cases/tpch/mdl.json`（已含全部模型）。前 5 条引用干净模型（`Part`/`Lineitem`/`Nation`），P3a 应转绿；`m_orders.sql` 引用 `Orders`，因传递性 Jinja（风险见「验收性质说明」）保持已知失败、记录 P6 依赖。

- [ ] **Step 1: 写 5 条干净模型查询**

`m_part.sql`：

```sql
select partkey, name from Part
```

`m_lineitem.sql`：

```sql
select orderkey, extendedprice, discount from Lineitem
```

`m_lineitem_calc.sql`（无关系的计算列 `orderkey_linenumber = concat(l_orderkey, l_linenumber)`）：

```sql
select orderkey, orderkey_linenumber from Lineitem
```

`m_nation.sql`：

```sql
select nationkey, name from Nation
```

`m_join.sql`（两个干净模型 JOIN）：

```sql
select l.orderkey, p.name
from Lineitem l join Part p on l.partkey = p.partkey
```

- [ ] **Step 2: 写 1 条 Orders 查询（预期已知失败，文档化 P6 依赖）**

`m_orders.sql`：

```sql
select orderkey, totalprice from Orders
```

- [ ] **Step 3: 启动 Java oracle 并捕获 golden**

需 Docker。

Run: `make capture-golden`
Expected: 控制台逐条 `OK tpch/m_part` …；`testdata/difftest/golden/tpch/` 下出现 `m_part.sql`…`m_orders.sql`（或 `.error`）。

- [ ] **Step 4: 人工检视 golden，确认形态**

阅读 `testdata/difftest/golden/tpch/m_part.sql`：应为 `WITH "Part" AS (SELECT ...) SELECT partkey, name FROM Part` 形态（含 WITH 子句、模型 CTE）。这是 P3a 要复刻的目标形态。把观察到的 CTE 结构（尤其多模型时的 CTE 排列顺序）记录在任务 18/21 的执行笔记里 —— 风险 #1 的逆向校验依据。

- [ ] **Step 5: 接受 baseline**

Run: `make difftest-accept`
此刻 `m_*` 应为 `fail`（透传桩未展开模型）或 `go-error`。`git diff testdata/difftest/baseline.json` 确认仅新增 `tpch/m_*` 条目。

- [ ] **Step 6: Commit**

```bash
git add testdata/difftest/cases/tpch/queries/m_*.sql testdata/difftest/golden/tpch/m_*.sql testdata/difftest/baseline.json
git commit -m "test(p3a): add model-referencing corpus queries + golden

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 1 — 树重写基础设施

目标：泛型 AST 变换器，覆盖 P2 后的全部 AST 节点；恒等 round-trip 不变。

## 任务 5：树重写器 `RewriteNode`

**Files:**
- Create: `internal/rewrite/base_tree_rewriter.go`
- Test: `internal/rewrite/base_tree_rewriter_test.go`

说明：Java 把树重写拆成 `BaseTreeRewriter`（查询节点）+ `BaseRewriter`（表达式/类型节点）两层，靠继承 + 方法覆盖做动态分派。Go 无虚分派，改用**钩子函数**惯用法（Approach C 允许基础设施惯用重写）：`RewriteNode(node, hook)` 遍历整棵树，每到一个节点先问 `hook`；`hook` 返回 `(替换节点, true)` 则停止下钻，返回 `(_, false)` 则用「被递归改写的子节点」重建该节点。Java 三个具体 rewriter（`WithRewriter`/`Rewriter`/`RelationshipRewriter`）的 context 都是 `Void`，故 Go 版省去泛型 context。两层合并进单文件 `base_tree_rewriter.go`。

- [ ] **Step 1: 写恒等 round-trip 测试（先失败）**

恒等钩子永不处理任何节点，故 `RewriteNode` 重建整棵树；重建后经 formatter 渲染应与原树完全一致。

```go
package rewrite

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

func TestRewriteNodeIdentity(t *testing.T) {
	cases := []string{
		"SELECT 1",
		"SELECT a, b FROM t WHERE c > 5 AND d < 10",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"SELECT count(*) FROM (SELECT k FROM t GROUP BY k) s",
		"SELECT a FROM t1 JOIN t2 ON t1.id = t2.id ORDER BY a DESC",
		"SELECT CASE WHEN a THEN 1 ELSE 2 END FROM t",
	}
	identity := func(n ast.Node) (ast.Node, bool) { return nil, false }
	for _, sql := range cases {
		stmt, err := parser.ParseSQL(sql)
		if err != nil {
			t.Fatalf("parse %q: %v", sql, err)
		}
		want := formatter.FormatSQL(stmt)
		got := formatter.FormatSQL(RewriteNode(stmt, identity).(ast.Statement))
		if got != want {
			t.Errorf("identity rewrite changed %q:\n want %q\n got  %q", sql, want, got)
		}
	}
}
```

Run: `go test ./internal/rewrite/ -run TestRewriteNodeIdentity`
Expected: 编译失败（`RewriteNode` 未定义）。

- [ ] **Step 2: 写 `base_tree_rewriter.go` —— 钩子类型与分派骨架**

```go
package rewrite

import "github.com/wren-engine/wren/internal/parser/ast"

// RewriteHook inspects a node. Returning (replacement, true) substitutes the
// node and stops descent; returning (_, false) makes RewriteNode rebuild the
// node from its rewritten children. Mirrors a Java BaseRewriter visitX override.
type RewriteHook func(node ast.Node) (ast.Node, bool)

// RewriteNode returns node with hook applied throughout the tree. Every
// container node is rebuilt from RewriteNode-recursed children; leaves and
// unknown nodes are returned unchanged (mirrors Java BaseTreeRewriter.visitNode).
func RewriteNode(node ast.Node, hook RewriteHook) ast.Node {
	if node == nil {
		return nil
	}
	if r, ok := hook(node); ok {
		return r
	}
	switch n := node.(type) {

	case *ast.Query:
		out := *n
		if n.With != nil {
			out.With = RewriteNode(n.With, hook).(*ast.With)
		}
		if n.Body != nil {
			out.Body = RewriteNode(n.Body, hook).(ast.QueryBody)
		}
		out.OrderBy = rewriteSortItems(n.OrderBy, hook)
		out.Limit = rewriteExpr(n.Limit, hook)
		out.Offset = rewriteExpr(n.Offset, hook)
		return &out

	case *ast.QuerySpecification:
		out := *n
		if n.Select != nil {
			out.Select = RewriteNode(n.Select, hook).(*ast.Select)
		}
		if n.From != nil {
			out.From = RewriteNode(n.From, hook).(ast.Relation)
		}
		out.Where = rewriteExpr(n.Where, hook)
		if n.GroupBy != nil {
			out.GroupBy = RewriteNode(n.GroupBy, hook).(*ast.GroupBy)
		}
		out.Having = rewriteExpr(n.Having, hook)
		out.OrderBy = rewriteSortItems(n.OrderBy, hook)
		out.Limit = rewriteExpr(n.Limit, hook)
		out.Offset = rewriteExpr(n.Offset, hook)
		return &out

	case *ast.Table:
		return n // leaf relation; identity preserved

	case *ast.DereferenceExpression:
		out := *n
		out.Base = rewriteExpr(n.Base, hook)
		return &out

	// ... 见 Step 3 的完整节点清单 ...

	default:
		return node
	}
}

// rewriteExpr is a nil-safe RewriteNode for expressions.
func rewriteExpr(e ast.Expression, hook RewriteHook) ast.Expression {
	if e == nil {
		return nil
	}
	return RewriteNode(e, hook).(ast.Expression)
}

// rewriteExprs rewrites every expression in a slice (returns a fresh slice).
func rewriteExprs(es []ast.Expression, hook RewriteHook) []ast.Expression {
	if es == nil {
		return nil
	}
	out := make([]ast.Expression, len(es))
	for i, e := range es {
		out[i] = rewriteExpr(e, hook)
	}
	return out
}

// rewriteSortItems rewrites the sort key of every SortItem.
func rewriteSortItems(items []ast.SortItem, hook RewriteHook) []ast.SortItem {
	if items == nil {
		return nil
	}
	out := make([]ast.SortItem, len(items))
	for i, it := range items {
		out[i] = it
		out[i].SortKey = rewriteExpr(it.SortKey, hook)
	}
	return out
}
```

- [ ] **Step 3: 补齐每一个有子节点的容器节点的 `case`**

为 `internal/parser/ast/`（P2 合并后）**每一个带子节点的节点类型**写 `case`，模式统一：`out := *n` 复制 → 用 `RewriteNode`/`rewriteExpr`/`rewriteExprs` 改写每个子字段 → 返回 `&out`。叶子节点（`*ast.Identifier`、各 `*ast.*Literal`、`*ast.StarExpression`、`*ast.NaturalJoin`、`*ast.Table`、`*ast.AllColumns`、`*ast.NumericParameter`）无 `case`，落入 `default` 原样返回。

必须覆盖的容器节点清单（参照 Java `BaseTreeRewriter.java` 1197 行 + `BaseRewriter.java` 650 行，逐节点对照）：

- 查询/语句：`Query`、`QuerySpecification`、`Union`/`Intersect`/`Except`（P2 集合运算节点 —— 改写其 `Relations` 切片）。
- 查询部件：`Select`（改写 `SelectItems`）、`SingleColumn`（改写 `Expression`，保留 `Alias`）、`With`（改写 `Queries` 内每个 `WithQuery`）、`WithQuery`（改写 `Query`）、`GroupBy`（改写 `Expressions`）、`Window`（改写 `PartitionBy`/`OrderBy`/`Frame`）、`WindowFrame`（改写 `Start.Value`/`End.Value`）。
- 关系：`AliasedRelation`（改写 `Relation`）、`Join`（改写 `Left`/`Right`/`Criteria`）、`JoinOn`（改写 `Expression`）、`JoinUsing`（叶子，列名不变）、`TableSubquery`（改写 `Query`）、`Unnest`（改写 `Expressions`）、`Values`（改写 `Rows`）、`SampledRelation`/`Lateral`/`FunctionRelation`（P2 关系节点）。
- 表达式：`ComparisonExpression`、`ArithmeticBinaryExpression`、`LogicalExpression`（P2 的 N 元逻辑节点 —— 改写 `Terms` 切片）、`NotExpression`、`FunctionCall`（改写 `Arguments`/`OrderBy`/`Filter`/`Window`）、`Cast`（改写 `Expression`）、`CoalesceExpression`、`InPredicate`（改写 `Value`/`ValueList`）、`InListExpression`、`BetweenPredicate`、`SubqueryExpression`、`AtTimeZone`、`IsNullPredicate`、`LikePredicate`、`SearchedCaseExpression`/`SimpleCaseExpression`/`WhenClause`/`IfExpression`/`NullIfExpression`/`ExtractExpression`/`SubscriptExpression`/`Row`/`ExistsPredicate`/`QuantifiedComparison`（P2 表达式节点）。
- 类型：`DataType`、`TypeParameter`（一般无需改写内部，但保持 `case` 以防漏遍历；可直接 `return node`）。

每个 `case` 一定要 `out := *n` 复制后改字段，**不可原地改 `n`**（Java 每个 `visitX` 都 `new` 新节点；原地改会破坏规则间隔离）。

**关键例外 —— `SingleColumn` 在 `GetChildren` 里把 `Alias` 当 child，但树重写器不能把 `Alias`（一个 `*Identifier`）交给 hook 当普通表达式改写**（否则 `Rewriter` 的剥前缀逻辑会误伤别名）。`SingleColumn` 的 `case` 只改写 `Expression`、原样保留 `Alias`。对照 Java `BaseTreeRewriter.visitSingleColumn`。

- [ ] **Step 4: 运行恒等测试**

Run: `go test ./internal/rewrite/ -run TestRewriteNodeIdentity -v`
Expected: PASS（6 条全部 round-trip 不变）。

- [ ] **Step 5: `go vet`**

Run: `go vet ./internal/rewrite/...`
Expected: 无输出。

- [ ] **Step 6: Commit**

```bash
git add internal/rewrite/base_tree_rewriter.go internal/rewrite/base_tree_rewriter_test.go
git commit -m "feat(p3a): add generic AST tree rewriter (slice 1)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 2 — 分析器

目标：`analyzer` 子包识别查询引用了哪些 Wren 模型，并为每个 `Table` 节点登记 sourceNodeName。

## 任务 6：分析器叶子支撑类型

**Files:**
- Create: `internal/rewrite/analyzer/catalog_schema_table_name.go`
- Create: `internal/rewrite/analyzer/relation_id.go`
- Create: `internal/rewrite/analyzer/relation_type.go`
- Create: `internal/rewrite/analyzer/field.go`
- Create: `internal/rewrite/analyzer/scope.go`
- Create: `internal/rewrite/analyzer/scope_analysis.go`

- [ ] **Step 1: `catalog_schema_table_name.go`**

对应 Java `io.wren.base.CatalogSchemaTableName`（三段表名）。

```go
package analyzer

// CatalogSchemaTableName is a fully qualified table name (catalog.schema.table).
// Mirrors Java io.wren.base.CatalogSchemaTableName.
type CatalogSchemaTableName struct {
	Catalog string
	Schema  string
	Table   string
}
```

- [ ] **Step 2: `relation_id.go`**

对应 `analyzer/RelationId.java`。Java 的 `RelationId` 用 `NodeRef`（身份）区分；匿名 `RelationId` 只等于自身。Go 用指针身份：

```go
package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

// RelationId identifies a relation by its source AST node (by identity).
// An anonymous RelationId (sourceNode == nil) equals only itself.
// Mirrors Java sqlrewrite.analyzer.RelationId.
type RelationId struct {
	sourceNode ast.Node
}

func RelationIdOf(sourceNode ast.Node) RelationId { return RelationId{sourceNode: sourceNode} }
func AnonymousRelationId() RelationId             { return RelationId{} }

func (r RelationId) IsAnonymous() bool      { return r.sourceNode == nil }
func (r RelationId) SourceNode() ast.Node   { return r.sourceNode }
```

- [ ] **Step 3: `relation_type.go`**

对应 `analyzer/RelationType.java`。`resolveFields`/`resolveAnyField` 只匹配「非关系列」字段。

```go
package analyzer

import "github.com/wren-engine/wren/internal/parser/ast"

// RelationType is an ordered list of Fields. Mirrors Java analyzer.RelationType.
type RelationType struct {
	fields []*Field
}

func NewRelationType(fields []*Field) *RelationType { return &RelationType{fields: fields} }

func (rt *RelationType) Fields() []*Field { return rt.fields }

// ResolveAnyField returns the first non-relationship field that resolves name.
// Mirrors RelationType.resolveAnyField.
func (rt *RelationType) ResolveAnyField(name *ast.QualifiedName) *Field {
	for _, f := range rt.fields {
		if f.sourceColumn != nil && f.sourceColumn.Relationship != "" {
			continue
		}
		if f.CanResolve(name) {
			return f
		}
	}
	return nil
}

// JoinWith concatenates the fields of two RelationTypes. Mirrors joinWith.
func (rt *RelationType) JoinWith(other *RelationType) *RelationType {
	return NewRelationType(append(append([]*Field{}, rt.fields...), other.fields...))
}
```

- [ ] **Step 4: `field.go`**

机械移植 `analyzer/Field.java`（204 行）。Go 用结构体 + 构造函数（不用 Java builder）。字段：`relationAlias *ast.QualifiedName`、`tableName CatalogSchemaTableName`、`columnName string`、`name *string`、`sourceDatasetName *string`、`sourceColumn *dto.Column`。移植 `matchesPrefix`（`Field.java:86-89`）与 `canResolve`（`Field.java:112-121`，含 struct 类型那一支）。`like(field)` 对应一个拷贝构造。

```go
package analyzer

import (
	"strings"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// Field is one resolvable column in a RelationType. Mirrors analyzer.Field.
type Field struct {
	relationAlias     *ast.QualifiedName
	tableName         CatalogSchemaTableName
	columnName        string
	name              *string
	sourceDatasetName *string
	sourceColumn      *dto.Column
}

func (f *Field) TableName() CatalogSchemaTableName { return f.tableName }
func (f *Field) ColumnName() string                { return f.columnName }
func (f *Field) Name() *string                     { return f.name }
func (f *Field) SourceColumn() *dto.Column          { return f.sourceColumn }

// MatchesPrefix mirrors Field.matchesPrefix: empty prefix matches; otherwise
// the relation alias (or table name) must have the prefix as a suffix.
func (f *Field) MatchesPrefix(prefix *ast.QualifiedName) bool {
	if prefix == nil {
		return true
	}
	scope := f.relationAlias
	if scope == nil {
		qn := tableNameToQualifiedName(f.tableName)
		scope = &qn
	}
	return hasSuffix(*scope, *prefix)
}

// CanResolve mirrors Field.canResolve.
func (f *Field) CanResolve(name *ast.QualifiedName) bool {
	if name == nil || f.name == nil {
		return false
	}
	if f.MatchesPrefix(qualifiedNamePrefix(name)) &&
		strings.EqualFold(*f.name, qualifiedNameSuffix(name)) {
		return true
	}
	if p := qualifiedNamePrefix(name); p != nil && p.String() == f.columnName {
		return true // struct type support
	}
	return false
}
```

辅助函数 `tableNameToQualifiedName`、`hasSuffix`、`qualifiedNamePrefix`、`qualifiedNameSuffix` 放在任务 8 的 `analyzer/utils.go`（QualifiedName 前缀/后缀语义对照 Java `QualifiedName.getPrefix`/`getSuffix`/`hasSuffix`）。

- [ ] **Step 5: `scope_analysis.go`**

机械移植 `analyzer/ScopeAnalysis.java`（70 行）：`usedWrenObjects []Relation` + `aliasedMap map[ast.NodeRef]string`；`Relation{Name, Alias string}`。`addUsedWrenObject(*ast.Table)` 取 `table.Name` 的 suffix（最后一段）。

- [ ] **Step 6: `scope.go`**

机械移植 `analyzer/Scope.java`（136 行）：`parent *Scope`、`relationId RelationId`、`relationType *RelationType`、`isDataSourceScope bool`、`namedQueries map[string]*ast.WithQuery`。移植 `GetNamedQuery`（沿 parent 链查找）。Go 用一个 `ScopeBuilder` 结构体复刻 Java builder（任务 10 的 `StatementAnalyzer` 大量用 `Scope.builder()`）。

- [ ] **Step 7: 编译**

Run: `go build ./internal/rewrite/analyzer/...`
Expected: 通过（叶子类型互相独立，无 `Analysis`/`StatementAnalyzer` 依赖）。

- [ ] **Step 8: Commit**

```bash
git add internal/rewrite/analyzer/
git commit -m "feat(p3a): add analyzer leaf types — RelationId/Field/Scope (slice 2)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 7：`Analysis` 数据载体

**Files:**
- Create: `internal/rewrite/analyzer/analysis.go`

- [ ] **Step 1: 写 `analysis.go`**

机械移植 `analyzer/Analysis.java`（230 行）。关键点：

- 所有按节点身份键的 map 用 `map[ast.NodeRef]X`（`ast.NodeRef` = `struct{ Node ast.Node }`，指针身份可比较）。
- `models`/`metrics`/`cumulativeMetrics`/`views` Java 是 `HashSet`；Go 用 `[]*dto.Model` 等切片，并在 `addModels` 时**按名字排序后去重**（风险 #1：`getModels()` 必须确定性，否则 CTE 顺序不确定）。
- `tables` Java `HashSet<CatalogSchemaTableName>`；Go 用 `[]CatalogSchemaTableName` + 去重。
- `scopes`/`sourceNodeNames` 按 `NodeRef` 键。

```go
package analyzer

import (
	"sort"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// Analysis carries everything StatementAnalyzer discovers about a statement.
// Mirrors Java sqlrewrite.analyzer.Analysis.
type Analysis struct {
	root ast.Statement

	scopes          map[ast.NodeRef]*Scope
	tables          []CatalogSchemaTableName
	models          []*dto.Model
	metrics         []*dto.Metric
	cumulativeMetrics []*dto.CumulativeMetric
	views           []*dto.View
	collectedColumns map[CatalogSchemaTableName]map[string]bool // table -> column set
	referenceFields map[ast.NodeRef]*Field
	requiredSourceNodes map[ast.NodeRef]ast.Node
	sourceNodeNames map[ast.NodeRef]ast.QualifiedName
}

func NewAnalysis(root ast.Statement) *Analysis {
	return &Analysis{
		root:                root,
		scopes:              map[ast.NodeRef]*Scope{},
		collectedColumns:    map[CatalogSchemaTableName]map[string]bool{},
		referenceFields:     map[ast.NodeRef]*Field{},
		requiredSourceNodes: map[ast.NodeRef]ast.Node{},
		sourceNodeNames:     map[ast.NodeRef]ast.QualifiedName{},
	}
}

func (a *Analysis) Root() ast.Statement { return a.root }

// AddTable adds a table, de-duplicating. Mirrors Analysis.addTable.
func (a *Analysis) AddTable(t CatalogSchemaTableName) {
	for _, x := range a.tables {
		if x == t {
			return
		}
	}
	a.tables = append(a.tables, t)
}
func (a *Analysis) Tables() []CatalogSchemaTableName { return a.tables }

// AddModels merges models, then sorts by name for deterministic CTE ordering.
func (a *Analysis) AddModels(models []*dto.Model) {
	seen := map[string]bool{}
	for _, m := range a.models {
		seen[m.Name] = true
	}
	for _, m := range models {
		if !seen[m.Name] {
			seen[m.Name] = true
			a.models = append(a.models, m)
		}
	}
	sort.Slice(a.models, func(i, j int) bool { return a.models[i].Name < a.models[j].Name })
}
func (a *Analysis) Models() []*dto.Model { return a.models }

// SetScope / TryGetScope / GetScope mirror the scope map accessors.
func (a *Analysis) SetScope(n ast.Node, s *Scope) { a.scopes[ast.NodeRef{Node: n}] = s }
func (a *Analysis) TryGetScope(n ast.Node) (*Scope, bool) {
	s, ok := a.scopes[ast.NodeRef{Node: n}]
	return s, ok
}

// AddSourceNodeName / SourceNodeName key by node identity.
func (a *Analysis) AddSourceNodeName(n ast.Node, name ast.QualifiedName) {
	a.sourceNodeNames[ast.NodeRef{Node: n}] = name
}
func (a *Analysis) SourceNodeName(n ast.Node) (ast.QualifiedName, bool) {
	name, ok := a.sourceNodeNames[ast.NodeRef{Node: n}]
	return name, ok
}
```

`AddMetrics`/`AddCumulativeMetrics`/`AddViews`（同 `AddModels` 模式，按名排序）、`AddCollectedColumns(fields []*Field)`（往 `collectedColumns` 写）、`AddReferenceFields`、`AddRequiredSourceNode`、`Metrics()`/`CumulativeMetrics()`/`Views()`/`CollectedColumns()` 等访问器一并补全 —— 逐条对照 `Analysis.java` 的 getter/setter。

- [ ] **Step 2: 编译**

Run: `go build ./internal/rewrite/analyzer/...`
Expected: 通过。

- [ ] **Step 3: Commit**

```bash
git add internal/rewrite/analyzer/analysis.go
git commit -m "feat(p3a): add Analysis data carrier (slice 2)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 8：`ScopeAnalyzer` 与分析器工具

**Files:**
- Create: `internal/rewrite/analyzer/utils.go`
- Create: `internal/rewrite/analyzer/scope_analyzer.go`

- [ ] **Step 1: 写 `utils.go` —— QualifiedName 工具 + `toCatalogSchemaTableName` + `analyzeFrom`/`toField`**

`toCatalogSchemaTableName`：机械移植 `sqlrewrite/Utils.java:260-276` —— 把 `ast.QualifiedName`（≤3 段）按 `[catalog.]schema.table` 反序解析，缺省段取 `SessionContext.Catalog`/`Schema`。

QualifiedName 工具（`field.go`/`statement_analyzer.go` 用）：
- `qualifiedNamePrefix(qn) *ast.QualifiedName` —— 去最后一段，0/1 段返回 nil。
- `qualifiedNameSuffix(qn) string` —— 最后一段。
- `hasSuffix(qn, suffix) bool` —— `qn` 末 N 段等于 `suffix`。
- `tableNameToQualifiedName(CatalogSchemaTableName) ast.QualifiedName`。
- `qualifiedNameOfExpression(expr) *ast.QualifiedName` —— 对应 trino `QueryUtil.getQualifiedName`：`Identifier`/`DereferenceExpression` 链 → QualifiedName，其它 → nil（复用 `ast.GetQualifiedName`）。

`analyzeFrom` + `toField`：机械移植 `Utils.java:284-322` —— 跑 `ScopeAnalyzer` 收集 used Wren 对象，对每个 model/metric 的列造 `Field`，建一个含这些字段的 `Scope`。

```go
package analyzer

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// toCatalogSchemaTableName resolves a (≤3-part) QualifiedName to a CSTN,
// filling missing parts from the session. Mirrors Utils.toCatalogSchemaTableName.
func toCatalogSchemaTableName(ctx *base.SessionContext, name ast.QualifiedName) (CatalogSchemaTableName, error) {
	parts := name.Parts
	if len(parts) > 3 {
		return CatalogSchemaTableName{}, errTooManyDots(name)
	}
	// reversed: parts[last] is the object name
	obj := parts[len(parts)-1]
	schema := ctx.Schema
	if len(parts) > 1 {
		schema = parts[len(parts)-2]
	}
	catalog := ctx.Catalog
	if len(parts) > 2 {
		catalog = parts[len(parts)-3]
	}
	return CatalogSchemaTableName{Catalog: catalog, Schema: schema, Table: obj}, nil
}

// AnalyzeFrom builds a Scope for a FROM relation. Mirrors Utils.analyzeFrom.
func AnalyzeFrom(wrenMDL *mdl.WrenMDL, ctx *base.SessionContext, node ast.Relation, parent *Scope) *Scope {
	scopeAnalysis := AnalyzeScope(wrenMDL, node, ctx)
	used := scopeAnalysis.UsedWrenObjects()
	var fields []*Field
	for _, model := range sortedModels(wrenMDL) {
		if !usedContains(used, model.Name) {
			continue
		}
		for i := range model.Columns {
			fields = append(fields, toField(wrenMDL, model.Name, &model.Columns[i], used))
		}
	}
	// metrics: 同理遍历 dimension+measure（P3a 语料无 metric 引用，但保留以对齐 Java）
	// ...
	return ScopeBuilderWithParent(parent).RelationType(NewRelationType(fields)).Build()
}
```

注：`sortedModels` 对 `wrenMDL.ListModels()` 按名排序（`ListModels` 是 map 遍历，无序 —— 必须排序，否则 `fields` 顺序不定，影响 `ResolveAnyField` 的「findAny」与最终 `collectedColumns` 顺序）。`toField` 对照 `Utils.java:308-322`。

- [ ] **Step 2: 写 `scope_analyzer.go`**

机械移植 `analyzer/ScopeAnalyzer.java`（86 行）。Java 是 `DefaultTraversalVisitor`（只下钻、不变换）。Go 用一个只读递归遍历：

```go
package analyzer

import (
	"strings"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// AnalyzeScope walks node and records Wren objects used in FROM.
// Mirrors ScopeAnalyzer.analyze.
func AnalyzeScope(wrenMDL *mdl.WrenMDL, node ast.Node, ctx *base.SessionContext) *ScopeAnalysis {
	sa := &ScopeAnalysis{aliasedMap: map[ast.NodeRef]string{}}
	(&scopeVisitor{wrenMDL: wrenMDL, analysis: sa, ctx: ctx}).visit(node)
	return sa
}

type scopeVisitor struct {
	wrenMDL  *mdl.WrenMDL
	analysis *ScopeAnalysis
	ctx      *base.SessionContext
}

func (v *scopeVisitor) visit(node ast.Node) {
	switch n := node.(type) {
	case *ast.Table:
		if v.isBelongToWren(n.Name) { // ScopeAnalyzer.visitTable
			v.analysis.AddUsedWrenObject(n)
		}
		return
	case *ast.TableSubquery:
		return // ScopeAnalyzer.visitTableSubquery: do not descend
	case *ast.AliasedRelation:
		v.analysis.AddAliasedNode(n.Relation, n.Alias.Value) // visitAliasedRelation
		v.visit(n.Relation)
		return
	}
	for _, c := range node.GetChildren() {
		v.visit(c)
	}
}
```

`isBelongToWren` 对照 `ScopeAnalyzer.java:76-84`：`toCatalogSchemaTableName` 后比对 catalog/schema，并检查表名命中 model/metric。

- [ ] **Step 3: 编译**

Run: `go build ./internal/rewrite/analyzer/...`
Expected: 通过。

- [ ] **Step 4: Commit**

```bash
git add internal/rewrite/analyzer/utils.go internal/rewrite/analyzer/scope_analyzer.go
git commit -m "feat(p3a): add ScopeAnalyzer + analyzer utils (slice 2)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 9：`ExpressionAnalyzer`

**Files:**
- Create: `internal/rewrite/analyzer/expression_analysis.go`
- Create: `internal/rewrite/analyzer/expression_analyzer.go`

- [ ] **Step 1: 写 `expression_analysis.go`**

机械移植 `analyzer/ExpressionAnalysis.java`（62 行）：`referencedFields map[ast.NodeRef]*Field`、`collectedFields []*Field`、`predicates []*ast.ComparisonExpression`、`requireRelation bool`。

- [ ] **Step 2: 写 `expression_analyzer.go`**

机械移植 `analyzer/ExpressionAnalyzer.java`（190 行）。Java 是 `DefaultTraversalVisitor`；Go 用只读递归。逐方法对照：

- `visitDereferenceExpression`（`:77-93`）：取 `getQualifiedName`，在 `scope.RelationType().Fields()` 里找能 `CanResolve` 的字段记入 `referenceFields`；不是 QN 时下钻 base。
- `visitIdentifier`（`:95-104`）：单段 QN 同理。
- `visitSubscriptExpression`（`:106-115`）。
- `visitComparisonExpression`（`:117-124`）：下钻左右，再 `predicates.add(node)`。
- `visitFunctionCall`（`:126-138`）：**`count()` 无参时 `requireRelation = true` 并 return**（这是 `count(*)` 的来源 —— P2 后 `count(*)` 解析成何种节点须确认：trino 把 `count(*)` 解析为 `FunctionCall` name=count、arguments 空。Go P2 formatter 对 count(*) 有特例，对应 parser 应产出 `FunctionCall{Name: count, Arguments: nil}`。按此判断 `len(Arguments)==0 && name=="count"`）。
- `visitSubqueryExpression`（`:140-145`）：递归调 `StatementAnalyzer.Analyze`（前向引用任务 10 —— 同包，编译期可见）。
- `visitWindowOperation`/`analyzeWindow`（`:147-164`）。

```go
// Analyze runs the expression visitor. Mirrors ExpressionAnalyzer.analyze.
func AnalyzeExpression(scope *Scope, expr ast.Expression, ctx *base.SessionContext, wrenMDL *mdl.WrenMDL, analysis *Analysis) *ExpressionAnalysis {
	v := &exprVisitor{scope: scope, ctx: ctx, wrenMDL: wrenMDL, analysis: analysis,
		referenceFields: map[ast.NodeRef]*Field{}}
	v.process(expr)
	return newExpressionAnalysis(v.referenceFields, v.predicates, v.requireRelation)
}
```

- [ ] **Step 3: 编译**

Run: `go build ./internal/rewrite/analyzer/...`
Expected: 通过（`StatementAnalyzer` 在同包，任务 10 补；若任务 10 未到，此步会因 `Analyze` 未定义失败 —— 把任务 9、10 视为一个编译单元，在任务 10 step 末统一编译；本步暂跳过编译，仅完成代码）。

- [ ] **Step 4: 完成代码，不单独提交**（与任务 10 合并提交）

## 任务 10：`StatementAnalyzer`

**Files:**
- Create: `internal/rewrite/analyzer/statement_analyzer.go`

- [ ] **Step 1: 写 `statement_analyzer.go` —— 顶层 `Analyze`**

机械移植 `analyzer/StatementAnalyzer.java`（637 行）。顶层 `analyze`（`:84-125`）：跑 Visitor，再从 `analysis.Tables()` 反查命中的 model/metric/cumulativeMetric/view 加入 `analysis`。

```go
// Analyze runs the statement analyzer. Mirrors StatementAnalyzer.analyze.
func Analyze(analysis *Analysis, statement ast.Statement, ctx *base.SessionContext, wrenMDL *mdl.WrenMDL) (*Scope, error) {
	v := &stmtVisitor{ctx: ctx, analysis: analysis, wrenMDL: wrenMDL}
	queryScope, err := v.process(statement, nil)
	if err != nil {
		return nil, err
	}
	// 把直接出现在 SQL 里的 model 加入 analysis（catalog/schema 命中）
	var models []*dto.Model
	for _, model := range wrenMDL.ListModels() {
		for _, t := range analysis.Tables() {
			if t.Catalog == wrenMDL.Catalog() && t.Schema == wrenMDL.Schema() && t.Table == model.Name {
				models = append(models, model)
				break
			}
		}
	}
	analysis.AddModels(models) // AddModels 内部按名排序
	// metrics / cumulativeMetrics / views 同理（P3a 语料无引用，但保留对齐）
	return queryScope, nil
}
```

- [ ] **Step 2: 写 Visitor —— 结构遍历各 `visitX`**

`stmtVisitor` 对应 Java 内部 `Visitor extends AstVisitor<Scope, Optional<Scope>>`。`process(node, scope)` 按节点类型分派；未知节点 → 返回 error（对应 `visitNode` 抛 `IllegalStateException`，设计 §7「不静默吞错」）。逐方法机械移植：

- `visitTable`（`:156-202`）—— **核心**。先查 WITH-CTE 同名（`scope.GetNamedQuery`）：命中则当 CTE 引用、建 CTE scope、**不**登记为表。否则 `toCatalogSchemaTableName` → `analysis.AddTable` → 若 catalog+schema 命中 MDL 则 `analysis.AddSourceNodeName(node, QualifiedName.of(tableName))` 并 `collectFieldFromMDL`。
- `visitQuery`（`:325-331`）、`visitQuerySpecification`（`:333-351`）、`analyzeFrom`/`analyzeSelect`/`analyzeWhere`/`analyzeWindowSpecification`（`:353-435`）。
- `analyzeSelectSingleColumn`（`:389-411`）—— 含 `requireRelation` → `addRequiredSourceNode` 的下钻；`requireRelation` 但无 source node 时返回 error（对应 q1 的 oracle-error；Go 端 q1 因 golden 是 `.error` 被 difftest 直接跳过，不会跑到这里 —— 但其它含 `count(*)` 的查询 source node 存在、不报错）。
- `visitJoin`（`:517-545`）、`visitAliasedRelation`（`:547-568`）、`visitTableSubquery`（`:570-576`）、`analyzeWith`（`:578-599`）、`visitSetOperation`（`:490-515`）、`visitValues`/`visitUnnest`/`visitFunctionRelation`/`visitPathRelation`（`:437-488`）。
- `createScopeForCommonTableExpression`/`createScopeForQuery`/`collectFieldFromMDL`/`createAndAssignScope`/`analyzeExpression`（`:204-323`、`:606-635`）。

每个方法逐行对照 Java；控制流一致。`AstVisitor` 的 `process` 用 `switch n := node.(type)` 分派。

- [ ] **Step 3: 编译任务 9 + 10**

Run: `go build ./internal/rewrite/analyzer/...`
Expected: 通过。

- [ ] **Step 4: Commit**

```bash
git add internal/rewrite/analyzer/expression_analysis.go internal/rewrite/analyzer/expression_analyzer.go internal/rewrite/analyzer/statement_analyzer.go
git commit -m "feat(p3a): add ExpressionAnalyzer + StatementAnalyzer (slice 2)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 11：分析器集成测试

**Files:**
- Test: `internal/rewrite/analyzer/analyzer_test.go`

- [ ] **Step 1: 写测试 —— 给定 query + MDL，Analysis 正确识别**

用 `cases/tpch/mdl.json`（相对测试文件路径 `../../../testdata/difftest/cases/tpch/mdl.json`）。

```go
package analyzer

import (
	"os"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func loadTPCH(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	raw, err := os.ReadFile("../../../testdata/difftest/cases/tpch/mdl.json")
	if err != nil {
		t.Fatalf("read mdl: %v", err)
	}
	m, err := mdl.WrenMDLFromJSON(string(raw))
	if err != nil {
		t.Fatalf("parse mdl: %v", err)
	}
	return m
}

func TestAnalyze_IdentifiesModels(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT partkey, name FROM Part")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := a.Models(); len(got) != 1 || got[0].Name != "Part" {
		t.Fatalf("Models() = %v, want [Part]", got)
	}
}

func TestAnalyze_RawTableNotModel(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT l_orderkey FROM lineitem") // 小写裸表
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(a.Models()) != 0 {
		t.Fatalf("Models() = %v, want []", a.Models())
	}
}

func TestAnalyze_WithCTENotModel(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	// CTE 名恰为模型名 Part —— 不应被识别为模型
	stmt, _ := parser.ParseSQL(`WITH Part AS (SELECT 1 x) SELECT x FROM Part`)
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(a.Models()) != 0 {
		t.Fatalf("CTE shadowing model: Models() = %v, want []", a.Models())
	}
}
```

- [ ] **Step 2: 运行**

Run: `go test ./internal/rewrite/analyzer/... -v`
Expected: 三个测试 PASS。`TestAnalyze_WithCTENotModel` 验证 `visitTable` 的 WITH-CTE 优先级（风险点：CTE 名遮蔽模型名）。

- [ ] **Step 3: Commit**

```bash
git add internal/rewrite/analyzer/analyzer_test.go
git commit -m "test(p3a): analyzer identifies models/raw-tables/CTEs (slice 2)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 3 — 关系分析

目标：解析计算列表达式（如 `customer.nation.name`）里的关系链，并能据此重写表达式。

## 任务 12：`ExpressionRelationship*` + MDL 关系工具

**Files:**
- Create: `internal/rewrite/analyzer/relationship_column_info.go`
- Create: `internal/rewrite/analyzer/expression_relationship_info.go`
- Create: `internal/rewrite/analyzer/expression_relationship_analyzer.go`
- Modify: `internal/dto/relationship.go`（加 `ReverseRelationship` helper）
- Modify: `internal/mdl/wren_mdl.go`（加 `GetRelationshipColumn`）

- [ ] **Step 1: `internal/dto/relationship.go` 加反转 helper**

Java `Relationship.reverse`（`Relationship.java:61-71`）：反转 `models` 列表、反转 `joinType`、`isReverse=true`。Go 已有 `ReverseJoinType`；补一个：

```go
// ReverseRelationship returns r with its models and join type reversed.
// Mirrors Java Relationship.reverse.
func ReverseRelationship(r *Relationship) *Relationship {
	models := make([]string, len(r.Models))
	for i, m := range r.Models {
		models[len(r.Models)-1-i] = m
	}
	return &Relationship{
		Name:             r.Name,
		Models:           models,
		JoinType:         ReverseJoinType(r.JoinType),
		Condition:        r.Condition,
		ManySideSortKeys: r.ManySideSortKeys,
		IsReverse:        true,
	}
}

// IsToOne / IsToMany classify a join type. Mirror JoinType.isToOne/isToMany.
func IsToOne(j JoinType) bool  { return j == JoinTypeOneToOne || j == JoinTypeManyToOne }
func IsToMany(j JoinType) bool { return j == JoinTypeOneToMany || j == JoinTypeManyToMany }
```

- [ ] **Step 2: `internal/mdl/wren_mdl.go` 加 `GetRelationshipColumn`**

对应 Java `WrenMDL.getRelationshipColumn(Model, String)` —— 在 model 的列里找「名字匹配且 relationship 非空」的列。

```go
// GetRelationshipColumn returns model's column named columnName if it is a
// relationship column. Mirrors Java WrenMDL.getRelationshipColumn.
func GetRelationshipColumn(model *dto.Model, columnName string) (*dto.Column, bool) {
	for i := range model.Columns {
		c := &model.Columns[i]
		if c.Name == columnName && c.Relationship != "" {
			return c, true
		}
	}
	return nil, false
}
```

- [ ] **Step 3: `relationship_column_info.go`**

机械移植 `analyzer/RelationshipColumnInfo.java`（66 行）。`reverseIfNeeded`：若 `relationship.Models[1] == column.Type` 则原样，否则 `dto.ReverseRelationship`。

```go
package analyzer

import (
	"github.com/wren-engine/wren/internal/dto"
)

// RelationshipColumnInfo pairs a relationship column with its normalized
// relationship. Mirrors analyzer.RelationshipColumnInfo.
type RelationshipColumnInfo struct {
	column                 *dto.Column
	model                  *dto.Model
	normalizedRelationship *dto.Relationship
}

func NewRelationshipColumnInfo(model *dto.Model, column *dto.Column, rel *dto.Relationship) *RelationshipColumnInfo {
	norm := rel
	if rel.Models[1] != column.Type {
		norm = dto.ReverseRelationship(rel)
	}
	return &RelationshipColumnInfo{column: column, model: model, normalizedRelationship: norm}
}

func (r *RelationshipColumnInfo) NormalizedRelationship() *dto.Relationship { return r.normalizedRelationship }
```

- [ ] **Step 4: `expression_relationship_info.go`**

机械移植 `analyzer/ExpressionRelationshipInfo.java`（118 行）。字段：`qualifiedName ast.QualifiedName`、`relationshipParts []string`、`remainingParts []string`、`relationships []*dto.Relationship`（来自 `relationshipColumnInfos` 的 `NormalizedRelationship`）、`baseModelRelationship *dto.Relationship`、`relationshipColumnInfos []*RelationshipColumnInfo`。构造时校验 `len(relationshipParts)+len(remainingParts) == len(qualifiedName.Parts)`。访问器 `QualifiedName()`/`RemainingParts()`/`Relationships()`。

- [ ] **Step 5: `expression_relationship_analyzer.go`**

机械移植 `analyzer/ExpressionRelationshipAnalyzer.java`（181 行）。`GetRelationships(expr, mdl, model)`（含 to-many）与 `GetToOneRelationships`（只 to-one、违例报错）；核心 `createRelationshipInfo`（`:112-141`）逐段沿关系链走 model。Go 用只读递归遍历表达式找 `DereferenceExpression`：

```go
// GetRelationships collects to-1 and to-N relationships used in expr.
// Mirrors ExpressionRelationshipAnalyzer.getRelationships.
func GetRelationships(expr ast.Expression, wrenMDL *mdl.WrenMDL, model *dto.Model) ([]*ExpressionRelationshipInfo, error) {
	c := &relationshipCollector{wrenMDL: wrenMDL, model: model, allowToMany: true}
	if err := c.process(expr); err != nil {
		return nil, err
	}
	return c.infos, nil
}
```

`process` 递归：遇 `*ast.DereferenceExpression` 且能取 `getQualifiedName` 时调 `createRelationshipInfo`，命中则收集；其它节点下钻 `GetChildren`。`createRelationshipInfo` 逐行对照 Java：从 base model 起，对 `qualifiedName.Parts` 每段查 `mdl.GetRelationshipColumn`，未命中且 `i>0` 时 `buildExpressionRelationshipInfo`，命中则推进到 `getNextModel`（`mdl.GetModel(column.Type)`）并 `checkForCycle`。

- [ ] **Step 6: 编译**

Run: `go build ./...`
Expected: 通过。

- [ ] **Step 7: Commit**

```bash
git add internal/dto/relationship.go internal/mdl/wren_mdl.go internal/rewrite/analyzer/relationship_column_info.go internal/rewrite/analyzer/expression_relationship_info.go internal/rewrite/analyzer/expression_relationship_analyzer.go
git commit -m "feat(p3a): add expression relationship analyzer (slice 3)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 13：`RelationshipRewriter`

**Files:**
- Create: `internal/rewrite/relationship_rewriter.go`
- Test: `internal/rewrite/relationship_rewriter_test.go`

- [ ] **Step 1: 写 `relationship_rewriter.go`**

机械移植 `RelationshipRewriter.java`（94 行）。Java 是 `BaseRewriter`，覆盖 `visitDereferenceExpression` —— 整个 dereference 的 QN 命中 replacements 则替换。Go 用任务 5 的钩子：

```go
package rewrite

import (
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// rewriteRelationship replaces relationship dereferences in expr with the
// dereference expression that points at the joined model. Mirrors
// RelationshipRewriter.rewrite.
func rewriteRelationship(infos []*analyzer.ExpressionRelationshipInfo, expr ast.Expression) ast.Expression {
	replacements := map[string]*ast.DereferenceExpression{}
	for _, info := range infos {
		replacements[info.QualifiedName().String()] = toDereferenceExpression(info)
	}
	return RewriteNode(expr, func(n ast.Node) (ast.Node, bool) {
		d, ok := n.(*ast.DereferenceExpression)
		if !ok {
			return nil, false // descend
		}
		if qn := ast.GetQualifiedName(d); qn != nil {
			if r, found := replacements[qn.String()]; found {
				return r, true
			}
		}
		return d, true // matched a dereference but no replacement: stop, unchanged
	}).(ast.Expression)
}

// toDereferenceExpression builds "<lastModel>.<remainingParts...>" as a
// delimited dereference chain. Mirrors RelationshipRewriter.toDereferenceExpression.
func toDereferenceExpression(info *analyzer.ExpressionRelationshipInfo) *ast.DereferenceExpression {
	rels := info.Relationships()
	base := rels[len(rels)-1].Models[1]
	parts := []ast.Identifier{{Value: base, Delimited: true}}
	for _, p := range info.RemainingParts() {
		parts = append(parts, ast.Identifier{Value: p, Delimited: true})
	}
	return dereferenceFrom(parts)
}
```

`dereferenceFrom([]ast.Identifier) *ast.DereferenceExpression` —— 对应 trino `DereferenceExpression.from(QualifiedName)`：把标识符序列折成左嵌套 dereference 链。注意 ≥2 段才是 `*DereferenceExpression`；这里 `toDereferenceExpression` 一定 ≥2 段（base + remaining 非空）。把 `dereferenceFrom` 放 `utils.go`（任务 20 的 `Rewriter` 也用），返回 `ast.Expression`，`toDereferenceExpression` 处断言为 `*ast.DereferenceExpression`。

- [ ] **Step 2: 写测试 —— 计算列 `customer.nation.name` 解析关系链**

测试文件 `relationship_rewriter_test.go`（package `rewrite`）顶部定义共享 helper `loadTPCHForRewrite` —— 任务 18 的 `model_sql_render_test.go` 也用它（同包共享）。

```go
package rewrite

import (
	"os"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// loadTPCHForRewrite loads the TPC-H MDL for rewrite-package tests.
// internal/rewrite is two levels below the repo root.
func loadTPCHForRewrite(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/difftest/cases/tpch/mdl.json")
	if err != nil {
		t.Fatalf("read mdl: %v", err)
	}
	m, err := mdl.WrenMDLFromJSON(string(raw))
	if err != nil {
		t.Fatalf("parse mdl: %v", err)
	}
	return m
}

func TestGetRelationships_CalculatedField(t *testing.T) {
	wrenMDL := loadTPCHForRewrite(t)
	orders, _ := wrenMDL.GetModel("Orders")
	expr, err := parser.ParseExpression("customer.nation.name")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	infos, err := analyzer.GetRelationships(expr, wrenMDL, orders)
	if err != nil {
		t.Fatalf("GetRelationships: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("got %d infos, want 1", len(infos))
	}
	rels := infos[0].Relationships()
	if len(rels) != 2 {
		t.Fatalf("got %d relationships, want 2 (OrdersCustomer, CustomerNation)", len(rels))
	}
	// remainingParts 应为 ["name"]
	if got := infos[0].RemainingParts(); len(got) != 1 || got[0] != "name" {
		t.Fatalf("RemainingParts = %v, want [name]", got)
	}
}
```

- [ ] **Step 3: 运行**

Run: `go test ./internal/rewrite/ -run TestGetRelationships -v`
Expected: PASS。

- [ ] **Step 4: Commit**

```bash
git add internal/rewrite/relationship_rewriter.go internal/rewrite/relationship_rewriter_test.go internal/rewrite/utils.go
git commit -m "feat(p3a): add RelationshipRewriter + relationship-chain test (slice 3)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 4 — SqlRender

目标：把单个模型渲染成 CTE 查询 AST，与 Java 一致。

## 任务 14：`QueryDescriptor` + `DummyInfo` + `WithRewriter`

**Files:**
- Create: `internal/rewrite/query_descriptor.go`
- Create: `internal/rewrite/dummy_info.go`
- Create: `internal/rewrite/with_rewriter.go`

- [ ] **Step 1: 写 `query_descriptor.go`**

机械移植 `QueryDescriptor.java`（61 行）。接口 + `QueryDescriptorOf(name, analyzedMDL, ctx)` 工厂（P3a 只需 model 一支；metric/cumulative/view 返回 error「P3b/P3c 未实现」，DateSpine 同）。

```go
package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// QueryDescriptor describes one CTE to be generated. Mirrors Java QueryDescriptor.
type QueryDescriptor interface {
	Name() string
	RequiredObjects() []string
	Query() *ast.Query
}

// QueryDescriptorOf builds a descriptor for the named object.
// Mirrors QueryDescriptor.of (P3a: models only).
func QueryDescriptorOf(name string, analyzedMDL *mdl.AnalyzedMDL, ctx *base.SessionContext) (QueryDescriptor, error) {
	wrenMDL := analyzedMDL.WrenMDL()
	if model, ok := wrenMDL.GetModel(name); ok {
		return relationInfoOfModel(model, wrenMDL)
	}
	if _, ok := wrenMDL.GetMetric(name); ok {
		return nil, fmt.Errorf("metric %q requires P3b", name)
	}
	if _, ok := wrenMDL.GetCumulativeMetric(name); ok {
		return nil, fmt.Errorf("cumulative metric %q requires P3b", name)
	}
	if _, ok := wrenMDL.GetView(name); ok {
		return nil, fmt.Errorf("view %q requires P3c", name)
	}
	return nil, fmt.Errorf("%s not found in wren mdl", name)
}
```

`relationInfoOfModel` 在任务 17 定义（同包，编译期可见）。

- [ ] **Step 2: 写 `dummy_info.go`**

机械移植 `DummyInfo.java`（50 行）：`Query()` 返回 `parseQuery("select 1")`。P3a 非动态路径其实用不到 `DummyInfo`（仅动态路径 `visitedTables` 用），但设计 §4 列出，且实现极小 —— 一并补齐，供 P3b/动态路径复用。

```go
package rewrite

import "github.com/wren-engine/wren/internal/parser/ast"

// DummyInfo is a QueryDescriptor whose CTE body is "SELECT 1".
// Mirrors Java DummyInfo.
type DummyInfo struct{ name string }

func NewDummyInfo(name string) *DummyInfo      { return &DummyInfo{name: name} }
func (d *DummyInfo) Name() string               { return d.name }
func (d *DummyInfo) RequiredObjects() []string   { return nil }
func (d *DummyInfo) Query() *ast.Query {
	q, err := parseQuery("select 1")
	if err != nil {
		panic(err) // "select 1" is always valid
	}
	return q
}
```

- [ ] **Step 3: 写 `with_rewriter.go`**

机械移植 `WithRewriter.java`（62 行）。Java `WithRewriter extends BaseRewriter` 覆盖 `visitQuery` 且**不下钻** —— 故只影响根 query。Go 直接实现为 `applyWith`（注释说明此等价）。**风险 #4/#5**：模型 CTE 必须排在用户 WITH 之前；必须保留 `q.Body` 的对象身份（不能 `RewriteNode` 重建 body，否则 `Rewriter` 用 `analysis` 按身份查 Table 会全部 miss）。

```go
package rewrite

import "github.com/wren-engine/wren/internal/parser/ast"

// getWithQuery wraps a descriptor as a WITH query with a delimited CTE name.
// Mirrors WithRewriter.getWithQuery (Java new Identifier(name, true)). Risk #6.
func getWithQuery(d QueryDescriptor) ast.WithQuery {
	return ast.WithQuery{
		Name:  &ast.Identifier{Value: d.Name(), Delimited: true},
		Query: d.Query(),
	}
}

// applyWith prepends withQueries to root's WITH clause. Model CTEs must come
// first (Java Stream.concat(withQueries, with.getQueries())). Mirrors
// WithRewriter.visitQuery; only the root Query is affected.
func applyWith(root ast.Statement, withQueries []ast.WithQuery) ast.Statement {
	q, ok := root.(*ast.Query)
	if !ok {
		return root
	}
	out := *q // body identity preserved — do NOT RewriteNode the body (risk #5)
	switch {
	case q.With != nil:
		merged := append(append([]ast.WithQuery{}, withQueries...), q.With.Queries...)
		out.With = &ast.With{Recursive: q.With.Recursive, Queries: merged}
	case len(withQueries) > 0:
		out.With = &ast.With{Recursive: false, Queries: withQueries}
	default:
		out.With = nil
	}
	return &out
}
```

- [ ] **Step 4: 编译**（`relationInfoOfModel` 未定义 → 暂跳过编译，待任务 17）

完成代码，不单独提交（与任务 15–17 合并）。

## 任务 15：`RelationableSqlRender` 基座

**Files:**
- Create: `internal/rewrite/relationable_sql_render.go`

- [ ] **Step 1: 写 `relationable_sql_render.go`**

机械移植 `RelationableSqlRender.java`（147 行）的共享状态与两个 info 类型。P3a 只有 `ModelSqlRender`（`MetricSqlRender` 是 P3b），故 Go 把共享字段做成一个被 `modelSqlRender` 内嵌的结构体。**风险 #9**：`requiredObjects` 来自原始 `model.BaseObject` 字段（refSql 模型为空），**不要**调 `GetBaseObject()`。**风险 #2**：`calculatedScopeSelectItems` 是 Java `LinkedHashMap`（保插入序）—— Go 用「keys 切片 + map」的有序 map。

```go
package rewrite

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// orderedMap preserves insertion order. Replaces Java LinkedHashMap (risk #2).
type orderedMap struct {
	keys []string
	m    map[string]string
}

func newOrderedMap() *orderedMap { return &orderedMap{m: map[string]string{}} }
func (o *orderedMap) put(k, v string) {
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}
func (o *orderedMap) entries() []struct{ K, V string } {
	out := make([]struct{ K, V string }, len(o.keys))
	for i, k := range o.keys {
		out[i] = struct{ K, V string }{k, o.m[k]}
	}
	return out
}

// calculatedFieldRelationshipInfo mirrors RelationableSqlRender.CalculatedFieldRelationshipInfo.
type calculatedFieldRelationshipInfo struct {
	column                 *dto.Column
	expressionRelationship []*analyzer.ExpressionRelationshipInfo
	isAggregated           bool
}

func newCalculatedFieldRelationshipInfo(column *dto.Column, infos []*analyzer.ExpressionRelationshipInfo) *calculatedFieldRelationshipInfo {
	agg := false
	for _, info := range infos {
		for _, rel := range info.Relationships() {
			if dto.IsToMany(rel.JoinType) {
				agg = true
			}
		}
	}
	return &calculatedFieldRelationshipInfo{column: column, expressionRelationship: infos, isAggregated: agg}
}

func (c *calculatedFieldRelationshipInfo) alias() string { return c.column.Name }

// subQueryJoinInfo mirrors RelationableSqlRender.SubQueryJoinInfo.
type subQueryJoinInfo struct {
	sql           string
	subqueryAlias string
	joinCriteria  string
}

// relationableSqlRender holds the shared render state. Mirrors the abstract
// RelationableSqlRender fields; embedded by modelSqlRender.
type relationableSqlRender struct {
	relationable *dto.Model
	mdl          *mdl.WrenMDL
	refSql       string
	requiredObjects map[string]bool
	selectItems  []string
	calculatedRequiredRelationshipInfos []*calculatedFieldRelationshipInfo
	calculatedScopeSelectItems *orderedMap
}

// getRelationableAlias mirrors RelationableSqlRender.getRelationableAlias.
func getRelationableAlias(baseModelName string) string { return baseModelName + "_relationsub" }
```

注：`requiredObjects` 用 `map[string]bool` 做集合；任务 16 渲染完后转成**按名排序**的 `[]string` 交给 `RelationInfo`（风险 #1：descriptor 的 `RequiredObjects()` 须确定性）。

- [ ] **Step 2: 完成代码，不单独提交**（与任务 16–17 合并）

## 任务 16：`ModelSqlRender`

**Files:**
- Create: `internal/rewrite/model_sql_render.go`

- [ ] **Step 1: 写 `model_sql_render.go`**

机械移植 `ModelSqlRender.java`（297 行）。**风险 #3**：所有 `format()` 模板逐字复刻（结果会被 `parseQuery` 再解析，token 须一致）。逐方法对照：

- `initRefSql`（`:62-79`）：`model.RefSql != "" → "(" + refSql + ")"`；`else model.BaseObject != "" → "(SELECT * FROM \"" + baseObject + "\")"`；`else tableReference → tableReference.toQualifiedName()`。**用原始字段**（风险 #9）。
- `getQuerySql`（`:92-96`）：`fmt.Sprintf("SELECT %s FROM %s", selectItemsSql, tableJoinsSql)`。
- `getSelectItemsExpression`（`:106-113`）：有 relationalBase → `"%s"."%s" AS "%s"`（base, col, col）；否则用 `relationable.Name`。
- `getModelSubQuerySelectItemsExpression`（`:98-104`）：`calculatedScopeSelectItems` 按**插入序**（`orderedMap.entries()`）拼 `"%s AS \"%s\""`，`", "` 连接。
- `getBaseModelSql`（`:288-296`）：非计算、非关系列 → `"%s AS \"%s\"" (sqlExpression, name)`，`", "` 连接 → `SELECT %s FROM %s AS "%s"`（refSql, modelName）。`sqlExpression` = `column.GetExpression()`（Go `dto.Column.GetExpression()` 已有：expr 或带引号列名）。
- `collectRelationship`（`:115-155`）：解析列表达式 → `GetRelationships`；计算列且关系非空 → 收集 `calculatedFieldRelationshipInfo`，把关系另一端 model 名加入 `requiredObjects`，按 `isAggregated` 决定 select item 的 relationalBase；无关系的计算列 → 普通处理；非计算列 → 普通处理（`calculatedScopeSelectItems.put(name, "\"%s\".\"%s\"")`）。
- `render(Model)`（`:213-250`）：先收普通列、再对「非关系且有表达式」列调 `collectRelationship`，拼 `calculatedSubQuery` 文本块，必要时 append `getCalculatedSubQuery` 的 LEFT JOIN，最后 `parseQuery(getQuerySql(...))` 返回 `RelationInfo`。
- `getToOneRelationshipsQuery`（`:167-211`）/`getToManyRelationshipsQuery`（`:255-286`）：关系 join 子查询，用 `rewriteRelationship`（任务 13）重写计算列表达式、用 `qualifiedConditionString`（下述）渲染 join 条件。

**`qualifiedConditionString(condition string) (string, error)`** —— 放 `utils.go`。对应 Java `Relationship.qualifiedCondition`（`Relationship.java:105-131`）：`parseExpression(condition)` → 把所有**非 delimited** 的 `Identifier` 标记为 delimited（用任务 5 的 `RewriteNode` 钩子）→ `formatter.FormatSQL`/表达式格式化为字符串。这是 `relationship.getQualifiedCondition()` 的 Go 等价（Java 在 `dto.Relationship` 构造时算；Go 因 `dto` 是纯数据、不引 parser，移到 `rewrite` 包算）。

`calculatedSubQuery` 模板（Java 文本块 `ModelSqlRender.java:229-235`，注意文本块尾随换行 —— 但因结果会被 `parseQuery` 再解析，空白不影响 AST，token 一致即可）：

```go
calculatedSubQuery := fmt.Sprintf(
	`(SELECT %s FROM (%s) AS "%s") AS "%s"`+"\n",
	calculatedFieldsWithoutRelationship, baseModelSql, baseModel.Name, baseModel.Name)
```

`render` 末尾：

```go
querySQL := r.getQuerySql(strings.Join(r.selectItems, ", "), tableJoinsSql)
query, err := parseQuery(querySQL)
if err != nil {
	return nil, fmt.Errorf("render model %q: %w", baseModel.Name, err)
}
return newRelationInfo(baseModel, sortedKeys(r.requiredObjects), query), nil
```

- [ ] **Step 2: 完成代码，不单独提交**（与任务 17 合并）

## 任务 17：`RelationInfo`

**Files:**
- Create: `internal/rewrite/relation_info.go`

- [ ] **Step 1: 写 `relation_info.go`**

机械移植 `RelationInfo.java`（108 行）—— `QueryDescriptor` 的实现。`RelationInfo.get(relationable, mdl)` 2 参版（非动态路径用），P3a 只处理 `Model`。

```go
package rewrite

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// RelationInfo is a QueryDescriptor backed by a rendered model query.
// Mirrors Java RelationInfo.
type RelationInfo struct {
	relationable    *dto.Model
	requiredObjects []string
	query           *ast.Query
}

func newRelationInfo(model *dto.Model, requiredObjects []string, query *ast.Query) *RelationInfo {
	return &RelationInfo{relationable: model, requiredObjects: requiredObjects, query: query}
}

func (r *RelationInfo) Name() string              { return r.relationable.Name }
func (r *RelationInfo) RequiredObjects() []string  { return r.requiredObjects }
func (r *RelationInfo) Query() *ast.Query          { return r.query }

// relationInfoOfModel renders a model into a RelationInfo. Mirrors
// RelationInfo.get(Relationable, WrenMDL) for the Model case.
func relationInfoOfModel(model *dto.Model, wrenMDL *mdl.WrenMDL) (*RelationInfo, error) {
	return newModelSqlRender(model, wrenMDL).render()
}
```

`newModelSqlRender` + `render() (*RelationInfo, error)` 在任务 16 的 `model_sql_render.go` 里。`render` 对应 `ModelSqlRender.render()`（`:81-90`）：空列模型 → `parseQuery(refSql)`；否则 `render(model)`。

- [ ] **Step 2: 编译切片 4 全部**

Run: `go build ./...`
Expected: 通过。

- [ ] **Step 3: Commit**

```bash
git add internal/rewrite/query_descriptor.go internal/rewrite/dummy_info.go internal/rewrite/with_rewriter.go internal/rewrite/relationable_sql_render.go internal/rewrite/model_sql_render.go internal/rewrite/relation_info.go internal/rewrite/utils.go
git commit -m "feat(p3a): add SqlRender — ModelSqlRender/RelationInfo/WithRewriter (slice 4)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 18：SqlRender 单测（含合成关系 MDL）

**Files:**
- Test: `internal/rewrite/model_sql_render_test.go`

说明：用真实 TPC-H MDL 验证干净模型（`Part`）的渲染；用**合成的小 MDL** 验证关系 join 渲染（`getToOneRelationshipsQuery`/`getToManyRelationshipsQuery`）—— 因 TPC-H 唯一的关系计算列 `Orders.nation_name` 被 Jinja 阻断，无法走端到端。

- [ ] **Step 1: 写 `Part` 渲染测试**

```go
func TestModelSqlRender_PlainModel(t *testing.T) {
	wrenMDL := loadTPCHForRewrite(t)
	part, _ := wrenMDL.GetModel("Part")
	info, err := relationInfoOfModel(part, wrenMDL)
	if err != nil {
		t.Fatalf("render Part: %v", err)
	}
	if len(info.RequiredObjects()) != 0 {
		t.Fatalf("Part RequiredObjects = %v, want []", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	// 关键 token 断言（formatter 输出的精确串以执行时实测为准；此处校验结构）：
	for _, want := range []string{`"Part"."partkey"`, `AS "partkey"`, `"Part"."name"`, `p_partkey`, `p_name`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered Part SQL missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 2: 写合成关系 MDL 的 to-one join 测试**

构造一个最小、无 Jinja 的 MDL：模型 `A`（含计算列 `b_name = b.name`、关系列 `b`）、模型 `B`（普通列 `name`）、关系 `AB`（`A` MANY_TO_ONE `B`）。渲染 `A`，断言输出含 `LEFT JOIN "B" ON`、`A_relationsub`、`requiredObjects` 含 `B`。

```go
func syntheticToOneMDL(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	manifest := dto.Manifest{
		Catalog: "c", Schema: "s",
		Models: []dto.Model{
			{Name: "A", RefSql: "select * from a", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "a_id"},
				{Name: "bkey", Type: "int4", Expression: "a_bkey"},
				{Name: "b", Type: "B", Relationship: "AB"},
				{Name: "b_name", Type: "varchar", IsCalculated: true, Expression: "b.name"},
			}},
			{Name: "B", RefSql: "select * from b", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "b_id"},
				{Name: "name", Type: "varchar", Expression: "b_name"},
			}},
		},
		Relationships: []dto.Relationship{
			{Name: "AB", Models: []string{"A", "B"}, JoinType: dto.JoinTypeManyToOne, Condition: "A.bkey = B.id"},
		},
	}
	return mdl.WrenMDLFromManifest(&manifest)
}

func TestModelSqlRender_ToOneRelationship(t *testing.T) {
	wrenMDL := syntheticToOneMDL(t)
	a, _ := wrenMDL.GetModel("A")
	info, err := relationInfoOfModel(a, wrenMDL)
	if err != nil {
		t.Fatalf("render A: %v", err)
	}
	if !contains(info.RequiredObjects(), "B") {
		t.Fatalf("RequiredObjects = %v, want to contain B", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{`LEFT JOIN`, `A_relationsub`, `"b_name"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered A SQL missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 3: 写合成 to-many MDL 的测试**

同理但关系 `AB` 用 `dto.JoinTypeOneToMany`，计算列 `b_count = count(b.id)`，断言输出含 `GROUP BY 1`。对照 `getToManyRelationshipsQuery`。

- [ ] **Step 4: 运行**

Run: `go test ./internal/rewrite/ -run TestModelSqlRender -v`
Expected: 三个测试 PASS。若 to-one/to-many 输出 token 与预期不符，对照 `ModelSqlRender.java` 的对应方法修正模板（风险 #3）。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/model_sql_render_test.go
git commit -m "test(p3a): SqlRender unit tests incl. synthetic relationship MDLs (slice 4)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 5 — WrenSqlRewrite 接线

目标：非动态路径全装配，引用模型的查询端到端转绿。

## 任务 19：CTE 拓扑排序

**Files:**
- Create: `internal/rewrite/graph.go`
- Test: `internal/rewrite/graph_test.go`

说明（**风险 #1，最高风险**）：Java 用 JGraphT `DirectedAcyclicGraph` 的拓扑迭代决定 WITH CTE 顺序。Go 必须确定性。本任务实现 Kahn 算法，**把 seed 顺序与平局规则收敛到单一函数 `topoTieBreak`**，初值为字典序；任务 21 用多模型 golden 逆向校验、不符只调此函数。

- [ ] **Step 1: 写 `graph.go`**

```go
package rewrite

import (
	"fmt"
	"sort"
)

// topoSort returns a topological order of vertices given edges (dependency ->
// dependent). The tie-break among ready vertices is topoTieBreak, isolated so
// it can be calibrated against Java's JGraphT iteration order (risk #1).
func topoSort(vertices []string, edges [][2]string) ([]string, error) {
	inDeg := map[string]int{}
	adj := map[string][]string{}
	for _, v := range vertices {
		inDeg[v] = 0
	}
	for _, e := range edges {
		from, to := e[0], e[1]
		adj[from] = append(adj[from], to)
		inDeg[to]++
	}
	var ready []string
	for v, d := range inDeg {
		if d == 0 {
			ready = append(ready, v)
		}
	}
	topoTieBreak(ready)
	var order []string
	for len(ready) > 0 {
		v := ready[0]
		ready = ready[1:]
		order = append(order, v)
		next := append([]string(nil), adj[v]...)
		sort.Strings(next) // deterministic edge processing
		for _, w := range next {
			inDeg[w]--
			if inDeg[w] == 0 {
				ready = append(ready, w)
				topoTieBreak(ready)
			}
		}
	}
	if len(order) != len(inDeg) {
		return nil, fmt.Errorf("found cycle in models")
	}
	return order, nil
}

// topoTieBreak orders ready vertices. CALIBRATION POINT for risk #1: the
// initial guess is lexical order; task 21 verifies against multi-model golden.
func topoTieBreak(ready []string) {
	sort.Strings(ready)
}
```

- [ ] **Step 2: 写 `graph_test.go`**

```go
func TestTopoSort_Linear(t *testing.T) {
	// B required by A: edge B->A. Order must be [B, A].
	order, err := topoSort([]string{"A", "B"}, [][2]string{{"B", "A"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "B" || order[1] != "A" {
		t.Fatalf("order = %v, want [B A]", order)
	}
}

func TestTopoSort_Cycle(t *testing.T) {
	_, err := topoSort([]string{"A", "B"}, [][2]string{{"A", "B"}, {"B", "A"}})
	if err == nil {
		t.Fatal("want cycle error, got nil")
	}
}

func TestTopoSort_TieBreakLexical(t *testing.T) {
	order, _ := topoSort([]string{"C", "A", "B"}, nil)
	if order[0] != "A" || order[1] != "B" || order[2] != "C" {
		t.Fatalf("order = %v, want [A B C]", order)
	}
}
```

- [ ] **Step 3: 运行**

Run: `go test ./internal/rewrite/ -run TestTopoSort -v`
Expected: PASS。

- [ ] **Step 4: Commit**

```bash
git add internal/rewrite/graph.go internal/rewrite/graph_test.go
git commit -m "feat(p3a): add deterministic CTE topological sort (slice 5)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 20：`WrenSqlRewrite` 全装配

**Files:**
- Modify: `internal/rewrite/wren_sql_rewrite.go`

- [ ] **Step 1: 实现非动态路径 `Apply`**

机械移植 `WrenSqlRewrite.java` 的 `apply`（`:78-143`，非动态分支 `:132-142`）+ 私有 `apply`（`:179-204`）+ `addSqlDescriptorToGraph`（`:206-232`）。

```go
package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"

	base "github.com/wren-engine/wren/internal/analyzer"
)

type WrenSqlRewrite struct{}

// Apply expands referenced Wren models into CTEs (non-dynamic-field path).
// Mirrors Java WrenSqlRewrite.apply.
func (r *WrenSqlRewrite) Apply(root ast.Statement, ctx *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	wrenMDL := analyzedMDL.WrenMDL()

	analysis := analyzer.NewAnalysis(root)
	if _, err := analyzer.Analyze(analysis, root, ctx, wrenMDL); err != nil {
		return nil, err
	}

	// non-dynamic path: only model descriptors (metrics/cumulative -> P3b)
	var allDescriptors []QueryDescriptor
	for _, model := range analysis.Models() { // already name-sorted (risk #1)
		info, err := relationInfoOfModel(model, wrenMDL)
		if err != nil {
			return nil, err
		}
		allDescriptors = append(allDescriptors, info)
	}
	if len(allDescriptors) == 0 {
		return root, nil // no model referenced: pass through
	}

	// build dependency graph, pulling required objects recursively
	descriptorMap := map[string]QueryDescriptor{}
	var vertices []string
	var edges [][2]string
	seen := map[string]bool{}
	var addToGraph func(d QueryDescriptor) error
	addToGraph = func(d QueryDescriptor) error {
		if !contains(vertices, d.Name()) {
			vertices = append(vertices, d.Name())
		}
		descriptorMap[d.Name()] = d
		for _, req := range d.RequiredObjects() { // RequiredObjects is name-sorted
			if !contains(vertices, req) {
				vertices = append(vertices, req)
			}
			edges = append(edges, [2]string{req, d.Name()})
			if seen[req] {
				continue
			}
			seen[req] = true
			reqDesc, err := QueryDescriptorOf(req, analyzedMDL, ctx)
			if err != nil {
				return err
			}
			if err := addToGraph(reqDesc); err != nil {
				return err
			}
		}
		return nil
	}
	for _, d := range allDescriptors {
		if err := addToGraph(d); err != nil {
			return nil, err
		}
	}

	order, err := topoSort(vertices, edges)
	if err != nil {
		return nil, err
	}

	var withQueries []ast.WithQuery
	for _, name := range order {
		d, ok := descriptorMap[name]
		if !ok {
			return nil, fmt.Errorf("%s not found in query descriptors", name)
		}
		withQueries = append(withQueries, getWithQuery(d))
	}

	rewriteWith := applyWith(root, withQueries)
	return rewriteModelTables(rewriteWith, wrenMDL, analysis).(ast.Statement), nil
}
```

- [ ] **Step 2: 实现 `Rewriter` 内部类 —— `rewriteModelTables`**

机械移植 `WrenSqlRewrite.Rewriter`（`:234-279`）。用任务 5 的钩子：`visitTable` 当 `analysis.SourceNodeName(node)` 命中时改写为 `Table(最后一段原始标识符)`；`visitDereferenceExpression` 剥 catalog/schema 前缀。

```go
// rewriteModelTables rewrites in-MDL table references to their CTE name and
// strips catalog/schema prefixes. Mirrors WrenSqlRewrite.Rewriter.
func rewriteModelTables(node ast.Node, wrenMDL *mdl.WrenMDL, analysis *analyzer.Analysis) ast.Node {
	return RewriteNode(node, func(n ast.Node) (ast.Node, bool) {
		switch x := n.(type) {
		case *ast.Table:
			if _, ok := analysis.SourceNodeName(x); ok {
				last := x.Name.OriginalParts[len(x.Name.OriginalParts)-1]
				return &ast.Table{Name: ast.QualifiedName{
					Parts:         []string{last.Value},
					OriginalParts: []ast.Identifier{last},
				}}, true
			}
			return x, true // a Table the analysis didn't flag: leave it
		case *ast.DereferenceExpression:
			return stripCatalogSchema(x, wrenMDL), true
		}
		return nil, false // descend everything else
	})
}

// stripCatalogSchema removes a leading catalog.schema (or schema) prefix from a
// dereference. Mirrors Rewriter.visitDereferenceExpression.
func stripCatalogSchema(d *ast.DereferenceExpression, wrenMDL *mdl.WrenMDL) ast.Expression {
	qn := ast.GetQualifiedName(d)
	if qn == nil || wrenMDL.Catalog() == "" || wrenMDL.Schema() == "" {
		return d
	}
	if hasPrefixParts(*qn, wrenMDL.Catalog(), wrenMDL.Schema()) {
		return dereferenceFrom(qn.OriginalParts[2:])
	}
	if hasPrefixParts(*qn, wrenMDL.Schema()) {
		return dereferenceFrom(qn.OriginalParts[1:])
	}
	return d
}
```

`hasPrefixParts(qn, prefix...)` 与 `dereferenceFrom([]ast.Identifier) ast.Expression`（≥2 段 → dereference 链；1 段 → `*Identifier`）放 `utils.go`。`contains([]string, string) bool` 也放 `utils.go`。

**风险 #5 复核**：`rewriteModelTables` 收到的是 `applyWith` 的结果 —— `applyWith` 保留了 query body 的对象身份，故 body 内的 `*ast.Table` 与 `analysis` 登记时是同一指针，`SourceNodeName` 按 `NodeRef` 身份能查中。`RewriteNode` 在把 Table 交给钩子时，Table 仍是原始对象（容器在其后才重建）—— 身份成立。

- [ ] **Step 3: 编译**

Run: `go build ./... && go vet ./internal/rewrite/...`
Expected: 通过、无输出。

- [ ] **Step 4: 运行既有单测**

Run: `go test ./internal/rewrite/...`
Expected: 切片 1–4 的全部单测仍 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/wren_sql_rewrite.go internal/rewrite/utils.go
git commit -m "feat(p3a): wire WrenSqlRewrite non-dynamic path (slice 5)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 21：端到端 —— 模型查询转绿

**Files:**
- Modify: `testdata/difftest/baseline.json`
- 可能 Modify: `internal/rewrite/graph.go`（仅当 CTE 顺序逆向校验不符时调 `topoTieBreak`）

- [ ] **Step 1: 跑差分测试，看模型查询状态**

Run: `make difftest`
Expected: 22 条标准查询无回归；`tpch/m_part`、`tpch/m_lineitem`、`tpch/m_lineitem_calc`、`tpch/m_nation`、`tpch/m_join` 由 `fail` 翻 `pass`（或仍 `fail`，进 Step 2 排查）；`tpch/m_orders` 仍 `go-error`/`fail`（Jinja 阻断，预期）。

- [ ] **Step 2: 逐条排查未转绿的模型查询**

对每个仍 `fail` 的 `m_*`，`make difftest` 的日志给出首个 token 差异。常见根因与对策：
- **CTE 顺序不符（风险 #1）**：`m_join` 引用 `Lineitem`+`Part` 两个模型，golden 的 `WITH` 子句 CTE 排列顺序若与 Go 不同 → 调 `topoTieBreak`（仅此一处）。先 `cat testdata/difftest/golden/tpch/m_join.sql` 看 Java 的 CTE 实际顺序，再令 `topoTieBreak` 匹配之。
- **模板 token 错（风险 #3）**：对照 `ModelSqlRender.java` 修 `model_sql_render.go` 的 `format` 串。
- **`go-error`**：读错误信息；若是 `Customer`/`Orders` 传递性 Jinja，属预期已知失败。

- [ ] **Step 3: 接受 baseline**

模型查询转绿后：

Run: `make difftest-accept`
`git diff testdata/difftest/baseline.json` 确认：`tpch/m_part`…`tpch/m_join` 变 `pass`；`tpch/m_orders` 保持已知失败；22 条标准查询无 `pass→fail` 回归。

- [ ] **Step 4: 运行全量测试**

Run: `make test`
Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add testdata/difftest/baseline.json internal/rewrite/graph.go
git commit -m "test(p3a): model-referencing queries pass differential test (slice 5)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 收尾

## 任务 22：终验与文档

**Files:**
- Create: `internal/rewrite/README.md`
- Modify: `internal/difftest/README.md`（补 P3a 说明）

- [ ] **Step 1: 写 `internal/rewrite/README.md`**

记录：包结构与 Java `wren-base/sqlrewrite` 的 1:1 对应表；P3a 实现非动态字段路径，动态路径/度量/视图/枚举待后续；CTE 顺序的确定性来源（`topoSort` + `topoTieBreak`，风险 #1）；模型语料 `cases/tpch/queries/m_*.sql` 的用途；`Orders`/`Customer` 因 Jinja 待 P6。

- [ ] **Step 2: 更新 `internal/difftest/README.md`**

在「失败分类」补：`m_orders` 等引用含 Jinja 列模型的查询在 P3a 阶段为已知失败，待 P6 的 Jinja 宏层。

- [ ] **Step 3: 终验全套**

Run（逐条须通过）：
```bash
go build ./...
go vet ./...
gofmt -l internal/rewrite/ internal/dto/ internal/mdl/
make test
make difftest
```
Expected：`go build` 通过；`go vet` 无输出；`gofmt -l` 无输出；`make test` 全绿；`make difftest` 无回归、模型查询计分板含 `m_part`…`m_join` 为 `pass`。

- [ ] **Step 4: 验收标准核对（设计 §8）**

- [ ] 引用「干净模型」（`Part`/`Lineitem`/`Nation`）的查询端到端字节一致 —— `m_part`/`m_lineitem`/`m_lineitem_calc`/`m_nation`/`m_join` 均 `pass`。
- [ ] 关系 join 渲染由任务 18 的合成 MDL 单测覆盖（to-one + to-many）。
- [ ] 引用 `Orders`/`Customer` 的查询在 baseline 标记为已知失败，文档化 P6 依赖。
- [ ] 22 条标准 TPC-H 查询无 `pass→fail` 回归。
- [ ] `go build` / `go vet` / `gofmt -l` 均干净。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/README.md internal/difftest/README.md
git commit -m "docs(p3a): document rewrite engine + finalize P3a acceptance

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## 附录 A — Java ↔ Go 文件对应表

| Java（`wren-base/sqlrewrite/`） | Go | 任务 |
|---|---|---|
| `WrenRule.java` | `internal/rewrite/rule.go` | 1 |
| `WrenPlanner.java` | `internal/rewrite/planner.go` | 1 |
| `Utils.java`（parseSql 等） | `internal/rewrite/utils.go` | 1, 13, 16, 20 |
| `GenerateViewRewrite`/`MetricRollupRewrite`/`EnumRewrite` | `internal/rewrite/passthrough_rules.go`（桩） | 2 |
| `BaseTreeRewriter.java` + `BaseRewriter.java` | `internal/rewrite/base_tree_rewriter.go` | 5 |
| `analyzer/RelationId`/`RelationType`/`Field`/`Scope`/`ScopeAnalysis` + `CatalogSchemaTableName` | `internal/rewrite/analyzer/{relation_id,relation_type,field,scope,scope_analysis,catalog_schema_table_name}.go` | 6 |
| `analyzer/Analysis.java` | `internal/rewrite/analyzer/analysis.go` | 7 |
| `analyzer/ScopeAnalyzer.java` + `Utils.analyzeFrom` | `internal/rewrite/analyzer/{scope_analyzer,utils}.go` | 8 |
| `analyzer/ExpressionAnalyzer`/`ExpressionAnalysis` | `internal/rewrite/analyzer/expression_analy{zer,sis}.go` | 9 |
| `analyzer/StatementAnalyzer.java` | `internal/rewrite/analyzer/statement_analyzer.go` | 10 |
| `analyzer/RelationshipColumnInfo`/`ExpressionRelationshipInfo`/`ExpressionRelationshipAnalyzer` | `internal/rewrite/analyzer/{relationship_column_info,expression_relationship_info,expression_relationship_analyzer}.go` | 12 |
| `RelationshipRewriter.java` | `internal/rewrite/relationship_rewriter.go` | 13 |
| `QueryDescriptor.java` | `internal/rewrite/query_descriptor.go` | 14 |
| `DummyInfo.java` | `internal/rewrite/dummy_info.go` | 14 |
| `WithRewriter.java` | `internal/rewrite/with_rewriter.go` | 14 |
| `RelationableSqlRender.java` | `internal/rewrite/relationable_sql_render.go` | 15 |
| `ModelSqlRender.java` | `internal/rewrite/model_sql_render.go` | 16 |
| `RelationInfo.java` | `internal/rewrite/relation_info.go` | 17 |
| JGraphT `DirectedAcyclicGraph` 用法 | `internal/rewrite/graph.go` | 19 |
| `WrenSqlRewrite.java`（非动态路径 + `Rewriter`） | `internal/rewrite/wren_sql_rewrite.go` | 2, 20 |

## 附录 B — 不在 P3a 范围（机械移植时遇到须停手）

- `WrenSqlRewrite` 动态字段分支（`isEnableDynamicField()` 为 true）—— `getTableRequiredFields`、`WrenDataLineage`、`DateSpineInfo`、`addDescriptor` 系列。P3a 只走 `else` 分支。
- `MetricSqlRender`、`CumulativeMetricInfo`、`MetricRollupInfo` —— P3b。
- `GenerateViewRewrite`、`EnumRewrite`、`ViewInfo` 真实实现 —— P3c。
- `analyzer/decisionpoint/`、`CacheAnalysis` —— P5。
- Jinja 宏（`{{ }}` 列表达式）—— P6。`Customer`/`Orders` 模型的端到端转绿待此。
