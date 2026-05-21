# Phase 6 设计：Postgres wire 协议 + Jinjava 全引擎（条件触发的可选阶段）

> 子项目：Phase 6（drop-in replacement 路线图 5 阶段之后的**可选**阶段）。
> 前置：Phase 5 已完成（drop-in 收官）。
> 与 Phase 1-5 关系：**两个子系统都不是 WrenAI 0.9.0 drop-in 的阻塞项**。Phase 6 是条件触发的
> 可选阶段，仅当生产环境真实需要其中任一子系统时启动。
> 目标：(A) 实现 Postgres wire 协议（端口 7432）以支持外部 SQL 客户端；
> (B) 用真 Jinja 引擎替换 Phase 6 的正则替换，支持完整 Jinja2 语法。

## 1. 背景

brainstorming 阶段把 PG wire（端口 7432）与 Jinjava 列为 drop-in gap。Phase 6 spec 调研阶段
对**两者实际被消费情况**做了证据收集，结论为：

### 子系统 A — Postgres wire (port 7432) 的真实消费

| 调研点 | 结论 |
|---|---|
| WrenAI ai-service (`grep '7432'`) | **0 命中**；ai-service 仅经 HTTP 8080 调 wren-engine |
| WrenAI wren-ui `server/` 代码 (`grep '7432'`) | **0 命中**；`pg` npm 依赖用于连接 WrenAI **自己的** Postgres 数据库（存项目元数据），与 wren-engine 无关 |
| ibis-server (`grep '7432'`) | **0 命中** |
| `docker-compose.yaml` `expose: [8080, 7432]` | 仅 K8s/Compose **暴露**端口，**无内部消费者**；为外部 BI 工具 / SQL workbench 预留 |
| Java wren-engine-0.9.3 自身 wire 实现 (`find -path '*wireprotocol*'`) | **找不到实现类**；仅有 `PostgresWireProtocolConfig.java` 配置类。0.9.3 分支可能**已抽离 wire impl** 或本就**未实现完整** |

**结论 A**：PG wire 在 WrenAI 0.9.0 默认部署下是**未消费的悬挂端口**。Java 自己 0.9.3 实现都
不完整。drop-in 不需要它。**默认状态下 Phase 6a 不应启动**。

### 子系统 B — Jinjava 引擎的真实消费

| 调研点 | 结论 |
|---|---|
| WrenAI demo MDL macro 数 | college_3_bigquery=0、ecommerce_duckdb=0、ecommerce2_duckdb=**1**、music_duckdb=0、nba_duckdb=0、pagila_postgres=0 |
| ecommerce2 那 1 个 macro 的复杂度 | `MoM` 月环比，定义 `(calc, ts) => (calc - LAG(calc) OVER ...) / COALESCE(LAG(calc), 1)`；同一参数被多次引用 + 命名参数；**当前 Go 正则替换已支持** |
| Java Jinjava 高级功能（`{% if %}` / `{% for %}` / filter / include）在 WrenAI 样例 | **0 使用** |
| `internal/mdl/jinja.go` 现状 | 正则替换 + 位置参数 + 命名参数替换；P6 spec 已记入 honest deviation |

**结论 B**：WrenAI 默认部署的 MDL **不使用** Jinjava 高级功能；现 Go 正则替换覆盖 100% 真实样例。
drop-in 不需要 full Jinjava。**默认状态下 Phase 6b 不应启动**。

### Phase 6 的真实定位

Phase 5 spec §9 把 Phase 6 列为「触发条件：端到端 smoke 发现 ai-service 调用 7432 / 复杂 macro MDL」。
本 spec 完成上述调研后**降级 Phase 6 为「条件触发可选阶段」**：

- drop-in 5 阶段路线图（Phase 1-5）完成 = WrenAI 0.9.0 drop-in **完成**
- Phase 6 仅在以下情况启动：
  - 6a 触发条件：用户接入了非 WrenAI 的外部 SQL 客户端（DBeaver / psql / Tableau 等）需通过 7432 查询 wren-engine
  - 6b 触发条件：用户的自定义 MDL 用了 `{% if %}` / `{% for %}` / Jinja filter（regex 替换不再够）

如果两条都不触发，Phase 6 **可永久搁置**，drop-in 路线图就此收官。

## 2. 范围

### Phase 6a — Postgres wire protocol（IN，条件触发）

