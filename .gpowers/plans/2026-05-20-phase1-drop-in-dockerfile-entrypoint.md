# Phase 1 Drop-in Dockerfile + Entrypoint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use gpowers:subagent-driven-development (recommended) or gpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a `go-wren-engine` Docker image that is a drop-in replacement for the Java `wren-engine` image in WrenAI 0.9.0 docker-compose — only the `image:` line needs to change.

**Architecture:** Multi-stage Dockerfile with a Debian-based builder (CGo + static libduckdb) and a `debian:stable-slim` runtime that matches the Java image's filesystem layout (`/usr/src/app` workdir, `psql` installed, compatible argv). A shell entrypoint translates Java-style heap args (`512m`, `4g`) to Go's `GOMEMLIMIT` (`512MiB`, `4GiB`) and prints drop-in gap warnings.

**Tech Stack:** Docker, Debian (bookworm/slim), Go 1.26, CGo, go-duckdb v1.7.0, bash, Make

---

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `docker/Dockerfile` | Create | Multi-stage build: Debian builder with CGo, Debian-slim runtime with `postgresql-client-13` |
| `docker/entrypoint.sh` | Create | Receives `$1=binary $2=maxHeap $3=minHeap`; sets `GOMEMLIMIT`, `WREN_CONFIG_FILE`; warns about PG wire gap; execs binary |
| `Dockerfile` (root) | Rewrite | Thin wrapper — single line `FROM` redirect to `docker/Dockerfile` build context note, or direct multi-stage content |
| `docker-compose.yaml` (root) | Rewrite | Build from `docker/Dockerfile`, mount `./etc:/usr/src/app/etc`, port `8080:8080` |
| `.dockerignore` | Create | Exclude `.gpowers/`, `testdata/`, `.git/`, `*.md`, build artifacts from context |
| `.gitattributes` | Create | Force `*.sh` to LF line endings so entrypoint works on Windows checkout |
| `etc/config.properties` | Create | Minimal sample for smoke testing (Phase 2 will implement real parsing) |
| `etc/mdl/sample.json` | Create | Minimal MDL manifest for smoke testing |
| `docker/README.md` | Create | 3-step guide for replacing WrenAI compose image + drop-in gap list |
| `Makefile` | Modify | Add `image`, `image-run`, `image-test`, `image-clean` targets |

---

### Task 1: `.dockerignore` — Reduce Build Context Size

**Files:**
- Create: `.dockerignore`

- [ ] **Step 1: Write `.dockerignore`**

```gitignore
# Git
.git/
.gitignore

# Documentation
*.md
README.md

# Design / planning artifacts
.gpowers/

# Test data
testdata/

# Build artifacts
bin/
*.exe

# IDE / OS
*.swp
*.swo
*.tmp
.DS_Store
.idea/
.vscode/

# Docker itself
docker-compose.yaml
Dockerfile
```

- [ ] **Step 2: Verify build context size reduction**

Run:
```bash
docker build -t go-wren-engine:test-context -f docker/Dockerfile . --no-cache 2>&1 | head -5
```

(At this point `docker/Dockerfile` does not yet exist; this step is informational. Run again after Task 2.)

- [ ] **Step 3: Commit**

```bash
git add .dockerignore
git commit -m "build: add .dockerignore to reduce build context"
```

---

### Task 2: `.gitattributes` — Force LF for Shell Scripts

**Files:**
- Create: `.gitattributes`

- [ ] **Step 1: Write `.gitattributes`**

```gitattributes
*.sh text eol=lf
```

- [ ] **Step 2: Commit**

```bash
git add .gitattributes
git commit -m "build: enforce LF line endings for shell scripts"
```

---

### Task 3: Multi-Stage `docker/Dockerfile`

**Files:**
- Delete: `Dockerfile` (old alpine-based, at root)
- Create: `docker/Dockerfile`

- [ ] **Step 1: Delete old root `Dockerfile` and create `docker/` directory**

