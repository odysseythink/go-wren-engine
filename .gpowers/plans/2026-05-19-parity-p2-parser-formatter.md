# P2 解析器/格式化器字节对齐 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use gpowers:subagent-driven-development (recommended) or gpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Go 的 `FormatSQL(ParseSQL(sql))` 产出与 Java `wren-engine:0.9.3` 的 `SqlFormatter.formatSql(parseSql(sql))` 逐字节一致的 SQL 文本（DEFAULT 方言），并保证 parser 能往返解析 formatter 自身的输出。

**Architecture:** 把 trino 的 `SqlFormatter`（缩进美化打印器，`Integer indent` 贯穿所有 visit）与 `ExpressionFormatter`（扁平 visitor，返回 `string`）忠实移植成两个 Go 文件。验证回路用一个专用 Java harness（Maven 项目，依赖 `io.wren:trino-parser:0.9.3`）离线捕获 golden，Go 侧 `go test` 只比对已提交的 `.golden` 文件、不需要 Java。计分板沿用 P1 的 `baseline.json` 机制。按 formatter 调用图从叶到根分 6 个切片增量推进。

**Tech Stack:** Go 1.26、ANTLR4 Go runtime、Java 17 + Maven（仅 `capture-format-golden` 用）。

**Spec:** `.gpowers/designs/2026-05-19-parity-p2-parser-formatter-design.md`

**源参考：** Java 引擎源码在 `../wren-engine-0.9.3`。两个关键文件：
- `trino-parser/src/main/java/io/trino/sql/SqlFormatter.java`（1999 行，语句/查询/关系 visit + indent 机制）
- `trino-parser/src/main/java/io/trino/sql/ExpressionFormatter.java`（1213 行，表达式 visit）

本计划中的 Go 代码已按上述两个 Java 文件逐 visit 核对；行号引用均指向这两个文件。

**关键字节事实（已从 Java 源码核实，全程适用）：**
- 缩进单位 `INDENT = "   "`（3 个空格）；`indentString(n)` = 重复 n 次。
- 二元表达式**强制全括号**：`formatBinaryExpression` = `'(' + left + ' ' + op + ' ' + right + ')'`。
- 标识符引用：delimited → `'"' + s.replace("\"","\"\"") + '"'`；非 delimited → 原样。
- 字符串字面量：`"'" + s.replace("'","''") + "'"`。
- `NULL` 字面量渲染为小写 `null`；布尔字面量渲染为小写 `true` / `false`。
- 未支持节点 = 硬错误：Java `visitNode` 抛 `UnsupportedOperationException`；Go 必须 `panic`，**禁止**静默输出 `/* unhandled */` 占位符。
- 查询级输出以 `\n` 结尾。

---

## Task 1: Java 格式化 oracle harness

建立验证回路的"标准答案机"：一个 Maven 项目，依赖 `io.wren:trino-parser:0.9.3`，对 `testdata/format/cases/**/*.sql` 逐个执行 `SqlFormatter.formatSql(parseSql(sql))` 并写出 `.golden`。这一步完全离线、与重写引擎解耦。

**Files:**
- Create: `tools/format-oracle/pom.xml`
- Create: `tools/format-oracle/src/main/java/io/wren/tools/FormatOracle.java`
- Create: `tools/capture-format-golden.sh`
- Create: `testdata/format/cases/slice0/select_one.sql`
- Modify: `Makefile`

- [ ] **Step 1: 创建 oracle 的 Maven 工程**

Create `tools/format-oracle/pom.xml`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
    <modelVersion>4.0.0</modelVersion>
    <groupId>io.wren.tools</groupId>
    <artifactId>format-oracle</artifactId>
    <version>0.9.3</version>
    <packaging>jar</packaging>

    <properties>
        <maven.compiler.source>17</maven.compiler.source>
        <maven.compiler.target>17</maven.compiler.target>
        <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
        <exec.mainClass>io.wren.tools.FormatOracle</exec.mainClass>
    </properties>

    <dependencies>
        <dependency>
            <groupId>io.wren</groupId>
            <artifactId>trino-parser</artifactId>
            <version>0.9.3</version>
        </dependency>
    </dependencies>
</project>
```

- [ ] **Step 2: 创建 oracle 主程序**

Create `tools/format-oracle/src/main/java/io/wren/tools/FormatOracle.java`:

```java
package io.wren.tools;

import io.trino.sql.SqlFormatter;
import io.trino.sql.parser.ParsingOptions;
import io.trino.sql.parser.SqlParser;
import io.trino.sql.tree.Statement;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.stream.Stream;

/**
 * FormatOracle freezes the Java reference output for P2's differential test.
 * For every cases/**.sql it runs SqlFormatter.formatSql(parseSql(sql)) — the
 * exact pair WrenPlanner uses — and writes the bytes to golden/**.sql.golden.
 * On a parse/format error it writes golden/**.sql.golden.error instead.
 */
public final class FormatOracle
{
    // Mirror io.wren.base.sqlrewrite.Utils: SqlParser with AS_DOUBLE.
    private static final SqlParser SQL_PARSER = new SqlParser();
    private static final ParsingOptions PARSING_OPTIONS =
            new ParsingOptions(ParsingOptions.DecimalLiteralTreatment.AS_DOUBLE);

    private FormatOracle() {}

    public static void main(String[] args)
            throws IOException
    {
        Path casesDir = Path.of(args.length > 0 ? args[0] : "testdata/format/cases");
        Path goldenDir = Path.of(args.length > 1 ? args[1] : "testdata/format/golden");

        List<Path> sqlFiles = new ArrayList<>();
        try (Stream<Path> walk = Files.walk(casesDir)) {
            walk.filter(p -> p.toString().endsWith(".sql")).sorted().forEach(sqlFiles::add);
        }

        int ok = 0;
        int errs = 0;
        for (Path sqlFile : sqlFiles) {
            String sql = Files.readString(sqlFile, StandardCharsets.UTF_8);
            Path rel = casesDir.relativize(sqlFile);
            Path goldenBase = goldenDir.resolve(rel.toString() + ".golden");
            Files.createDirectories(goldenBase.getParent());
            try {
                Statement stmt = SQL_PARSER.createStatement(sql, PARSING_OPTIONS);
                String formatted = SqlFormatter.formatSql(stmt);
                Files.writeString(goldenBase, formatted, StandardCharsets.UTF_8);
                Files.deleteIfExists(Path.of(goldenBase + ".error"));
                ok++;
                System.out.println("OK    " + rel);
            }
            catch (RuntimeException e) {
                Files.writeString(Path.of(goldenBase + ".error"),
                        e.getClass().getName() + ": " + e.getMessage(), StandardCharsets.UTF_8);
                Files.deleteIfExists(goldenBase);
                errs++;
                System.out.println("ERROR " + rel + " — " + e.getMessage());
            }
        }
        System.out.printf("%ncaptured %d golden, %d parse/format errors, %d total%n",
                ok, errs, sqlFiles.size());
    }
}
```

- [ ] **Step 3: 创建捕获脚本**

Create `tools/capture-format-golden.sh`:

```bash
#!/usr/bin/env bash
# 把 Java SqlFormatter 的输出冻结成 P2 差分测试的 golden 文件.
# 需要本机有 java(17+) 与 mvn, 且 ../wren-engine-0.9.3 源码可用.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WREN_JAVA="${WREN_JAVA:-${REPO_ROOT}/../wren-engine-0.9.3}"

if [ ! -d "${WREN_JAVA}/trino-parser" ]; then
  echo "找不到 trino-parser 源码: ${WREN_JAVA}/trino-parser" >&2
  echo "设置 WREN_JAVA 环境变量指向 wren-engine-0.9.3 检出目录" >&2
  exit 1
fi

# 1) 把 io.wren:trino-parser:0.9.3 装进本地 m2 (幂等; 已装则很快).
echo "安装 trino-parser 到本地 Maven 仓库..."
( cd "${WREN_JAVA}" && mvn -q -pl trino-parser -am -DskipTests install )

# 2) 编译并运行 oracle, 工作目录 = 仓库根, 使 testdata 相对路径生效.
echo "运行 format oracle..."
( cd "${REPO_ROOT}/tools/format-oracle" && mvn -q compile )
( cd "${REPO_ROOT}" && mvn -q -f tools/format-oracle/pom.xml exec:java )
```

- [ ] **Step 4: 赋予脚本可执行权限并建首个种子用例**

Run: `chmod +x tools/capture-format-golden.sh`

Create `testdata/format/cases/slice0/select_one.sql`:

```sql
SELECT 1
```

- [ ] **Step 5: 在 Makefile 增加 P2 相关目标**

把 `Makefile` 第一行的 `.PHONY` 改为包含新目标：

```makefile
.PHONY: build test clean generate capture-golden difftest difftest-accept capture-format-golden format-golden format-accept
```

并在文件末尾追加 3 个目标：

```makefile
capture-format-golden:
	./tools/capture-format-golden.sh

format-golden:
	go test ./internal/parser/formatter/ -run 'TestFormatGolden|TestFormatIdempotent' -v

format-accept:
	go test ./internal/parser/formatter/ -run TestFormatGolden -format.accept
```

- [ ] **Step 6: 验证 oracle 跑通**

Run: `make capture-format-golden`
Expected: 打印 `OK    slice0/select_one.sql`，末尾 `captured 1 golden, 0 parse/format errors, 1 total`，并生成文件 `testdata/format/golden/slice0/select_one.sql.golden`。

Run: `cat testdata/format/golden/slice0/select_one.sql.golden`
Expected: 内容为 `SELECT 1\n`（`SELECT` 后一空格、`1`、一个换行）。

若 `mvn install` 失败：确认本机 Java ≥ 17、`mvn -version` 可用；首次会从 Maven Central 拉取 guava/slice/antlr4-runtime 依赖，需要网络。

- [ ] **Step 7: Commit**

```bash
git add tools/format-oracle tools/capture-format-golden.sh Makefile testdata/format/cases/slice0/select_one.sql testdata/format/golden/slice0/select_one.sql.golden
git commit -m "feat: add Java format oracle harness for P2 differential test"
```

---

## Task 2: 重写 formatter 为脊柱骨架

丢弃现有单行紧凑打印器，按 trino `SqlFormatter` 的结构重建：`formatter` struct + `process`/`append`/`indentString` indent 脊柱，外加新建 `expression_formatter.go` 的扁平 visitor 骨架。此时两个 visitor 对**任何**节点都 `panic`（对齐 Java `visitNode` 抛 `UnsupportedOperationException`）。后续切片逐节点填充。

**Files:**
- Modify (整体重写): `internal/parser/formatter/formatter.go`
- Create: `internal/parser/formatter/expression_formatter.go`
- Delete: `internal/parser/formatter/formatter_test.go`（旧单行紧凑打印器的测试，与多行缩进结构不兼容）

- [ ] **Step 1: 删除与旧打印器绑定的测试**

Run: `git rm internal/parser/formatter/formatter_test.go`
Expected: 文件被移除（Task 3 会用 golden 差分测试取代它）。

- [ ] **Step 2: 整体重写 `formatter.go` 为脊柱骨架**

把 `internal/parser/formatter/formatter.go` 的全部内容替换为：

```go
// Package formatter renders an AST back to SQL text. It is a faithful port of
// trino's SqlFormatter (statement/query/relation visits, indentation) and
// ExpressionFormatter (expression visits). The byte output is the engine's
// external contract: WrenPlanner re-parses and re-formats between every
// rewrite rule, so P2 requires byte-for-byte parity with Java.
package formatter

import (
	"fmt"
	"strings"

	"github.com/wren-engine/wren/internal/parser/ast"
)

// Dialect selects dialect-specific formatting. P2 targets DialectStandard
// (trino SqlFormatter.Dialect.DEFAULT); the DuckDB branch is filled in by P4.
type Dialect int

const (
	DialectStandard Dialect = iota
	DialectDuckDB
	DialectPostgreSQL
)

// indentUnit mirrors trino SqlFormatter.INDENT — three spaces.
const indentUnit = "   "

// FormatSQL renders stmt to SQL text using the DEFAULT dialect.
func FormatSQL(stmt ast.Statement) string {
	return FormatSQLDialect(stmt, DialectStandard)
}

// FormatSQLDialect renders stmt to SQL text using the given dialect.
func FormatSQLDialect(stmt ast.Statement, dialect Dialect) string {
	f := &formatter{dialect: dialect}
	f.process(stmt, 0)
	return f.builder.String()
}

// formatter is the indented statement/query/relation visitor. It mirrors
// trino SqlFormatter.Formatter (an AstVisitor<Void, Integer>).
type formatter struct {
	dialect Dialect
	builder strings.Builder
}

// process dispatches one statement/query/relation/clause node. Expressions are
// not handled here — they go through formatExpression. An unhandled node is a
// hard error, mirroring Java SqlFormatter.visitNode throwing.
func (f *formatter) process(node ast.Node, indent int) {
	switch n := node.(type) {
	default:
		panic(fmt.Sprintf("SqlFormatter: not yet implemented: %T", n))
	}
}

// append writes indentString(indent) followed by s. Mirrors Java
// Formatter.append(int, String).
func (f *formatter) append(indent int, s string) {
	f.builder.WriteString(f.indentString(indent))
	f.builder.WriteString(s)
}

// indentString returns the indent prefix for the given depth.
func (f *formatter) indentString(indent int) string {
	return strings.Repeat(indentUnit, indent)
}
```

- [ ] **Step 3: 创建 `expression_formatter.go` 骨架**

Create `internal/parser/formatter/expression_formatter.go`:

```go
package formatter

import (
	"fmt"

	"github.com/wren-engine/wren/internal/parser/ast"
)

// formatExpression renders an expression to SQL text. It mirrors trino's
// ExpressionFormatter — a flat (non-indented) visitor returning a string.
func formatExpression(expr ast.Expression, dialect Dialect) string {
	return (&exprFormatter{dialect: dialect}).process(expr)
}

// exprFormatter mirrors trino ExpressionFormatter.Formatter
// (an AstVisitor<String, Void>).
type exprFormatter struct {
	dialect Dialect
}

