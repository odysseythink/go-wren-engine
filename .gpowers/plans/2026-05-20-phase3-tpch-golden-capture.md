# Phase 3 Implementation Plan: Golden Capture + Oracle Tooling + Scoreboard

> **For agentic workers:** REQUIRED SUB-SKILL: Use `gpowers:subagent-driven-development` (recommended) or `gpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate all `no-golden` and transient `oracle-error` entries from the 3 differential test baselines (rewrite, DuckDB-dialect, envelope), introduce `oracle-error-permanent` for known Java bugs, provide one-click Java oracle lifecycle scripts, and produce a quantified scoreboard.

**Architecture:** Add `tools/oracle-up.sh` / `oracle-down.sh` for reproducible Java oracle startup. Unify all 4 capture tools with consistent `--addr/--cases/--out/--groups/--timeout/--retry` flags and a `summary:` tail line. Introduce `.error.permanent` sentinel files that difftest maps to `oracle-error-permanent` status (excluded from pass-rate denominator). Run full capture against the oracle to populate `golden-envelope/` and `golden-duckdb/`, then rebaseline all JSON files. Deliver two docs: a scoreboard and an oracle runbook.

**Tech Stack:** Go 1.26, bash, Docker, curl, make

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `tools/oracle-up.sh` | Create | Docker-run Java oracle + readiness probe |
| `tools/oracle-down.sh` | Create | Stop + remove `wren-oracle` container |
| `Makefile` | Modify | Add `oracle-up`, `oracle-down`, `oracle-logs`, `capture-all-golden`, `rebaseline` targets |
| `cmd/capture-golden/main.go` | Modify | Add `--groups`, `--timeout`, `--retry`, `summary:` tail |
| `cmd/capture-envelope-golden/main.go` | Modify | Add `--timeout`, `--retry`, `summary:` tail |
| `cmd/capture-duckdb-golden/main.go` | Modify | Add `--groups`, `--timeout`, `--retry`, `summary:` tail |
| `cmd/capture-analysis-golden/main.go` | Modify | Migrate from positional args to flags (backward-compat), add `--timeout`, `--retry`, `summary:` tail |
| `internal/difftest/difftest_test.go` | Modify | Detect `.error.permanent` → `oracle-error-permanent`; exclude from denominator |
| `internal/difftest/duckdb_diff_test.go` | Modify | Same |
| `internal/difftest/envelope_diff_test.go` | Modify | Same |
| `testdata/difftest/golden/tpch/1.sql.error` | Rename → `.error.permanent` | Sentinel for known Java 0.9.3 bug |
| `testdata/difftest/golden/tpch/4.sql.error` | Rename → `.error.permanent` | Sentinel for known Java 0.9.3 bug |
| `docs/phase3-scoreboard.md` | Create | Quantified pass-rate tables + go-error inventory |
| `docs/oracle-runbook.md` | Create | 3-step standard operating procedure for re-capturing golden |

**Files produced by running capture (not in plan, listed for completeness):**
| `testdata/difftest/golden-envelope/*/*.json` | Capture output | 8 envelope golden files |
| `testdata/difftest/golden-duckdb/*/*.sql` | Capture output | 45 DuckDB-dialect golden files |
| `testdata/difftest/baseline*.json` | Rebaseline output | Rotated baselines + `.bak` backups |

---

## Task 1: Oracle Lifecycle Scripts

**Files:**
- Create: `tools/oracle-up.sh`
- Create: `tools/oracle-down.sh`
- Modify: `Makefile`

### Step 1: Write `tools/oracle-up.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

IMAGE="${WREN_ORACLE_IMAGE:-ghcr.io/canner/wren-engine:0.9.3}"
PORT="${WREN_ORACLE_PORT:-18080}"
MOUNT_DIR="${WREN_ORACLE_MOUNT:-}"

# Derive mount dir from sibling wren-engine-0.9.3 checkout if not set
if [ -z "$MOUNT_DIR" ]; then
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    CANDIDATE="$(cd "$SCRIPT_DIR/../wren-engine-0.9.3/example/duckdb-tpch-example/etc" 2>/dev/null && pwd)" || true
    if [ -n "$CANDIDATE" ] && [ -d "$CANDIDATE" ]; then
        MOUNT_DIR="$CANDIDATE"
    fi
fi

if [ -z "$MOUNT_DIR" ] || [ ! -d "$MOUNT_DIR" ]; then
    echo "ERROR: oracle mount directory not found." >&2
    echo "Please clone canner/wren-engine tag 0.9.3 into a sibling directory:" >&2
    echo "  cd .. && git clone --branch 0.9.3 https://github.com/canner/wren-engine.git wren-engine-0.9.3" >&2
    echo "Or set WREN_ORACLE_MOUNT to the path containing etc/config.properties." >&2
    exit 1
fi

# Check if already running
if docker ps --format '{{.Names}}' | grep -q '^wren-oracle$'; then
    echo "wren-oracle already running on port $PORT"
    exit 0
fi

echo "Pulling oracle image: $IMAGE"
if ! docker pull "$IMAGE" 2>&1; then
    echo "WARNING: docker pull failed." >&2
    echo "If you're behind a firewall, configure a Docker registry mirror," >&2
    echo "or pre-load the image with: docker load -i wren-engine-0.9.3.tar" >&2
    exit 1
