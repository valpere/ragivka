---
name: generate-onboard
description: "Generates a project-specific /onboard skill with actual architecture, commands, and gotchas baked in. Run once per project. Usage: /generate-onboard"
---

# Skill: /generate-onboard
# Generate Project-Specific /onboard Skill

Runs the full onboard discovery protocol, then writes a project-specific
`/onboard` skill to `.claude/skills/onboard/SKILL.md` — so future `/onboard`
invocations produce the brief instantly from cached knowledge.

---

## PROCESS

### Step 1 — Run the full audit

Follow the **Full audit** discovery protocol from the generic `/onboard` skill.
Collect all findings. Don't write anything yet.

### Step 2 — Identify gotchas

Gotchas come from:
- CLAUDE.md "intentional deviation" or warning comments
- TODO/FIXME/HACK in source
- Unusual directory structure or naming
- Non-standard test commands or setup steps
- Config traps (required env vars with no default, order-sensitive init)
- Anything that surprised you during discovery

### Step 3 — Write the skill

Write to `.claude/skills/onboard/SKILL.md`. The generated skill is a **document**, not a discovery protocol — all commands have been run, all findings are baked in.

Template:

```markdown
---
name: onboard
description: "[Project name] onboarding — architecture, commands, gotchas. Usage: /onboard"
---

# Skill: /onboard
# [Project Name] — Onboarding Brief

## ABOUT

[2–3 sentences from README]

## QUICK START

```bash
# Install
[command]

# Start
[command]

# Test
[command]
```

## ARCHITECTURE

[actual architecture from discovery — entry points, routing, data layer, key packages with one-line descriptions]

## DATA LAYER

[schema location, migration count, how to run migrations]

## TESTS

Run all  : [command]
Run one  : [command]
Count    : [N test files]

## CI/CD

[workflow names and what they gate]

## EXTERNAL SERVICES

| Service | Purpose | Env var |
|---------|---------|---------|
| [name] | [what for] | [VAR_NAME] |

## GOTCHAS

- [non-obvious thing 1]
- [non-obvious thing 2]
(write "None found during initial audit — add as discovered" if empty)

## CONTRIBUTORS

[git shortlog -sn output, top 5]
```
```

### Step 4 — Confirm

```
Wrote /onboard skill for [project name].
Stack: [runtime + framework]
Tests: [N files, command: ...]
Gotchas: [N found]
```

---

## RULES

- Write to `.claude/skills/onboard/SKILL.md` — overwrite generic if present.
- The generated skill is a document, not a protocol. Future `/onboard` reads it directly — no commands needed.
- Keep it concise. A map, not a wiki.
- If gotchas are empty, say so explicitly — don't omit the section.
- After writing, confirm with the summary line above.
