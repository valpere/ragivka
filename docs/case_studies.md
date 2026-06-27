# Case Studies & Reference Benchmarks

This document outlines six project archetypes driving the Ragivka architecture, mapped to orchestration tiers (L0–L3). Each case study is traced back to concrete freelance investigation files.

## Case Study 1: Knowledge Assistant / Summarizer (Tier: L0/L1)
**Source:** `AI-Powered Knowledge Assistant.md`, `03-ai-powered_knowledge_assistant_architecture_and_mvp.md`
**Profile:** A Telegram bot that summarizes user-submitted text or URLs. The simplest Ragivka deployment.
**Orchestration Level:** **L0** (single LLM call) scaling to **L1** (with session history and embeddings search). Must be synchronous to satisfy webhook timeout limits.
**Key Ragivka Features Validated:**
- **Ingestion Pipeline:** Fetch URL → Extract/Clean HTML → Chunk → Embed → Store.
- **Synchronous Execution:** Must handle summarization synchronously within the 10-second webhook limit. (If longer processing is needed, it must be upgraded to an L2 async job).
- **Channel Adapter:** Telegram via `gotgbot` with webhook-based integration.
**Timeline:** ~1 week (MVP), ~3 weeks (with vector search and history).

## Case Study 2: Marketing Automation & Research (Tier: L3)
**Source:** `1634149-Автоматизация маркетинг через ИИ.md`
**Profile:** A complex multi-agent system for deep research, deduplication against published articles, and social media post generation in brand style.
**Orchestration Level:** **L3 (Multi-Agent Graph)** — Requires Researcher → Analyst → Copywriter → Critic loop with dynamic task routing.
**Key Ragivka Features Validated:**
- **Graph Execution:** DAG orchestration with Critic review loops and deadlock timeouts.
- **Tool Registry:** Integration with web scrapers and social media APIs (Write Tools with idempotency).
- **Model Router:** Cheap models (GPT-4o-mini) for initial scoring; expensive models (Sonnet/GPT-4o) for final copywriting.
- **RAG Deduplication:** pgvector-based semantic search against previously published articles.
**Timeline:** ~5–7 weeks.

## Case Study 3: Customer Support Assistant (Tier: L1/L2)
**Source:** `1636378-створення-аі-асистента-для-комун.md`
**Profile:** A customer-facing chat widget that answers product questions and escalates to human operators when confidence is low.
**Orchestration Level:** **L1/L2** — Fast RAG lookups (L1) with durable escalation workflows (L2).
**Key Ragivka Features Validated:**
- **FSM & HITL Gates:** When confidence is low, the FSM transitions to `WaitingForHuman`, freezing agent actions and routing context to a human operator via Telegram or CRM.
- **Hybrid Retrieval:** ts_rank + Vector search over the product catalog.
- **Model Router:** GPT-4o-mini for intent classification; GPT-4o/Claude for complex technical answers.
- **Prompt Injection Defense:** Input passes through an LLM-validator with strict JSON-schema output.
**Timeline:** ~4–6 weeks. Budget range: $600–$4,000.

## Case Study 4: E-Commerce Sales Agent (Tier: L1)
**Source:** `1636852-розробка-ai-агента-з-продажу-для-і.md`
**Profile:** An AI salesperson for an online bookstore running on PrestaShop 1.6, integrated with KeyCRM for lead management.
**Orchestration Level:** **L1 (Tool Assistant)** — RAG over product catalog + real-time Function Calling to PrestaShop API.
**Key Ragivka Features Validated:**
- **Function Calling:** The LLM queries PrestaShop REST API for real-time price, stock, and product data before every response. AI must NOT fabricate products or prices.
- **Tool Caching:** PrestaShop 1.6 is legacy and slow; the Tool Layer caches API responses with configurable TTL to avoid overloading the external system.
- **Write Tools with Audit:** Cart management (adding products) and CRM lead creation (KeyCRM webhook) are Write Tools requiring idempotency keys.
- **Multilingual:** Must handle Ukrainian, Russian, and English within the same conversation.
**Timeline:** ~7–9 weeks. Budget range: $1,500–$9,000.

## Case Study 5: Legal Registry Analyzer (Tier: L2)
**Source:** `1637443-ai-агент-для-пошуку-та-аналізу-мас.md`
**Profile:** An AI agent that searches large government registries of legal decisions, classifies outcomes, and generates statistical reports with citations.
**Orchestration Level:** **L2 (Workflow Pipeline)** — Multi-step: Ingest → Retrieve → Classify → Aggregate → Report.
**Key Ragivka Features Validated:**
- **Hybrid Search:** ts_rank for exact legal terms + vector search for semantic similarity across 10,000+ documents.
- **Local Embeddings:** bge-m3 via Ollama to reduce cost and latency for high-volume embedding.
- **Citations:** Every AI conclusion must link to a source document. A disclaimer is required: human expert verification needed.
- **Structured Output:** Classification results (satisfied/partially/denied) as strict JSON, fed into statistical aggregation.
**Timeline:** ~9–12 weeks. Budget range: $2,500–$18,000.

## Case Study 6: Automated Report Generator (Tier: L2)
**Source:** `1637660-розробник для створення автоматизованого AI-сервісу з генерації.md`
**Profile:** A turnkey digital product: user pays → enters data → receives a personalized 30-page PDF report automatically.
**Orchestration Level:** **L2 (Workflow Pipeline)** — Payment webhook → Deterministic calculation → RAG personalization → PDF generation → Email delivery.
**Key Ragivka Features Validated:**
- **Deterministic Artifact Generation:** Business formulas are hard-coded in Go (NOT delegated to the LLM). AI is used only for natural-language text personalization on top of deterministic results.
- **Payment Webhook Integration:** Lemon Squeezy / WayForPay webhook triggers the generation pipeline via River job queue.
- **Object Storage:** Generated PDFs stored in S3-compatible storage for delivery.
- **Async Under Load:** Must handle peak loads during advertising launches via River's durable queue.
**Timeline:** ~7 weeks. Budget range: 12,000–60,000 UAH.