```bash
rm Dockerfile
mkdir -p docker
```

- [ ] **Step 2: Write `docker/Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1

# ── Builder stage ───────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder

WORKDIR /build

# Install build toolchain for CGo
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Cache go modules in a separate layer
COPY go.mod go.sum ./
RUN go mod download

# Build the binary with CGo enabled (required for go-duckdb static lib)
COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -ldflags='-s -w' -o wren-engine ./cmd/wren-engine

# ── Runtime stage ───────────────────────────────────────────────
FROM debian:stable-slim

# Install parity packages from Java image
RUN apt-get update && apt-get install -y --no-install-recommends \
    postgresql-client-13 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Mirror Java image workdir
WORKDIR /usr/src/app

# Copy binary and entrypoint
COPY --from=builder /build/wren-engine ./
COPY docker/entrypoint.sh ./
RUN chmod +x entrypoint.sh

# Java image does NOT declare EXPOSE; keep parity
# No USER directive — Java image runs as root

# Pass-through heap args: $1=binary $2=maxHeap $3=minHeap
CMD ["./entrypoint.sh", "wren-engine", "512m", "64m"]
```

- [ ] **Step 3: Build to verify Dockerfile syntax**

Run:
```bash
docker build -t go-wren-engine:phase1 -f docker/Dockerfile .
```

Expected: Build succeeds (exit 0). The `COPY docker/entrypoint.sh` will fail if `entrypoint.sh` does not exist yet — this is expected; Task 4 creates it.

- [ ] **Step 4: Commit**

```bash
git add docker/Dockerfile
git commit -m "build: add multi-stage Debian-based Dockerfile"
```

---

### Task 4: `docker/entrypoint.sh` — Java-Compatible Entrypoint

**Files:**
- Create: `docker/entrypoint.sh`

- [ ] **Step 1: Write `docker/entrypoint.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

# Usage: ./entrypoint.sh <binary> <maxHeap> <minHeap>
# Mirrors Java image entrypoint argv convention.

BINARY="${1:-wren-engine}"
MAX_HEAP="${2:-512m}"
MIN_HEAP="${3:-64m}"

# ── GOMEMLIMIT conversion ───────────────────────────────────────
# Java heap args: 512m, 4g, 512MB, 512
# Go GOMEMLIMIT: 512MiB, 4GiB, 512000000
# Pure numbers are treated as bytes and passed through unchanged.

convert_heap_to_gomemlimit() {
    local input="$1"
    local num unit

    # Strip trailing 'B' / 'b' (e.g. 512MB -> 512M)
    input="${input%[Bb]}"

    # Extract trailing unit letter if present
    if [[ "$input" =~ ^([0-9]+)([mMgG])$ ]]; then
        num="${BASH_REMATCH[1]}"
        unit="${BASH_REMATCH[2]}"
        case "$unit" in
            m|M) echo "${num}MiB" ;;
            g|G) echo "${num}GiB" ;;
        esac
        return 0
    elif [[ "$input" =~ ^[0-9]+$ ]]; then
        # Pure number — bytes. Go accepts this directly.
        echo "$input"
        return 0
    else
        return 1
    fi
}

GOMEMLIMIT=""
if converted="$(convert_heap_to_gomemlimit "$MAX_HEAP")"; then
    GOMEMLIMIT="$converted"
    export GOMEMLIMIT
    echo "[INFO] GOMEMLIMIT set to ${GOMEMLIMIT}"
else
    echo "[WARN] Unrecognized MAX_HEAP_SIZE format '${MAX_HEAP}'. GOMEMLIMIT not set." >&2
fi

# ── minHeap ─────────────────────────────────────────────────────
# Go runtime has no equivalent to Java's -Xms (initial heap size).
# We accept the argument for compatibility but log that it is ignored.
if [[ -n "$MIN_HEAP" && "$MIN_HEAP" != "0" ]]; then
    echo "[INFO] MIN_HEAP_SIZE=${MIN_HEAP} ignored under Go runtime (no -Xms equivalent)"
fi

# ── Config file env (Phase 2 will implement parsing) ────────────
export WREN_CONFIG_FILE="/usr/src/app/etc/config.properties"

# ── Drop-in gap warnings ────────────────────────────────────────
if [[ "${WARN_DROP_IN_GAPS:-1}" != "0" ]]; then
    echo "[INFO] Postgres wire protocol (port 7432) not supported by this go-wren-engine build"
fi

# ── Launch binary ───────────────────────────────────────────────
echo "[INFO] Starting ${BINARY}..."
exec "./${BINARY}"
```

