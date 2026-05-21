# 设计：P0 修复构建 + P1 差分测试框架

**日期**: 2026-05-19
**状态**: 待评审
**所属**: "Go 引擎 100% 替代 `ghcr.io/canner/wren-engine:0.9.3`" 的第 1 个子项目（共 7 个阶段 P0–P6）

## 1. 背景

当前 `go-wren-engine` 是 Java `wren-engine:0.9.3` 的 Go 重写，目前完成度约 30–40%：HTTP 路由面、DTO、WrenMDL、SQL 解析器与部分改写规则已有骨架，但连接器、查询执行、analysis 子系统均为桩，且**仓库从干净状态无法编译**。

目标是让 Go 引擎完全替代 Java 镜像，验收标准定为**"SQL 文本等价"**：同一份 MDL + SQL 喂给两个引擎，改写结果规范化后逐字一致。该标准要求一套差分测试设施作为客观判据。

整个替代工作拆为 7 个阶段：

| 阶段 | 内容 |
|---|---|
| **P0** | 修复构建，建立 CI |
| **P1** | 差分测试框架（golden 快照） |
| P2 | SQL 解析器 + formatter 字节级对齐 |
| P3 | 改写引擎对齐（全部规则，含 MetricRollup、lineage） |
| P4 | DuckDB 连接器 + 真实执行链路 |
| P5 | Analysis 子系统（decisionpoint 分析器） |
| P6 | Validation、config、jinja、剩余端点收尾 |

本文档只覆盖 **P0 + P1**。P1 依赖 P0（无法编译就无法跑差分测试），故合并为一个子项目、一份实施计划。

## 2. 范围

**在范围内**
- P0：让仓库从干净 checkout 能 `go build ./...` / `go test ./...` 通过，新增 GitHub Actions CI。
- P1：建立 golden-快照差分测试框架——以 Docker 中的 `wren-engine:0.9.3` 为标准答案机（oracle），把改写结果冻结成 golden 文件提交进仓库，Go 侧离线比对，用 baseline 计分板防回归。

**不在范围内**（后续阶段）
- 让差分用例真正通过——这是 P2/P3 的工作。P1 落地时绝大多数用例为 fail，属预期基线。
- 方言转换（DuckDB SqlConverter）的差分比对——P1 只比对 `modelingOnly=true` 的纯语义改写。
- analysis 端点、连接器执行、validation 的差分比对。
- 扩充 view/metric/enum/cumulative 类用例——随 P3 增量补；P1 只种子 TPC-H。

## 3. P0 — 修复构建

### 3.1 已发现的问题

1. `go.mod` 含三条 `replace`，指向不存在的本地目录：
   - `github.com/antlr4-go/antlr/v4 => /tmp/antlr4-go-antlr`
   - `golang.org/x/exp => /tmp/golang-exp`
   - `github.com/go-chi/chi/v5 => /tmp/chi-repo`
2. 无 `go.sum`。
3. 仓库根目录提交了 17MB 的编译产物 `wren-engine`。
4. `Dockerfile` 执行 `COPY go.mod go.sum ./`，但 `go.sum` 不存在。
5. `Makefile` 的 `generate` 目标硬编码本机 java 路径 `/opt/homebrew/Cellar/openjdk/25.0.2/bin/java`。
6. 无 CI。

### 3.2 做法

- 删除 `go.mod` 中三条 `replace` 指令。`github.com/antlr4-go/antlr/v4` 锁到与代码生成器匹配的版本（`tools/antlr-4.13.2-complete.jar` → runtime `v4.13.x`）；`github.com/go-chi/chi/v5` 锁 `v5.2.5`；`golang.org/x/exp` 作为间接依赖交给 `go mod tidy` 解析。
- 运行 `go mod tidy` 生成 `go.sum`。
- 从 git 删除 `wren-engine` 二进制；新增 `.gitignore`（忽略 `bin/`、`wren-engine`、`*.test` 等编译产物）。
- `Makefile` 的 `generate` 目标改用 PATH 中的 `java`。
- 新增 `.github/workflows/ci.yml`：在 push / PR 上依次执行 `go build ./...`、`go vet ./...`、`gofmt -l`（有未格式化文件即失败）、`go test ./...`。Go 版本用 `setup-go` 的 `go-version-file: go.mod`。

### 3.3 风险与缓解

- **(a) 需联网拉依赖**：`go mod tidy` 需访问 module proxy。若实施环境无外网，需先配置代理或预填 module 缓存。
- **(b) antlr4-go runtime 兼容性**：当初使用 `replace` 可能是因为本地 patch 过 antlr4-go runtime。若 `internal/parser/generated/` 下的生成代码无法对官方 release 编译，需用与 runtime 版本匹配的 ANTLR 工具重新 `make generate`。
- **缓解**：实施计划的第一个任务即"删除 replace 后验证 `go build ./...` 通过"，把上述两个风险作为最先暴露、最先解决的点。

### 3.4 P0 验收标准

