# Phase 3 Scoreboard: Go vs Java wren-engine 0.9.3

Generated after full capture against `ghcr.io/canner/wren-engine:0.9.3`.
Updated after Phase 4 — rewrite go-error cleanup.

## Rewrite Baseline (`modelingOnly=group-config`)

| Status | Count | Notes |
|--------|-------|-------|
| pass | 36 | |
| fail | 17 | Token-level diffs against Java golden |
| go-error | 2 | `exec_smoke/values_basic`, `exec_smoke/values_nulls` — out of P4 scope |
| oracle-error-permanent | 2 | tpch/1, tpch/4 — Java 0.9.3 `count(*) should have a followed source` |
| **Drop-in pass rate** | **36/55 = 65%** | Excludes 2 oracle-error-permanent |

### go-error Inventory (post-Phase 4)

| Case | Error | Status |
|------|-------|--------|
| exec_smoke/values_basic | panic: interface conversion: interface is nil, not ast.Relation | Out of P4 scope |
| exec_smoke/values_nulls | panic: interface conversion: interface is nil, not ast.Relation | Out of P4 scope |

**Resolved in Phase 4 (10 cases):** `tpch/m_orders`, `tpch/met_customer_revenue`, `tpch/met_daily`, `tpch/met_revenue`, `tpch/met_rollup`, `tpch/met_weekly`, `tpch/v_use_metric`, `tpch/v_use_model`, `tpch/v_use_nested`, `tpch/v_use_rollup` — all moved from `go-error` to `fail` (valid SQL, token diffs vs Java golden).

## DuckDB-Dialect Baseline (`modelingOnly=false`)

| Status | Count | Notes |
|--------|-------|-------|
| pass | 35 | |
| fail | 16 | Token-level diffs against Java golden |
| go-error | 4 | `metric/cumulative` (GENERATE_TIMESTAMP_ARRAY not ported), `tpch/met_weekly` (same), plus 2 exec_smoke values cases |
| oracle-error-permanent | 2 | tpch/1, tpch/4 — same Java bug |
| **Drop-in pass rate** | **35/55 = 64%** | Excludes 2 oracle-error-permanent |

## Envelope Baseline (`/v1/mdl/preview`)

| Status | Count | Notes |
|--------|-------|-------|
| pass | 1 | exec_smoke/const |
| go-error | 2 | exec_smoke/values_basic, values_nulls — Go preview panic |
| oracle-error | 5 | viewenum/* — Java preview errors (DuckDB CTE circular reference) |
| **Drop-in pass rate** | **1/8 = 13%** | Very low due to preview pipeline instability |

## Analysis Baseline (`/v2/analysis/sql`)

| Status | Count | Notes |
|--------|-------|-------|
| pass | 20 | |
| fail | 15 | Analysis output structural diffs |
| oracle-error | 21 | Java analysis endpoint errors (mostly TPC-H cases) |
| **Drop-in pass rate** | **20/36 = 56%** | Analysis endpoint coverage is partial |

## Config & Validation Baselines

| Baseline | pass | Notes |
|----------|------|-------|
| config | 12/12 | All config handler tests pass |
| validation | 4/4 | All validation tests pass |

## Summary

| Baseline | Total | pass | fail | go-error | oracle-error | oracle-error-permanent | Pass Rate |
|----------|-------|------|------|----------|--------------|------------------------|-----------|
| rewrite | 57 | 36 | 17 | 2 | 0 | 2 | 65% |
| duckdb | 57 | 35 | 16 | 4 | 0 | 2 | 64% |
| envelope | 8 | 1 | 0 | 2 | 5 | 0 | 13% |
| analysis | 57 | 20 | 15 | 0 | 21 | 0 | 56%* |

*Analysis pass rate excludes oracle-error cases (21) since the Java endpoint itself fails.

## Phase 4 Results

- **RC-A fixed:** `RenderJinja` now correctly expands Jinjava template variables (`{{ arg1 }}`) and nested macro calls via multi-pass substitution.
- **RC-B fixed:** 12 visitor methods in `ast_builder.go` now have defensive nil-checks, preventing panics on ANTLR error-recovery trees.
- **10 tpch cases** moved from `go-error` (panic) to `fail` (valid SQL with token-level diffs against Java golden).
- **0 regressions** across all baselines.