- 新包 `internal/wireprotocol/`：
  - `Server` 结构：监听 `:7432`，accept TCP，每连接 goroutine 处理
  - `Session` 结构：StartupMessage 解析 → 简化版鉴权（明文允许 / 拒绝） →
    SimpleQuery 处理 → ReadyForQuery
  - 用 `github.com/jackc/pgproto3/v2`（v3 是 jackc/pgx 主线）做 wire frame 编解码
- SQL 路由：PG wire 收到的 `SELECT ...` → 走与 HTTP `/v1/data-source/duckdb/query` 同一路径（DuckDB
  metadata.DirectQuery） → 结果按 PG `DataRow` 帧回传
- 列类型映射：DuckDB column type → PG OID（int4=23, varchar=1043, float8=701, date=1082, timestamp=1114
  等基本 7-10 种）
- 支持范围（**最小子集**，drop-in 触发后扩）：
  - StartupMessage / SSLRequest（响应 N 拒绝 SSL，明文继续）
  - AuthenticationOk（无密码）
  - ParameterStatus（server_version, client_encoding, DateStyle, TimeZone）
  - SimpleQuery: Parse + Bind + Execute + ReadyForQuery 链
  - ErrorResponse + ReadyForQuery on error
- **OUT-of-scope** (即使触发 Phase 6a 也不做)：
  - Extended Query Protocol（Parse/Bind/Execute 复杂流，prepared statement）
  - 二进制 row format（仅 Text format 输出）
  - 真实 PG auth（md5、SCRAM-SHA-256）
  - Cursor / Portal 多次 Fetch
  - SSL/TLS（response N forever）
  - Notification / LISTEN/NOTIFY

### Phase 6b — Jinjava 全引擎（IN，条件触发）

- 引入 `github.com/nikolalohinski/gonja/v2`（MIT，Jinja2 兼容，活跃维护）
- 重写 `internal/mdl/jinja.go`：
  - `RenderJinja(manifest *dto.Manifest) *dto.Manifest` 接口签名**不变**
  - 内部把每个 model column expression 喂给 gonja，传入 macros 作为 `{% macro %}` block
  - macros 序列化为 Jinja `{% macro Name(args) %}body{% endmacro %}` 拼接到 template prefix
    （等价 Java `JinjavaUtils.getMacroTag`）
  - 单元测试沿用现有 `jinja_test.go` + 新增高级用例（if / for / filter）
- 保留 Phase 6 正则实现作为 fallback：`RenderJinja` 入参可加 `mode` 区分；默认 `mode=gonja`，环境
  变量 `WREN_JINJA_MODE=regex` 可切回（safe rollback）

### OUT（不在 Phase 6 任何子系统）

- HTTP API 表面修改（与 Phase 1-5 边界一致）
- 性能优化（Phase 7 范围）
- 0.9.3 → 0.11.1 版本升级（Phase 8 范围）
- Phase 5 的 dynamic-field 分支修改
- WrenAI compose drop-in 验证（Phase 5 已完成）

## 3. 架构与数据流

### Phase 6a 数据流

```
psql -h localhost -p 7432 -U wren -d wrenai
  ↓ TCP connect
internal/wireprotocol/Server accept
  ↓ goroutine per session
internal/wireprotocol/Session:
  Read StartupMessage{user, database}
  Write AuthenticationOk (no password)
  Write ParameterStatus × N
  Write ReadyForQuery 'I'
  loop:
    Read Query{sql}
    ↓ route: same as POST /v1/data-source/duckdb/query
    duckdbMetadata.DirectQuery(ctx, sql, nil)
    ↓ iterate
    Write RowDescription{columns mapped to PG OIDs}
    for row: Write DataRow{values as Text format}
    Write CommandComplete + ReadyForQuery 'I'
```

**重要：Phase 6a 的 SQL 路径与 HTTP `/v1/data-source/duckdb/query` 共用** —— PG wire 不重做
rewrite chain（不需要 Wren MDL 语义），它只是 DuckDB 的 PG-protocol 前端。若用户希望 MDL-aware
查询，仍走 HTTP `/v1/mdl/preview`。这与 Java 行为一致（Java PG wire 也只是 DuckDB SQL 入口）。

### Phase 6b 数据流

```
manifest.Macros [...] + model.column.expression "{{ MoM(revenue, date) }}"
  ↓ RenderJinja(manifest)
  buildTemplate := strings.Builder
    for each macro:
      write "{% macro Name(args) %}body{% endmacro %}\n"
    write expression  // "{{ MoM(revenue, date) }}"
  template := gonja.FromString(buildTemplate.String())
  rendered := template.Execute(map[string]any{})  // no context vars
  ↓ replace column.expression with rendered
  return manifest
```

