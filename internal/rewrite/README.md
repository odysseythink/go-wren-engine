# Rewrite 引擎

Go 端 SQL 重写引擎，机械移植自 Java `wren-engine:0.9.3` 的 `wren-base/src/main/java/io/wren/base/sqlrewrite/`。

## 架构

`WrenPlanner.Rewrite(sql, ctx, mdl)` 按固定顺序应用 4 条规则，每条规则产出经 `parse(format(...))` 再解析的 AST：

1. **ModelRewrite** —— 无操作（Java 侧已废弃）。
2. **MetricRollupRewrite** —— 把 `roll_up(metric, timeColumn, timeUnit)` 函数关系替换为度量子查询。
3. **WrenSqlRewrite** —— 非动态字段路径：分析查询引用的模型/度量/累积度量，生成 CTE 依赖图，拓扑排序后注入 WITH 子句，重写表引用。
4. **DataSourceRewrite** —— 无操作（Java 侧已废弃）。

## P3a（模型 / 关系）

- `ModelSqlRender` —— 把 `Model` 渲染为 CTE 查询 AST。
- `RelationshipRewriter` —— 解析计算列中的关系遍历，生成 LEFT JOIN 子查询。
- `WrenSqlRewrite.Apply` —— 建 CTE 依赖图、拓扑排序（`topoSort` + `topoTieBreak`）、`applyWith` 注入、`rewriteModelTables` 表名改写。

## P3b（度量 / 累积度量 / 度量汇总）

- `MetricSqlRender` —— 把 `Metric` 渲染为 CTE 查询 AST。支持三种基：model / metric / cumulative metric。
- `CumulativeMetricInfo` / `DateSpineInfo` —— 累积度量与日期轴 CTE 描述符。
- `MetricRollupRewrite` —— 真实规则，替换 `roll_up(...)` 为 `AliasedRelation(TableSubquery(...))`。
- `WrenSqlRewrite.Apply` —— 补 metric / cumulative 描述符循环，图递归自动拉入 `date_spine`（经 `CumulativeMetricInfo.RequiredObjects()`）。

## P3c（视图 / 枚举）

- `EnumRewrite` —— 真实规则，用 `RewriteNode` 钩子遍历 AST；两段 `DereferenceExpression`
  (`EnumName.Value`) 若首段匹配 MDL enum 名，则替换为 `StringLiteral`（值来自
  `dto.EnumValue.GetValue()`，空值时回退到 name）。严格大小写匹配，三段及以上不视为 enum。
- `ViewInfo` —— `parseView` → `StatementAnalyzer.analyze` → 套 `MetricRollupRewrite`
  → `requiredObjects = analysis.WrenObjectNames()`（已按名排序）。
- `GenerateViewRewrite` —— 真实规则，`analysis.Views()` 作种子；DAG 仅含 view 顶点
  （`addToGraph` 用 `wrenMDL.GetView` 过滤非 view 依赖）；`topoSort` 拓扑排序后
  `applyWith` 前置 view CTEs。不调 `rewriteModelTables`（模型展开由 rule 3 处理）。

## 规则管线状态

`AllRules` 4 条规则全部为真实规则（`passthrough_rules.go` 已删除）：

1. `GenerateViewRewrite` —— view CTE 前置。
2. `MetricRollupRewrite` —— `roll_up(...)` 替换为子查询。
3. `WrenSqlRewrite` —— model/metric/cumulative CTE 前置 + 表引用改写。
4. `EnumRewrite` —— enum  dereference 替换为字符串字面量。

## P3 收官口径

| 类别 | 数量 | 状态（P3 收官时） |
|---|---|---|
| TPC-H 标准查询 `tpch/1..22` | 22 | 20 `pass` + 2 `oracle-error`（`tpch/1`/`tpch/4`，oracle 自身限制） |
| P3a 模型查询 `tpch/m_*` | 6 | 5 `pass` + 1 `go-error`（`tpch/m_orders` 传递性 Jinja，待 P6） |
| P3b 度量查询 `tpch/met_*` | 5 | 5 `go-error`（传递性 Jinja，待 P6） |
| 合成度量 `metric/*` | 4 | 4 `pass` |
| 合成视图/枚举 `viewenum/*` | 5 | 5 `pass` |
| TPC-H 视图 `tpch/v_use_*` | 4 | 4 `go-error`（传递性 Jinja，待 P6） |
| TPC-H 枚举 `tpch/v_enum` | 1 | 1 `pass` |

`tpch/1`/`tpch/4` 的 `oracle-error` 与 `m_orders`/`met_*`/`v_use_*` 的传递性 Jinja 失败
均为 oracle 限制 / 待 P6，非 Go 缺陷。P3 重写引擎奇偶校验（非动态字段路径）达成。

## 尚未实现（后续阶段）

- 动态字段路径（`isEnableDynamicField`）—— P3c+。
- Jinja 宏（`{{ }}` 列表达式）—— P6。TPC-H 度量查询 `tpch/met_*` 与视图查询
  `tpch/v_use_*` 的端到端转绿待此。
