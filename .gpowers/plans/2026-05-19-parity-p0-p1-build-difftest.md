# P0 修复构建 + P1 差分测试框架 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use gpowers:subagent-driven-development (recommended) or gpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `go-wren-engine` 从干净 checkout 能编译并通过 CI，并建立一套以 Java `wren-engine:0.9.3` 为标准答案机的 golden-快照差分测试框架。

**Architecture:** P0 删除 `go.mod` 中失效的 `replace` 指令、补 `go.sum`、清理仓库、加 GitHub Actions。P1 新增 `internal/difftest` 包——用 ANTLR lexer 把 SQL 规范化成 token 序列，把 Java 引擎的改写输出冻结成 golden 文件，Go 侧 `rewrite.Rewrite()` 的输出与之离线比对，用 `baseline.json` 计分板防回归。

**Tech Stack:** Go 1.26、ANTLR4 Go runtime、chi v5、Docker（仅 `capture-golden` 用）、GitHub Actions。

**Spec:** `.gpowers/designs/2026-05-19-parity-p0-p1-build-difftest-design.md`

**源参考:** Java 引擎源码在 `../wren-engine-0.9.3`；TPC-H 语料在 `../wren-engine-0.9.3/wren-tests/src/test/resources/`。

---

## Task 1: 修复 go.mod 依赖并验证构建

**Files:**
- Modify: `go.mod`
- Create: `go.sum`（由 `go mod tidy` 生成）

- [ ] **Step 1: 删除失效的 replace 指令并修正 antlr 版本**

把 `go.mod` 改成下面的内容（删除 3 条 `replace`，把 antlr 的占位版本 `v4.0.0-00010101000000-000000000000` 换成模块缓存中已存在、且与 `internal/parser/generated/` 生成代码匹配的伪版本）：

```
module github.com/wren-engine/wren

go 1.26.3

require (
	github.com/antlr4-go/antlr/v4 v0.0.0-20230518091524-98b52378c522
	github.com/go-chi/chi/v5 v5.2.5
)

require golang.org/x/exp v0.0.0-20240506185415-9bf2ced13842 // indirect
```

- [ ] **Step 2: 生成 go.sum 并拉取依赖**

Run: `go mod tidy`
Expected: 成功，生成 `go.sum`，无报错。
若失败提示 `golang.org/x/exp` 版本不可达，改用 `go get golang.org/x/exp@latest` 后再 `go mod tidy`。

- [ ] **Step 3: 验证编译**

Run: `go build ./...`
Expected: 退出码 0，无输出。
若 `internal/parser/generated/` 报 antlr runtime API 不兼容：先尝试 `go get github.com/antlr4-go/antlr/v4@v4.13.1 && go mod tidy && go build ./...`；仍失败则运行 `make generate`（Task 2 修好后）重新生成 parser，再 `go build ./...`。

- [ ] **Step 4: 验证 vet 与测试**

Run: `go vet ./...`
Expected: 退出码 0。

Run: `go test ./...`
Expected: 所有包编译通过。`internal/analyzer`、`internal/dto`、`internal/mdl`、`internal/parser/ast`、`internal/parser/formatter`、`internal/parser/visitor` 应为 `ok`。
若 `internal/parser`、`internal/rewrite`、`internal/server`、`internal/service` 中出现**测试失败**（非编译失败）：这是早于本计划存在的逻辑缺陷。**停下来向用户报告**失败的包与首条错误，不要在本计划内修复——P2/P3 才处理改写逻辑。P0 的硬要求是"编译与 setup 成功"。

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "fix: replace stale /tmp replace directives with resolvable deps"
```

---

## Task 2: 仓库清理与 Makefile 修正

**Files:**
- Delete: `wren-server`（仓库根目录的 17MB 编译产物）
- Create: `.gitignore`
- Modify: `Makefile`

- [ ] **Step 1: 从 git 移除编译产物**

Run: `git rm --cached wren-server && rm -f wren-server`
Expected: `wren-server` 从索引移除。

- [ ] **Step 2: 创建 .gitignore**

Create `.gitignore`:

```
# 编译产物
/wren-server
/bin/
*.test
*.out