gonja 处理 `{% macro %}` 定义 + `{{ macro(args) }}` 调用 + 嵌套表达式 + filter。

## 4. 组件与文件结构

### Phase 6a 文件

| 文件 | 操作 | 大小估算 |
|---|---|---|
| `internal/wireprotocol/server.go` | 新建 | ~150 行 (Listen + accept loop) |
| `internal/wireprotocol/session.go` | 新建 | ~300 行 (handshake + query loop) |
| `internal/wireprotocol/typemap.go` | 新建 | ~80 行 (DuckDB type ↔ PG OID 映射表) |
| `internal/wireprotocol/server_test.go` | 新建 | ~200 行 (用 `github.com/jackc/pgx/v5` 作为客户端跑 integration) |
| `cmd/wren-engine/main.go` | 修改 | +30 行 (启 wire protocol server 在独立 goroutine) |
| `internal/config/config.go` | 修改 | +10 行 (新增 `wren.postgres-wire.port` 等 config key，默认 7432) |
| `docker/Dockerfile`（Phase 1 资产） | 修改 | +1 行 EXPOSE 7432（可选，Java image 也没 EXPOSE） |
| `go.mod` | 修改 | +1 dep `github.com/jackc/pgproto3/v2` |

### Phase 6b 文件

| 文件 | 操作 | 大小估算 |
|---|---|---|
| `internal/mdl/jinja.go` | 重写 | ~120 行 (gonja 接线，保留 fallback 入口) |
| `internal/mdl/jinja_regex_fallback.go` | 新建 | ~80 行（Phase 6 原实现 move 到这里） |
| `internal/mdl/jinja_test.go` | 扩 | +150 行（高级语法 test cases） |
| `go.mod` | 修改 | +1 dep `github.com/nikolalohinski/gonja/v2` |

### Phase 6 共同

| 文件 | 操作 | 说明 |
|---|---|---|
| `docs/phase6-trigger-criteria.md` | 新建 | 文档化何时启动 Phase 6a / 6b；提供调研复现命令 |
| `docs/wire-protocol-supported-features.md` | 新建（仅 6a 启动时） | PG wire 支持/未支持特性清单 |

## 5. 实施切片（方案 C：增量切片）

Phase 6a 与 Phase 6b **独立**，可分别独立启动。下面按子项给出切片。

### Phase 6a 切片（仅在外部 SQL 客户端需求触发时启动）

#### 切片 1 — wire frame 编解码 + handshake

- 引入 `github.com/jackc/pgproto3/v2`
- `internal/wireprotocol/server.go`：Listen `:7432`，accept loop，per-conn goroutine
- `internal/wireprotocol/session.go`：StartupMessage / SSLRequest / AuthenticationOk /
  ParameterStatus / ReadyForQuery
- 集成测试：用 `github.com/jackc/pgx/v5` 起 client `pgx.Connect("postgres://localhost:7432/wrenai")`，
  握手成功即 pass

#### 切片 2 — SimpleQuery + DuckDB 路由

- Session 处理 Query{sql} → 调 `duckdb.NewMetadata().DirectQuery(ctx, sql, nil)`
- 列类型 → PG OID 映射表 (`typemap.go`)
- 行数据按 PG Text format 序列化（数字转字符串、日期 `YYYY-MM-DD`、null 用 -1 length）
- 集成测试：pgx `conn.Query("SELECT 1, 'hello', CURRENT_DATE")` → 三列正确

#### 切片 3 — 错误响应 + 完整 smoke

- ErrorResponse 帧：DuckDB error → ErrorResponse{Severity:"ERROR", Code:"42000", Message:...} +
  ReadyForQuery 'I' 不断连接
- 端到端 smoke：`psql -h localhost -p 7432 -U wren -d wrenai` 起会话跑 5 个查询 → 全成功
- commit：`feat(p6a): Postgres wire protocol minimum subset (SimpleQuery + DuckDB)`

#### 切片 4 — 文档 + drop-in 集成

- `docs/wire-protocol-supported-features.md`
- Dockerfile `EXPOSE 7432`
- main.go 加 wire server 启动（goroutine + graceful shutdown）
- `WREN_POSTGRES_WIRE_ENABLED=true` 环境变量控制（默认 true 与 Java 一致）

