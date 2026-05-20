# Phase 3 设计：golden 补齐 + baseline 量化 + Java oracle 可复现化

> 子项目：Phase 3（drop-in replacement 路线图 5 阶段中的第 3 阶段）。
> 前置：P0–P6 + Phase 1（Dockerfile）+ Phase 2（config.properties 解析）。
> 目标：把 4 个差分 baseline 中所有 `no-golden` 与 `oracle-error` 转化为
> `pass`/`fail`/`oracle-error-permanent`，产出量化「Go 对 wren-engine:0.9.3 静态分支的覆盖率」。

## 1. 背景

brainstorming 阶段假设 "TPC-H 21/22 oracle-error 未验证" —— **此假设已过时**。
现场调研当前 4 个 baseline 真实状态：

| Baseline | 总数 | pass | go-error | oracle-error | no-golden |
|---|---|---|---|---|---|
| `baseline.json`（rewrite，dry-plan modelingOnly=group-config） | 47 | 35 | 7 | 2 | 0 |
| `baseline-envelope.json`（preview JSON envelope） | 8 | 0 | 0 | 0 | **8** |
| `baseline-duckdb.json`（DuckDB-dialect dry-plan modelingOnly=false） | 47 | 0 | 0 | 0 | **47** |
| `baseline-config.json` / `baseline-validation.json` / `baseline-analysis.json` | — | 完整 | — | — | — |

真正待办：

1. **`tpch/1` + `tpch/4` 是 Java oracle 自己挂**：error 文件含
   `java.lang.IllegalArgumentException: count(*) should have a followed source` —— Java 0.9.3
   原版本对裸 `count(*)` 的 source 跟踪 bug，**永久 oracle-error**，需新增标记
   `oracle-error-permanent` 把它们从 pass-rate 分母里剔除
2. **7 个 `go-error`**（`tpch/m_orders`、5 个 `met_*`、4 个 `v_use_*`）是 Go 自身在 rewrite 层挂掉
   —— **Phase 4 范围**，Phase 3 仅清单化交付 Phase 4
3. **`golden-envelope/` + `golden-duckdb/` 几乎全空**：8 + 47 = 55 个 case 从未对 Java oracle 跑过
4. **Java oracle 启动方式没有可复现化**：每次手敲 `docker run ...` 加一堆 mount，没有
   `tools/oracle-up.sh` / Makefile target

**奇偶校验线（Phase 3）**：① envelope + duckdb-dialect 两个 baseline 不留 `no-golden`；
② `oracle-error-permanent` 状态加入 difftest framework；③ 「跑一次 Java oracle」可以一键，
脚本化 + 文档化；④ 输出量化 scoreboard。

Phase 3 **不修任何 Go 源码 bug**，纯 test-data + tooling。`go-error` 列表交 Phase 4。

## 2. 范围

### IN（Phase 3 负责）

- `tools/oracle-up.sh`：一键启 Java oracle 容器（`ghcr.io/canner/wren-engine:0.9.3` + 挂载
  `wren-engine-0.9.3/example/duckdb-tpch-example/etc/`）+ wait-for-ready 探针
- `tools/oracle-down.sh`：清理容器与卷
- Makefile targets：`make oracle-up` / `make oracle-down` / `make capture-all-golden`
- 改造 4 个 `cmd/capture-*` 工具：
  - 统一 `--addr` / `--cases` / `--out` flag
  - 加 `--groups` 过滤（envelope/analysis 已有）
  - 加 `--timeout` per-request（默认 60s）
  - 加 `--retry`（默认 0；transient 失败可手动调）
  - 输出尾部一行 `summary: ok=X errs=Y total=Z`
- 全量 capture：跑 `make capture-all-golden` 写入 `golden/` + `golden-envelope/` + `golden-duckdb/`
- `internal/difftest`：新增 `oracle-error-permanent` 状态，对应 case 在 difftest 跑测时不计入 fail
- baseline 自动 rotate：`make rebaseline` 跑 `go test ./internal/difftest/... -difftest.accept*`
  四个 flag，备份原 baseline 为 `.bak`
- **量化 scoreboard**：`docs/phase3-scoreboard.md`（新建）—— 表格列出每个 baseline 当前 pass-rate +
  go-error 案例清单 + oracle-error-permanent 注释

### OUT（不在 Phase 3）

