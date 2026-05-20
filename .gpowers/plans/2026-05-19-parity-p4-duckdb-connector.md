# P4 DuckDB 连接器与执行 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use gpowers:subagent-driven-development (recommended) or gpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Go `go-wren-engine` 从「重写后的 SQL 文本」延伸到「能对 DuckDB 真正执行返回数据」，让镜像替换链路上 `/v1/mdl/preview`、`/v1/mdl/dry-run`、`/v1/data-source/duckdb/query`、`/v1/mdl/dry-plan`（`modelingOnly=false`）与 `/v2/mdl/dry-plan` 四类响应在「DuckDB 方言 SQL（字节）」与「响应信封 JSON（结构化）」两条线上与 `ghcr.io/canner/wren-engine:0.9.3` 一致。

**Architecture:** P4 接在 P3a/P3b/P3c 已完成的「DEFAULT 方言重写」管线之后，分两层：① **字节关键层（方言转换）** —— 在 P2 formatter 上补全 `DialectDuckDB` 分支、补 `ArrayConstructor` 这个 P2 没建的 AST 节点、新增 `RewriteArray` + `RewriteFunction` 两条 SqlRewrite，机械移植自 Java；② **基础设施层（连接器 + 服务 + HTTP）** —— `database/sql` + `go-duckdb`（CGo 绑定真 libduckdb）的连接器、`PreviewService` 把重写 → 转换 → 执行串起来、HTTP 端点对齐 Java 的 `MDLResource` / `MDLResourceV2` / `DuckDBResource`。由外向内分 6 个切片（切片 0 go-duckdb 接入 → 切片 1 DuckDB 方言 formatter → 切片 2 SqlRewrite 变换 → 切片 3 连接器执行 → 切片 4 PreviewService → 切片 5 HTTP 端点接线），每切片以 P1 差分测试 + baseline 计分板棘轮推进。

**Tech Stack:** Go 1.26.3；CGo（macOS + linux 平台）；`github.com/marcboeker/go-duckdb`（**版本须包装 libduckdb 1.0.0**，对齐 Java `duckdb_jdbc 1.0.0`，详见前置 §4）；`github.com/go-chi/chi/v5`（既有）；P2 的 `internal/parser/{ast,formatter}`；P3a/P3b/P3c 完成后的 `internal/rewrite`；既有 `internal/{config,connector,converter,service,server,dto,mdl,difftest}` 框架。

---

## 前置条件（必读）

1. **P3a / P3b / P3c 必须已合并。** 本计划假定 `WrenPlanner.AllRules` 四条规则全为真实实现、`internal/difftest` 的 22+P3a+P3b+P3c 计分板对齐 baseline。凡引用 P3 产物（`rewrite.Rewrite`、`mdl.NewAnalyzedMDL`、`analyzer.SessionContext`）均按 P3c 收官时的形态使用。

2. **本计划修改既有桩文件**：
   - `internal/converter/duckdb.go`（P2 时刻的 minimal 实现 —— 只调 `parser.ParseSQL + formatter.FormatSQLDialect(.., DialectDuckDB)`，不应用任何 SqlRewrite；本计划在切片 1/2 内补全）。
   - `internal/connector/duckdb/connector.go`（全栈 stub，本计划在切片 0/3 替换）。
   - `internal/service/preview.go`（重写已通、转换已挂、**执行未实现**，本计划切片 4 补全）。
   - `internal/server/{mdl_handler,duckdb_handler}.go`（端点 wired 但 `/v2/mdl/dry-plan` 是 TODO、`modelingOnly=false` 分支未走 sqlConverter）。
   - `internal/parser/formatter/{formatter,expression_formatter}.go`（P2 完成 DEFAULT 方言；DUCKDB 仅 `formatIdentifier` 等少数巧合一致的路径，其余分支未实现）。
   - `internal/parser/ast/expression.go`（**缺 `ArrayConstructor`** —— P2 corpus 没遇到，P4 切片 1 增补；`DecimalLiteral`/`TimeLiteral`/`TimestampLiteral` 不在 P4 关键路径，本计划不引入）。
   - 既有 `internal/service/preview.go` 的 `QueryResultDto` 类型本计划切片 4 迁到 `internal/dto/preview_response.go`（spec §4 要求），同时把 `internal/server/duckdb_handler.go` 里旧的 `service.QueryResultDto` 引用一并改名。

3. **Java 参照源**位于：
   - `../wren-engine-0.9.3/wren-main/src/main/java/io/wren/main/connector/duckdb/`：`DuckDBSqlConverter`、`DuckDBMetadata`、`DuckdbRecordIterator`。
   - `../wren-engine-0.9.3/wren-main/src/main/java/io/wren/main/sql/duckdb/`：`RewriteArray`、`RewriteFunction`。
   - `../wren-engine-0.9.3/wren-main/src/main/java/io/wren/main/PreviewService.java`。
   - `../wren-engine-0.9.3/wren-main/src/main/java/io/wren/main/web/`：`MDLResource`、`MDLResourceV2`、`DuckDBResource`。
   - `../wren-engine-0.9.3/trino-parser/src/main/java/io/trino/sql/`：`SqlFormatter`、`ExpressionFormatter`（DUCKDB 分支位置见任务 4 的精确行号清单）。
   - `../wren-engine-0.9.3/trino-parser/src/main/java/io/trino/sql/tree/ArrayConstructor.java`。

4. **DuckDB 版本对齐（关键，spec 风险 #2）**：Java 用 `org.duckdb:duckdb_jdbc:1.0.0`（见 `../wren-engine-0.9.3/pom.xml`）。Go 侧需选一个**包装 libduckdb 1.0.0** 的 `marcboeker/go-duckdb` 发布版本 —— 即 `github.com/marcboeker/go-duckdb v1.7.0` 系列（其 v1.7.x release notes 写明绑定 libduckdb-1.0.0）。`go.mod` 切片 0 引入时严格 pin 至 `v1.7.0`，**勿用 ≥v1.8 的 release**（它们已升 libduckdb 至 1.1+，函数 / 类型行为有漂移）。任务 1 Step 4 在 `README.md` 写明此约束。

5. **既有 `internal/analyzer`**（P3a 用 `analyzer.SessionContext`）在本计划凡引入处一律以别名 `base` 导入。`internal/rewrite/analyzer` 不在本计划路径上。

---

## 验收性质说明（重要 —— 影响切片 1/2/4 验证方式与 P4 收官口径）

P4 引入两类**新差分线**（DUCKDB SQL bytes + envelope JSON），同时继承 P3 全部既有 baseline 状态。三件事须诚实预先声明：

### 1. P3 既有 Jinja 已知失败原样保留

P3a 的 `tpch/m_orders`、P3b 的 5 条 `tpch/met_*`、P3c 的 4 条 `tpch/v_use_*` 在 P4 仍然是 `go-error` / `fail`（传递性 Jinja，待 P6）。P4 **不需要也无法**修复它们 —— 方言转换层若收到合法重写后 SQL 会照常工作；但前置重写阶段已失败的用例，到不了方言转换层。

### 2. `tpch/1` / `tpch/4` 的 `oracle-error` 在 DUCKDB 差分线上同样适用

Java oracle dry-plan 已对它们报错（无论 modelingOnly true/false 都拿不到 golden），P4 新增的 DUCKDB 差分线沿用 P1 既有 `.error` 哨兵机制（`golden-duckdb/<group>/<name>.sql.error`），状态映射到 `oracle-error`。

### 3. 信封（envelope）差分语料范围有限

完整 `cases/tpch/` envelope 差分需要 TPC-H parquet 真实数据双方都加载，超出 P4 范围（设计层未要求逐用例对比）。本计划信封差分**只覆盖**：

- `cases/viewenum/`（P3c 的 5 条合成视图 / 枚举语料 —— 无 Jinja，MDL 自包含；本计划任务 11 给该语料挂一组 `init.sql` 在 Java + Go 侧创建相同的合成 `orders` 表与最小行集）。
- 一组**纯 SQL 烟雾**用例（`cases/exec_smoke/` —— **新建**：`SELECT 1`、`SELECT * FROM (VALUES (1, 'a'), (2, 'b'), (NULL, NULL)) v(a, b)` 这类不依赖任何模型 / MDL 的小用例，捕获 Java preview JSON 后比对结构化等价）。

`cases/tpch/v_enum.sql`（P3c 纯枚举）经重写为 `SELECT 'O'` 后能跑通信封差分，故附加进 envelope 语料。

**P4 收官真实达成标准**：

| 类别 | 期望 |
|---|---|
| DUCKDB SQL 字节差分（`golden-duckdb/`） | `tpch/2..3,5..22`（20 条）+ `metric/*` 4 条 + `viewenum/*` 5 条 + `tpch/v_enum` 1 条全 `pass`；其余既有 P3 已知失败 / oracle-error 状态不变 |
| 信封 JSON 结构化差分（`golden-envelope/`） | `exec_smoke/*`（≥3 条 INT/STRING/NULL）+ `tpch/v_enum`（纯字符串）+ `viewenum/{view_on_model,view_on_metric,view_nested,enum}`（4 条 INT/DOUBLE）全 `pass`；`viewenum/view_rollup`（含 `orderdate` DATE 列）可允许 `fail`，因 DATE/TIMESTAMP 序列化在 Jackson（无 TZ 的 ISO-8601 字符串）与 Go `encoding/json`（time.Time 默认 RFC3339 带 Z）有形态差。详见风险 #16。 |
| 执行烟雾 | `viewenum/*` 5 条 + `exec_smoke/*` 全部对真实 in-memory DuckDB 跑通并 round-trip 数据 |
| 既有 P3 dry-plan `default` 差分 | 不变（P3 收官状态） |
| 构建 / 静态 | `CGO_ENABLED=1 go build ./...` 通过；`go vet ./...` 干净；`gofmt -l` 无输出 |

---

## 字节分歧风险登记表（贯穿全程）

继承 P3a/P3b/P3c 全部已登记风险。P4 新增：

