---
name: lookup-docs
description: "Cascade documentation lookup: lorehouse → context7 → chub → web, with cache-back to lorehouse. Use when the user asks for library/API/framework documentation, setup, configuration steps, or 'how do I use X'. Usage: /lookup-docs <query>"
---

# Skill: /lookup-docs

Cascading documentation retrieval that always tries the local agent KB first
and falls back to external sources only on miss. External hits are cached
back into the KB so future lookups stay local.

Version-detection (Step 0), the source-hierarchy/citation discipline (CITE
PROTOCOL), and the UNVERIFIED fallback are adapted from
[addyosmani/agent-skills](https://github.com/addyosmani/agent-skills)'s
`source-driven-development` (MIT) — folded into this skill's own cascade
structure rather than installed as a separate skill, since they only make
sense layered onto an existing lookup flow.

---

## WHEN TO INVOKE

Invoke this skill (explicitly or via description match) when the user asks for:

- Library, framework, or SDK documentation (React, Next.js, Prisma, Django, etc.)
- API references (Stripe, OpenAI, Supabase, Anthropic, etc.)
- CLI tool usage (gh, kubectl, gcloud, etc.)
- Setup, configuration, or migration steps for any third-party tool
- Internal project knowledge that may already be captured in lorehouse

**Skip** for: refactoring, debugging business logic, writing scripts from
scratch, or general programming concepts.

---

## STEP 0 — DETECT STACK & VERSION

Before querying context7/chub (steps 2–3 below), check the project's
dependency file for the exact version in play — an unversioned query
("React hooks") can return docs for the wrong major version and produce
plausible-looking but wrong code.

```
package.json                       → Node/React/Vue/Angular/Svelte
composer.json                      → PHP/Symfony/Laravel
requirements.txt / pyproject.toml  → Python/Django/Flask
go.mod                             → Go
Cargo.toml                         → Rust
Gemfile                            → Ruby/Rails
```

State what was found, then fold the version into every downstream query:

```
STACK DETECTED: React 19.1.0 (package.json), Next.js 15.2.0
→ querying context7 for "React 19 useActionState", not just "useActionState"
```

If the version is missing or ambiguous and it matters for the answer (the
API changed across versions), **ask the user** rather than guessing — skip
this when the query is version-agnostic (e.g. a stable CLI flag) or when
lorehouse's Step 1 already yields a confident hit, since a local KB entry
is presumed already scoped to this project.

---

## CASCADE PROTOCOL

Run these steps **in order**. Stop at the first step that yields a usable answer.

### Step 1 — Local KB (lorehouse)

```bash
lorehouse search "<user query>" --top 3 --json
```

- Inspect the `similarity` field of each result.
- If the **top hit** has `similarity >= 0.55` AND the body answers the question:
  use it. **Stop.**
- For follow-up details, fetch the full entry: `lorehouse view <slug> --json`.

If results are below threshold or off-topic, treat as **MISS** and proceed.

### Step 2 — context7-mcp

```
mcp__context7__resolve-library-id  query="<library name + question>"
mcp__context7__query-docs           libraryId="<id>" query="<full question>"
```

Per `~/.claude/rules/context7.md`:

- Always start with `resolve-library-id` (unless the user provided `/org/project`).
- Pick the best match by name match, description relevance, source reputation.
- If results look wrong, try alternate names ("next.js" not "nextjs").

If context7 returns useful content, **go to caching step**, then stop.

If context7 has no match or returns irrelevant content, proceed.

### Step 3 — chub (Context Hub)

```bash
chub search "<query>" --json --limit 5
chub get <id> --lang <ts|py|js|rb|cs> --json
```

- `chub search` returns LLM-optimized doc IDs.
- `chub get` fetches the full content. Pick `--lang` based on the user's stack.

If chub returns useful content, **go to caching step**, then stop.

### Step 4 — Web search

Use `WebSearch` (or the default web search tool) only if all above missed.
This is the slowest and least curated option. Prefer authoritative sources
per the hierarchy in CITE PROTOCOL below over the first search hit.

If a web result is used, **go to caching step**.

If **all four steps miss** — nothing usable found: say so explicitly rather
than falling back to unstated training-data knowledge. State
`UNVERIFIED: no documentation found for <query>; answer below is from
training data and may be outdated` before answering, so the user knows to
double-check it. Don't silently answer from memory as if it were sourced.

---

## CACHE-BACK PROTOCOL

When **any** external source (steps 2–4) produced a usable answer, store it
in lorehouse so the next query hits locally.

```bash
cat > /tmp/cached-<slug>.md <<'EOF'
---
title: <short descriptive title>
slug: <project-or-topic>-<kebab-case-slug>
tags: [source:<context7|chub|web>, cached-at:YYYY-MM-DD, <topic-tag-1>, <topic-tag-2>]
---
FACT: <core fact distilled from the source>
FACT: …

PATTERN — <short name>:
  <code, command, or step sequence>

NEVER: <prohibition, if applicable>
WHY: <reason behind the rule, when non-obvious>

NOTE: source URL or library/version: <url or id>
EOF
lorehouse add /tmp/cached-<slug>.md
```

**Mandatory tags:**

- `source:<context7|chub|web>` — provenance
- `cached-at:YYYY-MM-DD` — for staleness sweeps

**For long reference docs** (full API specs, multi-section guides), prefer:

```bash
lorehouse ingest <file.md> --tag-prefix <project> --slug-prefix <project>-
```

It auto-chunks reference-shaped docs and distills narrative ones via Ollama.

---

## CITE PROTOCOL

Caching into lorehouse (above) is for the *next lookup*. This is for the
*current* one: when a fact found via steps 2–4 lands in code you write or
a claim you make to the user, say where it came from — cache-back alone
doesn't make the citation visible at the point of use.

**Source hierarchy** (prefer higher priority when sources conflict):

| Priority | Source | Example |
|----------|--------|---------|
| 1 | Official documentation | react.dev, docs.djangoproject.com, pkg.go.dev |
| 2 | Official blog / changelog | react.dev/blog, go.dev/doc/devel/release |
| 3 | Web standards references | MDN, web.dev, RFC/spec documents |
| 4 | Runtime/platform compatibility | caniuse.com |

**Not authoritative — never cite as the primary source:** Stack Overflow,
third-party blog posts/tutorials, AI-generated summaries, training data
(that's precisely what this skill exists to verify).

**In code**, a one-line comment with the deep link (not just the domain):

```go
// Go 1.22+ range-over-func iterators.
// Source: https://go.dev/ref/spec#Range_clause
for v := range seq { ... }
```

**In conversation**, name the source and quote the load-bearing line when
the decision is non-obvious:

```
Using `context.AfterFunc` instead of a manual goroutine+channel here —
Source: https://pkg.go.dev/context#AfterFunc
"AfterFunc registers a function to be called ... after ctx is done."
Simpler than the manual pattern and doesn't leak a goroutine if ctx never fires.
```

Skip citation for stable, version-independent facts nobody would dispute
(e.g. `strings.TrimSpace` trims whitespace) — cite what could plausibly be
wrong or version-dependent, not everything.

---

## QUICK REFERENCE

| Step | Command (head)                                   | Threshold for hit  |
|------|--------------------------------------------------|--------------------|
| 0    | read dependency file, state version              | before 2–3, if version-sensitive |
| 1    | `lorehouse search "<q>" --top 3 --json`          | similarity ≥ 0.55  |
| 2    | `mcp__context7__resolve-library-id` + query-docs | non-empty match    |
| 3    | `chub search "<q>" --json` + `chub get <id>`     | non-empty match    |
| 4    | `WebSearch "<q>"`                                | last resort — else UNVERIFIED |
| ★    | `lorehouse add /tmp/cached.md`                   | after any 2/3/4 hit — cache-back |
| ★    | inline `// Source: <url>#<anchor>`               | when the fact lands in code/claim |

---

## SERVICE INFO

- lorehouse runs at `http://127.0.0.1:7777` as a `systemd --user` service.
- REST docs: `~/wrk/common/lorehouse/API.md`.
- CLI works without the service via filesystem fallback (slightly slower).
- If the service is down: `systemctl --user start lorehouse.service`.

---

## EDGE CASES

**Library version drift.** If the user mentions a specific version, include
it in the query for steps 2–3 and in the cached entry's tags
(e.g. `react-19`, `nextjs-15`).

**Multi-language libraries.** When using `chub get`, pick `--lang` matching
the user's stack. Default to `ts` for web projects, `py` for ML/data.

**Stale cached entries.** If a cached entry's `cached-at` tag is older than
~6 months, treat with skepticism for fast-moving libraries; consider
re-fetching from source. (Future: auto-refresh job.)

**Large reference docs from web.** Don't paste raw HTML/Markdown into
`lorehouse add` — distill to FACT/PATTERN first or use `lorehouse ingest`
with `--mode distill`.