fi

echo "Starting oracle container (name=wren-oracle, port=$PORT)..."
cid=$(docker run -d \
    --name wren-oracle \
    -p "${PORT}:8080" \
    -v "${MOUNT_DIR}:/usr/src/app/etc:ro" \
    -e MAX_HEAP_SIZE=2g \
    -e MIN_HEAP_SIZE=512m \
    "$IMAGE")

echo "Waiting for oracle readiness (container ${cid:0:12})..."
ready=""
for i in $(seq 1 30); do
    if curl -sf --max-time 2 "http://localhost:${PORT}/v1/config" >/dev/null 2>&1; then
        ready=1
        break
    fi
    sleep 2
done

if [ -z "${ready}" ]; then
    echo "ERROR: oracle failed to start within 60s" >&2
    docker logs --tail 30 wren-oracle >&2
    docker rm -f wren-oracle >/dev/null 2>&1 || true
    exit 1
fi

count=$(curl -sf --max-time 5 "http://localhost:${PORT}/v1/config" | grep -o '"name"' | wc -l | tr -d ' ')
echo "Oracle ready: http://localhost:${PORT} ($count config entries)"
```

```bash
chmod +x tools/oracle-up.sh
```

### Step 2: Write `tools/oracle-down.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
docker rm -f wren-oracle >/dev/null 2>&1 || true
echo "Oracle stopped"
```

```bash
chmod +x tools/oracle-down.sh
```

### Step 3: Add Makefile targets

Append to `Makefile` before the `# ── Docker image targets ──` divider line:

```makefile
# ── Oracle lifecycle ────────────────────────────────────────────

oracle-up:
	./tools/oracle-up.sh

oracle-down:
	./tools/oracle-down.sh

oracle-logs:
	docker logs --tail 50 wren-oracle

# ── Full capture pipeline ──────────────────────────────────────

capture-all-golden: oracle-up
	go run ./cmd/capture-golden -addr http://localhost:18080
	go run ./cmd/capture-duckdb-golden -addr http://localhost:18080
	go run ./cmd/capture-envelope-golden -addr http://localhost:18080 --groups all
	go run ./cmd/capture-analysis-golden -addr http://localhost:18080

rebaseline: capture-all-golden
	cp testdata/difftest/baseline.json testdata/difftest/baseline.json.bak
	cp testdata/difftest/baseline-duckdb.json testdata/difftest/baseline-duckdb.json.bak
	cp testdata/difftest/baseline-envelope.json testdata/difftest/baseline-envelope.json.bak
	cp testdata/difftest/baseline-analysis.json testdata/difftest/baseline-analysis.json.bak
	go test ./internal/difftest/... -run TestDifferential -difftest.accept -count=1
	go test ./internal/difftest/... -run TestDifferentialDuckDB -difftest.accept-duckdb -count=1
	go test ./internal/difftest/... -run TestEnvelopeDifferential -difftest.accept-envelope -count=1
	go test ./internal/difftest/... -run TestAnalysis -difftest.accept-analysis -count=1
	@echo "Rebaseline complete. Review diffs with:"
	@echo "  diff -u testdata/difftest/baseline.json.bak testdata/difftest/baseline.json"
```

### Step 4: Verify scripts exist and are executable

Run:
```bash
ls -la tools/oracle-*.sh
grep -A2 "oracle-up:" Makefile
grep -A2 "capture-all-golden:" Makefile
```

Expected:
```
-rwxr-xr-x tools/oracle-up.sh
-rwxr-xr-x tools/oracle-down.sh
oracle-up:
	./tools/oracle-up.sh
...
capture-all-golden: oracle-up
```

### Step 5: Commit

```bash
git add tools/oracle-up.sh tools/oracle-down.sh Makefile
git commit -m "feat(tools): add oracle-up/oracle-down scripts and Makefile targets"
```

---

## Task 2: Unify Capture-Golden Flags

**Files:**
- Modify: `cmd/capture-golden/main.go`

### Step 1: Rewrite capture-golden with unified flags

Replace the entire file content:

```go
// Command capture-golden replays every difftest case against a running Java
// wren-engine and freezes the rewritten SQL into golden files.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	addr := flag.String("addr", "http://localhost:18080", "wren-engine oracle base URL")
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus directory")
	outDir := flag.String("out", "testdata/difftest/golden", "golden output directory")
	groupsFlag := flag.String("groups", "all", "comma-separated corpus groups, or 'all'")
	timeout := flag.Duration("timeout", 60*time.Second, "per-request timeout")
	retryCount := flag.Int("retry", 0, "retry count on transient failures")
	flag.Parse()

	cases, err := difftest.LoadCorpus(*casesDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	var wantGroups map[string]bool
	if *groupsFlag != "all" {
		wantGroups = map[string]bool{}
		for _, g := range strings.Split(*groupsFlag, ",") {
			wantGroups[strings.TrimSpace(g)] = true
		}
	}

	client := &http.Client{Timeout: *timeout}
	var ok, errs int
	var total int

	for _, c := range cases {
		if wantGroups != nil && !wantGroups[c.Group] {
			continue
		}
		total++

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

		resp, err := doWithRetry(client, req, *retryCount)
		if err != nil {
			log.Fatalf("%s: request failed after %d retries (is the oracle up?): %v", c.ID(), *retryCount, err)
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
			os.Remove(base + ".error.permanent")
			ok++
			fmt.Printf("OK    %s\n", c.ID())
		} else {
			writeFile(base+".error", payload)
			os.Remove(base)
			os.Remove(base + ".error.permanent")
			errs++
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
		}
	}
	fmt.Printf("summary: ok=%d errs=%d total=%d\n", ok, errs, total)
	if errs > 0 {
		os.Exit(1)
	}
}

func doWithRetry(client *http.Client, req *http.Request, retries int) (*http.Response, error) {
	var lastErr error
	for i := 0; i <= retries; i++ {
		resp, err := client.Do(req.Clone(context.Background()))
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if i < retries {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}
	return nil, lastErr
}

func writeFile(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
}
```