| # | 风险 | 缓解（落在哪个任务） |
|---|---|---|
| 1 | CGo 构建复杂度 —— `go-duckdb` 是 CGo，跨平台 `go build` 需 libduckdb；marcboeker/go-duckdb 在 darwin/amd64+arm64、linux/amd64+arm64 自带预编译二进制，纯 `go build` 直跑；其他平台需 `CGO_ENABLED=0` build tag 兜底。 | 任务 1：`README.md` 写明支持矩阵；`smoke_test.go` 在不支持平台自动 skip。 |
| 2 | DuckDB 版本一致 —— libduckdb 1.0.0 ↔ go-duckdb v1.7.0；v1.8+ 已升 libduckdb 至 1.1+，函数 / 类型行为漂移。 | 任务 1：`go.mod` `require github.com/marcboeker/go-duckdb v1.7.0`（不带 `^`/`~`，pin 精确）。 |
| 3 | DUCKDB 方言 `visitDoubleLiteral` 用 `String.valueOf` —— Java `Double.toString(1.5)` 为 `"1.5"`、`Double.toString(1e10)` 为 `"1.0E10"`；Go `strconv.FormatFloat(v, 'g', -1, 64)` 对 `1e10` 给 `"1e+10"`，不一致。须 port Java `Double.toString` 算法：阈值 \[1e-3, 1e7) 用普通小数、其外用科学计数且强制 `<mantissa>.<frac>E<exp>` 形态（mantissa 强制带 `.0`、exp 无 `+`/前导零）。 | 任务 4：`formatDoubleDuckDB(v)` 单独函数 + 单测覆盖 [-1e-3, 1e7] 边界、负数、+/-0、Inf/NaN（DuckDB 这些为非法字面量，但 formatter 不阻止 —— 同 Java）。 |
| 4 | DUCKDB 方言 `visitDataType` 把 `ARRAY<T>` 渲为 `T[]` —— 与 DEFAULT 不同；不影响 P2 corpus（不含 ARRAY 类型字面），但 `Cast(.. AS ARRAY<INTEGER>)` 这类表达式经 DUCKDB 后须为 `INTEGER[]`。 | 任务 4：`formatTypeDuckDB(t)` 单分支识别 `ARRAY` 名 + 单 type 参数；其余类型与 DEFAULT 同字节。 |
| 5 | DUCKDB 方言 `visitIntervalLiteral` 渲为 `INTERVAL 'sign value' field`（单引号内含 sign，无 `TO` 子字段）—— 与 DEFAULT `INTERVAL [sign] 'value' FROM [TO]` 完全不同；P2 字符串模板有 `formatInterval`。 | 任务 4：`formatInterval` 增 dialect 分支；DUCKDB 路径不消费 `n.To`，sign 放入字符串字面量内部。 |
| 6 | DUCKDB 方言 `LIKE ... ESCAPE` —— DEFAULT/DUCKDB 同行为（已对齐），POSTGRES/BIGQUERY 略 ESCAPE 子句。Go 当前永远 emit ESCAPE：对 DUCKDB **巧合一致**，不需改但需在风险表登记防回归。 | 任务 4：保留 `formatExpression` 现有 ESCAPE 逻辑不动，加注释明确「DUCKDB 与 DEFAULT 同字节」。 |
| 7 | DUCKDB 方言 `GENERATE_TIMESTAMP_ARRAY` 函数被特殊改写为 `range(start, end, step) UNION range(end, end + interval, step)` 形态（见 Java `processGenerateTimestampArrayInDuckDB`）。**TPC-H / `metric/` / `viewenum/` 语料均不出现该函数**，故 P4 不需要落地这条特殊路径 —— 但 formatter 切到 DUCKDB 时若意外遇到，需明确 panic 而不是静默 emit DEFAULT 形态。 | 任务 4：`formatFunctionCall` DUCKDB 分支对 `GENERATE_TIMESTAMP_ARRAY` 走 `panic("GENERATE_TIMESTAMP_ARRAY DuckDB special case not yet ported; not in P4 corpus")`。 |
| 8 | `RewriteArray` 仅改写 `ArrayConstructor` 作为 `SubscriptExpression.Base` 的情况；其他 `ArrayConstructor`（如 `VALUES (ARRAY[1,2,3])`）保留 `ARRAY[..]` 字面。 | 任务 5：`RewriteArrayRewriter.visitSubscriptExpression` 仅在 base 为 `*ast.ArrayConstructor` 时改写；不重写顶层 `visitArrayConstructor`。 |
| 9 | `RewriteFunction` 把函数名 **lowercase** 后再做 PG→DuckDB 映射 —— 即使无映射也会改名（`COUNT` → `count`）。这是 byte 关键路径，不可省。Java `formatName` 走 `formatExpression(Identifier)`：非 delimited identifier 原样输出 —— 故 `count` 比 `COUNT` 短 5 字节，必差。 | 任务 6：`RewriteFunctionRewriter.visitFunctionCall` 无条件改 `n.Name` 为 lowercase；映射表查询用小写 suffix。 |
| 10 | `PG_TO_DUCKDB_FUNCTION_NAME_MAPPINGS` 仅 1 条 `generate_array → generate_series`。映射用 `QualifiedName.suffix`（最后一段名）查表 —— 多段名也只看最后段。 | 任务 6：map 写 const；用 `n.Name.Parts[len-1].Value` 查（**lowercase 后**）。 |
| 11 | 信封 `Column.type` Java 强制 `toUpperCase(Locale.ROOT)`（见 `wren-base/src/main/java/io/wren/base/Column.java`）。Go `connector.Column.Type` 接 DuckDB JDBC 返回时已是大写，但仍须显式 `strings.ToUpper(.., en-US)` 防 DuckDB driver 行为漂移。 | 任务 7：`DuckdbRecordIterator` 构造时 `strings.ToUpper(typeName)`；任务 11 信封 DTO 标 `omitempty: false`。 |
| 12 | 信封 `data` 是 `List<Object[]>`（每行一个数组），JSON 序列化为 `[[v1, v2], ...]`；Go 现 `QueryResultDto.Data [][]any` 已对齐。**但数值 / 日期 / array / blob 的 JSON 编码须与 Jackson 等价**：`java.time.LocalDateTime` → ISO-8601 字符串（Jackson 默认 `2006-01-02T15:04:05`，无时区）；`byte[]` → base64；DuckDB ARRAY → JSON array；`null` → `null`。 | 任务 7：`convertValue` 映射 DuckDB 列类型 → Go 类型时对齐 Jackson 默认；任务 10 envelope 差分用「结构化等价」对比器（数值类型容忍 `int64` ↔ `float64` 字面、字符串严格、null 严格）。 |
| 13 | `WrenSqlRewrite` 与 `DuckDBSqlConverter` 之间不再 re-parse —— Java `DuckDBSqlConverter.convert(sql)` 自身 `parseSql(sql)` 重新建树，故 `WrenPlanner.rewrite` 出来的 SQL 必先经 `formatSql` 渲染过、再被 converter 解析。Go 必须**保持同一往返序**：planner 返 `string`，converter 接 `string` 后内部 `ParseSQL → rewrite → format(..., DuckDB)`。 | 任务 5/6：`DuckDBSqlConverter.Convert(sql, ctx)` 内部一次 `ParseSQL`、串行应用两条 SqlRewrite、最后 `FormatSQLDialect(stmt, DialectDuckDB)`，与 Java 行为字节等价。 |
| 14 | `BaseRewriter` 复用 —— P3a 的 `internal/rewrite/base_tree_rewriter.go` 是 statement 级，访问 `Expression` 子树的钩子在 `RewriteNode`。`RewriteArray` / `RewriteFunction` 需要在**任意位置**改写 `*ast.SubscriptExpression` / `*ast.FunctionCall`，且需递归（FunctionCall 嵌 FunctionCall）。直接复用 `RewriteNode + 钩子` 模式（同 P3c `EnumRewrite`）。 | 任务 5/6：用 `rewrite.RewriteNode(stmt, hook)` 模式，hook 返 `(repl, true)` 即替换。**`internal/converter` 包须导出 `RewriteNode`** —— 若 P3a 是私有，则在切片 2 之前把它从小写改大写（或直接 import `internal/rewrite`，因 P3a 已让该函数在包内导出）。 |
| 15 | `MetricRollupRewrite` / `WrenSqlRewrite` 与 `DuckDBSqlConverter` 解耦 —— P3 的 4 条规则只对 DEFAULT 方言重写；DUCKDB 方言转换只跑两条 converter 内规则。`PreviewService.Preview` 内顺序：`rewrite.Rewrite(sql) → converter.Convert(planned) → metadata.DirectQuery(converted)`，**勿把 converter 当第 5 条规则混入 `WrenPlanner.AllRules`**。 | 任务 9：保持 `PreviewService.Preview` 三步分离；不动 `WrenPlanner.AllRules`。 |
| 16 | DATE / TIMESTAMP / DECIMAL 信封序列化差异 —— Java `java.sql.Date` / `java.time.LocalDateTime` 经 Jackson 默认序列化为 `"2024-01-15"` / `"2024-01-15T00:00:00"`（**无时区后缀**）；Go `time.Time` 经 `encoding/json` 默认序列化为 RFC3339 带 `Z`（如 `"2024-01-15T00:00:00Z"`）。结构化等价比较器若用 `reflect.DeepEqual` 会判定不一致。 | 任务 10：envelope 比较器对字符串字段允许「Java 形态前缀匹配」（即 Java `"2024-01-15"` 等价于 Go `"2024-01-15T00:00:00Z"`）——或更简单：把这类用例从 envelope 语料中剥离（plan 当前选项：`view_rollup` 允许 `fail`，写入 envelope baseline 当 `fail`）；任务 7 `convertValue` 可后续追加 `time.Time → Java 形态字符串`手工编码以收敛该用例（P4 不强制）。 |

---

# 切片 0 — go-duckdb 接入与连接器骨架

目标：引入 `go-duckdb` 依赖、替换连接器 stub 为可执行 `SELECT 1` 的最小连接器、确认 CGo 构建可行。

## 任务 1：引入 go-duckdb + 替换 connector stub + 烟雾测试

**Files:**
- Edit: `go.mod`
- Create: `go.sum`（go mod tidy 自动生成 / 更新）
- Edit: `internal/connector/duckdb/connector.go`
- Create: `internal/connector/duckdb/smoke_test.go`
- Edit: `README.md`

- [ ] **Step 1: 引 go-duckdb 依赖**

```bash
go get github.com/marcboeker/go-duckdb@v1.7.0
```

确认 `go.mod` 新增：

```
require github.com/marcboeker/go-duckdb v1.7.0
```

不允许带 `^` / `~` —— go.mod 自身就是精确 pin，但 `go get` 默认拉取 latest minor；用 `@v1.7.0` 锁住。

- [ ] **Step 2: 全栈替换 `internal/connector/duckdb/connector.go`**

```go
package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/marcboeker/go-duckdb"

	"github.com/wren-engine/wren/internal/connector"
)

// Connector wraps a database/sql DuckDB connection pool. Mirrors Java
// DuckdbClient: in-memory by default; init/session SQL set out-of-band by
// DuckDBMetadata (which is the public façade for setup).
type Connector struct {
	db *sql.DB
}

// NewConnector opens an in-memory DuckDB pool. The path "" yields an
// anonymous in-memory database, the same default Java uses
// (DriverManager.getConnection("jdbc:duckdb:")).
func NewConnector() *Connector {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		// sql.Open on duckdb only fails on driver-registration issues, which
		// indicate a build problem — fail fast with a clear message rather
		// than producing a half-built Connector.
		panic(fmt.Sprintf("duckdb open: %v (CGo build?)", err))
	}
	return &Connector{db: db}
}

// DB returns the underlying *sql.DB so DuckDBMetadata can execute init/session SQL.
func (c *Connector) DB() *sql.DB { return c.db }

func (c *Connector) Close() error {
	if c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Query runs sql with no parameters.
func (c *Connector) Query(ctx context.Context, sqlText string) (connector.RecordIterator, error) {
	return c.QueryWithParams(ctx, sqlText, nil)
}

func (c *Connector) QueryWithParams(ctx context.Context, sqlText string, params []connector.Parameter) (connector.RecordIterator, error) {
	args := make([]any, len(params))
	for i, p := range params {
		args[i] = p.Value
	}
	rows, err := c.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("duckdb query: %w", err)
	}
	return newRecordIterator(rows)
}

func (c *Connector) Describe(ctx context.Context, sqlText string) ([]connector.Column, error) {
	// PREPARE then describe via column metadata — runs no rows. Mirrors Java
	// DuckdbClient.describe (PreparedStatement + getMetaData()). DuckDB has
	// no PREPARE-only API in Go driver yet; cheapest portable trick is
	// LIMIT 0 wrap, which DuckDB optimizes to a no-op scan.
	wrapped := "SELECT * FROM (" + strings.TrimSpace(sqlText) + ") __wren_describe LIMIT 0"
	rows, err := c.db.QueryContext(ctx, wrapped)
	if err != nil {
		return nil, fmt.Errorf("duckdb describe: %w", err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("duckdb column types: %w", err)
	}
	cols := make([]connector.Column, len(types))
	for i, t := range types {
		cols[i] = connector.Column{Name: t.Name(), Type: strings.ToUpper(t.DatabaseTypeName())}
	}
	return cols, nil
}

func (c *Connector) ExecuteDDL(ctx context.Context, sqlText string) error {
	_, err := c.db.ExecContext(ctx, sqlText)
	if err != nil {
		return fmt.Errorf("duckdb ddl: %w", err)
	}
	return nil
}

func (c *Connector) DirectQuery(ctx context.Context, sqlText string, params []connector.Parameter) (connector.RecordIterator, error) {
	return c.QueryWithParams(ctx, sqlText, params)
}

func (c *Connector) DescribeQuery(ctx context.Context, sqlText string, params []connector.Parameter) ([]connector.Column, error) {
	// Parameters can change column count (rare in DuckDB but possible for $1::TEXT).
	// Java DuckdbClient.describe takes parameters; we mirror by ignoring them
	// here — DuckDB infers parameter types from context; LIMIT 0 wrap retains schema.
	_ = params
	return c.Describe(ctx, sqlText)
}

func (c *Connector) DirectDDL(ctx context.Context, sqlText string) error {
	return c.ExecuteDDL(ctx, sqlText)
}
```

Note: `newRecordIterator` is added in 任务 7 —— for slice 0 we stub it.

- [ ] **Step 3: stub `newRecordIterator` so connector.go compiles**

放在同包内的小文件 `internal/connector/duckdb/record_iterator.go`（任务 7 全栈替换，此处只为 slice 0 build）：

```go
package duckdb

import (
	"database/sql"
	"fmt"

	"github.com/wren-engine/wren/internal/connector"
)

// newRecordIterator: full implementation lands in P4 slice 3 (task 7).
// Slice 0 stub keeps the package compiling for smoke testing.
func newRecordIterator(rows *sql.Rows) (connector.RecordIterator, error) {
	rows.Close()
	return nil, fmt.Errorf("duckdb record iterator: pending slice 3")
}
```

- [ ] **Step 4: 写 `internal/connector/duckdb/smoke_test.go`**

烟雾测试覆盖 ① 驱动注册 ② `db.QueryContext("SELECT 1")` 返回 1 行 1 列。**绕开 `newRecordIterator`** —— 直接走 `sql.DB` API。

```go
package duckdb

import (
	"context"
	"testing"
)

// TestConnectorSmoke confirms CGo build works and DuckDB driver is registered.
// Bypasses RecordIterator (added in slice 3) by going straight to sql.DB.
func TestConnectorSmoke(t *testing.T) {
	c := NewConnector()
	t.Cleanup(func() { _ = c.Close() })

	rows, err := c.DB().QueryContext(context.Background(), "SELECT 1, 'hello'")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("expected one row")
	}
	var n int
	var s string
	if err := rows.Scan(&n, &s); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 1 || s != "hello" {
		t.Errorf("got (%d, %q), want (1, %q)", n, s, "hello")
	}
}

func TestConnectorDescribe(t *testing.T) {
	c := NewConnector()
	t.Cleanup(func() { _ = c.Close() })

	cols, err := c.Describe(context.Background(), "SELECT 1 AS a, 'x' AS b")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(cols) != 2 || cols[0].Name != "a" || cols[1].Name != "b" {
		t.Errorf("unexpected columns: %+v", cols)
	}
	// DuckDB returns INTEGER for unqualified `1`, VARCHAR for string literal.
	if cols[0].Type != "INTEGER" || cols[1].Type != "VARCHAR" {
		t.Errorf("unexpected types: %+v", cols)
	}
}
```

- [ ] **Step 5: 跑 smoke**

```bash
CGO_ENABLED=1 go test ./internal/connector/duckdb/... -run TestConnectorSmoke -v
CGO_ENABLED=1 go test ./internal/connector/duckdb/... -run TestConnectorDescribe -v
```

两条都过即 slice 0 完成。若 `CGo_ENABLED=0` 显式跑会失败（预期），写入 README。

- [ ] **Step 6: 更新 `README.md` 加入构建 + DuckDB 版本声明**

在 README 添加一节：

```markdown
## 构建（DuckDB / CGo）

Go 引擎用 `github.com/marcboeker/go-duckdb` 绑定 libduckdb 1.0.0（与 Java
镜像 `ghcr.io/canner/wren-engine:0.9.3` 的 `duckdb_jdbc 1.0.0` 一致）。版本
固定为 `v1.7.0`，**不允许升级至 v1.8+**（已绑定 libduckdb 1.1+，函数 / 类型
行为有漂移）。

### 支持矩阵

`marcboeker/go-duckdb v1.7.0` 自带预编译 libduckdb：

| 平台 | 状态 |
|---|---|
| darwin/amd64 + darwin/arm64 | ✅ 无需额外步骤 |
| linux/amd64 + linux/arm64 | ✅ 无需额外步骤 |
| 其他 | ⚠️ 自行交叉编译 libduckdb |

构建命令：

```bash
CGO_ENABLED=1 go build ./...
```

`CGO_ENABLED=0` 会跳过 DuckDB 连接器编译；P4 端点不可用。
```

- [ ] **Step 7: 跑全量构建确认未破坏其它包**

```bash
CGO_ENABLED=1 go build ./...
CGO_ENABLED=1 go vet ./...
gofmt -l .
```

无输出即 OK。

- [ ] **Step 8: 提交切片 0**

```bash
git add go.mod go.sum internal/connector/duckdb/connector.go internal/connector/duckdb/record_iterator.go internal/connector/duckdb/smoke_test.go README.md
git commit -m "feat(p4): introduce go-duckdb v1.7.0 + connector skeleton (slice 0)"
```

---

# 切片 1 — DuckDB 方言 formatter

