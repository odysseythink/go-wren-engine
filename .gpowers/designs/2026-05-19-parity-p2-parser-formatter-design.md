# P2 设计：解析器/格式化器字节对齐

> 子项目：P2（七阶段拆解中的第三阶段）。前置：P0（修复构建）+ P1（差分测试框架）。
> 目标：让 Go 的 SQL parser 与 SQL formatter 产出与 Java `wren-engine:0.9.3` **逐字节一致**的 SQL 文本。

## 1. 背景

整体目标是让 Go 引擎 100% 替代 `ghcr.io/canner/wren-engine:0.9.3`，父项目已确定的两个约束：

- **奇偶校验线**：重写后的 SQL 文本（规范化后）必须与 Java 引擎逐字节一致。
- **实现路线**：Approach C（混合）—— 输出关键层机械移植，基础设施惯用法重写。

P2 **就是那个输出关键层**。Java 侧 `WrenPlanner` 的核心链路：

```java
// WrenPlanner.java
statement = rule.apply(parseSql(SqlFormatter.formatSql(statement)), sessionContext, analyzedMDL);
...
return SqlFormatter.formatSql(statement);
```

每轮重写规则之间都做 `parseSql(SqlFormatter.formatSql(...))`，最后再 `SqlFormatter.formatSql(...)`。
因此 **formatter 的字节输出 = 引擎对外契约**，而 parser 还必须能往返解析 formatter 自身的输出。
P3（重写引擎）能否达到字节奇偶校验，完全取决于 P2 先把 parser/formatter 对齐。

### 体检结论（差距）

| 层 | Java（字节输出关键层） | Go 现状 | 差距 |
|---|---|---|---|
| `SqlFormatter` | 1999 行，100 个 visit | — | 缺失 |
| `ExpressionFormatter` | 1213 行，70 个 visit | `formatter.go` 262 行，约 20 种节点 | 巨大 |
| AST 节点 | 235 个 tree 文件 | 68 个类型（含接口/枚举），约 45 个 struct | 偏小 |
| Parser（AstBuilder） | trino `AstBuilder.java` | `ast_builder.go` 876 行，覆盖子集 | 偏小 |

三个最致命的字节级分歧：现 Go formatter 是**单行紧凑**输出（Java 是多行缩进美化）、**不加括号**（Java 二元表达式强制全括号）、**标识符/字面量转义规则基本未实现**。

## 2. 范围

### IN（P2 负责）

- SELECT 查询子集：`Query` / `QuerySpecification` / `Select` / `SelectItem`
- 关系层：`Table` / `AliasedRelation` / `Join`（全类型）/ `TableSubquery` / `Unnest` / `Lateral` / `SampledRelation` / 括号关系 / `FunctionRelation`
- 集合运算：`Union` / `Intersect` / `Except`
- 完整表达式层：二元（比较/算术/逻辑）、`FunctionCall`、`Cast`/`TryCast`、Searched/Simple `Case` + `WhenClause`、`If`/`NullIf`/`Coalesce`、`In`/`Between`/`Like`/`IsNull`、`Row`/`Subscript`/`Extract`/`AtTimeZone`、子查询/`Exists`、`Dereference`、各类字面量、`QuantifiedComparison`
- 子句：`ORDER BY` / `GROUP BY`（含 grouping sets）/ `HAVING` / `WINDOW` / `LIMIT` / `OFFSET` / `FETCH FIRST`
- `WITH` / CTE、列别名
- 类型渲染（`GenericDataType` 等）
- **DEFAULT 方言**（trino `SqlFormatter.Dialect.DEFAULT`）
- parser（`AstBuilder`）同步扩充到能往返解析 formatter 自身的全部输出

### OUT（不在 P2）

- DDL/DML：`CREATE` / `INSERT` / `UPDATE` / `DELETE` / `MERGE`
- `SHOW` / `DESCRIBE` / `EXPLAIN`
- 事务控制（`START TRANSACTION` / `COMMIT` / `ROLLBACK` / `SET SESSION` 等）
- `GRANT` / `REVOKE` / `DENY` / 角色与权限
- `PREPARE` / `EXECUTE` / `DEALLOCATE` / `DECLARE CURSOR` / `FETCH`
- `MATCH_RECOGNIZE` / `RowPattern`（`RowPatternFormatter.java`）
- 合成节点 `FieldReference` / `SymbolReference` / `LabelDereference`（由分析器产生，parser 不产出，parse+format 往返不会出现）
- DuckDB / PostgreSQL 方言差异（归 P4 连接器/转换层）

理由：上述全部不在 TPC-H 语料中，也不会被重写引擎产出，且 P2 的语料无法覆盖、移了也验证不了（YAGNI）。

## 3. 架构与数据流

```
SQL 文本 ──ANTLR4──> 解析树 ──AstBuilder──> Go AST ──Formatter──> SQL 文本
                                                  （indent 贯穿的 visitor）
```

### 验证回路（专用 Java harness）

P2 的 formatter 是库、不是 HTTP 接口，无法复用 P1 的 `/v1/mdl/dry-plan` 快照。改用专用 Java
harness，与重写引擎完全解耦：

