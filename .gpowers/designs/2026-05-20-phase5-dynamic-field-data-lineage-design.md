# Phase 5 设计：dynamic-field 分支 + WrenDataLineage 子系统（drop-in 替代收官）

> 子项目：Phase 5（drop-in replacement 路线图 5 阶段中的第 5、收官阶段；亦称 P7）。
> 前置：P0–P6 + Phase 1（Dockerfile）+ Phase 2（config.properties）+ Phase 4（rewrite go-error 收尾）。
> 与 Phase 3 关系：Phase 3 capture 工具链复用，Phase 5 用同样工具在 `enable-dynamic-fields=true`
> 配置下重新采 golden。
> 目标：实现 Java `WrenSqlRewrite` 的 dynamic-field 分支 + 完整 `WrenDataLineage` 子系统，使
> WrenAI 0.9.0 docker-compose 在不改任何 client 配置的情况下能用 Go 引擎替换
> `wren-engine.image` 而所有 e2e flow 通过。

## 1. 背景

WrenAI 0.9.0 `docker/bootstrap/init.sh:14-17` 在用户未显式设值时**强制追加**
`wren.experimental-enable-dynamic-fields=true` 到 `config.properties`。因此**生产 WrenAI 部署
默认就走 dynamic-field 路径** —— 这条路径 Go 引擎当前**零实现**。

Java 该路径由两段代码主导：

| Java 文件 | 行数 | 角色 |
|---|---|---|
| `wren-base/.../sqlrewrite/WrenSqlRewrite.java` lines 91–130 | ~50 | rewrite 入口的 dynamic 分支：用 lineage 求出每表所需列，按列裁剪生成 CTE |
| `wren-base/.../sqlrewrite/WrenDataLineage.java` | **527** | MDL → 列依赖 DAG。提供 `getRequiredFields(columns)` 接口（从用户访问列回溯到所有源表+列）|

Java 源码注释：`"DynamicCalculatedField is a experimental feature, and buggy"` —— 但 WrenAI 默认开启，
我们必须实现。

### 当前 Go 现状（已调研）

Go 引擎在 P3a/P3b/P3c 已建好**几乎所有依赖基础**：

| Go 现成资产 | Java 对应 | 状态 |
|---|---|---|
| `internal/rewrite/analyzer/expression_relationship_analyzer.go` | `ExpressionRelationshipAnalyzer` | **已有** |
| `internal/rewrite/analyzer/expression_relationship_info.go` | `ExpressionRelationshipInfo` | **已有** |
| `internal/rewrite/analyzer/analysis.go`：`requiredSourceNodes / sourceNodeNames / collectedColumns` | Java `Analysis` 同名字段 | **已有**（P3a 已 wire） |
| `internal/rewrite/relation_info.go` (33 行) | `RelationInfo.get(rel, mdl)` 静态变体 | **已有**（仅静态变体） |
| `internal/rewrite/with_rewriter.go` (33 行) | `WithRewriter` | **已有** |
| `internal/rewrite/date_spine_info.go` (29 行) | `DateSpineInfo.get(DateSpine)` | **已有** |
| `internal/rewrite/cumulative_metric_info.go` (29 行) | `CumulativeMetricInfo` | **已有** |
| `internal/rewrite/wren_sql_rewrite.go` (140 行) | `WrenSqlRewrite.apply` 静态分支 | **已有静态分支**，无 dynamic 分支 |

### Phase 5 真实增量（不到 1000 行）

