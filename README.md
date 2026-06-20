<p align="center">
  <img src="docs/assets/logo.png" alt="Synapse Proxy Logo" width="650" />
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Status-Active-success.svg" alt="Status">
  <img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8.svg" alt="Go Version">
  <img src="https://img.shields.io/badge/OpenAI%20API-Compatible-orange.svg" alt="OpenAI Compatible">
</p>

<h1 align="center">Synapse Proxy: The Ultimate Agentic Firewall & Observability Gateway</h1>

> **A drop-in, open-source proxy that brings observability, security, and smart caching to your autonomous AI agents.**

Synapse Proxy sits gracefully between your application and any OpenAI-compatible LLM provider. Its primary mission is to empower developers with **Agentic Observability** and a **Smart Firewall**, keeping rogue agent loops in check, protecting sensitive PII data, and making multi-turn LLM interactions entirely visible and measurable.

While it actively protects your infrastructure, Synapse Proxy quietly optimizes token usage in the background with a four-tier cache pipeline (L0 to L3), ensuring you never pay twice for the same agentic thought process.

**Version française** : [README_FR.md](README_FR.md)

---

## 🛡️ Agentic Firewall & Security

When building autonomous agents, the biggest risk is infinite loops and runaway costs. Synapse Proxy introduces a robust Firewall specifically designed for AI agents:

- **Loop Kill Switch & Self-Correction:** Detects when an agent is drifting into an infinite loop (repeating identical request payloads). It intercepts the execution and returns a mock OpenAI-compatible chat completion response (`HTTP 200`) containing a descriptive self-correction warning to guide the agent to change strategy.
- **Granular Tool Cache TTLs:** Configure custom cache durations per tool name (including setting TTL to 0s to disable caching for specific stateful tools) via the Firewall Dashboard.
- **PII Redaction:** Native regex-based masking of sensitive data (Emails, API Keys) before the prompt ever reaches the upstream provider.
- **Tool Allowlisting:** Lock down your agent's capabilities. If an agent hallucinates a tool or tries to invoke an unauthorized function, the Proxy actively blocks the request.
- **Session Circuit Breaker:** Define strict prompt-token limits per session to cap expenditures on a per-task basis.

---

## 📊 Advanced Observability & Session Replay

Every request is persisted to a PostgreSQL database, turning black-box agent behavior into a transparent, analyzable flow.

- **Session Replay Timeline:** Inspect agent interactions step-by-step. Reconstruct the agent's flow, tool calls, and payload latency across a unified timeline.
- **Context Window Tracker:** A visual graph comparing the *Original Prompt Tokens* against the *L3 Compressed Tokens* over time, demonstrating exactly how context grows and how Synapse Proxy mitigates it.
- **System Prompt Diffing:** Agents sometimes rewrite their own instructions mid-session. The proxy extracts and diffs the system prompt, highlighting exactly what changed.
- **Dataset Export (JSONL):** One-click export of a session's trajectory into a Fine-Tuning ready JSONL dataset.
- **A/B Benchmark:** Toggle benchmark mode to fire control and optimized requests in parallel, using an LLM judge to score response similarity.

> 📖 **Deep Dive:** Want to know exactly what is logged? Read the [Telemetry & Database Schema](docs/telemetry_schema.md) documentation.

<p align="center">
  <img src="docs/assets/flow.png" alt="Synapse Proxy Flow" width="650" />
</p>

---

## ⚡ Smart Caching & Optimization

Though security and observability take center stage, Synapse Proxy features a state-of-art caching engine designed to minimize latency and token waste. 

