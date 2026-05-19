# P3b 重写引擎（度量 / 累积度量 / 度量汇总）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use gpowers:subagent-driven-development (recommended) or gpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 P3a 建立的 `internal/rewrite/` 包内机械移植 Java `wren-engine:0.9.3` 的度量重写路径（`MetricSqlRender` / `CumulativeMetricInfo` / `DateSpineInfo` / `MetricRollupRewrite`），让 Go 引擎对引用度量 / 累积度量 / 度量汇总语法的查询产出与 Java 逐字节一致的重写后 SQL。

**Architecture:** P3b 是 P3a 的延伸，不改 `WrenPlanner` 编排，只填充其调用的组件。把 P3a 的 `MetricRollupRewrite` 透传桩换成真实规则；给 `WrenSqlRewrite` 非动态字段路径补上 metric / cumulative 分支；新增 4 个 SqlRender / Info 类型 + 1 个 analyzer 类型。由外向内分 6 个切片（切片 0 语料 → 切片 1 分析器 → 切片 2 MetricSqlRender → 切片 3 累积 / 日期轴 → 切片 4 WrenSqlRewrite 接线 → 切片 5 MetricRollupRewrite），每切片自成一体并以 P1 差分测试 + baseline 计分板棘轮推进。

**Tech Stack:** Go 1.x；`internal/parser`（P2 字节对齐的 parser/formatter）；`internal/parser/ast`（P2 对齐的 AST）；`internal/difftest`（P1 golden-snapshot 差分框架，支持多语料组）；`internal/dto` / `internal/mdl`（MDL 数据模型）；P3a 的 `internal/rewrite` 包及其 `analyzer` 子包。

---

## 前置条件（必读）

1. **P3a 必须已按其计划（`.gpowers/plans/2026-05-19-parity-p3a-rewrite-models-relationships.md`）执行完毕并合并。** 本计划凡引用 P3a 产物，均指 P3a 完成后的状态：
   - 主包 `internal/rewrite/`（package `rewrite`）：`rule.go` / `planner.go` / `utils.go` / `passthrough_rules.go` / `wren_sql_rewrite.go` / `base_tree_rewriter.go` / `relationship_rewriter.go` / `query_descriptor.go` / `dummy_info.go` / `with_rewriter.go` / `relationable_sql_render.go` / `model_sql_render.go` / `relation_info.go` / `graph.go`。
   - 子包 `internal/rewrite/analyzer/`（package `analyzer`）：`analysis.go` / `statement_analyzer.go` / `scope_analyzer.go` / `expression_analyzer.go` / `expression_relationship_analyzer.go` / `field.go` / `scope.go` 等。
   - 既有 `internal/analyzer`（提供 `SessionContext`）在所有文件中一律以别名 `base` 导入。

2. **本计划要修改若干 P3a 文件**，针对 P3a 完成后的状态。除设计 §4 文件表列出的 4 个（`utils.go` / `wren_sql_rewrite.go` / `passthrough_rules.go` / `analyzer/statement_analyzer.go`）外，移植过程中还必须触及：`analyzer/analysis.go`（加 `metricRollups`）、`relation_info.go`（泛化 `RelationInfo` 以承载度量）、`relationship_rewriter.go`（补 `relationshipAware`，`MetricSqlRender` 依赖）、`query_descriptor.go`（`QueryDescriptorOf` 补度量分支）、`analyzer/utils.go`（`AnalyzeFrom` 补度量分支）。每处修改在对应任务内说明缘由。设计 §4 文件表非穷举。

3. **Java 参照源**位于 `../wren-engine-0.9.3/wren-base/src/main/java/io/wren/base/sqlrewrite/`（含 `analyzer/` 子目录）与 `../wren-engine-0.9.3/wren-base/src/main/java/io/wren/base/dto/`。机械移植 = 逐方法对照，控制流与命名一一对应。

## 验收性质说明（重要 —— 影响切片 4/5 的验证方式）

**TPC-H MDL 的 4 个度量全部经 `Customer` 模型传递性依赖 Jinja `{{ }}` 列**，端到端无法在 P3b 字节转绿（与 P3a 的 `m_orders` 同因，待 P6 的 Jinja 宏层）：

| 度量 | 基 | 阻断路径 |
|---|---|---|
| `Revenue` | model `Orders` | 维度 `customer = customer.name` 关系遍历 → 拉入 `Customer` 模型 CTE → Jinja |
| `CustomerRevenue` | model `Customer` | 直接依赖 `Customer` 模型 → Jinja |
| `CustomerDailyRevenue` | model `Orders` | 维度 `customer.name` 关系遍历 → 拉入 `Customer` → Jinja |
| `WeeklyRevenue`（累积） | model `Orders` | `Orders` 模型计算列 `nation_name = customer.nation.name` → LEFT JOIN `Customer` → Jinja |

因此 P3b 的端到端字节奇偶验证用**两套语料**（用户已确认「合成语料 + TPC-H 已知失败」方案）：

- **合成度量语料组 `cases/metric/`**（任务 1 新建）—— 一份**无 Jinja**的小 MDL，含 metric-on-model / metric-on-metric / cumulative metric / rollup，**P3b 应端到端字节转绿**。这是 P3b 核心的真证据。`internal/difftest` 的 `LoadCorpus` 已支持多语料组（`cases/<group>/`）。
- **TPC-H 度量语料 `cases/tpch/queries/met_*.sql`**（任务 1 追加）—— 引用上表 4 个度量 + 1 条 rollup，捕获 Java golden 作为 P6 目标形态；P3b 阶段在 baseline 记为已知失败（`go-error`，Go 端 Jinja 解析失败），文档化 P6 依赖。

**关系遍历度量列的 join 渲染**（`MetricSqlRender.getCalculatedSubQuery`）因合成语料的度量列无关系遍历、TPC-H 度量列被 Jinja 阻断，改由**任务 8 的合成 MDL 单测**覆盖（不依赖 Docker）。

## 字节分歧风险登记表（贯穿全程，执行时逐条核对）

继承 P3a 全部风险（CTE 顺序由拓扑迭代决定 —— P3a `graph.go` 的 `topoSort`+`topoTieBreak`；`format()` 字符串模板逐字节；Map/Set 迭代序），另加 P3b 特有：

| # | 风险 | 缓解（落在哪个任务） |
|---|---|---|
| 1 | `getCumulativeMetricSql` 大字符串模板 —— 结果经 `parseQuery` 再解析，token 须与 Java 一致（空白可不同）。 | 任务 9：逐字复刻 Java 文本块。 |
| 2 | `GROUP BY` 序号 —— `MetricSqlRender.getQuerySql` 用 `1,…requiredDims.size()`；`getMetricRollupSql` 用 `1,…selectItems.size()-measure.size()`；分隔符 `,` **无空格**。 | 任务 7、9：`strings.Join(ordinals, ",")`。 |
| 3 | `COUNT(*) AS _count_filler` —— `requiredMeasures` 为空才追加；非动态路径度量 measure 恒非空故不触发，但须忠实移植（含 `renderBasedOnMetric` 里「追加到字段、join 局部变量」的 Java 怪癖）。 | 任务 7：`addCountAllIfNeeded` 追加到 `r.selectItems`；`renderBasedOnMetric` join 自己的局部切片。 |
| 4 | metric-on-metric / metric-on-cumulative 走 `renderBasedOnMetric`，模板与 metric-on-model 不同。 | 任务 7：`render()` 三分派；任务 8 单测覆盖三种基。 |
| 5 | `awareModel` 把裸 `Identifier` 改写为 `DereferenceExpression(model, col)`，匹配**大小写不敏感**。 | 任务 7：`awareModelExpr` 钩子用 `strings.EqualFold`。 |
| 6 | rollup → `AliasedRelation` 的别名是**非 delimited** identifier（Java `new Identifier(name)` 单参构造）。 | 任务 14：`Alias: &ast.Identifier{Value: name}`（`Delimited` 默认 false）。 |
| 7 | `date_spine` CTE 注入 —— 非动态路径**不**显式追加，而是经 `CumulativeMetricInfo.RequiredObjects()` 含 `"date_spine"` → 图递归 `QueryDescriptorOf("date_spine")` 自动拉入。 | 任务 11：`QueryDescriptorOf` 认 `"date_spine"`；任务 12 不写显式注入。 |
| 8 | 规则间重解析 —— `MetricRollupRewrite`（规则 2）产出的子查询经 `parseSql(formatSql(...))` 再喂给 `WrenSqlRewrite`（规则 3）。 | 任务 14：依赖 P2 formatter 幂等 + P3a `planner.go` 编排循环。 |

---

# 切片 0 — 度量语料与 golden 冻结

目标：建合成度量语料组、追加 TPC-H 度量已知失败语料、冻结 Java golden、接受 baseline。

## 任务 1：新建合成度量语料组 + 追加 TPC-H 度量语料

**Files:**
- Create: `testdata/difftest/cases/metric/group.json`
- Create: `testdata/difftest/cases/metric/mdl.json`
- Create: `testdata/difftest/cases/metric/queries/metric_on_model.sql`
- Create: `testdata/difftest/cases/metric/queries/metric_on_metric.sql`
- Create: `testdata/difftest/cases/metric/queries/cumulative.sql`
- Create: `testdata/difftest/cases/metric/queries/rollup.sql`
- Create: `testdata/difftest/cases/tpch/queries/met_revenue.sql`
- Create: `testdata/difftest/cases/tpch/queries/met_customer_revenue.sql`
- Create: `testdata/difftest/cases/tpch/queries/met_daily.sql`
- Create: `testdata/difftest/cases/tpch/queries/met_weekly.sql`
- Create: `testdata/difftest/cases/tpch/queries/met_rollup.sql`

说明：`internal/difftest/corpus.go` 的 `LoadCorpus` 按 `root/<group>/{mdl.json,group.json,queries/*.sql}` 扫描每个语料组。新建的 `cases/metric/` 与既有 `cases/tpch/` 平级、互不影响。`cases/tpch/` 的 22 条标准查询 + P3a 的 `m_*.sql` 共用 `cases/tpch/mdl.json`，追加 `met_*.sql` 不改动该 MDL。

- [ ] **Step 1: 写 `cases/metric/group.json`**

对照 `cases/tpch/group.json`（`{"modelingOnly": true}`）—— dry-plan 重写校验。

```json
{
  "modelingOnly": true
}
```

- [ ] **Step 2: 写 `cases/metric/mdl.json` —— 无 Jinja 合成度量 MDL**

