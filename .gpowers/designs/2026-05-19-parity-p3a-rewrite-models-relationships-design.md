# P3a 设计：核心重写引擎 —— 模型与关系

> 子项目：P3a（P3「重写引擎奇偶校验」三分后的第一个子阶段）。
> 前置：P0（修复构建）、P1（差分测试框架 + TPC-H 语料 + dry-plan golden）、P2（parser/formatter 字节对齐）。
> 目标：让 Go 的 `WrenSqlRewrite`（模型/关系路径）产出与 Java `wren-engine:0.9.3` **字节一致**的重写后 SQL。

## 1. 背景

整体目标是让 Go 引擎 100% 替代 `ghcr.io/canner/wren-engine:0.9.3`。父项目已确定：

- **奇偶校验线**：重写后的 SQL 文本必须与 Java 引擎逐字节一致。
- **实现路线**：Approach C（混合）—— 输出关键层机械移植。

P3（重写引擎）约 6900 行 Java，超出单个实施计划的合理体量，按特性三分：

| 子阶段 | 范围 | 验证 |
|---|---|---|
| **P3a** | 模型 + 关系（分析器、树重写基础设施、`WrenSqlRewrite` 非动态路径、SqlRender） | TPC-H 22 条端到端 |
| P3b | 度量 + 累积度量 + 度量汇总 | 度量语料用例 |
| P3c | 视图 + 枚举 | 视图/枚举语料用例 |

本 spec 为 **P3a** —— 引擎主体，约 5000 行 Java。

### 体检结论

| Java（`wren-base/sqlrewrite`） | 行数 | Go 现状 |
|---|---|---|
| 重写规则 | ~550 | 惯用法近似重写，与 Java 算法结构性背离 |
| SqlRender（Model/Relationable + RelationshipRewriter） | ~840 | 缺失 |
| 树重写基础设施（BaseTreeRewriter/BaseRewriter） | ~1850 | 缺失 |
| 分析器（`sqlrewrite/analyzer/`） | ~2290 | 缺失 |
| Planner/Utils/Info 类型 | ~880 | 部分 |

现有 `internal/rewrite/`（901 行）是惯用法近似实现 —— 粗糙拓扑排序 + 巨型 switch，缺分析器、缺 SqlRender、
缺 DAG、缺 dummy CTE，不可能产出字节一致输出。**P3a 整体丢弃 `internal/rewrite/`，按 Java 结构干净
机械移植**（与 P2 重写 formatter 同理）。

TPC-H MDL 用满全部特性（5 模型 / 4 关系 / 3 度量 / 1 累积度量 / 1 枚举 / 4 视图 / 计算列），
P3a 只负责其中模型 + 关系部分；度量/视图/枚举类查询在 P3a 阶段保持已知失败，待 P3b/P3c。

## 2. 范围

### IN（P3a 负责）

- 丢弃 `internal/rewrite/` 现有全部文件，干净机械移植
- `WrenPlanner` 编排 + `WrenRule` 接口 + `Utils`（`parseSql` / `parseExpression` / `parseQuery` 等）
- 树重写基础设施：`BaseRewriter` / `BaseTreeRewriter`（泛型 AST 变换 visitor）
- 分析器 `sqlrewrite/analyzer/`：`StatementAnalyzer`、`ScopeAnalyzer`、`ExpressionAnalyzer`、
  `ExpressionRelationshipAnalyzer`、`Analysis`、`Scope`、`Field`、`RelationType`、`RelationId`、
  `ScopeAnalysis`、`ExpressionRelationshipInfo`、`RelationshipColumnInfo` 等
- `WrenSqlRewrite` **非动态字段路径**（`sessionContext.isEnableDynamicField() == false`）
- SqlRender：`RelationableSqlRender`、`ModelSqlRender`、`RelationshipRewriter`、`RelationInfo`、
  `QueryDescriptor`、`WithRewriter`、`DummyInfo`
- 模型特性：`refSql` / `baseObject` / `tableReference`、普通列、表达式列、计算列
  （含关系遍历 to-one / to-many）、关系（`OrdersCustomer` 等）、主键
- 其余 3 条规则（`GenerateViewRewrite` / `MetricRollupRewrite` / `EnumRewrite`）做**透传桩**
- 验证：TPC-H 22 条端到端 dry-plan golden 差分

### OUT（不在 P3a）

- `WrenSqlRewrite` 动态字段路径 + `WrenDataLineage`（默认 config 关闭 —— P3 之后的可选增强）
- 度量 / 累积度量 / 度量汇总（P3b）
- 视图 / 枚举（P3c）
- `CacheAnalysis`、`decisionpoint` 分析器（P5）

## 3. 架构与数据流

```
dry-plan 请求 (sql + mdl + sessionContext)
  └─> WrenPlanner.Rewrite(sql, ctx, analyzedMDL)
        statement = parseSql(sql)
        for rule in [GenerateView*, MetricRollup*, WrenSqlRewrite, Enum*]:   (* = P3a 透传桩)
            statement = rule.apply(parseSql(SqlFormatter.formatSql(statement)), ctx, analyzedMDL)
        return SqlFormatter.formatSql(statement)              ← P2 formatter 渲染字节输出
```