- 修任何 `go-error` 案例 —— Phase 4 范围
- 验证 / 实现 `enable-dynamic-fields=true` 路径下的 corpus —— Phase 5 (P7) 再用同样工具链跑一次
- 扩 corpus（加 WrenAI 真实产 query 等）—— 留独立 Phase
- Java oracle 升级到 0.11.1 —— Phase 8 评估
- CI 自动 re-capture（Java oracle 需明确版本 pin + 网络访问，不适合自动化）

## 3. 架构与数据流

### Java oracle 拉起

```
make oracle-up
  ↓
tools/oracle-up.sh
  ├─ docker pull ghcr.io/canner/wren-engine:0.9.3
  ├─ docker run -d \
  │    --name wren-oracle \
  │    -p 18080:8080 \
  │    -v $(pwd)/../wren-engine-0.9.3/example/duckdb-tpch-example/etc:/usr/src/app/etc:ro \
  │    -e MAX_HEAP_SIZE=2g -e MIN_HEAP_SIZE=512m \
  │    ghcr.io/canner/wren-engine:0.9.3
  └─ wait-for-ready: poll http://localhost:18080/v1/config 直到 200 (timeout 60s)
```

注意：mount 目录 **only-read** (`:ro`) 防 oracle 自己 PATCH 改变 baseline 输入。

### 全量 capture

```
make capture-all-golden  → 顺序串行四工具（避免 Java 并发挂掉）
  ├─ capture-golden                                ← /v1/mdl/dry-plan, modelingOnly=group-config
  ├─ capture-duckdb-golden                          ← /v1/mdl/dry-plan, modelingOnly=false
  ├─ capture-envelope-golden --groups all          ← /v1/mdl/preview
  └─ capture-analysis-golden                        ← /v1/analysis/sql + v2 (已 P5 完成)

每工具：
  for case in corpus:
      resp = oracle.do(case.body)
      if resp.status/100 == 2:
          write golden/<group>/<name>.<ext>
          remove .error marker
      else:
          write golden/<group>/<name>.<ext>.error  (包含 JSON 错误 body)
          remove golden file
      print "OK    " or "ERROR " <case.id>
  print summary
```

### baseline 重算

```
make rebaseline
  ├─ cp baseline.json baseline.json.bak                         ← 备份所有 4 个
  ├─ cp baseline-envelope.json baseline-envelope.json.bak
  ├─ cp baseline-duckdb.json baseline-duckdb.json.bak
  ├─ go test ./internal/difftest/... -difftest.accept -count=1   ← rewrite
  ├─ go test ./internal/difftest/... -difftest.accept-envelope -count=1
  ├─ go test ./internal/difftest/... -difftest.accept-duckdb -count=1
  └─ diff -u baseline.json.bak baseline.json                    ← 人工 review 变化
```

`oracle-error-permanent` 来源：
- 当 `<name>.<ext>.error` 文件存在 → difftest 跑出 `oracle-error`
- 维护者人工把已知 Java bug 的 `.error` 改名为 `.error.permanent`（或加 sentinel 行）
- difftest 读到 `.error.permanent` 后状态打 `oracle-error-permanent`

## 4. 组件与文件结构

| 文件 | 操作 | 说明 |
|---|---|---|
| `tools/oracle-up.sh` | 新建 | docker run + readiness probe |
| `tools/oracle-down.sh` | 新建 | docker stop/rm |
| `Makefile`（如不存在）/ extend | 新建/改 | `oracle-up` / `oracle-down` / `capture-all-golden` / `rebaseline` 4 target |
| `cmd/capture-golden/main.go` | 改 | 增 `--timeout` / `--retry` / `--groups` flag；尾行 summary |
| `cmd/capture-envelope-golden/main.go` | 改 | 同上；`--groups all` 支持 |
| `cmd/capture-duckdb-golden/main.go` | 改 | 同上 |
| `cmd/capture-analysis-golden/main.go` | 改 | flag 风格统一（当前用位置参数） |
| `internal/difftest/difftest_test.go` | 改 | 识别 `.error.permanent` → `oracle-error-permanent` 状态；不计入 fail |
| `internal/difftest/duckdb_diff_test.go` | 改 | 同上 |
| `internal/difftest/envelope_diff_test.go` | 改 | 同上（如已存在） |
| `testdata/difftest/golden/tpch/1.sql.error.permanent` | rename | `tpch/1` 永久 oracle-error 标记 |
| `testdata/difftest/golden/tpch/4.sql.error.permanent` | rename | 同上 |
| `testdata/difftest/golden-envelope/{exec_smoke,viewenum}/*.json` | 新建 (capture 产出) | 8 个文件 |
| `testdata/difftest/golden-duckdb/{tpch,metric,viewenum}/*.sql` | 新建 (capture 产出) | 47 个文件（扣除 q1/q4 permanent → 实际 45 个） |
| `testdata/difftest/baseline*.json` | rotate | 全部刷新；`*.bak` 备份 |
| `docs/phase3-scoreboard.md` | 新建 | 量化 scoreboard + go-error 案例清单（交付 Phase 4） |
| `docs/oracle-runbook.md` | 新建 | 「如何起 Java oracle 重采 golden」3 步操作 + 故障排查 |