# 本地环境
.DS_Store
```

- [ ] **Step 3: 修正 Makefile 的 generate 目标**

把 `Makefile` 的 `generate` 目标里硬编码的本机 java 路径改成 PATH 中的 `java`。修改后的 `generate` 目标：

```makefile
generate:
	cd internal/parser/generated && java -jar ../../../tools/antlr-4.13.2-complete.jar -Dlanguage=Go -package generated SqlBase.g4
```

- [ ] **Step 4: 验证 build/test 仍可用**

Run: `make build`
Expected: 生成 `bin/wren-server`，退出码 0。

Run: `make test`
Expected: 与 Task 1 Step 4 一致。

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: drop committed binary, add .gitignore, fix Makefile generate"
```

---

## Task 3: 新增 GitHub Actions CI

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: 创建 CI 工作流**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
  pull_request:

jobs:
  build-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true

      - name: gofmt
        run: |
          unformatted="$(gofmt -l .)"
          if [ -n "$unformatted" ]; then
            echo "以下文件未格式化:"; echo "$unformatted"; exit 1
          fi

      - name: go vet
        run: go vet ./...

      - name: build
        run: go build ./...

      - name: test
        run: go test ./...
```

- [ ] **Step 2: 本地预演 CI 各步骤**

Run: `gofmt -l .`
Expected: 输出为空（无未格式化文件）。若有，运行 `gofmt -w <文件>` 修正。

Run: `go vet ./... && go build ./...`
Expected: 退出码 0。

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add build/vet/gofmt/test workflow"
```

---

## Task 4: 新增 TPC-H 差分测试语料

**Files:**
- Create: `testdata/difftest/cases/tpch/mdl.json`（拷自 `../wren-engine-0.9.3/wren-tests/src/test/resources/tpch_mdl.json`）
- Create: `testdata/difftest/cases/tpch/queries/1.sql` … `22.sql`（拷自同目录 `tpch/queries/`）
- Create: `testdata/difftest/cases/tpch/group.json`

- [ ] **Step 1: 拷贝 TPC-H MDL 与查询**

```bash
mkdir -p testdata/difftest/cases/tpch/queries
cp ../wren-engine-0.9.3/wren-tests/src/test/resources/tpch_mdl.json testdata/difftest/cases/tpch/mdl.json
cp ../wren-engine-0.9.3/wren-tests/src/test/resources/tpch/queries/*.sql testdata/difftest/cases/tpch/queries/
```

- [ ] **Step 2: 创建组配置**

Create `testdata/difftest/cases/tpch/group.json`:

```json
{
  "modelingOnly": true
}
```

- [ ] **Step 3: 验证文件齐全**

Run: `ls testdata/difftest/cases/tpch/queries/ | wc -l`
Expected: `22`

Run: `head -1 testdata/difftest/cases/tpch/mdl.json`
Expected: 以 `{` 开头的 JSON。

- [ ] **Step 4: Commit**

```bash
git add testdata/difftest/cases/
git commit -m "test: add TPC-H corpus for differential testing"
```

---

## Task 5: 新增 parser.LexTokens 词法分析导出函数

`internal/difftest` 的规范化器需要把 SQL 切成 token，但大小写不敏感的输入流逻辑现在私有于 `internal/parser`。本任务在 `internal/parser` 导出一个稳定的词法接口，避免重复实现。

**Files:**
- Modify: `internal/parser/parser.go`
- Test: `internal/parser/lex_test.go`

- [ ] **Step 1: 写失败的测试**

Create `internal/parser/lex_test.go`:

```go
package parser

import "testing"

func TestLexTokens_SkipsWhitespace(t *testing.T) {
	toks := LexTokens("select   a  from t")
	if len(toks) != 4 {
		t.Fatalf("want 4 tokens, got %d: %+v", len(toks), toks)
	}
	wantText := []string{"select", "a", "from", "t"}
	for i, w := range wantText {
		if toks[i].Text != w {
			t.Errorf("token %d: want %q, got %q", i, w, toks[i].Text)
		}
	}
}

func TestLexTokens_SkipsComments(t *testing.T) {
	toks := LexTokens("select a -- a trailing comment\nfrom t")
	if len(toks) != 4 {
		t.Fatalf("want 4 tokens (comment excluded), got %d: %+v", len(toks), toks)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/parser/ -run TestLexTokens -v`