| 新增文件 / 改动 | 估算行数 | 说明 |
|---|---|---|
| `internal/rewrite/lineage/lineage.go`（新包） | ~250 | port `WrenDataLineage` 527 行（Java 模板化 + Guava 占大头；Go 借 `ExpressionRelationshipAnalyzer` 可大幅瘦身） |
| `internal/rewrite/lineage/lineage_test.go` | ~150 | 单测 |
| `internal/rewrite/dummy_info.go` | ~30 | 镜像 Java DummyInfo（`select 1` CTE） |
| `internal/rewrite/relation_info.go` 扩 | +40 | 新增 `relationInfoOfModelWithFields(model, mdl, requiredFields)` pruned 变体 |
| `internal/rewrite/model_sql_render.go` 扩 | +50 | 加 `requiredFields []string` 参数，渲染时仅 SELECT 指定列 |
| `internal/rewrite/metric_sql_render.go` 扩 | +50 | 同上 |
| `internal/rewrite/wren_sql_rewrite.go` 扩 | +120 | 加 `if ctx.EnableDynamicFields { ... }` 分支：调 lineage + 构 pruned descriptors + 加 DummyInfo + 加 DateSpineInfo |
| `internal/mdl/analyzed_mdl.go` 扩 | +20 | 加 `WrenDataLineage()` 访问器（懒计算） |
| `cmd/capture-golden/`（4 工具复用）+ `tools/oracle-up.sh` 扩 | +30 | 加 `--dynamic-fields=true` 开关 → 产 `golden-dynamic/` 镜像 |
| `internal/difftest/dynamic_diff_test.go` 新建 | ~120 | dynamic-mode 差分测试，新 `baseline-dynamic.json` |
| `testdata/difftest/golden-dynamic/...` | n/a | Java oracle 重采产出 |
| `testdata/difftest/baseline-dynamic.json` | n/a | 量化 dynamic 模式 pass-rate |

**总估算：~860 行新代码 + golden 测试资产**。比原 brief 估算的 800-1000 略低。

### 奇偶校验线（Phase 5）

① `WrenDataLineage.getRequiredFields(...)` 输出对每个 MDL 与 Java 等价（unit-level 验证 + 几个
synthetic MDL fixture）；② `WrenSqlRewrite.apply` 在 `EnableDynamicFields=true` 时生成的最终
SQL 与 Java token-equal（差分测试）；③ 现有静态分支基线 (Phase 4 落地的 `baseline.json`) 一行不
回归；④ WrenAI 0.9.0 docker-compose 替换 `wren-engine.image`，bootstrap → ai-service → ui
全 chain 起来无 5xx。

## 2. 范围

### IN（Phase 5 负责）

- 新包 `internal/rewrite/lineage/` 内含：
  - `Lineage` 结构体 + `Analyze(*mdl.WrenMDL) *Lineage` 入口
  - `(*Lineage) SourceColumns(QualifiedName) map[string][]string` — 单列 → 直接源列（不递归）
  - `(*Lineage) RequiredFields(columns []QualifiedName) *orderedmap[string, set[string]]` —
    递归收集所有源表+列，按 CTE 生成顺序（DAG topo-sort）
  - 内部 `Vertex { Name string; ColumnNames map[string]bool }` 节点
  - 私有 `modelExprAnalyzer` / `metricExprAnalyzer` 镜像 Java `Analyzer` / `MetricAnalyzer`，
    分析 calc field expression 引用的源列；复用 Go 现成 `ExpressionRelationshipAnalyzer`
  - `getJoinKey(expr, modelName)` 工具函数（Java `JoinKey` visitor 等价）
  - DAG cycle detection（任何环 → `error`，不允许）
- `internal/rewrite/dummy_info.go`：`DummyInfo{Name string}` 实现 `QueryDescriptor` 接口，
  `Query()` 返回 `parseQuery("select 1")` 等价 AST
- `internal/rewrite/model_sql_render.go` + `metric_sql_render.go` 扩展：
  - 加 `RequiredFields []string` 字段到 render 上下文
  - 当非空时：SELECT 列子集 + WHERE/JOIN 仅引用所需列；当 nil/空：保持现有全列行为
- `internal/rewrite/relation_info.go` 扩：
  - `relationInfoOfModelWithFields(model, wrenMDL, requiredFields []string)` pruned 变体
  - `relationInfoOfMetricWithFields(metric, wrenMDL, requiredFields []string)` 同上