### Step 2: Build and verify flags

```bash
go build ./cmd/capture-golden
go run ./cmd/capture-golden --help 2>&1
```

Expected output contains:
```
-addr string
-cases string
-groups string
-out string
-retry int
-timeout duration
```

### Step 3: Commit

```bash
git add cmd/capture-golden/main.go
git commit -m "feat(capture): unify capture-golden flags (--groups, --timeout, --retry, summary)"
```

---

## Task 3: Unify Capture-Envelope-Golden Flags

**Files:**
- Modify: `cmd/capture-envelope-golden/main.go`

### Step 1: Rewrite capture-envelope-golden with unified flags

Replace the entire file content:

```go
// Command capture-envelope-golden replays every case against a running Java
// wren-engine via /v1/mdl/preview and freezes the JSON envelope under
// testdata/difftest/golden-envelope/.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	addr := flag.String("addr", "http://localhost:18080", "wren-engine oracle base URL")
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus dir")
	outDir := flag.String("out", "testdata/difftest/golden-envelope", "envelope golden dir")
	groupsFlag := flag.String("groups", "exec_smoke,viewenum", "comma-separated corpus groups, or 'all'")
	timeout := flag.Duration("timeout", 60*time.Second, "per-request timeout")
	retryCount := flag.Int("retry", 0, "retry count on transient failures")
	flag.Parse()

	cases, err := difftest.LoadCorpus(*casesDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	var wantGroups map[string]bool
	if *groupsFlag != "all" {
		wantGroups = map[string]bool{}
		for _, g := range strings.Split(*groupsFlag, ",") {
			wantGroups[strings.TrimSpace(g)] = true
		}
	}

	client := &http.Client{Timeout: *timeout}
	ok, errs, total := 0, 0, 0

	for _, c := range cases {
		if wantGroups != nil && !wantGroups[c.Group] {
			continue
		}
		total++

		body, _ := json.Marshal(map[string]any{
			"manifest": json.RawMessage(c.ManifestJSON),
			"sql":      c.SQL,
			"limit":    100,
		})
		req, err := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/preview", bytes.NewReader(body))
		if err != nil {
			log.Fatalf("%s: build request: %v", c.ID(), err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := doWithRetry(client, req, *retryCount)
		if err != nil {
			log.Fatalf("%s: request failed after %d retries: %v", c.ID(), *retryCount, err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		dir := filepath.Join(*outDir, c.Group)
		_ = os.MkdirAll(dir, 0o755)
		base := filepath.Join(dir, c.Name+".json")
		if resp.StatusCode/100 == 2 {
			_ = os.WriteFile(base, payload, 0o644)
			_ = os.Remove(base + ".error")
			_ = os.Remove(base + ".error.permanent")
			ok++
		} else {
			_ = os.WriteFile(base+".error", payload, 0o644)
			_ = os.Remove(base)
			_ = os.Remove(base + ".error.permanent")
			errs++
		}
		fmt.Printf("%s %s\n", map[bool]string{true: "OK   ", false: "ERROR"}[resp.StatusCode/100 == 2], c.ID())
	}
	fmt.Printf("summary: ok=%d errs=%d total=%d (envelope, groups=%s)\n", ok, errs, total, *groupsFlag)
	if errs > 0 {
		os.Exit(1)
	}
}

func doWithRetry(client *http.Client, req *http.Request, retries int) (*http.Response, error) {
	var lastErr error
	for i := 0; i <= retries; i++ {
		resp, err := client.Do(req.Clone(context.Background()))
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if i < retries {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}
	return nil, lastErr
}
```

### Step 2: Build and verify

```bash
go build ./cmd/capture-envelope-golden
go run ./cmd/capture-envelope-golden --help 2>&1 | grep -E "addr|cases|groups|timeout|retry"
```

Expected: all 5 flags listed.

### Step 3: Commit

```bash
git add cmd/capture-envelope-golden/main.go
git commit -m "feat(capture): unify envelope golden flags (--groups=all, --timeout, --retry, summary)"
```

---

## Task 4: Unify Capture-DuckDB-Golden Flags

**Files:**
- Modify: `cmd/capture-duckdb-golden/main.go`

### Step 1: Rewrite capture-duckdb-golden with unified flags

Replace the entire file content:

```go
// Command capture-duckdb-golden replays every difftest case against a running
// Java wren-engine with modelingOnly=false (post DuckDB conversion) and
// freezes the dialect-converted SQL into golden files under golden-duckdb/.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	addr := flag.String("addr", "http://localhost:18080", "wren-engine oracle base URL")
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus directory")
	outDir := flag.String("out", "testdata/difftest/golden-duckdb", "DuckDB golden output dir")
	groupsFlag := flag.String("groups", "all", "comma-separated corpus groups, or 'all'")
	timeout := flag.Duration("timeout", 60*time.Second, "per-request timeout")
	retryCount := flag.Int("retry", 0, "retry count on transient failures")
	flag.Parse()

	cases, err := difftest.LoadCorpus(*casesDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	var wantGroups map[string]bool
	if *groupsFlag != "all" {
		wantGroups = map[string]bool{}
		for _, g := range strings.Split(*groupsFlag, ",") {
			wantGroups[strings.TrimSpace(g)] = true
		}
	}

	client := &http.Client{Timeout: *timeout}
	var ok, errs, total int

	for _, c := range cases {
		if wantGroups != nil && !wantGroups[c.Group] {
			continue
		}
		total++

		body, _ := json.Marshal(map[string]any{
			"manifest":     json.RawMessage(c.ManifestJSON),
			"sql":          c.SQL,
			"modelingOnly": false,
		})
		req, err := http.NewRequest(http.MethodGet, *addr+"/v1/mdl/dry-plan", bytes.NewReader(body))
		if err != nil {
			log.Fatalf("%s: build request: %v", c.ID(), err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := doWithRetry(client, req, *retryCount)
		if err != nil {
			log.Fatalf("%s: request failed after %d retries: %v", c.ID(), *retryCount, err)
		}
		payload, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		dir := filepath.Join(*outDir, c.Group)
		_ = os.MkdirAll(dir, 0o755)
		base := filepath.Join(dir, c.Name+".sql")
		if resp.StatusCode/100 == 2 {
			_ = os.WriteFile(base, payload, 0o644)
			_ = os.Remove(base + ".error")
			_ = os.Remove(base + ".error.permanent")
			ok++
			fmt.Printf("OK    %s\n", c.ID())
		} else {
			_ = os.WriteFile(base+".error", payload, 0o644)
			_ = os.Remove(base)
			_ = os.Remove(base + ".error.permanent")
			errs++
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
		}
	}
	fmt.Printf("summary: ok=%d errs=%d total=%d (duckdb-dialect)\n", ok, errs, total)
	if errs > 0 {
		os.Exit(1)
	}
}

func doWithRetry(client *http.Client, req *http.Request, retries int) (*http.Response, error) {
	var lastErr error
	for i := 0; i <= retries; i++ {
		resp, err := client.Do(req.Clone(context.Background()))
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if i < retries {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}
	return nil, lastErr
}
```

### Step 2: Build and verify

```bash
go build ./cmd/capture-duckdb-golden
go run ./cmd/capture-duckdb-golden --help 2>&1 | grep -E "addr|groups|timeout|retry"
```

### Step 3: Commit

```bash
git add cmd/capture-duckdb-golden/main.go
git commit -m "feat(capture): unify duckdb golden flags (--groups, --timeout, --retry, summary)"
```

---

## Task 5: Migrate Capture-Analysis-Golden to Flags

**Files:**
- Modify: `cmd/capture-analysis-golden/main.go`

### Step 1: Rewrite with flag interface (backward-compatible positional args)

Replace the entire file content:

```go
// Command capture-analysis-golden replays every difftest case against the
// Java wren-engine /v2/analysis/sql endpoint and freezes normalized JSON.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wren-engine/wren/internal/difftest"
)

func main() {
	addr := flag.String("addr", "http://localhost:18080", "java oracle URL")
	casesDir := flag.String("cases", "testdata/difftest/cases", "corpus root")
	outDir := flag.String("out", "testdata/difftest/golden-analysis", "output directory")
	groupsFlag := flag.String("groups", "all", "comma-separated groups, or 'all'")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout")
	retryCount := flag.Int("retry", 0, "retry count on transient failures")
	flag.Parse()

	// Backward compatibility: accept up to 2 positional args
	javaURL := strings.TrimSuffix(*addr, "/")
	if len(flag.Args()) >= 1 {
		javaURL = strings.TrimSuffix(flag.Args()[0], "/")
	}
	if len(flag.Args()) >= 2 {
		*casesDir = flag.Args()[1]
	}
	_ = os.MkdirAll(*outDir, 0o755)

	cases, err := difftest.LoadCorpus(*casesDir)
	if err != nil {
		log.Fatalf("load corpus: %v", err)
	}

	var wantGroups map[string]bool
	if *groupsFlag != "all" {
		wantGroups = map[string]bool{}
		for _, g := range strings.Split(*groupsFlag, ",") {
			wantGroups[strings.TrimSpace(g)] = true
		}
	}

	client := &http.Client{Timeout: *timeout}
	ok, errs, total := 0, 0, 0

	for _, c := range cases {
		if wantGroups != nil && !wantGroups[c.Group] {
			continue
		}
		total++

		groupDir := filepath.Join(*outDir, c.Group)
		_ = os.MkdirAll(groupDir, 0o755)
		outPath := filepath.Join(groupDir, c.Name+".json")

		reqBody, _ := json.Marshal(map[string]any{
			"manifestStr": base64.StdEncoding.EncodeToString(c.ManifestJSON),
			"sql":         c.SQL,
		})
		req, err := http.NewRequest(http.MethodGet, javaURL+"/v2/analysis/sql", bytes.NewReader(reqBody))
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: build request: %v\n", c.ID(), err)
			errs++
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := doWithRetry(client, req, *retryCount)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: request error after %d retries: %v\n", c.ID(), *retryCount, err)
			errs++
			continue
		}
		body := &bytes.Buffer{}
		body.ReadFrom(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			_ = os.WriteFile(outPath+".error", body.Bytes(), 0o644)
			_ = os.Remove(outPath)
			fmt.Printf("ERROR %s (HTTP %d)\n", c.ID(), resp.StatusCode)
			errs++
			continue
		}

		// Normalize: sort exprSources arrays for reproducible ordering.
		var payload []map[string]any
		if err := json.Unmarshal(body.Bytes(), &payload); err == nil {
			sortExprSourcesInPayload(payload)
			body.Reset()
			b, _ := json.MarshalIndent(payload, "", "  ")
			body.Write(b)
		}

		_ = os.WriteFile(outPath, body.Bytes(), 0o644)
		_ = os.Remove(outPath + ".error")
		fmt.Printf("OK    %s\n", c.ID())
		ok++
	}
	fmt.Printf("summary: ok=%d errs=%d total=%d (analysis)\n", ok, errs, total)
	if errs > 0 {
		os.Exit(1)
	}
}

func doWithRetry(client *http.Client, req *http.Request, retries int) (*http.Response, error) {
	var lastErr error
	for i := 0; i <= retries; i++ {
		resp, err := client.Do(req.Clone(context.Background()))
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if i < retries {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}
	return nil, lastErr
}

func sortExprSourcesInPayload(v any) {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			sortExprSourcesInPayload(item)
		}
	case map[string]any:
		if es, ok := x["exprSources"].([]any); ok {
			sort.Slice(es, func(i, j int) bool {
				mi, mj := es[i].(map[string]any), es[j].(map[string]any)
				li, lj := 0.0, 0.0
				ci, cj := 0.0, 0.0
				if nl, ok := mi["nodeLocation"].(map[string]any); ok {
					li, _ = nl["line"].(float64)
					ci, _ = nl["column"].(float64)
				}
				if nl, ok := mj["nodeLocation"].(map[string]any); ok {
					lj, _ = nl["line"].(float64)
					cj, _ = nl["column"].(float64)
				}
				if li != lj {
					return li < lj
				}
				if ci != cj {
					return ci < cj
				}
				si, _ := mi["expression"].(string)
				sj, _ := mj["expression"].(string)
				return si < sj
			})
			x["exprSources"] = es
		}
		for _, val := range x {
			sortExprSourcesInPayload(val)
		}
	}
}
```

### Step 2: Build and verify backward compat

```bash
go build ./cmd/capture-analysis-golden
go run ./cmd/capture-analysis-golden --help 2>&1 | grep -E "addr|cases|groups|timeout|retry"
```

Expected: all flags listed.

Verify old positional-arg invocation still works (dry-run, oracle not required):
```bash
go run ./cmd/capture-analysis-golden http://localhost:18080 testdata/difftest/cases --help 2>&1 | head -1
```

Actually that will fail because flag.Parse swallows `--help`. Let's just check it builds.

### Step 3: Update Makefile target

In `Makefile`, replace the `capture-analysis-golden:` target:

```makefile
capture-analysis-golden:
	go run ./cmd/capture-analysis-golden -addr http://localhost:18080
```

### Step 4: Commit

```bash
git add cmd/capture-analysis-golden/main.go Makefile
git commit -m "feat(capture): migrate analysis golden to flags with backward compat"
```

---

## Task 6: Introduce `oracle-error-permanent` in Difftest Framework

**Files:**
- Modify: `internal/difftest/difftest_test.go`
- Modify: `internal/difftest/duckdb_diff_test.go`
- Modify: `internal/difftest/envelope_diff_test.go`

### Step 1: Modify `difftest_test.go`

Replace the `runCase` function:

```go
// runCase rewrites one case with the Go engine and compares it to the frozen
// Java golden. It returns one of: pass, fail, parser-gap, go-error,
// oracle-error, oracle-error-permanent, no-golden.
func runCase(c Case) (status string, detail string) {
	goldenBase := filepath.Join(goldenDir, c.Group, c.Name+".sql")
	if _, err := os.Stat(goldenBase + ".error.permanent"); err == nil {
		return "oracle-error-permanent", "Java engine returned a permanent error for this case (known Java bug)"
	}
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java engine returned an error for this case"
	}
	wantSQL, err := os.ReadFile(goldenBase)
	if err != nil {
		return "no-golden", "golden file missing; run `make capture-golden`"
	}
	// ... rest unchanged
```

Replace the `TestDifferential` comparison loop:

```go
	regressions, improvements := 0, 0
	for id, status := range results {
		want := base.Cases[id]
		switch {
		case want == "pass" && status != "pass":
			regressions++
			t.Errorf("REGRESSION %s: baseline=pass, now=%s", id, status)
		case want == "oracle-error-permanent" && status == "pass":
			improvements++
			t.Errorf("%s now passes (was oracle-error-permanent) — run `make difftest-accept`", id)
		case want != "pass" && want != "oracle-error-permanent" && status == "pass":
			improvements++
			t.Errorf("%s now passes — run `make difftest-accept` to update baseline", id)
		}
	}
```

