# Phase 4 设计：rewrite 链路 go-error 收尾（10 case 同一根因簇）

> 子项目：Phase 4（drop-in replacement 路线图 5 阶段中的第 4 阶段）。
> 前置：P0–P6 + Phase 1（Dockerfile）+ Phase 2（config.properties）。
> 与 Phase 3 关系：**可并行**。Phase 3 是 test-data + tooling；Phase 4 是 Go 源码 bug fix。
> 目标：闭合 `baseline.json` 当前的 10 个 `go-error` case，使 rewrite-layer 差分基线
> 不留 Go panic / 无效输出。

## 1. 背景

brainstorming + Phase 3 spec 调研：`baseline.json` 47 个 case 中 **10 个 go-error**：

| 案例 ID | SQL | 涉及 MDL 对象 |
|---|---|---|
| `tpch/m_orders` | `select orderkey, totalprice from Orders` | Model（含 calc field + relationship） |
| `tpch/met_customer_revenue` | `select custkey, totalprice from CustomerRevenue` | Metric → Model |
| `tpch/met_daily` | (groupby date_spine) | CumulativeMetric |
| `tpch/met_revenue` | `select * from Revenue` | Metric |
| `tpch/met_rollup` | `select * from roll_up(Revenue, orderdate, YEAR)` | roll_up 函数 |
| `tpch/met_weekly` | (groupby date_spine, week) | CumulativeMetric |
| `tpch/v_use_metric` | `select * from useMetric` | View → Metric |
| `tpch/v_use_model` | `select * from useModel` | View → Model |
| `tpch/v_use_nested` | (view → view → model) | View 嵌套 |
| `tpch/v_use_rollup` | `select * from useMetricRollUp` | View → CumulativeMetric |

`tpch/v_enum` PASS，所以**基本 view rewrite 正常**；fail 的 view 案例都引用了 metric/cumulative/calc-field
路径。

### 现场调研锁定 2 个根因

跑 `go test -run TestPhase4Repro -v`（临时探针）抓 panic stack，发现：

```
panic: runtime error: invalid memory address or nil pointer dereference
parser.(*AstBuilder).VisitAliasedRelation       ast_builder.go:378  ← nil dereference 现场
parser.(*AstBuilder).VisitPatternRecognition    ast_builder.go:946
...
```

**所有 10 个 case 的 panic stack 起点完全一致**。配合 stderr 上方：

```
line 1:0 mismatched input '{' expecting {'(', '[', 'ADD', ...
line 1:329 mismatched input '{' expecting ...
line 1:339 mismatched input ''c_custkey'' expecting ...
```

可确认：

| Root cause | 层次 | 描述 |
|---|---|---|
| **RC-A**：parser 输入含 `{` 字符 | rewrite 链路 / formatter | rewrite 循环 `parse → format → re-parse` 中，formatter 把某种 MDL 表达式（calc field `customer.nation.name` / jinja 痕迹 `{{...}}` / 关系路径 dump）当 SQL 直接输出；下一轮 re-parse 时 ANTLR 词法器在 `{` 处报错并启动 error recovery |
| **RC-B**：`ast_builder.go:378` 缺 nil-check | parser AST 构造层 (P2) | ANTLR error recovery 后传给 visitor 的 `AliasedRelationContext` 出现 `Identifier() == nil && ColumnAliases() != nil` 畸形组合；第 374 行 `b.visitIdentifier(ctx.Identifier())` 把 `nil` 喂进去 → panic |

**RC-B 是 RC-A 的被动后果**：parser error recovery 把畸形树喂给 visitor，visitor 缺防御性 nil-check。
**修 RC-B 即不再 panic**，但 rewrite 输出仍是错误 SQL —— 需要同时修 RC-A 才能让 case 真正 pass。

### 奇偶校验线（Phase 4）

① 修 RC-B：visitor 不再 panic，错误正常返回 `error` 类型（不是 `panic` 类型）；
② 修 RC-A：formatter 不再 emit 含 `{` 的中间 SQL；calc field / relationship path / cumulative-metric
表达式有专门的 SQL-safe 输出路径；
③ 10 个 case 在 baseline 中转 `pass` 或 `fail`（**不允许 `go-error` 残留**）；
④ 35 个现有 pass case 全部不回归。

## 2. 范围

### IN（Phase 4 负责）

