# go-wren-engine

Go implementation of the Wren analytical engine, parity-matching the Java
`ghcr.io/canner/wren-engine:0.9.3` reference.

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
make duckdb-difftest-accept
go test ./internal/difftest -run TestEnvelopeDifferential -difftest.accept-envelope
```