- 干净 checkout（无 `/tmp/*` 目录、无预存 module 缓存假设）后，`make build` 与 `make test` 均成功。
- `go vet ./...` 无报错；`gofmt -l` 输出为空。
- CI 工作流在 PR 上跑绿。
- 仓库中不再含 `wren-engine` 二进制。

## 4. P1 — 差分测试框架（golden 快照）

### 4.1 数据流

```
testdata/difftest/cases/*           (用例: manifest + sql)
        │
        ├── make capture-golden ──► Docker 跑 wren-engine:0.9.3
        │                           POST /v1/mdl/dry-plan (modelingOnly=true)
        │                                       │
        │                                       ▼
        │                       testdata/difftest/golden/*.sql
        │                       (冻结的 Java 改写输出, 提交进仓库)
        ▼                                       │
internal/difftest 测试: rewrite.Rewrite() ◄─────┘
        │
   token 流规范化  ──►  比对  ──►  对照 baseline.json 计分
```

捕获（`make capture-golden`）与比对（`go test`）解耦：捕获只在本地、且语料变更时手动跑；比对在 CI 离线跑，不需要 Docker。

### 4.2 目录布局

```
go-wren-engine/
├── cmd/capture-golden/main.go          # golden 捕获程序
├── tools/
│   ├── capture-golden.sh               # Docker 生命周期 + 调用捕获程序
│   └── oracle-etc/config.properties    # 挂载给 Java 容器的最小配置
├── internal/difftest/
│   ├── corpus.go                       # 用例语料加载
│   ├── normalize.go                    # SQL token 流规范化
│   ├── normalize_test.go               # 规范化器单测
│   ├── difftest_test.go                # 差分测试主体
│   └── README.md                       # 如何加用例 / 重捕 golden
└── testdata/difftest/
    ├── cases/tpch/
    │   ├── mdl.json                    # TPC-H MDL (取自 wren-tests)
    │   ├── group.json                  # { "modelingOnly": true }
    │   └── queries/1.sql … 22.sql      # 22 条 TPC-H 查询
    ├── golden/tpch/1.sql … 22.sql      # 冻结的 Java 输出
    └── baseline.json                   # 每用例 pass/fail 计分板
```

### 4.3 用例语料

- **用例（case）** 的逻辑标识为 `<group>/<name>`，如 `tpch/1`。
- **语料组（group）** = `cases/<group>/` 目录，含：
  - `mdl.json` —— 该组共用的 MDL manifest。
  - `group.json` —— 组级配置，目前仅 `{ "modelingOnly": true }`。
  - `queries/<name>.sql` —— 每个 `.sql` 文件是一个用例，文件名即 `<name>`。
- `corpus.go` 提供 `LoadCorpus(dir string) ([]Case, error)`，遍历 `cases/*/` 自动发现。`Case` 结构：`{Group, Name, ManifestJSON []byte, SQL string, ModelingOnly bool}`。
- **P1 种子语料**：TPC-H 组——`tpch_mdl.json` 与 22 条查询从 `wren-engine-0.9.3/wren-tests/src/test/resources/` 拷入。用例格式写入 `README.md`；view/metric/enum/cumulative 类用例随 P3 增量补。

### 4.4 golden 捕获

`make capture-golden` → `tools/capture-golden.sh`：

1. `docker run -d` 启动 `ghcr.io/canner/wren-engine:0.9.3`，端口映射到本机 `18080`，挂载 `tools/oracle-etc/` 作为 `etc/`（提供最小 `config.properties`，否则 Java 服务因缺配置无法启动）。用 `trap` 保证退出时 `docker rm -f`。
2. 轮询 `http://localhost:18080/v1/config` 直至返回 200（健康检查），最多等约 120s；超时则报错退出。
3. 运行 `cmd/capture-golden`：参数 `-addr`（oracle 地址）、`-cases`（语料目录）、`-out`（golden 输出目录）。程序遍历语料，对每个用例以请求体 `{manifest, sql, modelingOnly}` 调 `GET /v1/mdl/dry-plan`：
   - HTTP 2xx → 把响应体（纯文本改写 SQL）写入 `golden/<group>/<name>.sql`。
   - HTTP 错误 → 把响应写入 `golden/<group>/<name>.sql.error`，标记该用例为 `oracle-error`。
4. 销毁容器。

golden 存**原始** Java 输出（不预先规范化），便于在 git diff 中人眼审查；规范化在比对时进行，规范化逻辑变更无需重新捕获。

### 4.5 SQL 规范化器

`internal/difftest/normalize.go` 提供 `Normalize(sql string) ([]string, error)`，产出规范化 token 序列：

1. 用 `generated.NewSqlBaseLexer` 配合大小写不敏感输入流（复用 `internal/parser` 已有的 `caseInsensitiveStream`）对 SQL 取全部 token。
2. 丢弃空白与注释 channel 的 token。
3. 逐 token 规范化文本：
   - 关键字、非引号标识符 → 转大写。
   - 引号标识符（`"..."`）→ 保留内部文本原样，统一为 `"..."` 形式。
   - 字符串字面量（`'...'`）→ 原样保留，不改大小写。
   - 数字字面量 → 原样保留。