- **RC-B 防御性修复**：`internal/parser/ast_builder.go:374` 加 nil-check —— `ctx.Identifier() == nil` 时
  alias 处理跳过；同步审计 visitor 中其它**所有**直接调用 `ctx.XXX()` 后无 nil 判断就解引用的位置
  （ANTLR error recovery 路径都可能传 nil）
- **RC-A formatter 审计**：枚举 rewrite chain（`internal/rewrite/`）4 条规则 + formatter
  （`internal/parser/format*.go`）所有可能 emit 字符串字面量的位置；定位哪条规则 / 哪个 formatter
  分支会产生 `{` 字符；修复其逻辑（最大嫌疑：calc-field expression / relationship path / jinja
  残留 / unknown-type expression）
- **测试加固**：
  - `internal/parser/ast_builder_test.go` 加 RC-B 单元测试（构造畸形 AliasedRelationContext 或注入
    伪 ANTLR tree）
  - `internal/rewrite/*_test.go` 给 10 个 fail case 加 focused unit test —— 直接 call `Rewrite()` +
    assert 输出 SQL token 等价 golden
- **baseline 刷新**：`make difftest-accept` 在 fix 落定后 rotate baseline；10 个 case 全部转 pass
  或带 detail 的 fail（不接受 go-error）
- **回归保护**：每个 fix commit 后 `go test ./internal/difftest/... -run TestDifferential` 必须 0
  regression（35 个原 pass case 都 pass）

### OUT（不在 Phase 4）

- `WrenSqlRewrite` dynamic-field 分支（Phase 5 / P7）
- envelope-layer go-error（preview/dry-run 执行层）—— 由 Phase 3 capture 增量发现，本 Phase 不接
- DuckDB-dialect 层 fail —— Phase 3 capture 后才知道有几个，留 Phase 4.5 或并入 Phase 7
- `tpch/1` + `tpch/4` 的 `oracle-error`（Java 自身 bug，Phase 3 标 `oracle-error-permanent`）
- 重构 parser visitor / formatter 整体架构 —— 仅 nil-check + 单点修复

## 3. 架构与数据流

### 故障传播链

```
user SQL: "select * from Revenue"          ← 起始
   ↓ parse                                   ✓ OK
   AST 1
   ↓ GenerateViewRewrite (P3c)               ✓ no-op for non-view
   AST 1
   ↓ format → parse                          ✓ OK
   AST 1'
   ↓ MetricRollupRewrite (P3b)               ← 可能在此处展开 Revenue metric
   AST 2: 含 metric 展开后的 calc field 表达式
   ↓ format → re-parse                       ✗ formatter emits "..."{custkey, totalprice, ...}"...
                                              ANTLR lexer error on '{'  → recovery
   malformed AST 3
   ↓ WrenSqlRewrite (P3a) - visitor 调用      ← AliasedRelation visitor 收到 nil Identifier
   PANIC at ast_builder.go:378
```

注意：parser lexer 报 `{` error 后，ANTLR 默认走 error recovery（构造一个尽量完整的 tree），
visitor 拿到的 tree 含 nil 节点 —— RC-B 暴露。

### 修复方向（双 fix）

```
Fix 1 (RC-B, defensive): ast_builder.go:374
   diff:
     -    alias := b.visitIdentifier(ctx.Identifier())
     +    var alias *ast.Identifier
     +    if ctx.Identifier() != nil {
     +        alias = b.visitIdentifier(ctx.Identifier())
     +    }
     -    ar := &ast.AliasedRelation{..., Alias: alias}
     +    ar := &ast.AliasedRelation{..., Alias: alias}  // Alias is *Identifier so nil OK

Fix 2 (RC-A, root cause): formatter 不应 emit '{'
   1. 用 binary search / probe 定位哪条 rule + 哪个 formatter 分支输出 '{'
      手段：tools/repro-phase4.go（一次性脚本），跑每条 rule 后打印 format 结果
   2. 修复该 formatter 分支：calc field 应输出 SQL 表达式而非 `{...}` 字面量
```

### Phase 4 工作循环

```
loop:
  pick a go-error case (e.g. tpch/m_orders, simplest model case first)
  ↓
  isolate to focused test in internal/rewrite/<rule>_test.go
  ↓
  run -> get current actual output / panic site
  ↓
  apply fix (defensive or root-cause)
  ↓
  re-run focused test → pass
  ↓
  run full baseline → 0 regression
  ↓
  commit
  ↓
  move on
```

