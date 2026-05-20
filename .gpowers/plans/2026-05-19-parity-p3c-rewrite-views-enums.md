# P3c 重写引擎（视图 / 枚举）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use gpowers:subagent-driven-development (recommended) or gpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 P3a / P3b 建立的 `internal/rewrite/` 包内机械移植 Java `wren-engine:0.9.3` 的视图与枚举重写路径（`EnumRewrite` / `ViewInfo` / `GenerateViewRewrite`），让 Go 引擎对引用视图 / 枚举的查询产出与 Java 逐字节一致的重写后 SQL，并把 `WrenPlanner` 的最后两条透传桩换成真实规则，宣告 P3「重写引擎奇偶校验」收官。

**Architecture:** P3c 是 P3a / P3b 的延伸，不改 `WrenPlanner` 编排，只把 P3a 留的 `GenerateViewRewrite` / `EnumRewrite` 两个透传桩换成真实规则，并补全分析器的 view 识别 + `WrenObjectNames` + `Utils.parseView`。`EnumRewrite` 走 P3a 的 `RewriteNode` 钩子重写两段 `DereferenceExpression`；`GenerateViewRewrite` 复用 P3a 的 `topoSort` / `applyWith` / `getWithQuery` 与 P3b 的 `MetricRollupRewrite`（`ViewInfo.get` 内套）。由外向内分 5 个切片（切片 0 语料 → 切片 1 EnumRewrite → 切片 2 分析器 view 识别 → 切片 3 ViewInfo → 切片 4 GenerateViewRewrite），每切片自成一体并以 P1 差分测试 + baseline 计分板棘轮推进。

**Tech Stack:** Go 1.x；`internal/parser`（P2 字节对齐的 parser/formatter）；`internal/parser/ast`（P2 对齐的 AST）；`internal/difftest`（P1 golden-snapshot 差分框架，支持多语料组）；`internal/dto` / `internal/mdl`（MDL 数据模型，已有 `View` / `EnumDefinition` / `EnumValue` 类型与 `WrenMDL.GetView` / `GetEnumDefinition`）；P3a 的 `internal/rewrite` 包及其 `analyzer` 子包；P3b 的 `MetricRollupRewrite` 真实规则与 metric/cumulative 分支。

---

## 前置条件（必读）

1. **P3a 与 P3b 必须已按其计划执行完毕并合并。** 本计划凡引用 P3a / P3b 产物，均指其完成后的状态：
   - P3a：`.gpowers/plans/2026-05-19-parity-p3a-rewrite-models-relationships.md`。主包 `internal/rewrite/` 已含 `rule.go` / `planner.go` / `utils.go` / `passthrough_rules.go` / `base_tree_rewriter.go` / `relationship_rewriter.go` / `query_descriptor.go` / `dummy_info.go` / `with_rewriter.go` / `relationable_sql_render.go` / `model_sql_render.go` / `relation_info.go` / `graph.go` / `wren_sql_rewrite.go`；`analyzer` 子包已含 `analysis.go`（带 `views`/`AddViews`/`Views()`）/ `statement_analyzer.go` / `scope_analyzer.go` / `expression_analyzer.go` / `expression_relationship_analyzer.go` / `field.go` / `scope.go` 等。
   - P3b：`.gpowers/plans/2026-05-19-parity-p3b-rewrite-metrics.md`。`MetricRollupRewrite` 已是真实规则（`passthrough_rules.go` 只剩 `GenerateViewRewrite` 与 `EnumRewrite` 两个桩）；`WrenSqlRewrite.Apply` 已含 metric/cumulative 分支；`QueryDescriptorOf` 已认 model/metric/cumulative/date_spine，view 仍 `return nil, fmt.Errorf("view %q requires P3c", name)`；`StatementAnalyzer.Analyze` 顶层已补 metrics / cumulativeMetrics 收集；`MetricSqlRender` / `CumulativeMetricInfo` / `DateSpineInfo` / `Utils` 度量解析函数齐全。

2. **本计划要修改若干 P3a / P3b 文件**，针对 P3a + P3b 完成后的状态。除设计 §4 文件表列出的 8 个文件外，凡触及处均在对应任务内说明缘由。设计 §4 文件表非穷举。

3. **既有 `internal/analyzer`**（提供 `SessionContext`）在所有文件中一律以别名 `base` 导入；`internal/rewrite/analyzer` 直接以 `analyzer` 引入（同 P3a / P3b 约定）。

4. **Java 参照源**位于 `../wren-engine-0.9.3/wren-base/src/main/java/io/wren/base/sqlrewrite/`（含 `analyzer/` 子目录）与 `../wren-engine-0.9.3/wren-base/src/main/java/io/wren/base/dto/`。机械移植 = 逐方法对照，控制流与命名一一对应。

## 验收性质说明（重要 —— 影响切片 1/4 的验证方式与 P3 收官口径）

**TPC-H MDL 的 4 个视图全部经 `Customer` 模型传递性依赖 Jinja `{{ }}` 列**，端到端无法在 P3c 字节转绿（与 P3a 的 `m_orders` / P3b 的 4 个度量同因，待 P6 的 Jinja 宏层）：

| 视图 | 语句 | 阻断路径 |
|---|---|---|
| `useModel` | `select * from Orders` | `Orders` 模型计算列 `nation_name = customer.nation.name` 关系遍历 → 拉入 `Customer` 模型 CTE → Jinja |
| `useMetric` | `select * from Revenue` | `Revenue.dimension.customer = customer.name` 关系遍历 → `Customer` → Jinja |
| `useMetricRollUp` | `select * from roll_up(Revenue, orderdate, YEAR)` | 同上：`Revenue` → `Customer` → Jinja |
| `useUseMetric` | `select * from useMetric` | 嵌套 `useMetric` → `Revenue` → `Customer` → Jinja |

**且 `tpch/1` 与 `tpch/4` 当前 baseline 为 `oracle-error`**（Java oracle 自身 dry-plan 失败），P3c 不可修复 —— 与本子项目无关，是 oracle 的固有限制。

因此 P3c 的端到端字节奇偶验证用**两套语料**（沿用 P3b 已确认的「合成语料 + TPC-H 已知失败」方案）：

- **合成视图 / 枚举语料组 `cases/viewenum/`**（任务 1 新建）—— 一份**无 Jinja**的小 MDL，含 model + metric + 4 个视图（包括嵌套）+ enum，**P3c 应端到端字节转绿**。这是 P3c 核心的真证据。`internal/difftest.LoadCorpus` 已支持多语料组（`cases/<group>/`），P3b 已建 `cases/metric/`。
- **TPC-H 视图 / 枚举语料 `cases/tpch/queries/v_*.sql`**（任务 1 追加）—— 引用 TPC-H 既有 4 个视图作 4 条已知失败查询（同 P3b `met_*` 模式，捕获 Java golden 作 P6 目标形态），另加 1 条**纯枚举**查询 `v_enum.sql`（`select Status.O`，不触及模型，对真 TPC-H MDL 端到端转绿）。

**P3 收官口径**（对 spec §8「TPC-H 22 条全部字节一致」的诚实重述）：

| 类别 | 数量 | 期望状态（P3c 收官时） |
|---|---|---|
| TPC-H 标准查询 `tpch/1..22` | 22 | 20 `pass` + 2 `oracle-error`（`tpch/1`/`tpch/4`，oracle 自身限制，**非 Go 缺陷**） |
| P3a 模型查询 `tpch/m_*` | 6 | 5 `pass` + 1 `fail`（`tpch/m_orders` 传递性 Jinja，待 P6） |
| P3b 度量查询 `tpch/met_*` | 5 | 5 `go-error`/`fail`（传递性 Jinja，待 P6） |
| 合成度量 `metric/*`（P3b） | 4 | 4 `pass` |
| **P3c 合成 `viewenum/*`** | **5** | **5 `pass`** ← P3c 核心证据 |
| **P3c TPC-H 视图 `tpch/v_use_*`** | **4** | **4 `go-error`/`fail`（传递性 Jinja，已知失败，待 P6）** |
| **P3c TPC-H 枚举 `tpch/v_enum`** | **1** | **1 `pass`**（纯枚举，不触模型） |

**P3 收官真实达成标准**：上表「期望状态」全满足；P3a/P3b 既有 `pass` 无 `pass→fail` 回归；`go build` / `go vet` / `gofmt -l` 干净。

## 字节分歧风险登记表（贯穿全程，执行时逐条核对）

