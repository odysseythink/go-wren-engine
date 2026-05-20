# Phase 3 Scoreboard: Go vs Java wren-engine 0.9.3

Generated after full capture against `ghcr.io/canner/wren-engine:0.9.3`.

## Rewrite Baseline (`modelingOnly=group-config`)

| Status | Count | Notes |
|--------|-------|-------|
| pass | 36 | |
| fail | 7 | Token-level diffs against Java golden |
| go-error | 12 | Go rewrite engine panics or returns error |
| oracle-error-permanent | 2 | tpch/1, tpch/4 — Java 0.9.3 `count(*) should have a followed source` |
| **Drop-in pass rate** | **36/55 = 65%** | Excludes 2 oracle-error-permanent |

### go-error Inventory (Phase 4 backlog)

| Case | Error |
|------|-------|
| tpch/m_orders | panic: interface conversion: interface is nil, not ast.Relation |
| tpch/met_customer_revenue | panic: interface conversion: interface is nil, not ast.Relation |
| tpch/met_daily | panic: interface conversion: interface is nil, not ast.Relation |
| tpch/met_revenue | panic: interface conversion: interface is nil, not ast.Relation |
| tpch/met_rollup | panic: interface conversion: interface is nil, not ast.Relation |
| tpch/met_weekly | panic: interface conversion: interface is nil, not ast.Relation |
| tpch/v_use_metric | panic: interface conversion: interface is nil, not ast.Relation |
| tpch/v_use_model | panic: interface conversion: interface is nil, not ast.Relation |
| tpch/v_use_nested | panic: interface conversion: interface is nil, not ast.Relation |
| tpch/v_use_rollup | panic: interface conversion: interface is nil, not ast.Relation |
| analysis/select_where | panic: interface conversion: interface is nil, not ast.Relation |
| analysis/select_with_alias | panic: interface conversion: interface is nil, not ast.Relation |

## DuckDB-Dialect Baseline (`modelingOnly=false`)

| Status | Count | Notes |
|--------|-------|-------|
| pass | 35 | |
| fail | 7 | Token-level diffs against Java golden |
| go-error | 13 | Go rewrite + DuckDB converter errors |
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
| rewrite | 57 | 36 | 7 | 12 | 0 | 2 | 65% |
| duckdb | 57 | 35 | 7 | 13 | 0 | 2 | 64% |
| envelope | 8 | 1 | 0 | 2 | 5 | 0 | 13% |
| analysis | 57 | 20 | 15 | 0 | 21 | 0 | 56%* |

*Analysis pass rate excludes oracle-error cases (21) since the Java endpoint itself fails.

## Phase 4 Work Items

1. **Fix 12 go-error cases in rewrite** — all share the same `panic: interface conversion: interface is nil, not ast.Relation`
2. **Fix 2 go-error cases in envelope preview** — exec_smoke values_basic/values_nulls panic in PreviewService
3. **Investigate 7 fail cases in rewrite** — token-level diffs may be benign formatting or semantic gaps
4. **Investigate 7 fail cases in duckdb** — same 7 cases as rewrite, plus DuckDB converter differences