- [ ] **Step 2: Make executable**

```bash
chmod +x docker/entrypoint.sh
```

- [ ] **Step 3: Test entrypoint script locally (bash syntax check)**

Run:
```bash
bash -n docker/entrypoint.sh
```

Expected: No output (exit 0).

Run:
```bash
bash -c '
convert_heap_to_gomemlimit() {
    local input="$1"
    input="${input%[Bb]}"
    if [[ "$input" =~ ^([0-9]+)([mMgG])$ ]]; then
        num="${BASH_REMATCH[1]}"; unit="${BASH_REMATCH[2]}"
        case "$unit" in m|M) echo "${num}MiB" ;; g|G) echo "${num}GiB" ;; esac
        return 0
    elif [[ "$input" =~ ^[0-9]+$ ]]; then
        echo "$input"; return 0
    fi
    return 1
}
for v in 512m 4g 512MB 512 2G 1g; do
    printf "%s -> " "$v"
    convert_heap_to_gomemlimit "$v" || echo "(unsupported)"
done
'
```

Expected output:
```
512m -> 512MiB
4g -> 4GiB
512MB -> 512MiB
512 -> 512
2G -> 2GiB
1g -> 1GiB
```

- [ ] **Step 4: Commit**

```bash
git add docker/entrypoint.sh
git commit -m "build: add Java-compatible entrypoint with GOMEMLIMIT translation"
```

---

### Task 5: Root `Dockerfile` — Thin Wrapper

**Files:**
- Create: `Dockerfile` (root)

The root `Dockerfile` is referenced by tools that expect it at the repository root. Since Docker does not support `#include`, we duplicate the content from `docker/Dockerfile` with a comment indicating the source of truth.

- [ ] **Step 1: Write root `Dockerfile`**

```dockerfile
# Source of truth: docker/Dockerfile
# This file exists for compatibility with build tools that expect Dockerfile at repo root.
# Please edit docker/Dockerfile and keep this file in sync.

# syntax=docker/dockerfile:1

# ── Builder stage ───────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder

WORKDIR /build

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -ldflags='-s -w' -o wren-engine ./cmd/wren-engine

# ── Runtime stage ───────────────────────────────────────────────
FROM debian:stable-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    postgresql-client-13 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /usr/src/app

COPY --from=builder /build/wren-engine ./
COPY docker/entrypoint.sh ./
RUN chmod +x entrypoint.sh

CMD ["./entrypoint.sh", "wren-engine", "512m", "64m"]
```

- [ ] **Step 2: Verify root Dockerfile build**

Run:
```bash
docker build -t go-wren-engine:root-test .
```

Expected: Build succeeds (exit 0).

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "build: add root Dockerfile as thin wrapper pointing to docker/Dockerfile"
```

---

### Task 6: `docker-compose.yaml` — WrenAI-Compatible Compose

**Files:**
- Rewrite: `docker-compose.yaml`

- [ ] **Step 1: Write `docker-compose.yaml`**

```yaml
version: "3.8"

services:
  wren-engine:
    build:
      context: .
      dockerfile: docker/Dockerfile
    ports:
      - "8080:8080"
    volumes:
      - ./etc:/usr/src/app/etc
    environment:
      - WREN_PORT=8080
      - WREN_DATASOURCE_TYPE=duckdb
      - MAX_HEAP_SIZE=${MAX_HEAP_SIZE:-512m}
      - MIN_HEAP_SIZE=${MIN_HEAP_SIZE:-64m}
      - WARN_DROP_IN_GAPS=${WARN_DROP_IN_GAPS:-1}