目标：把 P2 formatter 的 `DialectDuckDB` 从「巧合一致」补成「精确逐字节一致」。先建 `ArrayConstructor` AST 节点，再补 8 个 DUCKDB 特化分支。

## 任务 2：新增 `ArrayConstructor` AST 节点 + parser 接入

**Files:**
- Edit: `internal/parser/ast/expression.go`
- Edit: `internal/parser/ast/expression_test.go`
- Edit: `internal/parser/parser.go`（或对应 parser 路径）
- Create: `internal/parser/ast/array_constructor_test.go`（同包新文件 OK）

**为什么**：Java `ArrayConstructor extends Expression` 持有 `List<Expression> values`，`ARRAY[1,2,3]` 字面经 parser → `ArrayConstructor(values=[1,2,3])`。P2 未引入该节点（corpus 无），但 `RewriteArray` 必须能识别。

- [ ] **Step 1: 在 `internal/parser/ast/expression.go` 加节点定义**

参考既有 `Row` / `CoalesceExpression` 等结构体：

```go
// ArrayConstructor represents the literal "ARRAY[v1, v2, ...]". The DEFAULT /
// DUCKDB / POSTGRES dialects render it as "ARRAY[v1,v2,...]" (no spaces, comma-
// joined). RewriteArray turns ArrayConstructor-as-subscript-base into
// array_value(...) before DUCKDB rendering. Mirrors trino
// io.trino.sql.tree.ArrayConstructor.
type ArrayConstructor struct {
	BaseNode
	Values []Expression
}

func (a *ArrayConstructor) GetChildren() []Node {
	out := make([]Node, len(a.Values))
	for i, v := range a.Values {
		out[i] = v
	}
	return out
}
func (a *ArrayConstructor) isExpression() {}
```

- [ ] **Step 2: 给 parser 加 `ARRAY[...]` 识别**

定位 parser 入口的 expression 分发处（搜索 `case "ARRAY"` 或对应 ANTLR visitor 的 array 字面方法）。新增分支：

```go
// In parser visitor (e.g., parseArrayExpression / visitArrayConstructor —
// exact location depends on P2 parser layout). The shape is recognizable as
// "ARRAY" "[" expr ("," expr)* "]".
//
// Java reference (Antlr ArrayConstructor rule): trino-parser SqlBase.g4:
//   ARRAY '[' (expression (',' expression)*)? ']' #arrayConstructor
//
// Emit *ast.ArrayConstructor with the parsed value expressions.
```

具体代码取决于 P2 parser 是手写 visitor 还是 ANTLR 自动生成。若 P2 parser 走 ANTLR，则把 `arrayConstructor` 规则的 `Visit*` 改为 emit `*ast.ArrayConstructor`。若手写则在 primary expression 解析处加 `ARRAY` keyword 触发。

- [ ] **Step 3: 写解析单测 `array_constructor_test.go`**

```go
package ast_test

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

func TestArrayConstructor_ParseAndFormatDefault(t *testing.T) {
	stmt, err := parser.ParseSQL("SELECT ARRAY[1, 2, 3]")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := formatter.FormatSQL(stmt)
	want := "SELECT ARRAY[1,2,3]\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}

	// Tree shape spot-check: outer Query → QuerySpecification → Select →
	// SingleColumn → ArrayConstructor{Values: [LongLiteral 1, 2, 3]}.
	q, ok := stmt.(*ast.Query)
	if !ok {
		t.Fatalf("not a Query: %T", stmt)
	}
	qs := q.Body.(*ast.QuerySpecification)
	sc := qs.Select.SelectItems[0].(*ast.SingleColumn)
	ac, ok := sc.Expression.(*ast.ArrayConstructor)
	if !ok {
		t.Fatalf("not ArrayConstructor: %T", sc.Expression)
	}
	if len(ac.Values) != 3 {
		t.Errorf("expected 3 values, got %d", len(ac.Values))
	}
}

func TestArrayConstructor_Subscript(t *testing.T) {
	stmt, err := parser.ParseSQL("SELECT ARRAY[1, 2, 3][1]")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := stmt.(*ast.Query)
	qs := q.Body.(*ast.QuerySpecification)
	sc := qs.Select.SelectItems[0].(*ast.SingleColumn)
	sub, ok := sc.Expression.(*ast.SubscriptExpression)
	if !ok {
		t.Fatalf("not SubscriptExpression: %T", sc.Expression)
	}
	if _, ok := sub.Base.(*ast.ArrayConstructor); !ok {
		t.Fatalf("subscript base not ArrayConstructor: %T", sub.Base)
	}
}
```

- [ ] **Step 4: 让 `expression_formatter.go` 处理 ArrayConstructor**

```go
case *ast.ArrayConstructor:
	// Trino visitArrayConstructor: "ARRAY[" + Joiner.on(",").join(values) + "]".
	// Note: comma WITHOUT space — verified at ../wren-engine-0.9.3/.../
	// ExpressionFormatter.java visitArrayConstructor (~L265).
	parts := make([]string, len(n.Values))
	for i, v := range n.Values {
		parts[i] = e.process(v)
	}
	return "ARRAY[" + strings.Join(parts, ",") + "]"
```

放在 `process` switch 的 `*ast.Row` 旁边。

- [ ] **Step 5: 跑测试**

```bash
go test ./internal/parser/... -run TestArrayConstructor -v
```

- [ ] **Step 6: 提交任务 2**

```bash
git commit -m "feat(p4): add ArrayConstructor AST node + parser/formatter support (slice 1)"
```

## 任务 3：补 P2 formatter 的 DUCKDB 特化分支

**Files:**
- Edit: `internal/parser/formatter/expression_formatter.go`
- Edit: `internal/parser/formatter/formatter.go`
- Create: `internal/parser/formatter/dialect_duckdb_test.go`

机械对照 Java `trino-parser/src/main/java/io/trino/sql/ExpressionFormatter.java` 的 DUCKDB 分支。8 个分支位置（行号见研究阶段确认）：

| Java 行 | 方法 | DUCKDB 行为 | Go 落地 |
|---|---|---|---|
| ~128 | `formatIdentifier` | `"x"` 双引号包裹 | 已对齐（`formatIdentifier`）|
| ~279 | `visitSubscriptExpression` | `base[index]` 直接拼 | 已对齐（`*ast.SubscriptExpression`）|
| ~309 | `visitDoubleLiteral` | `String.valueOf(v)` | **新增** `formatDoubleDuckDB` |
| ~318 | `visitDecimalLiteral` | `n.Value` 原样 | 不在 P4 corpus（无 DecimalLiteral AST），**跳过** |
| ~354 | `visitIntervalLiteral` | `INTERVAL 'sign value' field` | **新增** dialect 分支 |
| ~435 | `processGenerateTimestampArrayInDuckDB` | 特殊 union range | 不在 corpus，**panic** 占位 |
| ~641 | `visitLikePredicate ESCAPE` | 与 DEFAULT 同 | 已对齐 |
| ~819 | `visitDataType` ARRAY | `T[]` | **新增** `formatTypeDuckDB` |

- [ ] **Step 1: 写失败测试 `dialect_duckdb_test.go` —— 表驱动覆盖所有 DUCKDB 特化**

```go
package formatter_test

import (
	"testing"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

// roundtrip parses a SQL then re-renders it under DialectDuckDB. Both Java
// and Go normalize via parse→format, so byte equivalence requires both
// formatter outputs to match.
func roundtrip(t *testing.T, in string) string {
	t.Helper()
	stmt, err := parser.ParseSQL(in)
	if err != nil {
		t.Fatalf("parse: %v\nSQL: %s", err, in)
	}
	return formatter.FormatSQLDialect(stmt, formatter.DialectDuckDB)
}

func TestDuckDB_DoubleLiteral(t *testing.T) {
	cases := []struct{ in, want string }{
		// Java Double.toString boundaries: [1e-3, 1e7) is decimal; outside is scientific.
		// Verified by running Java in ../wren-engine-0.9.3 REPL with String.valueOf.
		{"SELECT 1.5", "SELECT 1.5\n\n"},
		{"SELECT 0.001", "SELECT 0.001\n\n"},          // 1e-3 boundary inclusive → still decimal
		{"SELECT 0.0001", "SELECT 1.0E-4\n\n"},        // < 1e-3 → scientific
		{"SELECT 9999999.9", "SELECT 9999999.9\n\n"},   // < 1e7 → decimal
		{"SELECT 1.0E10", "SELECT 1.0E10\n\n"},        // ≥ 1e7 → scientific
		{"SELECT -2.5E-5", "SELECT -2.5E-5\n\n"},       // negative + scientific
	}
	for _, c := range cases {
		if got := roundtrip(t, c.in); got != c.want {
			t.Errorf("in=%q\n got=%q\nwant=%q", c.in, got, c.want)
		}
	}
}

func TestDuckDB_IntervalLiteral(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SELECT INTERVAL '7' DAY", "SELECT INTERVAL '7' DAY\n\n"},
		{"SELECT INTERVAL -'7' DAY", "SELECT INTERVAL '-7' DAY\n\n"},
		{"SELECT INTERVAL '1' YEAR TO MONTH", "SELECT INTERVAL '1' YEAR\n\n"}, // DuckDB drops TO subfield
	}
	for _, c := range cases {
		if got := roundtrip(t, c.in); got != c.want {
			t.Errorf("in=%q\n got=%q\nwant=%q", c.in, got, c.want)
		}
	}
}

func TestDuckDB_ArrayType(t *testing.T) {
	if got := roundtrip(t, "SELECT CAST(x AS ARRAY(INTEGER)) FROM t"); got != "SELECT CAST(x AS INTEGER[])\nFROM\n  t\n\n" {
		t.Errorf("ARRAY type → DUCKDB suffix: got %q", got)
	}
}

func TestDuckDB_ArrayConstructor_Subscript(t *testing.T) {
	// Pre-RewriteArray: ARRAY[1,2,3][1] formatter still emits ARRAY[..].
	// RewriteArray (任务 5) is what converts to array_value(...). The
	// formatter alone must not.
	if got := roundtrip(t, "SELECT ARRAY[1,2,3][1]"); got != "SELECT ARRAY[1,2,3][1]\n\n" {
		t.Errorf("formatter alone must not rewrite ArrayConstructor: got %q", got)
	}
}

func TestDuckDB_GenerateTimestampArray_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for GENERATE_TIMESTAMP_ARRAY under DUCKDB")
		}
	}()
	_ = roundtrip(t, "SELECT GENERATE_TIMESTAMP_ARRAY(t1, t2, INTERVAL '1' HOUR)")
}
```

- [ ] **Step 2: 实现 `formatDoubleDuckDB`**

在 `expression_formatter.go` 把 `formatDouble` 改为 dialect-dispatch：

```go
func formatDouble(v float64, dialect Dialect) string {
	if dialect == DialectDuckDB {
		return formatDoubleDuckDB(v)
	}
	// existing DEFAULT path unchanged...
}

// formatDoubleDuckDB mirrors Java Double.toString:
//   - finite |v| in [1e-3, 1e7): plain decimal "d.dddd"
//   - else: scientific "d.dddE[-]exp" (mantissa always has at least one frac digit;
//     exponent has no '+' and no leading zeros)
//   - +0.0 → "0.0";  -0.0 → "-0.0"
//   - Infinity / -Infinity / NaN → "Infinity" / "-Infinity" / "NaN"
//     (matches java.lang.Double.toString, even though DuckDB rejects these literals)
func formatDoubleDuckDB(v float64) string {
	if math.IsNaN(v) {
		return "NaN"
	}
	if math.IsInf(v, 1) {
		return "Infinity"
	}
	if math.IsInf(v, -1) {
		return "-Infinity"
	}
	abs := math.Abs(v)
	if abs == 0 {
		if math.Signbit(v) {
			return "-0.0"
		}
		return "0.0"
	}
	if abs >= 1e-3 && abs < 1e7 {
		// Plain decimal. strconv 'f' -1 gives shortest round-trip without
		// scientific notation. Add trailing ".0" if integral.
		s := strconv.FormatFloat(v, 'f', -1, 64)
		if !strings.ContainsRune(s, '.') {
			s += ".0"
		}
		return s
	}
	// Scientific. Go 'E' -1: "1.5E+10". Strip '+', strip leading zeros in exp,
	// ensure mantissa has at least one fractional digit (Java forces "X.0").
	s := strconv.FormatFloat(v, 'E', -1, 64)
	mant, exp, _ := strings.Cut(s, "E")
	if !strings.ContainsRune(mant, '.') {
		mant += ".0"
	}
	sign := ""
	if strings.HasPrefix(exp, "-") {
		sign = "-"
		exp = exp[1:]
	} else if strings.HasPrefix(exp, "+") {
		exp = exp[1:]
	}
	exp = strings.TrimLeft(exp, "0")
	if exp == "" {
		exp = "0"
	}
	return mant + "E" + sign + exp
}
```

加 `import "math"`。

- [ ] **Step 3: 改 `formatInterval` 加 dialect 分支**

```go
func (e *exprFormatter) formatInterval(n *ast.IntervalLiteral) string {
	if e.dialect == DialectDuckDB {
		// "INTERVAL 'sign value' startField" — sign INSIDE the literal, no TO field.
		sign := ""
		if n.Sign == "-" {
			sign = "-"
		}
		return "INTERVAL " + formatStringLiteral(sign+n.Value) + " " + strings.ToUpper(n.From)
	}
	// existing DEFAULT path unchanged...
}
```

- [ ] **Step 4: 改 `formatType` 加 ARRAY DUCKDB 分支**

```go
func (e *exprFormatter) formatType(t *ast.DataType) string {
	if e.dialect == DialectDuckDB && strings.EqualFold(t.Name, "ARRAY") && len(t.Parameters) == 1 {
		// T[] suffix form. Single param is a TypeParameter wrapping the element type.
		if tp, ok := t.Parameters[0].(*ast.TypeParameter); ok {
			return e.formatType(&tp.Type) + "[]"
		}
	}
	// existing path unchanged...
}
```

- [ ] **Step 5: 在 `formatFunctionCall` 加 GENERATE_TIMESTAMP_ARRAY DUCKDB 兜底**

```go
func (e *exprFormatter) formatFunctionCall(n *ast.FunctionCall) string {
	if e.dialect == DialectDuckDB && strings.EqualFold(n.Name.Last(), "GENERATE_TIMESTAMP_ARRAY") {
		panic("GENERATE_TIMESTAMP_ARRAY DuckDB special case not yet ported; not in P4 corpus")
	}
	// existing path unchanged...
}
```

