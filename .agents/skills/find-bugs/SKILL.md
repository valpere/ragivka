---
name: find-bugs
description: "Find bugs, security vulnerabilities, and code quality issues in branch changes. Report-only — no code changes. Usage: /find-bugs [topic]"
---

# Skill: /find-bugs
# Code Review — Bug & Vulnerability Hunt

Report only. Do not make changes.

---

## Phase 1 — Input Gathering

1. Get the full diff: `git diff $(git merge-base main HEAD)...HEAD`
2. If truncated, read each changed file individually until every changed line is seen.
3. List all modified files before proceeding. Do not skip any.

---

## Phase 2 — Attack Surface Mapping

For each changed file, identify and list:

- All **user-controlled inputs** (URL params, request bodies, query strings, form fields)
- All **external calls** — are errors checked? are timeouts set? are responses closed/consumed?
- All **state mutations** — shared state modified without locks? mutation visible to other goroutines/threads?
- All **file/path operations** — paths constructed from user data?
- All **resource allocations** — unbounded loops or allocations on user-supplied sizes?
- All **silent failures** — errors swallowed, empty fallbacks, missing nil/null checks?

---

## Phase 3 — Security Checklist

Check every item against every changed file:

- [ ] **Injection** — user input reaching SQL queries, shell commands, file paths, template strings?
- [ ] **Hardcoded secrets** — API keys, passwords, tokens in changed code?
- [ ] **Auth bypass** — can authentication or authorization checks be skipped?
- [ ] **Information disclosure** — error messages returning stack traces, internal paths, or sensitive data?
- [ ] **Path traversal** — file paths constructed from user-supplied strings without sanitization?
- [ ] **Request body limits** — is unbounded user-supplied data size possible?
- [ ] **Missing null/nil checks** — pointer dereference or null access on values that could be absent?
- [ ] **Race conditions** — shared mutable state accessed from concurrent paths?
- [ ] **Dependency confusion** — any new unreviewed packages introduced?

> **Stack-specific items:** run `/generate-find-bugs` to add language/framework checklists
> (Go: goroutine leaks, context propagation; JS: XSS, prototype pollution; etc.)

---

## Phase 4 — Verification

For each potential issue:
- Check if it is already guarded elsewhere in the changed code.
- Read at least 10 lines of surrounding context to confirm the issue is real.
- Only report issues you can substantiate with evidence from the diff.

---

## Phase 5 — Pre-Conclusion Audit

Before finalizing:
1. List every file reviewed — confirm each was read completely.
2. List every checklist item: issue found, or confirmed clean.
3. List anything you could NOT fully verify and why.

---

## Output Format

**Priority:** security vulnerabilities > correctness bugs > performance > code quality

**Skip:** style, formatting, naming preferences

For each issue:

```
**File:Line** — brief description
Severity  : Critical / High / Medium / Low
Problem   : what's wrong
Evidence  : why this is real (no existing guard, language semantics confirm it)
Fix       : concrete suggestion
```

If nothing significant: say so — don't invent issues.

---

## STACK-SPECIFIC CHECKLISTS

### Go Checklist

- [ ] Goroutine leaks — all goroutines bounded by context or timeout?
- [ ] Nil interface vs nil pointer — returning interface wrapping nil concrete?
- [ ] Context propagation — request contexts passed to all blocking operations?
- [ ] Mutex scope — mutex unlocked in the same scope it was locked?
- [ ] Response body close — deferred after nil check?
- [ ] Request size limits — applied before decoding JSON from untrusted input?
- [ ] SSE headers — WriteHeader called exactly once, before any body writes?

### Ragivka-Specific Attack Surface

- [ ] **Tenant isolation (NFR-16)** — every DB query uses `tenant.MustGetTenantID(ctx)` or `tenant.GetTenantID(ctx)`? Cross-tenant query is a critical data breach.
- [ ] **Transaction boundary (NFR-7)** — no `pgxpool` transaction held open during `http.Client` calls, LLM API calls, or any I/O? Violation causes connection pool exhaustion.
- [ ] **Write Tool idempotency (NFR-4)** — any new function that mutates external state has an idempotency key checked before execution and an `AUDIT_LOG` record written after?
- [ ] **DSN injection** — `pkg/db.NewPool` uses `url.URL` builder, not string concatenation. New DB connection code must follow the same pattern.
- [ ] **OTEL insecure flag** — `OTEL_EXPORTER_OTLP_INSECURE=true` check present before enabling insecure transport? Never default to insecure in production paths.
