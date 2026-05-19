# P3c 设计：重写引擎 —— 视图与枚举

> 子项目：P3c（P3「重写引擎奇偶校验」三分后的第三个、收官子阶段）。
> 前置：P0（构建）、P1（差分测试框架）、P2（parser/formatter 字节对齐）、**P3a（核心重写引擎）+ P3b（度量）**。
> 目标：让 Go 引擎对引用视图 / 枚举的查询产出与 Java `wren-engine:0.9.3` **字节一致**的重写后 SQL；
> P3c 完成后 TPC-H 22 条端到端全部字节一致，P3 收官。

## 1. 背景

P3（重写引擎，约 6900 行 Java）按特性三分为 P3a（模型+关系）、P3b（度量）、P3c（视图+枚举），
顺序为 P3a → P3b → P3c。本 spec 为 **P3c**。

P3c 是 P3a / P3b 的**延伸** —— 复用同一个 `internal/rewrite/` 包、同一套 Approach C 机械移植路线、
同一个 P1 差分测试框架与 baseline 计分板。P3a 把 `WrenPlanner` 的 `GenerateViewRewrite` 与
`EnumRewrite` 做成透传桩；P3c 把这两条换成真实规则。

- **View（视图）**：MDL 中以 SQL 语句定义的命名查询，可引用 model / metric / 其他 view。
  `GenerateViewRewrite` 把被引用的 view 展开为 WITH CTE。
- **Enum（枚举）**：MDL 中的命名枚举，查询中以 `EnumName.Value` 形式引用，
  `EnumRewrite` 把它替换为对应字符串字面量。

**关键依赖**：`ViewInfo.get` 内部对 view SQL 应用了 `MetricRollupRewrite`（view 内可用度量汇总语法），
故 **P3c 必须排在 P3b 之后**。

TPC-H MDL 含 4 个 view（`useModel`、`useMetric`、`useMetricRollUp`、`useUseMetric` —— 注意
`useUseMetric` 引用另一个 view，构成 view 嵌套）与 1 个 enum（`Status`），P3c 以这些为验证对象。

## 2. 范围

### IN（P3c 负责）

- `EnumRewrite` 规则 —— 替换 P3a 透传桩：
  - `BaseRewriter` 子类，`visitDereferenceExpression` 把 `EnumName.Value` 形式的
    `DereferenceExpression` 替换为 `StringLiteral`
  - 无 analyzer 依赖（`apply` 传 `null` analysis）
- `ViewInfo` —— view 的 `QueryDescriptor`：
  - `Utils.parseView(view.getStatement())` 解析 view SQL
  - `StatementAnalyzer.analyze` 分析 view 内引用
  - 对 view SQL 应用 `MetricRollupRewrite`（依赖 P3b）
  - `requiredObjects = analysis.getWrenObjectNames()`
- `GenerateViewRewrite` 规则 —— 替换 P3a 透传桩：
  - `analysis.getViews()` → `ViewInfo.get` → `DirectedAcyclicGraph` 拓扑（同 `WrenSqlRewrite` 模式）
  - 递归把被引用的 view 加入 DAG（`QueryDescriptor.of`）
  - 拓扑迭代 → `WithRewriter` 把 view CTE 前置
- 分析器补全：`StatementAnalyzer` 识别 views、`Analysis.getWrenObjectNames`
- `Utils.parseView`
- 验证：新增引用视图 / 枚举的语料用例，dry-plan golden 端到端差分

### OUT（不在 P3c）

- `WrenSqlRewrite` 动态字段路径 + `WrenDataLineage`
- `CacheAnalysis`、decisionpoint 分析器（P5）

## 3. 架构与数据流

P3c 不改 `WrenPlanner` 编排，只把两条透传桩换成真实规则。`WrenPlanner.ALL_RULES` 顺序为
`[GenerateViewRewrite, MetricRollupRewrite, WrenSqlRewrite, EnumRewrite]` —— `GenerateViewRewrite`
最先跑（把 view 展开成 CTE，后续规则再处理展开后的 model / metric 引用），`EnumRewrite` 最后跑。

**视图展开**（如 `SELECT * FROM useModel`）：

```
GenerateViewRewrite.apply:
  analysis.getViews()  → ViewInfo.get(view)   ─┐
    ViewInfo.get: parseView → StatementAnalyzer → 套 MetricRollupRewrite → QueryDescriptor
  DAG: view 顶点 + 被引用 view 为边；递归 QueryDescriptor.of 补嵌套 view
  graph 拓扑迭代 → WithRewriter.getWithQuery → view CTE 前置到查询
  （展开后的 model / metric 引用交由后续 WrenSqlRewrite 处理）
```

**枚举替换**（如 `WHERE status = Status.PROCESSING`）：

```
EnumRewrite.apply:
  visitDereferenceExpression(node):
    qn = DereferenceExpression.getQualifiedName(node)
    若 qn 为两段且第一段是 MDL 中的 enum 名:
        返回 StringLiteral( enum.valueOf(第二段).getValue() )
    否则递归处理 base
```

## 4. 组件与文件结构