继承 P3a 全部风险（CTE 顺序由拓扑迭代决定 —— P3a `graph.go` 的 `topoSort`+`topoTieBreak`；`format()` 字符串模板逐字节；Map/Set 迭代序）与 P3b 全部风险（`MetricRollupRewrite` 别名非 delimited、规则间重解析、metric/cumulative 模板等），另加 P3c 特有：

| # | 风险 | 缓解（落在哪个任务） |
|---|---|---|
| 1 | view 嵌套 view 的 DAG —— `useUseMetric` 引用 `useMetric`，CTE 序须与 Java 一致（同 P3a 风险 #1 的 `topoTieBreak`，但顶点限于 view）。 | 任务 7：`GenerateViewRewrite.Apply` 复用 P3a `topoSort`/`topoTieBreak`；任务 8 用 golden 逆向校验。 |
| 2 | 两轮 CTE 前置的叠加顺序 —— 规则 1 `GenerateViewRewrite` 先把 view 加为 CTE，规则 3 `WrenSqlRewrite` 再前置 model/metric CTE；P3a 的 `applyWith` `Stream.concat(withQueries, with.getQueries())` 即「新前置在旧 WITH 前」—— 故最终 CTE 序为 `[models, metrics, …, views, 用户 WITH]`。 | 任务 7：不增加显式合并逻辑，依赖 P3a `applyWith` 既有实现；任务 8 用 golden 验证。 |
| 3 | enum 值匹配 —— Java `EnumDefinition.valueOf` 用 `getName().equals(...)` 严格大小写匹配；Go `dto.EnumDefinition.ValueOf` 同语义（`v.Name == name`）。**勿改成 `EqualFold`。** | 任务 3：直接调 `wrenMDL.GetEnumDefinition(...).ValueOf(...)`，不加大小写折叠。 |
| 4 | enum 仅匹配两段 dereference —— `EnumName.Value` 必须**恰**为两段 `QualifiedName`；三段及以上不视为 enum，照常递归 base。 | 任务 3：`len(qn.OriginalParts) != 2` 直接返回 `(node, false)`（不匹配，descend）。 |
| 5 | enum 值字面量回退 —— `EnumValue.getValue()` 在 `value` 为空时回退到 `name`（Go `dto.EnumValue.GetValue()` 已实现）。 | 任务 3：直接调 `ev.GetValue()`，不重复实现回退。 |
| 6 | view 内度量汇总语法 —— `ViewInfo.get` 对 view body 套 `MetricRollupRewrite`；Go 用 P3b 的 3 参 `Apply`（内部自建 `Analysis`），对同一 `query` 树二次分析结果与 Java 复用 analysis 等价（节点身份一致），输出字节相同。 | 任务 6：`viewInfoGet` 调 `(&MetricRollupRewrite{}).Apply(query, ctx, analyzedMDL)`。 |
| 7 | `WithQuery` CTE 名仍走 P3a `getWithQuery` —— `Identifier(name, true)` delimited；view CTE 名故为 `"useModel"` 等（带双引号）。 | 任务 7：直接调 P3a `getWithQuery`，不另造 identifier。 |
| 8 | `getWrenObjectNames` 仅用于 view 嵌套发现 —— 它返回 `Set<String>`（model+metric+cumulative+view 名并集），非 view 的成员被 `GenerateViewRewrite.addToGraph` 过滤掉（非动态路径下 model/metric 由 `WrenSqlRewrite` 处理）。 | 任务 5：`WrenObjectNames()` 返回**按名排序**的 `[]string`（确定性）；任务 7 在 `addToGraph` 用 `wrenMDL.GetView(req)` 过滤。 |
| 9 | `parseView` —— Java `(Query) parseSql(sql)` 是无 try 的强转；Go 用类型断言 + 错误返回，**语义等价**（view 语句必为 Query，否则 MDL 非法）。 | 任务 6：`parseView` 在 `utils.go`，与 `parseQuery` 共享 `parseSQL`。 |

---

# 切片 0 — 视图 / 枚举语料与 golden 冻结

目标：建合成视图 / 枚举语料组、追加 TPC-H 视图 / 枚举语料、冻结 Java golden、接受 baseline。

## 任务 1：新建合成视图 / 枚举语料组 + 追加 TPC-H 语料

**Files:**
- Create: `testdata/difftest/cases/viewenum/group.json`
- Create: `testdata/difftest/cases/viewenum/mdl.json`
- Create: `testdata/difftest/cases/viewenum/queries/view_on_model.sql`
- Create: `testdata/difftest/cases/viewenum/queries/view_on_metric.sql`
- Create: `testdata/difftest/cases/viewenum/queries/view_rollup.sql`
- Create: `testdata/difftest/cases/viewenum/queries/view_nested.sql`
- Create: `testdata/difftest/cases/viewenum/queries/enum.sql`
- Create: `testdata/difftest/cases/tpch/queries/v_use_model.sql`
- Create: `testdata/difftest/cases/tpch/queries/v_use_metric.sql`
- Create: `testdata/difftest/cases/tpch/queries/v_use_rollup.sql`
- Create: `testdata/difftest/cases/tpch/queries/v_use_nested.sql`
- Create: `testdata/difftest/cases/tpch/queries/v_enum.sql`

说明：`internal/difftest/corpus.go` 的 `LoadCorpus` 按 `root/<group>/{mdl.json,group.json,queries/*.sql}` 扫描每个语料组。新建的 `cases/viewenum/` 与既有 `cases/tpch/`、`cases/metric/`（P3b）平级、互不影响。`cases/tpch/` 的 22 条标准查询 + P3a `m_*` + P3b `met_*` 共用 `cases/tpch/mdl.json`，追加 `v_*.sql` 不改动该 MDL（TPC-H MDL 已含 4 个视图与 `Status` enum）。

- [ ] **Step 1: 写 `cases/viewenum/group.json`**

对照 `cases/tpch/group.json`（`{"modelingOnly": true}`）—— dry-plan 重写校验。

```json
{
  "modelingOnly": true
}
```

- [ ] **Step 2: 写 `cases/viewenum/mdl.json` —— 无 Jinja 合成视图 / 枚举 MDL**

一份最小、Java oracle 与 Go 双方均可解析的 MDL：1 个干净模型 `Orders`（无 Jinja、无关系计算列、加 `orderstatus` 列供 enum where 用）；1 个 metric `Revenue`（含 `timeGrain orderdate` 供 rollup 用）；4 个视图（名字与 TPC-H 完全一致，便于横向对照）；1 个 enum `Status`（与 TPC-H 同结构：`F` / `O` / `P`，无显式 `value`）。

```json
{
  "catalog": "wren",
  "schema": "test",
  "models": [
    {
      "name": "Orders",
      "refSql": "select * from orders",
      "primaryKey": "orderkey",
      "columns": [
        {"name": "orderkey", "type": "int4", "expression": "o_orderkey"},
        {"name": "custkey", "type": "int4", "expression": "o_custkey"},
        {"name": "totalprice", "type": "float8", "expression": "o_totalprice"},
        {"name": "orderdate", "type": "date", "expression": "o_orderdate"},
        {"name": "orderstatus", "type": "varchar", "expression": "o_orderstatus"}
      ]
    }
  ],
  "relationships": [],
  "metrics": [
    {
      "name": "Revenue",
      "baseObject": "Orders",
      "dimension": [{"name": "custkey", "type": "int4", "expression": "custkey"}],
      "measure": [{"name": "totalprice", "type": "int4", "expression": "sum(totalprice)"}],
      "timeGrain": [{"name": "orderdate", "refColumn": "orderdate", "dateParts": ["YEAR", "MONTH"]}]
    }
  ],
  "cumulativeMetrics": [],
  "enumDefinitions": [
    {"name": "Status", "values": [{"name": "F"}, {"name": "O"}, {"name": "P"}]}
  ],
  "views": [
    {"name": "useModel", "statement": "select * from Orders"},
    {"name": "useMetric", "statement": "select * from Revenue"},
    {"name": "useMetricRollUp", "statement": "select * from roll_up(Revenue, orderdate, YEAR)"},
    {"name": "useUseMetric", "statement": "select * from useMetric"}
  ],
  "macros": []
}
```

- [ ] **Step 3: 写 `cases/viewenum/queries/` 5 条合成视图 / 枚举查询**

`view_on_model.sql`（view → model）：

```sql
select custkey, totalprice from useModel
```

`view_on_metric.sql`（view → metric → model）：

```sql
select custkey, totalprice from useMetric
```

`view_rollup.sql`（view → rollup → metric → model；行使风险 #6）：

```sql
select * from useMetricRollUp
```

`view_nested.sql`（view → view → metric → model；行使风险 #1 的嵌套 view DAG）：

```sql
select custkey, totalprice from useUseMetric
```