## 5. 实施切片（方案 C：增量切片）

### 切片 1 — Oracle 启停脚本 + Makefile

- 写 `tools/oracle-up.sh`：参数化 image tag (默认 `0.9.3`)、port (默认 18080)、mount path
  (默认 `../wren-engine-0.9.3/example/duckdb-tpch-example/etc`)
- 加 readiness probe：`curl --max-time 60 --retry 30 --retry-delay 2 -fs http://localhost:18080/v1/config`
- 写 `tools/oracle-down.sh`：`docker rm -f wren-oracle`
- Makefile：`oracle-up` / `oracle-down` / `oracle-logs` 3 target
- 验证：`make oracle-up && curl localhost:18080/v1/config | jq 'length'` 返 11

### 切片 2 — Capture 工具统一 flag + summary

- 4 个 `cmd/capture-*/main.go` 统一接口：
  - `--addr http://localhost:18080`
  - `--cases testdata/difftest/cases`
  - `--out testdata/difftest/<dir>`
  - `--groups tpch,metric,viewenum,exec_smoke` (`all` 默认)
  - `--timeout 60s`
  - `--retry 0`
- 尾行输出 `summary: ok=X errs=Y total=Z`
- `capture-analysis-golden` 从位置参数迁到 flag（保兼容旧脚本：位置参数仍接受）
- 验证：`go build ./cmd/...` clean；每工具 `--help` 显示新 flag

### 切片 3 — `oracle-error-permanent` 状态

- `internal/difftest/difftest_test.go`：在 case 跑测前 stat `<golden>.error.permanent`，存在则
  返回 `oracle-error-permanent` 状态而非 `oracle-error`
- 同样改造 `duckdb_diff_test.go` / `envelope_diff_test.go`（envelope 测试文件如已存在）
- baseline JSON 接受 `"oracle-error-permanent"` 作为有效状态值
- 给 `tpch/1.sql.error` → 改名 `tpch/1.sql.error.permanent`（带 sentinel 注释行说明原因：
  `# Java 0.9.3 IllegalArgumentException: count(*) should have a followed source`）
- 同样 rename `tpch/4.sql.error`
- 验证：`go test ./internal/difftest/...` 跑过 + baseline 显示这 2 case 为 `oracle-error-permanent`

### 切片 4 — 全量 capture + 量化 scoreboard

- `make capture-all-golden`：串行跑 4 个 capture 工具
- 跑完后 `make rebaseline` 自动 accept 所有新 golden
- 人工 diff `baseline*.json.bak` vs `baseline*.json`，确认变化合理
- 写 `docs/phase3-scoreboard.md`：
  ```
  | Baseline | Total | pass | fail | go-error | oracle-error-permanent | drop-in 通过率 |
  | rewrite        | 47 | 38 | 0 | 7 | 2 | 38/45 = 84% |
  | envelope       | 8  | 8  | 0 | 0 | 0 | 100% |
  | duckdb-dialect | 47 | TBD| TBD|TBD| TBD | TBD |
  ```
  + go-error 7 个 case 一行一条解释，交付 Phase 4
- 写 `docs/oracle-runbook.md`：「采 golden 标准流程」3 步操作 + 常见错误处理

## 6. 风险登记表