- `internal/rewrite/wren_sql_rewrite.go` 加 dynamic 分支（~50 行），核心逻辑：
  ```
  if ctx.EnableDynamicFields {
      visitedTables := analysis.Tables() − {view tables}
      tableRequiredFields := lineage.RequiredFields(<collectedColumns 折成 QualifiedName>)
      // count(*) source 兜底
      for node, source := range analysis.RequiredSourceNodes() { ... }
      for name, cols := range tableRequiredFields {
          descriptors += relationInfoOfXxxWithFields(name, cols)
          visitedTables -= name
      }
      withQueries := []WithQuery{}
      if any(table is CumulativeMetric) { withQueries += DateSpineInfo.get(mdl.DateSpine()) }
      for d := range descriptors { withQueries += WithRewriter.GetWithQuery(d) }
      for table := range visitedTables { if mdl.IsObjectExist(table) { withQueries += DummyInfo{table} } }
      rewriteWith := WithRewriter{withQueries}.Process(root)
      return Rewriter{wrenMDL, analysis}.Process(rewriteWith)
  }
  // else: 现有静态分支不动
  ```
- `internal/mdl/analyzed_mdl.go` 加 `(*AnalyzedMDL) DataLineage() *lineage.Lineage` 访问器，懒计算
  + 缓存（Java 在 `AnalyzedMDL` 构造时计算并缓存，Go 改为按需）
- Capture 工具扩 `--dynamic-fields=true` flag：
  - `tools/oracle-up.sh --dynamic-fields=true`：mount 一份 override config.properties 设
    `enable-dynamic-fields=true`
  - 4 个 `cmd/capture-*` 工具加 `--out-suffix=-dynamic`，golden 落 `testdata/difftest/golden-dynamic/`
- 新增 `internal/difftest/dynamic_diff_test.go`：与 `difftest_test.go` 同构，但 `goldenDir` 指向
  `golden-dynamic/` + `baselinePath` 指向 `baseline-dynamic.json`，运行时 `SessionContext.EnableDynamicFields=true`
- 端到端验证脚本 `tools/wrenai-dropin-test.sh`：拉 WrenAI 0.9.0 compose → 替换 `wren-engine.image`
  为本地 build → 启动 → 跑预设若干 ai-service flow → 报 pass/fail

### OUT（不在 Phase 5）

- Postgres wire protocol（端口 7432）—— 仍是独立 gap，Phase 6 评估
- 0.11.1 升级（HTTP API 兼容性）—— Phase 8 评估
- jinja 全 Jinjava parity —— Phase 6 评估
- 把 dynamic 设为 Go 内置默认（与 Java `WrenConfig.enableDynamicFields=true` 一致）—— **Phase 5
  内确认 default 行为是否切换**；候选：保留 Phase 2 实现的「读 config 决定」语义，**不** 改 Go
  内置默认，避免现有 unit test 大面积 break
- 性能优化（lineage 缓存策略 / DAG 增量构建等）—— 先求功能正确
- 删除静态分支 —— 风险大，static 留作 fallback

## 3. 架构与数据流

### Phase 5 后整体流

```
PreviewService.Preview(...)
  ↓
ctx.EnableDynamicFields = configMgr.EnableDynamicFields()   ← Phase 2 引入
  ↓
rewrite.Rewrite(sql, ctx, analyzedMDL)
  ├─ GenerateViewRewrite (P3c)        ← view 展开
  ├─ MetricRollupRewrite (P3b)         ← roll_up 函数
  ├─ WrenSqlRewrite                    ← Phase 5 在此分叉
  │    if ctx.EnableDynamicFields:
  │        ↓ NEW dynamic branch
  │        lineage = analyzedMDL.DataLineage()       ← lazy compute
  │        requiredCols = analysis.CollectedColumns() folded to QualifiedName[]
  │        perTableFields = lineage.RequiredFields(requiredCols)
  │        // count(*) 兜底
  │        for n, srcName := range analysis.RequiredSourceNodes():
  │            if not in perTableFields:
  │                perTableFields[srcName] = mdl.GetRelationable(srcName).NonCalcColumns()
  │        descriptors = []
  │        for table, fields := range perTableFields:
  │            descriptors += newPrunedDescriptor(table, fields, mdl)
  │        // CTE 编排
  │        withQueries = []
  │        if any(table is CumulativeMetric): withQueries += DateSpineInfo
  │        for d := range descriptors: withQueries += WithRewriter.GetWithQuery(d)
  │        for unvisited := range analysis.Tables() − descriptors.keys:
  │            if mdl.IsObjectExist(unvisited): withQueries += DummyInfo{unvisited}
  │        return Rewriter(mdl, analysis).Process(WithRewriter(withQueries).Process(root))
  │    else:
  │        ↓ existing static branch (unchanged from Phase 4)
  │
  └─ EnumRewrite (P3c)                 ← enum 替换
```