`enum.sql`（enum 在 WHERE，模型展开后 EnumRewrite 重写；行使 EnumRewrite + WrenSqlRewrite 组合）：

```sql
select orderkey from Orders where orderstatus = Status.O
```

- [ ] **Step 4: 写 5 条 TPC-H 视图 / 枚举查询**

引用 `cases/tpch/mdl.json` 既有 4 视图 + `Status` enum。

`v_use_model.sql`（已知失败：Jinja）：

```sql
select * from useModel
```

`v_use_metric.sql`（已知失败：Jinja）：

```sql
select * from useMetric
```

`v_use_rollup.sql`（已知失败：Jinja）：

```sql
select * from useMetricRollUp
```

`v_use_nested.sql`（已知失败：Jinja）：

```sql
select * from useUseMetric
```

`v_enum.sql`（**纯枚举，不触模型**，对真 TPC-H MDL 端到端转绿）：

```sql
select Status.O
```

- [ ] **Step 5: 确认语料可被 difftest 加载**

Run: `go test ./internal/difftest/...`
Expected: 加载无报错；`viewenum/*` 5 条与 `tpch/v_*` 5 条出现在用例集中。若 `LoadCorpus` 因缺 `group.json`/`mdl.json` 报错，补齐 Step 1–2 的文件。

- [ ] **Step 6: Commit**

