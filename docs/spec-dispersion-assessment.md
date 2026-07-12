# Specification dispersion assessment (H_spec sanity check, 2026-07-03)

Measured: docs/requirements.md @ ee0f6dd (also: + core_concepts.md + architecture.md)
Protocol: spec -> Go type definitions, x10 @ temperature=1.0;
          Go AST -> cosine similarity -> single-linkage clustering
H_max(N=10) = 3.32 bits

| Input                                    | Instrument                               | meanPairwiseSim | H@0.95 |
| ---------------------------------------- | ---------------------------------------- | --------------- | ------ |
| requirements.md only                     | qwen3-coder:30b (Ollama local)           | 0.282           | 3.32   |
| requirements.md only                     | kimi-k2.7-code (Ollama cloud, think=off) | 0.278*          | 3.32   |
| + core_concepts.md + architecture.md     | qwen3-coder:30b (Ollama local, num_ctx=16k) | **0.402**    | 3.32   |
| + core_concepts.md + architecture.md     | kimi-k2.7-code (Ollama cloud, think=off) | **0.393**       | 3.32   |

*1 syntactically invalid generation discarded and regenerated.

Reading: requirements.md alone scores the lowest pairwise similarity of all
five measured specs — despite the most formal markup (NFR tags, numeric
constraints). NFRs pin quality attributes, not type structure; system scope
dominates markup formality. On instrument A its 0.282 is below even the
deliberately vague toy spec (0.492).

Control experiment: feeding architecture.md + core_concepts.md alongside
requirements raised similarity on both independent instruments — 0.282 -> 0.402
on A and 0.278 -> 0.393 on B (nearly identical gains: +0.120 / +0.115).
The structural anchors already exist in this repo and measurably work — the
actionable conclusion is that codegen agents must receive them together with
requirements, and that schema-level anchors (entities, state machines, API
contracts) are worth more than additional NFRs.

Numbers are comparable only within one instrument+protocol configuration.
Raw data & harness: see `sanity/` in the article workspace.
