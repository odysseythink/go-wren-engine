# P3b 设计：重写引擎 —— 度量、累积度量、度量汇总

> 子项目：P3b（P3「重写引擎奇偶校验」三分后的第二个子阶段）。
> 前置：P0（构建）、P1（差分测试框架）、P2（parser/formatter 字节对齐）、**P3a（核心重写引擎）**。
> 目标：让 Go 引擎对引用度量 / 累积度量 / 度量汇总的查询产出与 Java `wren-engine:0.9.3` **字节一致**的重写后 SQL。

## 1. 背景

P3（重写引擎，约 6900 行 Java）按特性三分为 P3a（模型+关系）、P3b（度量）、P3c（视图+枚举），
顺序为 P3a → P3b → P3c。本 spec 为 **P3b**。

P3b 是 P3a 的**延伸** —— 复用同一个 `internal/rewrite/` 包、同一套 Approach C 机械移植路线、同一个
P1 差分测试框架与 baseline 计分板。P3a 把 `WrenPlanner` 的 4 条规则中 3 条做成透传桩；P3b 把其中的
`MetricRollupRewrite` 换成真实规则，并补全 `WrenSqlRewrite` 的度量分支。

度量在 wren 语义层有三类对象：

- **Metric**（度量）：基于 model / metric / cumulative metric，带维度（dimension）与度量值（measure），
  渲染为带 `GROUP BY` 的 CTE。
- **CumulativeMetric**（累积度量）：基于一个 baseObject + 一条日期轴 `date_spine`。
- **Metric Rollup**（度量汇总）：查询里的 `roll_up(...)` 语法，解析为 `FunctionRelation`，
  由 `MetricRollupRewrite` 规则替换为子查询。

TPC-H MDL 含 3 个 metric（`Revenue`、`CustomerRevenue`、`CustomerDailyRevenue`）、1 个 cumulative
metric（`WeeklyRevenue`），P3b 以这些为验证对象。

## 2. 范围

### IN（P3b 负责）

- `MetricSqlRender`（继承 P3a 的 `RelationableSqlRender`）—— 渲染 metric 的 CTE SQL：
  - 三种基：metric-on-model、metric-on-metric、metric-on-cumulative-metric
  - `requiredDims` / `requiredMeasures` 投影、`GROUP BY` 序号、`COUNT(*) AS _count_filler` 占位列
  - 计算列关系遍历（复用 P3a 的 `ExpressionRelationshipAnalyzer` / `RelationshipRewriter`）
- `CumulativeMetricInfo` —— 累积度量的 `QueryDescriptor`，`requiredObjects = {baseObject, date_spine}`
- `DateSpineInfo` —— `date_spine` 日期轴的 `QueryDescriptor`
- `Utils` 新增：`parseCumulativeMetricSql`、`createDateSpineQuery`、`parseMetricRollupSql`
- `WrenSqlRewrite` 非动态字段路径补全 metric / cumulative 分支：
  - `analysis.getMetrics()` → `RelationInfo.get(metric, wrenMDL)` → `MetricSqlRender`
  - `analysis.getCumulativeMetrics()` → `CumulativeMetricInfo.get`
  - 当存在 cumulative metric 时把 `date_spine` WITH-query 加入 CTE 列表
- `MetricRollupRewrite` 规则 —— 替换 P3a 的透传桩：
  - `analysis.getMetricRollups()`（`NodeRef` → `MetricRollupInfo` 映射）
  - `FunctionRelation`（rollup 语法）→ `AliasedRelation(TableSubquery(parseMetricRollupSql(info)))`
- 分析器补全：`StatementAnalyzer` 识别 metrics / cumulativeMetrics / metricRollups，
  `analyzer/MetricRollupInfo` 类型
- 验证：新增引用度量的语料用例，dry-plan golden 端到端差分

### OUT（不在 P3b）

- 视图 / 枚举（P3c）
- `WrenSqlRewrite` 动态字段路径 + `WrenDataLineage`
- `CacheAnalysis`、decisionpoint 分析器（P5）

## 3. 架构与数据流

P3b 不改 `WrenPlanner` 编排，只填充其调用的组件。两条数据流：

**度量作为 dataset 被引用**（如 `SELECT * FROM Revenue`）：

```
WrenSqlRewrite 非动态路径:
  analysis.getMetrics()           → RelationInfo.get(metric)  → MetricSqlRender.render() → metric CTE
  analysis.getCumulativeMetrics() → CumulativeMetricInfo.get  → 累积度量 CTE（依赖 date_spine）
  若有 cumulative metric → 追加 date_spine CTE
  其余同 P3a：DAG 拓扑 → WithRewriter → Rewriter
```

**度量汇总语法**（如 `SELECT * FROM roll_up(WeeklyRevenue, date, YEAR)`）：

```
StatementAnalyzer 捕获 FunctionRelation 形式的 rollup 语法 → analysis.metricRollups[NodeRef] = MetricRollupInfo
MetricRollupRewrite.apply:
  visitFunctionRelation(node):
    info  = analysis.getMetricRollups()[NodeRef.of(node)]
    query = parseMetricRollupSql(info)
    return AliasedRelation(TableSubquery(query), Identifier(metricName), [])
```

`MetricSqlRender` 通过 `format()` 字符串模板生成 metric CTE SQL（`SELECT … FROM (…) GROUP BY 1,2`），
经 `parseQuery` 解析为 `Query` AST，最终由 P2 的 `SqlFormatter` 渲染为字节输出。

## 4. 组件与文件结构