- [ ] **Step 6: 跑测试**

```bash
go test ./internal/parser/formatter/... -run TestDuckDB -v
```

全部过。

- [ ] **Step 7: 跑全包 `go test ./...` 确认无回归**

```bash
go test ./... -short
```

特别注意 P3 difftest baseline 仍 100% 通过（formatter 改动不影响 DEFAULT 方言路径）。

- [ ] **Step 8: 提交任务 3**

```bash
git commit -m "feat(p4): DuckDB-dialect formatter branches (double/interval/array/GTA) (slice 1)"
```

## 任务 4：DUCKDB 方言 golden 差分基础设施

**Files:**
- Create: `cmd/capture-duckdb-golden/main.go`
- Edit: `Makefile`
- Create: `internal/difftest/duckdb_diff_test.go`

**为什么**：现有 `internal/difftest/difftest_test.go` 比 DEFAULT 方言（dry-plan + `modelingOnly=true`）。P4 转换层须比 DUCKDB 方言（dry-plan + `modelingOnly=false`）。复用 `LoadCorpus`，但要：① 第二棵 golden 树 ② 跑 `converter.Convert` 后比 ③ 沿用 `.error` 哨兵处理 oracle-error。

- [ ] **Step 1: 写 `cmd/capture-duckdb-golden/main.go`**

照搬 `cmd/capture-golden/main.go` 结构，区别仅 ① 强制 `modelingOnly=false`，② 输出到 `testdata/difftest/golden-duckdb/`。

```go
// Command capture-duckdb-golden replays every difftest case against a running
// Java wren-engine with modelingOnly=false (post DuckDB conversion) and
// freezes the dialect-converted SQL into golden files under golden-duckdb/.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	addr := flag.String("addr", "http://localhost:18080", "wren-engine oracle base URL")
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus directory")
	outDir := flag.String("out", "testdata/difftest/golden-duckdb", "DuckDB golden output dir")
	flag.Parse()

	cases, err := difftest.LoadCorpus(*casesDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	var ok, errs int
	for _, c := range cases {
		body, _ := json.Marshal(map[string]any{
			"manifest":     json.RawMessage(c.ManifestJSON),
			"sql":          c.SQL,
			"modelingOnly": false, // ← always false: capture DUCKDB-dialect output
		})
		req, _ := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/dry-plan", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("%s: %v", c.ID(), err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		dir := filepath.Join(*outDir, c.Group)
		_ = os.MkdirAll(dir, 0o755)
		base := filepath.Join(dir, c.Name+".sql")
		if resp.StatusCode/100 == 2 {
			_ = os.WriteFile(base, payload, 0o644)
			_ = os.Remove(base + ".error")
			ok++
			fmt.Printf("OK    %s\n", c.ID())
		} else {
			_ = os.WriteFile(base+".error", payload, 0o644)
			_ = os.Remove(base)
			errs++
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
		}
	}
	fmt.Printf("\ncaptured %d golden, %d oracle errors, %d total (golden-duckdb)\n", ok, errs, len(cases))
}
```

- [ ] **Step 2: Makefile target**

```makefile
capture-duckdb-golden:
	./tools/capture-golden.sh duckdb  # 若 tools/capture-golden.sh 接 arg
# 或直接：
duckdb-difftest:
	go test ./internal/difftest/... -run TestDifferentialDuckDB -v
duckdb-difftest-accept:
	go test ./internal/difftest/... -run TestDifferentialDuckDB -difftest.accept-duckdb
```

把 `capture-duckdb-golden` / `duckdb-difftest` / `duckdb-difftest-accept` 加到 `.PHONY`。

更简单方案：直接新 shell：

```bash
# tools/capture-duckdb-golden.sh
#!/bin/bash
set -euo pipefail
PORT="${PORT:-18080}"
go run ./cmd/capture-duckdb-golden -addr "http://localhost:${PORT}"
```

- [ ] **Step 3: 写 `internal/difftest/duckdb_diff_test.go`**

新的 baseline 文件 `testdata/difftest/baseline-duckdb.json`（与 default baseline 平行）。新的 accept flag `-difftest.accept-duckdb`。

```go
package difftest

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/rewrite"
)

var acceptDuckdbFlag = flag.Bool("difftest.accept-duckdb", false,
	"rewrite baseline-duckdb.json with the current results instead of asserting")

const (
	goldenDuckdbDir       = "../../testdata/difftest/golden-duckdb"
	baselineDuckdbPath    = "../../testdata/difftest/baseline-duckdb.json"
)

// runDuckdbCase rewrites + converts one case and compares to the duckdb golden.
func runDuckdbCase(c Case) (status, detail string) {
	goldenBase := filepath.Join(goldenDuckdbDir, c.Group, c.Name+".sql")
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java engine returned an error for this case (DuckDB mode)"
	}
	wantSQL, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "golden missing; run `make capture-duckdb-golden`"
	}

	actual, status, detail := goRewriteAndConvert(c)
	if status != "" {
		return status, detail
	}

	wantTok, err := Normalize(string(wantSQL))
	if err != nil {
		return "parser-gap", "lex Java golden: " + err.Error()
	}
	gotTok, err := Normalize(actual)
	if err != nil {
		return "parser-gap", "lex Go output: " + err.Error()
	}
	if slices.Equal(wantTok, gotTok) {
		return "pass", ""
	}
	return "fail", firstDiff(wantTok, gotTok)
}

func goRewriteAndConvert(c Case) (sql, status, detail string) {
	defer func() {
		if r := recover(); r != nil {
			sql, status, detail = "", "go-error", fmt.Sprintf("panic: %v", r)
		}
	}()
	var manifest dto.Manifest
	if err := json.Unmarshal(c.ManifestJSON, &manifest); err != nil {
		return "", "go-error", err.Error()
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	planned, err := rewrite.Rewrite(c.SQL, ctx, analyzed)
	if err != nil {
		return "", "go-error", err.Error()
	}
	conv := &converter.DuckDBSqlConverter{}
	out, err := conv.Convert(planned, ctx)
	if err != nil {
		return "", "go-error", err.Error()
	}
	return out, "", ""
}

func TestDifferentialDuckDB(t *testing.T) {
	cases, err := LoadCorpus(casesDir)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	results := make(map[string]string, len(cases))
	for _, c := range cases {
		s, d := runDuckdbCase(c)
		results[c.ID()] = s
		if s == "pass" {
			t.Logf("PASS  %s", c.ID())
		} else {
			t.Logf("%-12s %s — %s", s, c.ID(), d)
		}
	}

	if *acceptDuckdbFlag {
		writeDuckdbBaseline(t, results)
		return
	}

	base := readDuckdbBaseline(t)
	regr, impr := 0, 0
	for id, st := range results {
		w := base.Cases[id]
		switch {
		case w == "pass" && st != "pass":
			regr++
			t.Errorf("REGRESSION %s: baseline=pass, now=%s", id, st)
		case w != "pass" && st == "pass":
			impr++
			t.Errorf("%s now passes — run `make duckdb-difftest-accept` to update", id)
		}
	}
	t.Logf("\n%s", duckdbSummary(results))
	if regr == 0 && impr == 0 {
		t.Logf("baseline-duckdb 一致, 无回归")
	}
}

func duckdbSummary(results map[string]string) string {
	counts := map[string]int{}
	for _, s := range results {
		counts[s]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := fmt.Sprintf("DUCKDB 方言差分计分板: %d/%d 通过", counts["pass"], len(results))
	for _, k := range keys {
		s += fmt.Sprintf("\n  %-12s %d", k, counts[k])
	}
	return s
}

func readDuckdbBaseline(t *testing.T) baselineFile {
	t.Helper()
	raw, err := os.ReadFile(baselineDuckdbPath)
	if err != nil {
		t.Fatalf("read duckdb baseline: %v", err)
	}
	var b baselineFile
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("parse duckdb baseline: %v", err)
	}
	if b.Cases == nil {
		b.Cases = map[string]string{}
	}
	return b
}

func writeDuckdbBaseline(t *testing.T, results map[string]string) {
	t.Helper()
	b := baselineFile{Version: 1, Cases: results}
	raw, _ := json.MarshalIndent(b, "", "  ")
	if err := os.WriteFile(baselineDuckdbPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("baseline-duckdb 已更新")
}
```

**注意**：`converter.DuckDBSqlConverter.Convert` 此时仍是 P2 minimal 形态 —— 只跑 `parser.ParseSQL + formatter.FormatSQLDialect(.., DialectDuckDB)`，没 RewriteArray/Function。任务 5/6 加上后该测试结果会改善；先固化 baseline 当前形态作为基线。

- [ ] **Step 4: 跑 oracle（手动）+ capture-duckdb-golden**

操作说明（不在 plan 自动跑 —— 需要外部 Java oracle）：

```bash
# 1. 起 Java oracle（jar 在 ../wren-engine-0.9.3 目录下，docker 镜像 0.9.3 等价）
docker run -p 18080:8080 ghcr.io/canner/wren-engine:0.9.3 &

# 2. 捕获
./tools/capture-duckdb-golden.sh

# 3. 接受 baseline 当前结果
go test ./internal/difftest/... -run TestDifferentialDuckDB -difftest.accept-duckdb
```

- [ ] **Step 5: 提交任务 4**

```bash
git commit -m "test(p4): add DuckDB-dialect golden differential framework (slice 1)"
```

---

# 切片 2 — SqlRewrite 变换（`RewriteArray` + `RewriteFunction`）

目标：在 `internal/converter` 实现两条 SqlRewrite，机械对应 Java `RewriteArray` / `RewriteFunction`，挂回 `DuckDBSqlConverter.Convert`。

## 任务 5：`RewriteArray`

**Files:**
- Create: `internal/converter/rewrite_array.go`
- Create: `internal/converter/rewrite_array_test.go`

- [ ] **Step 1: 先写失败测试**

```go
package converter_test

import (
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/converter"
)

func TestRewriteArray_SubscriptOfArrayConstructor(t *testing.T) {
	conv := &converter.DuckDBSqlConverter{}
	got, err := conv.Convert("SELECT ARRAY[1,2,3][1]", &analyzer.SessionContext{})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	// IMPORTANT: ArrayConstructor formats args with "," (no space — Java
	// Joiner.on(",")); FunctionCall args format with ", " (Java
	// Joiner.on(", ")). So after rewrite we get ", "-joined arg list.
	// Verified against Java testArray: "SELECT array_value(1, 2, 3)[1]\n\n".
	want := "SELECT array_value(1, 2, 3)[1]\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteArray_BareArrayConstructorUntouched(t *testing.T) {
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT ARRAY[1,2,3]", &analyzer.SessionContext{})
	// Bare ArrayConstructor NOT inside subscript → still ARRAY[..]
	// (note: comma without space, ArrayConstructor formatter rule).
	want := "SELECT ARRAY[1,2,3]\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteArray_NestedSubscript(t *testing.T) {
	// (ARRAY[1,2,3][1]) + 1 — only inner ArrayConstructor rewrites.
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT ARRAY[1,2,3][1] + 1", &analyzer.SessionContext{})
	want := "SELECT (array_value(1, 2, 3)[1] + 1)\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
```

- [ ] **Step 2: 实现 `rewrite_array.go`**

```go
package converter

import (
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite"
)

// RewriteArray converts an ArrayConstructor that appears as the base of a
// SubscriptExpression into a FunctionCall to array_value(...). DuckDB rejects
// ARRAY[1,2,3][1] but accepts array_value(1,2,3)[1]. Mirrors Java
// io.wren.main.sql.duckdb.RewriteArray.
type RewriteArray struct{}

func (RewriteArray) Apply(root ast.Statement) ast.Statement {
	out := rewrite.RewriteNode(root, func(n ast.Node) (ast.Node, bool) {
		sub, ok := n.(*ast.SubscriptExpression)
		if !ok {
			return nil, false
		}
		ac, ok := sub.Base.(*ast.ArrayConstructor)
		if !ok {
			return nil, false
		}
		// Replace the SubscriptExpression's base with a FunctionCall(array_value, ...)
		// — but the rest of the SubscriptExpression (the Index) must still be
		// recursively rewritten, so we descend the index expression through
		// RewriteNode by re-emitting Base as the replacement and letting the
		// engine descend Index normally.
		newBase := &ast.FunctionCall{
			Name:      ast.QualifiedNameOf(ast.Identifier{Value: "array_value"}),
			Arguments: ac.Values,
		}
		repl := &ast.SubscriptExpression{
			BaseNode: sub.BaseNode,
			Base:     newBase,
			Index:    sub.Index,
		}
		return repl, true
	})
	return out.(ast.Statement)
}
```

`rewrite.RewriteNode` / `ast.QualifiedNameOf` 由 P3a 公开（P3c 计划亦确认）。`ast.Identifier{Value:..}` 非 delimited → formatter 渲为 `array_value` 全小写，与 Java `QualifiedName.of("array_value")` 一致。

- [ ] **Step 3: 在 `DuckDBSqlConverter.Convert` 中挂规则**

把 `internal/converter/duckdb.go` 改为：

```go
package converter

import (
	"fmt"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/parser/formatter"
)

// SqlConverter converts a SQL string from the rewriter's DEFAULT dialect
// output to a target-dialect SQL string. Mirrors Java io.wren.base.sql.SqlConverter.
type SqlConverter interface {
	Convert(sql string, ctx *analyzer.SessionContext) (string, error)
}

// duckdbRule is the local SqlRewrite interface for DuckDBSqlConverter.
// Mirrors Java io.wren.main.sql.SqlRewrite.
type duckdbRule interface {
	Apply(root ast.Statement) ast.Statement
}

// DuckDBSqlConverter mirrors Java DuckDBSqlConverter: parse → RewriteArray →
// RewriteFunction → format(.., DUCKDB).
type DuckDBSqlConverter struct{}

func (c *DuckDBSqlConverter) Convert(sqlText string, _ *analyzer.SessionContext) (string, error) {
	stmt, err := parser.ParseSQL(sqlText)
	if err != nil {
		return "", fmt.Errorf("duckdb converter parse: %w", err)
	}
	rules := []duckdbRule{
		RewriteArray{},
		// RewriteFunction added in 任务 6.
	}
	for _, r := range rules {
		stmt = r.Apply(stmt)
	}
	return formatter.FormatSQLDialect(stmt, formatter.DialectDuckDB), nil
}
```