Expected: FAIL，报 `undefined: LexTokens`。

- [ ] **Step 3: 实现 LexTokens**

在 `internal/parser/parser.go` 末尾追加（`antlr` 与 `generated` 已在该文件 import）：

```go
// LexToken is a single lexical token: its lexer token type and source text.
// Type values are the generated.SqlBaseLexer* constants.
type LexToken struct {
	Type int
	Text string
}

// LexTokens tokenizes sql into default-channel tokens. Whitespace and comment
// tokens (which the grammar routes to the hidden channel) are excluded.
// Lexing is case-insensitive for keyword matching, matching ParseSQL.
func LexTokens(sql string) []LexToken {
	lexer := generated.NewSqlBaseLexer(newCaseInsensitiveStream(sql))
	var toks []LexToken
	for {
		t := lexer.NextToken()
		if t.GetTokenType() == antlr.TokenEOF {
			break
		}
		if t.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		toks = append(toks, LexToken{Type: t.GetTokenType(), Text: t.GetText()})
	}
	return toks
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/parser/ -run TestLexTokens -v`
Expected: PASS（两个测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/parser/parser.go internal/parser/lex_test.go
git commit -m "feat: add parser.LexTokens for token-stream tooling"
```

---

## Task 6: 新增 SQL 规范化器

**Files:**
- Create: `internal/difftest/normalize.go`
- Test: `internal/difftest/normalize_test.go`

- [ ] **Step 1: 写失败的测试**

Create `internal/difftest/normalize_test.go`:

```go
package difftest

import (
	"slices"
	"testing"
)

func eq(t *testing.T, a, b string, want bool) {
	t.Helper()
	na, err := Normalize(a)
	if err != nil {
		t.Fatalf("Normalize(%q): %v", a, err)
	}
	nb, err := Normalize(b)
	if err != nil {
		t.Fatalf("Normalize(%q): %v", b, err)
	}
	got := slices.Equal(na, nb)
	if got != want {
		t.Errorf("equal(%q, %q) = %v, want %v\n  a=%v\n  b=%v", a, b, got, want, na, nb)
	}
}

func TestNormalize_IgnoresWhitespaceAndKeywordCase(t *testing.T) {
	eq(t, "select a from t", "SELECT   a\nFROM  t", true)
}

func TestNormalize_IgnoresUnquotedIdentifierCase(t *testing.T) {
	eq(t, "select Col from T", "select col from t", true)
}

func TestNormalize_StringLiteralIsCaseSensitive(t *testing.T) {
	eq(t, "select 'A'", "select 'a'", false)
}

func TestNormalize_QuotedIdentifierIsCaseSensitive(t *testing.T) {
	eq(t, `select "Col"`, `select "col"`, false)
}

func TestNormalize_StructuralDifferenceDetected(t *testing.T) {
	eq(t, "select a from t", "select a, b from t", false)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/difftest/ -run TestNormalize -v`
Expected: FAIL，报 `undefined: Normalize`。

- [ ] **Step 3: 实现规范化器**

Create `internal/difftest/normalize.go`:

```go
// Package difftest provides a golden-snapshot differential testing harness
// that compares the Go rewrite engine against the Java wren-engine:0.9.3.
package difftest

import (
	"strings"

	"github.com/wren-engine/wren/internal/parser"
	"github.com/wren-engine/wren/internal/parser/generated"
)

// Normalize tokenizes sql and returns a canonical token sequence. Two SQL
// strings are considered equivalent iff their normalized sequences are equal.
//
// Whitespace and comments are dropped. Keywords, unquoted identifiers,
// numbers and operators are upper-cased (SQL treats them case-insensitively).
// String literals and quoted identifiers keep their original text verbatim,
// since they are case-sensitive.
func Normalize(sql string) ([]string, error) {
	toks := parser.LexTokens(sql)
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		out = append(out, normalizeToken(t))
	}
	return out, nil
}

