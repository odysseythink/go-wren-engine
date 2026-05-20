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

## 尚未实现（后续阶段）

- `GenerateViewRewrite` / `EnumRewrite` / `ViewInfo` —— P3c。
- 动态字段路径（`isEnableDynamicField`）—— P3c+。
- Jinja 宏（`{{ }}` 列表达式）—— P6。TPC-H 度量查询 `tpch/met_*` 的端到端转绿待此。