## 4. 组件与文件结构

| 文件 | 操作 | 说明 |
|---|---|---|
| `internal/parser/ast_builder.go` | 修改 | `VisitAliasedRelation:374` nil-check + 同样审计 `VisitTableName/VisitSubqueryRelation/VisitFunctionRelation/...` 整文件 ~110 个 visitor 方法 |
| `internal/parser/ast_builder_test.go` | 新增/扩 | RC-B 单元测试 —— 构造畸形 ParseTree 或 round-trip `parse(broken-sql) → visitor` 不 panic |
| `internal/parser/format*.go` | 修改 | RC-A fix —— 定位输出 `{` 的代码路径并修复（具体文件 Phase 4 切片 1 后才能确定） |
| `internal/rewrite/sql_rewrite_test.go` | 新增/扩 | 10 个 case 的 focused test：input SQL + expected SQL，旁路 difftest framework |
| `internal/rewrite/metric_rollup_test.go` / `generate_view_test.go` | 扩 | metric / view rewrite case 的 focused unit test |
| `tools/probe-rewrite-rules.go` | 新增（一次性） | 调试脚本：跑每条 rule 后 dump format 结果，定位 RC-A 注入位 |
| `testdata/difftest/baseline.json` | rotate | Phase 4 完成后 `make difftest-accept`；10 个 case 转 pass/fail |

## 5. 实施切片（方案 C：增量切片）

### 切片 1 — RC-B 防御性 nil-check（快速止 panic）

- 在 `ast_builder.go:374` 加 nil-check
- 同步**全文件审计** 110 个 `Visit*` 方法：任何 `ctx.XXX()` 调用结果传给 visit/visitIdentifier
  且**未先判 nil** 的位置都加 nil-check（防御性，覆盖所有 ANTLR error recovery 路径）
- 加 `ast_builder_test.go` 的 nil-context 单元测试
- 验证：`go test ./internal/difftest/... -run TestDifferential` 跑完，10 个 case **不再 panic**；
  状态从 `go-error` 转 `fail`（带 detail 说明 SQL 不等价）；这是预期 —— 还没改 RC-A 自然 fail
- commit：`fix(p4/rc-b): nil-safe visitor methods for ANTLR error-recovery trees`

### 切片 2 — RC-A 定位（formatter audit）

- 写 `tools/probe-rewrite-rules.go`：对每个 fail case 调用 `rewrite.Rewrite` 同时**逐 rule 截获**
  并打印当前 format 输出（修改 `planner.go.Rewrite` 加 debug hook，或反射调内部 step；Phase 4
  内不固化此调试接口，结束后删除）
- 跑 `m_orders` 的逐步 format 输出 → 找到第几条 rule 开始 emit `{`、emit 在哪个表达式位置
- 检查对应 formatter 分支 / rewrite rule 的 builder 代码
- 最大嫌疑（按可能性排序）：
  1. **calc field `customer.nation.name`** —— 关系路径在 formatter 中未转 SQL，原样输出
  2. **CumulativeMetric date_spine** —— `roll_up(...)` 函数参数 dump 含特殊符号
  3. **Jinja 残留** —— P6 jinja regex 替换不完整，留下 `{{...}}` 进入 rewrite chain
  4. **Optional 字段** —— Go fmt.Sprintf("%v", nil) 在 Optional 表达式中产生 `{}`
- 修复后跑 `m_orders` focused test → 真实 pass

### 切片 3 — 逐簇修复 + focused tests

按依赖簇修，每修一个跑一次全 baseline 防回归：

- **簇 1 — calc field / relationship**：`m_orders`（最简单，Model 含 `customer.nation.name`
  calc field）→ 修后预期 `met_customer_revenue` / `met_revenue` / `v_use_model` 同步解决
- **簇 2 — Metric**：`met_customer_revenue` / `met_revenue`（基础 Metric 走 calc 表达式）→
  修簇 1 后多数应当连带 pass
- **簇 3 — CumulativeMetric / roll_up**：`met_daily` / `met_weekly` / `met_rollup`（date_spine
  + roll_up 函数）—— 可能涉及 P3b 中独立的 CumulativeMetric rewrite 路径