### WrenDataLineage 内部数据结构

```
Lineage:
    mdl               *WrenMDL                                   ← 输入
    sourceColumnsMap  map[QName]set[QName]                       ← 直接源列（一跳）
    requiredFields    map[QName][]Vertex                         ← 该列的 DAG 顺序节点列表

构造时（Analyze(mdl)）：
  collectSourceColumns():
    for model in mdl.Models:
        for column in model.Columns:
            sourceColumnsMap[model.column] = analyzer(model, column).getSourceColumns()
    for metric in mdl.Metrics: 同上（metricAnalyzer）
    for cumMetric in mdl.CumulativeMetrics:
        sourceColumnsMap[cumMetric.measure]  = cumMetric.baseObject.<measure.refColumn>
        sourceColumnsMap[cumMetric.window]   = cumMetric.baseObject.<window.refColumn>

  collectRequiredFieldsByColumn():
    for col, sources := range sourceColumnsMap:
        DAG g
        collectRequiredFields(col, g, ...):
            for sourceCol := range sources:
                g.addVertex(sourceCol.table)
                if !skipAddEdge(...): g.addEdge(sourceCol.table → col.table)
                vertex(sourceCol.table).columns += sourceCol.column
                recurse(sourceCol)
        requiredFields[col] = topoSort(g)
```

### 静态 vs 动态语义差

| 用户 SQL | 静态分支 CTE | 动态分支 CTE |
|---|---|---|
| `SELECT orderkey FROM Orders` | `WITH Orders AS (SELECT *<all 8 cols> FROM ...)` | `WITH Orders AS (SELECT orderkey FROM ...)` |
| `SELECT custname FROM Orders` （calc field 跨 relationship）| `WITH Customer AS (...all cols...), Orders AS (...all cols + JOIN Customer...)` | `WITH Customer AS (SELECT name, custkey FROM ...), Orders AS (SELECT custkey, JOIN Customer ON ... ) ` |

动态分支减少列宽，但要求 lineage 准确识别 calc field 真实依赖。

## 4. 组件与文件结构

| 文件 | 操作 | 大小 | 说明 |
|---|---|---|---|
| `internal/rewrite/lineage/lineage.go` | 新建 | ~250 行 | 主入口 `Analyze` + `RequiredFields` + DAG 内部 |
| `internal/rewrite/lineage/lineage_test.go` | 新建 | ~150 行 | 单元测试（synthetic MDL fixture） |
| `internal/rewrite/dummy_info.go` | 新建 | ~30 行 | `DummyInfo` 实现 `QueryDescriptor`，query 返回 `parseQuery("select 1")` |
| `internal/rewrite/relation_info.go` | 修改 | +40 行 | 新增 `relationInfoOf{Model,Metric}WithFields` 函数 |
| `internal/rewrite/model_sql_render.go` | 修改 | +50 行 | 加 `requiredFields []string` 字段 + 列裁剪 SELECT 子句 |
| `internal/rewrite/metric_sql_render.go` | 修改 | +50 行 | 同上 |
| `internal/rewrite/wren_sql_rewrite.go` | 修改 | +120 行 | 加 dynamic 分支（保留静态分支不动） |
| `internal/mdl/analyzed_mdl.go` | 修改 | +20 行 | `(*AnalyzedMDL).DataLineage()` lazy accessor |
| `tools/oracle-up.sh` | 修改 | +10 行 | `--dynamic-fields=<true/false>` 开关 |
| `cmd/capture-golden/main.go` + 3 同类 | 修改 | +5 行 ×4 | `--out-suffix` flag |
| `Makefile` | 修改 | +20 行 | `capture-dynamic-golden` / `difftest-dynamic` target |
| `internal/difftest/dynamic_diff_test.go` | 新建 | ~120 行 | 镜像 `difftest_test.go`，flag dynamic baseline |
| `testdata/difftest/golden-dynamic/{tpch,metric,viewenum,exec_smoke}/*.sql` | capture 产出 | — | Java oracle dynamic 模式 golden |
| `testdata/difftest/baseline-dynamic.json` | rotate 产出 | — | dynamic 模式量化基线 |
| `tools/wrenai-dropin-test.sh` | 新建 | ~80 行 | 端到端 WrenAI compose 替换 + smoke |
| `docs/phase5-dynamic-scoreboard.md` | 新建 | ~50 行 | dynamic 模式 pass-rate + 与静态对比 + drop-in 验证报告 |