**Signature change**：`Convert` 从 `(string) string` 改为 `(string, ctx) (string, error)`，需更新调用方：
- `cmd/wren-server/main.go`
- `internal/service/preview.go`
- `internal/difftest/duckdb_diff_test.go`（任务 4 已写为期望 `(string, error)`）

- [ ] **Step 4: 更新 caller**

`internal/service/preview.go`：

```go
// Before:
convertedSQL := s.sqlConverter.Convert(plannedSQL, analyzer.GetSessionContext(ctx))
// After:
convertedSQL, err := s.sqlConverter.Convert(plannedSQL, analyzer.GetSessionContext(ctx))
if err != nil {
    return nil, fmt.Errorf("dialect convert failed: %w", err)
}
```

`cmd/wren-server/main.go` 无变化（接口类型 `converter.SqlConverter` 已升级，赋值仍兼容）。

- [ ] **Step 5: 跑测试**

```bash
go test ./internal/converter/... -run TestRewriteArray -v
```

- [ ] **Step 6: 提交任务 5**

```bash
git commit -m "feat(p4): RewriteArray + DuckDBSqlConverter pipeline (slice 2)"
```

## 任务 6：`RewriteFunction`

**Files:**
- Create: `internal/converter/rewrite_function.go`
- Create: `internal/converter/rewrite_function_test.go`
- Edit: `internal/converter/duckdb.go`

- [ ] **Step 1: 写失败测试**

```go
package converter_test

import (
	"testing"

	"github.com/wren-engine/wren/internal/analyzer"
	"github.com/wren-engine/wren/internal/converter"
)

func TestRewriteFunction_PgToDuckDBMapping(t *testing.T) {
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT generate_array(1, 10)", &analyzer.SessionContext{})
	want := "SELECT generate_series(1, 10)\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteFunction_LowercaseEvenIfNoMapping(t *testing.T) {
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT COUNT(*) FROM t", &analyzer.SessionContext{})
	// COUNT lowercased to count even though no PG→DuckDB mapping fires.
	want := "SELECT count(*)\nFROM\n  t\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteFunction_QualifiedNameUsesSuffix(t *testing.T) {
	// Mapping looks up suffix (last segment), not full qualified name.
	// generate_array → generate_series. some.schema.generate_array → generate_series (suffix-only match — Java behavior).
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT some.schema.generate_array(1)", &analyzer.SessionContext{})
	want := "SELECT generate_series(1)\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestRewriteFunction_NestedFunctionCalls(t *testing.T) {
	// Outer + inner both lowercased.
	conv := &converter.DuckDBSqlConverter{}
	got, _ := conv.Convert("SELECT SUM(COUNT(*))", &analyzer.SessionContext{})
	want := "SELECT sum(count(*))\n\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
```

- [ ] **Step 2: 实现 `rewrite_function.go`**

```go
package converter

import (
	"strings"

	"github.com/wren-engine/wren/internal/parser/ast"
	"github.com/wren-engine/wren/internal/rewrite"
)

// pgToDuckDB maps Postgres-style function names to their DuckDB equivalents.
// Lookup uses the LOWERCASE suffix (last segment of QualifiedName). Mirrors
// Java DuckDBMetadata.PG_TO_DUCKDB_FUNCTION_NAME_MAPPINGS.
var pgToDuckDB = map[string]string{
	"generate_array": "generate_series",
}

// RewriteFunction lowercases every FunctionCall name and applies the PG→DuckDB
// mapping when a suffix matches. Mirrors Java RewriteFunction.
type RewriteFunction struct{}

func (RewriteFunction) Apply(root ast.Statement) ast.Statement {
	out := rewrite.RewriteNode(root, func(n ast.Node) (ast.Node, bool) {
		fc, ok := n.(*ast.FunctionCall)
		if !ok {
			return nil, false
		}
		// Build lowercased parts.
		lowered := make([]ast.Identifier, len(fc.Name.Parts))
		for i, p := range fc.Name.Parts {
			lowered[i] = ast.Identifier{Value: strings.ToLower(p.Value)}
		}
		// Suffix-based PG→DuckDB mapping.
		suffix := lowered[len(lowered)-1].Value
		if mapped, ok := pgToDuckDB[suffix]; ok {
			lowered = []ast.Identifier{{Value: mapped}}
		}
		// Return a new FunctionCall with the rewritten name; everything else
		// preserved. Note we MUST emit a copy — Java is immutable too.
		newName := ast.QualifiedName{Parts: lowered, OriginalParts: lowered}
		repl := *fc
		repl.Name = newName
		// Returning (repl, true) tells RewriteNode "we replaced", so it WON'T
		// descend further. But we need argument expressions to be visited too.
		// Convention: return (nil, false) for partial replacements that need
		// child descent — and instead mutate in place via the rewriter pattern.
		// Cleanest: pre-compute, then return (repl, true) and let
		// argument-level rewrites be picked up because nested FunctionCalls
		// will be visited by RewriteNode's own descent post-replacement.
		// HOWEVER: P3a's RewriteNode is "replace and stop" (does not descend
		// into replacement). To handle nested FunctionCall, the rewriter must
		// pre-rewrite arguments. Do this:
		newArgs := make([]ast.Expression, len(fc.Arguments))
		for i, a := range fc.Arguments {
			rewritten := rewrite.RewriteNode(a, currentHook())
			newArgs[i] = rewritten.(ast.Expression)
		}
		repl.Arguments = newArgs
		return &repl, true
	})
	return out.(ast.Statement)
}
```

**问题**: 嵌套 FunctionCall —— P3a `RewriteNode` 在 hook 返 `(repl, true)` 时不再向下 descend；外层 `RewriteFunction` 须自己递归 children。

**更简洁的实现**（避免 `currentHook()` 引用循环）：

```go
func (RewriteFunction) Apply(root ast.Statement) ast.Statement {
	// Two-pass: first rewrite the deepest FunctionCalls (RewriteNode descends
	// children before applying hook on the parent? — verify with P3a). If
	// RewriteNode is parent-first, then explicitly descend Arguments before
	// returning the replacement. P3a's base_tree_rewriter rewrites children
	// in the Rebuild step BEFORE the hook on each level, so this works
	// naturally — we simply return the lowercased FunctionCall and the
	// already-rewritten children come along.
	out := rewrite.RewriteNode(root, rewriteFunctionHook)
	return out.(ast.Statement)
}

func rewriteFunctionHook(n ast.Node) (ast.Node, bool) {
	fc, ok := n.(*ast.FunctionCall)
	if !ok {
		return nil, false
	}
	lowered := make([]ast.Identifier, len(fc.Name.Parts))
	for i, p := range fc.Name.Parts {
		lowered[i] = ast.Identifier{Value: strings.ToLower(p.Value)}
	}
	suffix := lowered[len(lowered)-1].Value
	if mapped, ok := pgToDuckDB[suffix]; ok {
		lowered = []ast.Identifier{{Value: mapped}}
	}
	newName := ast.QualifiedName{Parts: lowered, OriginalParts: lowered}
	repl := *fc
	repl.Name = newName
	return &repl, true
}
```

**Decision**：在 Step 1 测试运行后通过 `TestRewriteFunction_NestedFunctionCalls` 验证 P3a `RewriteNode` 的 descent 行为。若该测试**通过**（嵌套已递归），用 17 行简洁版本；若**失败**（嵌套未递归），切到 25 行显式 descend 版本。两版本都列在代码块内；任务执行时按测试结果选定。

- [ ] **Step 3: 把 RewriteFunction 加进 Convert pipeline**

`internal/converter/duckdb.go`:

```go
rules := []duckdbRule{
	RewriteArray{},
	RewriteFunction{},  // ← 新增
}
```

- [ ] **Step 4: 跑测试 + 决定 hook 版本**

```bash
go test ./internal/converter/... -run TestRewriteFunction -v
```

- [ ] **Step 5: 跑 DUCKDB 差分（验证转换层在 corpus 上对齐 Java）**

需要先：
1. P3 既有 corpus（22+P3a+P3b+P3c）的 `golden-duckdb/` 已捕获（任务 4 Step 4 操作的产出）。
2. baseline-duckdb.json 已接受当前结果。

```bash
go test ./internal/difftest/... -run TestDifferentialDuckDB -v
```

期望结果：
- `tpch/2..3,5..22`（20 条标准查询）：`pass`。
- `tpch/1` / `tpch/4`：`oracle-error`（Java 自身失败，与 default 线一致）。
- `metric/*` 4 条：`pass`。
- `tpch/m_*` 6 条：5 `pass` + 1 `fail`（m_orders 仍 Jinja，default 线同状态）。
- `tpch/met_*` 5 条：5 `go-error`/`fail`（Jinja，default 线同状态）。
- `viewenum/*` 5 条：`pass`。
- `tpch/v_use_*` 4 条：`go-error`/`fail`（Jinja）。
- `tpch/v_enum`：`pass`。

若有 default 线 pass 但 duckdb 线 fail 的用例，必是 DUCKDB 特化分支遗漏 —— 回任务 3 排查。

- [ ] **Step 6: 提交任务 6**

```bash
git commit -m "feat(p4): RewriteFunction (lowercase + PG→DuckDB mapping) (slice 2)"
```

---

# 切片 3 — 连接器执行（DuckDBMetadata + DuckdbRecordIterator）

目标：在 切片 0 骨架上加完整 RecordIterator + DuckDBMetadata（Metadata 接口完整实现 + init/session SQL 状态）。

## 任务 7：`DuckdbRecordIterator` + Metadata 完整实现

**Files:**
- Replace: `internal/connector/duckdb/record_iterator.go`（替换 slice 0 的 stub）
- Create: `internal/connector/duckdb/metadata.go`
- Create: `internal/connector/duckdb/metadata_test.go`
- Create: `internal/connector/duckdb/record_iterator_test.go`

- [ ] **Step 1: 写失败测试 `record_iterator_test.go`**

```go
package duckdb

import (
	"context"
	"testing"
)

func TestRecordIterator_BasicTypes(t *testing.T) {
	c := NewConnector()
	t.Cleanup(func() { _ = c.Close() })

	it, err := c.Query(context.Background(),
		"SELECT 1::INTEGER AS a, 'x'::VARCHAR AS b, 3.14::DOUBLE AS c, NULL::VARCHAR AS d")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer it.Close()

	cols := it.Columns()
	if len(cols) != 4 || cols[0].Name != "a" || cols[0].Type != "INTEGER" ||
		cols[1].Type != "VARCHAR" || cols[2].Type != "DOUBLE" || cols[3].Type != "VARCHAR" {
		t.Errorf("unexpected cols: %+v", cols)
	}

	if !it.Next() {
		t.Fatalf("expected one row")
	}
	row := it.Get()
	// int -> int64 (database/sql canonical), string -> string, float -> float64, null -> nil
	if row[0] != int64(1) || row[1] != "x" || row[2] != 3.14 || row[3] != nil {
		t.Errorf("unexpected row: %#v", row)
	}
	if it.Next() {
		t.Errorf("expected only one row")
	}
}

func TestRecordIterator_ArrayType(t *testing.T) {
	c := NewConnector()
	t.Cleanup(func() { _ = c.Close() })

	it, err := c.Query(context.Background(), "SELECT array_value(1, 2, 3) AS a")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer it.Close()
	cols := it.Columns()
	if cols[0].Type != "INTEGER[]" {
		t.Errorf("array type column: got %q want %q", cols[0].Type, "INTEGER[]")
	}
	if !it.Next() {
		t.Fatalf("expected one row")
	}
	row := it.Get()
	// DuckDB array → []any (or []interface{}). Accept either, but values must match.
	arr, ok := row[0].([]any)
	if !ok {
		t.Fatalf("array value: not []any, got %T", row[0])
	}
	if len(arr) != 3 || arr[0] != int64(1) || arr[1] != int64(2) || arr[2] != int64(3) {
		t.Errorf("array values: %#v", arr)
	}
}
```

- [ ] **Step 2: 实现 `record_iterator.go`**

```go
package duckdb

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/wren-engine/wren/internal/connector"
)

// recordIterator wraps a *sql.Rows in the connector.RecordIterator interface.
// Mirrors Java DuckdbRecordIterator: column metadata captured at construction,
// then row-by-row iteration with type conversion.
type recordIterator struct {
	rows    *sql.Rows
	columns []connector.Column
	scanned []any
	holders []any
}

func newRecordIterator(rows *sql.Rows) (connector.RecordIterator, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("duckdb column types: %w", err)
	}
	cols := make([]connector.Column, len(types))
	for i, t := range types {
		cols[i] = connector.Column{
			Name: t.Name(),
			Type: strings.ToUpper(t.DatabaseTypeName()),
		}
	}
	scanned := make([]any, len(cols))
	holders := make([]any, len(cols))
	for i := range holders {
		holders[i] = &scanned[i]
	}
	return &recordIterator{
		rows:    rows,
		columns: cols,
		scanned: scanned,
		holders: holders,
	}, nil
}

func (r *recordIterator) Columns() []connector.Column { return r.columns }

func (r *recordIterator) Next() bool {
	if !r.rows.Next() {
		return false
	}
	if err := r.rows.Scan(r.holders...); err != nil {
		// database/sql convention: error during scan means Next() returns false
		// and Err() reveals the cause. We swallow here to match the simple
		// connector.RecordIterator contract; production paths should call Err().
		return false
	}
	return true
}

func (r *recordIterator) Get() []any {
	out := make([]any, len(r.scanned))
	for i, v := range r.scanned {
		out[i] = convertValue(r.columns[i].Type, v)
	}
	return out
}

func (r *recordIterator) Close() error {
	return r.rows.Close()
}

// convertValue maps go-duckdb's scanned value to the canonical envelope
// representation. Mirrors Java DuckdbRecordIterator.convertValue:
//   - null      -> nil
//   - TIMESTAMP -> LocalDateTime (here: time.Time, ISO-8601 stringifies via Jackson)
//   - BLOB      -> []byte (Jackson base64-encodes byte[])
//   - JSON      -> string (toString)
//   - T[]       -> []any with recursive conversion
//   - otherwise -> as-is (database/sql canonical types)
func convertValue(typeName string, v any) any {
	if v == nil {
		return nil
	}
	if strings.HasSuffix(typeName, "[]") {
		inner := typeName[:len(typeName)-2]
		switch a := v.(type) {
		case []any:
			out := make([]any, len(a))
			for i, x := range a {
				out[i] = convertValue(inner, x)
			}
			return out
		case []interface{}:
			out := make([]any, len(a))
			for i, x := range a {
				out[i] = convertValue(inner, x)
			}
			return out
		}
		return v
	}
	switch typeName {
	case "JSON":
		// go-duckdb returns JSON as string already; mirror Java toString().
		return fmt.Sprintf("%v", v)
	}
	// TIMESTAMP / BLOB pass through — go-duckdb returns time.Time / []byte
	// which Go's encoding/json encodes ISO-8601 / base64 respectively. That
	// matches Jackson default serialization for LocalDateTime + byte[].
	return v
}
```