// process renders one expression node. An unhandled node is a hard error,
// mirroring Java ExpressionFormatter.visitExpression throwing.
func (e *exprFormatter) process(expr ast.Expression) string {
	switch n := expr.(type) {
	default:
		panic(fmt.Sprintf("ExpressionFormatter: not yet implemented: %T", n))
	}
}
```

- [ ] **Step 4: 验证编译**

Run: `go build ./internal/parser/...`
Expected: 退出码 0。`internal/rewrite/planner.go` 仍调用 `formatter.FormatSQL`，签名未变，编译通过。

Run: `go build ./...`
Expected: 退出码 0。若其他包因删除 `formatter_test.go` 报错，不可能（测试文件不参与 `go build`）。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/formatter/formatter.go internal/parser/formatter/expression_formatter.go
git commit -m "refactor: rebuild formatter as SqlFormatter-shaped skeleton"
```

---

## Task 3: Go golden 差分测试与计分板

建好安全网：`go test` 离线读取已提交的 `.golden`，比对 `FormatSQL(ParseSQL(sql))` 的字节输出；`baseline.json` 计分板防回归（沿用 P1 机制：pass→fail 硬失败，fail→pass 提示 accept）。此时 formatter 骨架对 `SELECT 1` 也会 panic，首轮基线把它记为 `panic`。

**Files:**
- Create: `internal/parser/formatter/golden_test.go`
- Create: `testdata/format/baseline.json`

- [ ] **Step 1: 创建初始 baseline**

Create `testdata/format/baseline.json`:

```json
{
  "version": 1,
  "cases": {}
}
```

`cases` 留空：差分测试把"baseline 中不存在的用例"视同非 pass；首轮跑完用 `make format-accept` 写入真实状态。

- [ ] **Step 2: 写 golden 差分测试**

Create `internal/parser/formatter/golden_test.go`:

```go
package formatter

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wren-engine/wren/internal/parser"
)

var acceptFlag = flag.Bool("format.accept", false,
	"rewrite baseline.json with current results instead of asserting")

const (
	casesDir     = "../../../testdata/format/cases"
	goldenDir    = "../../../testdata/format/golden"
	baselinePath = "../../../testdata/format/baseline.json"
)

type baselineFile struct {
	Version int               `json:"version"`
	Cases   map[string]string `json:"cases"`
}

// caseID is the cases-dir-relative path of a .sql file, e.g. "slice1/cast.sql".
func collectCases(t *testing.T) []string {
	t.Helper()
	var ids []string
	err := filepath.WalkDir(casesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".sql") {
			rel, _ := filepath.Rel(casesDir, path)
			ids = append(ids, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk cases dir: %v", err)
	}
	sort.Strings(ids)
	return ids
}

// runFormatCase formats one case with the Go engine and compares it to the
// Java golden. Status is one of: pass, fail, panic, parse-error, oracle-error,
// no-golden.
func runFormatCase(id string) (status, detail string) {
	goldenBase := filepath.Join(goldenDir, id+".golden")
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java parser/formatter rejected this case"
	}
	want, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "golden missing; run `make capture-format-golden`"
	}
	sqlBytes, err := os.ReadFile(filepath.Join(casesDir, id))
	if err != nil {
		return "no-golden", "case file missing: " + err.Error()
	}

	got, status, detail := goFormat(string(sqlBytes))
	if status != "" {
		return status, detail
	}
	if got == string(want) {
		return "pass", ""
	}
	return "fail", firstDiff(string(want), got)
}

// goFormat parses and formats sql, recovering panics. On success it returns
// (output, "", ""); on failure ("", status, detail).
func goFormat(sql string) (out, status, detail string) {
	defer func() {
		if r := recover(); r != nil {
			out, status, detail = "", "panic", fmt.Sprintf("%v", r)
		}
	}()
	stmt, err := parser.ParseSQL(sql)
	if err != nil {
		return "", "parse-error", err.Error()
	}
	return FormatSQL(stmt), "", ""
}

// firstDiff returns a human-readable description of the first byte that differs.
func firstDiff(want, got string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			return fmt.Sprintf("byte %d: want %q, got %q\n--- want ---\n%s\n--- got ---\n%s",
				i, want[i], got[i], want, got)
		}
	}
	return fmt.Sprintf("length: want %d, got %d\n--- want ---\n%s\n--- got ---\n%s",
		len(want), len(got), want, got)
}

func readBaseline(t *testing.T) baselineFile {
	t.Helper()
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var b baselineFile
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	if b.Cases == nil {
		b.Cases = map[string]string{}
	}
	return b
}

func writeBaseline(t *testing.T, results map[string]string) {
	t.Helper()
	b := baselineFile{Version: 1, Cases: results}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	if err := os.WriteFile(baselinePath, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	t.Logf("baseline updated: %s", baselinePath)
}

func TestFormatGolden(t *testing.T) {
	ids := collectCases(t)
	results := make(map[string]string, len(ids))
	for _, id := range ids {
		status, detail := runFormatCase(id)
		results[id] = status
		if status == "pass" {
			t.Logf("PASS  %s", id)
		} else {
			t.Logf("%-12s %s — %s", status, id, detail)
		}
	}

	if *acceptFlag {
		writeBaseline(t, results)
		return
	}

	base := readBaseline(t)
	regressions, improvements := 0, 0
	for id, status := range results {
		want := base.Cases[id] // missing => "" => non-pass
		switch {
		case want == "pass" && status != "pass":
			regressions++
			t.Errorf("REGRESSION %s: baseline=pass, now=%s", id, status)
		case want != "pass" && status == "pass":
			improvements++
			t.Errorf("%s now passes — run `make format-accept` to update baseline", id)
		}
	}

	counts := map[string]int{}
	for _, s := range results {
		counts[s]++
	}
	t.Logf("format scoreboard: %d/%d byte-identical", counts["pass"], len(results))
	if regressions == 0 && improvements == 0 {
		t.Logf("baseline consistent, no regression")
	}
}
```

- [ ] **Step 3: 首轮运行（预期 improvements 报错或全非 pass）**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 打印 `panic slice0/select_one.sql — SqlFormatter: not yet implemented: *ast.Query`（formatter 骨架对任何节点 panic）。计分板 `0/1 byte-identical`。无 `REGRESSION`。

- [ ] **Step 4: 接受当前结果为基线**

Run: `make format-accept`
Expected: `baseline.json` 的 `cases` 写入 `{"slice0/select_one.sql": "panic"}`。

- [ ] **Step 5: 再次运行确认绿**

Run: `make format-golden`
Expected: PASS。打印 `baseline consistent, no regression`。

- [ ] **Step 6: 验证防回归机制**