在 P3a / P3b 建立的 `internal/rewrite/` 包内新增 / 修改：

| Go 文件 | 对应 Java | 操作 |
|---|---|---|
| `internal/rewrite/enum_rewrite.go` | `EnumRewrite` | 新建（替换透传桩） |
| `internal/rewrite/view_info.go` | `ViewInfo` | 新建 |
| `internal/rewrite/generate_view_rewrite.go` | `GenerateViewRewrite` | 新建（替换透传桩） |
| `internal/rewrite/utils.go` | `Utils.parseView` | 修改 |
| `internal/rewrite/query_descriptor.go` | `QueryDescriptor.of` 补 view 分支 | 修改 |
| `internal/rewrite/passthrough_rules.go` | 删除 `GenerateView` / `Enum` 透传桩 | 修改 |
| `internal/rewrite/analyzer/statement_analyzer.go` | `StatementAnalyzer` 识别 views | 修改 |
| `internal/rewrite/analyzer/analysis.go` | `Analysis.getWrenObjectNames` | 修改 |

## 5. 实施切片（方案 C：增量切片，每片端到端可验证）

每个切片：移植 → 端到端差分 / 单测 → baseline 接受 fail→pass。

- **切片 1 — EnumRewrite**：最简单、独立、无 analyzer 依赖。替换透传桩，实现
  `visitDereferenceExpression`。端到端：引用枚举的查询转绿。
- **切片 2 — 分析器补 view 识别**：`StatementAnalyzer` 识别 views，`Analysis.getWrenObjectNames`。
  单测：含 view 引用的 query → `Analysis` 正确归类。
- **切片 3 — ViewInfo**：`Utils.parseView` + `StatementAnalyzer` + 套 `MetricRollupRewrite`。
  单测 / golden：单个 view → `QueryDescriptor`。
- **切片 4 — GenerateViewRewrite 规则**：替换透传桩，`analysis.getViews()` → DAG 拓扑 →
  `WithRewriter`；递归处理嵌套 view。端到端：视图类查询（含嵌套 view）转绿。

## 6. 字节分歧风险登记表

继承 P3a 全部风险（CTE 顺序由 DAG 拓扑迭代决定、`format()` 字符串模板逐字节、Map/Set 迭代序），
另加 P3c 特有：

| # | 风险 | 说明 |
|---|---|---|
| 1 | view 嵌套 view 的依赖图 | `useUseMetric` 引用 `useMetric` —— DAG 须正确拓扑排序嵌套 view，CTE 顺序须与 Java 一致。 |
| 2 | view CTE 与 model CTE 的关系 | `GenerateViewRewrite` 先跑把 view 展开为 CTE，`WrenSqlRewrite` 后跑再展开 model；两轮 CTE 前置的叠加顺序须复刻。 |
| 3 | enum 大小写匹配 | `EnumDefinition.valueOf` 匹配枚举值名的大小写规则须与 Java 一致。 |
| 4 | enum 仅匹配两段 dereference | `EnumName.Value` 必须恰为两段 `QualifiedName`；三段及以上不视为枚举。 |
| 5 | view 内度量汇总语法 | `ViewInfo.get` 对 view SQL 套 `MetricRollupRewrite` —— 依赖 P3b 正确。 |

## 7. 错误处理

- 找不到 view / enum 值 / view 依赖环：返回 error（对齐 Java `IllegalArgumentException`）。
- 不静默吞错。

## 8. 测试与验收

- **端到端差分测试**：复用 P1 `internal/difftest`，新增引用 TPC-H MDL 视图与枚举的语料用例
  （`useModel` / `useMetric` / `useMetricRollUp` / `useUseMetric` / `Status`），可从 Java `TestView` /
  `TestEnumRewrite` 测试移植。
- **单元测试**：每切片组件级单测（`EnumRewrite` 替换、分析器 view 识别、`ViewInfo` 渲染）。
- **baseline 计分板**：沿用 P1，视图 / 枚举类查询 fail→pass 接受。
- **验收标准**：
  - 新增视图 / 枚举语料用例端到端 dry-plan golden 字节一致。
  - **TPC-H 22 条端到端 dry-plan golden 全部字节一致** —— P3 收官目标。
  - `go build ./...` 通过、`go vet ./...` 干净、`gofmt -l` 无输出。
  - P3a / P3b 已通过的查询不回归。

## 9. 与其他阶段的关系

- **依赖 P0 / P1 / P2 / P3a / P3b**：复用 P3a 的树重写基础设施、analyzer、`QueryDescriptor` /
  `WithRewriter` / DAG 机制；`ViewInfo` 复用 P3b 的 `MetricRollupRewrite`。
- **P3 收官**：P3c 完成即标志重写引擎奇偶校验（非动态字段路径）达成 —— Go 引擎对 TPC-H 全语料
  产出与 Java 字节一致的重写后 SQL。
- 动态字段路径 + `WrenDataLineage`、分析 / 校验子系统（P4 / P5 / P6）为后续阶段。
- 本 spec 完成后进入 writing-plans，产出逐任务实施计划。
