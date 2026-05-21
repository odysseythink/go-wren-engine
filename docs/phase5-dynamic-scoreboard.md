# Phase 5 Dynamic-Field Scoreboard

## Baseline Comparison

| Mode | Pass | Fail | go-error | oracle-error-permanent |
|---|---|---|---|---|
| Static (`baseline.json`) | 36 | 17 | 2 | 2 |
| Dynamic (`baseline-dynamic.json`) | TBD | TBD | **0** | TBD |

> **Note:** Dynamic mode fail count may be higher than static because Java itself
> marks dynamic fields as "experimental and buggy". The only hard requirement is
> **0 go-error** (no panics).
>
> Dynamic golden files (`golden-dynamic/`) are pending Java oracle capture with
> `--dynamic-fields=true`. Run `make capture-dynamic-golden` when the oracle is
> available.

## Drop-in Verification

| Check | Status |
|---|---|
| `make image-test` | ⬜ |
| `tools/wrenai-dropin-test.sh` exit 0 | ⬜ |
| dry-plan returns SQL | ⬜ |
| ai-service → wren-engine HTTP 200 | ⬜ |

## Remaining Gaps (Post-Phase 5)

| Gap | Phase |
|---|---|
| Postgres wire protocol (port 7432) | Phase 6 |
| Jinjava full parity (regex macro replacement) | Phase 6 |
| 0.9.3 → 0.11.1 upgrade | Phase 8 |
| Performance optimization | Phase 7 |

## Commits

1. `feat(p5): port WrenDataLineage to internal/rewrite/lineage/`
2. `feat(p5): pruned RelationInfo + renderer variants for dynamic field path`
3. `feat(p5): WrenSqlRewrite dynamic-field branch with DummyInfo + DateSpineInfo`
4. `test(p5): dynamic-mode difftest infrastructure`
5. `test(p5): initial baseline-dynamic.json (no-golden pending oracle capture)`
6. `docs(p5): drop-in completion report`