- **簇 4 — View**：`v_use_metric` / `v_use_model` / `v_use_nested` / `v_use_rollup` —— 修簇 1-3 后
  view 引用应当级联通过；剩余的 view-on-view-on-metric 嵌套是 P3c 自己的 bug

每修一簇：
- 新增 focused unit test 复盘该簇 SQL → expected SQL
- 跑 `go test ./internal/difftest/... -run TestDifferential` 验全量 0 regression
- commit：`fix(p4/cluster-<N>): <root-cause-class> for <cluster-case-list>`

### 切片 4 — baseline 接受 + 量化产出

- `make difftest-accept`（即 `go test ./internal/difftest/... -difftest.accept`）rotate baseline
- 人工 diff `baseline.json.bak vs baseline.json` 验证：
  - 10 个原 go-error 全部转 `pass`（理想）或 `fail`（说明 SQL 等价不全，但至少跑完）
  - 0 个原 pass 转 go-error / fail
- 更新 `docs/phase3-scoreboard.md`（Phase 3 引入）reflecting Phase 4 成果
- 删除 `tools/probe-rewrite-rules.go` 临时调试脚本（不进 main）
- commit：`chore(p4): baseline rotation — rewrite layer 0 go-error`

## 6. 风险登记表

| # | 风险 | 说明 |
|---|---|---|
| 1 | **RC-A 真实位置不在 formatter** 而在 rewrite rule 的 String builder | 切片 2 probe 工具必须**逐 rule** 而不是只看 final output；`internal/rewrite/<rule>.go` 内部如有 `fmt.Sprintf` 直接拼字符串可能跳过 formatter |
| 2 | **RC-B 的 visitor 全文件审计漏点** | ast_builder.go ~2000 行 110 个 Visit*，机械审计可能漏 1-2 个。**化解**：切片 1 的 baseline 跑完后，**任何**新发现的 `go-error` 案例如果根因也是 nil-deref，立即补该 visitor 的 nil-check |
| 3 | **calc field 表达式语义复杂**（关系路径 `a.b.c.d` 多跳 join） | Java 经由 `WrenSqlRewrite` 转 join + alias；Go 当前是否真实现该展开未知。若 Go 静态分支不支持深层 calc field，需要在簇 1 收缩 scope（用 stub 输出 `NULL`）+ 记入风险 |
| 4 | **CumulativeMetric date_spine** 实现完整性 | P3b spec 标 `roll_up(Revenue, orderdate, YEAR)` 函数式语法在静态分支下不完整。簇 3 可能并非 RC-A，而是 P3b 本身有未实现路径，需独立追溯 |
| 5 | **修一个 case 引入新 fail** | 簇 1 修 calc field 可能破坏 `m_lineitem_calc`（已 pass）。**化解**：每簇结束跑全 baseline，detect 任何新 regression 立即 revert + 重新设计 fix |
| 6 | **probe 工具污染 planner.go** | 切片 2 临时 debug hook 直接改 planner.go 不安全。**化解**：用 build tag `//go:build probephase4` 隔离；切片 4 删除整个 tag |
| 7 | **focused test 覆盖度** | 加 10 个 focused test 但只覆盖当前 corpus；新 corpus query 可能再炸 RC-A。**化解**：focused test 重点是**断言不 panic + 输出合法 SQL**（用 `parser.ParseSQL` 二次解析确认），不只断言 token 等价 |
| 8 | **ast_builder.go nil-check 引入 silent 错误吞噬** | 全文件加 nil-check 可能让本该报错的 invalid AST 被静默通过。**化解**：nil-check 路径**也输出错误**（log.Println("malformed AST at <method>") 或返回 error），不要 silent return |
| 9 | **Phase 3 与 Phase 4 baseline rotate 竞争** | 两 Phase 并行时都改 baseline.json。**化解**：Phase 4 切片 4 在 Phase 3 切片 4 之后跑；如同时跑用 `.bak` 备份双方各自的版本，最后人工 merge |
| 10 | **focused test 测的是 input → output SQL**，但 Java golden 在 token 级容忍 whitespace | difftest 用 `Normalize()` lexer 比对 token 序列，focused test 应当也用同样 normalizer 而非 `strings.Equal`，避免比对策略不一致 |

## 7. 错误处理

- **panic 替代为 error**：visitor 不再 panic 任何 nil 场景，而是返回 error 通过 `goRewrite` 的 err 路径
  传出 → difftest 状态从 `go-error` 转 `fail`（带 detail message）
