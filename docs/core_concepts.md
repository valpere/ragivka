# Core Concepts for AI Assistant & RAG Projects (Ragivka Framework)

Based on industry best practices, deep architectural reviews, and analysis of 10 freelance project briefs, here is the distilled blueprint of the Ragivka framework. Ragivka is designed as a **Session-backed workflow and RAG framework**, scalable from simple Q&A bots to complex multi-agent ecosystems.

## 1. Orchestration Levels (Tiered Complexity) (FR-1 to FR-4)

Not every project requires a complex multi-agent swarm. Most freelance projects ($600–$9,000) need L0 or L1. Ragivka supports progressive orchestration:

*   **L0 (Deterministic):** Single LLM call for summarization or extraction. No state machine needed. Example: Knowledge Assistant summarizer.
*   **L1 (Tool Assistant):** Synchronous RAG + Function Calling to external APIs + human escalation. Example: Customer support bot, e-commerce sales agent.
*   **L2 (Workflow Pipeline):** Durable, multi-step asynchronous jobs. Example: Payment → Calculate → Generate PDF → Email delivery. Also: document registry analysis pipelines.
*   **L3 (Multi-Agent Graph):** Full DAG execution with Critic/Reviewer loops. Reserved for complex, dynamic tasks like multi-source marketing research. Most projects do NOT need this tier.

## 2. Session & State Management (FR-5, FR-6, FR-7, FR-23, NFR-4, NFR-5, NFR-6, NFR-7)

*   **Session as a First-Class Citizen:** Managing context via strict Finite State Machines (FSM). Four canonical states: `Active`, `WaitingForHuman`, `Completed`, `Expired`. This prevents "Subject Drift" and context poisoning (a pattern validated across multiple investigation files). Conversation history retention is strictly managed via sliding context windows.
*   **Database-Backed Workflow:** We use **River** (PostgreSQL-backed job queue) for all durable async work. This ensures at-least-once delivery with idempotent handlers. If a server crashes, the workflow resumes where it left off. Redis is optional and cache-only.

## 3. RAG & Ingestion Pipeline (FR-8 to FR-12)

A robust RAG system requires a formal ingestion pipeline:

*   **Ingestion:** `Connector → Object Storage (raw) → Parser/OCR → Normalizer → Chunker (512 tokens, 15% overlap) → Embedder → pgvector Indexer`.
*   **Retrieval:** **PostgreSQL + pgvector** is the v1 default. The retrieval interface is designed for future pluggability (Qdrant, Milvus), but these are out of scope for v1.
*   **Hybrid Search:** Combining `pgvector` semantic similarity with `tsvector`/BM25 keyword matching. Essential for domains with exact terminology (legal terms, product SKUs, ISBNs).
*   **Re-ranking:** A cross-encoder re-ranker re-orders the top K results to maximize precision before passing context to the LLM.
*   **Citations:** Every RAG-grounded answer must cite specific source chunks. In legal/medical domains, a disclaimer is required: "human expert verification needed."

## 4. Function Calling & Tool Safety (FR-16, FR-17, FR-18, NFR-10)

*   **Function Calling:** The LLM can invoke registered Read Tools mid-conversation to fetch real-time data. The AI must NOT fabricate product prices, stock levels, or order statuses — it must call the actual API first (validated by the PrestaShop and CRM case studies).
*   **Tool Safety Boundaries:** Tools are strictly categorized:
    *   **Read** — Safe, read-only API queries (e.g., check CRM order status, search product catalog).
    *   **Draft** — Prepare an action without committing (e.g., draft an email, preview a report).
    *   **Write** — State-mutating actions (charge a card, update a DB, send an email). These require idempotency keys, audit logging, and optionally human confirmation via HITL Gates.
*   **Tool Caching:** Read Tools calling slow external APIs (e.g., PrestaShop 1.6) must cache responses with configurable TTL.

## 5. Observability, Audit & Evaluation (FR-19, NFR-11 to NFR-15)

We do not claim to "completely eliminate" hallucinations from LLM output. Instead, we implement defense-in-depth to *reduce and detect* unsupported claims:

*   **Self-RAG & Critic Patterns:** A Critic/Reviewer step evaluates the generated answer against retrieved chunks. In v1, this acts purely as a logging/evaluation hook (flagging mismatches in analytics). Runtime blocking (rejecting/regenerating) is reserved for L3/v2.
*   **Deterministic Outputs:** For artifact generation (PDF, Excel), business calculations, and statistical aggregations, the LLM extracts structured JSON. Actual file generation and calculations are handled by deterministic Go code (e.g., `excelize`), ensuring template validity.
*   **Observability & Audit:** OpenTelemetry tracing for every request, Prometheus metrics, per-request token cost tracking. All state-mutating actions generate immutable records in the `AUDIT_LOG`.
*   **Agent Timeouts:** Explicit deadlock detection and configurable timeouts for L3 Critic/Generator loops.