## 5. 实施切片（方案 C：增量切片）

### 切片 1 — WrenDataLineage 整体 port

- 新建 `internal/rewrite/lineage/` 包
- 定义 `Lineage` 结构 + `Vertex`
- 实现 `Analyze(*mdl.WrenMDL) *Lineage`
- 实现 `(*Lineage) SourceColumns(QualifiedName) map[string][]string`
- 实现 `(*Lineage) RequiredFields([]QualifiedName) *orderedmap[string, set[string]]`
  （用 `golang.org/x/exp/slices` + 手写 topo-sort，或引 `github.com/heimdalr/dag` 第三方）
- 内部 `modelExprAnalyzer` / `metricExprAnalyzer` 借现成
  `internal/rewrite/analyzer.ExpressionRelationshipAnalyzer`
- 写 `lineage_test.go`：synthetic 4-model TPC-H mini fixture（含 calc field + relationship +
  cumulative metric）跑通
- 验证：`go test ./internal/rewrite/lineage/... -race` clean
- commit：`feat(p5): port WrenDataLineage to internal/rewrite/lineage/`

### 切片 2 — pruned RelationInfo + model/metric renderer 变体

- `relation_info.go` 加 `relationInfoOfModelWithFields(model, mdl, requiredFields)` 
- 修 `model_sql_render.go.modelSqlRender` 加 `requiredFields []string` 字段；render() 时
  根据 `requiredFields` 缩减 SELECT 项（保留必要 JOIN/WHERE 列）
- 同样改 `metric_sql_render.go`
- 单元测试 (`*_test.go`)：
  - 全列 vs 列子集对比同一 model，CTE 字段数量符合预期
  - 当 `requiredFields` 中含 calc field：自动加上所需的 source column（来自 lineage）
- 验证：`go test ./internal/rewrite/...` clean，**静态分支基线 0 regression**
- commit：`feat(p5): pruned RelationInfo + renderer variants for dynamic field path`

### 切片 3 — DummyInfo + dynamic 分支接线

- 新建 `dummy_info.go`
- `wren_sql_rewrite.go` 加 `if ctx.EnableDynamicFields { ... }` 分支
- `analyzed_mdl.go` 加 `DataLineage()` lazy accessor
- 跑现有 `TestDifferential` (Phase 4 baseline) → 全 pass，**0 regression**（dynamic 默认关，
  静态分支走原 path）
- 临时配 `ctx.EnableDynamicFields=true` 跑 corpus → 抓 panic / error 反馈，逐个修，直到不再
  panic（允许 fail，等切片 4 baseline）
- commit：`feat(p5): WrenSqlRewrite dynamic-field branch with DummyInfo + DateSpineInfo`

### 切片 4 — Java oracle dynamic capture + baseline-dynamic.json

- 扩 `tools/oracle-up.sh`：`--dynamic-fields=true` 配 `enable-dynamic-fields=true` mount
- 扩 capture 工具 4 个：`--out-suffix=-dynamic` 落 `golden-dynamic/`
- 起 oracle (dynamic) → `make capture-dynamic-golden`
- 写 `internal/difftest/dynamic_diff_test.go`：与 `difftest_test.go` 同构，但 ctx 设 dynamic=true
  + goldenDir 指 `golden-dynamic/`