Replace the `summary` function:

```go
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
	denominator := len(results) - counts["oracle-error-permanent"]
	s := fmt.Sprintf("差分计分板: %d/%d 通过", counts["pass"], denominator)
	for _, k := range keys {
		s += fmt.Sprintf("\n  %-24s %d", k, counts[k])
	}
	return s
}
```

### Step 2: Modify `duckdb_diff_test.go`

Replace `runDuckdbCase`:

```go
func runDuckdbCase(c Case) (status, detail string) {
	goldenBase := filepath.Join(goldenDuckdbDir, c.Group, c.Name+".sql")
	if _, err := os.Stat(goldenBase + ".error.permanent"); err == nil {
		return "oracle-error-permanent", "Java engine returned a permanent error (DuckDB mode)"
	}
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java engine returned an error for this case (DuckDB mode)"
	}
	// ... rest unchanged
```

Replace `TestDifferentialDuckDB` comparison loop:

```go
	for id, st := range results {
		w := base.Cases[id]
		switch {
		case w == "pass" && st != "pass":
			regr++
			t.Errorf("REGRESSION %s: baseline=pass, now=%s", id, st)
		case w == "oracle-error-permanent" && st == "pass":
			impr++
			t.Errorf("%s now passes (was oracle-error-permanent) — run `make duckdb-difftest-accept`", id)
		case w != "pass" && w != "oracle-error-permanent" && st == "pass":
			impr++
			t.Errorf("%s now passes — run `make duckdb-difftest-accept` to update", id)
		}
	}
```

Replace `duckdbSummary`:

```go
func duckdbSummary(results map[string]string) string {
	counts := map[string]int{}
	for _, s := range results {
		counts[s]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	denominator := len(results) - counts["oracle-error-permanent"]
	s := fmt.Sprintf("DUCKDB 方言差分计分板: %d/%d 通过", counts["pass"], denominator)
	for _, k := range keys {
		s += fmt.Sprintf("\n  %-24s %d", k, counts[k])
	}
	return s
}
```

### Step 3: Modify `envelope_diff_test.go`

Replace `runEnvelopeCase`:

```go
func runEnvelopeCase(t *testing.T, c Case) (status, detail string) {
	t.Helper()
	goldenBase := filepath.Join(envelopeDir, c.Group, c.Name+".json")
	if _, err := os.Stat(goldenBase + ".error.permanent"); err == nil {
		return "oracle-error-permanent", "Java preview errored permanently"
	}
	if _, err := os.Stat(goldenBase + ".error"); err == nil {
		return "oracle-error", "Java preview errored"
	}
	// ... rest unchanged
```

Replace `TestEnvelopeDifferential` comparison loop:

```go
	for id, st := range results {
		w := base.Cases[id]
		switch {
		case w == "pass" && st != "pass":
			t.Errorf("ENVELOPE REGRESSION %s: baseline=pass, now=%s", id, st)
		case w == "oracle-error-permanent" && st == "pass":
			t.Errorf("ENVELOPE %s now passes (was oracle-error-permanent) — run `make envelope-difftest-accept`", id)
		case w != "pass" && w != "oracle-error-permanent" && st == "pass":
			t.Errorf("ENVELOPE IMPROVEMENT %s now passes — run `make envelope-difftest-accept`", id)
		}
	}
```

Add envelopeSummary function and update TestEnvelopeDifferential:

```go
	// After the comparison loop, add:
	t.Logf("\n%s", envelopeSummary(results))
```

```go
func envelopeSummary(results map[string]string) string {
	counts := map[string]int{}
	for _, s := range results {
		counts[s]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	denominator := len(results) - counts["oracle-error-permanent"]
	s := fmt.Sprintf("Envelope 差分计分板: %d/%d 通过", counts["pass"], denominator)
	for _, k := range keys {
		s += fmt.Sprintf("\n  %-24s %d", k, counts[k])
	}
	return s
}
```

### Step 4: Run tests to verify no compile errors

```bash
go test ./internal/difftest/... -run TestDifferential -count=1 2>&1 | tail -20
```

Expected: compiles cleanly, runs (results will show oracle-error-permanent for tpch/1 and tpch/4 after next task).

### Step 5: Commit

```bash
git add internal/difftest/difftest_test.go internal/difftest/duckdb_diff_test.go internal/difftest/envelope_diff_test.go
git commit -m "feat(difftest): introduce oracle-error-permanent status"
```

---

## Task 7: Rename Permanent Oracle-Error Files

**Files:**
- Rename: `testdata/difftest/golden/tpch/1.sql.error` → `.error.permanent`
- Rename: `testdata/difftest/golden/tpch/4.sql.error` → `.error.permanent`

### Step 1: Create `.error.permanent` sentinel files

```bash
# Read the existing error content
cat testdata/difftest/golden/tpch/1.sql.error
```

Create the permanent sentinel:
```bash
cat > testdata/difftest/golden/tpch/1.sql.error.permanent << 'EOF'
# Permanent oracle error — Java 0.9.3: IllegalArgumentException: count(*) should have a followed source
# Reproduce: POST /v1/mdl/dry-plan with modelingOnly=group-config and tpch query 1
EOF
cat testdata/difftest/golden/tpch/1.sql.error >> testdata/difftest/golden/tpch/1.sql.error.permanent
rm testdata/difftest/golden/tpch/1.sql.error
```

