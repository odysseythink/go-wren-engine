# P4 设计：DuckDB 连接器与执行

> 子项目：P4（七阶段拆解中的第四阶段）。
> 前置：P0（构建）、P1（差分测试框架）、P2（parser/formatter 字节对齐）、P3a/P3b/P3c（重写引擎）。
> 目标：让 Go 引擎把重写后的 SQL 转换为 DuckDB 方言并对 DuckDB 真正执行、返回数据，与 Java
> `wren-engine:0.9.3` 在方言 SQL 与响应信封上一致。

## 1. 背景

`ghcr.io/canner/wren-engine:0.9.3` 镜像是 DuckDB-only 的 Java `wren-server`。P1–P3 已覆盖
「重写后 SQL 文本」这条链路（`/v1/mdl/dry-plan` + `modelingOnly=true`）。但镜像还要能**执行**
查询返回数据 —— `/v1/mdl/preview`、`/v1/mdl/dry-run`、`/v1/data-source/duckdb/query`。P4 补这条链路。

`MDLResource.preview` 确认数据流：用户 SQL → `WrenPlanner` 重写（P3，DEFAULT 方言）→
`DuckDBSqlConverter` 转 DuckDB 方言 → 对 DuckDB 执行 → 返回行数据。

`DuckDBSqlConverter` 实现：`parseSql` → 应用 `RewriteArray` / `RewriteFunction` 两个 `SqlRewrite`
变换 → `SqlFormatter.formatSql(node, DUCKDB)`。P2 设计已明确把 DuckDB / PG 方言差异留给 P4。

**奇偶校验线（P4）**：① `DuckDBSqlConverter` 输出的 DuckDB 方言 SQL 与 Java 逐字节一致；
② 响应信封 JSON 结构化等价。数据值由 DuckDB 保证 —— 同一 SQL + 同一 DuckDB 版本，结果必然相同，
不逐格比对。

## 2. 范围

### IN（P4 负责）

- `DuckDBSqlConverter`：
  - DuckDB 方言 formatter —— 在 P2 的 formatter 上新增 / 完善 `DialectDuckDB` 分支
  - `RewriteArray`（`ARRAY[…][i]` → `array_value(…)[i]`）+ `RewriteFunction` 两个 `SqlRewrite` 变换
- DuckDB 连接器：`DuckDBMetadata`、`DuckdbRecordIterator` —— 基于 **`go-duckdb`（CGo 绑定真 libduckdb）**
- `PreviewService`：`preview` / `dryRun` / `dryPlan(modelingOnly=false)`
- HTTP 端点接线：`/v1/mdl/preview`、`/v1/mdl/dry-run`、`/v1/data-source/duckdb/query`、
  `/v1/data-source/duckdb/settings/init-sql`、`/v1/data-source/duckdb/settings/session-sql`、
  `/v2/mdl/dry-plan`（`DuckDBResource`、`MDLResourceV2`）
- 响应信封 DTO：列元数据（名称、类型）+ 行数据的 JSON 结构
- DuckDB 列类型 → JSON 类型名的映射

### OUT（不在 P4）

- 分析端点 decisionpoint（P5）
- 校验 / 配置（P6）
- `WrenSqlRewrite` 动态字段路径

## 3. 架构与数据流

```
GET /v1/mdl/preview  (sql + manifest + limit)
  └─> PreviewService.preview
        rewritten  = WrenPlanner.Rewrite(sql, ctx, analyzedMDL)         ← P3，DEFAULT 方言
        duckdbSql  = DuckDBSqlConverter.convert(rewritten, ctx)         ← P4
                       parseSql → RewriteArray → RewriteFunction → formatSql(node, DUCKDB)
        rows       = DuckDB.execute(duckdbSql, limit)                   ← P4，go-duckdb
        return envelope(columns, rows)                                  ← JSON 信封
```

- `dry-run`：同 preview，但 limit=0 / 只取 schema，返回列元数据不返回行。
- `/v1/data-source/duckdb/query`：直接对 DuckDB 执行给定 SQL（不经重写）。

DuckDB 方言转换是 P4 的字节关键层 —— 机械移植；连接器与服务层是基础设施 —— 惯用 Go 重写。

## 4. 组件与文件结构

