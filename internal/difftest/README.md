# 差分测试（difftest）

以 Java `wren-engine:0.9.3` 为标准答案机，验证 Go 改写引擎的输出与之
"SQL 文本等价"（规范化为 token 序列后逐字一致）。

## 工作方式

1. `testdata/difftest/cases/<group>/` 下放用例：`mdl.json` + `group.json`
   + `queries/<name>.sql`。
2. `make capture-golden` 用 Docker 跑一次 Java 引擎，把每个用例的改写结果
   冻结到 `testdata/difftest/golden/<group>/<name>.sql`（提交进仓库）。
3. `make difftest` 离线运行：Go 侧 `rewrite.Rewrite()` 的输出与 golden
   规范化后比对，结果对照 `baseline.json` 计分板防回归。

## 常用命令

- `make difftest` —— 运行差分测试（CI 默认，不需 Docker）。
- `make capture-golden` —— 重新捕获 golden（需 Docker；语料变更时运行）。
- `make difftest-accept` —— 把当前结果写回 `baseline.json`
  （改写引擎取得进展、用例从 fail 翻 pass 后运行）。

## 新增用例

- 同一 MDL 下的新查询：往对应 group 的 `queries/` 加 `.sql` 文件。
- 新 MDL：在 `cases/` 下建新 group 目录，含 `mdl.json`、`group.json`
  （`{"modelingOnly": true}`）、`queries/`。
- 之后运行 `make capture-golden` 与 `make difftest-accept`。

## 语料组

- `tpch/` —— TPC-H 标准 22 条查询 + P3a 模型查询 (`m_*.sql`) + P3b 度量查询
  (`met_*.sql`)。`met_*` 因传递性依赖含 Jinja 列的 `Customer` 模型，在 P3b 阶段为
  已知 `go-error`（待 P6 Jinja 宏层）。
- `metric/` —— P3b 合成度量语料组（无 Jinja 小 MDL），含 metric-on-model /
  metric-on-metric / cumulative metric / rollup，为 P3b 端到端字节奇偶证据。

## 失败分类

- `fail` —— token 序列与 Java 不一致（改写逻辑差异，P2/P3 的修复目标）。
- `parser-gap` —— Go lexer 无法切分某段 SQL（P2 的修复目标）。
- `go-error` —— Go 改写返回错误或 panic。
- `oracle-error` —— Java 引擎对该用例本身报错，已排除出比对。
- `no-golden` —— 缺 golden 文件，需运行 `make capture-golden`。
