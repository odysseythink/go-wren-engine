# Phase 6 Trigger Assessment

> **Date:** 2026-05-21
> **Assessor:** Agent (writing-plans skill)
> **Design Reference:** `.gpowers/designs/2026-05-20-phase6-postgres-wire-jinjava-design.md`

## Decision

**Phase 6 is NOT triggered. No implementation plan will be written at this time.**

Phase 1-5 = WrenAI 0.9.0 drop-in **complete**. Phase 6 is a conditionally-triggered optional stage. Both 6a and 6b trigger conditions are unmet. Per the design spec §5:

> *"本 spec 完成后**不自动**进入 `gpowers:writing-plans`。是否产出实施计划取决于触发条件是否成立。"*
>
> *"**默认建议：搁置**。"*

## Trigger Condition Check

| Subsystem | Trigger Condition | Current Status | Evidence |
|---|---|---|---|
| **6a Postgres wire (port 7432)** | User connects external SQL client (DBeaver / psql / Tableau) via port 7432 | ❌ **No signal** | Phase 5 just shipped; no end-to-end smoke test run yet; no user issue or BI tool integration request |
| **6b Jinjava full engine** | User MDL uses advanced Jinja (`{% if %}`, `{% for %}`, filters) beyond regex coverage | ❌ **No signal** | Current regex impl covers 100% of WrenAI demo MDLs including ecommerce2 `MoM` macro; no advanced syntax usage found |

## Why Now Is Not the Time

### 6a — Postgres Wire

The design spec's own research (§1) found:

- WrenAI `ai-service` has **0 references** to port 7432
- WrenAI `wren-ui` has **0 references** to port 7432
- `ibis-server` has **0 references** to port 7432
- Java wren-engine 0.9.3 itself **lacks a wire protocol implementation** — only a config class exists

Port 7432 is an **unconsumed dangling port** in the default WrenAI 0.9.0 deployment. Building it now would be speculative engineering with no consumer.

### 6b — Jinjava Full Engine

The design spec's research (§1) found:

- Only **1 macro** across all WrenAI demo MDLs: ecommerce2's `MoM` (month-over-month)
- That macro's complexity: `(calc, ts) => (calc - LAG(calc) OVER ...) / COALESCE(LAG(calc), 1)`
- **Current Go regex replacement already handles it** (positional + named params, multiple references)
- Zero usage of `{% if %}`, `{% for %}`, filters, or includes in any demo MDL

The regex implementation is sufficient for all real-world usage today.

## Action Paths When Triggered

### If 6a Trigger Signal Appears

**Signals to watch for:**
- `grep '7432'` in wren-ai-service logs shows hits
- GitHub issue: "Can I connect Tableau/DBeaver to wren-engine?"
- User reports: "Port 7432 refused connection"

**Action:** Execute the design spec §5.1 slices 1-4:
1. Wire frame codec + handshake (`internal/wireprotocol/`, `pgproto3` dep)
2. SimpleQuery + DuckDB routing (`DirectQuery` → PG `DataRow`)
3. Error response + smoke (`psql` end-to-end)
4. Integration (`main.go` goroutine, config, docs)

### If 6b Trigger Signal Appears

**Signals to watch for:**
- User MDL fails with regex replacement error containing `{% if %}`, `{% for %}`, or `|` filter syntax
- GitHub issue: "Macro with Jinja if/else not working"
- `--difftest` output shows regex vs Java Jinjava divergence on non-macro syntax

**Action:** Execute the design spec §5.2 slices 1-3:
1. `gonja/v2` integration + regex fallback split (`jinja.go` + `jinja_regex_fallback.go`)
2. Test coverage for advanced syntax (if/for/filter/nested macros)
3. Dual-mode parity verification + docs

## Registry: What Remains Open After Phase 5

| Gap | Phase 5 Status | Phase 6 Would Close (if triggered) |
|---|---|---|
| `WrenSqlRewrite` dynamic-field branch | ✅ Closed | — |
| WrenAI compose drop-in replacement | ✅ Closed | — |
| Postgres wire protocol (7432) | ❌ Unimplemented, **non-blocking** | ✅ (6a triggered) |
| Jinja advanced syntax | ⚠ Regex covers all real usage | ✅ (6b triggered) |
| 0.9.3 → 0.11.1 upgrade | ❌ Not audited | ❌ Out of scope (Phase 8) |
| Performance / memory tuning | ❌ Not done | ❌ Out of scope (Phase 7) |

## Related Documents

- Design spec: `.gpowers/designs/2026-05-20-phase6-postgres-wire-jinjava-design.md`
- Drop-in completion report: `docs/phase5-dynamic-scoreboard.md`