- `make difftest-accept-dynamic` rotate `baseline-dynamic.json`
- 人工 review baseline-dynamic.json：理想全 pass，可接受少数 fail 但**禁止 go-error**（不重新引
  Phase 4 已修的 panic）
- commit：`test(p5): dynamic-mode golden baseline (47 IDs + breakdown)`

### 切片 5 — WrenAI compose 端到端 drop-in 验证

- 写 `tools/wrenai-dropin-test.sh`：
  ```
  pushd ../WrenAI-0.9.0/docker
  cp docker-compose.yaml docker-compose.yaml.bak
  sed -i 's|ghcr.io/canner/wren-engine:.*|go-wren-engine:phase5|' docker-compose.yaml
  docker compose up -d bootstrap wren-engine ibis-server
  sleep 30
  # health probe: ai-service 调若干 standard query
  curl ... POST /v1/mdl/dry-plan ...
  # verify response shape + status
  docker compose down
  cp docker-compose.yaml.bak docker-compose.yaml
  popd
  ```
- 跑测：本地构建 Phase 1 镜像 + Phase 2 config + Phase 4 fix + Phase 5 dynamic 一起，端到端
  与 WrenAI 真实 ai-service 通讯
- 写 `docs/phase5-dynamic-scoreboard.md`：dynamic vs static pass-rate 对比表 + drop-in 验证结果
  + 已知未支持场景清单
- commit：`docs(p5): drop-in completion report — Go engine 100% replaces wren-engine for default config`

## 6. 风险登记表