- **probe 工具失败**：切片 2 probe 工具若无法定位 RC-A，进入 Plan B —— manual 逐 rule 注释 +
  跑 case 看哪条 rule 后 SQL 含 `{` 字符（grep 即可）
- **修 RC-A 修出新 regression**：每个 commit 强制运行 full baseline，detect regression 立即 revert
- **Java golden 与 Go fix 输出不 token-等价**：状态记 `fail`（不是 go-error），detail 含 token-diff 位置；
  这是 Phase 4 可接受的结果（drop-in 目标是「不 panic」而非「100% Java byte-perfect」）

## 8. 测试与验收

### 验收标准

- ✅ `go test ./internal/difftest/... -run TestDifferential` 输出中 **0 个 `go-error`**（10 个原
  go-error 全部转 `pass` 或 `fail`）
- ✅ 35 个原 `pass` case **全部仍 `pass`**（0 regression）
- ✅ `tpch/1` + `tpch/4` 仍是 `oracle-error-permanent`（Phase 3 引入的状态保持不变）
- ✅ `go test ./internal/parser/... -race` clean
- ✅ `go test ./internal/rewrite/... -race` clean
- ✅ `go build ./...` + `go vet ./...` 无新警告
- ✅ `tools/probe-rewrite-rules.go` 临时调试文件**已删除**（不进主分支）
- ✅ ast_builder.go nil-check 路径有对应单元测试覆盖
- ✅ `baseline.json` 已 rotate；`baseline.json.bak` 保留供 diff
- ✅ `docs/phase3-scoreboard.md` 更新到 Phase 4 后数字（如 `rewrite: 45/45 pass`）

### 测试方式

- **单元**：`ast_builder_test.go` + `sql_rewrite_test.go` + `metric_rollup_test.go` 各扩 2-4 个 case
- **集成**：`make difftest`（即 `go test ./internal/difftest/... -run TestDifferential`）每次
  fix commit 后跑一次
- **回归保护**：每个 simgle-cluster fix 完后 baseline 必须 byte-equal `*.bak`（除被 fix 的 10
  case 状态外），否则人工排查
- **race**：`-race` 跑全包，nil-check 改动不应引入 race

## 9. 与其他阶段的关系

- **依赖 P0–P6**：rewrite chain 整体框架在 P0-P6 已搭好；Phase 4 仅 patch
- **依赖 Phase 1/2**：弱 —— Phase 4 工作完全在源码层，Dockerfile / config.properties 不影响
- **与 Phase 3 并行**：Phase 3 是 test-data / tooling，Phase 4 是源码 bug fix；两者**共享**
  `baseline.json` 写权，需要 baseline rotate 时序协调（见 risk #9）
- **铺垫 Phase 5 (P7)**：Phase 4 修好的 calc-field / metric / view rewrite **同样**会被 P7
  dynamic-field 分支调用 —— 修在共用代码（formatter / visitor）的就立等可用；只在静态分支
  rewrite rule 内的修复，P7 实现 dynamic 分支时还要复制一遍逻辑

### Drop-in gap 登记表（Phase 4 后状态）

| Gap | Phase 3 后 | Phase 4 后 |
|---|---|---|
| `baseline.json` go-error | 10 | **0** |
| TPC-H 22 query pass rate | 20/22（含 2 永久 oracle-error） | 20/22（不变；Phase 4 修 fixture group case 不影响 q1-q22） |
| TPC-H model alias / metric / view 链路 | 7 个 go-error | **0** go-error（pass 数视 RC-A 修复深度而定） |
| `baseline-envelope.json` 完整性 | 8/8 有 golden (Phase 3) | 不变（envelope 是执行层，Phase 4 不动） |
| `baseline-duckdb.json` 完整性 | 45/47 有 golden (Phase 3) | 不变 |
| `WrenSqlRewrite` dynamic-field 分支 | ❌ 全缺 | ❌ 仍缺（Phase 5 / P7） |
| Postgres wire protocol (7432) | ❌ 仍缺 | ❌ 仍缺（Phase 6 评估） |
| WrenAI 0.11.1 vs Go 对齐 0.9.3 | ❌ 未审计 | ❌ 未审计（Phase 8） |

---

- 本 spec 完成后进入 `gpowers:writing-plans`，产出 Phase 4 逐任务实施计划。