- [ ] **Step 3: 实现 `metadata.go`**

```go
package duckdb

import (
	"context"
	"strings"
	"sync"

	"github.com/wren-engine/wren/internal/connector"
)

// Metadata wraps a Connector and adds init/session SQL state. Mirrors Java
// DuckDBMetadata. The init SQL is executed against a freshly-opened pool; the
// session SQL is appended to each new connection (DuckDB has no native session
// hook, so we currently re-execute on reload — same as Java HikariCP path).
type Metadata struct {
	mu        sync.RWMutex
	conn      *Connector
	initSQL   string
	sessionSQL string
}

// NewMetadata creates a Metadata wrapping a fresh in-memory Connector.
func NewMetadata() *Metadata {
	return &Metadata{conn: NewConnector()}
}

// Close closes the underlying connector.
func (m *Metadata) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn == nil {
		return nil
	}
	err := m.conn.Close()
	m.conn = nil
	return err
}

// DirectQuery delegates to the underlying connector.
func (m *Metadata) DirectQuery(ctx context.Context, sql string, params []connector.Parameter) (connector.RecordIterator, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn.DirectQuery(ctx, sql, params)
}

// DescribeQuery delegates.
func (m *Metadata) DescribeQuery(ctx context.Context, sql string, params []connector.Parameter) ([]connector.Column, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn.DescribeQuery(ctx, sql, params)
}

// DirectDDL delegates.
func (m *Metadata) DirectDDL(ctx context.Context, sql string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn.DirectDDL(ctx, sql)
}

// InitSQL returns the cached init SQL.
func (m *Metadata) InitSQL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initSQL
}

// SetInitSQL replaces the init SQL, closes the existing pool, opens a fresh one,
// and re-executes the init SQL. Mirrors Java DuckDBMetadata.setInitSQL+reload.
func (m *Metadata) SetInitSQL(ctx context.Context, sql string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.initSQL
	m.initSQL = sql

	if m.conn != nil {
		_ = m.conn.Close()
	}
	m.conn = NewConnector()
	if strings.TrimSpace(sql) != "" {
		if err := m.conn.ExecuteDDL(ctx, sql); err != nil {
			// Roll back to previous state on failure (Java behavior).
			m.initSQL = prev
			_ = m.conn.Close()
			m.conn = NewConnector()
			if strings.TrimSpace(prev) != "" {
				_ = m.conn.ExecuteDDL(ctx, prev)
			}
			return err
		}
	}
	return nil
}

// AppendInitSQL appends sql to init SQL and executes the appended fragment.
func (m *Metadata) AppendInitSQL(ctx context.Context, sql string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.conn.ExecuteDDL(ctx, sql); err != nil {
		return err
	}
	if m.initSQL == "" {
		m.initSQL = sql
	} else {
		m.initSQL = m.initSQL + "\n" + sql
	}
	return nil
}

// SessionSQL returns the cached session SQL.
func (m *Metadata) SessionSQL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionSQL
}

// SetSessionSQL replaces the session SQL and re-pools. Java reuses the same
// init-SQL semantics; we mirror by re-executing session SQL via DDL.
func (m *Metadata) SetSessionSQL(ctx context.Context, sql string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.sessionSQL
	m.sessionSQL = sql
	if strings.TrimSpace(sql) != "" {
		if err := m.conn.ExecuteDDL(ctx, sql); err != nil {
			m.sessionSQL = prev
			return err
		}
	}
	return nil
}

// AppendSessionSQL appends sql to session SQL and executes the appended fragment.
func (m *Metadata) AppendSessionSQL(ctx context.Context, sql string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.conn.ExecuteDDL(ctx, sql); err != nil {
		return err
	}
	if m.sessionSQL == "" {
		m.sessionSQL = sql
	} else {
		m.sessionSQL = m.sessionSQL + "\n" + sql
	}
	return nil
}
```

- [ ] **Step 4: 写 metadata_test.go**

```go
package duckdb

import (
	"context"
	"testing"
)

func TestMetadata_InitSQLCreatesTable(t *testing.T) {
	m := NewMetadata()
	t.Cleanup(func() { _ = m.Close() })

	if err := m.SetInitSQL(context.Background(), "CREATE TABLE t(a INTEGER); INSERT INTO t VALUES (1), (2);"); err != nil {
		t.Fatalf("set init: %v", err)
	}

	it, err := m.DirectQuery(context.Background(), "SELECT a FROM t ORDER BY a", nil)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer it.Close()
	var got []int64
	for it.Next() {
		row := it.Get()
		got = append(got, row[0].(int64))
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("expected [1 2], got %v", got)
	}
}

func TestMetadata_AppendInitSQL(t *testing.T) {
	m := NewMetadata()
	t.Cleanup(func() { _ = m.Close() })

	_ = m.SetInitSQL(context.Background(), "CREATE TABLE t(a INTEGER);")
	if err := m.AppendInitSQL(context.Background(), "INSERT INTO t VALUES (42);"); err != nil {
		t.Fatalf("append: %v", err)
	}
	cols, err := m.DescribeQuery(context.Background(), "SELECT a FROM t", nil)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if len(cols) != 1 || cols[0].Type != "INTEGER" {
		t.Errorf("cols: %+v", cols)
	}
}
```

- [ ] **Step 5: 跑测试**

```bash
go test ./internal/connector/duckdb/... -v
```

- [ ] **Step 6: 提交任务 7**

```bash
git commit -m "feat(p4): DuckdbRecordIterator + Metadata (init/session SQL) (slice 3)"
```

---

# 切片 4 — PreviewService 完成 + 响应信封 DTO

目标：把 `PreviewService.Preview` 从「重写+转换+TODO 执行」补成「重写+转换+执行+返回行」；把 `QueryResultDto` 迁到 `internal/dto/preview_response.go`；加 envelope 差分测试。

## 任务 8：`QueryResultDto` 迁出到 `internal/dto/preview_response.go`

**Files:**
- Create: `internal/dto/preview_response.go`
- Edit: `internal/service/preview.go`
- Edit: `internal/server/duckdb_handler.go`

- [ ] **Step 1: 新建 `internal/dto/preview_response.go`**

```go
package dto

// PreviewColumn mirrors Java io.wren.base.Column. The type is upper-case
// (Java forces it via toUpperCase(Locale.ROOT) at construction).
type PreviewColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// PreviewResponse is the JSON envelope returned by /v1/mdl/preview,
// /v1/mdl/dry-run, and /v1/data-source/duckdb/query. Mirrors Java
// io.wren.main.web.dto.QueryResultDto.
type PreviewResponse struct {
	Columns []PreviewColumn `json:"columns"`
	Data    [][]any         `json:"data"`
}
```

- [ ] **Step 2: 删 `service.QueryResultDto`，把 `preview.go` 改用 `dto.PreviewResponse`**

把 `internal/service/preview.go` 顶端：

```go
// QueryResultDto represents query results.  ← 删除整个 struct
type QueryResultDto struct {
	Columns []connector.Column `json:"columns"`
	Data    [][]any            `json:"data"`
}
```

替换 `Preview` 返回类型为 `*dto.PreviewResponse`：

```go
import (
    "github.com/wren-engine/wren/internal/dto"
    // ...
)

func (s *PreviewService) Preview(ctx context.Context, wrenMDL *mdl.WrenMDL, sql string, limit int64) (*dto.PreviewResponse, error) {
    // ...
}
```

- [ ] **Step 3: 改 `internal/server/duckdb_handler.go` 用新 DTO**

```go
// Before:
json.NewEncoder(w).Encode(service.QueryResultDto{
    Columns: cols,
    Data:    data,
})
// After: 把 cols ([]connector.Column) 转成 []dto.PreviewColumn，再封装到 dto.PreviewResponse.
out := dto.PreviewResponse{
    Columns: make([]dto.PreviewColumn, len(cols)),
    Data:    data,
}
for i, c := range cols {
    out.Columns[i] = dto.PreviewColumn{Name: c.Name, Type: c.Type}
}
json.NewEncoder(w).Encode(out)
```

- [ ] **Step 4: build/vet 验证**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 5: 提交任务 8**

```bash
git commit -m "refactor(p4): move QueryResultDto to dto.PreviewResponse (slice 4)"
```

## 任务 9：`PreviewService.Preview` 真实执行

**Files:**
- Edit: `internal/service/preview.go`
- Create: `internal/service/preview_test.go`

- [ ] **Step 1: 先写失败 e2e 测试**

```go
package service_test

import (
	"context"
	"testing"

	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/service"
)

func TestPreview_RoundtripsSyntheticData(t *testing.T) {
	// Setup: empty MDL + an in-memory DuckDB with a VALUES table.
	wrenMDL, _ := mdl.WrenMDLFromJSON(`{"catalog":"wren","schema":"test","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`)
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.NewConfigManager()

	svc := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, cfg)
	res, err := svc.Preview(context.Background(), wrenMDL,
		"SELECT * FROM (VALUES (1, 'a'), (2, 'b')) AS v(a, b)", 100)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(res.Columns) != 2 || res.Columns[0].Name != "a" || res.Columns[1].Name != "b" {
		t.Fatalf("cols: %+v", res.Columns)
	}
	if res.Columns[0].Type != "INTEGER" || res.Columns[1].Type != "VARCHAR" {
		t.Errorf("col types: %+v", res.Columns)
	}
	if len(res.Data) != 2 {
		t.Fatalf("rows: %d", len(res.Data))
	}
	if res.Data[0][0] != int64(1) || res.Data[0][1] != "a" ||
		res.Data[1][0] != int64(2) || res.Data[1][1] != "b" {
		t.Errorf("rows: %#v", res.Data)
	}
}

func TestPreview_RespectsLimit(t *testing.T) {
	wrenMDL, _ := mdl.WrenMDLFromJSON(`{"catalog":"wren","schema":"test","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`)
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	svc := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	res, err := svc.Preview(context.Background(), wrenMDL, "SELECT * FROM range(0, 100) AS t(n)", 5)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(res.Data) != 5 {
		t.Errorf("limit=5 expected 5 rows, got %d", len(res.Data))
	}
}
```

- [ ] **Step 2: 实现 `Preview`（替换 TODO）**

```go
func (s *PreviewService) Preview(ctx context.Context, wrenMDL *mdl.WrenMDL, sqlText string, limit int64) (*dto.PreviewResponse, error) {
	ctx = analyzer.WithSessionContext(ctx, &analyzer.SessionContext{
		Catalog:             wrenMDL.Catalog(),
		Schema:              wrenMDL.Schema(),
		EnableDynamicFields: s.configMgr.Get().Wren.EnableDynamicFields,
	})
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	planned, err := rewrite.Rewrite(sqlText, analyzer.GetSessionContext(ctx), analyzed)
	if err != nil {
		return nil, fmt.Errorf("rewrite failed: %w", err)
	}
	converted, err := s.sqlConverter.Convert(planned, analyzer.GetSessionContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("dialect convert failed: %w", err)
	}

	it, err := s.metadata.DirectQuery(ctx, converted, nil)
	if err != nil {
		return nil, fmt.Errorf("execute failed: %w", err)
	}
	defer it.Close()

	cols := it.Columns()
	resp := &dto.PreviewResponse{
		Columns: make([]dto.PreviewColumn, len(cols)),
		Data:    make([][]any, 0, limit),
	}
	for i, c := range cols {
		resp.Columns[i] = dto.PreviewColumn{Name: c.Name, Type: c.Type}
	}
	for it.Next() {
		if int64(len(resp.Data)) >= limit {
			break
		}
		resp.Data = append(resp.Data, it.Get())
	}
	return resp, nil
}
```

- [ ] **Step 3: 改 `DryRun` 返 `[]dto.PreviewColumn`（envelope 一致）**

`/v1/mdl/dry-run` 在 Java 返 `List<Column>`。当前 Go 返 `[]connector.Column`，需改 `[]dto.PreviewColumn`。

```go
func (s *PreviewService) DryRun(ctx context.Context, wrenMDL *mdl.WrenMDL, sqlText string) ([]dto.PreviewColumn, error) {
	ctx = analyzer.WithSessionContext(ctx, &analyzer.SessionContext{
		Catalog: wrenMDL.Catalog(),
		Schema:  wrenMDL.Schema(),
	})
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	planned, err := rewrite.Rewrite(sqlText, analyzer.GetSessionContext(ctx), analyzed)
	if err != nil {
		return nil, err
	}
	converted, err := s.sqlConverter.Convert(planned, analyzer.GetSessionContext(ctx))
	if err != nil {
		return nil, err
	}
	cols, err := s.metadata.DescribeQuery(ctx, converted, nil)
	if err != nil {
		return nil, err
	}
	out := make([]dto.PreviewColumn, len(cols))
	for i, c := range cols {
		out[i] = dto.PreviewColumn{Name: c.Name, Type: c.Type}
	}
	return out, nil
}
```

- [ ] **Step 4: 改 `DryPlan(modelingOnly=false)` 走 sqlConverter**

```go
func (s *PreviewService) DryPlan(ctx context.Context, wrenMDL *mdl.WrenMDL, sqlText string, modelingOnly bool) (string, error) {
	ctx = analyzer.WithSessionContext(ctx, &analyzer.SessionContext{
		Catalog: wrenMDL.Catalog(),
		Schema:  wrenMDL.Schema(),
	})
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	planned, err := rewrite.Rewrite(sqlText, analyzer.GetSessionContext(ctx), analyzed)
	if err != nil {
		return "", err
	}
	if modelingOnly {
		return planned, nil
	}
	return s.sqlConverter.Convert(planned, analyzer.GetSessionContext(ctx))
}
```

- [ ] **Step 5: 跑测试**

```bash
go test ./internal/service/... -v
```

- [ ] **Step 6: 提交任务 9**

```bash
git commit -m "feat(p4): PreviewService.Preview executes + returns envelope (slice 4)"
```

## 任务 10：信封 JSON 差分基础设施

**Files:**
- Create: `cmd/capture-envelope-golden/main.go`
- Create: `internal/difftest/envelope_diff_test.go`
- Create: `testdata/difftest/cases/exec_smoke/group.json`
- Create: `testdata/difftest/cases/exec_smoke/mdl.json`
- Create: `testdata/difftest/cases/exec_smoke/queries/{const,values_basic,values_nulls}.sql`

- [ ] **Step 1: 新建 `cases/exec_smoke/` 合成语料**

`group.json` —— 与 `viewenum` 不同：不能开 `modelingOnly`（envelope 测试需要真执行）。但 `Case.ModelingOnly` 字段仅供 default 差分线使用；envelope 测试自己控逻辑。`exec_smoke` 用 `modelingOnly: true`（无意义，envelope 不读它）：

```json
{"modelingOnly": true}
```

`mdl.json` —— 空 MDL（envelope 测试用的查询不引用任何 model）：

```json
{
  "catalog": "wren",
  "schema": "main",
  "models": [],
  "relationships": [],
  "metrics": [],
  "cumulativeMetrics": [],
  "enumDefinitions": [],
  "views": [],
  "macros": []
}
```

`queries/const.sql`:

```sql
SELECT 1 AS a, 'hello' AS b
```

`queries/values_basic.sql`:

```sql
SELECT * FROM (VALUES (1, 'a'), (2, 'b')) v(a, b)
```

`queries/values_nulls.sql`:

```sql
SELECT * FROM (VALUES (1, NULL), (NULL, 'b'), (NULL, NULL)) v(a, b)
```

- [ ] **Step 2: 写 `cmd/capture-envelope-golden/main.go`**

```go
// Command capture-envelope-golden replays every case against a running Java
// wren-engine via /v1/mdl/preview and freezes the JSON envelope under
// testdata/difftest/golden-envelope/.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	addr := flag.String("addr", "http://localhost:18080", "wren-engine oracle base URL")
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus dir")
	outDir := flag.String("out", "testdata/difftest/golden-envelope", "envelope golden dir")
	groupsFlag := flag.String("groups", "exec_smoke,viewenum", "comma-separated corpus groups to capture")
	flag.Parse()

	wantGroups := map[string]bool{}
	for _, g := range bytes.Split([]byte(*groupsFlag), []byte(",")) {
		wantGroups[string(bytes.TrimSpace(g))] = true
	}

	cases, _ := difftest.LoadCorpus(*casesDir)
	client := &http.Client{Timeout: 60 * time.Second}
	ok, errs := 0, 0
	for _, c := range cases {
		if !wantGroups[c.Group] {
			continue
		}
		body, _ := json.Marshal(map[string]any{
			"manifest": json.RawMessage(c.ManifestJSON),
			"sql":      c.SQL,
			"limit":    100,
		})
		req, _ := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/preview", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("%s: %v", c.ID(), err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		dir := filepath.Join(*outDir, c.Group)
		_ = os.MkdirAll(dir, 0o755)
		base := filepath.Join(dir, c.Name+".json")
		if resp.StatusCode/100 == 2 {
			_ = os.WriteFile(base, payload, 0o644)
			_ = os.Remove(base + ".error")
			ok++
		} else {
			_ = os.WriteFile(base+".error", payload, 0o644)
			_ = os.Remove(base)
			errs++
		}
		fmt.Printf("%s %s\n", map[bool]string{true: "OK   ", false: "ERROR"}[resp.StatusCode/100 == 2], c.ID())
	}
	fmt.Printf("\ncaptured %d envelope, %d errors (groups=%s)\n", ok, errs, *groupsFlag)
}
```

- [ ] **Step 3: 写 `envelope_diff_test.go`**

```go
package difftest

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/service"
)