一份最小、Java oracle 与 Go 双方均可解析的 MDL：1 个干净模型 `Orders`（无 Jinja、无关系计算列）、metric-on-model `Revenue`、metric-on-metric `RevenueByCustomer`、累积度量 `WeeklyRevenue`。`Revenue` 带 `timeGrain orderdate` 供 rollup 用。

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
        {"name": "orderdate", "type": "date", "expression": "o_orderdate"}
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
    },
    {
      "name": "RevenueByCustomer",
      "baseObject": "Revenue",
      "dimension": [{"name": "custkey", "type": "int4", "expression": "custkey"}],
      "measure": [{"name": "totalprice", "type": "int4", "expression": "sum(totalprice)"}]
    }
  ],
  "cumulativeMetrics": [
    {
      "name": "WeeklyRevenue",
      "baseObject": "Orders",
      "measure": {"name": "totalprice", "type": "int4", "operator": "sum", "refColumn": "totalprice"},
      "window": {"name": "orderdate", "refColumn": "orderdate", "timeUnit": "WEEK", "start": "1994-01-01", "end": "1994-12-31"}
    }
  ],
  "enumDefinitions": [],
  "views": [],
  "macros": []
}
```

- [ ] **Step 3: 写 `cases/metric/queries/` 4 条合成度量查询**

`metric_on_model.sql`：

```sql
select custkey, totalprice from Revenue
```

`metric_on_metric.sql`：

```sql
select custkey, totalprice from RevenueByCustomer
```

`cumulative.sql`：

```sql
select orderdate, totalprice from WeeklyRevenue
```

`rollup.sql`：

```sql
select * from roll_up(Revenue, orderdate, YEAR)
```

- [ ] **Step 4: 写 5 条 TPC-H 度量查询（预期 P6 前已知失败）**

引用 `cases/tpch/mdl.json` 既有度量。`met_revenue.sql`：

```sql
select customer, totalprice from Revenue
```

`met_customer_revenue.sql`：

```sql
select custkey, totalprice from CustomerRevenue
```

`met_daily.sql`：

```sql
select customer, date, totalprice from CustomerDailyRevenue
```

`met_weekly.sql`：

```sql
select orderdate, totalprice from WeeklyRevenue
```

`met_rollup.sql`：

```sql
select * from roll_up(Revenue, orderdate, YEAR)
```

- [ ] **Step 5: 确认语料可被 difftest 加载**

Run: `go test ./internal/difftest/ -run TestLoadCorpus -v`（若无此测试则跑 `go test ./internal/difftest/...`）
Expected: 加载无报错；`metric/*` 与 `tpch/met_*` 出现在用例集中。若 `LoadCorpus` 因缺 `group.json`/`mdl.json` 报错，补齐 Step 1–2 的文件。

- [ ] **Step 6: Commit**

```bash
git add testdata/difftest/cases/metric/ testdata/difftest/cases/tpch/queries/met_*.sql
git commit -m "test(p3b): add synthetic metric corpus + TPC-H metric queries

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 2：捕获 golden + 检视 + 接受 baseline

**Files:**
- Generated: `testdata/difftest/golden/metric/*.sql`、`testdata/difftest/golden/tpch/met_*.sql`（经 `make capture-golden`）
- Modify: `testdata/difftest/baseline.json`（经 `make difftest-accept`）

- [ ] **Step 1: 启动 Java oracle 并捕获 golden**

需 Docker。`tools/capture-golden.sh` 起 `ghcr.io/canner/wren-engine:0.9.3` 容器、对全语料 `GET /v1/mdl/dry-plan` 捕获。

Run: `make capture-golden`
Expected: 控制台逐条 `OK metric/cumulative` … `OK tpch/met_revenue` …；9 条新用例均 `OK`（Java oracle 处理 Jinja，TPC-H 度量查询返回 2xx）。

- [ ] **Step 2: 检视合成度量 golden，确认形态**

阅读 `testdata/difftest/golden/metric/metric_on_model.sql`：应为 `WITH "Orders" AS (...), "Revenue" AS (...) SELECT custkey, totalprice FROM Revenue` 形态（含 WITH 子句、度量 CTE）。这是切片 4 要复刻的目标。把多 CTE 用例（`cumulative.sql` 含 `Orders`/`date_spine`/`WeeklyRevenue` 三 CTE）的 **CTE 排列顺序**记录在任务 13 执行笔记里 —— 风险 #7 与 P3a 风险 #1（`topoTieBreak`）的逆向校验依据。

若某条 `metric/*` golden 是 `.error` 文件（oracle 4xx/5xx），说明合成 MDL 不被 Java 接受 —— 读错误信息修 `cases/metric/mdl.json`（任务 1 Step 2）后重跑 `make capture-golden`。

- [ ] **Step 3: 检视 TPC-H 度量 golden**

阅读 `testdata/difftest/golden/tpch/met_revenue.sql`：应为含 `WITH` 的多 CTE 重写结果（Java 已展开 Jinja，含 `Customer` CTE）。确认是 `.sql` 而非 `.error`。

- [ ] **Step 4: 跑差分测试，接受 baseline**

此刻 Go 端（P3a 状态）对度量查询走透传（`WrenSqlRewrite` 仅处理 model），输出 ≠ golden。

Run: `make difftest`
Expected: 22 条标准查询 + P3a `m_*` 无回归；`metric/*` 与 `tpch/met_*` 均 `fail`（透传未展开度量）。

Run: `make difftest-accept`
然后 `git diff testdata/difftest/baseline.json` 核对：仅新增 `metric/cumulative`、`metric/metric_on_metric`、`metric/metric_on_model`、`metric/rollup`、`tpch/met_customer_revenue`、`tpch/met_daily`、`tpch/met_revenue`、`tpch/met_rollup`、`tpch/met_weekly` 共 9 条，全为 `fail`；既有条目不变。

- [ ] **Step 5: Commit**

```bash
git add testdata/difftest/golden/metric/ testdata/difftest/golden/tpch/met_*.sql testdata/difftest/baseline.json
git commit -m "test(p3b): freeze metric golden + rebaseline

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 1 — 分析器补度量识别

目标：`analyzer` 子包识别查询引用的 metrics / cumulativeMetrics、捕获 `roll_up(...)` 语法为 `MetricRollupInfo`。

## 任务 3：dto / mdl 度量支撑

**Files:**
- Modify: `internal/dto/common.go`（`TimeUnit` 加 `IntervalExpression` / `ParseTimeUnit`）
- Modify: `internal/dto/metric.go`（`Metric.GetTimeGrain`、`Window.ToColumn`、`Measure.ToColumn`）
- Modify: `internal/mdl/wren_mdl.go`（`WrenMDL.GetDateSpine`）
- Test: `internal/dto/metric_support_test.go`

- [ ] **Step 1: 写失败测试 `internal/dto/metric_support_test.go`**

```go
package dto

import "testing"

func TestTimeUnit_IntervalExpression(t *testing.T) {
	cases := map[TimeUnit]string{
		TimeUnitYear: "INTERVAL '1 YEAR'",
		TimeUnitWeek: "INTERVAL '7 DAY'",
		TimeUnitDay:  "INTERVAL '1 DAY'",
	}
	for unit, want := range cases {
		if got := unit.IntervalExpression(); got != want {
			t.Errorf("%s.IntervalExpression() = %q, want %q", unit, got, want)
		}
	}
}

func TestParseTimeUnit(t *testing.T) {
	got, err := ParseTimeUnit("year")
	if err != nil || got != TimeUnitYear {
		t.Fatalf("ParseTimeUnit(year) = %v, %v; want YEAR, nil", got, err)
	}
	if _, err := ParseTimeUnit("decade"); err == nil {
		t.Fatal("ParseTimeUnit(decade): want error, got nil")
	}
}

func TestMetric_GetTimeGrain(t *testing.T) {
	m := Metric{TimeGrain: []TimeGrain{{Name: "orderdate", RefColumn: "orderdate"}}}
	tg, ok := m.GetTimeGrain("orderdate")
	if !ok || tg.RefColumn != "orderdate" {
		t.Fatalf("GetTimeGrain(orderdate) = %v, %v", tg, ok)
	}
	if _, ok := m.GetTimeGrain("missing"); ok {
		t.Fatal("GetTimeGrain(missing): want ok=false")
	}
}
```

Run: `go test ./internal/dto/ -run 'TimeUnit|TimeGrain'`
Expected: 编译失败（`IntervalExpression` / `ParseTimeUnit` / `GetTimeGrain` 未定义）。

- [ ] **Step 2: `internal/dto/common.go` 加 `TimeUnit` 方法**

对照 Java `TimeUnit.getIntervalExpression`（枚举字面量）与 `TimeUnit.timeUnit`（`valueOf(name.toUpperCase())`）。在 `common.go` 顶部 import 块加 `"fmt"` 与 `"strings"`，文件末尾追加：

```go
// IntervalExpression returns the SQL INTERVAL literal for this unit.
// Mirrors Java io.wren.base.dto.TimeUnit.getIntervalExpression.
func (t TimeUnit) IntervalExpression() string {
	switch t {
	case TimeUnitYear:
		return "INTERVAL '1 YEAR'"
	case TimeUnitQuarter:
		return "INTERVAL '3 MONTH'"
	case TimeUnitMonth:
		return "INTERVAL '1 MONTH'"
	case TimeUnitWeek:
		return "INTERVAL '7 DAY'"
	case TimeUnitDay:
		return "INTERVAL '1 DAY'"
	case TimeUnitHour:
		return "INTERVAL '1 HOUR'"
	case TimeUnitMinute:
		return "INTERVAL '1 MINUTE'"
	case TimeUnitSecond:
		return "INTERVAL '1 SECOND'"
	default:
		return ""
	}
}

// ParseTimeUnit resolves a case-insensitive name to a TimeUnit.
// Mirrors Java TimeUnit.timeUnit.
func ParseTimeUnit(name string) (TimeUnit, error) {
	u := TimeUnit(strings.ToUpper(name))
	switch u {
	case TimeUnitYear, TimeUnitQuarter, TimeUnitMonth, TimeUnitWeek,
		TimeUnitDay, TimeUnitHour, TimeUnitMinute, TimeUnitSecond:
		return u, nil
	default:
		return "", fmt.Errorf("no enum constant TimeUnit.%s", name)
	}
}
```

- [ ] **Step 3: `internal/dto/metric.go` 加度量 helper**

对照 Java `Metric.getTimeGrain(String)`、`Window.toColumn`、`Measure.toColumn`。文件末尾追加：

```go
// GetTimeGrain returns the time grain named name. Mirrors Java Metric.getTimeGrain(String).
func (m Metric) GetTimeGrain(name string) (TimeGrain, bool) {
	for _, tg := range m.TimeGrain {
		if tg.Name == name {
			return tg, true
		}
	}
	return TimeGrain{}, false
}

// ToColumn projects the window into a timestamp Column. Mirrors Java Window.toColumn.
func (w Window) ToColumn() Column {
	return Column{Name: w.Name, Type: "TIMESTAMP", Expression: w.RefColumn, Properties: w.Properties}
}

// ToColumn projects the measure into a Column. Mirrors Java Measure.toColumn.
func (ms Measure) ToColumn() Column {
	return Column{Name: ms.Name, Type: ms.Type, Expression: ms.RefColumn, Properties: ms.Properties}
}
```

- [ ] **Step 4: `internal/mdl/wren_mdl.go` 加 `GetDateSpine`**

对照 Java `WrenMDL.getDateSpine`。`internal/dto/manifest.go` 在 MDL 装载时已把空 `DateSpine` 兜底为 `DefaultDateSpine()`，故直接返回 manifest 字段即可。在 `wren_mdl.go` 末尾追加：

```go
// GetDateSpine returns the manifest's date spine (defaulted at manifest load).
// Mirrors Java WrenMDL.getDateSpine.
func (m *WrenMDL) GetDateSpine() dto.DateSpine {
	return m.manifest.DateSpine
}
```

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/dto/ -run 'TimeUnit|TimeGrain' && go build ./...`
Expected: 测试 PASS；`go build` 通过。

- [ ] **Step 6: Commit**

```bash
git add internal/dto/common.go internal/dto/metric.go internal/dto/metric_support_test.go internal/mdl/wren_mdl.go
git commit -m "feat(p3b): add TimeUnit/Metric/WrenMDL metric support helpers (slice 1)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 4：`MetricRollupInfo` 类型 + `Analysis` 加 `metricRollups`

**Files:**
- Create: `internal/rewrite/analyzer/metric_rollup_info.go`
- Modify: `internal/rewrite/analyzer/analysis.go`

- [ ] **Step 1: 写 `analyzer/metric_rollup_info.go`**

机械移植 `analyzer/MetricRollupInfo.java`（51 行）。Java 有 `metric` / `timeGrain` / `timeUnit` 三字段与 getter（`getDatePart()` 返回 `timeUnit`）。Go 用导出字段。

```go
package analyzer

import "github.com/wren-engine/wren/internal/dto"

// MetricRollupInfo describes a roll_up(metric, timeColumn, timeUnit) call.
// Mirrors Java io.wren.base.sqlrewrite.analyzer.MetricRollupInfo.
type MetricRollupInfo struct {
	Metric    *dto.Metric
	TimeGrain dto.TimeGrain
	TimeUnit  dto.TimeUnit // Java getDatePart()
}
```

- [ ] **Step 2: `analyzer/analysis.go` 加 `metricRollups` 字段与访问器**

对照 `Analysis.java` 的 `metricRollups`（Java `Map<NodeRef<Node>, MetricRollupInfo>`）。P3a 的 `Analysis` 已有 `metrics`/`cumulativeMetrics` 切片与 `AddMetrics`/`AddCumulativeMetrics`/`Metrics()`/`CumulativeMetrics()`（P3a 任务 7）；本步只补 `metricRollups`。

在 `Analysis` 结构体加字段：

```go
	metricRollups map[ast.NodeRef]*MetricRollupInfo
```

在 `NewAnalysis` 的初始化里加：

```go
		metricRollups: map[ast.NodeRef]*MetricRollupInfo{},
```

文件内追加访问器（对照 `Analysis.addMetricRollups` / `getMetricRollups`）：

```go
// AddMetricRollups registers a roll_up node's info, keyed by node identity.
// Mirrors Analysis.addMetricRollups.
func (a *Analysis) AddMetricRollups(n ast.Node, info *MetricRollupInfo) {
	a.metricRollups[ast.NodeRef{Node: n}] = info
}

// MetricRollups returns the node-ref -> info map. Mirrors Analysis.getMetricRollups.
func (a *Analysis) MetricRollups() map[ast.NodeRef]*MetricRollupInfo {
	return a.metricRollups
}

// GetMetricRollup looks up a roll_up node's info by identity.
func (a *Analysis) GetMetricRollup(n ast.Node) (*MetricRollupInfo, bool) {
	info, ok := a.metricRollups[ast.NodeRef{Node: n}]
	return info, ok
}
```

- [ ] **Step 3: 编译**

Run: `go build ./internal/rewrite/analyzer/...`
Expected: 通过。

- [ ] **Step 4: Commit**

```bash
git add internal/rewrite/analyzer/metric_rollup_info.go internal/rewrite/analyzer/analysis.go
git commit -m "feat(p3b): add MetricRollupInfo + Analysis.metricRollups (slice 1)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 5：`StatementAnalyzer` 补度量识别 + `roll_up` 捕获

**Files:**
- Modify: `internal/rewrite/analyzer/statement_analyzer.go`
- Modify: `internal/rewrite/analyzer/utils.go`（`AnalyzeFrom` 补度量分支）
- Test: `internal/rewrite/analyzer/metric_analyzer_test.go`

说明：P3a 机械移植了 `StatementAnalyzer.java` 整个文件，但因 P3b 类型缺失（`MetricRollupInfo`、`dto.Window.ToColumn`），其顶层 `Analyze` 的 metrics/cumulative 收集、`visitFunctionRelation` 的 `roll_up` 分支、`collectFieldFromMDL` 的 metric/cumulative 分支很可能被留空 / 桩 / throw。本任务补全这些分支并对齐 Java。**若 P3a 已写对，则核对一致即可，不重复。**

- [ ] **Step 1: 顶层 `Analyze` 补 metrics / cumulativeMetrics 收集**

对照 `StatementAnalyzer.java:96-124`。P3a 的 `Analyze` 已做 `AddModels`。在其后、`return queryScope` 前补 metrics（含 `metricInMetricRollups` 去重检查 `:102-107`）与 cumulativeMetrics 收集。`analysis.Tables()` 元素为 `CatalogSchemaTableName`；逐表查 `wrenMDL.GetMetric` / `GetCumulativeMetric`。

```go
	// metrics referenced as plain tables
	var metrics []*dto.Metric
	for _, t := range analysis.Tables() {
		if m, ok := wrenMDL.GetMetric(t.Table); ok {
			metrics = append(metrics, m)
		}
	}
	// a metric must not appear both as a table and as a rollup target
	rollupMetrics := map[string]bool{}
	for _, info := range analysis.MetricRollups() {
		rollupMetrics[info.Metric.Name] = true
	}
	for _, m := range metrics {
		if rollupMetrics[m.Name] {
			return nil, fmt.Errorf("duplicate metrics in metrics and metric rollups")
		}
	}
	analysis.AddMetrics(metrics)

	var cumulativeMetrics []*dto.CumulativeMetric
	for _, t := range analysis.Tables() {
		if cm, ok := wrenMDL.GetCumulativeMetric(t.Table); ok {
			cumulativeMetrics = append(cumulativeMetrics, cm)
		}
	}
	analysis.AddCumulativeMetrics(cumulativeMetrics)
```

注：Java `wrenMDL.getMetric(CatalogSchemaTableName)` 内含 catalog/schema 比对；P3a 的 `analysis.Tables()` 里的 `CatalogSchemaTableName` 已是 `toCatalogSchemaTableName` 结果。严格对齐时按 `t.Catalog == wrenMDL.Catalog() && t.Schema == wrenMDL.Schema()` 过滤后再 `GetMetric(t.Table)`；若 P3a 的 `AddModels` 那段已用同样过滤模式，复用之。`AddMetrics`/`AddCumulativeMetrics` 内部按名排序去重（P3a 任务 7 已保证）。

- [ ] **Step 2: `visitFunctionRelation` 补 `roll_up` 分支**

对照 `StatementAnalyzer.java:452-482`。P3a 的 `visitFunctionRelation` 已有 `DUCKDB_TABLE_FUNCTIONS` 分支与 else-throw。在方法**最前**插入 `roll_up` 分支（名字大小写不敏感比较）：

```go
	if strings.EqualFold(node.Name.String(), "roll_up") {
		args := node.Arguments
		if err := checkArgument(len(args) == 3, "rollup function should have 3 arguments"); err != nil {
			return nil, err
		}
		tableName := ast.GetQualifiedName(args[0])
		if err := checkArgument(tableName != nil, "'%v' cannot be resolved", args[0]); err != nil {
			return nil, err
		}
		timeId, ok1 := args[1].(*ast.Identifier)
		if err := checkArgument(ok1, "'%v' cannot be resolved", args[1]); err != nil {
			return nil, err
		}
		unitId, ok2 := args[2].(*ast.Identifier)
		if err := checkArgument(ok2, "'%v' cannot be resolved", args[2]); err != nil {
			return nil, err
		}
		cstn, err := toCatalogSchemaTableName(v.ctx, *tableName)
		if err != nil {
			return nil, err
		}
		var metric *dto.Metric
		if cstn.Catalog == v.wrenMDL.Catalog() && cstn.Schema == v.wrenMDL.Schema() {
			if m, found := v.wrenMDL.GetMetric(cstn.Table); found {
				metric = m
			}
		}
		if metric == nil {
			return nil, fmt.Errorf("Metric not found: %s.%s.%s", cstn.Catalog, cstn.Schema, cstn.Table)
		}
		timeGrain, found := metric.GetTimeGrain(timeId.Value)
		if !found {
			return nil, fmt.Errorf("Time column not found in metric: %s", timeId.Value)
		}
		unit, err := dto.ParseTimeUnit(unitId.Value)
		if err != nil {
			return nil, err
		}
		v.analysis.AddMetricRollups(node, &MetricRollupInfo{Metric: metric, TimeGrain: timeGrain, TimeUnit: unit})
		return scopeBuilderWithParent(scope).Build(), nil
	}
```

注：`checkArgument` 在主包 `internal/rewrite/utils.go`，`analyzer` 子包不可见 —— 若 P3a 没在 `analyzer` 包内提供等价物，本步用就地 `if !cond { return nil, fmt.Errorf(...) }` 替代，逻辑等价。`node.Name` 是 `ast.QualifiedName`，`.String()` 取其文本。`scopeBuilderWithParent`/`ScopeBuilderWithParent` 以 P3a 实际命名为准（P3a `scope.go` 的 builder）。`v.ctx`/`v.wrenMDL`/`v.analysis` 字段名以 P3a `stmtVisitor` 实际命名为准。

- [ ] **Step 3: `collectFieldFromMDL` 补 metric / cumulativeMetric 分支**

对照 `StatementAnalyzer.java:274-323`。P3a 的 `collectFieldFromMDL` 应已有 model 分支；补 metric（用 `metric.GetColumns()`）与 cumulativeMetric（用 `cumulativeMetric.Window.ToColumn()` 与 `Measure.ToColumn()` 各造 1 个 `Field`）两分支。逐字段对照 Java 的 `Field.builder()`（`tableName` / `columnName` / `name` / `sourceModelName` / `sourceColumn`）。若 P3a 已写则核对。

- [ ] **Step 4: `analyzer/utils.go` 的 `AnalyzeFrom` 补 metric 分支**

对照 `Utils.java:295-300`。P3a 的 `AnalyzeFrom` 注释处（`// metrics: 同理遍历 dimension+measure`）补实现：对每个被引用的 metric，遍历 `dimension`+`measure` 列造 `Field`。`Utils.analyzeFrom` 不处理 cumulative metric —— 保持与 Java 一致，不补 cumulative 分支。

```go
	for _, metric := range sortedMetrics(wrenMDL) {
		if !usedContains(used, metric.Name) {
			continue
		}
		for i := range metric.Dimension {
			fields = append(fields, toField(wrenMDL, metric.Name, &metric.Dimension[i], used))
		}
		for i := range metric.Measure {
			fields = append(fields, toField(wrenMDL, metric.Name, &metric.Measure[i], used))
		}
	}
```

`sortedMetrics(wrenMDL)` 对 `wrenMDL` 的 metric 按名排序（与 P3a `sortedModels` 同模式 —— map 遍历必须先排序，否则 `fields` 顺序不定）。若 `mdl.WrenMDL` 无 `ListMetrics()`，本步在 `analyzer/utils.go` 内用 `wrenMDL.Manifest().Metrics` 取得切片再排序：`sortedMetrics` 返回 `[]*dto.Metric`。

- [ ] **Step 5: 写集成测试 `metric_analyzer_test.go`**

复用 P3a `analyzer_test.go` 的 `loadTPCH` helper（同包，`../../../testdata/difftest/cases/tpch/mdl.json`）。

```go
package analyzer

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser"

	base "github.com/wren-engine/wren/internal/analyzer"
)

func TestAnalyze_IdentifiesMetric(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT customer, totalprice FROM Revenue")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := a.Metrics(); len(got) != 1 || got[0].Name != "Revenue" {
		t.Fatalf("Metrics() = %v, want [Revenue]", got)
	}
	if len(a.Models()) != 0 {
		t.Fatalf("Models() = %v, want [] (Revenue is a metric, not a model)", a.Models())
	}
}

func TestAnalyze_IdentifiesCumulativeMetric(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT orderdate, totalprice FROM WeeklyRevenue")
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := a.CumulativeMetrics(); len(got) != 1 || got[0].Name != "WeeklyRevenue" {
		t.Fatalf("CumulativeMetrics() = %v, want [WeeklyRevenue]", got)
	}
}

func TestAnalyze_CapturesRollup(t *testing.T) {
	wrenMDL := loadTPCH(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT * FROM roll_up(Revenue, orderdate, YEAR)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := NewAnalysis(stmt)
	if _, err := Analyze(a, stmt, ctx, wrenMDL); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := len(a.MetricRollups()); got != 1 {
		t.Fatalf("MetricRollups() has %d entries, want 1", got)
	}
	for _, info := range a.MetricRollups() {
		if info.Metric.Name != "Revenue" || info.TimeUnit != "YEAR" {
			t.Fatalf("rollup info = %+v, want metric=Revenue unit=YEAR", info)
		}
	}
}
```

- [ ] **Step 6: 运行**

Run: `go test ./internal/rewrite/analyzer/... -v`
Expected: 三个新测试 + P3a 既有分析器测试全部 PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/rewrite/analyzer/statement_analyzer.go internal/rewrite/analyzer/utils.go internal/rewrite/analyzer/metric_analyzer_test.go
git commit -m "feat(p3b): analyzer identifies metrics/cumulative/rollup (slice 1)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 2 — MetricSqlRender

目标：把单个度量渲染成 CTE 查询 AST，与 Java 一致。

## 任务 6：泛化 `RelationInfo` + 补 `relationshipAware`

**Files:**
- Modify: `internal/rewrite/relation_info.go`
- Modify: `internal/rewrite/model_sql_render.go`
- Modify: `internal/rewrite/relationship_rewriter.go`

说明：这是 `metric_sql_render.go` 的前置改造。(1) P3a 的 `RelationInfo` 把 `relationable` 硬编码为 `*dto.Model`，而 Java `RelationInfo` 是 model 与 metric 共用的 `QueryDescriptor`（`MetricSqlRender.render()` 也返回 `RelationInfo`）—— 把它泛化为存 `name string`（`RelationInfo` 除 `Name()` 外不用 `relationable`，对齐 Java 仅用 `getName()`）。(2) Java `MetricSqlRender.getSelectItemsExpression` 用 `RelationshipRewriter.relationshipAware`（3 参，前缀版）；`ModelSqlRender` 不用它，故 P3a 可能未移植 —— 本步补上。

- [ ] **Step 1: 泛化 `RelationInfo`（`relation_info.go`）**

把 P3a 的 `RelationInfo` 结构体与构造函数从「存 `*dto.Model`」改为「存 `name string`」：

```go
// RelationInfo is a QueryDescriptor backed by a rendered model/metric query.
// Mirrors Java RelationInfo.
type RelationInfo struct {
	name            string
	requiredObjects []string
	query           *ast.Query
}

func newRelationInfo(name string, requiredObjects []string, query *ast.Query) *RelationInfo {
	return &RelationInfo{name: name, requiredObjects: requiredObjects, query: query}
}

func (r *RelationInfo) Name() string             { return r.name }
func (r *RelationInfo) RequiredObjects() []string { return r.requiredObjects }
func (r *RelationInfo) Query() *ast.Query         { return r.query }
```

`relationInfoOfModel(model, wrenMDL)` 不变（仍调 `newModelSqlRender(model, wrenMDL).render()`）。若 `relation_info.go` 因去掉 `*dto.Model` 而出现未用 import（`dto`），删除该 import。

- [ ] **Step 2: 修 `model_sql_render.go` 的 `newRelationInfo` 调用点**

P3a 的 `model_sql_render.go` 有 1–2 处 `newRelationInfo(<model>, …)`（`render()` 的空列分支与 `render(baseModel)` 末尾，首参是 `*dto.Model`）。把首参由 `*dto.Model` 改成其 `.Name` 字段：

```bash
grep -n 'newRelationInfo(' internal/rewrite/model_sql_render.go
```

逐处把首参 `<modelVar>` 改为 `<modelVar>.Name`（如 `newRelationInfo(baseModel, …)` → `newRelationInfo(baseModel.Name, …)`）。

- [ ] **Step 3: `relationship_rewriter.go` 补 `relationshipAware` + `getRelationshipResultAsDereferenceExpression`**

机械移植 `RelationshipRewriter.java:48-54`（`relationshipAware`）与 `:84-93`（`getRelationshipResultAsDereferenceExpression`）。`relationshipAware` 与 P3a 的 `rewriteRelationship` 共用同一改写钩子，仅替换表 value 的构造方式不同（前缀 vs 末模型）。在 `relationship_rewriter.go` 追加：

```go
// relationshipAware rewrites relationship dereferences in expr to point at the
// joined model under relationablePrefix. Mirrors RelationshipRewriter.relationshipAware.
func relationshipAware(infos []*analyzer.ExpressionRelationshipInfo, relationablePrefix string, expr ast.Expression) ast.Expression {
	replacements := map[string]ast.Expression{}
	for _, info := range infos {
		replacements[info.QualifiedName().String()] = getRelationshipResultAsDereferenceExpression(info, relationablePrefix)
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

// getRelationshipResultAsDereferenceExpression builds "<prefix>.<remainingParts...>"
// as a delimited dereference chain. Mirrors
// RelationshipRewriter.getRelationshipResultAsDereferenceExpression.
func getRelationshipResultAsDereferenceExpression(info *analyzer.ExpressionRelationshipInfo, relationablePrefix string) ast.Expression {
	parts := []ast.Identifier{{Value: relationablePrefix, Delimited: true}}
	for _, p := range info.RemainingParts() {
		parts = append(parts, ast.Identifier{Value: p, Delimited: true})
	}
	return dereferenceFrom(parts)
}
```

注：`dereferenceFrom`（P3a `utils.go`）、`RewriteNode`（P3a `base_tree_rewriter.go`）、`ast.GetQualifiedName`、`analyzer.ExpressionRelationshipInfo` 的 `QualifiedName()`/`RemainingParts()`（P3a `analyzer`）均已存在。若 P3a 已移植了 `relationshipAware`（整文件机械移植的话），核对签名一致即可，不重复。

- [ ] **Step 4: 编译**

Run: `go build ./...`
Expected: 通过（`relationInfoOfMetric` 尚未定义 —— 它在任务 7 的 `metric_sql_render.go`，本任务不引用它，故可编译）。

- [ ] **Step 5: 运行既有测试**

Run: `go test ./internal/rewrite/...`
Expected: P3a 全部单测仍 PASS（`RelationInfo` 泛化是等价改写）。

- [ ] **Step 6: Commit**

```bash
git add internal/rewrite/relation_info.go internal/rewrite/model_sql_render.go internal/rewrite/relationship_rewriter.go
git commit -m "refactor(p3b): generalize RelationInfo, add relationshipAware (slice 2)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 7：`MetricSqlRender`

**Files:**
- Create: `internal/rewrite/metric_sql_render.go`

机械移植 `MetricSqlRender.java`（299 行）。**设计取舍**：Java 用 `RelationableSqlRender` 抽象基类 + 继承；P3a 已选择不做泛型基类（`relationableSqlRender` 的 `relationable` 硬编码 `*dto.Model`）。P3b 顺此选择 —— `metricSqlRender` 是独立结构体，**复用** P3a `relationable_sql_render.go` 的包级类型 `orderedMap` / `newOrderedMap` / `calculatedFieldRelationshipInfo` / `newCalculatedFieldRelationshipInfo` / `subQueryJoinInfo` / `getRelationableAlias`。输出关键的 `format()` 模板与控制流逐行 1:1 移植，结构体布局是基础设施、允许惯用。`MetricSqlRender` 的 `render(Model)` 与 `ModelSqlRender` 的 `render(Model)` 在 Java 里就是各自独立的私有方法（不共享），故 Go 端独立结构体不损失保真度。

- [ ] **Step 1: 写 `metric_sql_render.go` —— 结构体与构造函数**

`MetricSqlRender` 的 2 参构造（非动态路径用）：`requiredDims` = 全部 dimension 名，`requiredMeasures` = 全部 measure 名。`refSql` = `initRefSql` = `SELECT * FROM "<baseObject>"`。`requiredObjects` 初值含 `baseObject`（对照 `RelationableSqlRender` 构造器 `:51-60`；`dto.Metric.BaseObject` 是原始字段、恒非空）。

```go
package rewrite

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/formatter"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"
)

// metricSqlRender renders a Metric into a CTE query. Mirrors Java
// io.wren.base.sqlrewrite.MetricSqlRender (extends RelationableSqlRender).
type metricSqlRender struct {
	relationable     *dto.Metric
	mdl              *mdl.WrenMDL
	refSql           string
	requiredObjects  map[string]bool
	selectItems      []string
	calculatedRequiredRelationshipInfos []*calculatedFieldRelationshipInfo
	calculatedScopeSelectItems          *orderedMap
	requiredDims     map[string]bool
	requiredMeasures map[string]bool
}

// newMetricSqlRender mirrors the 2-arg MetricSqlRender(Metric, WrenMDL) ctor.
func newMetricSqlRender(metric *dto.Metric, wrenMDL *mdl.WrenMDL) *metricSqlRender {
	r := &metricSqlRender{
		relationable:               metric,
		mdl:                        wrenMDL,
		requiredObjects:            map[string]bool{},
		calculatedScopeSelectItems: newOrderedMap(),
		requiredDims:               map[string]bool{},
		requiredMeasures:           map[string]bool{},
	}
	r.refSql = r.initRefSql()
	if metric.BaseObject != "" {
		r.requiredObjects[metric.BaseObject] = true
	}
	for _, c := range metric.Dimension {
		r.requiredDims[c.Name] = true
	}
	for _, c := range metric.Measure {
		r.requiredMeasures[c.Name] = true
	}
	return r
}

// initRefSql mirrors MetricSqlRender.initRefSql (:81-84).
func (r *metricSqlRender) initRefSql() string {
	return fmt.Sprintf(`SELECT * FROM "%s"`, r.relationable.BaseObject)
}

// isRequiredColumn mirrors MetricSqlRender.isRequiredColumn (:286-289).
func (r *metricSqlRender) isRequiredColumn(name string) bool {
	return r.requiredDims[name] || r.requiredMeasures[name]
}

// addCountAllIfNeeded mirrors MetricSqlRender.addCountAllIfNeeded (:293-298).
// Appends to the struct field selectItems (as the Java field does). Inert on the
// non-dynamic path (metric measures are always present), kept for fidelity.
func (r *metricSqlRender) addCountAllIfNeeded() {
	if len(r.requiredMeasures) == 0 {
		r.selectItems = append(r.selectItems, "COUNT(*) AS _count_filler")
	}
}
```

- [ ] **Step 2: 写 `render()` 三分派 + `renderBasedOnMetric`**

对照 `MetricSqlRender.render()`（`:86-105`）与 `renderBasedOnMetric`（`:107-120`）。`render()` 按 baseObject 是 model / metric / cumulative metric 三分派。

```go
// render dispatches on the metric's base object. Mirrors MetricSqlRender.render().
func (r *metricSqlRender) render() (*RelationInfo, error) {
	base := r.relationable.BaseObject
	if model, ok := r.mdl.GetModel(base); ok {
		return r.renderOnModel(model)
	}
	if metric, ok := r.mdl.GetMetric(base); ok {
		return r.renderBasedOnMetric(metric.Name)
	}
	if cm, ok := r.mdl.GetCumulativeMetric(base); ok {
		return r.renderBasedOnMetric(cm.Name)
	}
	return nil, fmt.Errorf("invalid metric, cannot render metric sql")
}

// renderBasedOnMetric handles metric-on-metric / metric-on-cumulative.
// Mirrors MetricSqlRender.renderBasedOnMetric. NOTE: Java joins a *local*
// selectItems while addCountAllIfNeeded mutates the field — a latent quirk that
// is inert here (measures always present). Ported 1:1: local slice + field append.
func (r *metricSqlRender) renderBasedOnMetric(metricName string) (*RelationInfo, error) {
	var selectItems []string
	for _, c := range r.relationable.GetColumns() {
		if !r.isRequiredColumn(c.Name) {
			continue
		}
		selectItems = append(selectItems, fmt.Sprintf(`%s AS "%s"`, c.GetExpression(), c.Name))
	}
	r.addCountAllIfNeeded()
	sql := r.getQuerySql(strings.Join(selectItems, ", "), metricName)
	query, err := parseQuery(sql)
	if err != nil {
		return nil, fmt.Errorf("render metric %q: %w", r.relationable.Name, err)
	}
	return newRelationInfo(r.relationable.Name, []string{metricName}, query), nil
}
```

注：Java `column.getSqlExpression()` = Go `dto.Column.GetExpression()`（表达式或带引号列名兜底）。

- [ ] **Step 3: 写 `getQuerySql` / `getModelSubQuerySelectItemsExpression`**

对照 `MetricSqlRender.getQuerySql`（`:122-130`）与 `getModelSubQuerySelectItemsExpression`（`:132-137`，恒返回 `"*"`）。**风险 #2**：`GROUP BY` 序号分隔符 `,` 无空格。

```go
// getQuerySql mirrors MetricSqlRender.getQuerySql.
func (r *metricSqlRender) getQuerySql(selectItemsSql, tableJoinsSql string) string {
	if len(r.requiredDims) == 0 {
		return fmt.Sprintf("SELECT %s FROM %s", selectItemsSql, tableJoinsSql)
	}
	ordinals := make([]string, 0, len(r.requiredDims))
	for i := 1; i <= len(r.requiredDims); i++ {
		ordinals = append(ordinals, strconv.Itoa(i))
	}
	return fmt.Sprintf("SELECT %s FROM %s GROUP BY %s", selectItemsSql, tableJoinsSql, strings.Join(ordinals, ","))
}

// getModelSubQuerySelectItemsExpression mirrors the MetricSqlRender override
// (:132-137): metric model sub-query always projects "*".
func (r *metricSqlRender) getModelSubQuerySelectItemsExpression() string {
	return "*"
}
```

- [ ] **Step 4: 写 `awareModel` —— 裸标识符模型感知改写**

对照 `MetricSqlRender.awareModel`（`:161-179`）。**风险 #5**：裸 `Identifier` 命中 baseModel 列名（**大小写不敏感**）→ 改写为 `DereferenceExpression(model, col)`，两段均 delimited。复用 P3a `dereferenceFrom`。

```go
// awareModelStr mirrors MetricSqlRender.awareModel(String, Model).
func (r *metricSqlRender) awareModelStr(expression string, baseModel *dto.Model) (string, error) {
	expr, err := parseExpression(expression)
	if err != nil {
		return "", err
	}
	return formatter.FormatSQL(r.awareModelExpr(expr, baseModel)), nil
}

// awareModelExpr mirrors MetricSqlRender.awareModel(Expression, Model): a bare
// identifier matching a base-model column (case-insensitive) becomes
// "<model>"."<col>".
func (r *metricSqlRender) awareModelExpr(expression ast.Expression, baseModel *dto.Model) ast.Expression {
	return RewriteNode(expression, func(n ast.Node) (ast.Node, bool) {
		id, ok := n.(*ast.Identifier)
		if !ok {
			return nil, false // descend
		}
		for _, c := range baseModel.Columns {
			if strings.EqualFold(c.Name, id.Value) {
				return dereferenceFrom([]ast.Identifier{
					{Value: baseModel.Name, Delimited: true},
					{Value: id.Value, Delimited: true},
				}), true
			}
		}
		return id, true
	}).(ast.Expression)
}
```

- [ ] **Step 5: 写 `getSelectItemsExpression`**

对照 `MetricSqlRender.getSelectItemsExpression`（`:139-159`）。先 `parseExpression(column.getSqlExpression())` 取关系信息；有关系且有 relationalBase → `relationshipAware`；否则 measure 用原始 `column.Expression`、非 measure 用 `column.GetExpression()`，均经 `awareModel`。

```go
// getSelectItemsExpression mirrors MetricSqlRender.getSelectItemsExpression.
func (r *metricSqlRender) getSelectItemsExpression(column dto.Column, relationableBase string, hasRelationableBase bool) (string, error) {
	isMeasure := false
	for _, m := range r.relationable.Measure {
		if m.Name == column.Name {
			isMeasure = true
			break
		}
	}
	baseModel, ok := r.mdl.GetModel(r.relationable.BaseObject)
	if !ok {
		return "", fmt.Errorf("cannot find model %s", r.relationable.BaseObject)
	}
	expr, err := parseExpression(column.GetExpression())
	if err != nil {
		return "", err
	}
	relInfos, err := analyzer.GetRelationships(expr, r.mdl, baseModel)
	if err != nil {
		return "", err
	}
	if len(relInfos) > 0 && hasRelationableBase {
		newExpr := relationshipAware(relInfos, relationableBase, expr)
		return fmt.Sprintf(`%s AS "%s"`, formatter.FormatSQL(newExpr), column.Name), nil
	}
	if isMeasure {
		if column.Expression == "" {
			return "", fmt.Errorf("measure column must have expression")
		}
		am, err := r.awareModelStr(column.Expression, baseModel)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`%s AS "%s"`, am, column.Name), nil
	}
	am, err := r.awareModelStr(column.GetExpression(), baseModel)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`%s AS "%s"`, am, column.Name), nil
}
```

- [ ] **Step 6: 写 `collectRelationship`**

对照 `MetricSqlRender.collectRelationship`（`:181-208`）。仅处理 required 列；有关系 → 收集 `calculatedFieldRelationshipInfo` + 把关系另一端模型加入 `requiredObjects` + 用 `getRelationableAlias` 作 relationalBase；无关系 → 普通处理并写 `calculatedScopeSelectItems`。

```go
// collectRelationship mirrors MetricSqlRender.collectRelationship.
func (r *metricSqlRender) collectRelationship(column dto.Column, baseModel *dto.Model) error {
	if !r.isRequiredColumn(column.Name) {
		return nil
	}
	expr, err := parseExpression(column.GetExpression())
	if err != nil {
		return err
	}
	relInfos, err := analyzer.GetRelationships(expr, r.mdl, baseModel)
	if err != nil {
		return err
	}
	if len(relInfos) > 0 {
		col := column
		r.calculatedRequiredRelationshipInfos = append(
			r.calculatedRequiredRelationshipInfos, newCalculatedFieldRelationshipInfo(&col, relInfos))
		for _, info := range relInfos {
			for _, rel := range info.Relationships() {
				for _, mn := range rel.Models {
					if mn != baseModel.Name {
						r.requiredObjects[mn] = true
					}
				}
			}
		}
		item, err := r.getSelectItemsExpression(column, getRelationableAlias(baseModel.Name), true)
		if err != nil {
			return err
		}
		r.selectItems = append(r.selectItems, item)
		return nil
	}
	item, err := r.getSelectItemsExpression(column, "", false)
	if err != nil {
		return err
	}
	r.selectItems = append(r.selectItems, item)
	r.calculatedScopeSelectItems.put(column.Name, column.GetExpression())
	return nil
}
```

注：`newCalculatedFieldRelationshipInfo` 取 `*dto.Column`；`relInfos` 类型为 `[]*analyzer.ExpressionRelationshipInfo`，若 P3a 的 `newCalculatedFieldRelationshipInfo` 形参类型与之不符，以 P3a 为准做适配。

- [ ] **Step 7: 写 `getCalculatedSubQuery`**

对照 `MetricSqlRender.getCalculatedSubQuery`（`:210-249`）。度量版与 `ModelSqlRender` 的 to-one/to-many 版不同：把 baseModel 与所有所需关系 LEFT JOIN 成**一个**子查询，required 表达式用 `toDereferenceExpression`（P3a `relationship_rewriter.go`）。**关系 join 条件**用 P3a 的 `qualifiedConditionString`（`utils.go`，对应 Java `relationship.getQualifiedCondition()`）。Java `.distinct()` 对 `Expression`/`Relationship` 去重 —— Go 按格式化串 / 关系名去重。

```go
// getCalculatedSubQuery mirrors MetricSqlRender.getCalculatedSubQuery.
func (r *metricSqlRender) getCalculatedSubQuery(baseModel *dto.Model, infos []*calculatedFieldRelationshipInfo) ([]subQueryJoinInfo, error) {
	if len(infos) == 0 {
		return nil, nil
	}
	var reqExprs []string
	seenExpr := map[string]bool{}
	for _, ci := range infos {
		for _, eri := range ci.expressionRelationship {
			s := formatter.FormatSQL(toDereferenceExpression(eri))
			if !seenExpr[s] {
				seenExpr[s] = true
				reqExprs = append(reqExprs, s)
			}
		}
	}
	var reqRels []*dto.Relationship
	seenRel := map[string]bool{}
	for _, ci := range infos {
		for _, eri := range ci.expressionRelationship {
			for _, rel := range eri.Relationships() {
				if !seenRel[rel.Name] {
					seenRel[rel.Name] = true
					reqRels = append(reqRels, rel)
				}
			}
		}
	}
	joins := ""
	for _, rel := range reqRels {
		cond, err := qualifiedConditionString(rel.Condition)
		if err != nil {
			return nil, err
		}
		joins += fmt.Sprintf("LEFT JOIN \"%s\" ON %s\n", rel.Models[1], cond)
	}
	tableJoins := fmt.Sprintf("\"%s\"\n%s", baseModel.Name, joins)
	alias := getRelationableAlias(baseModel.Name)
	joinCriteria := fmt.Sprintf(`"%s"."%s" = "%s"."%s"`,
		baseModel.Name, baseModel.PrimaryKey, alias, baseModel.PrimaryKey)
	sql := fmt.Sprintf("SELECT \"%s\".\"%s\", %s FROM (%s)",
		baseModel.Name, baseModel.PrimaryKey, strings.Join(reqExprs, ", "), tableJoins)
	return []subQueryJoinInfo{{sql: sql, subqueryAlias: alias, joinCriteria: joinCriteria}}, nil
}
```

注：`calculatedFieldRelationshipInfo` 的字段名（此处用 `expressionRelationship`）以 P3a `relationable_sql_render.go` 实际定义为准。`subQueryJoinInfo` 字段名（`sql`/`subqueryAlias`/`joinCriteria`）同理。`toDereferenceExpression`、`qualifiedConditionString` 为 P3a 产物。

- [ ] **Step 8: 写 `renderOnModel` —— metric-on-model 主路径**

对照 `MetricSqlRender.render(Model baseModel)`（`:251-284`）。

```go
// renderOnModel mirrors MetricSqlRender.render(Model baseModel).
func (r *metricSqlRender) renderOnModel(baseModel *dto.Model) (*RelationInfo, error) {
	// non-relationship, no-expression, required columns
	for _, c := range r.relationable.GetColumns() {
		if c.Relationship == "" && c.Expression == "" && r.isRequiredColumn(c.Name) {
			item, err := r.getSelectItemsExpression(c, "", false)
			if err != nil {
				return nil, err
			}
			r.selectItems = append(r.selectItems, item)
		}
	}
	// non-relationship, with-expression columns
	for _, c := range r.relationable.GetColumns() {
		if c.Relationship == "" && c.Expression != "" {
			if err := r.collectRelationship(c, baseModel); err != nil {
				return nil, err
			}
		}
	}
	r.addCountAllIfNeeded()

	modelSubQuery := fmt.Sprintf(`(SELECT %s FROM (%s) AS "%s") AS "%s"`,
		r.getModelSubQuerySelectItemsExpression(), r.refSql, baseModel.Name, baseModel.Name)

	tableJoinsSql := modelSubQuery
	if len(r.calculatedRequiredRelationshipInfos) > 0 {
		subs, err := r.getCalculatedSubQuery(baseModel, r.calculatedRequiredRelationshipInfos)
		if err != nil {
			return nil, err
		}
		for _, info := range subs {
			tableJoinsSql += fmt.Sprintf("\nLEFT JOIN (%s) AS \"%s\" ON %s", info.sql, info.subqueryAlias, info.joinCriteria)
		}
	}
	tableJoinsSql += "\n"

	query, err := parseQuery(r.getQuerySql(strings.Join(r.selectItems, ", "), tableJoinsSql))
	if err != nil {
		return nil, fmt.Errorf("render metric %q: %w", r.relationable.Name, err)
	}
	return newRelationInfo(r.relationable.Name, sortedKeys(r.requiredObjects), query), nil
}
```

- [ ] **Step 9: 写 `relationInfoOfMetric` —— `RelationInfo.get` 的 metric 分支**

对照 `RelationInfo.get(Relationable, WrenMDL)`（`:49-60`）的 metric 分支。供任务 11/12 调用。

```go
// relationInfoOfMetric renders a metric into a RelationInfo. Mirrors
// RelationInfo.get(Relationable, WrenMDL) for the Metric case.
func relationInfoOfMetric(metric *dto.Metric, wrenMDL *mdl.WrenMDL) (*RelationInfo, error) {
	return newMetricSqlRender(metric, wrenMDL).render()
}
```

- [ ] **Step 10: 编译**

Run: `go build ./...`
Expected: 通过。

- [ ] **Step 11: Commit**

```bash
git add internal/rewrite/metric_sql_render.go
git commit -m "feat(p3b): add MetricSqlRender (slice 2)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 8：`MetricSqlRender` 单测（含合成关系 MDL）

**Files:**
- Test: `internal/rewrite/metric_sql_render_test.go`

说明：用合成 MDL 验证三种基（metric-on-model / metric-on-metric / metric-on-cumulative）与**关系遍历度量列**的 join 渲染（`getCalculatedSubQuery`/`relationshipAware`/`awareModel`）—— 后者因合成语料度量列无关系遍历、TPC-H 度量被 Jinja 阻断，只能靠合成 MDL 单测覆盖。复用 P3a `relationship_rewriter_test.go` 顶部的 `loadTPCHForRewrite` helper（同包共享）。

- [ ] **Step 1: 写 metric-on-model 渲染测试**

用合成 MDL（与 `cases/metric/mdl.json` 同构，但内联构造）。

```go
package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

func metricMDL(t *testing.T) *mdl.WrenMDL {
	t.Helper()
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "test",
		Models: []dto.Model{
			{Name: "Orders", RefSql: "select * from orders", PrimaryKey: "orderkey", Columns: []dto.Column{
				{Name: "orderkey", Type: "int4", Expression: "o_orderkey"},
				{Name: "custkey", Type: "int4", Expression: "o_custkey"},
				{Name: "totalprice", Type: "float8", Expression: "o_totalprice"},
				{Name: "orderdate", Type: "date", Expression: "o_orderdate"},
			}},
		},
		Metrics: []dto.Metric{
			{Name: "Revenue", BaseObject: "Orders",
				Dimension: []dto.Column{{Name: "custkey", Type: "int4", Expression: "custkey"}},
				Measure:   []dto.Column{{Name: "totalprice", Type: "int4", Expression: "sum(totalprice)"}}},
			{Name: "RevenueByCustomer", BaseObject: "Revenue",
				Dimension: []dto.Column{{Name: "custkey", Type: "int4", Expression: "custkey"}},
				Measure:   []dto.Column{{Name: "totalprice", Type: "int4", Expression: "sum(totalprice)"}}},
		},
		CumulativeMetrics: []dto.CumulativeMetric{
			{Name: "WeeklyRevenue", BaseObject: "Orders",
				Measure: dto.Measure{Name: "totalprice", Type: "int4", Operator: "sum", RefColumn: "totalprice"},
				Window:  dto.Window{Name: "orderdate", RefColumn: "orderdate", TimeUnit: dto.TimeUnitWeek, Start: "1994-01-01", End: "1994-12-31"}},
		},
	}
	return mdl.WrenMDLFromManifest(&manifest)
}

func TestMetricSqlRender_OnModel(t *testing.T) {
	wrenMDL := metricMDL(t)
	revenue, _ := wrenMDL.GetMetric("Revenue")
	info, err := relationInfoOfMetric(revenue, wrenMDL)
	if err != nil {
		t.Fatalf("render Revenue: %v", err)
	}
	if !contains(info.RequiredObjects(), "Orders") {
		t.Fatalf("RequiredObjects = %v, want to contain Orders", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{`"Orders"."custkey"`, `sum("Orders"."totalprice")`, `GROUP BY 1`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered Revenue SQL missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 2: 写 metric-on-metric 测试**

```go
func TestMetricSqlRender_OnMetric(t *testing.T) {
	wrenMDL := metricMDL(t)
	m, _ := wrenMDL.GetMetric("RevenueByCustomer")
	info, err := relationInfoOfMetric(m, wrenMDL)
	if err != nil {
		t.Fatalf("render RevenueByCustomer: %v", err)
	}
	if got := info.RequiredObjects(); len(got) != 1 || got[0] != "Revenue" {
		t.Fatalf("RequiredObjects = %v, want [Revenue]", got)
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{`FROM Revenue`, `GROUP BY 1`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered RevenueByCustomer SQL missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 3: 写 metric-on-cumulative 测试**

构造一个 baseObject 为 `WeeklyRevenue`（累积度量）的度量，验证 `renderBasedOnMetric` 的 cumulative 分支：`requiredObjects` 含 `WeeklyRevenue`。

```go
func TestMetricSqlRender_OnCumulative(t *testing.T) {
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "test",
		Models: []dto.Model{
			{Name: "Orders", RefSql: "select * from orders", PrimaryKey: "orderkey", Columns: []dto.Column{
				{Name: "totalprice", Type: "float8", Expression: "o_totalprice"},
				{Name: "orderdate", Type: "date", Expression: "o_orderdate"},
			}},
		},
		CumulativeMetrics: []dto.CumulativeMetric{
			{Name: "WeeklyRevenue", BaseObject: "Orders",
				Measure: dto.Measure{Name: "totalprice", Type: "int4", Operator: "sum", RefColumn: "totalprice"},
				Window:  dto.Window{Name: "orderdate", RefColumn: "orderdate", TimeUnit: dto.TimeUnitWeek, Start: "1994-01-01", End: "1994-12-31"}},
		},
		Metrics: []dto.Metric{
			{Name: "RevenueOnCumulative", BaseObject: "WeeklyRevenue",
				Dimension: []dto.Column{{Name: "orderdate", Type: "date", Expression: "orderdate"}},
				Measure:   []dto.Column{{Name: "totalprice", Type: "int4", Expression: "sum(totalprice)"}}},
		},
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	m, _ := wrenMDL.GetMetric("RevenueOnCumulative")
	info, err := relationInfoOfMetric(m, wrenMDL)
	if err != nil {
		t.Fatalf("render RevenueOnCumulative: %v", err)
	}
	if got := info.RequiredObjects(); len(got) != 1 || got[0] != "WeeklyRevenue" {
		t.Fatalf("RequiredObjects = %v, want [WeeklyRevenue]", got)
	}
}
```

- [ ] **Step 4: 写关系遍历度量列测试（合成关系 MDL）**

构造模型 `A`（含关系列 `b`）、`B`（普通列 `name`）、关系 `AB`（`A` MANY_TO_ONE `B`），度量 `M`（baseObject `A`，维度 `b_name = b.name` 关系遍历）。渲染 `M`，断言输出含 `LEFT JOIN`、`A_relationsub`、`requiredObjects` 含 `B`。这条覆盖 `collectRelationship` 的关系分支 + `getCalculatedSubQuery` + `relationshipAware`。

```go
func TestMetricSqlRender_RelationshipDimension(t *testing.T) {
	manifest := dto.Manifest{
		Catalog: "wren", Schema: "test",
		Models: []dto.Model{
			{Name: "A", RefSql: "select * from a", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "a_id"},
				{Name: "bkey", Type: "int4", Expression: "a_bkey"},
				{Name: "b", Type: "B", Relationship: "AB"},
			}},
			{Name: "B", RefSql: "select * from b", PrimaryKey: "id", Columns: []dto.Column{
				{Name: "id", Type: "int4", Expression: "b_id"},
				{Name: "name", Type: "varchar", Expression: "b_name"},
			}},
		},
		Relationships: []dto.Relationship{
			{Name: "AB", Models: []string{"A", "B"}, JoinType: dto.JoinTypeManyToOne, Condition: "A.bkey = B.id"},
		},
		Metrics: []dto.Metric{
			{Name: "M", BaseObject: "A",
				Dimension: []dto.Column{{Name: "b_name", Type: "varchar", Expression: "b.name"}},
				Measure:   []dto.Column{{Name: "cnt", Type: "int4", Expression: "count(id)"}}},
		},
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	m, _ := wrenMDL.GetMetric("M")
	info, err := relationInfoOfMetric(m, wrenMDL)
	if err != nil {
		t.Fatalf("render M: %v", err)
	}
	if !contains(info.RequiredObjects(), "B") {
		t.Fatalf("RequiredObjects = %v, want to contain B", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{`LEFT JOIN`, `A_relationsub`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered M SQL missing %q:\n%s", want, got)
		}
	}
}
```

- [ ] **Step 5: 运行**

Run: `go test ./internal/rewrite/ -run TestMetricSqlRender -v`
Expected: 4 个测试 PASS。若 token 与预期不符，对照 `MetricSqlRender.java` 对应方法修模板（风险 #2/#3/#5）。

- [ ] **Step 6: Commit**

```bash
git add internal/rewrite/metric_sql_render_test.go
git commit -m "test(p3b): MetricSqlRender unit tests incl. synthetic relationship MDL (slice 2)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 3 — 累积度量 / 日期轴 / Utils 解析函数

目标：累积度量与 `date_spine` 渲染、`roll_up` 子查询 SQL 生成。

## 任务 9：`Utils` 度量解析函数

**Files:**
- Modify: `internal/rewrite/utils.go`

机械移植 `Utils.java` 的度量相关函数：`parseCumulativeMetricSql` / `getCumulativeMetricSql` / `getWindowType` / `createDateSpineQuery` / `parseMetricRollupSql` / `getMetricRollupSql`。**风险 #1**：`getCumulativeMetricSql` 大模板逐字复刻。本任务写的代码引用任务 10 的 `dateSpineName` 常量 —— 同包，编译延后到任务 10 末统一进行。

- [ ] **Step 1: 写 `getWindowType`**

对照 `Utils.java:197-228`。沿 baseObject 找 window refColumn 的列类型；baseObject 为累积度量时递归（window 名须匹配）。

```go
// getWindowType resolves the SQL type of a cumulative metric's window column.
// Mirrors Java Utils.getWindowType.
func getWindowType(cm *dto.CumulativeMetric, wrenMDL *mdl.WrenMDL) (string, error) {
	if model, ok := wrenMDL.GetModel(cm.BaseObject); ok {
		for _, c := range model.Columns {
			if c.Name == cm.Window.RefColumn {
				return c.Type, nil
			}
		}
		return "", fmt.Errorf("window type not found in %s", cm.BaseObject)
	}
	if metric, ok := wrenMDL.GetMetric(cm.BaseObject); ok {
		for _, c := range metric.GetColumns() {
			if c.Name == cm.Window.RefColumn {
				return c.Type, nil
			}
		}
		return "", fmt.Errorf("window type not found in %s", cm.BaseObject)
	}
	if base, ok := wrenMDL.GetCumulativeMetric(cm.BaseObject); ok {
		if base.Window.Name == cm.Window.RefColumn {
			return getWindowType(base, wrenMDL)
		}
		return "", fmt.Errorf("CumulativeMetric measure cannot be window as it is not date/timestamp type")
	}
	return "", fmt.Errorf("window type not found in %s", cm.BaseObject)
}
```

注：Java 用 `findAny()`（无序），Go 用首个名字匹配 —— 列名唯一，等价。Java baseObject 三种都查不到时返回 `Optional.empty()`，调用方 `getCumulativeMetricSql` 再 `orElseThrow(NoSuchElementException)`；Go 直接返回 error，等价。

- [ ] **Step 2: 写 `getCumulativeMetricSql`**

逐字复刻 `Utils.java:135-195` 的模板（**风险 #1**）。结果经 `parseQuery` 再解析，空白不影响 AST，但 token（关键字 / 标识符 / 标点）须一致。

```go
// getCumulativeMetricSql builds the cumulative-metric CTE SQL.
// Mirrors Java Utils.getCumulativeMetricSql. Template kept verbatim (risk #1).
func getCumulativeMetricSql(cm *dto.CumulativeMetric, wrenMDL *mdl.WrenMDL) (string, error) {
	windowType, err := getWindowType(cm, wrenMDL)
	if err != nil {
		return "", err
	}
	const pattern = `select 
  metric_time as %s,
  %s(distinct measure_field) as %s
from 
  (
    select 
      date_trunc('%s', d.metric_time) as metric_time,
      measure_field
    from 
      (%s) d 
      left join (
        select 
          measure_field,
          metric_time
        from (%s) sub1
        where 
          metric_time >= cast('%s' as %s) 
          and metric_time <= cast('%s' as %s)
      ) sub2 on (
        sub2.metric_time <= d.metric_time 
        and sub2.metric_time > %s
      )
    where 
      d.metric_time >= cast('%s' as %s)  
      and d.metric_time <= cast('%s' as %s)   
  ) sub3 
group by 1
order by 1
`
	castingDateSpine := fmt.Sprintf(`select cast(metric_time as %s) as metric_time from "%s"`, windowType, dateSpineName)
	windowRange := fmt.Sprintf("d.metric_time - %s", cm.Window.TimeUnit.IntervalExpression())
	selectFromModel := fmt.Sprintf("select %s as measure_field, %s as metric_time from %s",
		cm.Measure.RefColumn, cm.Window.RefColumn, cm.BaseObject)
	return fmt.Sprintf(pattern,
		cm.Window.Name,
		cm.Measure.Operator,
		cm.Measure.Name,
		string(cm.Window.TimeUnit),
		castingDateSpine,
		selectFromModel,
		cm.Window.Start, windowType,
		cm.Window.End, windowType,
		windowRange,
		cm.Window.Start, windowType,
		cm.Window.End, windowType), nil
}
```

注：Java `cumulativeMetric.getWindow().getTimeUnit().name()`（枚举名 "WEEK"）= Go `string(cm.Window.TimeUnit)`。`dateSpineName` 常量见任务 10。

- [ ] **Step 3: 写 `parseCumulativeMetricSql` / `createDateSpineQuery`**

对照 `Utils.java:120-133`（`parseCumulativeMetricSql`）与 `:324-333`（`createDateSpineQuery`）。

```go
// parseCumulativeMetricSql builds and parses the cumulative-metric CTE query.
// Mirrors Java Utils.parseCumulativeMetricSql.
func parseCumulativeMetricSql(cm *dto.CumulativeMetric, wrenMDL *mdl.WrenMDL) (*ast.Query, error) {
	sql, err := getCumulativeMetricSql(cm, wrenMDL)
	if err != nil {
		return nil, err
	}
	q, err := parseQuery(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cumulative metric sql for %q: %w", cm.Name, err)
	}
	return q, nil
}

// createDateSpineQuery builds the date-spine CTE query. Mirrors Java
// Utils.createDateSpineQuery (uses the BigQuery GENERATE_TIMESTAMP_ARRAY form).
func createDateSpineQuery(ds dto.DateSpine) (*ast.Query, error) {
	sql := fmt.Sprintf(
		`SELECT * FROM UNNEST(GENERATE_TIMESTAMP_ARRAY(TIMESTAMP '%s', TIMESTAMP '%s', %s)) t(metric_time)`,
		ds.Start, ds.End, ds.Unit.IntervalExpression())
	q, err := parseQuery(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse date spine query: %w", err)
	}
	return q, nil
}
```

- [ ] **Step 4: 写 `getMetricRollupSql` / `parseMetricRollupSql`**

对照 `Utils.java:230-258`（`getMetricRollupSql`）与 `:105-118`（`parseMetricRollupSql`）。**风险 #2**：`String.join(",", selectItems)` 与 GROUP BY 序号 `joining(",")` 均**逗号无空格**。

```go
// getMetricRollupSql builds the SQL for a roll_up(...) sub-query.
// Mirrors Java Utils.getMetricRollupSql.
func getMetricRollupSql(info *analyzer.MetricRollupInfo) string {
	metric := info.Metric
	timeGrain := fmt.Sprintf(`DATE_TRUNC('%s', %s) "%s"`,
		string(info.TimeUnit), info.TimeGrain.RefColumn, info.TimeGrain.Name)
	selectItems := []string{timeGrain}
	for _, c := range metric.Dimension {
		selectItems = append(selectItems, fmt.Sprintf(`%s AS "%s"`, c.GetExpression(), c.Name))
	}
	for _, c := range metric.Measure {
		selectItems = append(selectItems, fmt.Sprintf(`%s AS "%s"`, c.GetExpression(), c.Name))
	}
	n := len(selectItems) - len(metric.Measure)
	ordinals := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ordinals = append(ordinals, strconv.Itoa(i))
	}
	return fmt.Sprintf(`SELECT %s FROM "%s" GROUP BY %s`,
		strings.Join(selectItems, ","), metric.BaseObject, strings.Join(ordinals, ","))
}

// parseMetricRollupSql builds and parses the roll_up sub-query.
// Mirrors Java Utils.parseMetricRollupSql.
func parseMetricRollupSql(info *analyzer.MetricRollupInfo) (*ast.Query, error) {
	q, err := parseQuery(getMetricRollupSql(info))
	if err != nil {
		return nil, fmt.Errorf("failed to parse metric rollup sql for %q: %w", info.Metric.Name, err)
	}
	return q, nil
}
```

注：Java `column.getSqlExpression()` = Go `c.GetExpression()`。`info.TimeUnit` 是 `dto.TimeUnit`（字符串），`'%s'` → "YEAR"（**风险 #6** 见任务 14）。

- [ ] **Step 5: 确认 import**

`utils.go` 顶部 import 块须含 `fmt`、`strconv`、`strings`、`internal/dto`、`internal/mdl`、`internal/parser/ast`、`internal/rewrite/analyzer`。补齐缺失项；P3a 已有的不重复。

- [ ] **Step 6: 暂不单独编译/提交**（引用任务 10 的 `dateSpineName`，与任务 10 合并编译与提交）

## 任务 10：`CumulativeMetricInfo` + `DateSpineInfo`

**Files:**
- Create: `internal/rewrite/cumulative_metric_info.go`
- Create: `internal/rewrite/date_spine_info.go`
- Test: `internal/rewrite/cumulative_metric_info_test.go`

- [ ] **Step 1: 写 `date_spine_info.go`**

机械移植 `DateSpineInfo.java`（60 行）。`dateSpineName` 常量在此定义（任务 9 的 `utils.go` 引用它）。

```go
package rewrite

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// dateSpineName is the reserved CTE name for the date spine.
// Mirrors Java DateSpineInfo.NAME.
const dateSpineName = "date_spine"

// DateSpineInfo is a QueryDescriptor for the date-spine CTE.
// Mirrors Java io.wren.base.sqlrewrite.DateSpineInfo.
type DateSpineInfo struct {
	query *ast.Query
}

// dateSpineInfoGet builds a DateSpineInfo. Mirrors DateSpineInfo.get.
func dateSpineInfoGet(ds dto.DateSpine) (*DateSpineInfo, error) {
	query, err := createDateSpineQuery(ds)
	if err != nil {
		return nil, err
	}
	return &DateSpineInfo{query: query}, nil
}

func (d *DateSpineInfo) Name() string              { return dateSpineName }
func (d *DateSpineInfo) RequiredObjects() []string { return nil }
func (d *DateSpineInfo) Query() *ast.Query         { return d.query }
```

- [ ] **Step 2: 写 `cumulative_metric_info.go`**

机械移植 `CumulativeMetricInfo.java`（64 行）。`requiredObjects` = `{baseObject, "date_spine"}`，P3a 约定 `RequiredObjects()` 返回按名排序切片（确定性 —— 风险 #7/拓扑）。

```go
package rewrite

import (
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
)

// CumulativeMetricInfo is a QueryDescriptor for a cumulative metric.
// Mirrors Java io.wren.base.sqlrewrite.CumulativeMetricInfo.
type CumulativeMetricInfo struct {
	name            string
	requiredObjects []string
	query           *ast.Query
}

// cumulativeMetricInfoGet builds a CumulativeMetricInfo. Mirrors CumulativeMetricInfo.get.
func cumulativeMetricInfoGet(cm *dto.CumulativeMetric, wrenMDL *mdl.WrenMDL) (*CumulativeMetricInfo, error) {
	query, err := parseCumulativeMetricSql(cm, wrenMDL)
	if err != nil {
		return nil, err
	}
	req := sortedKeys(map[string]bool{cm.BaseObject: true, dateSpineName: true})
	return &CumulativeMetricInfo{name: cm.Name, requiredObjects: req, query: query}, nil
}

func (c *CumulativeMetricInfo) Name() string              { return c.name }
func (c *CumulativeMetricInfo) RequiredObjects() []string { return c.requiredObjects }
func (c *CumulativeMetricInfo) Query() *ast.Query         { return c.query }
```

注：`sortedKeys`（P3a `utils.go`）把 set 转有序切片。`DateSpineInfo`/`CumulativeMetricInfo` 均实现 P3a 的 `QueryDescriptor` 接口（`Name()`/`RequiredObjects()`/`Query()`）。

- [ ] **Step 3: 编译切片 3**

Run: `go build ./...`
Expected: 通过（任务 9 的 `utils.go` 此刻能解析 `dateSpineName`）。

- [ ] **Step 4: 写 `cumulative_metric_info_test.go`**

```go
package rewrite

import (
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

func TestCreateDateSpineQuery(t *testing.T) {
	q, err := createDateSpineQuery(dto.DefaultDateSpine())
	if err != nil {
		t.Fatalf("createDateSpineQuery: %v", err)
	}
	got := formatter.FormatSQL(q)
	for _, want := range []string{"GENERATE_TIMESTAMP_ARRAY", "metric_time", "INTERVAL '1 DAY'"} {
		if !strings.Contains(got, want) {
			t.Errorf("date spine SQL missing %q:\n%s", want, got)
		}
	}
}

func TestDateSpineInfo(t *testing.T) {
	d, err := dateSpineInfoGet(dto.DefaultDateSpine())
	if err != nil {
		t.Fatalf("dateSpineInfoGet: %v", err)
	}
	if d.Name() != "date_spine" {
		t.Fatalf("Name() = %q, want date_spine", d.Name())
	}
	if d.RequiredObjects() != nil {
		t.Fatalf("RequiredObjects() = %v, want nil", d.RequiredObjects())
	}
}

func TestCumulativeMetricInfo(t *testing.T) {
	wrenMDL := metricMDL(t) // from metric_sql_render_test.go (same package)
	cm, _ := wrenMDL.GetCumulativeMetric("WeeklyRevenue")
	info, err := cumulativeMetricInfoGet(cm, wrenMDL)
	if err != nil {
		t.Fatalf("cumulativeMetricInfoGet: %v", err)
	}
	if info.Name() != "WeeklyRevenue" {
		t.Fatalf("Name() = %q, want WeeklyRevenue", info.Name())
	}
	if !contains(info.RequiredObjects(), "Orders") || !contains(info.RequiredObjects(), "date_spine") {
		t.Fatalf("RequiredObjects() = %v, want to contain Orders and date_spine", info.RequiredObjects())
	}
	got := formatter.FormatSQL(info.Query())
	for _, want := range []string{"date_trunc('WEEK'", `"date_spine"`, "group by 1", "order by 1"} {
		if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
			t.Errorf("cumulative metric SQL missing %q:\n%s", want, got)
		}
	}
}
```

注：`metricMDL` helper 在任务 8 的 `metric_sql_render_test.go`、同包可见。

- [ ] **Step 5: 运行**

Run: `go test ./internal/rewrite/ -run 'DateSpine|CumulativeMetricInfo' -v`
Expected: 3 个测试 PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/rewrite/utils.go internal/rewrite/date_spine_info.go internal/rewrite/cumulative_metric_info.go internal/rewrite/cumulative_metric_info_test.go
git commit -m "feat(p3b): add CumulativeMetricInfo/DateSpineInfo + Utils metric parsers (slice 3)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 4 — WrenSqlRewrite 度量 / 累积接线

目标：非动态路径补 metric / cumulative 分支，引用度量 / 累积度量的查询端到端转绿。

## 任务 11：`QueryDescriptorOf` 补度量分支

**Files:**
- Modify: `internal/rewrite/query_descriptor.go`

机械移植 `QueryDescriptor.of`（`QueryDescriptor.java:37-60`）。P3a 的 `QueryDescriptorOf` 对 metric / cumulative / view 返回「待 P3b/P3c」error、且不认 `date_spine`。本任务把 metric / cumulative / date_spine 三支改为真实实现（view 仍 P3c）。**风险 #7**：`date_spine` 这一支是非动态路径自动注入 `date_spine` CTE 的唯一入口 —— `CumulativeMetricInfo.RequiredObjects()` 含 `"date_spine"`，`WrenSqlRewrite` 的图递归会对它调 `QueryDescriptorOf("date_spine")`。

- [ ] **Step 1: 改写 `QueryDescriptorOf`**

```go
// QueryDescriptorOf builds a descriptor for the named object.
// Mirrors Java QueryDescriptor.of.
func QueryDescriptorOf(name string, analyzedMDL *mdl.AnalyzedMDL, ctx *base.SessionContext) (QueryDescriptor, error) {
	wrenMDL := analyzedMDL.WrenMDL()
	if model, ok := wrenMDL.GetModel(name); ok {
		return relationInfoOfModel(model, wrenMDL)
	}
	if metric, ok := wrenMDL.GetMetric(name); ok {
		return relationInfoOfMetric(metric, wrenMDL)
	}
	if cm, ok := wrenMDL.GetCumulativeMetric(name); ok {
		return cumulativeMetricInfoGet(cm, wrenMDL)
	}
	if _, ok := wrenMDL.GetView(name); ok {
		return nil, fmt.Errorf("view %q requires P3c", name)
	}
	if name == dateSpineName {
		return dateSpineInfoGet(wrenMDL.GetDateSpine())
	}
	return nil, fmt.Errorf("%s not found in wren mdl", name)
}
```

注：`ctx` 参数 P3a 已在签名里（Java `QueryDescriptor.of` 第 3 参，view 分支用）—— 保留不动。若 P3a 的 `query_descriptor.go` 因此前不引用 `ctx` 而带 `_ *base.SessionContext`，本步可保留下划线（仍未用，view 待 P3c）。

- [ ] **Step 2: 编译**

Run: `go build ./...`
Expected: 通过。

- [ ] **Step 3: Commit**

```bash
git add internal/rewrite/query_descriptor.go
git commit -m "feat(p3b): QueryDescriptorOf resolves metric/cumulative/date_spine (slice 4)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 12：`WrenSqlRewrite` 补 metric / cumulative 描述符

**Files:**
- Modify: `internal/rewrite/wren_sql_rewrite.go`

机械移植 `WrenSqlRewrite.apply` 非动态分支（`WrenSqlRewrite.java:132-142`）的 metric / cumulative 部分。P3a 的 `Apply` 已从 `analysis.Models()` 建 `modelDescriptors`、建依赖图、拓扑、`applyWith`、`rewriteModelTables`。本任务**只**在 `allDescriptors` 里补 metric 与 cumulative 描述符两个循环 —— 图递归 / 拓扑 / `date_spine` 注入（风险 #7）全部沿用 P3a 既有代码，无需新增。

- [ ] **Step 1: 在 `Apply` 的模型循环后补 metric / cumulative 循环**

P3a 的 `Apply` 形如：先 `analyzer.Analyze` 得 `analysis`，再 `for _, model := range analysis.Models() { … allDescriptors = append(...) }`，再 `if len(allDescriptors) == 0 { return root, nil }`。在**模型循环之后、`len == 0` 判断之前**插入：

```go
	for _, metric := range analysis.Metrics() {
		info, err := relationInfoOfMetric(metric, wrenMDL)
		if err != nil {
			return nil, err
		}
		allDescriptors = append(allDescriptors, info)
	}
	for _, cm := range analysis.CumulativeMetrics() {
		info, err := cumulativeMetricInfoGet(cm, wrenMDL)
		if err != nil {
			return nil, err
		}
		allDescriptors = append(allDescriptors, info)
	}
```

`analysis.Metrics()` / `analysis.CumulativeMetrics()` 由 P3a `Analysis` 提供、已按名排序去重（P3a 任务 7）。`wrenMDL` 是 P3a `Apply` 里已绑定的局部变量（`analyzedMDL.WrenMDL()`）；若 P3a 的变量名不同，以 P3a 为准。

注：Java 把 model / metric / cumulative 装进 `ImmutableSet` 再喂图。Go 端 `allDescriptors` 的追加顺序不影响最终 CTE 顺序 —— P3a `topoSort` 的 `topoTieBreak` 对就绪集排序，确定性与追加序无关。

- [ ] **Step 2: 编译 + 既有测试**

Run: `go build ./... && go test ./internal/rewrite/...`
Expected: 通过；切片 1–3 全部单测仍 PASS。

- [ ] **Step 3: Commit**

```bash
git add internal/rewrite/wren_sql_rewrite.go
git commit -m "feat(p3b): wire metric/cumulative descriptors into WrenSqlRewrite (slice 4)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 13：端到端 —— 合成度量 / 累积查询转绿

**Files:**
- Modify: `testdata/difftest/baseline.json`
- 可能 Modify: `internal/rewrite/graph.go`（仅当 CTE 顺序逆向校验不符时调 `topoTieBreak`）

- [ ] **Step 1: 跑差分测试，看度量查询状态**

Run: `make difftest`
Expected: 22 条标准查询 + P3a `m_*` 无回归；`metric/metric_on_model`、`metric/metric_on_metric`、`metric/cumulative` 由 `fail` 翻 `pass`（或仍 `fail`，进 Step 2）；`metric/rollup` 仍 `fail`（待切片 5）；`tpch/met_*` 由 `fail` 变 `go-error`（Go 已能识别度量并尝试渲染，但传递性拉入含 Jinja 列的 `Customer` 模型 → `parseExpression` 失败 → Go 报错，属预期已知失败）。

- [ ] **Step 2: 逐条排查未转绿的合成度量查询**

对每个仍 `fail` 的 `metric/*`，`make difftest` 日志给出首个 token 差异。常见根因与对策：
- **CTE 顺序不符（风险 #7 / P3a 风险 #1）**：`metric/cumulative` 含 `Orders`/`date_spine`/`WeeklyRevenue` 三 CTE；`metric/metric_on_metric` 含 `Orders`/`Revenue`/`RevenueByCustomer` 三 CTE。若 golden 的 `WITH` 子句 CTE 排列与 Go 不同 → `cat testdata/difftest/golden/metric/cumulative.sql` 看 Java 实际顺序，再调 P3a `graph.go` 的 `topoTieBreak`（仅此一处函数；P3a 已把平局规则隔离于此）。
- **`getCumulativeMetricSql` 模板 token 错（风险 #1）**：对照 `Utils.java:135-195` 修 `utils.go`。
- **`MetricSqlRender` 模板 token 错（风险 #2/#3/#5）**：对照 `MetricSqlRender.java` 修 `metric_sql_render.go`。

- [ ] **Step 3: 确认 `tpch/met_*` 为已知失败**

`make difftest` 日志里 `tpch/met_revenue` 等应为 `go-error`，错误信息指向 Jinja 列解析失败（`Customer` 模型的 `custkey_name = {{ concat(...) }}`）。这是预期的 P6 依赖，非回归。

- [ ] **Step 4: 接受 baseline**

合成度量查询转绿后：

Run: `make difftest-accept`
`git diff testdata/difftest/baseline.json` 确认：`metric/metric_on_model`、`metric/metric_on_metric`、`metric/cumulative` 变 `pass`；`metric/rollup` 仍 `fail`；`tpch/met_*` 由 `fail` 变 `go-error`（横向移动，仍为红、文档化 P6）；22 条标准查询 + `m_*` 无 `pass→fail` 回归。

- [ ] **Step 5: Commit**

```bash
git add testdata/difftest/baseline.json internal/rewrite/graph.go
git commit -m "test(p3b): metric/cumulative queries pass differential test (slice 4)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 切片 5 — MetricRollupRewrite 规则

目标：把 P3a 的 `MetricRollupRewrite` 透传桩换成真实规则，`roll_up(...)` 语法查询端到端转绿。

## 任务 14：`MetricRollupRewrite` 真实规则

**Files:**
- Create: `internal/rewrite/metric_rollup_rewrite.go`
- Modify: `internal/rewrite/passthrough_rules.go`（删除 `MetricRollupRewrite` 透传桩）
- Test: `internal/rewrite/metric_rollup_rewrite_test.go`

机械移植 `MetricRollupRewrite.java`（76 行）。`planner.go` 的 `AllRules` 引用 `&MetricRollupRewrite{}` —— 删桩后该名字解析到本任务新建的真实类型，**类型名不变、`planner.go` 无需改**。

- [ ] **Step 1: 从 `passthrough_rules.go` 删除 `MetricRollupRewrite` 桩**

P3a 的 `passthrough_rules.go` 含 `GenerateViewRewrite` / `MetricRollupRewrite` / `EnumRewrite` 三个透传桩。删除 `MetricRollupRewrite` 结构体及其 `Apply` 方法（连同其上方注释）；保留另两个。

- [ ] **Step 2: 写 `metric_rollup_rewrite.go`**

对照 `MetricRollupRewrite.java`。Java 的 `apply`：建 `Analysis`、跑 `StatementAnalyzer.analyze`、再用 `Rewriter`（`BaseRewriter`，仅覆盖 `visitFunctionRelation`）遍历。Go 用 P3a 的 `RewriteNode` 钩子。**风险 #6**：`AliasedRelation` 的别名是**非 delimited** identifier（Java `new Identifier(name)` 单参 → `Delimited` 默认 false）。

```go
package rewrite

import (
	"fmt"

	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite/analyzer"

	base "github.com/wren-engine/wren/internal/analyzer"
)

// MetricRollupRewrite replaces roll_up(...) FunctionRelations with a metric
// sub-query. Mirrors Java io.wren.base.sqlrewrite.MetricRollupRewrite.
type MetricRollupRewrite struct{}

// Apply rewrites every roll_up FunctionRelation captured by the analyzer.
// Mirrors MetricRollupRewrite.apply + the inner Rewriter.visitFunctionRelation.
func (r *MetricRollupRewrite) Apply(root ast.Statement, ctx *base.SessionContext, analyzedMDL *mdl.AnalyzedMDL) (ast.Statement, error) {
	analysis := analyzer.NewAnalysis(root)
	if _, err := analyzer.Analyze(analysis, root, ctx, analyzedMDL.WrenMDL()); err != nil {
		return nil, err
	}
	var rewriteErr error
	out := RewriteNode(root, func(n ast.Node) (ast.Node, bool) {
		fr, ok := n.(*ast.FunctionRelation)
		if !ok {
			return nil, false // descend
		}
		info, found := analysis.GetMetricRollup(fr)
		if !found {
			// every roll_up node is captured + syntax-checked in StatementAnalyzer;
			// reaching here means an unsupported FunctionRelation. Mirrors Java throw.
			rewriteErr = fmt.Errorf("MetricRollup node is not replaced")
			return fr, true
		}
		query, err := parseMetricRollupSql(info)
		if err != nil {
			rewriteErr = err
			return fr, true
		}
		return &ast.AliasedRelation{
			Relation: &ast.TableSubquery{Query: query},
			Alias:    &ast.Identifier{Value: info.Metric.Name}, // non-delimited (risk #6)
		}, true
	})
	if rewriteErr != nil {
		return nil, rewriteErr
	}
	return out.(ast.Statement), nil
}
```

注：节点身份 —— `Analyze(analysis, root, …)` 与 `RewriteNode(root, …)` 走同一棵 `root`；`RewriteNode` 在重建容器前先把**原始** `*ast.FunctionRelation` 交给钩子，故 `analysis.GetMetricRollup(fr)` 按 `NodeRef` 身份能命中（与 P3a 风险 #5 同理）。`ast.AliasedRelation` 字段名（`Relation`/`Alias`/`ColumnNames`）与 `ast.TableSubquery.Query`、`ast.Identifier{Value,Delimited}` 已在 P2 AST 中确认；`ColumnNames` 省略（Java `List.of()` 空 → nil）。

- [ ] **Step 3: 写 `metric_rollup_rewrite_test.go`**

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

func TestMetricRollupRewrite_ReplacesRollup(t *testing.T) {
	wrenMDL := metricMDL(t) // from metric_sql_render_test.go (same package)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, err := parser.ParseSQL("SELECT * FROM roll_up(Revenue, orderdate, YEAR)")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rule := &MetricRollupRewrite{}
	out, err := rule.Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := formatter.FormatSQL(out)
	for _, want := range []string{`DATE_TRUNC('YEAR'`, `FROM "Revenue"`, "GROUP BY 1,2"} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten rollup missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "roll_up") {
		t.Errorf("roll_up not replaced:\n%s", got)
	}
}

func TestMetricRollupRewrite_PassThroughNoRollup(t *testing.T) {
	wrenMDL := metricMDL(t)
	ctx := &base.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	stmt, _ := parser.ParseSQL("SELECT custkey FROM Revenue")
	rule := &MetricRollupRewrite{}
	out, err := rule.Apply(stmt, ctx, mdl.NewAnalyzedMDL(wrenMDL))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := formatter.FormatSQL(out); got != formatter.FormatSQL(stmt) {
		t.Errorf("non-rollup query changed: got %q", got)
	}
}
```

- [ ] **Step 4: 编译 + 运行**

Run: `go build ./... && go test ./internal/rewrite/ -run TestMetricRollupRewrite -v`
Expected: `go build` 通过（`AllRules` 的 `&MetricRollupRewrite{}` 解析到新类型）；2 个测试 PASS。`GROUP BY 1,2` —— `Revenue` 有 1 维 + 1 量，`selectItems` = [timeGrain, custkey, totalprice] 共 3，减 measure 数 1 → 序号 `1,2`。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/metric_rollup_rewrite.go internal/rewrite/passthrough_rules.go internal/rewrite/metric_rollup_rewrite_test.go
git commit -m "feat(p3b): real MetricRollupRewrite rule, drop pass-through stub (slice 5)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

## 任务 15：端到端 —— rollup 查询转绿

**Files:**
- Modify: `testdata/difftest/baseline.json`

- [ ] **Step 1: 跑差分测试**

Run: `make difftest`
Expected: `metric/rollup` 由 `fail` 翻 `pass`（`MetricRollupRewrite` 把 `roll_up(Revenue, orderdate, YEAR)` 换成度量子查询，`WrenSqlRewrite` 再展开 `Revenue`/`Orders` CTE）；切片 4 已转绿的 `metric/*` 不回归；22 条标准查询 + `m_*` 无回归；`tpch/met_rollup` 仍 `go-error`（传递性 Jinja，预期）。

- [ ] **Step 2: 排查（若 `metric/rollup` 未转绿）**

`make difftest` 日志给出 token 差异。常见根因：
- **rollup 别名 delimited 与否（风险 #6）**：golden 里 `roll_up` 结果别名应是裸 `Revenue`（无引号）。若 Go 输出 `"Revenue"` → `metric_rollup_rewrite.go` 的 `Alias` 误设了 `Delimited: true`，去掉。
- **`getMetricRollupSql` 模板（风险 #2）**：`GROUP BY` 序号逗号无空格；`SELECT` 项逗号无空格。对照 `Utils.java:230-258`。
- **规则间重解析（风险 #8）**：确认 `planner.go` 编排循环在 `MetricRollupRewrite` 与 `WrenSqlRewrite` 之间有 `parseSql(formatSql(...))`（P3a 既有，应无需改）。

- [ ] **Step 3: 接受 baseline**

Run: `make difftest-accept`
`git diff testdata/difftest/baseline.json` 确认：`metric/rollup` 变 `pass`；其余 `metric/*` 保持 `pass`；`tpch/met_*`（含 `met_rollup`）为 `go-error`；无 `pass→fail` 回归。

- [ ] **Step 4: 运行全量测试**

Run: `make test`
Expected: 全绿。

- [ ] **Step 5: Commit**

```bash
git add testdata/difftest/baseline.json
git commit -m "test(p3b): rollup query passes differential test (slice 5)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

# 收尾

## 任务 16：终验与文档

**Files:**
- Modify: `internal/rewrite/README.md`（补 P3b 说明）
- Modify: `internal/difftest/README.md`（补度量语料说明）

- [ ] **Step 1: 更新 `internal/rewrite/README.md`**

在 P3a 写的 README 上追加 P3b 段落：`MetricSqlRender`/`CumulativeMetricInfo`/`DateSpineInfo`/`MetricRollupRewrite` 与 Java 的对应；`MetricRollupRewrite` 已是真实规则（`GenerateViewRewrite`/`EnumRewrite` 仍为透传桩，待 P3c）；非动态路径 `date_spine` CTE 经 `CumulativeMetricInfo.RequiredObjects()` + 图递归自动注入；合成度量语料 `cases/metric/` 的用途；TPC-H 度量查询 `cases/tpch/queries/met_*.sql` 因传递性 Jinja（`Customer` 模型）待 P6。

- [ ] **Step 2: 更新 `internal/difftest/README.md`**

在「失败分类」补：`metric/` 是合成度量语料组（P3b 端到端字节奇偶证据）；`tpch/met_*` 引用 TPC-H 度量、因传递性依赖含 Jinja 列的 `Customer` 模型在 P3b 阶段为已知失败（`go-error`），待 P6 的 Jinja 宏层。

- [ ] **Step 3: 终验全套**

Run（逐条须通过）：
```bash
go build ./...
go vet ./...
gofmt -l internal/rewrite/ internal/dto/ internal/mdl/
make test
make difftest
```
Expected：`go build` 通过；`go vet` 无输出；`gofmt -l` 无输出；`make test` 全绿；`make difftest` 无回归、`metric/metric_on_model`/`metric/metric_on_metric`/`metric/cumulative`/`metric/rollup` 计分板为 `pass`。

- [ ] **Step 4: 验收标准核对（设计 §8）**

- [ ] 合成度量语料 `metric/metric_on_model`（metric-on-model）、`metric/metric_on_metric`（metric-on-metric）、`metric/cumulative`（累积度量 + date_spine）、`metric/rollup`（汇总语法）端到端 dry-plan golden 字节一致 —— 均 `pass`。
- [ ] metric-on-cumulative 与关系遍历度量列 join 渲染由任务 8 的合成 MDL 单测覆盖。
- [ ] 引用 TPC-H 度量的 `tpch/met_*` 在 baseline 标记为已知失败（`go-error`），文档化 P6 依赖。
- [ ] P3a 已通过的模型 / 关系类查询（22 条标准 + `m_*`）无 `pass→fail` 回归。
- [ ] `go build` / `go vet` / `gofmt -l` 均干净。

- [ ] **Step 5: Commit**

```bash
git add internal/rewrite/README.md internal/difftest/README.md
git commit -m "docs(p3b): document metric rewrite + finalize P3b acceptance

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## 附录 A — Java ↔ Go 文件对应表

| Java（`wren-base/`） | Go | 任务 | 操作 |
|---|---|---|---|
| `dto/TimeUnit.java`（`getIntervalExpression`/`timeUnit`） | `internal/dto/common.go` | 3 | 修改 |
| `dto/Metric.java`（`getTimeGrain`）/`Window.toColumn`/`Measure.toColumn` | `internal/dto/metric.go` | 3 | 修改 |
| `WrenMDL.getDateSpine` | `internal/mdl/wren_mdl.go` | 3 | 修改 |
| `sqlrewrite/analyzer/MetricRollupInfo.java` | `internal/rewrite/analyzer/metric_rollup_info.go` | 4 | 新建 |
| `sqlrewrite/analyzer/Analysis.java`（`metricRollups`） | `internal/rewrite/analyzer/analysis.go` | 4 | 修改 |
| `sqlrewrite/analyzer/StatementAnalyzer.java`（度量识别 + `visitFunctionRelation`） | `internal/rewrite/analyzer/statement_analyzer.go` | 5 | 修改 |
| `Utils.analyzeFrom`（metric 分支） | `internal/rewrite/analyzer/utils.go` | 5 | 修改 |
| `sqlrewrite/RelationInfo.java`（model/metric 共用） | `internal/rewrite/relation_info.go` | 6 | 修改 |
| `sqlrewrite/RelationshipRewriter.java`（`relationshipAware`） | `internal/rewrite/relationship_rewriter.go` | 6 | 修改 |
| `sqlrewrite/MetricSqlRender.java` | `internal/rewrite/metric_sql_render.go` | 7 | 新建 |
| `sqlrewrite/Utils.java`（度量解析函数） | `internal/rewrite/utils.go` | 9 | 修改 |
| `sqlrewrite/DateSpineInfo.java` | `internal/rewrite/date_spine_info.go` | 10 | 新建 |
| `sqlrewrite/CumulativeMetricInfo.java` | `internal/rewrite/cumulative_metric_info.go` | 10 | 新建 |
| `sqlrewrite/QueryDescriptor.java`（`of`） | `internal/rewrite/query_descriptor.go` | 11 | 修改 |
| `sqlrewrite/WrenSqlRewrite.java`（非动态 metric/cumulative 分支） | `internal/rewrite/wren_sql_rewrite.go` | 12 | 修改 |
| `sqlrewrite/MetricRollupRewrite.java` | `internal/rewrite/metric_rollup_rewrite.go` | 14 | 新建 |
| 删 `MetricRollupRewrite` 透传桩 | `internal/rewrite/passthrough_rules.go` | 14 | 修改 |

## 附录 B — 不在 P3b 范围（机械移植时遇到须停手）

- `WrenSqlRewrite` 动态字段分支（`isEnableDynamicField()` 为 true）—— `getTableRequiredFields`、`WrenDataLineage`、动态路径的显式 `date_spine` 注入、`MetricSqlRender` 3 参构造（`requiredFields`）。P3b 只走 `else`（非动态）分支，沿用 2 参构造。
- `GenerateViewRewrite`、`EnumRewrite`、`ViewInfo` 真实实现 —— P3c。`QueryDescriptorOf` 的 view 分支仍返回 error。
- `analyzer/decisionpoint/`、`CacheAnalysis` —— P5。
- Jinja 宏（`{{ }}` 列表达式）—— P6。TPC-H 度量查询 `tpch/met_*` 的端到端转绿待此。

## 附录 C — 与 P3a 共享 / 复用的产物

P3b 不重新实现以下 P3a 产物，直接复用：

- `internal/rewrite/utils.go`：`parseSQL` / `parseExpression` / `parseQuery` / `checkArgument` / `contains` / `sortedKeys` / `dereferenceFrom` / `qualifiedConditionString`。
- `internal/rewrite/base_tree_rewriter.go`：`RewriteHook` / `RewriteNode`。
- `internal/rewrite/relationship_rewriter.go`：`rewriteRelationship` / `toDereferenceExpression`（任务 6 在其上补 `relationshipAware`）。
- `internal/rewrite/relationable_sql_render.go`：`orderedMap` / `newOrderedMap` / `calculatedFieldRelationshipInfo` / `newCalculatedFieldRelationshipInfo` / `subQueryJoinInfo` / `getRelationableAlias`。
- `internal/rewrite/relation_info.go`：`relationInfoOfModel`（任务 6 泛化 `RelationInfo` 结构体）。
- `internal/rewrite/query_descriptor.go`：`QueryDescriptor` 接口（任务 11 扩 `QueryDescriptorOf`）。
- `internal/rewrite/graph.go`：`topoSort` / `topoTieBreak`（CTE 顺序确定性 —— 风险 #7 的逆向校验只调 `topoTieBreak`）。
- `internal/rewrite/with_rewriter.go`：`getWithQuery` / `applyWith`。
- `internal/rewrite/wren_sql_rewrite.go`：`Apply` 的依赖图 / 拓扑 / `applyWith` / `rewriteModelTables`（任务 12 只在 `allDescriptors` 追加 metric/cumulative）。
- `internal/rewrite/analyzer/`：`NewAnalysis` / `Analyze` / `GetRelationships` / `ExpressionRelationshipInfo` / `Scope` builder / `toCatalogSchemaTableName` / `toField` 等。
- `internal/rewrite/planner.go`：`AllRules` / `Rewrite` 编排循环（任务 14 删桩后 `&MetricRollupRewrite{}` 自动指向真实规则，无需改 `planner.go`）。
