# Technical Requirements & Constraints

This document defines the functional and non-functional requirements that guide the development of the Ragivka framework. Every requirement is traceable to a finding or case study.

## 1. Non-Functional Constraints

### 1.1 Performance & Concurrency
*   **NFR-1 Low Latency (L0/L1):** Synchronous RAG requests must complete within webhook timeout limits (< 10 seconds). L2/L3 jobs are asynchronous and have no sync latency constraint.
*   **NFR-2 Concurrency:** The system must handle high concurrent loads (300+ req/min per tenant) using Go goroutines without memory bloat.
*   **NFR-3 Connection Pooling:** PostgreSQL connections must be pooled via `pgxpool` to prevent exhaustion during worker spikes.
*   **NFR-20 Rate Limiting:** The API and Channel Adapters must enforce per-tenant rate limits with configurable thresholds to prevent abuse.

### 1.2 Durability & Reliability
*   **NFR-4 Idempotency:** All Write Tool operations (generating PDFs, CRM webhooks, billing triggers, cart modifications) must be idempotent via unique operation keys (refs: Case Studies 2, 4, 6).
*   **NFR-5 Job Processing:** Background jobs (River) must support exponential backoff, configurable retry limits, and dead-letter queues, providing at-least-once delivery.
*   **NFR-6 State Persistence:** FSM transitions must be handled within a PostgreSQL transaction. Redis is strictly for ephemeral caching and rate limiting — never for durable state.
*   **NFR-7 Transaction Boundaries:** Database transactions must NOT be held open during external API calls. Claim job (short txn) → execute work → complete job (short txn).
*   **NFR-22 Backup/Recovery:** The PostgreSQL database must support continuous archiving for point-in-time recovery (PITR).
*   **NFR-24 Disaster Recovery:** The system must support an RTO of < 4 hours and RPO of < 1 hour in the event of a catastrophic database failure.

### 1.3 Extensibility & Deployment
*   **NFR-8 Pluggable LLMs:** Support for OpenAI, Anthropic, OpenRouter, Gemini, and local offline modes via Ollama. The Model Router interface must allow swapping providers without altering business logic.
*   **NFR-9 Deployment Modes:** The framework must support: (a) single-binary local deployment, (b) Docker Compose for development, (c) separate API/Worker binaries for horizontal scaling. Offline mode (Ollama + local embeddings) must be a first-class deployment target (ref: Case Study 5).
*   **NFR-10 Tool Registry:** Tools must be dynamically registerable (MCP-compatible transport) and enforce strict permission schemas (Read / Draft / Write).
*   **NFR-21 Error Standardization:** The REST API must return structured, standardized error responses (e.g., standard JSON with `code`, `message`, `details`) to clients.

### 1.4 Observability & Evaluation
*   **NFR-11 Tracing:** Every request must generate an OpenTelemetry distributed trace spanning the API boundary, database queries, and LLM API calls.
*   **NFR-12 Metrics:** Prometheus metrics must track: LLM token usage (prompt/completion), retrieval latency (p50/p95), River queue depth, and error rates.
*   **NFR-13 Cost Tracking:** Per-request token cost must be logged, enabling per-tenant cost attribution and budget enforcement.
*   **NFR-14 Quality Gates:** The system must track Retrieval Recall@K, Citation Coverage, and provide hooks for groundedness tests. In v1, these are strictly offline evaluation/logging hooks. Runtime blocking (rejecting/regenerating based on critic mismatches) is deferred to v2/L3.
*   **NFR-15 Audit Logging:** All Write Tool executions and FSM state transitions must be persistently logged in the `AUDIT_LOG` table with `idempotency_key`, tool name, and request/response hash.

### 1.5 Security & Multi-Tenancy
*   **NFR-16 Tenant Isolation:** All database queries and vector searches must be strictly tenant-scoped via `tenant_id` metadata filtering.
*   **NFR-17 Prompt Injection Defense:** User input must pass through a validation layer before being interpolated into prompts or tool arguments. For tools, the defense is the Read/Draft/Write permission boundary combined with strict JSON-schema output parsing.
*   **NFR-18 Data Privacy:** PII stripping hooks must be available in the ingestion pipeline before data reaches external LLM providers. Raw ingested documents in Object Storage must remain unmodified for traceability. For offline deployments using Ollama, PII never leaves the local machine.
*   **NFR-23 API Authentication:** The API must enforce strict authorization via short-lived JWT tokens or tenant-scoped API keys for all endpoints.

### 1.6 Internationalization
*   **NFR-19 Multilingual:** The framework must support multilingual knowledge bases and conversations (Ukrainian, Russian, English at minimum). Embedding models must handle Cyrillic text effectively (ref: Case Studies 3, 4).