```
input.sql ──> [Java harness] parseSql + SqlFormatter.formatSql ──> input.golden  （离线捕获一次，提交入库）
input.sql ──> [Go] FormatSQL(ParseSQL(sql))                     ──> 必须与 .golden 逐字节相等
```

- harness 镜像 `WrenPlanner` 真实行为：`SqlFormatter.formatSql(parseSql(sql))`，**DEFAULT 方言**
  （`WrenPlanner` 用的就是单参 `formatSql(Node)` = `DEFAULT`）。
- golden 捕获是**一次性离线步骤**（与 P1 golden 快照决策一致）；`go test` / CI 只比对已提交的
  `.golden` 文件，不需要 Java 运行时。

### Formatter 脊柱（移植 trino `SqlFormatter.Formatter`）

`SqlFormatter` 是缩进美化打印器，通过一个 `Integer indent` 参数贯穿所有 visit。Go 侧脊柱：

```go
type formatter struct {
    dialect Dialect
    builder strings.Builder
}
// process(node, indent) 分派；append(indent, s) 写 indentString(indent)+s；
// indentString(n) = strings.Repeat("   ", n)
```

`ExpressionFormatter` 是独立的扁平 visitor（不带 indent），返回 `string`。两者镜像 Java 的双文件
拆分，避免单文件过大。

## 4. 组件与文件结构

| 文件 | 职责 | 操作 |
|---|---|---|
| `internal/parser/ast/*.go` | 扩充 AST 节点至覆盖 §2 IN 范围 | 修改 |
| `internal/parser/ast_builder.go` | 扩充 visitor 方法填充新增节点 | 修改 |
| `internal/parser/formatter/formatter.go` | 移植 `SqlFormatter`：语句/查询/关系 + indent 机制 | 重写 |
| `internal/parser/formatter/expression_formatter.go` | 移植 `ExpressionFormatter`：表达式（扁平 visitor） | 新建 |
| `tools/format-oracle/FormatOracle.java` | Java harness，对 trino-parser 模块编译 | 新建 |
| `tools/capture-format-golden.sh` | 离线捕获 golden 的编排脚本 | 新建 |
| `testdata/format/cases/*.sql` | 输入语料（snippet + TPC-H） | 新建 |
| `testdata/format/golden/*.golden` | 捕获的 Java 格式化输出 | 新建 |
| `internal/parser/formatter/golden_test.go` | P2 差分测试（逐字节比对 + 幂等性） | 新建 |
| `testdata/format/baseline.json` | 计分板（沿用 P1 机制） | 新建 |
| `Makefile` | 新增 `capture-format-golden` target | 修改 |

现有 `internal/parser/formatter/formatter.go`（262 行）与 `formatter_test.go` 是单行紧凑打印器，
与 Java 多行缩进结构不兼容，P2 将其重写为忠实移植。

## 5. 实施切片（Approach B：增量纵切，表达式优先）

每个切片自成一体：扩 AST → 扩 AstBuilder → 移植 formatter visit → 加 snippet golden 用例 →
跑差分测试 → baseline 接受 fail→pass。切片顺序按 formatter 调用图从叶到根。

### 切片 0 — 验证回路

建好安全网：`tools/format-oracle/FormatOracle.java`（对 trino-parser 模块编译，逐个读取
`testdata/format/cases/*.sql`，写出 `*.golden`）、`tools/capture-format-golden.sh`、Makefile
target、golden 语料骨架、`golden_test.go`、`baseline.json`。同时搭好两个 formatter 骨架：
`formatter.go` 的 `formatter` struct 与 `process`/`append`/`indentString` indent 脊柱、
`expression_formatter.go` 的扁平 visitor 骨架。此时 formatter 仍有大量缺口，baseline 记录现状。

### 切片 1 — 标识符 / 字面量 / 类型

叶子节点：`formatIdentifier` 引用规则（delimited → `"` 包裹且 `"`→`""`；非 delimited → 原样）、
`formatStringLiteral` 转义（`'`→`''`）、`LongLiteral`/`DoubleLiteral`/`DecimalLiteral`/
`GenericLiteral`/`BooleanLiteral`/`NullLiteral`、类型渲染（`GenericDataType` 及参数）、关键字大写、
`formatName`（`QualifiedName` 点号拼接）。

### 切片 2 — 表达式

`expression_formatter.go`：二元表达式**强制全括号**（`formatBinaryExpression`）、比较/算术/逻辑、
`FunctionCall`（含 `FILTER` / `OVER` / `DISTINCT` / `ORDER BY` / null-treatment）、`Cast`/`TryCast`、
Searched/Simple `Case` + `WhenClause`、`If`/`NullIf`/`Coalesce`、`In`/`Between`/`Like`/`IsNull`、
`Row`/`Subscript`/`Extract`/`AtTimeZone`、`SubqueryExpression`/`Exists`、`Dereference`、
`QuantifiedComparison`。

### 切片 3 — 关系 / FROM