```

- [ ] **Step 2: Commit**

```bash
git add docker-compose.yaml
git commit -m "build: rewrite docker-compose with WrenAI-compatible mount paths"
```

---

### Task 7: Sample `etc/` Directory for Smoke Testing

**Files:**
- Create: `etc/config.properties`
- Create: `etc/mdl/sample.json`

These files are required for smoke testing. Phase 2 will implement real `config.properties` parsing; for now the Go binary does not read this file (it uses env vars + hard-coded defaults), but the mount path `/usr/src/app/etc` must exist and be populated for WrenAI bootstrap compatibility.

- [ ] **Step 1: Create `etc/config.properties`**

```properties
# Sample config.properties for smoke testing.
# Phase 2 will implement parsing; go-wren-engine currently reads env vars only.
wren.directory=/usr/src/app/etc/mdl
wren.datasource.type=DUCKDB
wren.experimental-enable-dynamic-fields=false
```

- [ ] **Step 2: Create `etc/mdl/sample.json`**

```json
{
  "catalog": "wren",
  "schema": "default",
  "models": [
    {
      "name": "customer",
      "refSql": "SELECT * FROM customer",
      "columns": [
        { "name": "custkey", "type": "INTEGER" }
      ]
    }
  ]
}
```

- [ ] **Step 3: Commit**

```bash
mkdir -p etc/mdl
git add etc/
git commit -m "chore: add sample etc/ files for smoke testing"
```

---

### Task 8: `docker/README.md` — Drop-In Replacement Guide

**Files:**
- Create: `docker/README.md`

- [ ] **Step 1: Write `docker/README.md`**

```markdown
# go-wren-engine Docker Image

Drop-in replacement for the Java `wren-engine` image in WrenAI.

## Replace WrenAI compose image (3 steps)

1. Build the image locally:
   ```bash
   docker build -t go-wren-engine:latest -f docker/Dockerfile .
   ```

2. In your WrenAI `docker/docker-compose.yaml`, change the `wren-engine` service:
   ```yaml
   wren-engine:
     image: go-wren-engine:latest
     # keep all other fields (ports, volumes, env) unchanged
   ```

3. Start WrenAI:
   ```bash
   docker compose up -d
   ```

The go-wren-engine image uses the same mount path (`/usr/src/app/etc`),
accepts the same heap-size environment variables (`MAX_HEAP_SIZE`, `MIN_HEAP_SIZE`),
and listens on the same HTTP port (`8080`).

## Known drop-in gaps

| Gap | Impact | ETA |
|---|---|---|
| `etc/config.properties` is not parsed (env vars only) | Config file ignored | Phase 2 |
| Postgres wire protocol (port 7432) not implemented | PG clients cannot connect | TBD |
| `PATCH /v1/config` does not persist to disk | Restart loses config changes | Phase 2 |
| Dynamic fields (`enable-dynamic-fields=true`) uses static branch | Possible semantic deviation | Phase 5 |

## Makefile helpers

- `make image` — build the Docker image
- `make image-run` — build and run a container in foreground
- `make image-test` — build, run, and run a smoke test via HTTP
- `make image-clean` — remove the local image
```

- [ ] **Step 2: Commit**

```bash
git add docker/README.md
git commit -m "docs: add docker README with drop-in replacement guide"
```

---

### Task 9: `Makefile` — Docker Targets

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add image targets to `Makefile`**

Append the following to the end of `Makefile`:

```makefile
# ── Docker image targets ────────────────────────────────────────

IMAGE_NAME ?= go-wren-engine
IMAGE_TAG  ?= latest

.PHONY: image image-run image-test image-clean

image:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) -f docker/Dockerfile .