| # | 风险 | 说明 |
|---|---|---|
| 1 | **Java oracle 输出不字节稳定** | Java 内部用 `HashMap` 序列化 `List<Map>` 顺序不稳；偶发 capture 不一致。**化解**：每个 capture 之前 fresh `oracle-up`（全新 JVM），如发现重跑 diff，逐字段排查；envelope JSON 已有 `JSONEqualWithOptions` 容忍 |
| 2 | **tpch/1+q4 Java bug 是否真"永久"** | Java 0.9.3 `count(*) should have a followed source` 在 0.11.1 可能已修。**化解**：sentinel 注释里写明 Java 0.9.3 版本号；Phase 8 升 0.11.1 时重新尝试 capture，可能转为 pass |
| 3 | **TPC-H DuckDB 数据不存在导致 envelope 报 runtime error** | TPC-H example mdl 引用 `tpch.orders` 等表；DuckDB 容器内是否真有数据，要看 `duckdb-init.sql` 是否预填。**化解**：`tools/oracle-up.sh` 启动后 `curl /v1/data-source/duckdb/query` 跑 `SHOW TABLES` 探针；如缺数据手动跑 `INSTALL tpch; LOAD tpch; CALL dbgen(sf=0.01)` |
| 4 | **capture 工具 4 个二进制重复代码** | flag 解析 + http 调用 + 文件写入逻辑近似。**化解**：Phase 3 不重构，仅统一 flag。重构留独立 cleanup PR |
| 5 | **baseline.json 自动 accept 掩盖 Go 真实 regression** | `--difftest.accept` 把所有当前结果一股脑写回 baseline，新引入的 fail 也会被 accept 成新基准。**化解**：每次 rebaseline 强制 `.bak` 备份 + 强制人工 diff |
| 6 | **envelope JSON 含 wall-clock 时间戳 / 行 ID** | preview 响应可能含 query duration / execution timestamp。**化解**：`JSONEqualWithOptions` 已对 `duration` 做 mask（P6 引入），envelope 测试沿用 |
| 7 | **viewenum group 用 `init.sql`，其它 group 没有** | `cases/viewenum/init.sql` 是 DuckDB 初始化用，但 Java oracle 不读这个 (它读 `etc/duckdb-init.sql`)。**化解**：把 `viewenum/init.sql` 合并进 Java oracle 的 init-sql 配置；Phase 3 oracle-up 脚本支持 `--extra-init-sql` 参数 |
| 8 | **oracle 镜像 pull 失败 / 网络问题** | 中国大陆 `ghcr.io` 访问不稳。**化解**：脚本 `docker pull` 失败时打印「请配置 docker registry mirror」hint；不强制要求；可手动 `docker load` 离线镜像 |
| 9 | **Phase 3 后 Phase 4 才能开工 → bottleneck** | 全量 capture 跑完 + rebaseline 后，go-error 清单才能交付 Phase 4。**化解**：实际并行可行 —— Phase 4 可以在 Phase 3 切片 4 之前就开始针对**已知的 7 个 go-error**（baseline.json 里看得到）做修复；Phase 3 切片 4 跑完只是给增量 go-error（envelope/duckdb 新发现的）补 ticket |
| 10 | **WrenAI 实际用 0.11.1 vs Go 对齐 0.9.3** | Phase 3 capture 用 0.9.3，但生产环境是 0.11.1。0.9.3 → 0.11.1 间 HTTP API 兼容性未审计。**化解**：Phase 3 只关心 Go vs 0.9.3 等价；版本差异留 Phase 8 评估 |

## 7. 错误处理

- **`make oracle-up` 失败**（docker daemon down / image pull fail / port 占用）：
  exit 非零 + stderr 含原始 docker 错误；不静默吞
- **readiness probe 超时**：60s 内 `/v1/config` 没返 200 → 打印 `docker logs wren-oracle` 后 30 行 +
  fail；用户排查
- **capture 工具单个 case 失败**（HTTP timeout / 连接断）：写 `.error` + 继续下一 case；
  最终 summary 显示 errs 数；非零 exit
- **capture 工具全部失败**（连不上 oracle）：第 1 个 case 就快速失败 + fatal exit；
  日志含 `is the oracle up?` 提示
- **rebaseline diff 显示大量 fail→pass / pass→fail 摇摆**：人工排查；Phase 3 不自动 commit
  baseline，等人工 review
