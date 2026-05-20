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