4. 返回规范化后的 `[]string`。

两段 SQL"等价" ⇔ 规范化 token 序列完全相等。

**选 token 流而非 AST 比对的理由**：AST 比对依赖 Go parser 自身正确，parser 有缺陷时会掩盖真实差异；token 流只依赖 lexer，更可靠。lexer 缺陷会在 P2 单独处理。

### 4.6 差分测试主体

`internal/difftest/difftest_test.go`（标准 `go test`，CI 中运行）：

对每个用例：
1. 读 golden（`golden/<group>/<name>.sql`）作为预期。若存在 `.sql.error` → 该用例归类 `oracle-error`，跳过比对。
2. 构造 Go 侧输入：`mdl.WrenMDLFromManifest(manifest)` → `mdl.NewAnalyzedMDL(...)`，SessionContext 取 MDL 的 catalog/schema，调 `rewrite.Rewrite(sql, ctx, analyzedMDL)` 取实际输出。
   - Go 侧 panic 或返回 error → 该用例归类 `go-error`。
3. 对预期与实际分别 `Normalize`。Go parser 无法解析 Java 的 golden → 归类 `parser-gap`。
4. token 序列相等 → 用例 `pass`；否则 `fail`，打印可读 diff（首个差异 token 的位置与上下文）。

失败分类：`fail`（token 不一致）、`parser-gap`、`go-error`、`oracle-error`。后三类在报告中单列，它们本身是 P2 的输入。

### 4.7 baseline 计分板与防回归

`testdata/difftest/baseline.json`：
```json
{
  "version": 1,
  "cases": {
    "tpch/1": "fail",
    "tpch/2": "fail"
  }
}
```

差分测试对照 baseline 判定：

| baseline 记录 | 当前结果 | 测试判定 |
|---|---|---|
| `pass` | `pass` | 通过 |
| `pass` | 非 `pass` | **失败（回归）** |
| `fail` 等 | 非 `pass` | 通过（仅 `t.Log` 记录） |
| `fail` 等 | `pass` | **失败**，提示运行 `make difftest-accept` 更新 baseline |

效果：P1 落地时 baseline 把全部用例记为 `fail`，CI 即为绿（基线即现状）；P2/P3 推进时用例从 fail 翻 pass，开发者用 `make difftest-accept` 把新基线写回；任何 pass→fail 的回归会立即让 CI 变红。

`make difftest-accept`：以当前一轮差分结果重写 `baseline.json`。

### 4.8 Makefile 目标

- `capture-golden` —— 运行 `tools/capture-golden.sh`（需 Docker）。
- `difftest` —— `go test ./internal/difftest/...`，并打印计分板摘要（`N/M 通过`，失败按分类汇总）。
- `difftest-accept` —— 以当前结果重写 `baseline.json`。

### 4.9 错误处理

- 容器启动失败 / 健康检查超时 → `capture-golden.sh` 以非零码退出并打印诊断（容器日志末尾）。
- oracle 对某用例返回 HTTP 错误 → golden 记 `.sql.error`，用例归 `oracle-error`，排除出比对。
- Go parser 解析不了 golden → 用例归 `parser-gap`，单列。
- Go 改写 panic → `difftest_test.go` 用 `recover` 捕获，用例归 `go-error`，不中断整轮测试。

### 4.10 测试

- `normalize_test.go`：喂成对 SQL（等价对 / 不等价对）断言规范化判定正确——覆盖空白差异、关键字大小写、引号标识符、字符串字面量大小写敏感。
- 捕获程序：以 1–2 个用例做冒烟（需本地 Docker，不进 CI）。
- 差分测试：在 CI 离线运行；`capture-golden` 仅本地 / 语料变更时手动运行。

## 5. 交付物清单

**P0**
- 修复后的 `go.mod` / 新增 `go.sum`
- `.gitignore`；从 git 移除 `wren-engine` 二进制
- 修正 `Makefile` 的 `generate` 目标
- `.github/workflows/ci.yml`

**P1**
- `internal/difftest/{corpus.go, normalize.go, normalize_test.go, difftest_test.go, README.md}`
- `cmd/capture-golden/main.go`
- `tools/capture-golden.sh`、`tools/oracle-etc/config.properties`
- `Makefile` 新增目标 `capture-golden` / `difftest` / `difftest-accept`
- `testdata/difftest/cases/tpch/`（mdl.json + group.json + 22 条查询）
- `testdata/difftest/golden/tpch/`（22 个冻结 golden）
- `testdata/difftest/baseline.json`（全部记为 `fail` 的初始基线）

## 6. 总体验收标准

- 干净 checkout 后 `make build`、`make test` 通过，CI 绿。
- `make capture-golden` 能在装有 Docker 的机器上跑通，产出 22 个 TPC-H golden 文件。
- `make difftest` 离线运行通过：用例对照 baseline 无回归；计分板摘要正确反映通过/失败分布。
- 规范化器单测通过。
- 人为制造一处回归（把某用例 baseline 改成 `pass`）能让 `make difftest` 失败——验证防回归机制生效。