func normalizeToken(t parser.LexToken) string {
	switch t.Type {
	case generated.SqlBaseLexerSTRING,
		generated.SqlBaseLexerUNICODE_STRING,
		generated.SqlBaseLexerQUOTED_IDENTIFIER,
		generated.SqlBaseLexerBACKQUOTED_IDENTIFIER:
		return t.Text
	default:
		return strings.ToUpper(t.Text)
	}
}
```

注：`Normalize` 当前不会返回非 nil error；保留 error 返回值是为了将来规范化逻辑变复杂时不破坏调用方签名。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/difftest/ -run TestNormalize -v`
Expected: PASS（5 个测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/difftest/normalize.go internal/difftest/normalize_test.go
git commit -m "feat: add SQL token-stream normalizer for difftest"
```

---

## Task 7: 新增差分测试语料加载器

**Files:**
- Create: `internal/difftest/corpus.go`
- Test: `internal/difftest/corpus_test.go`

- [ ] **Step 1: 写失败的测试**

Create `internal/difftest/corpus_test.go`:

```go
package difftest

import "testing"

func TestLoadCorpus_TPCH(t *testing.T) {
	cases, err := LoadCorpus("../../testdata/difftest/cases")
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(cases) != 22 {
		t.Fatalf("want 22 cases, got %d", len(cases))
	}
	c := cases[0]
	if c.Group != "tpch" {
		t.Errorf("Group = %q, want tpch", c.Group)
	}
	if !c.ModelingOnly {
		t.Errorf("ModelingOnly = false, want true")
	}
	if len(c.ManifestJSON) == 0 || c.SQL == "" {
		t.Errorf("case %s: empty manifest or sql", c.ID())
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/difftest/ -run TestLoadCorpus -v`
Expected: FAIL，报 `undefined: LoadCorpus`。

- [ ] **Step 3: 实现语料加载器**

Create `internal/difftest/corpus.go`:

```go
package difftest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Case is one differential testing case: an MDL manifest plus a SQL query.
type Case struct {
	Group        string
	Name         string
	ManifestJSON []byte
	SQL          string
	ModelingOnly bool
}

// ID returns the case's logical identifier "<group>/<name>".
func (c Case) ID() string { return c.Group + "/" + c.Name }

type groupConfig struct {
	ModelingOnly bool `json:"modelingOnly"`
}

// LoadCorpus loads every case under root. Layout: root/<group>/{mdl.json,
// group.json, queries/<name>.sql}. Cases are returned sorted by ID.
func LoadCorpus(root string) ([]Case, error) {
	groups, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read corpus root %s: %w", root, err)
	}
	var cases []Case
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		groupDir := filepath.Join(root, g.Name())

		manifest, err := os.ReadFile(filepath.Join(groupDir, "mdl.json"))
		if err != nil {
			return nil, fmt.Errorf("group %s: %w", g.Name(), err)
		}

		cfgRaw, err := os.ReadFile(filepath.Join(groupDir, "group.json"))
		if err != nil {
			return nil, fmt.Errorf("group %s: %w", g.Name(), err)
		}
		var cfg groupConfig
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return nil, fmt.Errorf("group %s group.json: %w", g.Name(), err)
		}

		queries, err := os.ReadDir(filepath.Join(groupDir, "queries"))
		if err != nil {
			return nil, fmt.Errorf("group %s: %w", g.Name(), err)
		}
		for _, q := range queries {
			if q.IsDir() || !strings.HasSuffix(q.Name(), ".sql") {
				continue
			}
			sql, err := os.ReadFile(filepath.Join(groupDir, "queries", q.Name()))
			if err != nil {
				return nil, fmt.Errorf("group %s query %s: %w", g.Name(), q.Name(), err)
			}
			cases = append(cases, Case{
				Group:        g.Name(),
				Name:         strings.TrimSuffix(q.Name(), ".sql"),
				ManifestJSON: manifest,
				SQL:          string(sql),
				ModelingOnly: cfg.ModelingOnly,
			})
		}
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID() < cases[j].ID() })
	return cases, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/difftest/ -run TestLoadCorpus -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/difftest/corpus.go internal/difftest/corpus_test.go