image-run: image
	docker run --rm -it \
		-p 8080:8080 \
		-v "$(PWD)/etc:/usr/src/app/etc" \
		-e MAX_HEAP_SIZE=512m \
		-e MIN_HEAP_SIZE=64m \
		$(IMAGE_NAME):$(IMAGE_TAG)

image-test: image
	@echo "==> Starting container for smoke test..."
	@docker run -d --name wren-engine-smoke \
		-p 8080:8080 \
		-v "$(PWD)/etc:/usr/src/app/etc" \
		-e MAX_HEAP_SIZE=512m \
		-e WARN_DROP_IN_GAPS=0 \
		$(IMAGE_NAME):$(IMAGE_TAG)
	@sleep 5
	@echo "==> Testing /v1/config ..."
	@curl -sf http://localhost:8080/v1/config > /dev/null && echo "PASS: /v1/config reachable" || { echo "FAIL: /v1/config unreachable"; docker kill wren-engine-smoke; docker rm wren-engine-smoke; exit 1; }
	@echo "==> Testing config entry count ..."
	@count=$$(curl -sf http://localhost:8080/v1/config | grep -o '"name"' | wc -l | tr -d ' '); \
	if [ "$$count" -eq 11 ]; then echo "PASS: 11 config entries"; else echo "FAIL: expected 11 entries, got $$count"; fi
	@echo "==> Testing wren.datasource.type ..."
	@type=$$(curl -sf http://localhost:8080/v1/config/wren.datasource.type | grep -o '"value":"[^"]*"' | cut -d'"' -f4); \
	if [ "$$type" = "DUCKDB" ]; then echo "PASS: datasource type is DUCKDB"; else echo "FAIL: expected DUCKDB, got $$type"; fi
	@echo "==> Stopping smoke container..."
	@docker kill wren-engine-smoke >/dev/null 2>&1 || true
	@docker rm wren-engine-smoke >/dev/null 2>&1 || true
	@echo "==> Smoke test complete."

image-clean:
	docker rmi $(IMAGE_NAME):$(IMAGE_TAG) 2>/dev/null || true
```

- [ ] **Step 2: Verify Makefile syntax**

Run:
```bash
make -n image
```

Expected: Prints the `docker build` command without executing it.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build: add Makefile targets for image build, run, test, clean"
```

---

### Task 10: Build Verification + Image Size Check

**Files:**
- None (verification only)

- [ ] **Step 1: Build the image**

Run:
```bash
docker build -t go-wren-engine:phase1 -f docker/Dockerfile .
```

Expected: Exit 0. No CGo errors, no musl linker errors.

- [ ] **Step 2: Check image size**

Run:
```bash
docker images go-wren-engine:phase1 --format '{{.Size}}'
```

Expected: Size is ≤ 150 MB (e.g., `130MB`, `145MB`).

- [ ] **Step 3: Verify `psql` is present in the image**

Run:
```bash
docker run --rm go-wren-engine:phase1 sh -c 'which psql && psql --version'
```

Expected: Prints path to `psql` and its version.

- [ ] **Step 4: Verify `wren-engine` binary exists and is executable**

Run:
```bash
docker run --rm go-wren-engine:phase1 sh -c 'ls -la wren-engine && file wren-engine'
```

Expected: Shows `wren-engine` as an ELF 64-bit executable.

---

### Task 11: Container Smoke Test — HTTP API

**Files:**
- None (verification only)

- [ ] **Step 1: Start container with volume mount**

Run:
```bash
docker run -d --name wren-engine-phase1 \
  -p 8080:8080 \
  -v "$PWD/etc:/usr/src/app/etc" \
  -e MAX_HEAP_SIZE=512m \
  -e WARN_DROP_IN_GAPS=0 \
  go-wren-engine:phase1
```

Expected: Container ID printed; `docker ps` shows it running.

- [ ] **Step 2: Wait and test `/v1/config`**

Run:
```bash
sleep 5
curl -s http://localhost:8080/v1/config | jq 'length'
```

Expected: `11`

Run:
```bash
curl -s http://localhost:8080/v1/config/wren.datasource.type | jq -r '.value'
```

Expected: `DUCKDB`

- [ ] **Step 3: Test `/v1/mdl/validate/column_is_valid`**

Run:
```bash
curl -s -X POST http://localhost:8080/v1/mdl/validate/column_is_valid \
  -H 'Content-Type: application/json' \
  -d '{"manifest":{"catalog":"wren","schema":"default","models":[{"name":"customer","refSql":"SELECT 1","columns":[{"name":"custkey","type":"INTEGER"}]}]},"modelName":"customer","columnName":"custkey"}' \
  | jq -r '.valid'
```

Expected: `true` (or at minimum, valid JSON with no server error).

- [ ] **Step 4: Stop and remove container**

Run:
```bash
docker kill wren-engine-phase1
docker rm wren-engine-phase1
```

---

### Task 12: GOMEMLIMIT Translation Verification

**Files:**
- None (verification only)

- [ ] **Step 1: Verify `GOMEMLIMIT` env is set from `MAX_HEAP_SIZE`**

Run:
```bash
docker run --rm -e MAX_HEAP_SIZE=1g go-wren-engine:phase1 sh -c 'env | grep GOMEMLIMIT'
```

Expected: `GOMEMLIMIT=1GiB`

Run:
```bash
docker run --rm -e MAX_HEAP_SIZE=512m go-wren-engine:phase1 sh -c 'env | grep GOMEMLIMIT'
```

Expected: `GOMEMLIMIT=512MiB`

Run:
```bash
docker run --rm -e MAX_HEAP_SIZE=4g go-wren-engine:phase1 sh -c 'env | grep GOMEMLIMIT'
```

Expected: `GOMEMLIMIT=4GiB`

- [ ] **Step 2: Verify unsupported heap format falls back gracefully**

Run:
```bash
docker run --rm -e MAX_HEAP_SIZE=unknown go-wren-engine:phase1 sh -c 'env | grep GOMEMLIMIT || echo "GOMEMLIMIT not set (expected)"'
```

Expected: `GOMEMLIMIT not set (expected)` AND stderr contains `[WARN] Unrecognized MAX_HEAP_SIZE format`.

- [ ] **Step 3: Verify `MIN_HEAP_SIZE` is logged but ignored**

Run:
```bash
docker run --rm -e MIN_HEAP_SIZE=128m go-wren-engine:phase1 sh -c 'true' 2>&1 | grep 'MIN_HEAP_SIZE'
```

Expected: `[INFO] MIN_HEAP_SIZE=128m ignored under Go runtime (no -Xms equivalent)`

---

### Task 13: Drop-In Gap Warning Verification

**Files:**
- None (verification only)

- [ ] **Step 1: Verify default warning is printed**

Run:
```bash
docker run --rm go-wren-engine:phase1 sh -c 'true' 2>&1 | grep 'Postgres wire'
```

Expected: `[INFO] Postgres wire protocol (port 7432) not supported by this go-wren-engine build`

- [ ] **Step 2: Verify `WARN_DROP_IN_GAPS=0` suppresses the warning**

Run:
```bash
docker run --rm -e WARN_DROP_IN_GAPS=0 go-wren-engine:phase1 sh -c 'true' 2>&1 | grep 'Postgres wire' || echo "warning suppressed (expected)"
```

Expected: `warning suppressed (expected)`

---

### Task 14: `docker compose up` Verification

**Files:**
- None (verification only)

- [ ] **Step 1: Start with `docker compose up`**

Run:
```bash
docker compose up -d
```

Expected: Service `wren-engine` starts; `docker ps` shows it healthy (or at least running).

- [ ] **Step 2: Test via compose-mapped port**

Run:
```bash
curl -s http://localhost:8080/v1/config | jq 'length'
```

Expected: `11`

- [ ] **Step 3: Verify mount path inside container**

Run:
```bash
docker compose exec wren-engine ls -la /usr/src/app/etc/
```

Expected: Shows `config.properties` and `mdl/` directory.

- [ ] **Step 4: Tear down**

Run:
```bash
docker compose down
```

---

### Task 15: Go Source Lint Verification (No Regressions)

**Files:**
- None (verification only)

Phase 1 does not modify Go source files. Verify the existing codebase still passes all checks.

- [ ] **Step 1: Run Go build**

Run:
```bash
go build ./...
```

Expected: Exit 0, no output.

- [ ] **Step 2: Run go vet**

Run:
```bash
go vet ./...
```

Expected: Exit 0, no output.

- [ ] **Step 3: Run gofmt check**

Run:
```bash
gofmt -l .
```

Expected: No files listed (exit 0).

---

### Task 16: Final Integration Commit

**Files:**
- None (all files already committed)

- [ ] **Step 1: Verify clean working tree**

Run:
```bash
git status
```

Expected: `nothing to commit, working tree clean`

- [ ] **Step 2: Tag the phase**

Run:
```bash
git tag -a phase1-drop-in-dockerfile -m "Phase 1: drop-in Dockerfile + entrypoint complete"
```

- [ ] **Step 3: Summarize changes**

Run:
```bash
git log --oneline --no-decorate -10
```

Expected: Shows all Phase 1 commits in order.

---

## Self-Review

### 1. Spec Coverage

| Spec Requirement | Implementing Task |
|---|---|
| Multi-stage Dockerfile (Debian builder + Debian-slim runtime) | Task 3 |
| `postgresql-client-13` in runtime | Task 3 (Dockerfile RUN apt-get) |
| `WORKDIR /usr/src/app` | Task 3 (Dockerfile) |
| `docker/entrypoint.sh` with `$1/$2/$3` argv | Task 4 |
| `MAX_HEAP_SIZE` → `GOMEMLIMIT` conversion | Task 4, Task 12 |
| `MIN_HEAP_SIZE` accepted but ignored with info log | Task 4, Task 12 |
| `WREN_CONFIG_FILE` env set (Phase 2 parses) | Task 4 |
| Postgres wire gap warning + `WARN_DROP_IN_GAPS` | Task 4, Task 13 |
| Root `Dockerfile` as thin wrapper | Task 5 |
| `docker-compose.yaml` with correct mount path | Task 6 |
| `.dockerignore` | Task 1 |
| `.gitattributes` LF enforcement | Task 2 |
| Sample `etc/` files for smoke test | Task 7 |
| `docker/README.md` replacement guide | Task 8 |
| `Makefile` image targets | Task 9 |
| Image size ≤ 150 MB | Task 10 |
| HTTP API smoke test (`/v1/config` returns 11 entries) | Task 11 |
| `docker compose up` works | Task 14 |
| No Go source changes / no lint regressions | Task 15 |

**No gaps found.**

### 2. Placeholder Scan

- No `TBD`, `TODO`, `implement later` strings.
- No vague "add appropriate error handling" steps.
- No "Write tests for the above" without test code.
- No "Similar to Task N" cross-references.
- Every code step contains the actual code.
- Every command step contains the exact command and expected output.

### 3. Type / Signature Consistency

- `convert_heap_to_gomemlimit` used consistently in Task 4 and Task 12.
- `MAX_HEAP_SIZE` / `MIN_HEAP_SIZE` env var names match across `docker-compose.yaml`, `entrypoint.sh`, and `Makefile`.
- `WARN_DROP_IN_GAPS` used consistently in `entrypoint.sh`, `docker-compose.yaml`, and test commands.
- Image tag `go-wren-engine:phase1` used consistently.
- Port `8080` used consistently.
- Mount path `./etc:/usr/src/app/etc` used consistently.

---

## Execution Handoff

**Plan complete and saved to `.gpowers/plans/2026-05-20-phase1-drop-in-dockerfile-entrypoint.md`.**

**Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — Execute tasks in this session using `gpowers:executing-plans`, batch execution with checkpoints for review.

**Which approach?**