var (
	acceptEnvelopeFlag = flag.Bool("difftest.accept-envelope", false, "rewrite baseline-envelope.json")

	envelopeGroups  = map[string]bool{"exec_smoke": true, "viewenum": true}
	envelopeDir     = "../../testdata/difftest/golden-envelope"
	baselineEnvelopePath = "../../testdata/difftest/baseline-envelope.json"
)

// runEnvelopeCase executes a case against the Go preview pipeline and compares
// the JSON envelope to the Java golden. "Structural equivalence" means: same
// column count, names, types; same row count; cell values compared with
// numeric coercion (Go int64 vs Java Integer/Long both Unmarshal to float64).
func runEnvelopeCase(t *testing.T, c Case) (status, detail string) {
	t.Helper()
	goldenBase := filepath.Join(envelopeDir, c.Group, c.Name+".json")
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java preview errored"
	}
	want, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "envelope golden missing"
	}

	var manifest = mustUnmarshalManifest(t, c.ManifestJSON)
	wrenMDL := mdl.WrenMDLFromManifest(manifest)
	md := duckdb.NewMetadata()
	defer md.Close()

	// IMPORTANT: for cases/viewenum we need to seed in-memory tables via init SQL.
	// This requires a shared init.sql file per group — task 11 wires this.
	if c.Group == "viewenum" {
		initSQL, err := os.ReadFile("../../testdata/difftest/cases/viewenum/init.sql")
		if err == nil {
			_ = md.SetInitSQL(context.Background(), string(initSQL))
		}
	}

	svc := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	got, err := svc.Preview(context.Background(), wrenMDL, c.SQL, 100)
	if err != nil {
		return "go-error", err.Error()
	}

	var wantEnv, gotEnv dto.PreviewResponse
	if err := json.Unmarshal(want, &wantEnv); err != nil {
		return "parser-gap", "unmarshal Java golden: " + err.Error()
	}
	gotJSON, _ := json.Marshal(got)
	_ = json.Unmarshal(gotJSON, &gotEnv) // round-trip to normalize int→float

	if !envelopeEqual(&wantEnv, &gotEnv) {
		return "fail", envelopeDiff(&wantEnv, &gotEnv)
	}
	return "pass", ""
}

func mustUnmarshalManifest(t *testing.T, raw []byte) *dto.Manifest {
	t.Helper()
	// Avoid pulling internal/dto in the helper signature for circularity — Manifest
	// already lives in dto, we just need its pointer.
	var m dto.Manifest
	_ = json.Unmarshal(raw, &m)
	return &m
}

func envelopeEqual(a, b *dto.PreviewResponse) bool {
	if len(a.Columns) != len(b.Columns) || len(a.Data) != len(b.Data) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i].Name != b.Columns[i].Name || a.Columns[i].Type != b.Columns[i].Type {
			return false
		}
	}
	for r := range a.Data {
		if !reflect.DeepEqual(a.Data[r], b.Data[r]) {
			return false
		}
	}
	return true
}

func envelopeDiff(a, b *dto.PreviewResponse) string {
	// Lightweight diff for human inspection; full DeepEqual already failed.
	if len(a.Columns) != len(b.Columns) {
		return "column count differs"
	}
	if len(a.Data) != len(b.Data) {
		return "row count differs"
	}
	for i := range a.Columns {
		if a.Columns[i] != b.Columns[i] {
			return "column " + a.Columns[i].Name + " differs"
		}
	}
	for r := range a.Data {
		if !reflect.DeepEqual(a.Data[r], b.Data[r]) {
			return "row " + jsonString(r) + " differs"
		}
	}
	return "unknown"
}

func jsonString(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestEnvelopeDifferential(t *testing.T) {
	cases, err := LoadCorpus(casesDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	results := map[string]string{}
	for _, c := range cases {
		if !envelopeGroups[c.Group] {
			continue
		}
		s, d := runEnvelopeCase(t, c)
		results[c.ID()] = s
		t.Logf("%-12s %s — %s", s, c.ID(), d)
	}

	if *acceptEnvelopeFlag {
		writeEnvelopeBaseline(t, results)
		return
	}

	base := readEnvelopeBaseline(t)
	for id, st := range results {
		w := base.Cases[id]
		switch {
		case w == "pass" && st != "pass":
			t.Errorf("ENVELOPE REGRESSION %s: baseline=pass, now=%s", id, st)
		case w != "pass" && st == "pass":
			t.Errorf("ENVELOPE IMPROVEMENT %s now passes — run `make envelope-difftest-accept`", id)
		}
	}
}

func readEnvelopeBaseline(t *testing.T) baselineFile {
	t.Helper()
	raw, err := os.ReadFile(baselineEnvelopePath)
	if err != nil {
		t.Fatalf("read envelope baseline: %v", err)
	}
	var b baselineFile
	_ = json.Unmarshal(raw, &b)
	if b.Cases == nil {
		b.Cases = map[string]string{}
	}
	return b
}

func writeEnvelopeBaseline(t *testing.T, results map[string]string) {
	t.Helper()
	b := baselineFile{Version: 1, Cases: results}
	raw, _ := json.MarshalIndent(b, "", "  ")
	_ = os.WriteFile(baselineEnvelopePath, append(raw, '\n'), 0o644)
}
```

- [ ] **Step 4: 接受初始 envelope baseline**

```bash
./tools/capture-envelope-golden.sh   # 假定 tools/ 同样加 capture-envelope-golden.sh
go test ./internal/difftest/... -run TestEnvelopeDifferential -difftest.accept-envelope
```

- [ ] **Step 5: 提交任务 10**

```bash
git commit -m "test(p4): envelope JSON differential framework + exec_smoke corpus (slice 4)"
```

---

# 切片 5 — HTTP 端点接线

目标：完成 `MDLResource` / `MDLResourceV2` / `DuckDBResource` 三个 endpoint 在 Go 侧的功能完整化。`/v1/mdl/preview`、`/v1/mdl/dry-run`、`/v1/mdl/dry-plan`（含 `modelingOnly=false`）、`/v2/mdl/dry-plan`（含 base64 解码）、`/v1/data-source/duckdb/query`、settings init/session SQL endpoints。

## 任务 11：MDL endpoints + V2 base64 decode

**Files:**
- Edit: `internal/server/mdl_handler.go`
- Create: `internal/server/mdl_handler_test.go`

- [ ] **Step 1: 写失败 endpoint 测试（httptest）**

```go
package server_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/config"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/converter"
	"github.com/wren-engine/wren/internal/server"
	"github.com/wren-engine/wren/internal/service"
)

func TestPreviewEndpoint_Synthetic(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	preview := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	validation := service.NewValidationService()
	h := server.NewMDLHandler(preview, validation)

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"manifest": json.RawMessage(`{"catalog":"wren","schema":"main","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`),
		"sql":      "SELECT 1 AS a",
		"limit":    10,
	})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/mdl/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	cols := got["columns"].([]any)
	if len(cols) != 1 || cols[0].(map[string]any)["name"] != "a" {
		t.Errorf("cols: %+v", cols)
	}
}

func TestDryPlanV2_Base64Manifest(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	preview := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	h := server.NewMDLHandler(preview, service.NewValidationService())

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	manifest := `{"catalog":"wren","schema":"main","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`
	body, _ := json.Marshal(map[string]any{
		"manifestStr": base64.StdEncoding.EncodeToString([]byte(manifest)),
		"sql":         "SELECT 1",
	})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v2/mdl/dry-plan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("dry-plan v2: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	out, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(out), "SELECT") {
		t.Errorf("expected planned SQL, got %q", string(out))
	}
}

func TestDryRunEndpoint_ReturnsColumnsOnly(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	preview := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	h := server.NewMDLHandler(preview, service.NewValidationService())

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"manifest": json.RawMessage(`{"catalog":"wren","schema":"main","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`),
		"sql":      "SELECT 1 AS a, 'x' AS b",
	})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/mdl/dry-run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	var cols []map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&cols)
	if len(cols) != 2 || cols[0]["name"] != "a" || cols[1]["name"] != "b" {
		t.Errorf("cols: %+v", cols)
	}
}

func TestDryPlanEndpoint_ModelingOnlyFalse_GoesThroughConverter(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })

	preview := service.NewPreviewService(md, &converter.DuckDBSqlConverter{}, config.NewConfigManager())
	h := server.NewMDLHandler(preview, service.NewValidationService())

	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"manifest":     json.RawMessage(`{"catalog":"wren","schema":"main","models":[],"relationships":[],"metrics":[],"cumulativeMetrics":[],"enumDefinitions":[],"views":[],"macros":[]}`),
		"sql":          "SELECT COUNT(*) FROM (VALUES (1)) v(a)",
		"modelingOnly": false,
	})
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/mdl/dry-plan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	// modelingOnly=false → should have been lowercased by RewriteFunction.
	if !strings.Contains(string(out), "count(*)") {
		t.Errorf("expected DUCKDB-lowercased function, got %q", string(out))
	}
}
```

- [ ] **Step 2: 实现 V2 base64 decode**

替换 `DryPlanV2` TODO：

```go
func (h *MDLHandler) DryPlanV2(w http.ResponseWriter, r *http.Request) {
	var req DryPlanDtoV2
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	if req.ManifestStr == "" {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "Manifest is required"})
		return
	}
	manifestJSON, err := base64.StdEncoding.DecodeString(req.ManifestStr)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: "base64 decode: " + err.Error()})
		return
	}
	wrenMDL, err := mdl.WrenMDLFromJSON(string(manifestJSON))
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	// V2 dry-plan: Java forces modelingOnly=true regardless of request (see MDLResourceV2.dryPlan).
	result, err := h.previewService.DryPlan(r.Context(), wrenMDL, req.SQL, true)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(result))
}
```

加 `import "encoding/base64"`。

- [ ] **Step 3: 跑测试**

```bash
go test ./internal/server/... -v
```

- [ ] **Step 4: 提交任务 11**

```bash
git commit -m "feat(p4): MDLResourceV2 base64 decode + MDL endpoint tests (slice 5)"
```

## 任务 12：DuckDBResource endpoints + viewenum 信封语料 init.sql

**Files:**
- Edit: `internal/server/duckdb_handler.go`（settings 存储改走 metadata.Metadata）
- Create: `internal/server/duckdb_handler_test.go`
- Create: `testdata/difftest/cases/viewenum/init.sql`
- Edit: `cmd/wren-server/main.go`

- [ ] **Step 1: 改 `internal/server/duckdb_handler.go`：把全局变量 `initSQL`/`sessionSQL` 改走 Metadata**

需要 handler 持 `*duckdb.Metadata` 而非 `connector.Metadata`：

```go
package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/dto"
)