git commit -m "feat: add difftest corpus loader"
```

---

## Task 8: 新增 golden 捕获程序

**Files:**
- Create: `cmd/capture-golden/main.go`

- [ ] **Step 1: 实现捕获程序**

Create `cmd/capture-golden/main.go`:

```go
// Command capture-golden replays every difftest case against a running Java
// wren-engine and freezes the rewritten SQL into golden files.
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
	outDir := flag.String("out", "testdata/difftest/golden", "golden output directory")
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
			"modelingOnly": c.ModelingOnly,
		})
		req, err := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/dry-plan", bytes.NewReader(body))
		if err != nil {
			log.Fatalf("%s: build request: %v", c.ID(), err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("%s: request failed (is the oracle up?): %v", c.ID(), err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		dir := filepath.Join(*outDir, c.Group)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("%s: mkdir: %v", c.ID(), err)
		}
		base := filepath.Join(dir, c.Name+".sql")
		if resp.StatusCode/100 == 2 {
			writeFile(base, payload)
			os.Remove(base + ".error")
			ok++
			fmt.Printf("OK    %s\n", c.ID())
		} else {
			writeFile(base+".error", payload)
			os.Remove(base)
			errs++
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
		}
	}
	fmt.Printf("\ncaptured %d golden, %d oracle errors, %d total\n", ok, errs, len(cases))
}