`WrenSqlRewrite` 非动态字段路径内部：

```
Analysis analysis = new Analysis(root)
StatementAnalyzer.analyze(analysis, root, ctx, wrenMDL)        ← 识别哪些 Table 是 model
  → analysis.getModels() / getTables() / getCollectedColumns() / getSourceNodeNames(node)
modelDescriptors = analysis.getModels().map(m -> RelationInfo.get(m, wrenMDL))   ← 经 ModelSqlRender
allDescriptors  = modelDescriptors（P3a 仅 model；metric/cumulative 为空集）
DAG: 每个 descriptor 为顶点，requiredObjects 为入边；递归 QueryDescriptor.of 补依赖对象
graph 拓扑迭代 → WithRewriter.getWithQuery(descriptor) 逐个 → withQueries
new WithRewriter(withQueries).process(root)    ← 把 model CTE 前置到查询
new Rewriter(wrenMDL, analysis).process(...)   ← Table 引用改写为 CTE 名、剥 catalog/schema 前缀
```

`RelationInfo.get(model, wrenMDL)` → `ModelSqlRender.render()` 用 `format()` 字符串模板生成 model 的
CTE 查询 SQL（`SELECT "m"."c" AS "c" FROM (...) ... LEFT JOIN ... ON ...`），经 `parseQuery` 解析为
`Query` AST。最终所有 AST 由 P2 的 `SqlFormatter` 渲染为字节输出。

## 4. 组件与文件结构

新建 `internal/rewrite/` 包（替换丢弃的旧包）+ `internal/rewrite/analyzer/` 子包。文件 1:1 镜像 Java：

| Go 文件 | 对应 Java | 体量 |
|---|---|---|
| `internal/rewrite/planner.go` | `WrenPlanner` | 小 |
| `internal/rewrite/rule.go` | `WrenRule` | 小 |
| `internal/rewrite/utils.go` | `Utils` | 中 |
| `internal/rewrite/base_rewriter.go` | `BaseRewriter` | 大 |
| `internal/rewrite/base_tree_rewriter.go` | `BaseTreeRewriter`（按 visit 分组拆多文件） | 大 |
| `internal/rewrite/wren_sql_rewrite.go` | `WrenSqlRewrite` | 中 |
| `internal/rewrite/relationable_sql_render.go` | `RelationableSqlRender` | 中 |
| `internal/rewrite/model_sql_render.go` | `ModelSqlRender` | 中 |
| `internal/rewrite/relationship_rewriter.go` | `RelationshipRewriter` | 小 |
| `internal/rewrite/relation_info.go` | `RelationInfo` | 中 |
| `internal/rewrite/query_descriptor.go` | `QueryDescriptor` | 小 |
| `internal/rewrite/with_rewriter.go` | `WithRewriter` | 小 |
| `internal/rewrite/dummy_info.go` | `DummyInfo` | 小 |
| `internal/rewrite/passthrough_rules.go` | `GenerateView`/`MetricRollup`/`Enum` 透传桩 | 小 |
| `internal/rewrite/analyzer/analysis.go` | `Analysis` | 中 |
| `internal/rewrite/analyzer/statement_analyzer.go` | `StatementAnalyzer` | 大 |
| `internal/rewrite/analyzer/scope_analyzer.go` | `ScopeAnalyzer` | 中 |
| `internal/rewrite/analyzer/expression_analyzer.go` | `ExpressionAnalyzer` | 中 |
| `internal/rewrite/analyzer/expression_relationship_analyzer.go` | `ExpressionRelationshipAnalyzer` | 中 |
| `internal/rewrite/analyzer/scope.go` `field.go` `relation_type.go` `relation_id.go` … | 同名 | 小-中 |

文件应保持聚焦、单一职责；超大 Java 文件（`BaseTreeRewriter` 1197 行）移植时按 visit 分组拆为多个 Go 文件。

## 5. 实施切片（方案 C：透传脊柱 + 由外向内增量）

每个切片自成一体：移植 → 单测 / 差分测试 → baseline 接受 fail→pass。

### 切片 0 — 透传脊柱

删除 `internal/rewrite/` 旧文件；建新包骨架；`WrenRule` 接口；`WrenPlanner.Rewrite` 编排循环；4 条规则
全部实现为透传桩（`apply` 原样返回）；接入 P1 的 dry-plan golden 差分测试 + baseline 计分板。
验证：无模型引用的查询端到端字节一致（输出 = P2 formatter 结果）。

### 切片 1 — 树重写基础设施

`BaseTreeRewriter` / `BaseRewriter`（泛型 AST 变换 visitor）。单测：恒等变换 round-trip（任意 AST 经
基础 rewriter 处理后不变）。

### 切片 2 — 分析器

`Analysis` + `StatementAnalyzer` + `ScopeAnalyzer` + `ExpressionAnalyzer` + `Scope` / `Field` /
`RelationType` / `RelationId`。单测：给定 query + MDL，`Analysis` 正确识别 `models` / `tables` /
`collectedColumns` / `sourceNodeNames`。