## 2. Functional Requirements

### 2.1 Orchestration Tiers
*   **FR-1 L0 (Deterministic):** Single LLM call for summarization or extraction. No state machine required.
*   **FR-2 L1 (Tool Assistant):** Synchronous workflow with RAG retrieval, Function Calling to external APIs, and HITL escalation.
*   **FR-3 L2 (Workflow Pipeline):** Durable, multi-step asynchronous jobs via River (e.g., Ingest → Retrieve → Calculate → Generate PDF → Email).
*   **FR-4 L3 (Multi-Agent Graph):** DAG orchestration with Critic/Reviewer nodes, deadlock detection, and configurable timeouts per node.

### 2.2 State Machine (Session Management)
*   **FR-5 FSM States:** Four canonical states: `Active`, `WaitingForHuman`, `Completed`, `Expired`.
*   **FR-6 Optimistic Locking:** Session updates must use a `version` column to prevent race conditions from concurrent messages.
*   **FR-7 Session Expiry:** Sessions must auto-transition to `Expired` after a configurable inactivity timeout.
*   **FR-23 Conversation History Limits:** The session manager must enforce a context window retention policy (e.g., keeping only the last N turns or summarizing older messages) to prevent exceeding LLM token limits.

### 2.3 Knowledge & RAG Pipeline
*   **FR-8 Ingestion Lifecycle:** Connectors ingest raw documents (PDF, URL, DB rows) into Object Storage. Parsers (including OCR for scanned PDFs) normalize text. The pipeline supports document versioning, re-ingestion, and stale chunk cleanup.
*   **FR-9 Chunking:** Configurable semantic chunking (default: 512 tokens, 15% overlap). Chunks retain metadata: `document_id`, ordinal position, and source location for citation linking.
*   **FR-10 Hybrid Search:** Retrieval must combine `pgvector` HNSW similarity search with `tsvector`-based full-text keyword search (using `ts_rank`). Results are merged and deduplicated before re-ranking.
*   **FR-11 Re-ranking:** A cross-encoder re-ranker (e.g., Cohere Rerank or local BGE reranker) must re-order the top K results to maximize precision before feeding context to the LLM.
*   **FR-12 Citations:** Every RAG-grounded answer must include traceable citations linking back to specific source chunks. The citation format must include document name and chunk ordinal.

### 2.4 AI Layer
*   **FR-13 Model Router:** Routes requests to the appropriate LLM based on task complexity and cost policy. Cheap models (GPT-4o-mini, Haiku) for classification/extraction; expensive models (Sonnet, GPT-4o) for complex reasoning. Must support fallback on provider failure.
*   **FR-14 Prompt Registry:** Version-controlled storage for system prompts in PostgreSQL. Prompts are loaded by name+version, preventing drift between deployments.
*   **FR-15 Structured Output:** The LLM must be constrained to return strict JSON matching Go struct definitions. This enables deterministic downstream processing (artifact generation, tool arguments).
### 2.5 Tool Layer
*   **FR-16 Function Calling:** The LLM must be able to invoke registered Read, Draft, and Write Tools mid-conversation based on policy to fetch data, prepare actions, or mutate state (e.g., querying PrestaShop for price, or updating a cart).
*   **FR-17 HITL Gates:** The system must transition the FSM to `WaitingForHuman` (FR-5) and notify an operator when: (a) a Write Tool requires explicit human approval, or (b) the LLM expresses low confidence or detects an out-of-knowledge-base query (customer support escalation).
*   **FR-18 Tool Caching:** Read Tools calling slow or rate-limited external APIs must support configurable response caching with TTL to avoid overloading external systems.

### 2.6 Deterministic Artifact Generation
*   **FR-19 Separation of Concerns:** LLM output is used for structured data extraction (JSON). Actual file generation (PDF, Excel) is handled deterministically by Go libraries (`excelize`, HTML-to-PDF converters). The LLM never directly produces binary file content.
*   **FR-20 Object Storage:** Generated artifacts and raw ingested documents must be stored in S3-compatible object storage, referenced by the `DOCUMENT` and `ARTIFACT` tables in PostgreSQL.

### 2.7 Channel Adapters
*   **FR-21 Telegram Adapter:** Webhook-based integration via `gotgbot`. Must handle message parsing, Markdown/HTML response formatting, and inline keyboard interactions.
*   **FR-22 Web Widget API:** REST + WebSocket endpoints for embedding a chat UI on client websites. Must support tenant-scoped API key authentication.
*   **FR-24 Rate Limiting Strategy:** The system must enforce API rate limits (NFR-20) using a Redis-backed sliding window algorithm to ensure fairness and prevent tenant abuse.