| Go 文件 | 对应 Java | 操作 |
|---|---|---|
| `internal/parser/formatter/formatter.go` 等 | `SqlFormatter` 的 `DUCKDB` 分支 | 修改（在 P2 基础上） |
| `internal/converter/duckdb.go` | `DuckDBSqlConverter` | 重写 |
| `internal/converter/rewrite_array.go` | `RewriteArray` | 新建 |
| `internal/converter/rewrite_function.go` | `RewriteFunction` | 新建 |
| `internal/connector/duckdb/connector.go` | `DuckDBMetadata` | 重写 |
| `internal/connector/duckdb/record_iterator.go` | `DuckdbRecordIterator` | 新建 |
| `internal/service/preview.go` | `PreviewService` | 重写 |
| `internal/server/mdl_handler.go` | `MDLResource` / `MDLResourceV2` 的 preview/dry-run | 修改 |
| `internal/server/duckdb_handler.go` | `DuckDBResource` | 修改 |
| `internal/dto/preview_response.go` | 响应信封 DTO | 新建 |
| `go.mod` | 新增 `github.com/marcboeker/go-duckdb` 依赖 | 修改 |

## 5. 实施切片（方案 C：增量切片）

每个切片：移植 / 实现 → 差分 / 单测 → baseline 接受 fail→pass。

- **切片 0 — go-duckdb 接入**：引入 `go-duckdb` 依赖，连接器骨架，一个 smoke 查询
  （`SELECT 1`）端到端跑通；确认 CGo 构建与 Docker 镜像可行。
- **切片 1 — DuckDB 方言 formatter**：在 P2 formatter 上新增 / 完善 `DialectDuckDB` 分支。
  golden：转换后 SQL 字节一致。
- **切片 2 — SqlRewrite 变换**：`RewriteArray` + `RewriteFunction`。
- **切片 3 — 连接器执行**：`DuckDBMetadata` + `DuckdbRecordIterator`（执行、结果集迭代、
  DuckDB 类型 → JSON 类型映射）。
- **切片 4 — PreviewService**：`preview` / `dryRun` / `dryPlan(modelingOnly=false)` + 响应信封 DTO。
- **切片 5 — HTTP 端点接线**：`DuckDBResource`、`MDLResourceV2`、MDL preview/dry-run 端点。

## 6. 字节分歧风险登记表

| # | 风险 | 说明 |
|---|---|---|
| 1 | CGo 构建复杂度 | `go-duckdb` 是 CGo，交叉编译、Docker 镜像须打包 libduckdb；切片 0 验证。 |
| 2 | DuckDB 版本一致 | Go 用的 DuckDB 版本须与 Java 镜像一致，否则方言 / 函数 / 类型行为漂移。 |
| 3 | DUCKDB 方言 vs DEFAULT | `visitDoubleLiteral` DUCKDB 用 `String.valueOf`、DEFAULT 用 `DecimalFormat`；其余 DUCKDB 分支差异须逐一核对 Java `SqlFormatter`。 |
| 4 | `RewriteArray` / `RewriteFunction` | 变换规则须逐字节复刻（输出经 formatSql 渲染）。 |
| 5 | 列类型 → JSON 类型名 | DuckDB 返回的列类型映射到响应信封的类型名，须对齐 Java。 |
| 6 | 信封 JSON 结构 | 列元数据 / 行数据的字段名、嵌套、null 编码须与 Java 一致（结构化等价）。 |

## 7. 错误处理

- DuckDB 执行错误：捕获并按 Java `WrenExceptionMapper` 的格式返回错误 JSON。
- 转换失败 / 不支持的方言节点：返回明确 error。
- 不静默吞错。

## 8. 测试与验收

- **转换层差分测试**：golden 快照 —— 捕获 Java `DuckDBSqlConverter` 输出（或 `dry-plan`
  `modelingOnly=false`），Go 转换输出与之逐字节一致。
- **信封差分测试**：捕获 Java `preview` / `dry-run` 响应 JSON，结构化等价比对。
- **执行烟雾测试**：对真实 DuckDB（TPC-H parquet 样例数据）跑 preview，确认数据 round-trip。
- **baseline 计分板**：沿用 P1。
- **验收标准**：
  - DuckDB 方言转换对 TPC-H 语料逐字节命中。
  - `preview` / `dry-run` 响应信封结构化等价。
  - `go build ./...`（含 CGo）通过、`go vet ./...` 干净、`gofmt -l` 无输出。

## 9. 与其他阶段的关系

- **依赖 P1 / P2 / P3a / P3b / P3c**：preview 链路 = 重写（P3）→ 方言转换（基于 P2 formatter）→ 执行。
- **被 P6 依赖**：P6 的 `ColumnIsValid` 校验规则需对 DuckDB 执行。
- 本 spec 完成后进入 writing-plans，产出逐任务实施计划。
