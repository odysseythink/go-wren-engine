# P5 设计：分析子系统 decisionpoint

> 子项目：P5（七阶段拆解中的第五阶段）。
> 前置：P0（构建）、P1（差分测试框架）、P2（parser/formatter 字节对齐）、P3a（重写引擎的分析器）。
> 目标：让 Go 引擎的 `/v1/analysis/sql`、`/v2/analysis/sql`、`/v2/analysis/sqls` 端点产出与 Java
> `wren-engine:0.9.3` 结构化等价的查询分析 JSON。

## 1. 背景

`wren-engine:0.9.3` 的分析端点对**用户输入的 SQL** 做结构化拆解（"decision points" —— 查询用了哪些
关系、过滤、列、分组、排序），供前端展示查询结构。核心是 `sqlrewrite/analyzer/decisionpoint/` 包
（10 文件、1454 行）。

`DecisionPointAnalyzer.analyze(Statement query, SessionContext sessionContext, WrenMDL mdl)` 在 AST 上
工作，并复用 P3a 移植的 `sqlrewrite/analyzer/Analysis` 与 `StatementAnalyzer`。

**奇偶校验线（P5）**：分析端点返回的 JSON 与 Java **结构化等价**（两边各自 parse 成树后比对，
容忍键序 / 空白 / 数字格式差异）。

Go 现状：`internal/analyzer/decisionpoint/` 是空目录，`internal/server/analysis_handler.go`（80 行）
为桩。

## 2. 范围

### IN（P5 负责）

- `decisionpoint/` 包全套机械移植（10 文件 1454 行）：
  - `DecisionPointAnalyzer`（顶层，232）
  - `QueryAnalysis`（292）、`RelationAnalysis`（208）、`FilterAnalysis`（113）—— 分析结果结构
  - `RelationAnalyzer`（279）、`FilterAnalyzer`（74）、`DecisionExpressionAnalyzer`（83）、
    `ExpressionLocationAnalyzer`（69）—— 各维度分析器
  - `ExprSource`（46）、`DecisionPointContext`（58）—— 上下文与辅助类型
- HTTP 端点：`/v1/analysis/sql`、`/v2/analysis/sql`、`/v2/analysis/sqls`
  （`AnalysisResource`、`AnalysisResourceV2`）
- 输出 DTO：分析结果的 JSON 结构

### OUT（不在 P5）

- DuckDB 连接器 / 执行（P4）
- 校验 / 配置（P6）

## 3. 架构与数据流

```
POST /v1/analysis/sql  (sql + manifest)
  └─> mdl = WrenMDL.fromManifest(manifest)
      stmt = parseSql(sql)                                         ← P2 parser
      analysis = Analysis(stmt); StatementAnalyzer.analyze(...)     ← 复用 P3a 分析器
      List<QueryAnalysis> = DecisionPointAnalyzer.analyze(stmt, ctx, mdl)
        每个 QueryAnalysis：relations / filters / selectItems / groupBy / sortings / ...
      → JSON 响应
```

`decisionpoint` 分析纯在 AST 上做，不重写、不执行。它依赖 P2（parser/AST）与 P3a（`Analysis` /
`StatementAnalyzer`），但与 P3b / P3c / P4 解耦 —— 可与它们并行实施。

整个 `decisionpoint` 包是 P5 的字节关键层（输出须结构化等价）—— 机械移植；HTTP 层惯用 Go 重写。

## 4. 组件与文件结构

新建 `internal/analyzer/decisionpoint/` 子包，文件 1:1 镜像 Java：

