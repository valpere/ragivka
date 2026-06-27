# Ragivka Framework Implementation Plan

This document outlines the phased approach to building the Ragivka framework, adopting a bottom-up strategy based on proven architectural primitives.

## Phase 1: Foundation & Runtime Primitives

1.  **Initialize Go Module & Ops Layer (NFR-2, NFR-3, NFR-9, NFR-16, NFR-19, NFR-22, NFR-24):** Set up the Go project (`go mod init`), directory structure, and `pgxpool` connection pooling. Establish multitenancy middleware (`tenant_id` context). Configure Docker Compose (PostgreSQL + pgvector with PITR backup and RTO/RPO DR scaffolding). Define Deployment Modes.
2.  **Observability Stack (NFR-11, NFR-12, NFR-13):** Implement baseline OpenTelemetry distributed tracing. Expose Prometheus endpoints for metrics (LLM latency, River queue depth, cost tracking per tenant).
3.  **Session & Job Store (FR-5, FR-6, FR-7, FR-23, NFR-4, NFR-5, NFR-6, NFR-7):** Implement the PostgreSQL-backed FSM with explicit database transaction boundaries, optimistic locking, and context window history limits (FR-23). Integrate the **River** job queue.
4.  **Model Router & Prompt Registry (FR-13, FR-14, FR-15, NFR-8, NFR-17):** Build the LLM abstraction layer with token budgeting and cost-based routing. Implement the Prompt Registry schema. Integrate Structured Output enforcement and JSON-schema prompt injection defenses.

## Phase 2: RAG Pipeline & Knowledge Layer

1.  **Object Storage & Artifacts (FR-20):** Initialize S3-compatible storage interfaces for raw document and artifact persistence.
2.  **Ingestion Engine (FR-8, FR-9, NFR-18):** Build the pipeline: Connector → S3 (raw document, unmodified) → Parser/OCR → Chunker (with PII stripping hooks before LLM embedding) → Embedder → pgvector Indexer.
3.  **Retrieval Engine (FR-10, FR-11, FR-12):** Implement hybrid search (pgvector + ts_rank), cross-encoder re-ranking, and citation tracking.
4.  **Evaluation Hooks (NFR-14):** Build logging hooks to measure Retrieval Recall@K and Citation Coverage.

## Phase 3: Tooling & Orchestration

1.  **Tool Registry & Audit (FR-16, FR-17, FR-18, NFR-10, NFR-15):** Implement the Read/Draft/Write Tool boundaries. Enforce strict `AUDIT_LOG` entries for all write actions. Build HITL approval gates, low-confidence escalation routes, and read-tool caching.
2.  **Deterministic Generation (FR-19):** Build Go handlers for deterministic artifact creation (PDF/Excel) isolated from direct LLM output generation.
3.  **Linear Pipeline Orchestrator (FR-1, FR-2, FR-3, NFR-1):** Build the synchronous L0/L1 handlers (strict webhook latency) and the asynchronous L2 pipeline workflows.
4.  **Graph Engine (FR-4):** Introduce the optional DAG runtime for L3 multi-agent loops.

## Phase 4: Interface Adapters & MVP

1.  **Channel Adapters (FR-21, FR-22, FR-24, NFR-20, NFR-21, NFR-23):** Implement the Telegram adapter (`gotgbot` webhook). Build the Web Widget REST/WebSocket API with standardized error structures (NFR-21), per-tenant Redis rate limiting (FR-24/NFR-20), and strict JWT/API Key auth (NFR-23).
2.  **MVP Implementation:** Build an **L1 Customer Support Assistant** (Case Study 3) to validate the framework end-to-end: RAG retrieval → Model Router → Structured Output → HITL Escalation → Telegram response.