在 P3a 建立的 `internal/rewrite/` 包内新增 / 修改：

| Go 文件 | 对应 Java | 操作 |
|---|---|---|
| `internal/rewrite/metric_sql_render.go` | `MetricSqlRender` | 新建 |
| `internal/rewrite/cumulative_metric_info.go` | `CumulativeMetricInfo` | 新建 |
| `internal/rewrite/date_spine_info.go` | `DateSpineInfo` | 新建 |
| `internal/rewrite/metric_rollup_rewrite.go` | `MetricRollupRewrite` | 新建（替换透传桩） |
| `internal/rewrite/utils.go` | `Utils` 度量相关解析函数 | 修改 |
| `internal/rewrite/wren_sql_rewrite.go` | `WrenSqlRewrite` 度量 / 累积分支 | 修改 |
| `internal/rewrite/passthrough_rules.go` | 删除 `MetricRollupRewrite` 透传桩 | 修改 |
| `internal/rewrite/analyzer/statement_analyzer.go` | `StatementAnalyzer` 度量识别 | 修改 |
| `internal/rewrite/analyzer/metric_rollup_info.go` | `analyzer/MetricRollupInfo` | 新建 |

## 5. 实施切片（方案 C：增量切片，每片端到端可验证）

每个切片：移植 → 端到端差分 / 单测 → baseline 接受 fail→pass。

- **切片 1 — 分析器补度量识别**：`StatementAnalyzer` 识别 metrics / cumulativeMetrics / metricRollups，
  新增 `analyzer/MetricRollupInfo`。单测：含度量引用的 query → `Analysis` 正确归类。
- **切片 2 — MetricSqlRender**：先 metric-on-model（最常见），再 metric-on-metric /
  metric-on-cumulative。单测 / golden：单个 metric → CTE 查询 AST。
- **切片 3 — CumulativeMetricInfo + DateSpineInfo + Utils 解析函数**：`parseCumulativeMetricSql`、
  `createDateSpineQuery`、`parseMetricRollupSql`。单测：累积度量与 date_spine 渲染。
- **切片 4 — WrenSqlRewrite 度量 / 累积接线**：补全非动态路径的 metric / cumulative 分支与
  date_spine WITH-query 添加。端到端：度量类查询逐条转绿。
- **切片 5 — MetricRollupRewrite 规则**：替换透传桩，实现 `Rewriter.visitFunctionRelation`。
  端到端：rollup 语法查询转绿。

## 6. 字节分歧风险登记表

继承 P3a 全部风险（CTE 顺序由 DAG 拓扑迭代决定、`format()` 字符串模板逐字节、Map/Set 迭代序），
另加 P3b 特有：

| # | 风险 | 说明 |
|---|---|---|
| 1 | `COUNT(*) AS _count_filler` | `requiredMeasures` 为空时追加占位列；追加位置与时机须与 Java 一致。 |
| 2 | `GROUP BY` 序号 | `getQuerySql` 用 `1,2,…requiredDims.size()` 生成 GROUP BY；序号个数 = 维度数。 |
| 3 | metric-on-metric 递归基 | metric 基于 metric / cumulative metric 时走 `renderBasedOnMetric`，模板不同。 |
| 4 | `date_spine` 注入 | 仅当所引用对象含 cumulative metric 时才加 `date_spine` CTE；判定条件须复刻。 |
| 5 | `awareModel` 重写 | `MetricSqlRender` 用 `ExpressionTreeRewriter` 把裸 `Identifier` 改写为 `DereferenceExpression(model, col)`，匹配 **大小写不敏感**。 |
| 6 | rollup → AliasedRelation | `MetricRollupRewrite` 产出 `AliasedRelation(TableSubquery, Identifier(metricName), [])`，别名为非 delimited identifier。 |

## 7. 错误处理

- 找不到 metric base / 无效 metric / rollup 节点未被分析器捕获：返回 error
  （对齐 Java `IllegalArgumentException`）。
- 范围外（视图 / 枚举引用）：对应规则仍为透传桩，端到端差分失败由 baseline 记为已知失败，待 P3c。
- 不静默吞错。

## 8. 测试与验收

- **端到端差分测试**：复用 P1 `internal/difftest`，新增引用 TPC-H MDL 度量的语料用例
  （`Revenue` / `CustomerRevenue` / `CustomerDailyRevenue` / `WeeklyRevenue`，以及 `roll_up(...)`
  汇总语法），可从 Java `TestMetric` / `TestCumulativeMetric` 测试移植。
- **单元测试**：每切片组件级单测（分析器度量识别、`MetricSqlRender` 渲染、解析函数）。
- **baseline 计分板**：沿用 P1，度量类查询 fail→pass 接受。
- **验收标准**：
  - 新增度量 / 累积度量 / 汇总语料用例端到端 dry-plan golden 字节一致。
  - `go build ./...` 通过、`go vet ./...` 干净、`gofmt -l` 无输出。
  - P3a 已通过的模型 / 关系类查询不回归。

## 9. 与其他阶段的关系

- **依赖 P0 / P1 / P2 / P3a**：复用 P3a 的 `RelationableSqlRender`、`WrenSqlRewrite` 骨架、
  analyzer、`QueryDescriptor` / `WithRewriter` / DAG 机制与差分框架。
- **被 P3c 依赖**：P3c 的 `ViewInfo` 内部对 view SQL 应用 `MetricRollupRewrite`，故 P3c 必须在 P3b 之后。
- 本 spec 完成后进入 writing-plans，产出逐任务实施计划。