```bash
git add testdata/difftest/cases/viewenum/ testdata/difftest/cases/tpch/queries/v_*.sql
git commit -m "test(p3c): add synthetic viewenum corpus + TPC-H view/enum queries

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 2：捕获 golden + 检视 + 接受 baseline

**Files:**
- Generated: `testdata/difftest/golden/viewenum/*.sql`、`testdata/difftest/golden/tpch/v_*.sql`（经 `make capture-golden`）
- Modify: `testdata/difftest/baseline.json`（经 `make difftest-accept`）

- [ ] **Step 1: 启动 Java oracle 并捕获 golden**

需 Docker。`tools/capture-golden.sh` 起 `ghcr.io/canner/wren-engine:0.9.3` 容器、对全语料 `GET /v1/mdl/dry-plan` 捕获。

Run: `make capture-golden`
Expected: 控制台逐条 `OK viewenum/enum` … `OK tpch/v_enum` …；10 条新用例均 `OK`（Java oracle 处理 Jinja，TPC-H 视图查询返回 2xx）。

- [ ] **Step 2: 检视合成视图 / 枚举 golden，确认形态**

逐条阅读 `testdata/difftest/golden/viewenum/`：

- `view_on_model.sql` 应含 `WITH "Orders" AS (...), "useModel" AS (SELECT * FROM Orders)` —— model CTE 先、view CTE 后（风险 #2）。
- `view_nested.sql` 应含 `"Orders" AS (...), "Revenue" AS (...), "useMetric" AS (...), "useUseMetric" AS (SELECT * FROM useMetric)` —— **`useMetric` 必须排在 `useUseMetric` 之前**（风险 #1：嵌套 view 拓扑序）。把此 CTE 序记录在任务 8 执行笔记里 —— 风险 #1 与 P3a `topoTieBreak` 的逆向校验依据。
- `view_rollup.sql` 应含 rollup 已被展开为子查询（无 `roll_up` 字样）的 view CTE。
- `enum.sql` 应把 `Status.O` 替换为 `'O'`（`WHERE orderstatus = 'O'`）。

若某条 `viewenum/*` golden 是 `.error` 文件（oracle 4xx/5xx），说明合成 MDL 不被 Java 接受 —— 读错误信息修 `cases/viewenum/mdl.json`（任务 1 Step 2）后重跑 `make capture-golden`。

- [ ] **Step 3: 检视 TPC-H 视图 / 枚举 golden**

- `tpch/v_use_*.sql` 应为含 `WITH` 的多 CTE 重写结果（Java 已展开 Jinja，含 `Customer` CTE）。确认 4 条均为 `.sql` 而非 `.error`。
- `tpch/v_enum.sql` 应为 `SELECT 'O'` 形态（无 model、无 WITH）。

- [ ] **Step 4: 跑差分测试，接受 baseline**

此刻 Go 端（P3a + P3b 状态）对视图查询走透传（`GenerateViewRewrite` / `EnumRewrite` 仍是桩）—— 输出 ≠ golden。

Run: `make difftest`
Expected: 22 条标准查询 + `m_*` + `met_*` + `metric/*` 无回归；`viewenum/*` 5 条与 `tpch/v_*` 5 条均 `fail`（透传未展开视图 / 未重写枚举）。

Run: `make difftest-accept`
然后 `git diff testdata/difftest/baseline.json` 核对：仅新增 `viewenum/enum`、`viewenum/view_nested`、`viewenum/view_on_metric`、`viewenum/view_on_model`、`viewenum/view_rollup`、`tpch/v_enum`、`tpch/v_use_metric`、`tpch/v_use_model`、`tpch/v_use_nested`、`tpch/v_use_rollup` 共 10 条，全为 `fail`；既有条目不变。

- [ ] **Step 5: Commit**

```bash
git add testdata/difftest/golden/viewenum/ testdata/difftest/golden/tpch/v_*.sql testdata/difftest/baseline.json
git commit -m "test(p3c): freeze view/enum golden + rebaseline

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 1 — EnumRewrite

目标：把 `EnumRewrite` 透传桩换成真实规则；引用枚举的查询（合成 + TPC-H 纯枚举）端到端转绿。EnumRewrite 无 analyzer 依赖、最独立 —— 故置于切片 1。

## 任务 3：`EnumRewrite` 真实规则

**Files:**
- Create: `internal/rewrite/enum_rewrite.go`
- Modify: `internal/rewrite/passthrough_rules.go`（删除 `EnumRewrite` 桩）
- Test: `internal/rewrite/enum_rewrite_test.go`

机械移植 `EnumRewrite.java`（92 行）。Java 的 `apply`：`new Rewriter(wrenMDL).process(root)` —— 无 analyzer。Go 用 P3a 的 `RewriteNode` 钩子。**风险 #3 / #4 / #5**：严格大小写匹配；仅两段 `QualifiedName`；值字面量回退由 `dto.EnumValue.GetValue()` 已实现。

- [ ] **Step 1: 先写 `enum_rewrite_test.go` —— 失败测试**

```go
package rewrite

import (
	"os"
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

// viewenumMDL loads the synthetic view/enum MDL. Shared across P3c tests.
func viewenumMDL(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/difftest/cases/viewenum/mdl.json")
	if err != nil {
		t.Fatalf("read viewenum mdl: %v", err)
	}
	wrenMDL, err := mdl.WrenMDLFromJSON(string(raw))
	if err != nil {
		t.Fatalf("parse viewenum mdl: %v", err)
	}
	return wrenMDL
}

func TestEnumRewrite_ReplacesEnumInWhere(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	stmt, err := parser.ParseSQL("SELECT orderkey FROM Orders WHERE orderstatus = Status.O")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := (&EnumRewrite{}).Apply(stmt, nil, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	if !strings.Contains(got, "'O'") {
		t.Errorf("missing 'O' literal:\n%s", got)
	}
	if strings.Contains(got, "Status.O") {
		t.Errorf("Status.O not replaced:\n%s", got)
	}
}

func TestEnumRewrite_ThreePartDereferenceUntouched(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	// x.Status.O is 3-part: not an enum match, base recurses but Status alone
	// is also not 2-part-enum since base is x.Status not just Status.
	stmt, _ := parser.ParseSQL("SELECT x.Status.O FROM t")
	out, err := (&EnumRewrite{}).Apply(stmt, nil, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	if !strings.Contains(got, "Status") || !strings.Contains(got, "O") {
		t.Errorf("3-part dereference should be untouched:\n%s", got)
	}
}

func TestEnumRewrite_UnknownEnumNameLeftAlone(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	stmt, _ := parser.ParseSQL("SELECT NotAnEnum.X FROM t")
	out, err := (&EnumRewrite{}).Apply(stmt, nil, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	if !strings.Contains(got, "NotAnEnum") {
		t.Errorf("non-enum dereference should be untouched:\n%s", got)
	}
}

func TestEnumRewrite_MissingEnumValueErrors(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	stmt, _ := parser.ParseSQL("SELECT Status.NOPE FROM t")
	_, err := (&EnumRewrite{}).Apply(stmt, nil, mdl.NewAnalyzedMDL(wrenMDL))
	if err == nil {
		t.Fatal("expected error for unknown enum value, got nil")
	}
	if !strings.Contains(err.Error(), "NOPE") || !strings.Contains(err.Error(), "Status") {
		t.Errorf("error should mention NOPE and Status, got: %v", err)
	}
}
```

Run: `go test ./internal/rewrite/ -run TestEnumRewrite -v`
Expected: 编译失败 —— `EnumRewrite` 桩的 `Apply` 不会重写、`MissingEnumValueErrors` 与 `ReplacesEnumInWhere` 不达预期；或编译报错（取决于桩签名）。无论何种，本步「失败先行」。

- [ ] **Step 2: 从 `passthrough_rules.go` 删除 `EnumRewrite` 桩**

P3b 完成后的 `passthrough_rules.go` 应仅含 `GenerateViewRewrite` 与 `EnumRewrite` 两个桩。删除 `EnumRewrite` 结构体及其 `Apply` 方法（连同其上方注释）；保留 `GenerateViewRewrite` 桩（待任务 7 删）。

- [ ] **Step 3: 写 `enum_rewrite.go`**

机械移植 `EnumRewrite.java`。Java `visitDereferenceExpression`：若 `rewriteEnumIfNeed` 返回了不同的节点 → 返回；否则用 `process(base)` 重建。Go 用 `RewriteNode` 钩子：枚举命中 → 返回 `(StringLiteral, true)`；不命中 → 返回 `(nil, false)`（让 `RewriteNode` 把 `Base` 递归重写、`Field` 保留）—— 行为等价。

```go
package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// EnumRewrite replaces "EnumName.Value" dereferences with a string literal.
// Mirrors Java io.wren.base.sqlrewrite.EnumRewrite.
type EnumRewrite struct{}

// Apply rewrites every 2-part DereferenceExpression whose first part names an
// MDL enum into a StringLiteral of the matched EnumValue's value. Mirrors
// EnumRewrite.apply + the inner Rewriter.visitDereferenceExpression.
func (r *EnumRewrite) Apply(root ast.Statement, _ *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	wrenMDL := analyzedMDL.WrenMDL()
	var rewriteErr error
	out := RewriteNode(root, func(n ast.Node) (ast.Node, bool) {
		d, ok := n.(*ast.DereferenceExpression)
		if !ok {
			return nil, false // descend everything else
		}
		repl, matched, err := rewriteEnumIfNeed(d, wrenMDL)
		if err != nil {
			rewriteErr = err
			return d, true // stop descent on error path
		}
		if matched {
			return repl, true
		}
		return nil, false // not an enum: let RewriteNode recurse into Base
	})
	if rewriteErr != nil {
		return nil, rewriteErr
	}
	return out.(ast.Statement), nil
}

// rewriteEnumIfNeed returns (literal, true, nil) when node is a 2-part
// dereference whose first part is an MDL enum name; (node, false, nil) when
// it's not an enum match; or (node, false, err) when the enum name matches
// but the value doesn't (mirrors Java's IllegalArgumentException).
func rewriteEnumIfNeed(node *ast.DereferenceExpression, wrenMDL *mdl.WrenMDL) (ast.Expression, bool, error) {
	qn := ast.GetQualifiedName(node)
	if qn == nil || len(qn.OriginalParts) != 2 { // risk #4
		return node, false, nil
	}
	enumName := qn.OriginalParts[0].Value
	enumDef, ok := wrenMDL.GetEnumDefinition(enumName)
	if !ok {
		return node, false, nil
	}
	valueName := qn.OriginalParts[1].Value
	ev := enumDef.ValueOf(valueName) // risk #3: strict equality
	if ev == nil {
		return node, false, fmt.Errorf("Enum value '%s' not found in enum '%s'", qn.Parts[1], qn.Parts[0])
	}
	return &ast.StringLiteral{Value: ev.GetValue()}, true, nil // risk #5
}
```

注：`ast.GetQualifiedName` 已在 P3a `relationship_rewriter.go` / `wren_sql_rewrite.go` 中使用；`ast.DereferenceExpression` 的 `Base` / `Field` 字段、`ast.QualifiedName.OriginalParts` / `Parts`、`ast.Identifier.Value`、`ast.StringLiteral{Value string}` 均在 P2 AST 中已定义。`dto.EnumDefinition.ValueOf(name) *EnumValue`（返回 nil 表示未命中）与 `dto.EnumValue.GetValue() string`（value 空时回退到 name）已在 `internal/dto/enum.go` 中实现。

`planner.go` 的 `AllRules` 引用 `&EnumRewrite{}` —— 删桩后该名字解析到本任务新建的真实类型，**类型名不变、`planner.go` 无需改**（同 P3b 任务 14 的 `MetricRollupRewrite` 模式）。

- [ ] **Step 4: 运行测试**

Run: `go build ./... && go test ./internal/rewrite/ -run TestEnumRewrite -v`
Expected: `go build` 通过；4 个测试 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/enum_rewrite.go internal/rewrite/passthrough_rules.go internal/rewrite/enum_rewrite_test.go
git commit -m "feat(p3c): real EnumRewrite rule, drop pass-through stub (slice 1)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 4：端到端 —— 枚举查询转绿

**Files:**
- Modify: `testdata/difftest/baseline.json`

- [ ] **Step 1: 跑差分测试，看枚举查询状态**

Run: `make difftest`
Expected: 22 条标准查询 + P3a `m_*` + P3b `metric/*` + `met_*` 无回归；`viewenum/enum` 由 `fail` 翻 `pass`（`Status.O` → `'O'`，Orders 模型已被 P3a 展开为 CTE）；`tpch/v_enum` 由 `fail` 翻 `pass`（`Status.O` → `'O'`，无 model 展开）；`viewenum/view_*`（4 条）与 `tpch/v_use_*`（4 条）仍 `fail`（待切片 4）。

- [ ] **Step 2: 排查（若 `viewenum/enum` 或 `tpch/v_enum` 未转绿）**

`make difftest` 日志给出首个 token 差异。常见根因：

- **大小写匹配错（风险 #3）**：Go 输出 `'O'`，但 Java golden 是 `'o'` —— `EnumDefinition.values[].name` 是 `O` 大写，Java `valueOf("O").equals(...)` 命中 → 返回 `EnumValue{name:"O"}.getValue()=="O"` → `'O'`。若 Go 输出全小写说明误用了 `EqualFold` 或 `strings.ToLower` —— 改回 `==`。
- **`ev.GetValue()` 回退错（风险 #5）**：Java 当 `value` 为空回退到 `name`。Go `dto.EnumValue.GetValue` 已正确实现 `if e.Value != "" { return e.Value }; return e.Name`。若 golden 不匹配，先核对 `cases/viewenum/mdl.json` 的 `values` 是否带显式 `value`。
- **3 段误命中（风险 #4）**：若 `x.Status.O` 被改写为 `'O'`，说明判定写错（`len != 2` 应直接 return），核对 `len(qn.OriginalParts) != 2` 分支。
- **DereferenceExpression base 重建丢字段（base 递归后 Field 误改）**：`RewriteNode` 的 `*ast.DereferenceExpression` case 只重写 `Base`，`Field` 保留 —— P3a `base_tree_rewriter.go:343-346` 即此实现，无需改。

- [ ] **Step 3: 接受 baseline**

枚举查询转绿后：

Run: `make difftest-accept`
`git diff testdata/difftest/baseline.json` 确认：`viewenum/enum` 与 `tpch/v_enum` 变 `pass`；视图类（`viewenum/view_*`、`tpch/v_use_*`）仍 `fail`；既有 `pass` 无 `pass→fail` 回归。

- [ ] **Step 4: Commit**

```bash
git add testdata/difftest/baseline.json
git commit -m "test(p3c): enum queries pass differential test (slice 1)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 2 — 分析器补 view 识别 + `WrenObjectNames`

目标：`StatementAnalyzer` 把出现在查询里的 view 名加入 `Analysis.views`；`Analysis` 暴露 `WrenObjectNames()`（model + metric + cumulative + view 名并集，按名排序）。这是 `ViewInfo.RequiredObjects()`（切片 3）与 `GenerateViewRewrite` 视图嵌套 DAG（切片 4）的数据源。

## 任务 5：`StatementAnalyzer.Analyze` 补 view 收集 + `Analysis.WrenObjectNames`

**Files:**
- Modify: `internal/rewrite/analyzer/statement_analyzer.go`
- Modify: `internal/rewrite/analyzer/analysis.go`
- Test: `internal/rewrite/analyzer/view_analyzer_test.go`

说明：P3a `Analysis` 已有 `views []*dto.View` 字段、`AddViews`、`Views()`（见 `analyzer/analysis.go:20,100-113`）—— 仅 `WrenObjectNames()` 与 `StatementAnalyzer.Analyze` 顶层的 view 收集尚缺（P3a 留注释占位 `// metrics / cumulativeMetrics / views: P3a语料无引用，保留对齐Java`，P3b 任务 5 已替换为 metrics/cumulative 收集；本任务补 views）。

- [ ] **Step 1: 先写失败测试 `analyzer/view_analyzer_test.go`**

```go
package analyzer

import (
	"sort"
	"testing"

	"github.com/wren-engine/wren/internal/parser"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestAnalyze_IdentifiesView(t *testing.T) {
	wrenMDL := loadTPCH(t) // P3a helper, same package
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT * FROM useModel")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	views := a.Views()
	if len(views) != 1 || views[0].Name != "useModel" {
		t.Fatalf("Views() = %v, want [useModel]", views)
	}
}

func TestAnalyze_WrenObjectNames(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	// useUseMetric references useMetric (a view) inside its body, but the body
	// isn't analyzed here — this test only checks top-level reference: the
	// outer query references useMetric directly.
	stmt, _ := parser.ParseSQL("SELECT * FROM useMetric")
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	got := a.WrenObjectNames()
	want := []string{"useMetric"} // useMetric is the only top-level wren object
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("WrenObjectNames() = %v, want %v", got, want)
	}
	// also verify it's sorted: insert a model + view together
	stmt2, _ := parser.ParseSQL("SELECT * FROM Orders, useModel")
	a2 := NewAnalysis(stmt2)
	if _, err := Analyze(a2, stmt2, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	got2 := a2.WrenObjectNames()
	want2 := []string{"Orders", "useModel"}
	sort.Strings(want2) // already sorted
	if len(got2) != len(want2) || got2[0] != want2[0] || got2[1] != want2[1] {
		t.Fatalf("WrenObjectNames() = %v, want %v", got2, want2)
	}
}
```

`loadTPCH(t)` 是 P3a `analyzer_test.go` 同包 helper（读 `../../../testdata/difftest/cases/tpch/mdl.json`）。

Run: `go test ./internal/rewrite/analyzer/ -run TestAnalyze_IdentifiesView -v`
Expected: 编译失败 —— `WrenObjectNames` 未定义；或测试失败 —— `Views()` 为空（顶层未收集）。

- [ ] **Step 2: 在 `analyzer/statement_analyzer.go` 顶层 `Analyze` 补 view 收集**

对照 Java `StatementAnalyzer.java:117-123`。P3b 完成后，`Analyze` 顶层在 `AddModels` 之后已有 metrics / cumulative 收集（P3b 任务 5 Step 1）。**在 cumulative 收集之后、`return queryScope` 之前**追加 views 收集：

```go
	// views referenced as plain tables
	var views []*dto.View
	for _, t := range analysis.Tables() {
		if t.Catalog == wrenMDL.Catalog() && t.Schema == wrenMDL.Schema() {
			if v, ok := wrenMDL.GetView(t.Table); ok {
				views = append(views, v)
			}
		}
	}
	analysis.AddViews(views)
```

注：若 P3b 任务 5 把 metrics/cumulative 写在 `// metrics / cumulativeMetrics / views: P3a语料无引用，保留对齐Java` 注释处并已删除该注释，本步在 cumulative 之后续写；若注释仍存在（P3b 未完全清理），把注释一并删除并补齐 metrics + cumulative + views 三者（与 P3b 任务 5 Step 1 一致）。

- [ ] **Step 3: 在 `analyzer/analysis.go` 加 `WrenObjectNames`**

对照 Java `Analysis.getWrenObjectNames`（`Analysis.java:141-149`）：4 类对象名并集 → `Set<String>`。Go 返回**按名排序**的 `[]string`（风险 #8 + 沿用 P3a / P3b 「集合 → 排序切片」约定）。

在 `analysis.go` 末尾追加：

```go
// WrenObjectNames returns the union of model / metric / cumulative metric / view
// names referenced by this analysis, sorted by name (deterministic).
// Mirrors Java Analysis.getWrenObjectNames.
func (a *Analysis) WrenObjectNames() []string {
	set := map[string]bool{}
	for _, m := range a.models {
		set[m.Name] = true
	}
	for _, m := range a.metrics {
		set[m.Name] = true
	}
	for _, c := range a.cumulativeMetrics {
		set[c.Name] = true
	}
	for _, v := range a.views {
		set[v.Name] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

`sort` 已在 `analysis.go:4` import。无需新增 import。

- [ ] **Step 4: 运行测试**

Run: `go test ./internal/rewrite/analyzer/... -v`
Expected: 2 个新测试 + P3a / P3b 既有分析器测试全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/analyzer/statement_analyzer.go internal/rewrite/analyzer/analysis.go internal/rewrite/analyzer/view_analyzer_test.go
git commit -m "feat(p3c): analyzer identifies views + Analysis.WrenObjectNames (slice 2)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 3 — ViewInfo + `parseView` + `QueryDescriptorOf` 视图分支

目标：把 view 渲染为 `QueryDescriptor`；让 `QueryDescriptorOf("viewName")` 返回 `*ViewInfo`，供切片 4 的 `GenerateViewRewrite` 递归调用。

## 任务 6：`Utils.parseView` + `ViewInfo` + `QueryDescriptorOf` 视图分支

**Files:**
- Modify: `internal/rewrite/utils.go`（加 `parseView`）
- Create: `internal/rewrite/view_info.go`
- Modify: `internal/rewrite/query_descriptor.go`（view 分支由 error 改为真实实现）
- Test: `internal/rewrite/view_info_test.go`

机械移植 `ViewInfo.java`（70 行）+ `Utils.parseView`（`Utils.java:66-69`）。`ViewInfo.get` 关键步骤：`parseView` → `StatementAnalyzer.analyze` → 套 `MetricRollupRewrite` → `new ViewInfo(name, analysis.getWrenObjectNames(), query)`。**风险 #6**：Go 用 P3b 三参 `MetricRollupRewrite.Apply`（内部自建 analysis）；同一 `query` 树二次分析结果与 Java 复用 analysis 等价（节点身份一致）。

- [ ] **Step 1: 先写失败测试 `view_info_test.go`**

```go
package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/formatter"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestViewInfo_GetOnModel(t *testing.T) {
	wrenMDL := viewenumMDL(t) // helper from enum_rewrite_test.go
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	view, _ := wrenMDL.GetView("useModel")
	info, err := viewInfoGet(view, mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("viewInfoGet: %v", err)
	}
	if info.Name() != "useModel" {
		t.Fatalf("Name() = %q, want useModel", info.Name())
	}
	// useModel = "select * from Orders" → required = {Orders}
	req := info.RequiredObjects()
	if len(req) != 1 || req[0] != "Orders" {
		t.Fatalf("RequiredObjects() = %v, want [Orders]", req)
	}
	got := formatter.FormatSQL(info.Query())
	if !strings.Contains(got, "Orders") {
		t.Errorf("view query missing Orders:\n%s", got)
	}
}

func TestViewInfo_GetOnMetric(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	view, _ := wrenMDL.GetView("useMetric")
	info, err := viewInfoGet(view, mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("viewInfoGet: %v", err)
	}
	// useMetric = "select * from Revenue" → required = {Revenue}
	req := info.RequiredObjects()
	if len(req) != 1 || req[0] != "Revenue" {
		t.Fatalf("RequiredObjects() = %v, want [Revenue]", req)
	}
}

func TestViewInfo_GetNested(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	view, _ := wrenMDL.GetView("useUseMetric")
	info, err := viewInfoGet(view, mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("viewInfoGet: %v", err)
	}
	// useUseMetric = "select * from useMetric" → required = {useMetric}
	req := info.RequiredObjects()
	if len(req) != 1 || req[0] != "useMetric" {
		t.Fatalf("RequiredObjects() = %v, want [useMetric]", req)
	}
}

func TestViewInfo_GetRollupExpanded(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	view, _ := wrenMDL.GetView("useMetricRollUp")
	info, err := viewInfoGet(view, mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("viewInfoGet: %v", err)
	}
	got := formatter.FormatSQL(info.Query())
	// MetricRollupRewrite should have replaced roll_up(...) with a subquery
	if strings.Contains(got, "roll_up") {
		t.Errorf("roll_up not replaced in view body:\n%s", got)
	}
	if !strings.Contains(got, "DATE_TRUNC") {
		t.Errorf("rollup subquery missing DATE_TRUNC:\n%s", got)
	}
}

func TestQueryDescriptorOf_View(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	d, err := QueryDescriptorOf("useModel", mdl.NewAnalyzedMDL(wrenMDL), ctx)
	if err != nil {
		t.Fatalf("QueryDescriptorOf(useModel): %v", err)
	}
	if d.Name() != "useModel" {
		t.Fatalf("Name() = %q, want useModel", d.Name())
	}
	if _, ok := d.(*ViewInfo); !ok {
		t.Fatalf("expected *ViewInfo, got %T", d)
	}
}
```

Run: `go test ./internal/rewrite/ -run 'TestViewInfo|TestQueryDescriptorOf_View' -v`
Expected: 编译失败（`viewInfoGet` / `ViewInfo` 未定义）。

- [ ] **Step 2: 在 `utils.go` 加 `parseView`**

对照 Java `Utils.java:66-69`（强转 `(Query) parseSql(sql)`）。Go 用类型断言 + 错误返回。在 `utils.go` 现有 `parseQuery` 之后追加：

```go
// parseView parses a view's SQL statement and asserts it is a *ast.Query.
// Mirrors Java Utils.parseView.
func parseView(sql string) (*ast.Query, error) {
	stmt, err := parseSQL(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse view: %s: %w", sql, err)
	}
	q, ok := stmt.(*ast.Query)
	if !ok {
		return nil, fmt.Errorf("view statement is not a query: %s", sql)
	}
	return q, nil
}
```

`fmt` 与 `ast` 已在 `utils.go:5-9` import。无需新增 import。

- [ ] **Step 3: 写 `view_info.go`**

```go
package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// ViewInfo is a QueryDescriptor backed by an MDL view's parsed (and
// rollup-rewritten) statement. Mirrors Java io.wren.base.sqlrewrite.ViewInfo.
type ViewInfo struct {
	name            string
	requiredObjects []string
	query           *ast.Query
}

func newViewInfo(name string, requiredObjects []string, query *ast.Query) *ViewInfo {
	return &ViewInfo{name: name, requiredObjects: requiredObjects, query: query}
}

func (v *ViewInfo) Name() string              { return v.name }
func (v *ViewInfo) RequiredObjects() []string { return v.requiredObjects }
func (v *ViewInfo) Query() *ast.Query         { return v.query }

// viewInfoGet builds a ViewInfo for an MDL view. Mirrors ViewInfo.get.
// Steps mirror Java line by line:
//  1. parseView(view.getStatement())
//  2. StatementAnalyzer.analyze on the parsed body
//  3. apply MetricRollupRewrite to the body (SQL in a view can use roll_up syntax)
//  4. requiredObjects = analysis.getWrenObjectNames() (already name-sorted)
//
// Risk #6: Go's MetricRollupRewrite.Apply (P3b 3-arg) builds a fresh internal
// Analysis from the same query tree. Node identity is preserved, so the rollup
// FunctionRelation captured during step 2 is also captured (identically) during
// the 3-arg call's internal Analyze — byte-equivalent to Java reusing analysis.
func viewInfoGet(view *dto.View, analyzedMDL *mdl.AnalyzedMDL, ctx *base.SessionContext) (*ViewInfo, error) {
	query, err := parseView(view.Statement)
	if err != nil {
		return nil, err
	}
	analysis := analyzer.NewAnalysis(query)
	if _, err := analyzer.Analyze(analysis, query, ctx, analyzedMDL.WrenMDL()); err != nil {
		return nil, err
	}
	rewritten, err := (&MetricRollupRewrite{}).Apply(query, ctx, analyzedMDL)
	if err != nil {
		return nil, err
	}
	rq, ok := rewritten.(*ast.Query)
	if !ok {
		return nil, fmt.Errorf("view %q body is not a query after rollup rewrite", view.Name)
	}
	return newViewInfo(view.Name, analysis.WrenObjectNames(), rq), nil
}
```

注：`dto.View.Statement`、`dto.View.Name` 字段已在 `internal/dto/view.go` 定义。

- [ ] **Step 4: 把 `query_descriptor.go` 的 view 分支由 error 改为真实实现**

P3b 任务 11 完成后，`QueryDescriptorOf` 的 view 分支形如：

```go
	if _, ok := wrenMDL.GetView(name); ok {
		return nil, fmt.Errorf("view %q requires P3c", name)
	}
```

改写为：

```go
	if view, ok := wrenMDL.GetView(name); ok {
		return viewInfoGet(view, analyzedMDL, ctx)
	}
```

注：P3b 任务 11 注释提到「`ctx` 参数 P3a 已在签名里 ... 若带 `_ *base.SessionContext`，本步可保留下划线」。本任务**必须**把第三参数命名为 `ctx`（不是 `_`），因 `viewInfoGet` 用到。若 P3b 落地为 `_`，改为 `ctx`。

- [ ] **Step 5: 运行测试**

Run: `go build ./... && go test ./internal/rewrite/ -run 'TestViewInfo|TestQueryDescriptorOf_View' -v`
Expected: `go build` 通过；5 个测试 PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/rewrite/utils.go internal/rewrite/view_info.go internal/rewrite/query_descriptor.go internal/rewrite/view_info_test.go
git commit -m "feat(p3c): add ViewInfo + parseView; QueryDescriptorOf resolves views (slice 3)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 4 — GenerateViewRewrite 规则

目标：把 P3a 的 `GenerateViewRewrite` 透传桩换成真实规则；视图查询（含嵌套）端到端转绿；P3 收官。

## 任务 7：`GenerateViewRewrite` 真实规则

**Files:**
- Create: `internal/rewrite/generate_view_rewrite.go`
- Modify: `internal/rewrite/passthrough_rules.go`（删除 `GenerateViewRewrite` 桩；删空后整文件可删）
- Test: `internal/rewrite/generate_view_rewrite_test.go`

机械移植 `GenerateViewRewrite.java`（104 行）。结构与 P3a 任务 20 的 `WrenSqlRewrite.Apply` 高度对称（复用 `topoSort` / `getWithQuery` / `applyWith` / `addToGraph` 模式），**关键差异**：(a) 顶级种子是 `analysis.Views()` 而非 models；(b) `addToGraph` 在加边前用 `wrenMDL.GetView` 过滤 —— 只有 view 名进入图（model/metric/cumulative 留给 rule 3 `WrenSqlRewrite` 处理）；(c) 不调 `rewriteModelTables`（Java 直接 `WithRewriter(...).process(root)` —— Go `applyWith`）。

- [ ] **Step 1: 先写失败测试 `generate_view_rewrite_test.go`**

```go
package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestGenerateViewRewrite_ExpandsViewOnModel(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT custkey FROM useModel")
	out, err := (&GenerateViewRewrite{}).Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	if !strings.Contains(got, `"useModel"`) {
		t.Errorf("missing useModel CTE:\n%s", got)
	}
	if !strings.Contains(got, "FROM Orders") && !strings.Contains(got, `FROM "Orders"`) {
		t.Errorf("view CTE body should reference Orders (model expansion is rule 3's job):\n%s", got)
	}
}

func TestGenerateViewRewrite_NestedViewTopologicalOrder(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT custkey FROM useUseMetric")
	out, err := (&GenerateViewRewrite{}).Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	idxUseMetric := strings.Index(got, `"useMetric"`)
	idxUseUseMetric := strings.Index(got, `"useUseMetric"`)
	if idxUseMetric < 0 || idxUseUseMetric < 0 {
		t.Fatalf("both useMetric and useUseMetric CTEs expected:\n%s", got)
	}
	if idxUseMetric >= idxUseUseMetric {
		t.Errorf("useMetric CTE must precede useUseMetric (risk #1):\n%s", got)
	}
}

func TestGenerateViewRewrite_PassThroughNoView(t *testing.T) {
	wrenMDL := viewenumMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT custkey FROM Orders")
	out, err := (&GenerateViewRewrite{}).Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// No view referenced: should be byte-identical to input (modulo formatter).
	if got, want := formatter.FormatSQL(out), formatter.FormatSQL(stmt); got != want {
		t.Errorf("non-view query changed:\n got %q\nwant %q", got, want)
	}
}
```

Run: `go test ./internal/rewrite/ -run TestGenerateViewRewrite -v`
Expected: 编译失败 —— 桩的 `Apply` 不重写，`ExpandsViewOnModel` / `NestedViewTopologicalOrder` 失败；`PassThroughNoView` 偶然通过。

- [ ] **Step 2: 从 `passthrough_rules.go` 删除 `GenerateViewRewrite` 桩**

任务 3 完成后，`passthrough_rules.go` 只剩 `GenerateViewRewrite` 一个桩。删除该结构体及其 `Apply` 与上方注释。**删空后整个 `passthrough_rules.go` 不再有任何类型** —— 文件只剩 `package rewrite` 和 import。**直接删除整个文件**：`rm internal/rewrite/passthrough_rules.go`。

- [ ] **Step 3: 写 `generate_view_rewrite.go`**

```go
package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// GenerateViewRewrite expands MDL view references into WITH CTEs.
// Mirrors Java io.wren.base.sqlrewrite.GenerateViewRewrite.
type GenerateViewRewrite struct{}

// Apply runs StatementAnalyzer, then turns every directly-referenced view (and
// every nested-view dependency) into a WITH CTE, topologically ordered so a
// view is defined after the views it references. Mirrors GenerateViewRewrite.apply.
//
// Unlike WrenSqlRewrite this rule does NOT rewrite Table references — it just
// prepends view CTEs; the original "FROM useModel" then resolves to the CTE.
func (r *GenerateViewRewrite) Apply(root ast.Statement, ctx *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	wrenMDL := analyzedMDL.WrenMDL()

	analysis := analyzer.NewAnalysis(root)
	if _, err := analyzer.Analyze(analysis, root, ctx, wrenMDL); err != nil {
		return nil, err
	}

	// seed: directly-referenced views (name-sorted by Analysis.AddViews)
	var viewDescriptors []QueryDescriptor
	for _, view := range analysis.Views() {
		info, err := viewInfoGet(view, analyzedMDL, ctx)
		if err != nil {
			return nil, err
		}
		viewDescriptors = append(viewDescriptors, info)
	}
	if len(viewDescriptors) == 0 {
		return root, nil // no view referenced: pass through
	}

	// DAG: vertices = view names (only); edges = requiredView -> view
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
		for _, req := range d.RequiredObjects() { // name-sorted by WrenObjectNames
			if _, isView := wrenMDL.GetView(req); !isView {
				continue // risk #8: only views become graph vertices/edges here
			}
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
	for _, d := range viewDescriptors {
		if err := addToGraph(d); err != nil {
			return nil, err
		}
	}

	order, err := topoSort(vertices, edges)
	if err != nil {
		return nil, fmt.Errorf("found cycle in view: %w", err)
	}

	var withQueries []ast.WithQuery
	for _, name := range order {
		d, ok := descriptorMap[name]
		if !ok {
			return nil, fmt.Errorf("%s not found in query descriptors", name)
		}
		withQueries = append(withQueries, getWithQuery(d)) // risk #7: delimited CTE name
	}

	return applyWith(root, withQueries), nil // risk #2: applyWith prepends; rule 3 WrenSqlRewrite later prepends model CTEs
}
```

注：`contains` / `topoSort` / `getWithQuery` / `applyWith` / `QueryDescriptorOf` 均为 P3a / P3b 既有；`viewInfoGet` 来自任务 6。结构与 P3a `wren_sql_rewrite.go:Apply` 高度对称，差异已在文件头注释与 `addToGraph` 内的 `isView` 过滤注明。

`planner.go` 的 `AllRules` 引用 `&GenerateViewRewrite{}` —— 删桩 + 新建后该名字解析到本任务新建的真实类型，**`planner.go` 无需改**。

- [ ] **Step 4: 运行测试**

Run: `go build ./... && go test ./internal/rewrite/ -run TestGenerateViewRewrite -v`
Expected: `go build` 通过（`passthrough_rules.go` 已删，无未定义符号）；3 个测试 PASS。

`TestGenerateViewRewrite_NestedViewTopologicalOrder` 验证风险 #1：`useMetric` CTE 必须排在 `useUseMetric` 之前。若失败 → `topoSort` 的 tie-break 不符 → 调 P3a `graph.go` 的 `topoTieBreak`（仅此一处函数）。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/generate_view_rewrite.go internal/rewrite/generate_view_rewrite_test.go
git rm internal/rewrite/passthrough_rules.go
git commit -m "feat(p3c): real GenerateViewRewrite rule, drop pass-through stubs (slice 4)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 8：端到端 —— 视图查询转绿 + P3 收官

**Files:**
- Modify: `testdata/difftest/baseline.json`
- 可能 Modify: `internal/rewrite/graph.go`（仅当 CTE 顺序逆向校验不符时调 `topoTieBreak`）

- [ ] **Step 1: 跑差分测试，看视图查询状态**

Run: `make difftest`
Expected: 22 条标准查询 + `m_*` + `met_*` + `metric/*` + `viewenum/enum` + `tpch/v_enum` 无回归；`viewenum/view_on_model`、`viewenum/view_on_metric`、`viewenum/view_rollup`、`viewenum/view_nested` 由 `fail` 翻 `pass`（或仍 `fail`，进 Step 2）；`tpch/v_use_*` 4 条由 `fail` 变 `go-error`（Go 已能识别视图并尝试展开，但传递性拉入含 Jinja 列的 `Customer` 模型 → `parseExpression` 失败 → Go 报错，**属预期已知失败**，与 P3b `met_*` 同因）。

- [ ] **Step 2: 逐条排查未转绿的合成视图查询**

对每个仍 `fail` 的 `viewenum/view_*`，`make difftest` 日志给出首个 token 差异。常见根因：

- **CTE 顺序不符（风险 #1 / #2）**：`viewenum/view_nested` 含 `Orders` / `Revenue` / `useMetric` / `useUseMetric` 4 个 CTE。golden 序应为 `[Orders, Revenue, useMetric, useUseMetric]`（model 在前、view 在后；view 内嵌套排序）。若 Go 与 golden 不同 → `cat testdata/difftest/golden/viewenum/view_nested.sql` 看 Java 实际顺序，再调 P3a `graph.go` 的 `topoTieBreak`（仅此一处函数；P3a 已把平局规则隔离于此）。注意 `GenerateViewRewrite` 的图只含 view 顶点，model CTE 由 `WrenSqlRewrite`（rule 3）在下一轮独立排序 —— 两轮拼接顺序由 `applyWith` 的 `Stream.concat` 语义保证。
- **`useMetricRollUp` 内部 rollup 子查询模板（风险 #6）**：对照任务 6 `view_rollup.sql` golden 与 P3b `cases/metric/queries/rollup.sql` golden 的子查询体应**字节一致**（同一 metric、同一 unit）。若不一致 → P3b `getMetricRollupSql` 模板 token 错，回到 P3b 任务 14 修复（应已通过 P3b 任务 15 的 baseline 验证）。
- **delimited CTE 名（风险 #7）**：CTE 名应是 `"useModel"`（带引号）。若 Go 输出 `useModel`（无引号） → `getWithQuery` 未把 identifier 标 delimited，回 P3a 任务 14 排查。
- **`go-error` 而非 `fail`**：Go 端在 `viewInfoGet` 的 `Analyze` 阶段就报错（合成 MDL 不应有 Jinja，但若误写 → 改 `cases/viewenum/mdl.json` Step 2）。

- [ ] **Step 3: 确认 `tpch/v_use_*` 为已知失败**

`make difftest` 日志里 `tpch/v_use_model` 等应为 `go-error`，错误信息指向 Jinja 列解析失败（`Customer` 模型的 `custkey_name = {{ concat(...) }}`）。这是预期的 P6 依赖，非回归。`tpch/v_enum` 应仍 `pass`（任务 4 已转绿）。

- [ ] **Step 4: 接受 baseline**

合成视图查询转绿后：

Run: `make difftest-accept`
`git diff testdata/difftest/baseline.json` 确认：
- `viewenum/view_on_model`、`viewenum/view_on_metric`、`viewenum/view_rollup`、`viewenum/view_nested` 变 `pass`；
- `tpch/v_use_model`、`tpch/v_use_metric`、`tpch/v_use_rollup`、`tpch/v_use_nested` 由 `fail` 变 `go-error`（横向移动，仍为红、文档化 P6）；
- 既有 `pass` 无 `pass→fail` 回归（特别确认：22 条标准查询、`m_*`、`metric/*`、`viewenum/enum`、`tpch/v_enum`）。

- [ ] **Step 5: Commit**

```bash
git add testdata/difftest/baseline.json internal/rewrite/graph.go
git commit -m "test(p3c): view queries pass differential test — P3 closure (slice 4)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 收尾

## 任务 9：终验与文档

**Files:**
- Modify: `internal/rewrite/README.md`（补 P3c 段落与 P3 收官小结）
- Modify: `internal/difftest/README.md`（补 viewenum 语料与 P3 状态说明）

- [ ] **Step 1: 更新 `internal/rewrite/README.md`**

在 P3a / P3b 已写段落后追加 P3c 段落：

- `EnumRewrite` 与 Java 对应（`BaseRewriter` → `RewriteNode` 钩子；两段 DereferenceExpression → StringLiteral；严格大小写匹配；值字面量回退由 `dto.EnumValue.GetValue()` 提供）。
- `ViewInfo` 与 Java 对应（`parseView` → `StatementAnalyzer.analyze` → 套 `MetricRollupRewrite` → `requiredObjects = WrenObjectNames`）。
- `GenerateViewRewrite` 与 Java 对应（`analysis.Views()` 种子 → DAG 仅含 view 顶点 → `topoSort` / `applyWith`；不调 `rewriteModelTables`）。
- 规则管线状态：**4 条规则全部为真实规则；`passthrough_rules.go` 已删除**。`AllRules` = `[GenerateViewRewrite, MetricRollupRewrite, WrenSqlRewrite, EnumRewrite]`。
- 「P3 收官口径」一节：直接引用本计划「验收性质说明」§的状态矩阵；点明 `tpch/1`/`tpch/4` 的 oracle-error 与 `m_orders`/`met_*`/`v_use_*` 的传递性 Jinja 是 oracle 限制 + 待 P6，非 Go 缺陷。

- [ ] **Step 2: 更新 `internal/difftest/README.md`**

在「失败分类」一节补：

- `viewenum/` 是合成视图 / 枚举语料组（P3c 端到端字节奇偶证据），全部 `pass`。
- `tpch/v_use_*` 引用 TPC-H 视图、因传递性依赖含 Jinja 列的 `Customer` 模型在 P3c 阶段为已知失败（`go-error`），待 P6 的 Jinja 宏层。
- `tpch/v_enum` 是纯枚举查询（不触模型），P3c 端到端字节转绿，对真 TPC-H MDL 的 EnumRewrite 证据。

「P3 状态」小结：22 标准（20 pass + 2 oracle-error）+ P3a `m_*` + P3b `met_*` + P3b `metric/*` + P3c `viewenum/*` + P3c `tpch/v_*`；P3 重写引擎奇偶校验（非动态字段路径）达成。

- [ ] **Step 3: 终验全套**

Run（逐条须通过）：

```bash
go build ./...
go vet ./...
gofmt -l internal/rewrite/ internal/dto/ internal/mdl/
make test
make difftest
```

Expected：`go build` 通过；`go vet` 无输出；`gofmt -l` 无输出；`make test` 全绿；`make difftest` 无回归、`viewenum/*` 5 条 + `tpch/v_enum` 计分板为 `pass`，`tpch/v_use_*` 为 `go-error`/`fail`。

- [ ] **Step 4: P3 收官验收标准核对（设计 §8 重述）**

逐条勾选：

- [ ] 合成视图 / 枚举语料 `viewenum/view_on_model` / `viewenum/view_on_metric` / `viewenum/view_rollup` / `viewenum/view_nested`（嵌套）/ `viewenum/enum` 端到端 dry-plan golden 字节一致 —— 均 `pass`。
- [ ] `tpch/v_enum` 纯枚举查询对真 TPC-H MDL 端到端字节一致 —— `pass`。
- [ ] `tpch/v_use_*` 4 条 TPC-H 视图查询在 baseline 标记为已知失败（`go-error`），文档化 P6 依赖。
- [ ] P3a / P3b 已通过查询（22 标准 - 2 oracle-error / `m_*` 5 条 / `metric/*` 4 条 / `viewenum/enum` / `tpch/v_enum`）无 `pass→fail` 回归。
- [ ] `passthrough_rules.go` 文件已删除；`AllRules` 4 条规则全部为真实规则。
- [ ] `go build` / `go vet` / `gofmt -l` 均干净。
- [ ] **P3 重写引擎奇偶校验（非动态字段路径）达成**。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/README.md internal/difftest/README.md
git commit -m "docs(p3c): document view/enum rewrite + finalize P3 closure

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## 附录 A — Java ↔ Go 文件对应表

| Java（`wren-base/`） | Go | 任务 | 操作 |
|---|---|---|---|
| `sqlrewrite/EnumRewrite.java` | `internal/rewrite/enum_rewrite.go` | 3 | 新建 |
| `sqlrewrite/analyzer/StatementAnalyzer.java`（顶层 view 收集 `:117-123`） | `internal/rewrite/analyzer/statement_analyzer.go` | 5 | 修改 |
| `sqlrewrite/analyzer/Analysis.java`（`getWrenObjectNames` `:141-149`） | `internal/rewrite/analyzer/analysis.go` | 5 | 修改 |
| `sqlrewrite/Utils.java`（`parseView` `:66-69`） | `internal/rewrite/utils.go` | 6 | 修改 |
| `sqlrewrite/ViewInfo.java` | `internal/rewrite/view_info.go` | 6 | 新建 |
| `sqlrewrite/QueryDescriptor.java`（`of` 的 view 分支） | `internal/rewrite/query_descriptor.go` | 6 | 修改 |
| `sqlrewrite/GenerateViewRewrite.java` | `internal/rewrite/generate_view_rewrite.go` | 7 | 新建 |
| 删除 `GenerateViewRewrite` / `EnumRewrite` 透传桩 | `internal/rewrite/passthrough_rules.go` | 3 / 7 | 删除文件 |
| `internal/rewrite/README.md` | 同 | 9 | 修改 |
| `internal/difftest/README.md` | 同 | 9 | 修改 |

## 附录 B — 不在 P3c 范围（机械移植时遇到须停手）

- `WrenSqlRewrite` 动态字段分支（`isEnableDynamicField()` 为 true）—— `getTableRequiredFields`、`WrenDataLineage`、动态路径的显式 view 注入。P3c 只走非动态分支。
- `analyzer/decisionpoint/`、`CacheAnalysis` —— P5。
- Jinja 宏（`{{ }}` 列表达式）—— P6。TPC-H 视图查询 `tpch/v_use_*` 与 P3a `m_orders` / P3b `met_*` 的端到端转绿待此。

## 附录 C — 与 P3a / P3b 共享 / 复用的产物

P3c 不重新实现以下 P3a / P3b 产物，直接复用：

- P3a `internal/rewrite/utils.go`：`parseSQL` / `parseExpression` / `parseQuery` / `checkArgument` / `contains` / `sortedKeys` / `dereferenceFrom` / `hasPrefixParts`（本计划任务 6 在此加 `parseView`）。
- P3a `internal/rewrite/base_tree_rewriter.go`：`RewriteHook` / `RewriteNode`（EnumRewrite 的核心遍历依赖；尤其 `*ast.DereferenceExpression` case 的 base 递归 + Field 保留语义）。
- P3a `internal/rewrite/query_descriptor.go`：`QueryDescriptor` 接口（任务 6 把 view 分支由 error 改为真实实现）。
- P3a `internal/rewrite/graph.go`：`topoSort` / `topoTieBreak`（嵌套 view DAG 确定性 —— 风险 #1 的逆向校验只调 `topoTieBreak`）。
- P3a `internal/rewrite/with_rewriter.go`：`getWithQuery` / `applyWith`（delimited CTE 名 + 前置语义 —— 风险 #2 / #7）。
- P3a `internal/rewrite/wren_sql_rewrite.go`：`Apply` 不动；P3c `GenerateViewRewrite` 与之结构对称但独立实现。
- P3a `internal/rewrite/analyzer/`：`NewAnalysis` / `Analyze` / `Tables` / `AddViews` / `Views()` / `Models()` / `Metrics()` / `CumulativeMetrics()` / `Scope` builder / `toCatalogSchemaTableName` 等（任务 5 在 `Analyze` 顶层加 view 收集、给 `Analysis` 加 `WrenObjectNames`）。
- P3a `internal/rewrite/planner.go`：`AllRules` / `Rewrite` 编排循环（任务 3 / 7 删桩后 `&EnumRewrite{}` / `&GenerateViewRewrite{}` 自动指向真实规则，无需改 `planner.go`）。
- P3b `internal/rewrite/metric_rollup_rewrite.go`：`MetricRollupRewrite` 真实规则（任务 6 `viewInfoGet` 内部套用 —— 风险 #6）。
- P3b `internal/rewrite/utils.go`：度量解析函数（`getMetricRollupSql` 等）—— `useMetricRollUp` view 体的渲染依赖。
- P3b `internal/rewrite/analyzer/statement_analyzer.go`：metrics / cumulative 收集 + `visitFunctionRelation` 的 `roll_up` 分支（任务 5 在其后续写 view 收集）。
- `internal/dto/enum.go`：`EnumDefinition.ValueOf` / `EnumValue.GetValue`（严格匹配 + value 回退）。
- `internal/dto/view.go`：`View` 结构体（`Name` / `Statement` / `Properties`）。
- `internal/mdl/wren_mdl.go`：`WrenMDL.GetView` / `GetEnumDefinition`。