`Table`、`AliasedRelation`（含列别名 `appendAliasColumns`）、`Join`（INNER/LEFT/RIGHT/FULL/CROSS/
IMPLICIT + `ON`/`USING`/`NATURAL`）、`TableSubquery`、`Unnest`（含 `WITH ORDINALITY`）、`Lateral`、
`SampledRelation`、括号关系、`FunctionRelation`。移植 `processRelation`/`processRelationSuffix`。

### 切片 4 — 查询 / SELECT / 缩进

`visitQuery`（`WITH` + `RECURSIVE`）、`visitQuerySpecification`、`visitSelect`（单列 vs 多列布局
分叉）、`SingleColumn`/`AllColumns`、`visitOrderBy`、`GROUP BY`（含 grouping sets/distinct）、
`HAVING`、`WINDOW`（`formatDefinitionList`）、`visitOffset`/`visitLimit`/`visitFetchFirst`。

### 切片 5 — 集合运算

`Union` / `Intersect` / `Except`，及 `processRelation` 对集合运算的分派。

## 6. 字节分歧风险登记表（已从 Java 源码核实）

| # | 风险 | 事实 |
|---|---|---|
| 1 | 缩进 | `INDENT = "   "`（3 空格）；`indentString(n)` = 重复 n 次；子查询 `indent+1`，部分关系 `indent+2` |
| 2 | 二元表达式全括号 | `formatBinaryExpression` = `'(' + left + ' ' + op + ' ' + right + ')'`；`a+b*c` → `(a + (b * c))`，比较/算术/逻辑全部如此 |
| 3 | SELECT 布局分叉 | 1 列 → `SELECT item\n`；多列 → 每列换行 `\n` + `indentString(indent)` + 首列 `  `/后续 `, ` |
| 4 | FROM 自带换行 | `FROM\n` + `indentString(indent)` + `  ` + relation |
| 5 | 标识符引用 | delimited → `'"' + s.replace("\"","\"\"") + '"'`；非 delimited → 原样 |
| 6 | 字符串字面量 | `formatStringLiteral` = `"'" + s.replace("'","''") + "'"` |
| 7 | DoubleLiteral 方言相关 | DEFAULT 用 `DecimalFormat`，DUCKDB 用 `String.valueOf`；P2 目标 = DEFAULT |
| 8 | 未支持节点 = 硬错误 | Java `visitNode` 抛 `UnsupportedOperationException`；Go 必须 panic/返回 error，**禁止**静默 `/* unhandled */` |
| 9 | 结尾换行 | 查询级输出以 `\n` 结尾（`visitSelect`/`visitQuerySpecification` 都 append `\n`），golden 必须保留 |
| 10 | 关键字大写 | 所有 SQL 关键字大写输出 |

## 7. 错误处理

- **未支持节点**：formatter 遇到范围外或未实现的节点 panic/返回 error（对齐 Java
  `UnsupportedOperationException`），绝不静默输出注释占位符。
- **parser 错误**：ANTLR 错误监听器收集语法错误，`ParseSQL` 返回 error。
- **范围外语句**：parser 可解析（语法属于 trino 文法），但 formatter 报明确的 "out of P2 scope" 错误。

## 8. 测试与验收

### 语料

- **snippet 用例**：每个节点/分支一个小 `.sql`，约 40-60 个，按切片新增（如 `cast.sql`、
  `case_searched.sql`、`join_using.sql`、`union.sql`、`nested_arithmetic.sql`、`with_cte.sql`）。
- **TPC-H 22 条**：复用 P1 已落地的 `testdata/difftest/cases/tpch/` 查询。

每个 `.sql` 经 harness 捕获为 `.golden` 并提交入库。

### 测试方式

- **差分测试**：`FormatSQL(ParseSQL(sql))` 与 `.golden` **逐字节相等**（不做规范化 —— 字节对齐
  本身就是 P2 的目的，与 P1 重写输出的规范化比较不同）。
- **幂等性测试**：`FormatSQL(ParseSQL(golden)) == golden` —— 保证 parser 能往返解析 formatter
  自身输出（`WrenPlanner` 规则间会重解析，此性质是 P3 的前提）。
- **计分板**：沿用 P1 `baseline.json` 机制 —— pass→fail 为硬失败；fail→pass 提示接受。

### 验收标准

- 全部 snippet golden 用例 `FormatSQL(ParseSQL(sql))` 与 `.golden` 逐字节命中。
- TPC-H 22 条全部 parse+format 往返与 `.golden` 逐字节命中。
- 幂等性测试全绿。
- `gofmt -l` 无输出、`go vet ./...` 干净、`go build ./...` 通过。

## 9. 与其他阶段的关系

- **依赖 P0/P1**：构建已修复、差分测试框架（corpus loader、baseline 计分板）已就位。
- **被 P3 依赖**：P3（重写引擎奇偶校验）将重写后的 AST 交给本 formatter 输出；P2 的字节对齐与
  幂等性是 P3 端到端 TPC-H 差分测试能通过的前提。
- 本 spec 完成后进入 writing-plans，产出逐任务实施计划。
