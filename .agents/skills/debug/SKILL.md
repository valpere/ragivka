---
name: debug
description: "Systematic bug diagnosis — reproduce, isolate, hypothesize, verify, fix. Usage: /debug [what's broken]"
---

# Skill: /debug
# Systematic Bug Diagnosis

---

## PROTOCOL

Bugs have two parts: the **symptom** (what you observe) and the **cause** (what's actually wrong). The protocol moves from symptom → cause → fix, one verified step at a time.

### Phase 1 — Reproduce

**Goal:** Reliable, minimal reproduction before touching any code.

1. State the symptom precisely: what input, what output, what was expected.
2. Find the smallest input that triggers it.
3. Verify you can reproduce it consistently.
4. Is this a regression? `git log --oneline -20` — when did it last work?

**If you can't reproduce it:** Stop. Gather more information. Unreproducible bugs are unsolvable.

### Phase 2 — Isolate

**Goal:** Narrow the search space to the smallest possible area.

1. Identify the layers involved: UI → API → service → DB → external?
2. Test each boundary — where does correct input produce wrong output?
3. Binary-search the call stack: disable half, does the bug disappear?
4. Is it environment-specific? dev vs prod? one machine vs all?

**Isolation heuristics:**
- Works in tests, breaks live → check environment (config, secrets, timing)
- Started after a deploy → `git bisect`
- Intermittent → look for shared mutable state, races, time-dependent logic

### Phase 3 — Hypothesize

State one falsifiable hypothesis before changing any code:

```
Hypothesis  : [what I think is wrong]
Evidence for: [what supports this]
Against     : [what doesn't fit]
Test        : [one action that confirms or refutes]
```

**One hypothesis at a time.** Testing multiple simultaneously makes it impossible to know what fixed it.

### Phase 4 — Verify

1. Run the test that confirms or refutes.
2. **Confirmed** → move to fix.
3. **Refuted** → form a new hypothesis. Don't modify the failing thing yet.
4. Keep a short log: what you tried, what you learned.

### Phase 5 — Fix

1. Make the minimal change that fixes the root cause — not the symptom.
2. Run the full test suite.
3. Add a regression test that would have caught this bug.
4. Commit message explains *why*, not just *what*:
   ```
   fix: [what was broken]

   Root cause: [why it happened]
   Fix: [what changed and why this is correct]
   ```

---

## COMMON BUG PATTERNS

| Pattern | Symptom | Where to look |
|---------|---------|---------------|
| **Off-by-one** | Wrong last/first element | Loop bounds, slice indices, pagination |
| **Nil/null dereference** | Crash on access | Unguarded pointer use, missing null checks |
| **Race condition** | Intermittent, timing-dependent | Shared state, goroutines/threads, async code |
| **Wrong input assumptions** | Works in tests, breaks in prod | Input validation, edge cases, empty/max values |
| **Config/env mismatch** | Works locally, breaks in CI/prod | `.env` files, env var names, defaults |
| **Stale state** | Shows old data | Caches, memoization, DB transaction isolation |
| **Type coercion** | Wrong math, unexpected falsy | JS `==`, int/float truncation, string/int mixing |
| **Dependency version** | Broke after upgrade | Changelog, breaking changes, peer dependencies |

---

## REGRESSION TEST FORMAT

```
// Regression: [short description of what was broken]
// Arrange: exact conditions that triggered the bug
// Act:     the action that was broken
// Assert:  the correct outcome
```

The test must reproduce the **exact** failing scenario, not a simplified analogue.

---

## RULES

- **Reproduce before touching code.** A fix without reproduction is a guess.
- **One change at a time.** Multiple simultaneous changes make causation unknowable.
- **Fix the root cause, not the symptom.** Wrapping a bug in an `if` is not a fix.
- **Always add a regression test.** If it broke once, it can break again.
- **`git bisect` for regressions.** Faster than reading the diff.
- Tag unverified claims `[hypothesis]` when communicating status.

---

## PROJECT QUICK REFERENCE

### Test commands

```bash
Run all  : go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...
Run one  : go test -v -run TestCalculateCost ./pkg/obs/...
Coverage : go test -coverprofile=coverage.txt -covermode=atomic ./... && go tool cover -func=coverage.txt
```

### Debug logging

No `LOG_LEVEL` / `DEBUG` env vars in the current codebase. Debug via:
- `OTEL_EXPORTER_OTLP_INSECURE=true` — allow insecure OTEL export to local collector
- `OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318` — send traces to local Jaeger/Tempo
- Prometheus metrics at `:8080/metrics` (server) and `:8081/metrics` (worker)
- `go test -v -run TestName ./...` — verbose test output

### Known fragile areas

Most-changed files (historical churn):
- `cmd/worker/main.go` — River worker stub, will grow significantly in Phase 1 step 3
- `cmd/server/main.go` — HTTP routing, will grow with Phase 4 adapters
- `pkg/obs/cost.go` — pricing registry; `defaultPricingRegistry` is a package-level var (test-swappable but not concurrent-write-safe)
- `pkg/db/db.go` — connection pool config; `MaxConns`/`MinConns` are caller-set, easy to misconfigure

No TODO/FIXME in current code.

### Stack-specific debug notes

- **Race detector**: always run `go test -race` — the codebase uses `promauto` global vars which are safe, but new code must be verified.
- **Tenant context panics**: `tenant.MustGetTenantID(ctx)` panics if called without prior `tenant.WithTenantID` — check context propagation in call chain.
- **pgxpool exhaustion**: if tests hang, check that test DB connections are released (use `defer pool.Close()`).
- **OTEL no-op mode**: if `OTEL_EXPORTER_OTLP_ENDPOINT` is empty, `obs.InitTracer` sets up a no-op tracer — spans are created but discarded. Safe for local dev.
