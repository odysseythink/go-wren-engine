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
# Run each capture tool individually (recommended for review)
go run ./cmd/capture-golden -addr http://localhost:18080
go run ./cmd/capture-duckdb-golden -addr http://localhost:18080
go run ./cmd/capture-envelope-golden -addr http://localhost:18080 --groups exec_smoke,viewenum
go run ./cmd/capture-analysis-golden -addr http://localhost:18080
```

Or use the Makefile target:
```bash
make capture-all-golden
```

**Note:** `capture-all-golden` exits with code 1 if any case produces an oracle
error (e.g. tpch/1, tpch/4 with `count(*)`). This is expected — the Makefile
target runs tools with `|| true` to continue on non-zero exit.

### Step 3: Rebaseline and review

```bash
make rebaseline
```

This backs up existing baselines to `.bak` and regenerates all baseline JSON
files. **Always review the diffs before committing:**

```bash
for f in baseline baseline-duckdb baseline-envelope baseline-analysis; do
  echo "=== $f ==="
  diff -u testdata/difftest/${f}.json.bak testdata/difftest/${f}.json | head -20
done
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
| Diffs show large unexpected changes | Java output unstable | Re-run capture and compare |
| `.error.permanent` overwritten by capture | Known — capture removes sentinel on re-run | Manually restore from git or recreate with sentinel comment |

## Restoring `.error.permanent` Sentinels

After `make capture-all-golden`, known permanent oracle errors may have their
`.error.permanent` files replaced with plain `.error`. To restore:

```bash
# For each known permanent error:
cat > testdata/difftest/golden/tpch/1.sql.error.permanent << 'EOF'
# Permanent oracle error — Java 0.9.3: IllegalArgumentException: count(*) should have a followed source
EOF
cat testdata/difftest/golden/tpch/1.sql.error >> testdata/difftest/golden/tpch/1.sql.error.permanent
rm testdata/difftest/golden/tpch/1.sql.error
```

Repeat for any other known permanent errors.