func writeFile(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}
```

- [ ] **Step 2: 验证编译**

Run: `go build ./cmd/capture-golden/`
Expected: 退出码 0。

- [ ] **Step 3: Commit**

```bash
git add cmd/capture-golden/main.go
git commit -m "feat: add capture-golden tool for difftest oracle"
```

---

## Task 9: 新增捕获脚本与 oracle 配置，捕获 golden

> 本任务需要本机可用的 Docker 与拉取 `ghcr.io/canner/wren-engine:0.9.3` 的网络。

**Files:**
- Create: `tools/oracle-etc/config.properties`
- Create: `tools/oracle-etc/mdl/.gitkeep`
- Create: `tools/capture-golden.sh`
- Modify: `Makefile`
- Create: `testdata/difftest/golden/tpch/*.sql`（由脚本生成）

- [ ] **Step 1: 创建 oracle 最小配置**

Create `tools/oracle-etc/config.properties`:

```
node.environment=production
wren.directory=/usr/src/app/etc/mdl
wren.experimental-enable-dynamic-fields=false
wren.datasource.type=duckdb
```

Create 空文件 `tools/oracle-etc/mdl/.gitkeep`（占位，使挂载目录存在）：

```bash
mkdir -p tools/oracle-etc/mdl && touch tools/oracle-etc/mdl/.gitkeep
```

- [ ] **Step 2: 创建捕获脚本**

Create `tools/capture-golden.sh`:

```bash
#!/usr/bin/env bash
# 启动 Java wren-engine:0.9.3 作为 oracle, 捕获 difftest golden, 然后销毁容器.
set -euo pipefail

IMAGE="ghcr.io/canner/wren-engine:0.9.3"
PORT="18080"
ETC_DIR="$(cd "$(dirname "$0")/oracle-etc" && pwd)"

cid="$(docker run -d -p "${PORT}:8080" -v "${ETC_DIR}:/usr/src/app/etc" "${IMAGE}")"
trap 'docker rm -f "${cid}" >/dev/null 2>&1 || true' EXIT

echo "等待 oracle 就绪 (容器 ${cid:0:12})..."
ready=""
for _ in $(seq 1 60); do
  if curl -sf "http://localhost:${PORT}/v1/config" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 2
done
if [ -z "${ready}" ]; then
  echo "oracle 启动失败, 容器日志末尾:" >&2
  docker logs --tail 40 "${cid}" >&2
  exit 1
fi

echo "oracle 就绪, 开始捕获..."
go run ./cmd/capture-golden -addr "http://localhost:${PORT}"
```

- [ ] **Step 3: 赋予脚本可执行权限**

Run: `chmod +x tools/capture-golden.sh`

- [ ] **Step 4: 在 Makefile 增加 difftest 相关目标**

把 `Makefile` 第一行的 `.PHONY` 改为包含新目标，并在文件末尾追加 3 个目标：

```makefile
.PHONY: build test clean generate capture-golden difftest difftest-accept
```

```makefile
capture-golden:
	./tools/capture-golden.sh

difftest:
	go test ./internal/difftest/... -v

difftest-accept:
	go test ./internal/difftest/... -run TestDifferential -difftest.accept
```

- [ ] **Step 5: 运行捕获**

Run: `make capture-golden`
Expected: 打印 22 行 `OK tpch/N`，末尾 `captured 22 golden, 0 oracle errors, 22 total`，并生成 `testdata/difftest/golden/tpch/1.sql` … `22.sql`。
若某些用例为 `ERROR`：检查 `testdata/difftest/golden/tpch/<name>.sql.error` 内容，这是 Java 引擎对该用例报错——记录但不阻塞，差分测试会把它归类为 `oracle-error`。

- [ ] **Step 6: 验证 golden 已生成**

Run: `ls testdata/difftest/golden/tpch/*.sql | wc -l`
Expected: `22`（若有 oracle 错误则相应减少）。

- [ ] **Step 7: Commit**

```bash
git add tools/oracle-etc tools/capture-golden.sh Makefile testdata/difftest/golden/
git commit -m "feat: add golden capture script and freeze TPC-H goldens"
```

---

## Task 10: 新增差分测试、baseline 计分板与 README

**Files:**
- Create: `internal/difftest/difftest_test.go`
- Create: `testdata/difftest/baseline.json`
- Create: `internal/difftest/README.md`

- [ ] **Step 1: 创建初始 baseline**

Create `testdata/difftest/baseline.json`（初始把所有用例记为 `fail`——P2/P3 未做，这是预期基线）：

```json
{
  "version": 1,
  "cases": {}
}
```

`cases` 留空即可：差分测试会把"baseline 中不存在的用例"视同 `fail`（见 Step 3 的判定逻辑）。首次运行后用 `make difftest-accept` 写入实际状态。

- [ ] **Step 2: 写差分测试**

Create `internal/difftest/difftest_test.go`:

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
	"github.com/wren-engine/wren/internal/dto"
	"github.com/wren-engine/wren/internal/mdl"
	"github.com/wren-engine/wren/internal/rewrite"
)

var acceptFlag = flag.Bool("difftest.accept", false,
	"rewrite baseline.json with the current results instead of asserting")

const (
	casesDir     = "../../testdata/difftest/cases"
	goldenDir    = "../../testdata/difftest/golden"
	baselinePath = "../../testdata/difftest/baseline.json"
)

type baselineFile struct {
	Version int               `json:"version"`
	Cases   map[string]string `json:"cases"`
}

// runCase rewrites one case with the Go engine and compares it to the frozen
// Java golden. It returns one of: pass, fail, parser-gap, go-error,
// oracle-error, no-golden.
func runCase(c Case) (status string, detail string) {
	goldenBase := filepath.Join(goldenDir, c.Group, c.Name+".sql")
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java engine returned an error for this case"
	}
	wantSQL, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "golden file missing; run `make capture-golden`"
	}

	actual, status, detail := goRewrite(c)
	if status != "" {
		return status, detail
	}

	wantTokens, err := Normalize(string(wantSQL))
	if err != nil {
		return "parser-gap", "cannot lex Java golden: " + err.Error()
	}
	gotTokens, err := Normalize(actual)
	if err != nil {
		return "parser-gap", "cannot lex Go output: " + err.Error()
	}
	if slices.Equal(wantTokens, gotTokens) {
		return "pass", ""
	}
	return "fail", firstDiff(wantTokens, gotTokens)
}

// goRewrite runs the Go rewrite engine, recovering panics. On success it
// returns (sql, "", ""); on failure ("", "go-error", detail).
func goRewrite(c Case) (sql, status, detail string) {
	defer func() {
		if r := recover(); r != nil {
			sql, status, detail = "", "go-error", fmt.Sprintf("panic: %v", r)
		}
	}()
	var manifest dto.Manifest
	if err := json.Unmarshal(c.ManifestJSON, &manifest); err != nil {
		return "", "go-error", "unmarshal manifest: " + err.Error()
	}
	wrenMDL := mdl.WrenMDLFromManifest(&manifest)
	analyzed := mdl.NewAnalyzedMDL(wrenMDL)
	ctx := &analyzer.SessionContext{Catalog: wrenMDL.Catalog(), Schema: wrenMDL.Schema()}
	out, err := rewrite.Rewrite(c.SQL, ctx, analyzed)
	if err != nil {
		return "", "go-error", err.Error()
	}
	return out, "", ""
}

func firstDiff(want, got []string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			return fmt.Sprintf("token %d: want %q, got %q", i, want[i], got[i])
		}
	}
	return fmt.Sprintf("token count: want %d, got %d", len(want), len(got))
}

func TestDifferential(t *testing.T) {
	cases, err := LoadCorpus(casesDir)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}

	results := make(map[string]string, len(cases))
	for _, c := range cases {
		status, detail := runCase(c)
		results[c.ID()] = status
		if status == "pass" {
			t.Logf("PASS  %s", c.ID())
		} else {
			t.Logf("%-12s %s — %s", status, c.ID(), detail)
		}
	}

	if *acceptFlag {
		writeBaseline(t, results)
		return
	}

	base := readBaseline(t)
	regressions, improvements := 0, 0
	for id, status := range results {
		want := base.Cases[id] // missing => "" => treated as non-pass
		switch {
		case want == "pass" && status != "pass":
			regressions++
			t.Errorf("REGRESSION %s: baseline=pass, now=%s", id, status)
		case want != "pass" && status == "pass":
			improvements++
			t.Errorf("%s now passes — run `make difftest-accept` to update baseline", id)
		}
	}

	t.Logf("\n%s", summary(results))
	if regressions == 0 && improvements == 0 {
		t.Logf("baseline 一致, 无回归")
	}
}

func summary(results map[string]string) string {
	counts := map[string]int{}
	for _, s := range results {
		counts[s]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := fmt.Sprintf("差分计分板: %d/%d 通过", counts["pass"], len(results))
	for _, k := range keys {
		s += fmt.Sprintf("\n  %-12s %d", k, counts[k])
	}
	return s
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
	t.Logf("baseline 已更新: %s", baselinePath)
}
```

- [ ] **Step 3: 运行差分测试（首轮，预期有 improvements 报错）**

Run: `go test ./internal/difftest/ -run TestDifferential -v`
Expected: 测试逐用例打印状态与计分板。由于 `baseline.json` 的 `cases` 为空，任何 `pass` 的用例都会触发 `now passes` 报错——这是设计预期。下一步用 accept 写入真实基线。

- [ ] **Step 4: 接受当前结果为基线**

Run: `make difftest-accept`
Expected: `baseline.json` 被重写，`cases` 填入每个用例的当前状态。

- [ ] **Step 5: 再次运行差分测试，确认绿**

Run: `make difftest`
Expected: PASS。末尾打印 `baseline 一致, 无回归` 与计分板。

- [ ] **Step 6: 验证防回归机制**

手动把 `baseline.json` 中任意一个用例的状态从其当前值改为 `"pass"`（挑一个当前不是 pass 的）。

Run: `go test ./internal/difftest/ -run TestDifferential`
Expected: FAIL，报 `REGRESSION ...: baseline=pass, now=...`。

随后用 `git checkout testdata/difftest/baseline.json` 还原。

- [ ] **Step 7: 编写 README**

Create `internal/difftest/README.md`:

```markdown
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

## 失败分类

- `fail` —— token 序列与 Java 不一致（改写逻辑差异，P2/P3 的修复目标）。
- `parser-gap` —— Go lexer 无法切分某段 SQL（P2 的修复目标）。
- `go-error` —— Go 改写返回错误或 panic。
- `oracle-error` —— Java 引擎对该用例本身报错，已排除出比对。
- `no-golden` —— 缺 golden 文件，需运行 `make capture-golden`。
```

- [ ] **Step 8: Commit**

```bash
git add internal/difftest/difftest_test.go internal/difftest/README.md testdata/difftest/baseline.json
git commit -m "feat: add differential test with baseline scoreboard"
```

---

## 完成标准

- 干净 checkout 后 `make build`、`make test` 通过；CI 工作流在 PR 上跑绿。
- `make capture-golden` 在装有 Docker 的机器上跑通，产出 TPC-H golden。
- `make difftest` 离线通过：用例对照 baseline 无回归，计分板摘要正确。
- 规范化器单测通过；防回归机制经 Task 10 Step 6 验证生效。
- 仓库不再含 `wren-server` 二进制；`go.mod` 无 `/tmp` `replace`。
```