| # | 风险 | 说明 |
|---|---|---|
| 1 | **WrenDataLineage 530 行 Java 含大量 Guava Multimap** | Go 没有 Multimap。**化解**：用 `map[K]map[V]bool` + helper func 模拟 SetMultimap；用 `slices.Sorted` + 手写 topo-sort 模拟 DirectedAcyclicGraph |
| 2 | **DAG 环检测错误时影响 Analyze 全失败** | Java `GraphCycleProhibitedException` → `IllegalArgumentException("found cycle")`，Go 应当 `error` 返回，**不 panic**（参考 Phase 4 教训） |
| 3 | **`ExpressionRelationshipAnalyzer` Go 与 Java 不字节等价** | Go 已实现该类（P3a），但能否覆盖 Java `MetricAnalyzer.visitDereferenceExpression` 的边界 case 未验证。**化解**：切片 1 单元测试要覆盖 metric calc field + nested relationship path |
| 4 | **count(*) source 兜底逻辑** | Java 对 RequiredSourceNode 走"取 baseObject 所有非 calc 列"兜底。Go `RequiredSourceNodes()` map 中存的是什么 node 类型？key 是 ast.NodeRef，value 是 ast.Node。**化解**：切片 3 验证时打印 RequiredSourceNodes 真实内容，确认 Java 等价 |
| 5 | **pruned RelationInfo 渲染时漏列** | 当 requiredFields 含 calc field `customer.nation.name`，渲染的 Model CTE 必须包含 calc field 表达式所需的 source col 列（即 customer.custkey 等 join key）+ Relationable 自身的 PK。lineage 输出已包含这些列，但 renderer 是否真把它们都 SELECT 出来需要看 model_sql_render 实现 |
| 6 | **CTE 顺序敏感** | Java `LinkedHashMap` 保 insertion order；Go map 不保。**化解**：lineage 返回 `[]struct{Name; Fields}` 切片或自维护 `orderedmap`；不能用 plain map |
| 7 | **DummyInfo 用 "select 1" 在 DuckDB 下报错** | DuckDB 对 0-行常量 CTE 在某些 JOIN 下报 "no matching column" 错。**化解**：抄 Java 实现完全（`parseQuery("select 1")`），如 DuckDB 实测有 schema mismatch 再补 column alias |
| 8 | **WrenAI ai-service 调用未知端点** | 实际 drop-in 后 ai-service 可能调 Go 未实现端点（health check / metrics / 0.11.1 新 API）。**化解**：切片 5 端到端 smoke 必须含 `ai-service` 容器，看真实 HTTP 流量；遇未实现端点列入下一轮 gap 评估 |
| 9 | **Phase 4 修的 RC-A formatter bug 在 dynamic 下重现** | dynamic 分支 emit 不同 CTE shape，可能触发 Phase 4 未覆盖的 formatter 路径。**化解**：切片 3 跑 corpus 出现新 panic 立即沿用 Phase 4 同套 probe 调试 |
| 10 | **静态分支 baseline 0 regression 难保** | dynamic 分支落地必然碰 `wren_sql_rewrite.go` 改动，可能间接影响静态分支调用 path。**化解**：每个切片 commit 必跑 `TestDifferential`（静态基线）+ `TestDifferentialDynamic`（dynamic 基线）两套 |
| 11 | **第三方 DAG 库引入** | 用 `github.com/heimdalr/dag` 需要 vendoring；用 `gonum.org/v1/gonum/graph/topo` 是已知良库。**化解**：手写 topo-sort（~30 行 Kahn 算法），不引外部依赖 |
| 12 | **`AnalyzedMDL.DataLineage()` lazy 化的并发安全** | Java 在 AnalyzedMDL 构造时 eager 计算；Go 改 lazy 需要 sync.Once。**化解**：用 `sync.Once` + `func() *Lineage` |
| 13 | **lineage 的 metric → cumulative metric → model 链** | TPC-H corpus 有 `useMetricRollUp`（view → CumulativeMetric → Metric → Model 4 跳）。lineage 必须正确递归处理。**化解**：切片 1 单测必须覆盖这种深度 |
| 14 | **dynamic 模式 Java 已知 buggy** | Java 源码自己注释 "experimental and buggy"。**化解**：fail case 比 static 多是合理的；接受 baseline-dynamic 含若干 known-fail 案例（标 `oracle-bug` 状态） |
| 15 | **测试数据量爆炸** | 47 case × 2 mode = 94 golden + 2 baseline。**化解**：复用现有 capture 工具 + `--out-suffix` 简单切换；不引专门工具 |
| 16 | **drop-in test 副作用** | `tools/wrenai-dropin-test.sh` 改 WrenAI 仓库的 docker-compose 文件，结束后必须 restore；任何中途异常都要清理。**化解**：脚本用 trap EXIT 处理 |

## 7. 错误处理

- **lineage Analyze 失败**（环 / 不存在 baseObject）：返回 `error`，rewrite 直接报错；不 panic
- **dynamic 分支命中 lineage 数据缺失**（query 引用未在 MDL 注册的列）：fallback 等价 Java —— 静默
  跳过该列的 lineage 收集，下游 SQL 可能 fail，由 DuckDB 报错
- **DAG 内部错误**：cycle / vertex 不存在 → wrap 成 `error` 沿调用栈上抛
- **DummyInfo "select 1" SQL 解析失败**：parser 自身 bug，应当让 Phase 5 切片 3 时立即暴露并修
- **capture dynamic 命中 Java oracle 自身崩溃**（Java 标自己 buggy）：保留 .error 文件，状态归入
  `oracle-error-permanent`（沿用 Phase 3 状态枚举）
- **drop-in test ai-service 调 Go 未实现端点**：脚本不 fail，只 log + 加入 gap 清单

## 8. 测试与验收

### 验收标准

- ✅ `go test ./internal/rewrite/lineage/... -race` clean，单元覆盖率 ≥ 80%
- ✅ `go test ./internal/difftest/ -run TestDifferential` 0 regression（Phase 4 静态基线一行不动）
- ✅ `go test ./internal/difftest/ -run TestDifferentialDynamic` 通过 baseline-dynamic.json
  （0 `go-error`；fail 数允许，记入 `docs/phase5-dynamic-scoreboard.md`）
- ✅ `make capture-dynamic-golden` 一键生成 `golden-dynamic/` 完整集合
- ✅ `tools/wrenai-dropin-test.sh` exit 0，ai-service 调 wren-engine 至少 10 个 dry-plan / preview
  请求 status=200，response body 通过结构 sanity check