把 `testdata/format/baseline.json` 中 `slice0/select_one.sql` 的值从 `"panic"` 改成 `"pass"`。

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden`
Expected: FAIL，报 `REGRESSION slice0/select_one.sql: baseline=pass, now=panic`。

随后 `git checkout testdata/format/baseline.json` 还原。

- [ ] **Step 7: Commit**

```bash
git add internal/parser/formatter/golden_test.go testdata/format/baseline.json
git commit -m "test: add P2 format golden differential test with baseline"
```

---

## Task 4: 切片 1 — 标识符 / 字面量

移植 `ExpressionFormatter` 的叶子节点：标识符引用规则、字符串/数值/布尔/NULL/类型构造器字面量。其中 `DoubleLiteral` 的 DEFAULT 方言渲染（Java 用 `DecimalFormat("0.###################E0###")` 科学计数法）是字节分歧高风险点，本任务用充足的 golden 用例锁定。

**Files:**
- Modify: `internal/parser/formatter/expression_formatter.go`
- Create: `testdata/format/cases/slice1/*.sql`（见 Step 1）

参考 Java：`ExpressionFormatter.java` 的 `visitIdentifier`(404)、`visitStringLiteral`(231)、`visitLongLiteral`(296)、`visitDoubleLiteral`(302)、`visitGenericLiteral`(318)、`visitBooleanLiteral`(225)、`visitNullLiteral`(347)、`formatIdentifier`(125)、`formatStringLiteral`(999)。

- [ ] **Step 1: 写切片 1 的 snippet 用例**

创建以下文件（每个一行 SQL，无尾随换行）：

`testdata/format/cases/slice1/ident_plain.sql`:
```sql
SELECT abc FROM t
```

`testdata/format/cases/slice1/ident_delimited.sql`:
```sql
SELECT "a b", "weird""quote" FROM t
```

`testdata/format/cases/slice1/string_literal.sql`:
```sql
SELECT 'hello', 'O''Brien'
```

`testdata/format/cases/slice1/long_literal.sql`:
```sql
SELECT 0, 42, 9223372036854775807
```

`testdata/format/cases/slice1/double_literal.sql`:
```sql
SELECT 0.05, 0.06, 0.07, 0.08, 0.1, 0.2, 1.0, 24.0, 100.0, 0.0001, 1234.5
```

`testdata/format/cases/slice1/bool_null.sql`:
```sql
SELECT true, false, null
```

`testdata/format/cases/slice1/generic_literal.sql`:
```sql
SELECT DATE '1995-01-01', TIMESTAMP '1995-01-01 00:00:00'
```

- [ ] **Step 2: 捕获这些用例的 golden**

Run: `make capture-format-golden`
Expected: 打印新增 7 个 `OK    slice1/...`。生成对应 `.golden` 文件。

注：`bool_null.sql` 的 golden 里 `true`/`false`/`null` 全部小写——Java `visitBooleanLiteral` = `String.valueOf(boolean)`、`visitNullLiteral` 返回字面量 `"null"`。

- [ ] **Step 3: 运行测试确认这些用例失败**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 7 个 `slice1/*` 用例为 `panic`（formatter 尚未实现表达式 visit）。无 `REGRESSION`。

- [ ] **Step 4: 实现标识符 / 字面量 visit**

在 `internal/parser/formatter/expression_formatter.go` 的 import 块加入 `"strconv"` 与 `"strings"`，并把 `process` 的 `switch` 补成（保留 `default` 分支）：

```go
func (e *exprFormatter) process(expr ast.Expression) string {
	switch n := expr.(type) {
	case *ast.Identifier:
		if !n.Delimited {
			return n.Value
		}
		return formatIdentifier(n.Value, e.dialect)
	case *ast.LongLiteral:
		return strconv.FormatInt(n.Value, 10)
	case *ast.DoubleLiteral:
		return formatDouble(n.Value, e.dialect)
	case *ast.StringLiteral:
		return formatStringLiteral(n.Value)
	case *ast.GenericLiteral:
		return n.Type + " " + formatStringLiteral(n.Value)
	case *ast.BooleanLiteral:
		if n.Value {
			return "true"
		}
		return "false"
	case *ast.NullLiteral:
		return "null"
	default:
		panic(fmt.Sprintf("ExpressionFormatter: not yet implemented: %T", n))
	}
}

// formatIdentifier quotes a delimited identifier. trino formatIdentifier:
// DEFAULT/DUCKDB/POSTGRES all wrap in double quotes and double any embedded
// double quote.
func formatIdentifier(s string, dialect Dialect) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// formatStringLiteral wraps s in single quotes, doubling any embedded quote.
// Mirrors trino ExpressionFormatter.formatStringLiteral.
func formatStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// formatDouble renders a double literal. The DUCKDB dialect (filled in by P4)
// uses String.valueOf; the DEFAULT dialect uses Java's
// DecimalFormat("0.###################E0###") — normalized scientific notation
// with one integer digit, up to 19 fractional digits, and a sign-on-negative
// exponent with no leading zeros.
func formatDouble(v float64, dialect Dialect) string {
	// strconv 'E' with precision -1 gives the shortest round-tripping mantissa
	// (<=17 significant digits for float64, within DecimalFormat's 19-# limit).
	// Go emits e.g. "6E-02" / "1E+02"; Java emits "6E-2" / "1E2": strip the
	// '+' and any leading zeros from the exponent.
	s := strconv.FormatFloat(v, 'E', -1, 64)
	mantissa, exp, ok := strings.Cut(s, "E")
	if !ok {
		return s
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
	return mantissa + "E" + sign + exp
}
```

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 7 个 `slice1/*` 用例报 `now passes`（formatter 现在正确，baseline 仍记旧值）。`slice0/select_one.sql` 仍 `panic`（语句层未实现）。无 `REGRESSION`。

若 `double_literal.sql` 仍 `fail`：把测试输出里的 `--- want ---` / `--- got ---` 逐值对照——某个值的科学计数法表示与 Java `DecimalFormat` 不一致。记录该值，对照 `formatDouble` 的转换逻辑修正（最可能是指数位数或尾数舍入）。

若 `generic_literal.sql` 报 `parse-error`：Go parser 未处理 `DATE '...'` 类型构造器——见 Task 6 Step 4（本切片先把它留作 `parse-error`，baseline 记录，Task 6 修复）。

- [ ] **Step 6: 接受基线并确认绿**

Run: `make format-accept && make format-golden`
Expected: 第二条 PASS，`baseline consistent`。

- [ ] **Step 7: Commit**

```bash
git add internal/parser/formatter/expression_formatter.go testdata/format/cases/slice1 testdata/format/golden/slice1 testdata/format/baseline.json
git commit -m "feat: format identifiers and literals (P2 slice 1)"
```

---

## Task 5: 切片 1 — 类型渲染与 formatName

移植类型节点（`GenericDataType` 及参数）与 `SqlFormatter.formatName`（`QualifiedName` 点号拼接，每段经 `formatExpression`）。

**Files:**
- Modify: `internal/parser/formatter/expression_formatter.go`
- Modify: `internal/parser/formatter/formatter.go`
- Create: `testdata/format/cases/slice1/cast_type.sql`

参考 Java：`ExpressionFormatter.visitGenericDataType`(806)、`visitTypeParameter`(845)、`visitNumericTypeParameter`(851)；`SqlFormatter.formatName`(191)。

- [ ] **Step 1: 写用例**

Create `testdata/format/cases/slice1/cast_type.sql`:

```sql
SELECT CAST(x AS bigint), CAST(y AS varchar(10)), CAST(z AS decimal(18, 4))
```

- [ ] **Step 2: 实现类型渲染 helper**

在 `expression_formatter.go` 末尾追加（`ast.DataType` 字段见 `internal/parser/ast/types.go`：`Name string`、`Parameters []DataTypeParameter`，参数为 `*TypeParameter`{`Type DataType`} 或 `*NumericParameter`{`Value string`}）：

```go
// formatType renders a data type. Mirrors trino ExpressionFormatter
// .visitGenericDataType: NAME optionally followed by (arg, arg, ...).
func (e *exprFormatter) formatType(t *ast.DataType) string {
	result := t.Name
	if len(t.Parameters) == 0 {
		return result
	}
	parts := make([]string, len(t.Parameters))
	for i, p := range t.Parameters {
		parts[i] = e.formatTypeParameter(p)
	}
	return result + "(" + strings.Join(parts, ", ") + ")"
}

// formatTypeParameter renders one type parameter — a nested type or a number.
func (e *exprFormatter) formatTypeParameter(p ast.DataTypeParameter) string {
	switch tp := p.(type) {
	case *ast.TypeParameter:
		return e.formatType(&tp.Type)
	case *ast.NumericParameter:
		return tp.Value
	default:
		panic(fmt.Sprintf("ExpressionFormatter: not yet implemented type param: %T", tp))
	}
}
```

- [ ] **Step 3: 实现 `formatName` helper**

在 `formatter.go` 末尾追加（`ast.QualifiedName` 见 `internal/parser/ast/qualified_name.go`：`OriginalParts []Identifier`）：

```go
// formatName joins a qualified name with dots, each part rendered as an
// expression. Mirrors trino SqlFormatter.formatName.
func formatName(name ast.QualifiedName, dialect Dialect) string {
	parts := make([]string, len(name.OriginalParts))
	for i := range name.OriginalParts {
		parts[i] = formatExpression(&name.OriginalParts[i], dialect)
	}
	return strings.Join(parts, ".")
}
```

注：`CAST` 表达式本身在切片 2 实现（Task 9），本任务只备好 `formatType`；`cast_type.sql` 本切片仍会因 `Cast` 未实现而非 pass，baseline 记录、Task 9 翻 pass。

- [ ] **Step 4: 捕获 golden、跑测试、接受基线**

Run: `make capture-format-golden`
Expected: 新增 `OK    slice1/cast_type.sql`。

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: `cast_type.sql` 为 `panic`（`Cast` 未实现）。无 `REGRESSION`。

Run: `make format-accept && make format-golden`
Expected: PASS。

- [ ] **Step 5: 验证编译**

Run: `go build ./... && go vet ./internal/parser/...`
Expected: 退出码 0。

- [ ] **Step 6: Commit**

```bash
git add internal/parser/formatter testdata/format
git commit -m "feat: format data types and qualified names (P2 slice 1)"
```

---

## Task 6: 切片 2 — AstBuilder 与 trino 对齐（CASE / EXTRACT / 类型构造器 / 区间 / 下标）

在移植表达式 formatter 之前，先补齐 Go parser 缺失的表达式节点，并修正两处 AST 结构相对 trino 的字节分歧。**这是切片 2 的前置任务。**

两处分歧（已从 Java 源码核实）：
1. **逻辑表达式**：trino `AstBuilder.visitOr/visitAnd`（行 1560/1575）把连续同操作符的项**展平**成 N 元 `LogicalExpression`；Go 现在的 `VisitOr/VisitAnd` 建左嵌套二元 `LogicalBinaryExpression`。`a AND b AND c` → Java `(a AND b AND c)`、Go `((a AND b) AND c)`，字节不同。
2. **NOT 谓词**：trino 的 `InPredicate`/`BetweenPredicate`/`LikePredicate` 无 `Not` 字段；`x NOT IN (...)` 由 `AstBuilder.visitPredicated` 包成 `NotExpression`。Go 的这些节点带 `Not bool`。`x NOT IN (1,2)` → Java `(NOT (x IN (1, 2)))`、Go 若渲染 `(x NOT IN ...)` 则字节不同。

**Files:**
- Modify: `internal/parser/ast/expression.go`
- Modify: `internal/parser/ast_builder.go`
- Modify: `internal/parser/parser_test.go`

参考 Java：`AstBuilder.java` 的 `visitOr`(1560)、`visitAnd`(1575)、`visitExtract`(2171)、`visitSubscript`(2267)、`visitSimpleCase`(2294)、`visitSearchedCase`(2304)、`visitWhenClause`(2313)、`visitTypeConstructor`(2754)、`visitInterval`(2824)、`visitPredicated`（搜索 `context.NOT()`）。

- [ ] **Step 1: 新增表达式 AST 节点**

在 `internal/parser/ast/expression.go` 末尾追加：

```go
// LogicalExpression represents an N-ary AND / OR. trino flattens consecutive
// same-operator terms (a AND b AND c => one node with three terms), so the Go
// AST must match for byte-identical output.
type LogicalExpression struct {
	BaseNode
	Operator LogicalOperator
	Terms    []Expression
}

func (l *LogicalExpression) GetChildren() []Node {
	children := make([]Node, len(l.Terms))
	for i, t := range l.Terms {
		children[i] = t
	}
	return children
}
func (l *LogicalExpression) isExpression() {}

// SearchedCaseExpression represents CASE WHEN ... THEN ... [ELSE ...] END.
type SearchedCaseExpression struct {
	BaseNode
	WhenClauses  []WhenClause
	DefaultValue Expression
}

func (c *SearchedCaseExpression) GetChildren() []Node { return nil }
func (c *SearchedCaseExpression) isExpression()       {}

// SimpleCaseExpression represents CASE operand WHEN ... THEN ... [ELSE ...] END.
type SimpleCaseExpression struct {
	BaseNode
	Operand      Expression
	WhenClauses  []WhenClause
	DefaultValue Expression
}

func (c *SimpleCaseExpression) GetChildren() []Node { return nil }
func (c *SimpleCaseExpression) isExpression()       {}

// WhenClause represents WHEN operand THEN result.
type WhenClause struct {
	BaseNode
	Operand Expression
	Result  Expression
}

func (w *WhenClause) GetChildren() []Node { return []Node{w.Operand, w.Result} }
func (w *WhenClause) isExpression()       {}

// IfExpression represents IF(condition, trueValue [, falseValue]).
type IfExpression struct {
	BaseNode
	Condition  Expression
	TrueValue  Expression
	FalseValue Expression
}

func (i *IfExpression) GetChildren() []Node { return nil }
func (i *IfExpression) isExpression()       {}

// NullIfExpression represents NULLIF(first, second).
type NullIfExpression struct {
	BaseNode
	First  Expression
	Second Expression
}

func (n *NullIfExpression) GetChildren() []Node { return []Node{n.First, n.Second} }
func (n *NullIfExpression) isExpression()       {}

// ExtractExpression represents EXTRACT(field FROM expr). Field is upper-cased.
type ExtractExpression struct {
	BaseNode
	Field      string
	Expression Expression
}

func (x *ExtractExpression) GetChildren() []Node { return []Node{x.Expression} }
func (x *ExtractExpression) isExpression()       {}

// SubscriptExpression represents base[index].
type SubscriptExpression struct {
	BaseNode
	Base  Expression
	Index Expression
}

func (s *SubscriptExpression) GetChildren() []Node { return []Node{s.Base, s.Index} }
func (s *SubscriptExpression) isExpression()       {}

// Row represents ROW(item, item, ...).
type Row struct {
	BaseNode
	Items []Expression
}

func (r *Row) GetChildren() []Node {
	children := make([]Node, len(r.Items))
	for i, it := range r.Items {
		children[i] = it
	}
	return children
}
func (r *Row) isExpression() {}

// ExistsPredicate represents EXISTS (subquery).
type ExistsPredicate struct {
	BaseNode
	Subquery Statement
}

func (e *ExistsPredicate) GetChildren() []Node { return []Node{e.Subquery} }
func (e *ExistsPredicate) isExpression()       {}

// QuantifiedComparison represents value op ALL/ANY/SOME (subquery).
type QuantifiedComparison struct {
	BaseNode
	Operator   ComparisonOperator
	Quantifier string // "ALL" | "ANY" | "SOME"
	Value      Expression
	Subquery   Expression
}

func (q *QuantifiedComparison) GetChildren() []Node { return []Node{q.Value, q.Subquery} }
func (q *QuantifiedComparison) isExpression()       {}
```

若 `internal/parser/ast/expression.go` 已声明 `ExistsPredicate` 或 `QuantifiedComparison`（`VisitExists` / `VisitQuantifiedComparison` 已存在于 AstBuilder，可能对应已有节点）：不要重复声明——改为复用现有节点，并据其实际字段调整 Task 9–10 的 formatter 代码。先 Run: `grep -rn "ExistsPredicate\|QuantifiedComparison\|type Exists" internal/parser/ast/` 确认。

- [ ] **Step 2: 修正 `VisitOr` / `VisitAnd` 展平为 N 元**

在 `internal/parser/ast_builder.go` 中，把现有的 `VisitOr` 与 `VisitAnd` 替换为（ANTLR 的 `OrContext`/`AndContext` 是左递归、每个节点恰 2 个 `BooleanExpression` 子节点；递归展开同操作符即得展平项序列）：

```go
// VisitOr flattens nested OR contexts into one N-ary LogicalExpression,
// matching trino AstBuilder.visitOr.
func (b *AstBuilder) VisitOr(ctx *generated.OrContext) interface{} {
	return &ast.LogicalExpression{
		Operator: ast.LogicalOr,
		Terms:    b.flattenLogical(ctx, ast.LogicalOr),
	}
}

// VisitAnd flattens nested AND contexts into one N-ary LogicalExpression,
// matching trino AstBuilder.visitAnd.
func (b *AstBuilder) VisitAnd(ctx *generated.AndContext) interface{} {
	return &ast.LogicalExpression{
		Operator: ast.LogicalAnd,
		Terms:    b.flattenLogical(ctx, ast.LogicalAnd),
	}
}

// flattenLogical collects the operand expressions of a chain of same-operator
// AND/OR contexts in left-to-right order.
func (b *AstBuilder) flattenLogical(ctx antlr.ParseTree, op ast.LogicalOperator) []ast.Expression {
	var terms []ast.Expression
	var children []generated.IBooleanExpressionContext
	switch c := ctx.(type) {
	case *generated.AndContext:
		if op == ast.LogicalAnd {
			children = c.AllBooleanExpression()
		}
	case *generated.OrContext:
		if op == ast.LogicalOr {
			children = c.AllBooleanExpression()
		}
	}
	if children == nil {
		// Not a same-operator context: this whole subtree is one term.
		return []ast.Expression{b.visitExpression(ctx)}
	}
	for _, child := range children {
		terms = append(terms, b.flattenLogical(child, op)...)
	}
	return terms
}
```

注：`generated` 的接口/方法名（`IBooleanExpressionContext`、`AllBooleanExpression`）以 `internal/parser/generated/` 实际生成代码为准；若不同，Run: `grep -n "BooleanExpression" internal/parser/generated/sqlbase_parser.go | head` 核对。

- [ ] **Step 3: 新增 CASE / EXTRACT / SUBSCRIPT 的 AstBuilder visitor**

在 `internal/parser/ast_builder.go` 末尾追加（逐字移植自 Java，见上方行号；`generated` 的 context 字段名以生成代码为准）：

```go
// VisitSimpleCase mirrors trino AstBuilder.visitSimpleCase.
func (b *AstBuilder) VisitSimpleCase(ctx *generated.SimpleCaseContext) interface{} {
	c := &ast.SimpleCaseExpression{Operand: b.visitExpression(ctx.GetOperand())}
	for _, wc := range ctx.AllWhenClause() {
		c.WhenClauses = append(c.WhenClauses, *b.visitWhenClause(wc))
	}
	if e := ctx.GetElseExpression(); e != nil {
		c.DefaultValue = b.visitExpression(e)
	}
	return c
}

// VisitSearchedCase mirrors trino AstBuilder.visitSearchedCase.
func (b *AstBuilder) VisitSearchedCase(ctx *generated.SearchedCaseContext) interface{} {
	c := &ast.SearchedCaseExpression{}
	for _, wc := range ctx.AllWhenClause() {
		c.WhenClauses = append(c.WhenClauses, *b.visitWhenClause(wc))
	}
	if e := ctx.GetElseExpression(); e != nil {
		c.DefaultValue = b.visitExpression(e)
	}
	return c
}

// VisitWhenClause mirrors trino AstBuilder.visitWhenClause.
func (b *AstBuilder) VisitWhenClause(ctx *generated.WhenClauseContext) interface{} {
	return &ast.WhenClause{
		Operand: b.visitExpression(ctx.GetCondition()),
		Result:  b.visitExpression(ctx.GetResult()),
	}
}

// visitWhenClause is a typed helper around VisitWhenClause.
func (b *AstBuilder) visitWhenClause(tree antlr.ParseTree) *ast.WhenClause {
	if tree == nil {
		return nil
	}
	return tree.Accept(b).(*ast.WhenClause)
}

// VisitExtract mirrors trino AstBuilder.visitExtract: the field is upper-cased.
func (b *AstBuilder) VisitExtract(ctx *generated.ExtractContext) interface{} {
	return &ast.ExtractExpression{
		Field:      strings.ToUpper(ctx.Identifier().GetText()),
		Expression: b.visitExpression(ctx.ValueExpression()),
	}
}

// VisitSubscript mirrors trino AstBuilder.visitSubscript.
func (b *AstBuilder) VisitSubscript(ctx *generated.SubscriptContext) interface{} {
	return &ast.SubscriptExpression{
		Base:  b.visitExpression(ctx.GetValue()),
		Index: b.visitExpression(ctx.GetIndex()),
	}
}
```

- [ ] **Step 4: 新增类型构造器 visitor（`DATE '...'` 等）**

trino `visitTypeConstructor`（行 2754）把 `DATE '1995-01-01'` 这类构造为 `GenericLiteral{Type, Value}`。在 `ast_builder.go` 末尾追加：

```go
// VisitTypeConstructor mirrors trino AstBuilder.visitTypeConstructor: it builds
// a GenericLiteral such as DATE '1995-01-01'. (DECIMAL keeps a dedicated path
// in trino; the corpus parses decimals AS_DOUBLE, so DECIMAL is not produced.)
func (b *AstBuilder) VisitTypeConstructor(ctx *generated.TypeConstructorContext) interface{} {
	value := stripQuotes(ctx.String_().GetText())
	typeName := ctx.Identifier().GetText()
	if ctx.DOUBLE_PRECISION() != nil {
		typeName = "DOUBLE PRECISION"
	}
	return &ast.GenericLiteral{Type: strings.ToUpper(typeName), Value: value}
}
```

若 `ast_builder.go` 中没有 `stripQuotes` helper，则一并追加（去掉字符串字面量的外层单引号并把 `''` 还原为 `'`）：

```go
// stripQuotes removes the surrounding single quotes of a SQL string literal
// token and unescapes doubled quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		s = s[1 : len(s)-1]
	}
	return strings.ReplaceAll(s, "''", "'")
}
```

注：`generated` 的方法名（`String_`、`DOUBLE_PRECISION`、`Identifier`）以生成代码为准；Run: `grep -n "TypeConstructorContext) " internal/parser/generated/sqlbase_parser.go` 核对可用方法。

- [ ] **Step 5: 更新 `parser_test.go` 中对 `LogicalBinaryExpression` 的断言**

`internal/parser/parser_test.go` 现在断言 `LogicalBinaryExpression`。Run: `grep -n "LogicalBinary" internal/parser/parser_test.go` 找到相关测试，把对 `*ast.LogicalBinaryExpression` 的类型断言与字段访问改为 `*ast.LogicalExpression`（`Terms []Expression` 取代 `Left`/`Right`）。例如 `a AND b` 现在产出 `LogicalExpression{Operator: LogicalAnd, Terms: [a, b]}`。

注：`ast.LogicalBinaryExpression` 类型本身**保留声明**（`internal/rewrite/` 仍引用它；P3a 才会清理 rewrite 包），只是 parser 不再产出它。

- [ ] **Step 6: 验证编译与既有测试**

Run: `go build ./...`
Expected: 退出码 0。

Run: `go test ./internal/parser/ -v`
Expected: parser 包测试通过（含改写后的逻辑表达式断言）。若 `internal/parser/generated` 的接口名与本任务代码不符导致编译失败，按生成代码实际名称修正。

- [ ] **Step 7: Commit**

```bash
git add internal/parser/ast/expression.go internal/parser/ast_builder.go internal/parser/parser_test.go
git commit -m "feat: align AstBuilder with trino — N-ary logical, CASE, EXTRACT (P2 slice 2)"
```

---

## Task 7: 切片 2 — 二元 / 逻辑 / NOT 表达式

移植 `formatBinaryExpression`（强制全括号）、比较 / 算术 / 逻辑 / `NOT`。

**Files:**
- Modify: `internal/parser/formatter/expression_formatter.go`
- Create: `testdata/format/cases/slice2/binary_*.sql`

参考 Java：`ExpressionFormatter` 的 `formatBinaryExpression`(965)、`visitComparisonExpression`(550)、`visitArithmeticBinary`(620)、`visitLogicalExpression`(537)、`visitNotExpression`(544)。

- [ ] **Step 1: 写用例**

`testdata/format/cases/slice2/binary_arith.sql`:
```sql
SELECT a + b * c, (a + b) * c, a - b - c
```

`testdata/format/cases/slice2/binary_compare.sql`:
```sql
SELECT a FROM t WHERE x = 1 AND y < 2 OR z >= 3
```

`testdata/format/cases/slice2/binary_not.sql`:
```sql
SELECT a FROM t WHERE NOT x = 1
```

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 3 个 `OK    slice2/binary_*`。`binary_arith.sql` 的 golden 里 `a + b * c` 渲染为 `(a + (b * c))`——二元表达式强制全括号。`binary_compare.sql` 里 `x = 1 AND y < 2 OR z >= 3` 渲染为 `(((x = 1) AND (y < 2)) OR (z >= 3))`。

- [ ] **Step 3: 实现二元 / 逻辑 / NOT visit**

在 `expression_formatter.go` 的 `process` switch 中，`default` 之前插入：

```go
	case *ast.ComparisonExpression:
		return e.formatBinary(string(n.Operator), n.Left, n.Right)
	case *ast.ArithmeticBinaryExpression:
		return e.formatBinary(string(n.Operator), n.Left, n.Right)
	case *ast.LogicalExpression:
		parts := make([]string, len(n.Terms))
		for i, t := range n.Terms {
			parts[i] = e.process(t)
		}
		return "(" + strings.Join(parts, " "+string(n.Operator)+" ") + ")"
	case *ast.NotExpression:
		return "(NOT " + e.process(n.Value) + ")"
```

并在文件末尾追加 helper：

```go
// formatBinary renders a binary expression fully parenthesized:
// '(' left ' ' op ' ' right ')'. Mirrors trino formatBinaryExpression.
func (e *exprFormatter) formatBinary(op string, left, right ast.Expression) string {
	return "(" + e.process(left) + " " + op + " " + e.process(right) + ")"
}
```

- [ ] **Step 4: 跑测试、接受基线、确认绿**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 3 个 `slice2/binary_*` 报 `now passes`。无 `REGRESSION`。

Run: `make format-accept && make format-golden`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/formatter/expression_formatter.go testdata/format
git commit -m "feat: format binary, logical and NOT expressions (P2 slice 2)"
```

---

## Task 8: 切片 2 — 函数调用与窗口

移植 `visitFunctionCall`（`DISTINCT` / `ORDER BY` / `FILTER` / `OVER` / null-treatment / `count(*)` 特例）与窗口规格 helper。

**Files:**
- Modify: `internal/parser/formatter/expression_formatter.go`
- Create: `testdata/format/cases/slice2/function_*.sql`

参考 Java：`ExpressionFormatter.visitFunctionCall`(421)、`formatWindow`(1015)、`formatWindowSpecification`(1024)、`formatFrame`(1046)、`formatFrameBound`(1098)；`SqlFormatter.formatOrderBy`(753)、`formatSortItems`(758)、`sortItemFormatterFunction`(902)。

- [ ] **Step 1: 写用例**

`testdata/format/cases/slice2/function_basic.sql`:
```sql
SELECT count(*), sum(x), avg(DISTINCT y) FROM t
```

`testdata/format/cases/slice2/function_window.sql`:
```sql
SELECT rank() OVER (PARTITION BY a ORDER BY b DESC) FROM t
```

`testdata/format/cases/slice2/function_filter.sql`:
```sql
SELECT count(*) FILTER (WHERE x > 0) FROM t
```

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 3 个 `OK    slice2/function_*`。注意 `count(*)` 的特例：无参数且函数名为 `count` 时参数渲染为 `*`。

- [ ] **Step 3: 实现 `FunctionCall` 与窗口 helper**

在 `process` switch 插入 `case *ast.FunctionCall: return e.formatFunctionCall(n)`，并在文件末尾追加（`ast.FunctionCall` 字段见 `internal/parser/ast/expression.go`：`Name QualifiedName`、`Arguments []Expression`、`OrderBy []SortItem`、`Filter Expression`、`Window *Window`、`Distinct bool`、`IgnoreNulls bool`）：

```go
// formatFunctionCall mirrors trino ExpressionFormatter.visitFunctionCall for
// the DEFAULT dialect (BigQuery/DuckDB special-casing is out of P2 scope).
func (e *exprFormatter) formatFunctionCall(n *ast.FunctionCall) string {
	arguments := e.joinExpressions(n.Arguments)
	if len(n.Arguments) == 0 && strings.EqualFold(n.Name.Last(), "count") {
		arguments = "*"
	}
	if n.Distinct {
		arguments = "DISTINCT " + arguments
	}

	var b strings.Builder
	b.WriteString(formatName(n.Name, e.dialect))
	b.WriteString("(")
	b.WriteString(arguments)
	if len(n.OrderBy) > 0 {
		b.WriteString(" ")
		b.WriteString(e.formatOrderBy(n.OrderBy))
	}
	b.WriteString(")")

	if n.IgnoreNulls {
		b.WriteString(" IGNORE NULLS")
	}
	if n.Filter != nil {
		b.WriteString(" FILTER (WHERE ")
		b.WriteString(e.process(n.Filter))
		b.WriteString(")")
	}
	if n.Window != nil {
		b.WriteString(" OVER ")
		b.WriteString(e.formatWindowSpec(n.Window))
	}
	return b.String()
}

// joinExpressions renders a comma-separated list of expressions.
func (e *exprFormatter) joinExpressions(exprs []ast.Expression) string {
	parts := make([]string, len(exprs))
	for i, x := range exprs {
		parts[i] = e.process(x)
	}
	return strings.Join(parts, ", ")
}

// formatOrderBy renders "ORDER BY <items>". Mirrors trino formatOrderBy.
func (e *exprFormatter) formatOrderBy(items []ast.SortItem) string {
	return "ORDER BY " + e.formatSortItems(items)
}

// formatSortItems renders a comma-separated list of sort items.
func (e *exprFormatter) formatSortItems(items []ast.SortItem) string {
	parts := make([]string, len(items))
	for i := range items {
		parts[i] = e.formatSortItem(&items[i])
	}
	return strings.Join(parts, ", ")
}

// formatSortItem renders "<key> ASC|DESC [NULLS FIRST|LAST]". Mirrors trino
// sortItemFormatterFunction.
func (e *exprFormatter) formatSortItem(s *ast.SortItem) string {
	out := e.process(s.SortKey)
	if s.Ordering == ast.OrderingDesc {
		out += " DESC"
	} else {
		out += " ASC"
	}
	switch s.NullOrdering {
	case ast.NullOrderingFirst:
		out += " NULLS FIRST"
	case ast.NullOrderingLast:
		out += " NULLS LAST"
	}
	return out
}

// formatWindowSpec renders a window specification "( ... )". Mirrors trino
// formatWindowSpecification + formatFrame.
func (e *exprFormatter) formatWindowSpec(w *ast.Window) string {
	var parts []string
	if len(w.PartitionBy) > 0 {
		parts = append(parts, "PARTITION BY "+e.joinExpressions(w.PartitionBy))
	}
	if len(w.OrderBy) > 0 {
		parts = append(parts, e.formatOrderBy(w.OrderBy))
	}
	if w.Frame != nil {
		parts = append(parts, e.formatFrame(w.Frame))
	}
	return "(" + strings.Join(parts, " ") + ")"
}

// formatFrame renders a window frame. Mirrors trino formatFrame for the
// ROWS/RANGE BETWEEN ... AND ... shapes the corpus exercises.
func (e *exprFormatter) formatFrame(f *ast.WindowFrame) string {
	out := string(f.Type) + " "
	if f.End.Type != "" {
		return out + "BETWEEN " + e.formatFrameBound(&f.Start) +
			" AND " + e.formatFrameBound(&f.End)
	}
	return out + e.formatFrameBound(&f.Start)
}

// formatFrameBound renders one frame bound. Mirrors trino formatFrameBound.
func (e *exprFormatter) formatFrameBound(b *ast.FrameBound) string {
	switch b.Type {
	case ast.BoundTypeUnboundedPreceding:
		return "UNBOUNDED PRECEDING"
	case ast.BoundTypePreceding:
		return e.process(b.Value) + " PRECEDING"
	case ast.BoundTypeCurrentRow:
		return "CURRENT ROW"
	case ast.BoundTypeFollowing:
		return e.process(b.Value) + " FOLLOWING"
	case ast.BoundTypeUnboundedFollowing:
		return "UNBOUNDED FOLLOWING"
	default:
		panic(fmt.Sprintf("ExpressionFormatter: unhandled frame bound: %q", b.Type))
	}
}
```

注：`ast.WindowFrame` 的 `End` 是值类型 `FrameBound`（非指针），用 `f.End.Type != ""` 判断是否存在；若 `internal/parser/ast/query.go` 里 `End` 为 `*FrameBound` 指针，则改用 `f.End != nil`。先 Run: `grep -n "End " internal/parser/ast/query.go` 核对。

- [ ] **Step 4: 跑测试、接受基线、确认绿**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 3 个 `slice2/function_*` 报 `now passes`。无 `REGRESSION`。

Run: `make format-accept && make format-golden`
Expected: PASS。

若 `function_window.sql` 报 `parse-error` 或 `fail`：核对 Go parser 的 `VisitOver` / `VisitWindowFrame` 是否填齐 `Window` 结构；窗口由切片 2 覆盖。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/formatter/expression_formatter.go testdata/format
git commit -m "feat: format function calls and window specs (P2 slice 2)"
```

---

## Task 9: 切片 2 — CAST / CASE / IF / COALESCE / NULLIF

**Files:**
- Modify: `internal/parser/formatter/expression_formatter.go`
- Create: `testdata/format/cases/slice2/cast_case_*.sql`

参考 Java：`ExpressionFormatter` 的 `visitCast`(675)、`visitSearchedCaseExpression`(682)、`visitSimpleCaseExpression`(700)、`visitWhenClause`(719)、`visitIfExpression`(587)、`visitNullIfExpression`(581)、`visitCoalesceExpression`(610)。

- [ ] **Step 1: 写用例**

`testdata/format/cases/slice2/cast_case_cast.sql`:
```sql
SELECT CAST(x AS bigint), TRY_CAST(y AS varchar)
```

`testdata/format/cases/slice2/cast_case_searched.sql`:
```sql
SELECT CASE WHEN x > 0 THEN 'pos' WHEN x < 0 THEN 'neg' ELSE 'zero' END FROM t
```

`testdata/format/cases/slice2/cast_case_simple.sql`:
```sql
SELECT CASE x WHEN 1 THEN 'one' ELSE 'other' END FROM t
```

`testdata/format/cases/slice2/cast_case_misc.sql`:
```sql
SELECT COALESCE(a, b, 0), NULLIF(a, b)
```

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 4 个 `OK    slice2/cast_case_*`。注意 `CASE` 整体被一层括号包裹：`(CASE WHEN ... THEN ... ELSE ... END)`。

- [ ] **Step 3: 实现 CAST / CASE / IF / COALESCE / NULLIF visit**

在 `process` switch 的 `default` 前插入：

```go
	case *ast.Cast:
		kind := "CAST"
		if n.Safe {
			kind = "TRY_CAST"
		}
		return kind + "(" + e.process(n.Expression) + " AS " + e.formatType(&n.Type) + ")"
	case *ast.SearchedCaseExpression:
		return e.formatSearchedCase(n)
	case *ast.SimpleCaseExpression:
		return e.formatSimpleCase(n)
	case *ast.WhenClause:
		return "WHEN " + e.process(n.Operand) + " THEN " + e.process(n.Result)
	case *ast.IfExpression:
		out := "IF(" + e.process(n.Condition) + ", " + e.process(n.TrueValue)
		if n.FalseValue != nil {
			out += ", " + e.process(n.FalseValue)
		}
		return out + ")"
	case *ast.NullIfExpression:
		return "NULLIF(" + e.process(n.First) + ", " + e.process(n.Second) + ")"
	case *ast.CoalesceExpression:
		return "COALESCE(" + e.joinExpressions(n.Operands) + ")"
```

并在文件末尾追加：

```go
// formatSearchedCase renders "(CASE WHEN ... [ELSE ...] END)". Mirrors trino
// visitSearchedCaseExpression — the whole CASE is wrapped in parentheses.
func (e *exprFormatter) formatSearchedCase(n *ast.SearchedCaseExpression) string {
	parts := []string{"CASE"}
	for i := range n.WhenClauses {
		parts = append(parts, e.process(&n.WhenClauses[i]))
	}
	if n.DefaultValue != nil {
		parts = append(parts, "ELSE", e.process(n.DefaultValue))
	}
	parts = append(parts, "END")
	return "(" + strings.Join(parts, " ") + ")"
}

// formatSimpleCase renders "(CASE operand WHEN ... [ELSE ...] END)". Mirrors
// trino visitSimpleCaseExpression.
func (e *exprFormatter) formatSimpleCase(n *ast.SimpleCaseExpression) string {
	parts := []string{"CASE", e.process(n.Operand)}
	for i := range n.WhenClauses {
		parts = append(parts, e.process(&n.WhenClauses[i]))
	}
	if n.DefaultValue != nil {
		parts = append(parts, "ELSE", e.process(n.DefaultValue))
	}
	parts = append(parts, "END")
	return "(" + strings.Join(parts, " ") + ")"
}
```

- [ ] **Step 4: 跑测试、接受基线、确认绿**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 4 个 `slice2/cast_case_*` 与 `slice1/cast_type.sql` 报 `now passes`。无 `REGRESSION`。

Run: `make format-accept && make format-golden`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/formatter/expression_formatter.go testdata/format
git commit -m "feat: format cast, case, if, coalesce, nullif (P2 slice 2)"
```

---

## Task 10: 切片 2 — 谓词与其余表达式

移植 `In` / `Between` / `Like` / `IsNull` / `Row` / `Subscript` / `Extract` / `AtTimeZone` / `SubqueryExpression` / `Exists` / `Dereference` / `QuantifiedComparison`。

**Files:**
- Modify: `internal/parser/formatter/expression_formatter.go`
- Create: `testdata/format/cases/slice2/predicate_*.sql`

参考 Java：`ExpressionFormatter` 的 `visitInPredicate`(749)、`visitInListExpression`(755)、`visitBetweenPredicate`(742)、`visitLikePredicate`(631)、`visitIsNullPredicate`(556)、`visitIsNotNullPredicate`(562)、`visitExtract`(217)、`visitSubscriptExpression`(266)、`visitRow`(150)、`visitAtTimeZone`(170)、`visitSubqueryExpression`(372)、`visitExists`(378)、`visitDereferenceExpression`(421→`process(base)+"."+field`)、`visitQuantifiedComparisonExpression`(764)。

- [ ] **Step 1: 写用例**

`testdata/format/cases/slice2/predicate_in.sql`:
```sql
SELECT a FROM t WHERE x IN (1, 2, 3)
```

`testdata/format/cases/slice2/predicate_between.sql`:
```sql
SELECT a FROM t WHERE x BETWEEN 1 AND 10
```

`testdata/format/cases/slice2/predicate_like.sql`:
```sql
SELECT a FROM t WHERE name LIKE '%abc%'
```

`testdata/format/cases/slice2/predicate_null.sql`:
```sql
SELECT a FROM t WHERE x IS NULL AND y IS NOT NULL
```

`testdata/format/cases/slice2/predicate_extract.sql`:
```sql
SELECT EXTRACT(YEAR FROM order_date) FROM orders
```

`testdata/format/cases/slice2/predicate_dereference.sql`:
```sql
SELECT t.col FROM t
```

`testdata/format/cases/slice2/predicate_exists.sql`:
```sql
SELECT a FROM t WHERE EXISTS (SELECT 1 FROM s WHERE s.id = t.id)
```

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 7 个 `OK    slice2/predicate_*`。`predicate_in/between/like/null/exists` 的谓词均带外层括号（`(x IN (1, 2, 3))` 等）。

- [ ] **Step 3: 实现谓词与其余表达式 visit**

在 `process` switch 的 `default` 前插入：

```go
	case *ast.InPredicate:
		return "(" + e.process(n.Value) + " IN " + e.process(n.ValueList) + ")"
	case *ast.InListExpression:
		return "(" + e.joinExpressions(n.Values) + ")"
	case *ast.BetweenPredicate:
		return "(" + e.process(n.Value) + " BETWEEN " +
			e.process(n.Min) + " AND " + e.process(n.Max) + ")"
	case *ast.LikePredicate:
		out := "(" + e.process(n.Value) + " LIKE " + e.process(n.Pattern)
		if n.Escape != nil {
			out += " ESCAPE " + e.process(n.Escape)
		}
		return out + ")"
	case *ast.IsNullPredicate:
		if n.Not {
			return "(" + e.process(n.Value) + " IS NOT NULL)"
		}
		return "(" + e.process(n.Value) + " IS NULL)"
	case *ast.ExtractExpression:
		return "EXTRACT(" + n.Field + " FROM " + e.process(n.Expression) + ")"
	case *ast.SubscriptExpression:
		return e.process(n.Base) + "[" + e.process(n.Index) + "]"
	case *ast.Row:
		return "ROW (" + e.joinExpressions(n.Items) + ")"
	case *ast.AtTimeZone:
		return e.process(n.Value) + " AT TIME ZONE " + e.process(n.TimeZone)
	case *ast.DereferenceExpression:
		base := e.process(n.Base)
		if n.Field == nil {
			return base + ".*"
		}
		return base + "." + e.process(n.Field)
	case *ast.SubqueryExpression:
		return "(" + FormatSQLDialect(n.Query, e.dialect) + ")"
	case *ast.ExistsPredicate:
		return "(EXISTS " + FormatSQLDialect(n.Subquery, e.dialect) + ")"
	case *ast.QuantifiedComparison:
		return "(" + e.process(n.Value) + " " + string(n.Operator) + " " +
			n.Quantifier + " " + e.process(n.Subquery) + ")"
	case *ast.StarExpression:
		return "*"
```

注意几处与现有 Go AST 字段对齐：
- `InPredicate.Not` / `BetweenPredicate.Not` / `LikePredicate.Not`：Task 6 已让 AstBuilder 把 `NOT` 包成 `NotExpression`，故 parse 得到的这些节点 `Not` 恒为 `false`，此处不渲染 `Not`。
- `IsNullPredicate.Not`：Go 用单节点带 `Not` 表示 `IS NULL` / `IS NOT NULL`（与 trino 拆成两节点字节等价），保留分支。
- `ExistsPredicate` / `QuantifiedComparison`：若 Task 6 Step 1 发现 Go AST 已有同义节点，按其实际字段名调整本段。

- [ ] **Step 4: 跑测试、接受基线、确认绿**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 7 个 `slice2/predicate_*` 报 `now passes`。无 `REGRESSION`。

若某谓词用例 `fail`：用 `--- want ---` / `--- got ---` 定位。常见原因是 Go parser 把 `NOT` 谓词产出成带 `Not=true` 的节点而非 `NotExpression`——回到 Task 6 核对 `VisitPredicated`。

Run: `make format-accept && make format-golden`
Expected: PASS。

- [ ] **Step 5: 验证编译干净**

Run: `gofmt -l internal/parser/ && go vet ./internal/parser/...`
Expected: 均无输出 / 退出码 0。

- [ ] **Step 6: Commit**

```bash
git add internal/parser/formatter/expression_formatter.go testdata/format
git commit -m "feat: format predicates and remaining expressions (P2 slice 2)"
```

---

## Task 11: 切片 3 — 关系 AST 节点补齐

补 Go AST 缺失的关系节点：`Lateral` / `SampledRelation` / `FunctionRelation`，并扩 AstBuilder。

**Files:**
- Modify: `internal/parser/ast/relation.go`
- Modify: `internal/parser/ast_builder.go`

参考 Java：`SqlFormatter` 的 `visitLateral`(283)、`visitSampledRelation`(665)、`visitFunctionRelation`(234)；`AstBuilder` 的 `visitLateral` / `visitSampledRelation` / `visitTableFunctionInvocation`（按需 grep）。

- [ ] **Step 1: 检查 Go AST 已有哪些关系节点**

Run: `grep -n "^type .* struct\|isRelation" internal/parser/ast/relation.go`
Expected: 现有 `Table` / `AliasedRelation` / `Join` / `TableSubquery` / `Unnest` / `Values`。`Lateral` / `SampledRelation` / `FunctionRelation` 缺失。

- [ ] **Step 2: 新增缺失的关系节点**

在 `internal/parser/ast/relation.go` 末尾追加：

```go
// Lateral represents LATERAL (subquery) in a FROM clause.
type Lateral struct {
	BaseNode
	Query Statement
}

func (l *Lateral) GetChildren() []Node { return []Node{l.Query} }
func (l *Lateral) isRelation()         {}

// SampledRelation represents <relation> TABLESAMPLE <type> (<percentage>).
type SampledRelation struct {
	BaseNode
	Relation         Relation
	SampleType       string // "BERNOULLI" | "SYSTEM"
	SamplePercentage Expression
}

func (s *SampledRelation) GetChildren() []Node { return []Node{s.Relation} }
func (s *SampledRelation) isRelation()         {}

// FunctionRelation represents a table function invocation name(args...) in FROM.
type FunctionRelation struct {
	BaseNode
	Name      QualifiedName
	Arguments []Expression
}

func (f *FunctionRelation) GetChildren() []Node {
	children := make([]Node, len(f.Arguments))
	for i, a := range f.Arguments {
		children[i] = a
	}
	return children
}
func (f *FunctionRelation) isRelation() {}
```

- [ ] **Step 3: 扩 AstBuilder 的关系 visitor**

在 `ast_builder.go` 检查是否已有 `VisitLateral` / `VisitFunctionRelation`。Run: `grep -n "VisitLateral\|VisitFunctionRelation\|VisitSampledRelation" internal/parser/ast_builder.go`。`VisitSampledRelation` 已存在（但可能只填了部分字段）。对缺失者，逐字移植对应 Java `AstBuilder` 方法，产出 Step 2 的新节点。`VisitSampledRelation` 现状若已产出 `SampledRelation` 即可；若产出别的类型，按 Step 2 的结构对齐。

注：`Lateral` / `SampledRelation` / `FunctionRelation` 不在 TPC-H 语料中，本任务的 AST/AstBuilder 改动靠切片 3 的 snippet 用例（Task 14）验证；此处只保证 `go build` 通过。

- [ ] **Step 4: 验证编译**

Run: `go build ./...`
Expected: 退出码 0。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/ast/relation.go internal/parser/ast_builder.go
git commit -m "feat: add Lateral, SampledRelation, FunctionRelation AST nodes (P2 slice 3)"
```

---

## Task 12: 切片 3 — 表 / 别名关系 / 子查询关系

移植 `visitTable` / `visitAliasedRelation` / `processRelationSuffix` / `visitTableSubquery`。

**Files:**
- Modify: `internal/parser/formatter/formatter.go`
- Create: `testdata/format/cases/slice3/relation_*.sql`

参考 Java：`SqlFormatter` 的 `visitTable`(539)、`visitAliasedRelation`(596)、`processRelationSuffix`(693)、`visitTableSubquery`(727)、`appendAliasColumns`(1987)。

- [ ] **Step 1: 写用例**

`testdata/format/cases/slice3/relation_table.sql`:
```sql
SELECT a FROM catalog.schema.orders
```

`testdata/format/cases/slice3/relation_aliased.sql`:
```sql
SELECT a FROM orders o
```

`testdata/format/cases/slice3/relation_subquery.sql`:
```sql
SELECT a FROM (SELECT a FROM t) sub
```

`testdata/format/cases/slice3/relation_alias_cols.sql`:
```sql
SELECT x FROM (SELECT a FROM t) sub (x)
```

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 4 个 `OK    slice3/relation_*`。注意 `visitTableSubquery` 产出多行：`(\n` + 缩进的子查询 + `\n) `。

- [ ] **Step 3: 实现表 / 别名 / 子查询关系 visit**

把 `formatter.go` 的 `process` switch 补成（在 `default` 前插入这些 case）：

```go
	case *ast.Table:
		f.builder.WriteString(formatName(n.Name, f.dialect))
	case *ast.AliasedRelation:
		f.visitAliasedRelation(n, indent)
	case *ast.TableSubquery:
		f.visitTableSubquery(n, indent)
```

并在 `formatter.go` 末尾追加：

```go
// visitAliasedRelation mirrors trino SqlFormatter.visitAliasedRelation.
func (f *formatter) visitAliasedRelation(n *ast.AliasedRelation, indent int) {
	f.processRelationSuffix(n.Relation, indent)
	f.builder.WriteString(" ")
	f.builder.WriteString(formatExpression(n.Alias, f.dialect))
	f.appendAliasColumns(n.ColumnNames)
}

// processRelationSuffix wraps a relation in "( ... )" when it needs grouping
// (aliased / sampled / join), otherwise processes it directly. Mirrors trino
// SqlFormatter.processRelationSuffix.
func (f *formatter) processRelationSuffix(relation ast.Relation, indent int) {
	switch relation.(type) {
	case *ast.AliasedRelation, *ast.SampledRelation, *ast.Join:
		f.builder.WriteString("( ")
		f.process(relation, indent+1)
		f.append(indent, ")")
	default:
		f.process(relation, indent)
	}
}

// visitTableSubquery mirrors trino SqlFormatter.visitTableSubquery.
func (f *formatter) visitTableSubquery(n *ast.TableSubquery, indent int) {
	f.builder.WriteString("(\n")
	f.process(n.Query, indent+1)
	f.append(indent, ") ")
}

// appendAliasColumns appends " (col, col, ...)" when columns is non-empty.
// Mirrors trino SqlFormatter.appendAliasColumns.
func (f *formatter) appendAliasColumns(columns []ast.Identifier) {
	if len(columns) == 0 {
		return
	}
	parts := make([]string, len(columns))
	for i := range columns {
		parts[i] = formatExpression(&columns[i], f.dialect)
	}
	f.builder.WriteString(" (")
	f.builder.WriteString(strings.Join(parts, ", "))
	f.builder.WriteString(")")
}
```

注：Java `visitTable` 还处理 `queryPeriod`（`FOR ... AS OF ...`），不在 P2 语料中，省略。`ast.Table.Alias` 字段在 Go 里存在，但 trino 的 `Table` 节点**不带别名**——表别名一律由外层 `AliasedRelation` 承载。Go parser 若把 `orders o` 解析成带 `Alias` 的 `Table` 而非 `AliasedRelation`，`relation_aliased.sql` 会 `fail`；届时回到 `ast_builder.go` 的 `VisitTableName` / `VisitSampledRelation` 核对：别名必须产出 `AliasedRelation` 包 `Table`。

- [ ] **Step 4: 跑测试、接受基线、确认绿**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 4 个 `slice3/relation_*` 现为 `parse-error` 或 `panic`（语句层 `Query`/`Select` 尚未实现，会在切片 4 落地）。**本切片关系 visit 已就位，但要等切片 4 的 `Query`/`QuerySpecification`/`Select` 才能端到端跑通。** 故此处仅确认无 `REGRESSION`，baseline 记录现状。

Run: `make format-accept && make format-golden`
Expected: PASS（`baseline consistent`）。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/formatter/formatter.go testdata/format
git commit -m "feat: format table, aliased and subquery relations (P2 slice 3)"
```

---

## Task 13: 切片 3 — JOIN

移植 `visitJoin`（INNER/LEFT/RIGHT/FULL/CROSS/IMPLICIT + `ON`/`USING`/`NATURAL`）。

**Files:**
- Modify: `internal/parser/formatter/formatter.go`
- Create: `testdata/format/cases/slice3/join_*.sql`

参考 Java：`SqlFormatter.visitJoin`(556)。

关键字节事实：`visitJoin` 先 `process(left)`，再 `\n`，再 `append(indent, type)` + ` JOIN `（IMPLICIT 则 `append(indent, ", ")`），再 `process(right)`，最后 `ON`/`USING`。`type` 取 `node.getType().toString()`（`INNER`/`LEFT`/`RIGHT`/`FULL`/`CROSS`），`NaturalJoin` 时前缀 `NATURAL `。

- [ ] **Step 1: 写用例**

`testdata/format/cases/slice3/join_on.sql`:
```sql
SELECT a FROM orders o JOIN lineitem l ON o.orderkey = l.orderkey
```

`testdata/format/cases/slice3/join_left.sql`:
```sql
SELECT a FROM orders o LEFT JOIN customer c ON o.custkey = c.custkey
```

`testdata/format/cases/slice3/join_using.sql`:
```sql
SELECT a FROM orders JOIN lineitem USING (orderkey)
```

`testdata/format/cases/slice3/join_cross.sql`:
```sql
SELECT a FROM t1 CROSS JOIN t2
```

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 4 个 `OK    slice3/join_*`。

- [ ] **Step 3: 实现 `visitJoin`**

在 `process` switch 插入 `case *ast.Join: f.visitJoin(n, indent)`，并在 `formatter.go` 末尾追加：

```go
// visitJoin mirrors trino SqlFormatter.visitJoin.
func (f *formatter) visitJoin(n *ast.Join, indent int) {
	joinType := string(n.JoinType) // CROSS / INNER / LEFT / RIGHT / FULL
	if _, natural := n.Criteria.(*ast.NaturalJoin); natural {
		joinType = "NATURAL " + joinType
	}

	f.process(n.Left, indent)
	f.builder.WriteString("\n")
	if n.JoinType == ast.JoinTypeImplicit {
		f.append(indent, ", ")
	} else {
		f.append(indent, joinType)
		f.builder.WriteString(" JOIN ")
	}
	f.process(n.Right, indent)

	if n.JoinType == ast.JoinTypeCross || n.JoinType == ast.JoinTypeImplicit {
		return
	}
	switch c := n.Criteria.(type) {
	case *ast.JoinUsing:
		cols := make([]string, len(c.Columns))
		for i := range c.Columns {
			cols[i] = c.Columns[i].Value
		}
		f.builder.WriteString(" USING (")
		f.builder.WriteString(strings.Join(cols, ", "))
		f.builder.WriteString(")")
	case *ast.JoinOn:
		f.builder.WriteString(" ON ")
		f.builder.WriteString(formatExpression(c.Expression, f.dialect))
	case *ast.NaturalJoin, nil:
		// no criteria suffix
	default:
		panic(fmt.Sprintf("SqlFormatter: unknown join criteria: %T", c))
	}
}
```

注：Java `visitJoin` 的 `USING` 用 `Joiner.on(", ").join(using.getColumns())`——`using.getColumns()` 是 `List<Identifier>`，`Identifier.toString()` 直接给名字（非引用形式）。Go 这里用 `c.Columns[i].Value` 与之对齐。`ast.JoinType` 常量值（`"INNER"`/`"LEFT"`/...）见 `internal/parser/ast/relation.go`，与 Java `Join.Type.toString()` 一致。

- [ ] **Step 4: 跑测试、接受基线、确认绿**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 4 个 `slice3/join_*` 仍 `parse-error`/`panic`（等切片 4 的语句层）。无 `REGRESSION`。

Run: `make format-accept && make format-golden`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/formatter/formatter.go testdata/format
git commit -m "feat: format joins (P2 slice 3)"
```

---

## Task 14: 切片 3 — UNNEST / LATERAL / SAMPLED / FUNCTION 关系

**Files:**
- Modify: `internal/parser/formatter/formatter.go`
- Create: `testdata/format/cases/slice3/rel_unnest.sql`, `rel_values.sql`

参考 Java：`SqlFormatter` 的 `visitUnnest`(248)、`visitLateral`(283)、`visitSampledRelation`(665)、`visitFunctionRelation`(234)、`visitValues`(709)。

- [ ] **Step 1: 写用例**

`testdata/format/cases/slice3/rel_unnest.sql`:
```sql
SELECT x FROM UNNEST(my_array) AS t (x)
```

`testdata/format/cases/slice3/rel_values.sql`:
```sql
SELECT a FROM (VALUES (1), (2), (3)) AS t (a)
```

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 2 个 `OK    slice3/rel_*`。

- [ ] **Step 3: 实现 UNNEST / LATERAL / SAMPLED / FUNCTION / VALUES visit**

在 `process` switch 插入：

```go
	case *ast.Unnest:
		f.visitUnnest(n)
	case *ast.Lateral:
		f.visitLateral(n, indent)
	case *ast.SampledRelation:
		f.visitSampledRelation(n, indent)
	case *ast.FunctionRelation:
		f.visitFunctionRelation(n)
	case *ast.Values:
		f.visitValues(n, indent)
```

并在 `formatter.go` 末尾追加：

```go
// visitUnnest mirrors trino SqlFormatter.visitUnnest (DEFAULT dialect).
func (f *formatter) visitUnnest(n *ast.Unnest) {
	parts := make([]string, len(n.Expressions))
	for i, x := range n.Expressions {
		parts[i] = formatExpression(x, f.dialect)
	}
	f.builder.WriteString("UNNEST(")
	f.builder.WriteString(strings.Join(parts, ", "))
	f.builder.WriteString(")")
	if n.Ordinality {
		f.builder.WriteString(" WITH ORDINALITY")
	}
}

// visitLateral mirrors trino SqlFormatter.visitLateral.
func (f *formatter) visitLateral(n *ast.Lateral, indent int) {
	f.append(indent, "LATERAL (")
	f.process(n.Query, indent+1)
	f.append(indent, ")")
}

// visitSampledRelation mirrors trino SqlFormatter.visitSampledRelation.
func (f *formatter) visitSampledRelation(n *ast.SampledRelation, indent int) {
	f.processRelationSuffix(n.Relation, indent)
	f.builder.WriteString(" TABLESAMPLE ")
	f.builder.WriteString(n.SampleType)
	f.builder.WriteString(" (")
	f.builder.WriteString(formatExpression(n.SamplePercentage, f.dialect))
	f.builder.WriteString(")")
}

// visitFunctionRelation mirrors trino SqlFormatter.visitFunctionRelation.
func (f *formatter) visitFunctionRelation(n *ast.FunctionRelation) {
	parts := make([]string, len(n.Arguments))
	for i, a := range n.Arguments {
		parts[i] = formatExpression(a, f.dialect)
	}
	f.builder.WriteString(formatName(n.Name, f.dialect))
	f.builder.WriteString("(")
	f.builder.WriteString(strings.Join(parts, ", "))
	f.builder.WriteString(")")
}

// visitValues mirrors trino SqlFormatter.visitValues: each row on its own line.
func (f *formatter) visitValues(n *ast.Values, indent int) {
	f.builder.WriteString(" VALUES ")
	for i, row := range n.Rows {
		f.builder.WriteString("\n")
		f.builder.WriteString(f.indentString(indent))
		if i == 0 {
			f.builder.WriteString("  ")
		} else {
			f.builder.WriteString(", ")
		}
		if len(row) == 1 {
			if r, ok := row[0].(*ast.Row); ok {
				f.builder.WriteString(formatExpression(r, f.dialect))
				continue
			}
		}
		parts := make([]string, len(row))
		for j, x := range row {
			parts[j] = formatExpression(x, f.dialect)
		}
		f.builder.WriteString("(")
		f.builder.WriteString(strings.Join(parts, ", "))
		f.builder.WriteString(")")
	}
	f.builder.WriteString("\n")
}
```

注：Java `visitValues` 的逐行结构是 `getRows()` 每行一个 `Expression`（通常为 `Row` 节点）。Go 的 `ast.Values.Rows` 是 `[][]Expression`——每行一个表达式切片。上面的 `visitValues` 对"单元素且为 `Row`"沿用 `Row` 渲染、否则包一层括号，与 Java 的 `row instanceof Row` 分支等价。捕获 `rel_values.sql` 的 golden 后若字节不符，按 `--- want ---` 对照修正（最可能是 Go parser 把 `VALUES (1)` 的每行建成单元素 `[]Expression{LongLiteral}` 而非 `Row`——此时上面的 else 分支输出 `(1)`，与 Java 一致）。

- [ ] **Step 4: 跑测试、接受基线、确认绿**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: `slice3/rel_*` 仍 `parse-error`/`panic`（等切片 4）。无 `REGRESSION`。

Run: `make format-accept && make format-golden`
Expected: PASS。

- [ ] **Step 5: 验证编译干净**

Run: `gofmt -l internal/parser/ && go vet ./internal/parser/... && go build ./...`
Expected: 均干净。

- [ ] **Step 6: Commit**

```bash
git add internal/parser/formatter/formatter.go testdata/format
git commit -m "feat: format unnest, lateral, sampled, function relations (P2 slice 3)"
```

---

## Task 15: 切片 4 — SELECT 子句

移植 `visitSelect`（**单列 vs 多列布局分叉**）、`visitSingleColumn`、`visitAllColumns`。

**Files:**
- Modify: `internal/parser/formatter/formatter.go`
- Create: `testdata/format/cases/slice4/select_*.sql`

参考 Java：`SqlFormatter` 的 `visitSelect`(481)、`visitSingleColumn`(510)、`visitAllColumns`(521)。

关键字节事实（`visitSelect`）：先 `append(indent, "SELECT")`，`DISTINCT` 则补 ` DISTINCT`。**多于 1 列**：每列前 `"\n" + indentString(indent)`，首列再加 `"  "`、后续加 `", "`。**恰 1 列**：补一个空格 + 该列。最后统一补 `'\n'`。

- [ ] **Step 1: 写用例**

`testdata/format/cases/slice4/select_one_col.sql`:
```sql
SELECT a FROM t
```

`testdata/format/cases/slice4/select_multi_col.sql`:
```sql
SELECT a, b, c FROM t
```

`testdata/format/cases/slice4/select_distinct.sql`:
```sql
SELECT DISTINCT a, b FROM t
```

`testdata/format/cases/slice4/select_star.sql`:
```sql
SELECT * FROM t
```

`testdata/format/cases/slice4/select_alias.sql`:
```sql
SELECT a AS x, b col2 FROM t
```

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 5 个 `OK    slice4/select_*`。

- [ ] **Step 3: 实现 SELECT 子句 visit**

在 `process` switch 插入：

```go
	case *ast.Select:
		f.visitSelect(n, indent)
	case *ast.SingleColumn:
		f.visitSingleColumn(n)
	case *ast.AllColumns:
		f.visitAllColumns(n)
```

并在 `formatter.go` 末尾追加：

```go
// visitSelect mirrors trino SqlFormatter.visitSelect — note the one-column vs
// multi-column layout fork.
func (f *formatter) visitSelect(n *ast.Select, indent int) {
	f.append(indent, "SELECT")
	if n.Distinct {
		f.builder.WriteString(" DISTINCT")
	}
	if len(n.SelectItems) > 1 {
		for i, item := range n.SelectItems {
			f.builder.WriteString("\n")
			f.builder.WriteString(f.indentString(indent))
			if i == 0 {
				f.builder.WriteString("  ")
			} else {
				f.builder.WriteString(", ")
			}
			f.process(item, indent)
		}
	} else {
		f.builder.WriteString(" ")
		f.process(n.SelectItems[0], indent)
	}
	f.builder.WriteString("\n")
}

// visitSingleColumn mirrors trino SqlFormatter.visitSingleColumn.
func (f *formatter) visitSingleColumn(n *ast.SingleColumn) {
	f.builder.WriteString(formatExpression(n.Expression, f.dialect))
	if n.Alias != nil {
		f.builder.WriteString(" ")
		f.builder.WriteString(formatExpression(n.Alias, f.dialect))
	}
}

// visitAllColumns mirrors trino SqlFormatter.visitAllColumns ("t.*" / "*").
func (f *formatter) visitAllColumns(n *ast.AllColumns) {
	if n.QualifiedName != nil {
		f.builder.WriteString(formatName(*n.QualifiedName, f.dialect))
		f.builder.WriteString(".")
	}
	f.builder.WriteString("*")
}
```

注：Java `visitSingleColumn` 的别名前缀是单空格（` ` + alias），**不是** ` AS `——`a AS x` 与 `b col2` 都渲染成 `a x` / `b col2`（`AS` 关键字不出现在 formatter 输出里）。`select_alias.sql` 的 golden 会确认这一点。

- [ ] **Step 4: 跑测试、接受基线、确认绿**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: `slice4/select_*` 仍 `parse-error`/`panic`（缺 `Query`/`QuerySpecification`）。无 `REGRESSION`。

Run: `make format-accept && make format-golden`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/formatter/formatter.go testdata/format
git commit -m "feat: format SELECT clause with one/multi-column fork (P2 slice 4)"
```

---

## Task 16: 切片 4 — QuerySpecification 与子句

移植 `visitQuerySpecification`（`FROM` 自带换行、`WHERE`/`GROUP BY`/`HAVING`/`WINDOW`/`ORDER BY`/`OFFSET`/`LIMIT`），以及 `formatGroupBy`、`formatDefinitionList`。

**Files:**
- Modify: `internal/parser/formatter/formatter.go`
- Create: `testdata/format/cases/slice4/qs_*.sql`

参考 Java：`SqlFormatter` 的 `visitQuerySpecification`(403)、`visitOrderBy`(443)、`visitOffset`(451)、`visitLimit`(471)、`formatGroupBy`(871)、`formatGroupingSet`(914)、`formatDefinitionList`(1968)。

关键字节事实（`visitQuerySpecification`）：`process(select)` → 若有 `FROM`：`append(indent,"FROM")` + `'\n'` + `append(indent,"  ")` + `process(from)` → 统一 `'\n'` → `WHERE`/`GROUP BY`/`HAVING` 各 `append(indent, "...")` + `'\n'` → `WINDOW` → `ORDER BY` → `OFFSET` 然后 `LIMIT`（DEFAULT 方言顺序）。

- [ ] **Step 1: 写用例**

`testdata/format/cases/slice4/qs_where.sql`:
```sql
SELECT a FROM t WHERE x > 0
```

`testdata/format/cases/slice4/qs_groupby.sql`:
```sql
SELECT k, count(*) FROM t GROUP BY k
```

`testdata/format/cases/slice4/qs_having.sql`:
```sql
SELECT k, count(*) FROM t GROUP BY k HAVING count(*) > 1
```

`testdata/format/cases/slice4/qs_orderby_limit.sql`:
```sql
SELECT a FROM t ORDER BY a DESC LIMIT 10
```

`testdata/format/cases/slice4/qs_grouping_sets.sql`:
```sql
SELECT a, b FROM t GROUP BY GROUPING SETS ((a), (b), ())
```

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 5 个 `OK    slice4/qs_*`。

- [ ] **Step 3: 实现 `visitQuerySpecification` 与子句 helper**

在 `process` switch 插入 `case *ast.QuerySpecification: f.visitQuerySpecification(n, indent)`，并在 `formatter.go` 末尾追加：

```go
// visitQuerySpecification mirrors trino SqlFormatter.visitQuerySpecification.
func (f *formatter) visitQuerySpecification(n *ast.QuerySpecification, indent int) {
	f.process(n.Select, indent)

	if n.From != nil {
		f.append(indent, "FROM")
		f.builder.WriteString("\n")
		f.append(indent, "  ")
		f.process(n.From, indent)
	}
	f.builder.WriteString("\n")

	if n.Where != nil {
		f.append(indent, "WHERE "+formatExpression(n.Where, f.dialect))
		f.builder.WriteString("\n")
	}
	if n.GroupBy != nil {
		f.append(indent, "GROUP BY "+f.formatGroupBy(n.GroupBy))
		f.builder.WriteString("\n")
	}
	if n.Having != nil {
		f.append(indent, "HAVING "+formatExpression(n.Having, f.dialect))
		f.builder.WriteString("\n")
	}

	f.appendOrderBy(n.OrderBy, indent)
	f.appendOffset(n.Offset, indent)
	f.appendLimit(n.Limit, indent)
}

// appendOrderBy emits an ORDER BY line when items is non-empty. Mirrors trino
// SqlFormatter.visitOrderBy.
func (f *formatter) appendOrderBy(items []ast.SortItem, indent int) {
	if len(items) == 0 {
		return
	}
	ef := &exprFormatter{dialect: f.dialect}
	f.append(indent, ef.formatOrderBy(items))
	f.builder.WriteString("\n")
}

// appendOffset emits an OFFSET line. Mirrors trino SqlFormatter.visitOffset
// (DEFAULT dialect: an extra "ROWS" line follows).
func (f *formatter) appendOffset(offset ast.Expression, indent int) {
	if offset == nil {
		return
	}
	f.append(indent, "OFFSET "+formatExpression(offset, f.dialect))
	f.builder.WriteString("\n")
	f.append(indent, "ROWS\n")
}

// appendLimit emits a LIMIT line. Mirrors trino SqlFormatter.visitLimit.
func (f *formatter) appendLimit(limit ast.Expression, indent int) {
	if limit == nil {
		return
	}
	f.append(indent, "LIMIT "+formatExpression(limit, f.dialect))
	f.builder.WriteString("\n")
}

// formatGroupBy renders the grouping elements. Mirrors trino formatGroupBy.
// The Go AST GroupBy carries Expressions plus a Sets flag (true for GROUPING
// SETS); Cube/Rollup are not in the P2 corpus.
func (f *formatter) formatGroupBy(g *ast.GroupBy) string {
	ef := &exprFormatter{dialect: f.dialect}
	if !g.Sets {
		parts := make([]string, len(g.Expressions))
		for i, x := range g.Expressions {
			parts[i] = ef.process(x)
		}
		return strings.Join(parts, ", ")
	}
	// GROUPING SETS: each element is itself a parenthesized list. The Go AST
	// models each set as a *Row of its grouping columns.
	sets := make([]string, len(g.Expressions))
	for i, x := range g.Expressions {
		if r, ok := x.(*ast.Row); ok {
			cols := make([]string, len(r.Items))
			for j, it := range r.Items {
				cols[j] = ef.process(it)
			}
			sets[i] = "(" + strings.Join(cols, ", ") + ")"
		} else {
			sets[i] = "(" + ef.process(x) + ")"
		}
	}
	return "GROUPING SETS (" + strings.Join(sets, ", ") + ")"
}
```

注：`ast.GroupBy` 的实际字段见 `internal/parser/ast/statement.go`（`Expressions []Expression`、`Sets bool`——无 `Distinct` 字段，故不渲染 `GROUP BY DISTINCT`；该构造不在 P2 语料中）。`qs_grouping_sets.sql` 捕获 golden 后，若 Go parser 对 `GROUPING SETS` 的建模与上面假设（每个 set 为 `*Row`）不符，按 `--- want ---` 对照、并核对 `ast_builder.go` 的 `VisitGroupingSet` / `VisitSingleGroupingSet` 实际产出来修正 `formatGroupBy` 的分支。

**关于 `FETCH FIRST`（spec §2 IN 项）**：Go AST 的 `Query` / `QuerySpecification` 把行数限制建模为 `Limit Expression` / `Offset Expression`，无 `FetchFirst` 节点；`FETCH FIRST` 既不在 TPC-H 22 条、也不在本计划的 snippet 语料中。本任务先实现 `OFFSET` / `LIMIT`。`fetch_first.sql` 暂不纳入语料——理由与 spec §2 对 OUT 项的 YAGNI 论证一致（"移了也验证不了"）。若后续切片的某条 SQL 触发 `FETCH FIRST` 的 `parse-error`，再按 trino `visitFetchFirst`(459) 补 `FetchFirst` AST 节点 + AstBuilder visitor + `appendFetchFirst` helper。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: `slice4/qs_*` 仍因缺 `Query` 而 `parse-error`/`panic`（`QuerySpecification` 通常被 `Query` 包裹）。无 `REGRESSION`。Task 17 接上 `Query` 后这些用例端到端跑通。

Run: `make format-accept && make format-golden`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/formatter/formatter.go testdata/format
git commit -m "feat: format QuerySpecification and clauses (P2 slice 4)"
```

---

## Task 17: 切片 4 — Query / WITH，并跑通 TPC-H 22

移植 `visitQuery`（`WITH` + `RECURSIVE`、`processRelation`、`ORDER BY`/`OFFSET`/`LIMIT`），接上语句层后所有 slice2/3/4 用例端到端跑通，并加入 TPC-H 22 条全量验收。

**Files:**
- Modify: `internal/parser/formatter/formatter.go`
- Create: `testdata/format/cases/slice4/with_cte.sql`
- Create: `testdata/format/cases/tpch/1.sql` … `22.sql`（拷自 P1 语料）

参考 Java：`SqlFormatter` 的 `visitQuery`(367)、`processRelation`(1944)。

关键字节事实（`visitQuery`）：有 `WITH` 时 → `append(indent,"WITH")` → `RECURSIVE` 补 ` RECURSIVE` → `"\n  "` → 每个 CTE：`append(indent, formatExpression(name))` + 可选列名 + `" AS "` + `process(TableSubquery(query), indent)` + `'\n'`，**非末项再补 `", "`**。然后 `processRelation(queryBody, indent)` → `ORDER BY` → `OFFSET` → `LIMIT`。

`processRelation`：`Table` 特例为 `"TABLE " + name + '\n'`，否则 `process(relation, indent)`。

- [ ] **Step 1: 写 WITH 用例并拷入 TPC-H 语料**

Create `testdata/format/cases/slice4/with_cte.sql`:
```sql
WITH revenue AS (SELECT a FROM t) SELECT a FROM revenue
```

```bash
mkdir -p testdata/format/cases/tpch
cp testdata/difftest/cases/tpch/queries/*.sql testdata/format/cases/tpch/
```

Run: `ls testdata/format/cases/tpch/*.sql | wc -l`
Expected: `22`

- [ ] **Step 2: 捕获 golden**

Run: `make capture-format-golden`
Expected: 新增 `OK    slice4/with_cte.sql` 与 22 个 `OK    tpch/N.sql`。若个别 TPC-H 用例为 `ERROR`：Java 对该 SQL parse/format 报错，记录其 `.golden.error`，差分测试把它归 `oracle-error`、不阻塞。

- [ ] **Step 3: 实现 `visitQuery` 与 `processRelation`**

在 `process` switch 插入 `case *ast.Query: f.visitQuery(n, indent)`，并在 `formatter.go` 末尾追加：

```go
// visitQuery mirrors trino SqlFormatter.visitQuery.
func (f *formatter) visitQuery(n *ast.Query, indent int) {
	if n.With != nil {
		f.append(indent, "WITH")
		if n.With.Recursive {
			f.builder.WriteString(" RECURSIVE")
		}
		f.builder.WriteString("\n  ")
		for i := range n.With.Queries {
			q := &n.With.Queries[i]
			f.append(indent, formatExpression(q.Name, f.dialect))
			f.appendAliasColumns(q.ColumnNames)
			f.builder.WriteString(" AS ")
			f.visitTableSubquery(&ast.TableSubquery{Query: q.Query}, indent)
			f.builder.WriteString("\n")
			if i < len(n.With.Queries)-1 {
				f.builder.WriteString(", ")
			}
		}
	}

	f.processRelation(n.Body, indent)
	f.appendOrderBy(n.OrderBy, indent)
	f.appendOffset(n.Offset, indent)
	f.appendLimit(n.Limit, indent)
}

// processRelation mirrors trino SqlFormatter.processRelation: a bare Table
// gets the "TABLE name" shorthand, everything else is processed normally.
func (f *formatter) processRelation(node ast.Node, indent int) {
	if t, ok := node.(*ast.Table); ok {
		f.builder.WriteString("TABLE ")
		f.builder.WriteString(formatName(t.Name, f.dialect))
		f.builder.WriteString("\n")
		return
	}
	f.process(node, indent)
}
```

注：`ast.Query.Body` 的静态类型是 `QueryBody` 接口；`processRelation` 形参用 `ast.Node` 以便切片 5 复用于集合运算。`ast.WithQuery` 字段（`Name *Identifier`、`Query Statement`、`ColumnNames []Identifier`）见 `internal/parser/ast/query.go`。Java 把 CTE 体包成 `new TableSubquery(query)` 再 `process`——上面直接构造临时 `ast.TableSubquery` 与之对齐。

- [ ] **Step 4: 跑测试 —— slice2/3/4 与 TPC-H 应大面积翻 pass**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: slice2 / slice3 / slice4 的几乎全部用例 + 多数 `tpch/*` 报 `now passes`。无 `REGRESSION`。

逐一排查仍 `fail` / `parse-error` 的 TPC-H 用例：
- `fail`：用 `--- want ---` / `--- got ---` 定位首个分歧字节，对照本计划"关键字节事实"与 Java 源码修正对应 visit。
- `parse-error`：Go parser 不能解析该 SQL 片段——属 P2 parser 缺口，回 `ast_builder.go` 补对应 visitor（参照 Task 6 的方式，移植 Java `AstBuilder` 同名方法）。
- 集合运算（`UNION`/`INTERSECT`/`EXCEPT`）相关的 `panic`：留给切片 5（Task 18），baseline 记录。

- [ ] **Step 5: 接受基线并确认绿**

Run: `make format-accept && make format-golden`
Expected: 第二条 PASS，`baseline consistent`。

- [ ] **Step 6: Commit**

```bash
git add internal/parser/formatter/formatter.go testdata/format
git commit -m "feat: format Query/WITH; wire up TPC-H corpus (P2 slice 4)"
```

---

## Task 18: 切片 5 — 集合运算

移植 `Union` / `Intersect` / `Except`，及 `processRelation` 对集合运算的分派。

**Files:**
- Modify: `internal/parser/ast/statement.go`（按需新增集合运算节点）
- Modify: `internal/parser/ast_builder.go`（按需）
- Modify: `internal/parser/formatter/formatter.go`
- Create: `testdata/format/cases/slice5/setop_*.sql`

参考 Java：`SqlFormatter` 的 `visitUnion`(746)、`visitExcept`(766)、`visitIntersect`(780)。

关键字节事实：`visitUnion` 遍历 `relations`，相邻两项之间插 `"UNION "`，非 distinct 再补 `"ALL "`。`visitExcept` 是二元（`left` / `right`），中间插 `"EXCEPT "` + 可选 `"ALL "`。`visitIntersect` 同 `visitUnion`。三者都对每个操作数走 `processRelation`。

- [ ] **Step 1: 检查 Go AST 的集合运算建模**

Run: `grep -rn "Union\|Intersect\|Except\|SetOperation" internal/parser/ast/ internal/parser/ast_builder.go`
Expected: 确认 Go 是否已有集合运算节点。`internal/parser/ast/statement.go` 注释提到 `QueryBody is satisfied by QuerySpecification and SetOperation`——可能已有 `SetOperation` 节点或尚缺。

- [ ] **Step 2: 按需新增集合运算 AST 节点**

若 Step 1 显示尚无集合运算节点，在 `internal/parser/ast/statement.go` 末尾追加（trino 把 `Union`/`Intersect` 建为可含多个 relation 的节点、`Except` 为二元；三者都实现 `QueryBody`）：

```go
// Union represents a UNION [ALL] of two or more query bodies.
type Union struct {
	BaseNode
	Relations []QueryBody
	Distinct  bool
}

func (u *Union) GetChildren() []Node {
	children := make([]Node, len(u.Relations))
	for i, r := range u.Relations {
		children[i] = r
	}
	return children
}
func (u *Union) isQueryBody() {}
func (u *Union) isStatement() {}

// Intersect represents an INTERSECT [ALL] of two or more query bodies.
type Intersect struct {
	BaseNode
	Relations []QueryBody
	Distinct  bool
}

func (i *Intersect) GetChildren() []Node {
	children := make([]Node, len(i.Relations))
	for j, r := range i.Relations {
		children[j] = r
	}
	return children
}
func (i *Intersect) isQueryBody() {}
func (i *Intersect) isStatement() {}

// Except represents <left> EXCEPT [ALL] <right>.
type Except struct {
	BaseNode
	Left     QueryBody
	Right    QueryBody
	Distinct bool
}

func (e *Except) GetChildren() []Node { return []Node{e.Left, e.Right} }
func (e *Except) isQueryBody()        {}
func (e *Except) isStatement()        {}
```

若 Step 1 显示 Go 已有等价节点（例如统一的 `SetOperation`），则不新增——改为按其实际结构调整 Step 4 的 formatter 代码。

- [ ] **Step 3: 按需扩 AstBuilder**

若 Step 2 新增了节点，则在 `ast_builder.go` 移植 trino `AstBuilder` 的 `visitSetOperation`（grep `visitSetOperation` 定位）：把语法树的集合运算上下文转成 `Union`/`Intersect`/`Except`。trino 对 `a UNION b UNION c` 的建模是嵌套二元（每个 `setOperation` 上下文两个操作数），formatter 的 `visitUnion` 遍历时仍逐层展开——不需要像逻辑表达式那样展平。Go 侧把每个集合运算上下文建成两元素的 `Relations`（`Union`/`Intersect`）或 `Left`/`Right`（`Except`）即可。

- [ ] **Step 4: 实现集合运算 visit**

在 `formatter.go` 的 `process` switch 插入：

```go
	case *ast.Union:
		f.visitUnion(n, indent)
	case *ast.Intersect:
		f.visitIntersect(n, indent)
	case *ast.Except:
		f.visitExcept(n, indent)
```

并在 `formatter.go` 末尾追加：

```go
// visitUnion mirrors trino SqlFormatter.visitUnion.
func (f *formatter) visitUnion(n *ast.Union, indent int) {
	for i, rel := range n.Relations {
		f.processRelation(rel, indent)
		if i < len(n.Relations)-1 {
			f.builder.WriteString("UNION ")
			if !n.Distinct {
				f.builder.WriteString("ALL ")
			}
		}
	}
}

// visitIntersect mirrors trino SqlFormatter.visitIntersect.
func (f *formatter) visitIntersect(n *ast.Intersect, indent int) {
	for i, rel := range n.Relations {
		f.processRelation(rel, indent)
		if i < len(n.Relations)-1 {
			f.builder.WriteString("INTERSECT ")
			if !n.Distinct {
				f.builder.WriteString("ALL ")
			}
		}
	}
}

// visitExcept mirrors trino SqlFormatter.visitExcept.
func (f *formatter) visitExcept(n *ast.Except, indent int) {
	f.processRelation(n.Left, indent)
	f.builder.WriteString("EXCEPT ")
	if !n.Distinct {
		f.builder.WriteString("ALL ")
	}
	f.processRelation(n.Right, indent)
}
```

- [ ] **Step 5: 写用例、捕获 golden、跑测试**

`testdata/format/cases/slice5/setop_union.sql`:
```sql
SELECT a FROM t1 UNION SELECT a FROM t2
```

`testdata/format/cases/slice5/setop_union_all.sql`:
```sql
SELECT a FROM t1 UNION ALL SELECT a FROM t2
```

`testdata/format/cases/slice5/setop_intersect.sql`:
```sql
SELECT a FROM t1 INTERSECT SELECT a FROM t2
```

`testdata/format/cases/slice5/setop_except.sql`:
```sql
SELECT a FROM t1 EXCEPT SELECT a FROM t2
```

Run: `make capture-format-golden`
Expected: 新增 4 个 `OK    slice5/setop_*`。

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v`
Expected: 4 个 `slice5/setop_*` 报 `now passes`。无 `REGRESSION`。

Run: `make format-accept && make format-golden`
Expected: PASS。

- [ ] **Step 6: 验证编译干净**

Run: `gofmt -l internal/parser/ && go vet ./internal/parser/... && go build ./...`
Expected: 均干净。

- [ ] **Step 7: Commit**

```bash
git add internal/parser testdata/format
git commit -m "feat: format set operations UNION/INTERSECT/EXCEPT (P2 slice 5)"
```

---

## Task 19: 幂等性测试与 CI 接线

加幂等性测试（`FormatSQL(ParseSQL(golden)) == golden`，保证 parser 能往返解析 formatter 自身输出——`WrenPlanner` 规则间会重解析，这是 P3 的前提），并把 P2 测试接入 CI。

**Files:**
- Modify: `internal/parser/formatter/golden_test.go`
- Modify: `.github/workflows/ci.yml`
- Create: `internal/parser/formatter/README.md`

- [ ] **Step 1: 加幂等性测试**

在 `internal/parser/formatter/golden_test.go` 末尾追加：

```go
// TestFormatIdempotent checks that the formatter's own output round-trips:
// FormatSQL(ParseSQL(golden)) == golden for every byte-identical case. The
// WrenPlanner re-parses formatter output between rewrite rules, so this
// property is a prerequisite for P3 end-to-end parity.
func TestFormatIdempotent(t *testing.T) {
	base := readBaseline(t)
	for id, status := range base.Cases {
		if status != "pass" {
			continue // only cases known to be byte-identical with Java
		}
		t.Run(id, func(t *testing.T) {
			golden, err := os.ReadFile(filepath.Join(goldenDir, id+".golden"))
			if err != nil {
				t.Skipf("golden missing: %v", err)
			}
			got, status, detail := goFormat(string(golden))
			if status != "" {
				t.Fatalf("re-format %s: %s — %s", status, id, detail)
			}
			if got != string(golden) {
				t.Errorf("not idempotent:\n%s", firstDiff(string(golden), got))
			}
		})
	}
}
```

- [ ] **Step 2: 运行幂等性测试**

Run: `go test ./internal/parser/formatter/ -run TestFormatIdempotent -v`
Expected: 全部 `pass` 用例的子测试通过。

若某用例非幂等（`not idempotent` 或 re-format 报 `parse-error`）：说明 parser 不能往返解析 formatter 自身输出——属 P2 parser 缺口。用 `--- want ---` 定位失败的 SQL 构造，回 `ast_builder.go` 补对应 visitor。修复后重跑。

- [ ] **Step 3: 把 P2 测试接入 CI**

在 `.github/workflows/ci.yml` 的 `test` 步骤之后（或之内）确认 `go test ./...` 已覆盖 `internal/parser/formatter/`——`TestFormatGolden` / `TestFormatIdempotent` 只读已提交的 `.golden` 与 `baseline.json`，不需要 Java，CI 直接跑。无需改 workflow，只需确认现有 `test` 步骤是 `go test ./...`。

Run: `grep -n "go test" .github/workflows/ci.yml`
Expected: 存在 `run: go test ./...`。若 CI 只跑了子集，把它改成 `go test ./...`。

- [ ] **Step 4: 写 README**

Create `internal/parser/formatter/README.md`:

```markdown
# SQL Formatter（P2 字节对齐）

把 AST 渲染回 SQL 文本，与 Java `wren-engine:0.9.3` 的
`SqlFormatter.formatSql` 逐字节一致（DEFAULT 方言）。

- `formatter.go` —— 移植 trino `SqlFormatter`：语句/查询/关系 visit +
  缩进机制（`INDENT = "   "`，3 空格）。
- `expression_formatter.go` —— 移植 trino `ExpressionFormatter`：表达式
  扁平 visitor。二元表达式强制全括号。

## 验证回路

- `testdata/format/cases/**/*.sql` —— 输入语料（snippet + TPC-H 22）。
- `testdata/format/golden/**/*.golden` —— Java `SqlFormatter` 的输出，
  由 `make capture-format-golden` 离线捕获并提交入库。
- `make format-golden` —— 离线差分测试：`FormatSQL(ParseSQL(sql))` 与
  `.golden` 逐字节比对 + 幂等性检查；CI 默认跑，不需要 Java。
- `make format-accept` —— 把当前结果写回 `baseline.json`（formatter
  取得进展、用例从非 pass 翻 pass 后运行）。

## 捕获 golden（需 Java 17 + Maven）

`make capture-format-golden` 会把 `io.wren:trino-parser:0.9.3` 装进本地
Maven 仓库，再用 `tools/format-oracle/` 的 Java harness 逐用例执行
`SqlFormatter.formatSql(parseSql(sql))`。新增/修改 `cases/` 后运行。

## 未支持节点

formatter 遇到范围外或未实现的节点 `panic`（对齐 Java
`SqlFormatter.visitNode` 抛 `UnsupportedOperationException`），绝不静默
输出占位符。P2 范围见
`.gpowers/designs/2026-05-19-parity-p2-parser-formatter-design.md` §2。
```