### Phase 6b 切片（仅在用户 MDL 使用 Jinja 高级语法时启动）

#### 切片 1 — gonja v2 集成

- 引入 `github.com/nikolalohinski/gonja/v2`
- 把 Phase 6 现 `internal/mdl/jinja.go` 内容**整体 move 到** `jinja_regex_fallback.go`
- 重写 `internal/mdl/jinja.go`：
  ```
  RenderJinja(manifest):
      mode := os.Getenv("WREN_JINJA_MODE")  // 默认 gonja
      if mode == "regex": return renderJinjaRegex(manifest)
      return renderJinjaGonja(manifest)
  ```
- `renderJinjaGonja`：用 `gonja.FromString` + macros prefix

#### 切片 2 — 测试覆盖 Jinja 高级语法

- `jinja_test.go` 新增 cases：
  - `{% if condition %}A{% else %}B{% endif %}`
  - `{% for item in list %}item{% endfor %}`
  - `{{ value | upper }}` filter
  - 嵌套 macro 调用 `{{ outer(inner(x)) }}`
- 同时保持现有 5 个 case 在 `WREN_JINJA_MODE=regex` 下仍 pass

#### 切片 3 — 文档 + WrenAI compose 验证

- `docs/phase6-trigger-criteria.md` 更新「Phase 6b 启动」节
- 把 ecommerce2 MDL 的 `MoM` macro 作为 fixture 测试 gonja 输出与 regex 等价（双 mode parity）
- commit：`feat(p6b): full Jinja2 engine via gonja with regex fallback`

## 6. 风险登记表

| # | 风险 | 子项 | 说明 |
|---|---|---|---|
| 1 | **PG wire 实现复杂度爆炸** | 6a | 真实 PG client（pgx / psycopg）可能发 ExtendedQuery，需 Parse/Bind/Execute 三段式。**化解**：切片 2 限定 SimpleQuery；ExtendedQuery 触发时返回 ErrorResponse「not supported」，让 client fallback 到 SimpleQuery 或换工具 |
| 2 | **DuckDB type → PG OID 映射不完整** | 6a | DuckDB 有 LIST/STRUCT/MAP 等非标准类型，PG 没有对应 OID。**化解**：未知类型一律映射到 `text` (OID 25)；记入 supported-features 文档 |
| 3 | **goroutine 泄漏 / 拒绝服务** | 6a | 每连接 goroutine 无限制。**化解**：用 `golang.org/x/sync/semaphore` 限制最大 ~100 并发连接；超过返回 ErrorResponse 立即关闭 |
| 4 | **gonja v2 与 Java Jinjava 行为不字节等价** | 6b | gonja 是独立实现，与 Jinjava 在 corner case（whitespace 处理 / 错误行为 / autoescape）可能差异。**化解**：保留 regex fallback；新增 `--difftest.jinja` 跑 WrenAI 真实 MDL 双 mode 输出对比 |
| 5 | **gonja 依赖体积** | 6b | gonja v2 + 间接依赖可能加 ~500KB binary。**化解**：可接受；image size 在 Phase 1 已宽松 ~150MB 上限 |
| 6 | **Phase 6a 触发但 Java 0.9.3 自身无实现可对照** | 6a | 调研发现 Java 0.9.3 没有 wire impl 类文件。**化解**：6a 启动时改用 **0.11.1** Java image 作为 oracle 对照（如有），或仅做 protocol-level smoke（不做 byte-equal 差分） |
| 7 | **wire protocol 安全风险** | 6a | 暴露 DuckDB 直接查询入口 = SQL injection 风险。**化解**：drop-in 场景 wren-engine 已经在内网，端口 7432 不应对公网暴露；docker-compose 默认 `expose:` 而非 `ports:` 即内网 only |
| 8 | **6b 后正则模式逐渐 rot** | 6b | regex fallback 长期没人维护可能 break。**化解**：CI 跑两个 mode 的相同 test set；fallback 保 6 个月，无回归后删除 |

## 7. 错误处理

- **6a wire server 启动 :7432 失败**（端口占用）：log warning，继续启 HTTP server；不 fatal
- **6a session 读到未知 message type**：写 ErrorResponse{XX000, "protocol error"} + close
- **6a SQL 执行失败**：DuckDB error → PG ErrorResponse + ReadyForQuery 不断开
- **6b gonja 解析失败**（语法错）：返回 error，handler 写 ErrorMessageDto 报错
- **6b gonja 运行时 panic**：recover + 转 error；不让模板错误打挂整服务
- **6b 同时启用 regex + gonja 模式冲突**：env 优先 regex；冲突时 log warning