| Go 文件 | 对应 Java |
|---|---|
| `internal/analyzer/decisionpoint/decision_point_analyzer.go` | `DecisionPointAnalyzer` |
| `internal/analyzer/decisionpoint/query_analysis.go` | `QueryAnalysis` |
| `internal/analyzer/decisionpoint/relation_analysis.go` | `RelationAnalysis` |
| `internal/analyzer/decisionpoint/relation_analyzer.go` | `RelationAnalyzer` |
| `internal/analyzer/decisionpoint/filter_analysis.go` | `FilterAnalysis` |
| `internal/analyzer/decisionpoint/filter_analyzer.go` | `FilterAnalyzer` |
| `internal/analyzer/decisionpoint/decision_expression_analyzer.go` | `DecisionExpressionAnalyzer` |
| `internal/analyzer/decisionpoint/expression_location_analyzer.go` | `ExpressionLocationAnalyzer` |
| `internal/analyzer/decisionpoint/expr_source.go` | `ExprSource` |
| `internal/analyzer/decisionpoint/context.go` | `DecisionPointContext` |
| `internal/server/analysis_handler.go` | `AnalysisResource` / `AnalysisResourceV2` |
| `internal/dto/analysis_response.go` | 分析结果 JSON DTO |

## 5. 实施切片（方案 C：增量切片）

每个切片：移植 → golden 差分 / 单测 → baseline 接受 fail→pass。

- **切片 0 — 端点骨架 + 验证回路**：HTTP 端点骨架；接 P1 golden 差分（捕获 Java analysis 端点
  JSON）+ baseline；JSON 结构化等价比对器。
- **切片 1 — 分析结果结构**：`QueryAnalysis` / `RelationAnalysis` / `FilterAnalysis` /
  `ExprSource` / `DecisionPointContext` 结构体 + JSON DTO。
- **切片 2 — RelationAnalyzer**：关系 / FROM 子句分析。
- **切片 3 — FilterAnalyzer + 表达式分析**：`FilterAnalyzer` + `DecisionExpressionAnalyzer` +
  `ExpressionLocationAnalyzer`。
- **切片 4 — DecisionPointAnalyzer 顶层装配**：串起各分析器，产出 `QueryAnalysis` 列表。
- **切片 5 — HTTP 端点**：`/v1/analysis/sql`、`/v2/analysis/sql`、`/v2/analysis/sqls`。

## 6. 字节分歧风险登记表

| # | 风险 | 说明 |
|---|---|---|
| 1 | 表达式文本位置 | `ExpressionLocationAnalyzer` 输出 SQL 文本中的行 / 列偏移，依赖 ANTLR token 位置；Go parser 的 token 位置须与 Java 一致。 |
| 2 | JSON 字段命名 / 嵌套 | 分析结果 DTO 的字段名、嵌套层级、可选字段处理须与 Java 一致（结构化等价容忍键序，但字段名 / 结构须相同）。 |
| 3 | 复用 P3a 分析器 | `DecisionPointAnalyzer` 用 `Analysis` / `StatementAnalyzer` —— 依赖 P3a 正确。 |
| 4 | 子查询 / CTE 上下文 | `DecisionPointContext.isSubqueryOrCte` 等上下文判定须复刻。 |

## 7. 错误处理

- 解析失败 / manifest 缺失：按 Java `WrenExceptionMapper` 格式返回错误 JSON。
- 不静默吞错。

## 8. 测试与验收

- **端到端差分测试**：golden 快照 —— 捕获 Java analysis 端点 JSON，Go 端点输出结构化等价比对。
- **语料**：TPC-H 22 条 + 额外覆盖各分析维度（关系 / 过滤 / 分组 / 排序 / 子查询 / CTE）的查询，
  可从 Java `TestDecisionPointAnalyzer` 移植。
- **单元测试**：每切片组件级单测。
- **baseline 计分板**：沿用 P1。
- **验收标准**：
  - 全部分析语料端到端结构化等价。
  - `go build ./...` 通过、`go vet ./...` 干净、`gofmt -l` 无输出。

## 9. 与其他阶段的关系

- **依赖 P0 / P1 / P2 / P3a**：在 AST 上分析，复用 P3a 的 `Analysis` / `StatementAnalyzer`。
- **与 P3b / P3c / P4 解耦**：不重写、不执行，可与它们并行实施。
- 本 spec 完成后进入 writing-plans，产出逐任务实施计划。
