# Useful Findings & Market Demand Insights

This document consolidates key findings from investigating 10 freelance project briefs and the architectural reviews provided by external AI agents. It serves as the rationale behind Ragivka's design choices.

## 1. Market Demand Patterns

Analysis of the investigation corpus reveals these recurring demands across projects:

*   **Structured Output is King:** Clients rarely want open-ended conversational bots; they want bots that perform classification, generate statistics, extract facts, and produce formatted reports (refs: 1637443 registry analysis, 1637660 numerology reports).
*   **Private Data RAG:** Almost all projects require RAG over private data — product catalogs, engineering PDFs, marketing sources, legal registries (refs: 1634718, 1636378, 1636852, 1637443).
*   **Tool Integration & Function Calling:** Seamlessly interacting with CRMs (KeyCRM), e-commerce platforms (PrestaShop), payment systems (Lemon Squeezy, WayForPay), and external scrapers. The LLM must call real APIs to get live data before responding — not hallucinate product prices or stock levels (refs: 1636852, 1636992, 1634149).
*   **Offline / Local Capabilities:** Strong demand for solutions that can run entirely offline using local LLMs via Ollama (Llama 3, Mistral) and local embeddings (bge-m3) for data privacy and confidentiality (refs: 1634718, 1636728, 1636992).
*   **Multilingual Support:** Systems frequently need to handle Ukrainian, Russian, and English interchangeably within the same conversation and knowledge base (refs: 1636852, 1636378).
*   **Deterministic Calculations:** Several projects require hard-coded business logic (numerology formulas, statistical aggregations) that must NOT be delegated to the LLM. AI is used only for text personalization on top of deterministic results (ref: 1637660).
*   **Telegram as Primary Channel:** 8 of 10 investigated projects use Telegram as the primary or secondary interface. Web widgets are secondary (refs: all files).

## 2. Architectural Pivot Points

*   **From Multi-Agent to Progressive Orchestration:** Multi-agent swarms are massive over-engineering for most freelance budgets ($600–$9,000 range). Most projects need L0/L1 orchestration. The pivot to L0–L3 tiers ensures we don't over-build (refs: 1636378 is L1, 1637660 is L2, only 1634149 is L3).
*   **From Redis to River (PostgreSQL):** Redis is excellent for caching but lacks transactional durability. River (a Postgres-backed job queue) provides at-least-once delivery which, combined with idempotent handlers, enables reliable side effects. Redis is retained strictly as an optional cache-only layer for rate limits and ephemeral session acceleration.
*   **Tool Safety Boundaries:** Unconstrained AI tool usage is dangerous — an AI agent must NOT autonomously charge a card or modify a CRM record. Ragivka mandates explicit safety boundaries: Read (safe), Draft (safe), and Write (audited, idempotent, optionally requires human confirmation based on policy).
*   **Softening "No Hallucinations":** It is architecturally dishonest to claim zero hallucinations from LLM output. The framework focuses on "reducing and detecting unsupported claims" via citation tracking and groundedness metrics. Deterministic outputs (PDF formatting, calculations) are handled by Go code, not the LLM.
*   **Caching for Slow External APIs:** Legacy platforms like PrestaShop 1.6 have slow, fragile APIs. The Tool Layer must implement a caching strategy (with configurable TTL) to avoid overloading external systems while keeping data fresh enough for real-time use (ref: 1636852).

## 3. The "Scope Creep" Threat & Out of Scope for v1

7 of 10 investigated projects carried high scope creep risk due to vague client requirements and subjective acceptance criteria ("the bot should sell effectively"). Ragivka's explicit tiering (L0–L3) provides a vocabulary for scoping proposals.

**Explicitly Out of Scope for v1:**
*   Full UI / Admin Dashboard (API/DB views initially).
*   Qdrant or Milvus scale-out (pgvector is the strict v1 default; the retrieval interface is designed for future pluggability).
*   Complex multi-agent debating swarms (reserved for L3/v2).
*   n8n or low-code workflow integration (Ragivka is code-first; n8n can wrap it externally).
*   Payment processing (handled by external payment providers; Ragivka processes their webhooks).