### 切片 3 — 关系分析

`ExpressionRelationshipAnalyzer` + `ExpressionRelationshipInfo` + `RelationshipColumnInfo` +
`RelationshipRewriter`。单测：计算列表达式（如 `customer.nation.name`）正确解析出关系链。

### 切片 4 — SqlRender

`RelationableSqlRender` + `ModelSqlRender` + `RelationInfo` + `QueryDescriptor` + `WithRewriter` +
`DummyInfo`。单测 / golden：单个 model → CTE 查询 AST 与 Java 一致。

### 切片 5 — WrenSqlRewrite 接线

非动态字段路径全装配：`Analysis` → descriptors → DAG → 拓扑迭代 → `WithRewriter` → `Rewriter`
内部类。端到端：TPC-H 模型 / 关系 / 计算列类查询逐条转绿。

## 6. 字节分歧风险登记表

| # | 风险 | 说明 |
|---|---|---|
| 1 | **CTE 顺序（最高风险）** | Java 用 `DirectedAcyclicGraph` 拓扑迭代决定 WITH CTE 顺序；喂入的 `allDescriptors` / `requiredObjects` 是 `Set`，`modelDescriptors` 来自 `.collect(toSet())`（HashSet）。Go map 随机序。CTE 顺序的确定性来源须在切片 2/5 精确确认（`Analysis` 集合类型 + 拓扑迭代算法 + 平局规则），并**以 golden 为准绳逆向校验**。 |
| 2 | Map/Set 迭代序 | `LinkedHashMap`（如 `calculatedScopeSelectItems`）保插入序；凡顺序流入输出 SQL 的，Go 必须用有序结构（slice / 有序 map）。 |
| 3 | 字符串模板逐字节 | `ModelSqlRender` 的 `format()` 模板（`SELECT %s FROM %s`、`"%s"."%s" AS "%s"`、`LEFT JOIN … ON …`、`GROUP BY 1`、Java 文本块的缩进/换行）须逐字符复刻 —— 这些字符串被 `parseQuery` 解析，错一字符 AST 即不同。 |
| 4 | CTE 拼接位序 | model CTE 必须排在用户原有 WITH 之前（`Stream.concat(withQueries, with.getQueries())`）。 |
| 5 | `Rewriter` 改写条件 | `visitTable` 仅当 `analysis.getSourceNodeNames(node).isPresent()` 才改写，改写为 `Table(QualifiedName.of(最后一段))`；`visitDereferenceExpression` 剥 `catalog.schema` / `schema` 前缀的精确条件须复刻。 |
| 6 | CTE 名为 delimited identifier | `WithRewriter.getWithQuery` 用 `new Identifier(name, true)` —— 带引号标识符。 |
| 7 | 规则间重解析 | 每轮规则间 `parseSql(SqlFormatter.formatSql(...))` 依赖 P2 formatter 幂等 + parser 往返。 |
| 8 | 动态字段默认关 | P1 golden 须以 `enable-dynamic-fields=false`（默认）捕获 —— 切片 0 验证此假设；若不符则需重捕获或调整范围。 |

## 7. 错误处理

- 分析失败 / 找不到 model / 依赖环：返回 error（对齐 Java `IllegalArgumentException`）。
- 范围外特性（动态字段开启、度量 / 视图引用）：P3a 阶段对应规则为透传桩；若查询确需该特性，端到端
  差分会失败 —— 由 baseline 计分板记录为已知失败，待 P3b / P3c。
- 不静默吞错。

## 8. 测试与验收

- **端到端差分测试**：复用 P1 `internal/difftest`，TPC-H 22 条经 `WrenPlanner.Rewrite` 重写后与
  dry-plan golden 逐字节比对。
- **单元测试**：每个切片的组件级单测（`BaseTreeRewriter` 恒等变换、`Analysis` 识别、关系链解析、
  单 model CTE 渲染）。
- **golden 验证**：切片 4 可对单 model 的渲染 SQL 建 golden。
- **baseline 计分板**：沿用 P1 机制 —— 模型类查询 fail→pass 接受；度量 / 视图类保持已知失败。
- **验收标准**：
  - TPC-H 22 条中所有「仅引用模型 / 关系 / 计算列」的查询端到端字节一致。
  - 其余查询在 baseline 中标记为待 P3b / P3c。
  - `go build ./...` 通过、`go vet ./...` 干净、`gofmt -l` 无输出。

## 9. 与其他阶段的关系

- **依赖 P0 / P1 / P2**：构建已修复、差分测试框架就位、parser/formatter 已字节对齐。
  **P2 必须先完成** —— P3a 的重写输出经 P2 的 `SqlFormatter` 渲染。
- **被 P3b / P3c 依赖**：二者在 P3a 之上复用同一套包结构与差分框架，届时把对应透传桩替换为真实规则。
- 动态字段路径 + `WrenDataLineage` 作为 P3 之后的可选增强。
- 本 spec 完成后进入 writing-plans，产出逐任务实施计划。