- **Drop-in OpenAI replacement:** No SDK changes required. Just point your client at `http://<host>:8080/v1` with an `Authorization: Bearer sk-opti-...` virtual key.
- **Four caches in one binary:**
  - **L0 In-flight Dedup:** Blocks and deduplicates identical concurrent requests (useful for agent fan-outs).
  - **L1 Exact Match:** Ultra-fast SHA-256 match for scripts retrying the exact same query.
  - **L2 Semantic Match:** ONNX-based vector search (MiniLM) for conceptually identical queries. Auto-disabled on multi-turn conversations to prevent state corruption.
  - **L3 Prefix-Preserving Compression:** Intelligently prunes stale `<thought>` blocks, truncates oversized tool outputs, and condenses older history. It maintains a byte-exact prefix so the upstream provider's native prompt cache remains 99% effective.
  - **Semantic Tool Deduplication:** Intercepts LLM tool calls and retrieves cached outputs from similar prior invocations (exact matching + ONNX embeddings with cosine similarity >90% on VSS), recursively calling the LLM upstream to bypass client-side tool execution.

> 📖 **Deep Dive:** Learn the magic behind our Cache-Preserving L3 and ONNX L2 search in the [Caching Architecture](docs/caching_architecture.md) documentation.

<p align="center">
  <img src="docs/assets/diag_en.png" alt="Synapse Proxy Diagram" width="650" />
</p>

---

## 🔌 MCP Server (Model Context Protocol)

Synapse Proxy doubles as a robust MCP server, exposing **14 specialized tools** (including A/B Benchmarks, Session Recording, Key Management, and Analytics) directly to your IDE (Cursor, Claude Code, Continue, etc.).

Since Synapse Proxy is fully open source, **all 14 tools are completely free to use** locally or on your self-hosted stack.

To expose all 14 tools, run the server with `--mcp-tier=full` (pointing it to your self-hosted dashboard):

```bash
# Stdio mode (recommended for local Cursor/IDE integrations)
./synapse-proxy --mcp --mcp-tier=full

# HTTP SSE mode (for remote/multi-user server setup)
./synapse-proxy --mcp-http --mcp-http-port=8081 --mcp-tier=full --dashboard-url=http://localhost:3000
```

> 📖 **Deep Dive:** Read the [Model Context Protocol (MCP) Guide](docs/mcp_server.md) for the complete list of all 14 tools, parameter schemas, and IDE setup details.

---

## 📊 Dashboard (Next.js) — Fully Open Source

The repo ships with a complete Next.js dashboard under `./dashboard` that turns the proxy's raw telemetry into an actionable control plane. It is **fully open source under the same MIT license** as the proxy: audit it, fork it, self-host it, theme it — there is no closed-source SaaS-only path.

| Capability | Where it lives | Why it matters |
|---|---|---|
| **Live Telemetry** (group by Agent / Session / Model) | `app/page.tsx`, `components/LiveTelemetryGrouped.tsx` | See every request arrive via SSE. Rows that share a conversation (same system prompt + tool set) auto-group using `convSignature` (see "Multiturn tracking" below). |
| **Agent Firewall Modal** (per-key) | `components/FirewallModal.tsx` | The headline feature. Toggle, per virtual key: L1 / L2 / L3 cache stages, the kill switch, PII redaction, the session token cap, and the tool allow-list. Changes propagate to Redis in real time so the proxy picks them up on the next request. |
| **Tool-call fingerprinting** (`optiagent/tool_fingerprint.go` + `transport_http.go`) | Proxy-side | Detects an agent calling the same tool with the same arguments 4× within 30 seconds. Returns **HTTP 429 + Retry-After: 60** ("Recursive Loop Detected") when cache check misses, so the agent backs off without dying. |
| **Multiturn session tracking** (`utils/multiturn.go` + `RequestLog.turnCount`/`convSignature`) | Proxy-side + DB | Every row records `(turnCount, convSignature)` so the dashboard can group rows by natural conversation even when the agent never sent an explicit `X-Session-Id`. The conversation signature is `sha1(system_prompt || tool_names)[:8]`. |
| **Session Summary** (3 observability graphs) | `app/page.tsx`, `app/admin/sessions/page.tsx` | Context Window (Original vs L3 Compressed), System Prompt Diff (with `react-diff-viewer-continued`), Agent Flow Timeline (step-by-step with tool calls). Available for every group with 2+ rows. |
| **Playground v3** (`/playground`) | `app/playground/` | Side-by-side A/B chat: same prompt twice in parallel, once through the proxy, once directly upstream (forced `X-Bypass-Cache: true`). Per-bubble cache badges, sparklines, artifact renderer (`<iframe sandbox>` for HTML, code download for python/js/etc.). |
| **Request Explorer** (`/admin/explorer`) | `app/admin/explorer/page.tsx` | Sortable, filterable table over `RequestLog` with per-row drill-down to full payload + optimized payload + system prompt. |
| **Admin / Sessions / Pricing / Users** | `app/admin/*` | Self-host the whole product: virtual keys, model pricing, alert rules, email campaigns, Stripe billing (set `STRIPE_*` env to enable). |