- [ ] **Step 5: Commit**

```bash
git add internal/parser/formatter/golden_test.go internal/parser/formatter/README.md .github/workflows/ci.yml
git commit -m "test: add formatter idempotency check; wire P2 into CI"
```

---

## Task 20: 终验

跑齐全部验收门槛，确认 P2 完成。

**Files:** 无（仅验证）

- [ ] **Step 1: 全量差分测试**

Run: `make format-golden`
Expected: PASS。计分板打印 `N/N byte-identical`（N = 全部 snippet + TPC-H 用例数，扣除 `oracle-error` 用例）。`baseline consistent, no regression`。

- [ ] **Step 2: 确认 TPC-H 22 全部字节命中**

Run: `go test ./internal/parser/formatter/ -run TestFormatGolden -v 2>&1 | grep "tpch/"`
Expected: 22 行全部 `PASS  tpch/N.sql`（若有 `oracle-error` 用例则相应那几条为 `oracle-error`，其余全 `PASS`）。

若仍有 `tpch/*` 为 `fail` / `parse-error` / `panic`：这是 P2 未完成。逐一按 Task 17 Step 4 的方法定位修复——formatter 字节分歧回对应 visit、parser 缺口回 `ast_builder.go`。修复后 `make capture-format-golden`（若动了语料）→ `make format-accept` → 重跑。**不得带着未命中的 TPC-H 用例收尾。**