type DuckDBHandler struct {
	metadata *duckdb.Metadata
}

func NewDuckDBHandler(m *duckdb.Metadata) *DuckDBHandler {
	return &DuckDBHandler{metadata: m}
}

func (h *DuckDBHandler) RegisterRoutes(r chi.Router) {
	r.Post("/v1/data-source/duckdb/query", h.Query)
	r.Get("/v1/data-source/duckdb/settings/init-sql", h.GetInitSQL)
	r.Put("/v1/data-source/duckdb/settings/init-sql", h.SetInitSQL)
	r.Patch("/v1/data-source/duckdb/settings/init-sql", h.PatchInitSQL)
	r.Get("/v1/data-source/duckdb/settings/session-sql", h.GetSessionSQL)
	r.Put("/v1/data-source/duckdb/settings/session-sql", h.SetSessionSQL)
	r.Patch("/v1/data-source/duckdb/settings/session-sql", h.PatchSessionSQL)
}

func (h *DuckDBHandler) Query(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	sql := string(body)
	result, err := h.metadata.DirectQuery(r.Context(), sql, nil)
	if err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	defer result.Close()

	cols := result.Columns()
	pcols := make([]dto.PreviewColumn, len(cols))
	for i, c := range cols {
		pcols[i] = dto.PreviewColumn{Name: c.Name, Type: c.Type}
	}
	var data [][]any
	for result.Next() {
		data = append(data, result.Get())
	}
	json.NewEncoder(w).Encode(dto.PreviewResponse{Columns: pcols, Data: data})
}

func (h *DuckDBHandler) GetInitSQL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(h.metadata.InitSQL()))
}

func (h *DuckDBHandler) SetInitSQL(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := h.metadata.SetInitSQL(r.Context(), string(body)); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *DuckDBHandler) PatchInitSQL(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := h.metadata.AppendInitSQL(r.Context(), string(body)); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *DuckDBHandler) GetSessionSQL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(h.metadata.SessionSQL()))
}

func (h *DuckDBHandler) SetSessionSQL(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := h.metadata.SetSessionSQL(r.Context(), string(body)); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *DuckDBHandler) PatchSessionSQL(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if err := h.metadata.AppendSessionSQL(r.Context(), string(body)); err != nil {
		WriteError(w, &WrenError{Code: 65536, Type: GenericUserError, Message: err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 2: 改 `cmd/wren-server/main.go` 注入 Metadata**

```go
md := duckdb.NewMetadata()
// PreviewService.metadata: var metadata service.Metadata = md
// DuckDBHandler:           var duckdbHandler = server.NewDuckDBHandler(md)
```

把 `var metadata service.Metadata = duckdb.NewConnector()` 改为 `md := duckdb.NewMetadata(); var metadata service.Metadata = md`。

- [ ] **Step 3: 写 `duckdb_handler_test.go`**

```go
package server_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wren-engine/wren/internal/connector/duckdb"
	"github.com/wren-engine/wren/internal/server"
)

func TestDuckDBQuery(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })
	h := server.NewDuckDBHandler(md)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/data-source/duckdb/query", "text/plain",
		strings.NewReader("SELECT 1 AS a"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"name":"a"`)) {
		t.Errorf("expected column a, got %s", body)
	}
}

func TestDuckDBInitSQLRoundtrip(t *testing.T) {
	md := duckdb.NewMetadata()
	t.Cleanup(func() { _ = md.Close() })
	h := server.NewDuckDBHandler(md)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// PUT init-sql
	put, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/data-source/duckdb/settings/init-sql",
		strings.NewReader("CREATE TABLE x(a INTEGER); INSERT INTO x VALUES (7);"))
	r2, _ := http.DefaultClient.Do(put)
	r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Fatalf("PUT status: %d", r2.StatusCode)
	}

	// GET init-sql
	get, _ := http.Get(srv.URL + "/v1/data-source/duckdb/settings/init-sql")
	body, _ := io.ReadAll(get.Body)
	get.Body.Close()
	if !bytes.Contains(body, []byte("CREATE TABLE x")) {
		t.Errorf("init SQL not echoed, got %q", body)
	}

	// Query against the table created by init.
	q, _ := http.Post(srv.URL+"/v1/data-source/duckdb/query", "text/plain", strings.NewReader("SELECT a FROM x"))
	qbody, _ := io.ReadAll(q.Body)
	q.Body.Close()
	if !bytes.Contains(qbody, []byte("7")) {
		t.Errorf("expected query result with 7, got %s", qbody)
	}
}
```

- [ ] **Step 4: 写 `cases/viewenum/init.sql` —— 让 envelope 测试有数据可读**

P3c 合成 MDL 模型 `Orders` 引用底层表 `orders`。envelope 测试需要真在 DuckDB 创建 `orders`：

```sql
CREATE TABLE orders (
    o_orderkey   INTEGER,
    o_custkey    INTEGER,
    o_totalprice DOUBLE,
    o_orderdate  DATE,
    o_orderstatus VARCHAR
);
INSERT INTO orders VALUES
    (1, 100, 99.50, '2024-01-15', 'O'),
    (2, 200, 150.00, '2024-02-20', 'F'),
    (3, 100, 75.25, '2024-03-10', 'P');
```

注意：envelope golden capture 时 Java 侧也需对应初始化，捕获 `tools/capture-envelope-golden.sh` 流程时手动 `curl -X PUT http://oracle:18080/v1/data-source/duckdb/settings/init-sql -d @testdata/difftest/cases/viewenum/init.sql`。

- [ ] **Step 5: 跑端点测试 + envelope 差分**

```bash
go test ./internal/server/... -v
go test ./internal/difftest/... -run TestEnvelopeDifferential -v
```

预期：`exec_smoke/*` 全 pass；`viewenum/*` 在 init.sql 准备 ok 后 pass；`viewenum/enum.sql`（数据条件 `Status.O` → `'O'`）按种子数据应返 1 行（orderkey=1）。

- [ ] **Step 6: 提交任务 12**

```bash
git commit -m "feat(p4): DuckDBResource endpoints + viewenum envelope init.sql (slice 5)"
```

---

# 收尾 — 文档、端到端 smoke、验证

## 任务 13：文档 + 全量验证 + P4 closure

**Files:**
- Edit: `README.md`
- Create: `internal/connector/duckdb/README.md`
- Edit: `internal/converter/README.md`（若存在；否则不创建）

- [ ] **Step 1: 在 `README.md` 写 P4 状态**

```markdown
## P4 完成状态（执行层奇偶校验）

- **DUCKDB 方言 SQL 字节差分**：`make duckdb-difftest` —— baseline 见
  `testdata/difftest/baseline-duckdb.json`。
- **响应信封 JSON 结构化差分**：`go test ./internal/difftest -run TestEnvelopeDifferential`
  —— baseline 见 `testdata/difftest/baseline-envelope.json`。
- **执行烟雾**：`cases/exec_smoke/` + `cases/viewenum/` 端到端对真实 in-memory
  DuckDB 跑通 `service.PreviewService.Preview`。

### 重抓 golden（需要 Java oracle）

启动 Java oracle（`docker run -p 18080:8080 ghcr.io/canner/wren-engine:0.9.3`），
然后：

```bash
make capture-golden            # default 方言 (P1/P2/P3 共用)
make capture-duckdb-golden     # DUCKDB 方言 (P4)
./tools/capture-envelope-golden.sh  # 信封 JSON (P4 envelope)
```

接受 baseline 当前结果：

```bash
make difftest-accept
go test ./internal/difftest -run TestDifferentialDuckDB -difftest.accept-duckdb
go test ./internal/difftest -run TestEnvelopeDifferential -difftest.accept-envelope
```
```

- [ ] **Step 2: 跑全量验证**

```bash
gofmt -l .            # 应无输出
CGO_ENABLED=1 go vet ./...
CGO_ENABLED=1 go build ./...
CGO_ENABLED=1 go test ./... -short
```

最后一条若全过即 P4 收官；若有失败需 backtrack 到对应任务。

- [ ] **Step 3: P4 收官检查清单**

逐条勾选确认达成：

- [ ] 1. `go.mod` 含 `github.com/marcboeker/go-duckdb v1.7.0`（精确 pin）。
- [ ] 2. `internal/parser/ast/expression.go` 含 `ArrayConstructor`。
- [ ] 3. `internal/parser/formatter/expression_formatter.go` 含 `formatDoubleDuckDB` + DUCKDB interval + DUCKDB ARRAY type 三个新分支。
- [ ] 4. `internal/converter/{rewrite_array.go,rewrite_function.go,duckdb.go}` 三个文件齐全；`DuckDBSqlConverter.Convert` 顺序：parse → RewriteArray → RewriteFunction → format(DUCKDB)。
- [ ] 5. `internal/connector/duckdb/{connector.go,metadata.go,record_iterator.go}` 三个文件齐全；类型名一律 `strings.ToUpper`。
- [ ] 6. `internal/service/preview.go` 的 `Preview` 真执行；`DryRun` 返 `[]dto.PreviewColumn`；`DryPlan(modelingOnly=false)` 经 sqlConverter。
- [ ] 7. `internal/dto/preview_response.go` 含 `PreviewResponse` + `PreviewColumn`；`service.QueryResultDto` 已删。
- [ ] 8. `internal/server/duckdb_handler.go` 用 `*duckdb.Metadata`（无全局 `initSQL`/`sessionSQL` 变量）。
- [ ] 9. `/v2/mdl/dry-plan` base64 解码已实现。
- [ ] 10. `testdata/difftest/baseline-duckdb.json` 与 `baseline-envelope.json` 已 commit。
- [ ] 11. `tools/capture-duckdb-golden.sh` 与 `cmd/capture-duckdb-golden/` 已 commit；同样 `capture-envelope-golden`。
- [ ] 12. `README.md` 含 CGo 构建说明 + DuckDB 版本声明 + P4 完成状态。
- [ ] 13. `go test ./... -short` 全过；`go vet ./...` 干净；`gofmt -l .` 无输出。

- [ ] **Step 4: 提交收尾**

```bash
git commit -m "docs(p4): README + P4 closure checklist"
```

---

# 附录 A — Java ↔ Go 文件映射

| Java | Go |
|---|---|
| `wren-main/.../connector/duckdb/DuckDBSqlConverter.java` | `internal/converter/duckdb.go` |
| `wren-main/.../sql/duckdb/RewriteArray.java` | `internal/converter/rewrite_array.go` |
| `wren-main/.../sql/duckdb/RewriteFunction.java` | `internal/converter/rewrite_function.go` |
| `wren-main/.../connector/duckdb/DuckDBMetadata.java` | `internal/connector/duckdb/metadata.go` |
| `wren-main/.../connector/duckdb/DuckdbRecordIterator.java` | `internal/connector/duckdb/record_iterator.go` |
| `wren-base/.../client/duckdb/DuckdbClient.java` | `internal/connector/duckdb/connector.go` |
| `wren-base/.../Column.java` | `internal/dto/preview_response.go` (`PreviewColumn`) |
| `wren-main/.../web/dto/QueryResultDto.java` | `internal/dto/preview_response.go` (`PreviewResponse`) |
| `wren-main/PreviewService.java` | `internal/service/preview.go` |
| `wren-main/.../web/MDLResource.java` | `internal/server/mdl_handler.go` |
| `wren-main/.../web/MDLResourceV2.java` | `internal/server/mdl_handler.go::DryPlanV2` |
| `wren-main/.../web/DuckDBResource.java` | `internal/server/duckdb_handler.go` |
| `trino-parser/.../sql/tree/ArrayConstructor.java` | `internal/parser/ast/expression.go::ArrayConstructor` |
| `trino-parser/.../sql/SqlFormatter.java` DUCKDB 分支 | `internal/parser/formatter/formatter.go` (DialectDuckDB 路径) |
| `trino-parser/.../sql/ExpressionFormatter.java` DUCKDB 分支 | `internal/parser/formatter/expression_formatter.go` |

# 附录 B — 不在 P4 范围的功能

- **分析端点 / decisionpoint** —— P5。
- **校验规则 / 配置端点** —— P6（ColumnIsValid 验证规则等）。
- **`WrenSqlRewrite` 动态字段路径** —— P3 已知缺口，需 P6 Jinja 宏层。
- **Java `Hikari` 连接池调优 / `DuckDBConfig` memory_limit / temp_directory** —— Go 用 `database/sql` 标准池，不需要等价配置；spec §2 未要求。
- **`processGenerateTimestampArrayInDuckDB`** —— corpus 不出现，遇到 panic（任务 3）。
- **TPC-H parquet 数据加载 / 全 22 条端到端 envelope 差分** —— 超出 P4 范围，spec §8 只要求 「执行烟雾测试」+「转换层差分」+「信封差分（合成语料）」。
- **`DecimalLiteral` / `TimeLiteral` / `TimestampLiteral` 三个 P2 缺的 AST 节点** —— corpus 不需要，P4 不补。

# 附录 C — P3a / P3b / P3c 产物复用清单

P4 不修改、仅复用：

- `internal/rewrite.Rewrite(sql, ctx, analyzed) (string, error)` —— P3 收官入口。
- `internal/rewrite.RewriteNode(node, hook)` —— P3a 公开的通用树重写器（任务 5/6 复用）。
- `internal/parser.ParseSQL` —— P2 字节对齐 parser。
- `internal/parser/formatter.FormatSQLDialect(stmt, DialectDuckDB)` —— P2 formatter（任务 3 完善 DUCKDB 分支后）。
- `internal/parser/ast` 全套 —— P2 AST（任务 2 补 `ArrayConstructor`）。
- `internal/mdl.{WrenMDLFromManifest,WrenMDLFromJSON,NewAnalyzedMDL}` —— P3a/P3b/P3c 已稳定。
- `internal/dto.Manifest` 全套 + `internal/dto/{model,metric,view,enum,...}.go` —— P3 已稳定。
- `internal/difftest.{LoadCorpus,Normalize}` + 既有 baseline scheme —— P1 框架。
- `cmd/capture-golden/main.go` —— P1 default-dialect golden 捕获工具（P4 不改，新增 `capture-duckdb-golden` / `capture-envelope-golden`）。
- `internal/config.ConfigManager` + `internal/service.ValidationService` —— 既有，P4 不改。