### What's new (post-launch)

- **Agent Firewall as a first-class concept** — every virtual key has firewall configurations (L1/L2/L3 cache toggles, kill switch, session token limit, tool whitelist, PII redaction, fingerprint loop detection, custom tool TTLs). Configured in the dashboard and synced to Redis.
- **Self-Correction loop warnings** — replaces traditional hard loops (HTTP 400/429 blocks) with mock HTTP 200 completion messages warning the agent about the repeated action, allowing the agent to self-correct within the prompt history.
- **Semantic Tool Deduplication** — intercepting tool calls in LLM responses and resolving them against cached tool executions using ONNX embeddings and Redis VSS, triggering recursive upstream calls to bypass client-side execution loops.
- **Multiturn session detection** — the dashboard groups requests by conversation fingerprint instead of per-request buckets, so a 4-turn debugging session shows up as one row with `Tour 1/2/3/4` badges, not 4 separate rows.
- **MCP server in HTTP mode** — runs as a long-lived process behind the same Caddy reverse proxy, exposing 14 tools (4 free + 10 paid) to any MCP-compatible IDE.

### Dashboard architecture

```
dashboard/
├── app/                        # Next.js App Router
│   ├── (auth)/                 # login / signup / forgot-password / reset-password
│   ├── admin/                  # admin pages (sessions / explorer / pricing / users / alerts)
│   ├── api/                    # REST + SSE endpoints (analytics, keys, sessions, telemetry)
│   ├── playground/             # A/B Playground v3
│   └── settings/               # per-key Firewall + Zero-Log + benchmark toggles
├── components/                 # LiveTelemetryGrouped, FirewallModal, RequestExplorer, etc.
├── lib/                        # authOptions, prisma, stripe, email
├── prisma/
│   ├── schema.prisma           # ApiKey firewall fields, RequestLog turnCount / convSignature
│   └── migrations/             # 2026_06_*: agent_detector, pricing, zero_log, alert_rules, payload_hash, response_payload, multiturn
├── public/                     # logo, diag_en, diag_fr, flow, mega_flow
└── .env.example                # template; .env is git-ignored
```

The dashboard reads from the same Postgres + Redis instances as the proxy, so a self-hosted deployment has **one database to back up**.

---

## 🛠️ API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/v1/chat/completions` | The core OpenAI-compatible proxy endpoint. |
| `GET`  | `/healthz` | Liveness probe. |
| `GET`  | `/readyz`  | Readiness probe (verifies Postgres & Redis connections). |
| `GET`  | `/metrics` | Prometheus metrics (cache hits, panics, loop blocks). |
| `GET`  | `/v1/models` | List of supported models. |

---

## 📄 License

Synapse Proxy is **fully open source under the MIT License** — proxy, dashboard, and SDKs alike. Self-host the whole stack, audit every line, fork whatever you need. We offer a managed SaaS at [synapse-proxy.com](https://synapse-proxy.com) for teams that prefer not to operate Postgres + Redis themselves; the hosted version is the exact same code as this repo, just pre-configured. The SaaS is a **convenience**, not a gatekeeper.