- [ ] **Step 3: 幂等性全绿**

Run: `go test ./internal/parser/formatter/ -run TestFormatIdempotent`
Expected: PASS。

- [ ] **Step 4: 构建与静态检查**

Run: `go build ./...`
Expected: 退出码 0。

Run: `go vet ./...`
Expected: 退出码 0。

Run: `gofmt -l .`
Expected: 无输出。

- [ ] **Step 5: 全量测试**

Run: `go test ./internal/parser/...`
Expected: `internal/parser`、`internal/parser/ast`、`internal/parser/formatter` 全 `ok`。

注：`internal/rewrite`、`internal/server`、`internal/service` 等包的测试可能因 formatter 输出从单行紧凑变为多行缩进而失败——这些是 P3 才处理的改写逻辑，P3a 设计已决定整体丢弃 `internal/rewrite/`。**P2 的硬要求是 `go build ./...` 通过、`internal/parser/...` 测试全绿、formatter 差分测试与幂等性全绿。** 若 `internal/rewrite`/`internal/server`/`internal/service` 出现测试失败，记录失败包与首条错误向用户报告，不在 P2 内修复。

- [ ] **Step 6: 最终提交**

```bash
git add -A
git commit -m "chore: P2 parser/formatter byte-alignment complete" --allow-empty
```

---

## 完成标准

- 全部 snippet golden 用例 `FormatSQL(ParseSQL(sql))` 与 `.golden` 逐字节命中。
- TPC-H 22 条全部 parse+format 往返与 `.golden` 逐字节命中（`oracle-error` 用例除外）。
- 幂等性测试 `FormatSQL(ParseSQL(golden)) == golden` 全绿。
- `go build ./...` 通过、`go vet ./...` 干净、`gofmt -l .` 无输出。
- `internal/parser/...` 全部包测试 `ok`。
- 未支持节点一律 `panic`，无静默占位符输出。
- formatter 差分测试与计分板接入 CI，CI 不依赖 Java。
