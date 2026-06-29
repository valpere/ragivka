---
name: improve
description: "Critique any plan, architecture decision, or implementation approach. Research-first — gives SHIP IT / IMPROVE IT / RETHINK IT / KILL IT verdict. Usage: /improve <topic>"
---

# Skill: /improve
# Technical Design Critic

Research-first — never critique from zero.

```
subject → find best references → extract patterns → critique → verdict + fixes
```

---

## When to run proactively

Suggest `/improve` when:
- A design proposal is being elevated to active work
- A new interface or API contract is being designed
- A non-trivial architectural decision needs validation before implementation

Say: "This is new design — want me to run /improve on it before we build?"

---

## Procedure

### Step 1 — Understand the subject

- What is it? (interface design, algorithm, API shape, data model, config structure)
- What problem does it solve?
- What's the current proposal?

### Step 2 — Research

**Internal first:**
- Read `CLAUDE.md` for architecture constraints and the three non-negotiable invariants
- Read `docs/architecture.md` for the 7-layer breakdown and ER diagram
- Read `docs/requirements.md` for the NFR/FR the change is implementing or touching
- Read the affected source files directly

**External if needed:** look for established patterns in the domain (official docs, RFCs,
well-known library conventions). Reference specific sources — not vague "best practices."

### Step 3 — Structured critique

#### 3A — Architecture alignment
- Does this respect Ragivka's 7 layers: Interfaces → App API → Orchestrator → AI Layer / RAG Layer / Tool Layer → Data Layer?
- Does it follow Dependency Inversion (interfaces defined near consumers)?
- Does it introduce circular dependencies or tight coupling?
- **Tenant isolation (NFR-16):** does every new DB query carry `tenant_id`?
- **Transaction boundaries (NFR-7):** are external API calls outside any open DB transaction?
- **Write Tool idempotency (NFR-4/NFR-15):** do new state-mutating operations have idempotency keys + `AUDIT_LOG` writes?
- Does it belong in the correct phase? (Phase 1: foundation/obs/db; Phase 2: RAG; Phase 3: tooling; Phase 4: adapters)

#### 3B — Flaws and risks
- What can go wrong?
- Worst-case: data corruption, silent failure, resource leak, blocked request, security hole?
- What assumptions are being made that could be wrong?

#### 3C — Best-practice gap
- How does this compare to established patterns in this domain?
- What are production systems doing that this is missing?
- What is being overengineered?

#### 3D — Simplicity (YAGNI / KISS)
- Can this be simpler?
- What is the minimum viable version?
- What can be cut without losing core value?

#### 3E — Testability
- Can this be tested in isolation (without real DB, external API, network)?
- Are the interfaces narrow enough to mock easily?

#### 3F — Security
- Any user input reaching file paths, shell, or external calls unvalidated?
- Any new configuration that could leak secrets?

### Step 4 — Improvement proposals

For each issue found:

```
ISSUE     : [what's wrong or missing]
REFERENCE : [who does it better and how]
FIX       : [specific change to make]
IMPACT    : [what improves]
EFFORT    : Low / Medium / High
```

### Step 5 — Verdict and score

| Dimension | Score (1–10) | Notes |
|-----------|-------------|-------|
| Architecture alignment | | |
| Correctness | | |
| Simplicity | | |
| Best-practice match | | |
| Testability | | |
| Security | | |
| **Overall** | | |

- **SHIP IT** (8+) — good enough, minor tweaks only
- **IMPROVE IT** (5–7) — solid foundation, fix specific issues before building
- **RETHINK IT** (3–4) — core approach has problems, consider alternatives
- **KILL IT** (<3) — doesn't serve the goals, redirect energy elsewhere

### Step 6 — Apply or propose

- **SHIP IT / IMPROVE IT** → apply fixes directly.
- **RETHINK IT** → present 2–3 alternatives with pros/cons and references.
- **KILL IT** → explain clearly why; suggest where energy should go instead.

---

## Rules

- Research before critiquing. Opinions without references are noise.
- Reference specific sources — "RFC 7231 §6.5" not "REST best practices."
- Score honestly. Inflated scores waste time.
- If the subject has project-specific context (CLAUDE.md conventions, known constraints),
  apply it — don't critique things that are intentional project decisions.