- **`oracle-error-permanent` 标记被误用**：维护者把临时失败标永久 → 隐藏真实 Java bug。
  `.error.permanent` sentinel 注释**必须**写明：Java 版本号 + error message + 复现命令

## 8. 测试与验收

### 验收标准

- ✅ `make oracle-up` exit 0；后续 `curl http://localhost:18080/v1/config | jq 'length'` 返 11
- ✅ `make capture-all-golden` exit 0；尾部打印每工具 summary，total 数与 corpus 一致
- ✅ `make rebaseline` 后 `baseline.json` + `baseline-envelope.json` + `baseline-duckdb.json` **不留**
  `"no-golden"`；`oracle-error` 仅出现在 `oracle-error-permanent` 形态
- ✅ `tpch/1` + `tpch/4` 状态在所有 baseline 都是 `oracle-error-permanent`
- ✅ `docs/phase3-scoreboard.md` 含 4 表 + go-error 7 case 一行一注解
- ✅ `docs/oracle-runbook.md` 3 步操作可重复
- ✅ `go test ./internal/difftest/... -race` clean
- ✅ `go build ./...` + `go vet ./...` 无新警告

### 测试方式

- **集成**：在 clean checkout 上跑 `make oracle-up && make capture-all-golden && make rebaseline`，
  从头到尾 < 10 分钟
- **重入性**：第二次跑 `make capture-all-golden` 输出**完全相同**（确认 Java oracle 输出稳定）；
  如果不同 → 走 risk #1 排查
- **regression 防护**：故意把一个 Go test pass 改 fail（e.g. P3a 某 rewrite rule 注释掉），
  `make rebaseline` 不会自动 accept，必须人工选择
- **量化 scoreboard 真实性**：随机抽 2 个 pass 案例手动跑 `curl` 对比 golden，确认 baseline 与 golden 一致

## 9. 与其他阶段的关系

- **依赖 P0–P6**：difftest framework / 4 个 capture 工具骨架 / corpus 结构都 P5 已经建好
- **依赖 Phase 1**：可选 —— 如果 Go 镜像可作为 baseline 自检 (运行 Go 镜像 + 一份 curl-driven smoke
  test 对比 Java oracle 输出)，Phase 3 后可加。但非强依赖
- **依赖 Phase 2**：oracle 容器用 Java 默认 ConfigManager 解析 `etc/config.properties`，Phase 2
  让 Go 也走相同语义 —— 但 Phase 3 capture 不实际启 Go 容器，所以**不强依赖** Phase 2 完成
- **交付 Phase 4**：`docs/phase3-scoreboard.md` 列出 7 个 (或经 envelope/duckdb capture 增多的)
  `go-error` 案例 + 失败信息，作为 Phase 4 工作清单
- **铺垫 Phase 5 (P7)**：Phase 5 实现 dynamic-field 后用同样工具链重新 capture（仅改 oracle 的
  `enable-dynamic-fields=true`）；当前 Phase 3 输出的 baseline 是「静态分支基线」，P7 后会出
  「动态分支基线」对照

### Drop-in gap 登记表（Phase 3 后状态）

| Gap | Phase 2 后 | Phase 3 后 |
|---|---|---|
| TPC-H 22 query 验证 | 20/22 pass + 2 oracle-error | 20/22 pass + 2 oracle-error-permanent (Java bug 标记清) |
| envelope baseline 完整性 | 0/8 (全 no-golden) | 8/8 有 golden + 真实 pass-rate |
| duckdb-dialect baseline 完整性 | 0/47 (全 no-golden) | 45/47 有 golden + 真实 pass-rate (q1/q4 permanent) |
| `tools/oracle-up.sh` 可复现 | ❌ | ✅ |
| go-error 7 个案例清单 | 散在 baseline.json | docs/phase3-scoreboard.md 集中 |
| Postgres wire protocol (7432) | ❌ 仍缺 | ❌ 仍缺（Phase 6 评估） |
| `WrenSqlRewrite` dynamic-field 分支 | ❌ 全缺 | ❌ 仍缺（Phase 5 / P7） |
| WrenAI 0.11.1 vs Go 对齐 0.9.3 | ❌ 未审计 | ❌ 仍未审计（Phase 8） |

---

- 本 spec 完成后进入 `gpowers:writing-plans`，产出 Phase 3 逐任务实施计划。