Repeat for tpch/4:
```bash
cat > testdata/difftest/golden/tpch/4.sql.error.permanent << 'EOF'
# Permanent oracle error — Java 0.9.3: IllegalArgumentException: count(*) should have a followed source
# Reproduce: POST /v1/mdl/dry-plan with modelingOnly=group-config and tpch query 4
EOF
cat testdata/difftest/golden/tpch/4.sql.error >> testdata/difftest/golden/tpch/4.sql.error.permanent
rm testdata/difftest/golden/tpch/4.sql.error
```

### Step 2: Verify difftest recognizes them

```bash
go test ./internal/difftest/... -run TestDifferential -count=1 2>&1 | grep -E "tpch/1|tpch/4|oracle-error-permanent"
```

Expected:
```
  oracle-error-permanent   tpch/1 — Java engine returned a permanent error...
  oracle-error-permanent   tpch/4 — Java engine returned a permanent error...
```

### Step 3: Commit

```bash
git add testdata/difftest/golden/tpch/1.sql.error.permanent testdata/difftest/golden/tpch/4.sql.error.permanent
git rm testdata/difftest/golden/tpch/1.sql.error testdata/difftest/golden/tpch/4.sql.error
git commit -m "test(golden): mark tpch/1 and tpch/4 as oracle-error-permanent"
```

---

## Task 8: Full Capture + Rebaseline Execution

**Files:** (capture output, no code changes in this task)
- `testdata/difftest/golden-envelope/*`
- `testdata/difftest/golden-duckdb/*`
- `testdata/difftest/baseline*.json` (rotated)

### Step 1: Start the Java oracle

Prerequisite: sibling `wren-engine-0.9.3` checkout exists with TPC-H example.

```bash
make oracle-up
```

Expected: exits 0, prints "Oracle ready" with 11 config entries.

### Step 2: Verify TPC-H data is loaded

```bash
curl -sf --max-time 10 "http://localhost:18080/v1/data-source/duckdb/query" \
  -X POST -H "Content-Type: application/json" \
  -d '{"sql": "SHOW TABLES"}' | head -c 200
```

Expected: response contains table names like `orders`, `lineitem`, etc. If empty, the oracle mount may be wrong.

### Step 3: Run full capture

```bash
make capture-all-golden
```

Expected: four capture tools run sequentially. Each prints per-case OK/ERROR and ends with `summary: ok=X errs=Y total=Z`. Note the error counts for each tool.

### Step 4: Stop oracle

```bash
make oracle-down
```

### Step 5: Rebaseline

```bash
make rebaseline
```

Expected: four baselines backed up to `.bak`, then rewritten by `-difftest.accept*` flags.

### Step 6: Review diffs

```bash
for f in baseline baseline-duckdb baseline-envelope baseline-analysis; do
  echo "=== $f ==="
  diff -u testdata/difftest/${f}.json.bak testdata/difftest/${f}.json | head -30
done
```

Expected:
- `baseline.json`: tpch/1 and tpch/4 changed from `oracle-error` to `oracle-error-permanent`. No other changes.
- `baseline-duckdb.json`: all `no-golden` replaced with `pass`/`fail`/`go-error`/`oracle-error` (or `oracle-error-permanent` for q1/q4).
- `baseline-envelope.json`: all `no-golden` replaced with actual results.
- `baseline-analysis.json`: changes according to capture output.

### Step 7: Commit capture outputs and new baselines

```bash
git add testdata/difftest/golden-envelope/ testdata/difftest/golden-duckdb/ testdata/difftest/baseline*.json
git commit -m "test(golden): full capture for envelope + duckdb dialect + rebaseline"
```

---

## Task 9: Quantified Scoreboard

**Files:**
- Create: `docs/phase3-scoreboard.md`

### Step 1: Write scoreboard

```markdown
# Phase 3 Scoreboard: Go vs Java wren-engine 0.9.3

Generated after full capture against `ghcr.io/canner/wren-engine:0.9.3`.

## Rewrite Baseline (`modelingOnly=group-config`)

| Status | Count |
|--------|-------|
| pass | TBD |
| fail | TBD |
| go-error | TBD |
| oracle-error-permanent | 2 (tpch/1, tpch/4) |
| **Drop-in pass rate** | **TBD%** |

### go-error Inventory (Phase 4 backlog)

| Case | Error |
|------|-------|
| tpch/m_orders | TBD |
| tpch/met_customer_revenue | TBD |
| tpch/met_daily | TBD |
| tpch/met_revenue | TBD |
| tpch/met_rollup | TBD |
| tpch/met_weekly | TBD |
| tpch/v_use_metric | TBD |
| tpch/v_use_model | TBD |
| tpch/v_use_nested | TBD |
| tpch/v_use_rollup | TBD |

## DuckDB-Dialect Baseline (`modelingOnly=false`)

| Status | Count |
|--------|-------|
| pass | TBD |
| fail | TBD |
| go-error | TBD |
| oracle-error-permanent | TBD |
| **Drop-in pass rate** | **TBD%** |

## Envelope Baseline (`/v1/mdl/preview`)

| Status | Count |
|--------|-------|
| pass | TBD |
| fail | TBD |
| go-error | TBD |
| oracle-error-permanent | TBD |
| **Drop-in pass rate** | **TBD%** |

## Analysis Baseline (`/v2/analysis/sql`)

| Status | Count |
|--------|-------|
| pass | TBD |
| fail | TBD |
| go-error | TBD |
| oracle-error | TBD |
| **Drop-in pass rate** | **TBD%** |
```