- ✅ `go build ./...` + `go vet ./...` + `gofmt -l .` 全 clean
- ✅ `make image-test` (Phase 1 引入) 跑通，dynamic 模式 image build 大小 ≤ 150 MB
- ✅ `docs/phase5-dynamic-scoreboard.md` 含：dynamic vs static 双 baseline 对比 + drop-in 验证报告
  + Phase 5 后剩余 gap 清单（Postgres wire / 0.11.1 升级 / Jinjava）

### 测试方式

- **单元**：`lineage_test.go` 含 5-6 个 synthetic MDL fixture（覆盖 calc field / relationship /
  metric / cumulative / nested）
- **集成**：`difftest_test.go` + `dynamic_diff_test.go` 跑全 corpus 双模式
- **端到端**：`wrenai-dropin-test.sh` 跑 WrenAI compose；产出 `docs/wrenai-smoke.log` 留底
- **回归保护**：每个切片 commit 后双 baseline 跑一遍；任何 regression 立即 revert

## 9. 与其他阶段的关系

- **依赖 P0–P6**：dynamic 分支重用 P3a/P3b/P3c 的 analyzer / renderer / view 展开 / metric rollup 全部基础
- **依赖 Phase 2**：`ConfigManager.EnableDynamicFields()` 读 `etc/config.properties` 设的值；
  Phase 5 后 dynamic 模式才真正可被 WrenAI bootstrap 触发
- **依赖 Phase 4**：Phase 4 修 formatter `{` bug 后，dynamic 分支重用同一 formatter；切片 3 时
  RC-A 应该已经修好，不应再现
- **复用 Phase 3 工具链**：Phase 3 的 oracle-up / capture-golden 4 工具基础 + 加 `--dynamic`
  flag；不重写
- **收官 drop-in**：Phase 5 完成 = drop-in 5 阶段路线图全部完成；WrenAI 0.9.0 docker-compose
  替换 `wren-engine.image` → Go 镜像 → 全 chain 起来通过端到端 smoke

### Drop-in gap 登记表（Phase 5 后状态）

| Gap | Phase 4 后 | Phase 5 后 |
|---|---|---|
| `WrenSqlRewrite` dynamic-field 分支 | ❌ 全缺 | ✅ **闭合** |
| `WrenDataLineage` | ❌ 全缺 | ✅ **闭合**（lineage 包） |
| WrenAI compose drop-in 替换 | ❌ 即起即崩 | ✅ **闭合**（端到端 smoke） |
| `baseline-dynamic.json` | n/a | ✅ 新增量化基线 |
| Postgres wire protocol (7432) | ❌ 仍缺 | ❌ 仍缺（Phase 6 评估） |
| 0.11.1 升级 | ❌ 未审计 | ❌ 未审计（Phase 8 评估） |
| jinja 全 Jinjava parity | ⚠ 正则替换 | ⚠ 仍正则替换（Phase 6 评估） |
| TPC-H q1 / q4 Java bug | ⚠ oracle-error-permanent | ⚠ 不变（Java 自身 bug） |

### 后续 Phase 候选（drop-in 5 阶段路线图之后）

| Phase | 内容 | 触发条件 |
|---|---|---|
| Phase 6 | Postgres wire protocol（端口 7432）+ Jinjava 全引擎 | 端到端 smoke 发现 ai-service 调用 7432 / 复杂 macro MDL |
| Phase 7 | 性能优化 + memory 调优 | drop-in 后 perf 不达标 |
| Phase 8 | 0.9.3 → 0.11.1 升级 | WrenAI 升级到引用 0.11.1+ 的 image，HTTP API 兼容性审计 |

---

- 本 spec 完成后进入 `gpowers:writing-plans`，产出 Phase 5 逐任务实施计划。
- Phase 5 是**收官** —— 完成后 Go 引擎对 WrenAI 0.9.0 默认配置 100% drop-in 替代 `wren-engine:0.9.3`。