## 8. 测试与验收

### Phase 6a 验收

- ✅ `pgx.Connect("postgres://wren@localhost:7432/wrenai")` 成功
- ✅ `conn.Query("SELECT 1")` 返回 1 行 1 列值 = 1
- ✅ `conn.Query("SELECT name FROM wrenai.tpch_tiny.Customer LIMIT 5")` 返回 5 行
- ✅ DuckDB error 通过 PG ErrorResponse 正确传递
- ✅ `psql -h localhost -p 7432 -U wren -d wrenai -c "SELECT version()"` 正常返回
- ✅ 并发 50 个 pgx 连接同时跑查询，5 分钟无 leak / crash
- ✅ `docs/wire-protocol-supported-features.md` 列出支持 / 未支持特性

### Phase 6b 验收

- ✅ `internal/mdl/jinja_test.go` 现有 5 个 case 在 gonja mode 下**全 pass**（与 regex mode 等价）
- ✅ 新增 4 个高级语法 case 在 gonja mode 下 pass
- ✅ `WREN_JINJA_MODE=regex` 切回 fallback；现有 5 case 仍 pass
- ✅ ecommerce2 demo MDL 的 MoM macro 经 RenderJinja 后输出与 Java JinjavaExpressionProcessor 输出
  字节等价（用 `tools/oracle-up.sh` 启 Java oracle，POST MDL → 抓 `/v1/mdl/dry-plan` 返回 SQL 比对）
- ✅ `go test ./internal/mdl/... -race` clean
- ✅ `docs/phase6-trigger-criteria.md` 更新

### 共同验收

- ✅ `go build ./...` + `go vet ./...` + `gofmt -l .` 全 clean
- ✅ Phase 1-5 所有 baseline 0 regression
- ✅ `make image-test` 镜像 size ≤ 150 MB（gonja 加进去后仍达标）

## 9. 与其他阶段的关系

- **可选阶段**：Phase 1-5 完成 = WrenAI 0.9.0 drop-in 完成；Phase 6 是"上限拉满"
- **依赖 P0–P5**：6a 共享 DuckDB metadata 与 SessionContext；6b 在 MDL pipeline 中替换 jinja 实现
- **触发后再启动**：Phase 6 不主动开工，只在以下信号触发时考虑：
  - 6a 触发：用户提需求接外部 BI 工具 / psql / 自动化 SQL 测试通过 7432 访问
  - 6b 触发：用户 MDL 使用 `{% if %}` / `{% for %}` / filter 等高级语法（regex 不再够）
- **铺垫 Phase 7/8**：6a wire impl 是 Phase 7 性能优化 + Phase 8 0.11.1 升级时的潜在重写点

### Drop-in gap 登记表（Phase 6 后状态）

| Gap | Phase 5 后 | Phase 6 后（如全部启动） |
|---|---|---|
| `WrenSqlRewrite` dynamic-field 分支 | ✅ 闭合 (Phase 5) | ✅ |
| WrenAI compose drop-in 替换 | ✅ 闭合 (Phase 5) | ✅ |
| Postgres wire protocol (7432) | ❌ 仍缺但**不阻塞 drop-in** | ✅（6a 启动时） |
| jinja 高级语法 | ⚠ regex 替换（WrenAI 默认 MDL 不用高级语法） | ✅（6b 启动时） |
| 0.11.1 升级 | ❌ 未审计 | ❌ 仍未审计（Phase 8） |
| 性能 / mem 调优 | ❌ 未做 | ❌ 仍未做（Phase 7） |

### 后续 Phase 候选

| Phase | 内容 | 触发条件 |
|---|---|---|
| Phase 7 | 性能 / mem / GOMEMLIMIT 调优 + 并发优化 | drop-in 后 perf 不达标 |
| Phase 8 | 0.9.3 → 0.11.1 升级 + HTTP API 兼容性审计 | WrenAI 升级到引用 0.11.1+ image，或 0.9.3 Java image 弃用 |

---

- 本 spec 完成后**不自动**进入 `gpowers:writing-plans`。是否产出实施计划取决于触发条件是否成立。
- **关键判断**：在跑完 Phase 1-5 + Phase 5 切片 5 端到端 smoke 后，根据 ai-service 实际调用日志 +
  用户使用场景再决定 Phase 6a / 6b 是否上线。**默认建议：搁置**。