> **Note:** Fill the "TBD" values after running `make rebaseline` by inspecting the generated baselines and running `go test ./internal/difftest/...` to get exact counts.

### Step 2: Commit

```bash
git add docs/phase3-scoreboard.md
git commit -m "docs: add Phase 3 quantified scoreboard"
```

---

## Task 10: Oracle Runbook

**Files:**
- Create: `docs/oracle-runbook.md`

### Step 1: Write runbook

```markdown
# Oracle Runbook: Reproducible Golden Capture

This document describes the standard operating procedure for running the Java
wren-engine oracle to (re-)capture golden files.

## Prerequisites

1. Docker installed and running.
2. The `wren-engine-0.9.3` checkout exists as a sibling directory:
   ```bash
   cd .. && git clone --branch 0.9.3 https://github.com/canner/wren-engine.git wren-engine-0.9.3
   ```
3. Port 18080 is free on localhost.

## 3-Step Standard Flow

### Step 1: Start the oracle

```bash
make oracle-up
```

Wait for "Oracle ready" message. If it times out:
- Check `make oracle-logs` for startup errors.
- Verify the mount directory exists and contains `config.properties`.
- Ensure Docker has network access to `ghcr.io`.

### Step 2: Capture all golden files

```bash
make capture-all-golden
```

This runs 4 capture tools sequentially:
1. `capture-golden` → `golden/` (rewrite, modelingOnly=group-config)
2. `capture-duckdb-golden` → `golden-duckdb/` (DuckDB dialect, modelingOnly=false)
3. `capture-envelope-golden` → `golden-envelope/` (preview JSON envelope)
4. `capture-analysis-golden` → `golden-analysis/` (analysis SQL)

Each tool prints `summary: ok=X errs=Y total=Z`. Non-zero `errs` are normal for
cases that trigger known Java bugs (e.g. tpch/1, tpch/4 with `count(*)`).

### Step 3: Rebaseline and review

```bash
make rebaseline
```

This backs up existing baselines to `.bak` and regenerates all 4 baseline JSON
files. **Always review the diffs before committing:**

```bash
diff -u testdata/difftest/baseline.json.bak testdata/difftest/baseline.json
```

Then stop the oracle:
```bash
make oracle-down
```

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `oracle-up` times out | Docker daemon down | `docker ps` to verify |
| `docker pull` fails | Network / firewall | Configure registry mirror or `docker load` |
| Capture returns HTTP 500 for all cases | Oracle crashed | `make oracle-logs` and restart |
| `golden-envelope` cases return empty | TPC-H data not loaded | Verify mount path contains `duckdb/init.sql` |
| Diffs show large unexpected changes | Java output unstable | Re-run `make capture-all-golden` and compare |
```

### Step 2: Commit

```bash
git add docs/oracle-runbook.md
git commit -m "docs: add oracle runbook for reproducible golden capture"
```

---

## Self-Review Checklist

### 1. Spec Coverage

| Design Requirement | Task |
|---|---|
| `tools/oracle-up.sh` + readiness probe | Task 1 |
| `tools/oracle-down.sh` | Task 1 |
| Makefile `oracle-up` / `oracle-down` / `oracle-logs` | Task 1 |
| 4 capture tools unified flags | Tasks 2–5 |
| `--groups` filtering | Tasks 2, 3, 4, 5 |
| `--timeout` per-request | Tasks 2, 3, 4, 5 |
| `--retry` transient failures | Tasks 2, 3, 4, 5 |
| `summary: ok=X errs=Y total=Z` tail | Tasks 2, 3, 4, 5 |
| `oracle-error-permanent` difftest status | Task 6 |
| `.error.permanent` sentinel files | Task 7 |
| `make capture-all-golden` | Task 1 (Makefile) |
| `make rebaseline` with `.bak` | Task 1 (Makefile) |
| Full capture execution | Task 8 |
| `docs/phase3-scoreboard.md` | Task 9 |
| `docs/oracle-runbook.md` | Task 10 |

**Gap:** None. All design requirements map to a task.

### 2. Placeholder Scan

- No "TBD" in code blocks (TBD only in scoreboard doc where values must be filled after capture).
- No "implement later" or "fill in details".
- All function signatures, file paths, and commands are exact.

### 3. Type Consistency

- `doWithRetry` signature: `func doWithRetry(client *http.Client, req *http.Request, retries int) (*http.Response, error)` — consistent across all 4 capture tools.
- `summary` / `duckdbSummary` / `envelopeSummary` all use `denominator := len(results) - counts["oracle-error-permanent"]` — consistent.
- `oracle-error-permanent` string literal — consistent across all 3 difftest files and capture tools.

---

## Execution Handoff

**Plan complete and saved to `.gpowers/plans/2026-05-20-phase3-tpch-golden-capture.md`.**

Two execution options:

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration. Use `gpowers:subagent-driven-development`.

**2. Inline Execution** — Execute tasks in this session using `gpowers:executing-plans`, batch execution with checkpoints.

**Which approach?**
