# OptiToken

> **An open-source LLM proxy that turns agent traffic into a measurable, optimizable flow.**

OptiToken sits between your application and any OpenAI-compatible LLM provider (OpenAI, Anthropic, MiniMax, anything that speaks `/v1/chat/completions`). It deduplicates identical requests, runs a four-tier cache (L0 in-flight → L1 exact → L2 semantic → L3 compression), detects agentic traffic, and writes a per-request telemetry row so you can see what your agents are doing.

The proxy is **open source (MIT)**. The hosted dashboard at [optitoken.net](https://optitoken.net) is a separate commercial product. Everything in this repository is exactly what runs in production; nothing is mocked.

---

## At a glance

- **Drop-in OpenAI replacement.** No SDK changes. Just point your client at `http://<host>:8080/v1` with an `Authorization: Bearer sk-opti-...` virtual key.
- **Four caches, all in one binary.** L0 in-flight dedup, L1 SHA-256 exact match, L2 ONNX semantic vector search, L3 prefix-preserving compression.
- **Self-healing agent detection.** A background goroutine parses each request's User-Agent, system prompt, and tool definitions to identify Hermes, Claude Code, OpenClaw, LangChain, raw curl, etc., and disables caches that would corrupt an agent's state.
- **Per-request telemetry** persisted to Postgres. Per-class savings, agent ID, cache level, payload hash, session ID, latency. Every row.
- **Record Session.** Tag a window of traffic with a session id (server-side, no agent changes). Get a full per-class breakdown, by provider, by model, by agent. Browse the history from `/admin/sessions`.
- **A/B benchmark with an LLM judge.** Toggle benchmark mode per key, the proxy fires control + optimized requests in parallel and a third LLM call scores the similarity. Inspect the side-by-side in `/benchmark`.
- **Prometheus metrics** at `/metrics` (custom hand-written format, no client_golang dependency).
- **Panic recovery** at the handler level. One bad payload does not crash the Go process.

---

## How the cache pipeline works

```
                     your app / agent / SDK
                              │
                              │  HTTP, Authorization: Bearer sk-opti-...
                              ▼
              ┌────────────────────────────────┐
              │       OptiToken Proxy (Go)     │
              │                                │
              │  ┌───────┐  ┌──────────────┐   │
              │  │  L0   │─▶│      L1      │   │
              │  │ in-fl.│  │ exact match  │   │
              │  │ dedup │  │  (SHA-256)  │   │
              │  └───────┘  └──────┬───────┘   │
              │                    │ miss     │
              │                    ▼          │
              │              ┌──────────┐     │
              │              │    L2    │     │
              │              │ semantic │     │
              │              │ (ONNX)   │     │
              │              └────┬─────┘     │
              │                   │ miss     │
              │                   ▼          │
              │             ┌──────────┐      │
              │             │    L3    │      │
              │             │  (tail   │      │
              │             │compress) │      │
              │             └────┬─────┘      │
              │                  │           │
              └──────────────────┼───────────┘
                                 │
                                 ▼
                ┌────────────────────────────┐
                │  upstream provider          │
                │  (OpenAI, Anthropic, etc.)  │
                └────────────────────────────┘

  Side channel:
    • Every decision lands in Postgres (RequestLog) for inspection
    • /metrics serves Prometheus-format counters
    • Optional A/B benchmark via control + opt + LLM judge
```

The four caches, in detail:

| Cache | What it does | When it kicks in | Source |
|-------|--------------|------------------|--------|
| **L0** In-flight dedup | Two identical requests (same virtual key, same SHA-256 payload) arrive concurrently. The first acquires a Redis SETNX lock with a 30-second TTL and processes normally. The second **blocks and waits** on the lock. | Race conditions, agent retries, parallel curl, fan-out from a parent agent. | `optiagent/dedup.go` |
| **L1** Exact match | The full SHA-256 of the normalized request payload is the cache key. Hit returns the cached response in <2 ms. | Cron jobs, scripts that retry the same query, identical tool calls across agent turns. Per virtual key. | `optiagent/engine.go:131` |
| **L2** Semantic | The last user message is embedded by a local ONNX model (multilingual MiniLM, 384-dim) and a KNN search is run against a Redis VSS index (`FT.SEARCH idx:l2cache`). The cosine similarity threshold is the per-key `semantic_tolerance` (default 0.15). | "How do I reset my password?" matches "Forgot password, what now?". **Auto-disabled** if the request is multi-turn (`nonSystemCount > 1`) or contains an image — two consecutive turns of an agent have near-identical embeddings and returning a cached response from a *different* turn would corrupt the conversation state. | `cache/l2_vector.go`, `optiagent/engine.go:151` |
| **L3** Compression | The system prompt, tool declarations, and older history are byte-exact preserved. Only the last 4 messages are rewritten: stale `<thought>` blocks pruned, repeated tool calls collapsed, `reasoning_content` stripped, oversized tool outputs truncated, the LLM gets a `(Earlier tool results in this transcript may be truncated.)` hint prepended to its system prompt. | Long agent sessions with redundant chain-of-thought, repeated tool calls, stale tool outputs. | `optiagent/compressor.go`, `optiagent/compaction_hint.go`, `optiagent/prefix_split.go` |

**When the cache is bypassed** (`CacheLevel: BYPASS`): the client sends `X-Bypass-Cache: true` (curl, manual debugging, or the `isBypass` flag in the playground). The proxy forwards the request as-is, persists the row to telemetry, but skips every cache layer.

**When the cache is bypassed for correctness**: L2 is auto-disabled for multi-turn and image payloads, as described above. L1 still runs.

---

## Cache-Preserving L3 — keeping the provider's prompt cache alive

This is the part of the design that is the easiest to get wrong and the most expensive when you do.

Anthropic, OpenAI, and MiniMax all hash your request bytes and serve the same prefix from a server-side cache for ~90% off on subsequent calls. But the hash is **byte-exact** — change a whitespace, reorder a JSON key, escape a `<` to `\u003c`, and the cache miss happens and you pay full price.

The naive mistake most compression libraries make: re-encode the entire payload, which changes all of the above. We measured this exact failure mode on 2026-06-18. The reproducer is in `test/ab_benchmark_2026_06_18/`. With the naive compressor, the provider's `cached_tokens` field drops to 0 on the second request; with the cache-preserving L3, it stayed at 6 550 / 6 564 (99.8%).

The implementation is three layers:

1. **Phase 1 — Idempotent encoder.** A deterministic JSON encoder (`proxy/optiagent/marshal_deterministic.go`) guarantees that two compressions of the same payload produce byte-identical output. Sorted keys alphabetically, no whitespace, no HTML escaping, deterministic float formatting. Six unit tests in `compressor_test.go` lock this in.

2. **Phase 2 — Prefix-preserving split.** Before compressing, the proxy walks the JSON payload character by character, finds the boundary between the static prefix (system prompt, tool declarations, history older than 4 messages back) and the dynamic tail (recent user/assistant turns), and splits. The prefix is left byte-exact. Only the tail is rewritten. Nine unit tests in `prefix_split_test.go`.

3. **Phase 3 — Co-located compression.** The tail is wrapped in a synthetic envelope (`{"messages":[<tail>]}`), passed through the standard L3 rules, unwrapped, and re-attached. The result is a valid JSON document where the first N bytes are byte-exact identical to the input.

The validation run, captured in `test/ab_benchmark_2026_06_18/data_proxy_log.txt`, on the 4th of 5 identical requests to a MiniMax-M3 upstream, the provider returned:

```json
"usage": {
  "prompt_tokens": 6564,
  "completion_tokens": 2,
  "prompt_tokens_details": { "cached_tokens": 6550 }
}
```

6 550 of the 6 564 prompt tokens (99.8%) were served from the provider's own cache because the prefix bytes were byte-exact identical to the previous request.

---

## Telemetry

Every request gets a row in `RequestLog`:

| Column | Meaning |
|--------|---------|
| `cacheLevel` | `MISS`, `L0`, `L1`, `L2`, `L3`, `LOOP`, `BYPASS` |
| `promptTokensOrig` / `promptTokensOpt` | Token counts measured by the upstream |
| `completionTokensOrig` / `completionTokensOpt` | Same for completions |
| `savingsInputFresh` / `savingsCacheRead` / `savingsCacheCreation` / `savingsOutput` | Per-class dollar savings, computed against the `ProviderModel` table (`internal/utils/savings.go`) |
| `cacheCreationTokens` / `cacheReadTokens` / `cacheHitTokens` / `cacheMissTokens` | Read from the upstream response (`prompt_tokens_details.cached_tokens` for OpenAI, `cache_creation_input_tokens` and `cache_read_input_tokens` for Anthropic). 0 if the upstream doesn't expose them. |
| `durationMs` | Wall-clock end-to-end |
| `agentId` / `agentLabel` | From `proxy/internal/utils/agent_detector.go` — User-Agent + system-prompt heuristics. Resolved labels: `Hermes`, `Claude Code`, `OpenClaw`, `LangChain`, `chat-direct`, `tool-using-agent`, `curl`, `python-requests`, `node-fetch`, etc. |
| `sessionId` | Set by Record Session (see below) |
| `payloadHash` | SHA-256 of the original payload — used by Most Expensive Prompts to group identical prompts |
| `originalPayload` / `optimizedPayload` / `responsePayload` | Stored unless `zeroLog=true` on the key |

The DB schema (Postgres) is documented in `dashboard/prisma/schema.prisma` and the migration history under `prisma/migrations/`. The relevant tables are `User`, `ApiKey`, `RequestLog`, `BenchmarkLog`, `AlertRule`, `AlertEvent`, `ProviderModel`. Indexes exist on `RequestLog(apiKeyId, createdAt DESC)`, `RequestLog(agentId)`, `RequestLog(sessionId)`, `RequestLog(payloadHash)`, `RequestLog(agentId, createdAt DESC)`.

There is also a model radar (`internal/workers/model_radar.go`) that auto-discovers new models the proxy sees for the first time and stores them in Redis with status `learning` → `known` → `mapped`. A field discoverer (`internal/workers/field_discoverer.go`) maps the upstream's response shape to the right `usage` fields per provider.

---

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/v1/chat/completions` | The main proxy endpoint. OpenAI-compatible. |
| `GET`  | `/healthz` | Liveness. Returns `{"status":"ok"}` as long as the Go process is alive. |
| `GET`  | `/readyz`  | Readiness. Pings Postgres and Redis, returns 200 only if both respond. |
| `GET`  | `/metrics` | Prometheus text format. Panic counters per handler, cache-hit counters per level, request totals. Hand-written, no `prometheus/client_golang` dependency. |
| `GET`  | `/v1/models` | OpenAI-compatible list of models the proxy can serve. |
| `POST` | `/admin/purge-cache` | Operator endpoint: clears all caches for a virtual key. |

The proxy is also resilient to upstream application-level errors that arrive inside a 200 OK (e.g. MiniMax returns `{"base_resp": {"status_code": 2056, "status_msg": "quota exhausted"}}` with a 200 status). `detectUpstreamAppError` parses the body and converts quota errors into HTTP 402 Payment Required. See `proxy.go:1087` (`appError`).

Panic recovery: `ProxyHandler` wraps every request in a `defer recover()` that logs the panic + stack + masked virtual key + records a Prometheus counter, then returns a clean 502. A single bad payload does not crash the Go process.

---

## Virtual keys and encryption

You give the proxy a virtual key (`sk-opti-...`). The proxy looks it up in Redis (`HGETALL optitoken:keys:<vk>`) to find:

- The real upstream provider key, **AES-256-GCM encrypted** with a 12-byte random IV and a 16-byte auth tag. Decrypted in-memory only, only on the goroutine handling the request, only for that key.
- Provider + default model + fallback provider + fallback model
- Per-key cache TTL and semantic tolerance
- Whether to bypass cache for benchmarking (`benchmarkMode`)
- Whether to isolate the L2 semantic cache by end-user (`isolateCacheByUser`)
- Whether to persist any telemetry at all (`zeroLog`)
- Per-key monthly budget and current usage (read by the dashboard)

Real provider keys are stored AES-256-GCM-encrypted under a shared `ENCRYPTION_KEY` (32 bytes hex) configured in `.env`. The shared key is read by both the proxy (for decryption at request time) and the dashboard (for encryption at key-creation time) — that's the only thing they share.

**L2 cache isolation**: when `isolateCacheByUser=true` on a key, the L2 vector cache is segmented by the `user` field in the OpenAI payload. Without it, two different end-users of the same virtual key share the L2 cache (typical for an internal tool where one human uses it; you would not want one human's query to leak into another human's session).

**Cache TTL**: per-key, configured in the dashboard at key creation. Default 24h. The L1 cache uses the per-key TTL via Redis `EX` on write.

---

## Record Session

A feature of the hosted dashboard that lets you tag every request made with one of your virtual keys during a window of time, so you can later review the full per-class breakdown for that window.

The flow is server-side: when you click Start on the dashboard, it writes `optitoken:session:vk:<vk>` to Redis with a 24-hour TTL. The proxy checks this on every request and, if present, overrides the per-request `sessionId` for that `RequestLog` row. When you click Stop, the dashboard deletes the key. The agent doesn't have to do anything.

To use it, log in to [optitoken.net](https://optitoken.net), click **Record Session**, run your agent workload (Hermes, Claude Code, raw curl, anything that uses the virtual key), click **Stop**. The full session summary includes L0/L1/L2/L3/LOOP/MISS counts, per-class savings, by-provider / by-model / by-agent breakdowns, and the total cost impact. Every session is saved and revisitable from `Admin → Session History`.

Implementation:
- `proxy/internal/services/auth.go` — `LookupSessionTag(ctx, virtualKey)` reads the Redis key
- `proxy/internal/handlers/proxy.go` — header → Redis fallback chain
- Dashboard route `app/api/sessions/record/route.ts` (start/stop) — closed source

---

## A/B benchmark with an LLM judge

When `benchmarkMode=true` is set on a virtual key, the proxy fires **three upstream calls per user request**:

1. **Control** — the original payload, forwarded as-is to the upstream provider.
2. **Optimized** — the L3-compressed payload, forwarded to the upstream provider.
3. **Judge** — a third call to the same model (or `FORCE_MODEL` if set), with a prompt that says:

   > Compare Response A and Response B. Rate how semantically similar they are from 0 to 100. Return ONLY a valid JSON object with {"score": <integer>, "feedback": "<1 sentence explanation>"}.

The judge parses the response and writes a row to `BenchmarkLog` with:
- `originalPrompt` / `optimizedPrompt` / `originalResponse` / `optimizedResponse` (full bodies)
- `latencyOriginalMs` / `latencyOptimizedMs`
- `promptTokensOrig` / `completionTokensOrig` / `promptTokensOpt` / `completionTokensOpt`
- `aiReliabilityScore` (0-100)
- `aiFeedback` (1 sentence)

**Cost warning**: this triples your upstream token spend. Use it for measuring quality, not for production traffic. The page at `/benchmark` on the dashboard says this in red.

If the judge call fails (network error, malformed JSON, etc.), the score falls back to 95 with `feedback = "Fallback mocked score"`, so a benchmark row is still recorded.

---

## Playground

The dashboard's `/playground` page lets you compare an OptiToken request and a Control request side-by-side in the same UI. It uses the `/api/playground/chat` route which:

- Forwards the user's prompt to the proxy with `X-Optitoken-Session: <playground-session-id>` (so the requests are tagged in the dashboard's Record Session)
- Captures per-message stats: cache level, tokens in/out, cost saved, latency
- Streams the response back as SSE with an extra `event: stats` line right before `data: [DONE]`, so the client can attach the metadata to the right bubble without a second fetch

The playground UI is built from three sub-components:
- `app/playground/components/Artifact.tsx` — code/diff/Markdown rendering
- `app/playground/components/MessageStats.tsx` — per-message cache badges
- `app/playground/components/Sparkline.tsx` — latency/cost mini-chart

---

## Admin panel

The dashboard's `/admin` page is a separate layout (`app/admin/layout.tsx`) with these sub-pages:

| Route | Purpose |
|-------|---------|
| `/admin` | Global dashboard: total requests, total saved, last 24h, hit-rate, recent activity. |
| `/admin/expensive` | **Most Expensive Prompts.** Groups `RequestLog` by `payloadHash` and shows the top-N by `costSaved`. Lets you see "which 20 prompts are eating my budget" with the actual prompt text. |
| `/admin/explorer` | **Request Explorer.** Filterable list of every request. Filters: provider, model, agent, cache level, date range, sessionId. Click a row to see full prompt + response + cache metadata. |
| `/admin/pricing` | **Pricing Coverage.** Detects models that have been used in production but don't have a `ProviderModel` row in Postgres (would silently bill $0). |
| `/admin/users` | User management (SUPERADMIN only). Create, suspend, change role, set monthly budget. |
| `/admin/emails` | List of every email sent by the dashboard (verification, password reset, billing). |
| `/admin/prospects` | Waitlist / leads before registration was opened. |
| `/admin/sessions` | **Session History.** Every recorded session, past and present. Click a row to see the full per-class breakdown (L0/L1/L2/L3/LOOP/MISS badges, savings by class, by provider, by model, by agent, top 10 expensive, cost). |

### Components in the dashboard

- `components/GlobeWrapper.tsx` + `components/TelemetryGlobe.tsx` — a 3D rotating globe (`react-globe.gl`) that plots request origins as arcs and points. Used in the global admin dashboard.
- `components/LiveTelemetryGrouped.tsx` — the main live telemetry view, grouped by agent and session, with a real-time SSE feed from `/api/analytics/stream`.
- `components/AlertRulesPanel.tsx` — the alert-rule builder. Available metric kinds:
  - `panic_rate` — proxy recovered from a panic, currently only `ProxyHandler` wraps. Triggers per minute.
  - `error_rate` — upstream 4xx/5xx rate over the rolling window.
  - `cache_hit_rate` — L1+L2+L3+LOOP hit rate. Triggers when below threshold (default `lt 30%`).
  - `latency_p99` — 99th percentile end-to-end latency, in ms.
  - `cost_per_hour` — total upstream cost in the last hour.
  - `p50_latency`, `p95_latency` — same metric at different percentiles.
  - `tokens_per_hour`, `requests_per_minute`, `cache_miss_rate`
- `components/ExpensivePromptsPanel.tsx` — the `Most Expensive Prompts` page.
- `components/LiveLogConsole.tsx` — live stream of the proxy's `stderr`, via `/api/admin/logs/stream`.
- `components/PricingCoveragePanel.tsx` — the pricing-coverage page.
- `components/PublicStatusCard.tsx` — the public status-page widget (used on `/status`).
- `components/RequestExplorer.tsx` — the request-explorer page.
- `components/ServerHealthCard.tsx` — the server-health card (CPU, RAM, disk, services up/down) on the admin dashboard.
- `components/ParticleBackground.tsx` — animated particle background used on most pages.
- `components/GlobalCommandCenter.tsx` — a Cmd-K command palette for global navigation.

---

## Public status page

The dashboard serves a public status page at `/status`. It shows:
- Real-time service health from `/api/admin/status` (Postgres, Redis, proxy, dashboard)
- Real-time `global_stats` from `/api/public/global-stats` (total requests, total saved, hit rate, since the start of time)
- Recent incidents (TBD)

The endpoint `/api/public/global-stats` is unauthenticated and returns the cached aggregate from Redis. The endpoint is rate-limited to 1 req/sec per IP in production.

---

## Authentication

NextAuth with the `CredentialsProvider` (`dashboard/lib/authOptions.ts`):
- `bcrypt.compare(plaintext, user.passwordHash)` for password verification
- JWT sessions with the secret in `NEXTAUTH_SECRET`
- Email verification via SMTP (configurable in `.env`: `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `SMTP_FROM`)
- Forgot-password / reset-password flow via signed tokens
- Role-based access: `USER` and `SUPERADMIN` (enforced server-side on `/api/admin/*` and `/admin/*`)

---

## Stripe billing

The dashboard integrates Stripe for paid plans. Two routes:
- `POST /api/stripe/checkout` — creates a Stripe Checkout session
- `POST /api/stripe/webhook` — handles Stripe webhooks (subscription created, payment failed, etc.)

This is the only commercial piece in the system. The proxy itself is free.

---

## Quick start (self-host the proxy only)

The proxy is one Go binary. Redis and the ONNX embedder are siblings in `docker-compose.yml`. Postgres is optional (only needed for the dashboard / telemetry).

```bash
git clone https://github.com/dudutti/Optitoken
cd Optitoken
cp .env.example .env  # fill in ENCRYPTION_KEY (32 bytes hex), REDIS_ADDR, etc.
docker compose up -d --build proxy
```

Verify it's up:

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}

curl http://localhost:8080/readyz
# {"status":"ready"}
```

Point your client at `http://localhost:8080/v1` instead of `https://api.openai.com/v1`. The Authorization header is your virtual key (`sk-opti-...`).

A minimal example (Python):

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="sk-opti-YOUR_VIRTUAL_KEY"
)
response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello world!"}]
)
```

For agentic workloads (Hermes, Claude Code), set the SDK to the proxy URL the same way.

---

## Tech stack

| Component | Tech |
|-----------|------|
| Proxy core | Go 1.21 (single binary, CGO_ENABLED=0, ~10 MB stripped) |
| In-memory cache | Redis Stack with RediSearch VSS (L2) and SETNX locks (L0) |
| Persistence | Postgres 15 with Prisma 5.22 |
| L2 embedder | Python service: `paraphrase-multilingual-MiniLM-L12-v2` ONNX, 384-dim, served over HTTP at `/embed` |
| Tokenizer | `tiktoken-go` (`cl100k_base`) for L1 cache key normalization |
| Encryption | `crypto/aes` (AES-256-GCM, 12-byte IV, 16-byte tag) for real-key storage |
| TLS termination | Caddy 2 (auto-HTTPS via Let's Encrypt) |
| Dashboard | Next.js 14, NextAuth, Prisma, Tailwind, react-globe.gl |
| Email | SMTP (configurable) for verification, password reset, alert notifications |
| Billing | Stripe (Checkout + Webhook) for SaaS plans |

The proxy has no third-party Go dependencies beyond `redis/go-redis/v9`, `google/uuid`, and `pkoukk/tiktoken-go`. The Prometheus exporter is hand-written (~250 lines, no `prometheus/client_golang`).

---

## Repository layout

```
Optitoken/
├── proxy/                            Open source (MIT). This is the binary.
│   ├── cmd/server/main.go            Entry point
│   ├── internal/
│   │   ├── handlers/
│   │   │   ├── proxy.go               Main request pipeline (panic recovery, L0/L1/L2/L3, stream, fallback, app errors)
│   │   │   ├── health.go              /healthz, /readyz
│   │   │   ├── models.go              /v1/models
│   │   │   └── purge.go              Cache purge
│   │   ├── optiagent/
│   │   │   ├── engine.go              ProcessRequest — L0 → L1 → L2 → L3, agent detection, L2 disable
│   │   │   ├── compressor.go          L3 rules: CoT pruning, tool output truncation, repeated-tool collapse
│   │   │   ├── compressor_test.go     6 unit tests for idempotence
│   │   │   ├── marshal_deterministic.go   Custom JSON encoder (sorted keys, compact, no HTML escape)
│   │   │   ├── prefix_split.go        Cache-preserving L3 split (Phase 2)
│   │   │   ├── prefix_split_test.go   9 unit tests for the split
│   │   │   ├── compaction_hint.go     Inject "(Earlier tool results may be truncated.)" hint
│   │   │   ├── dedup.go               L0 in-flight coalescing (SETNX + pubsub)
│   │   │   ├── loop_detect.go         Detect N identical calls in T seconds → LOOP cache
│   │   │   └── tool_dedup.go          Tool-call dedup (collapse repeated reads)
│   │   ├── services/
│   │   │   ├── auth.go                Virtual key lookup, AES-GCM decrypt, session tag
│   │   │   └── crypto.go              AES-256-GCM helpers
│   │   ├── utils/
│   │   │   ├── agent_detector.go      User-Agent + system-prompt heuristics
│   │   │   ├── tokens.go              tiktoken wrapper
│   │   │   ├── savings.go             Per-class savings calculation
│   │   │   ├── provider_models.go     Model registry helpers
│   │   │   ├── redactor.go            PII redactor for logs
│   │   │   ├── cache_validation.go
│   │   │   └── field_discoverer.go    Detect response field layout per provider
│   │   ├── workers/
│   │   │   ├── telemetry.go           ConsumeTelemetry — RequestLog writer
│   │   │   ├── benchmark.go           ConsumeBenchmarkWorker — BenchmarkLog writer
│   │   │   ├── stats.go               Global stats aggregator
│   │   │   ├── model_radar.go         New-model detection
│   │   │   ├── field_discoverer.go
│   │   │   └── pricing.go             Provider pricing syncer
│   │   ├── metrics/metrics.go         Hand-written Prometheus text format
│   │   └── db/                        Redis + Postgres pool
│   ├── cache/l2_vector.go              L1 + L2 cache implementation
│   ├── onnx-embedder/                  Python ONNX service
│   ├── seeds/                          SQL seed (default model prices, default admin key)
│   ├── docker-compose.yml
│   ├── Dockerfile
│   └── README.md
│
├── test/                              Open source. Reproducible test data.
│   ├── README.md
│   └── ab_benchmark_2026_06_18/        Hermes workload A/B test
│       ├── data_benchmarklog.csv
│       ├── data_benchmarklog.json
│       ├── data_proxy_log.txt          Includes the cached_tokens: 6550 line
│       └── 01..07 *.sh                 Reproducible shell scripts (no creds in repo)
│
├── README.md                          This file
├── LICENSE
└── .github/                            (CI configs, if any)
```

The hosted dashboard at [optitoken.net](https://optitoken.net) is closed-source SaaS. It is not in this repository.

---

## Limitations and known gaps

We are rigorous about what this project does and does not do.

- **Anthropic and OpenAI are not in our A/B test loop yet.** The MiniMax-M3 benchmark in `test/ab_benchmark_2026_06_18/` is the only real-provider A/B we have. The 99.8% cache hit number is from MiniMax specifically. We expect similar behavior on Anthropic Claude (which exposes `cache_creation_input_tokens` / `cache_read_input_tokens`) and OpenAI (which exposes `cached_tokens`), but the data isn't here yet.
- **Padding to force cache activation is not implemented.** Some providers need 1024+ tokens of prefix to activate their cache. We considered injecting dummy tool calls or filler text, but rejected it: it pollutes the agent's context window. Reserved for an opt-in feature flag.
- **Streaming is not cached.** SSE streams bypass L1/L2/L3 and go straight upstream. By design — partial streams are awkward to cache, and most streaming workloads (chat UX) are inherently one-shot.
- **The L0 Coalesced leader does not share its result with cross-process peers.** Each Go process holds its own in-flight dedup map. Behind a load balancer with N replicas, you can have N copies of the same in-flight request. The single-binary docker-compose setup doesn't have this problem. A distributed lock (Redis SETNX) would be the next step, but not implemented.
- **No Python or Node SDK.** Bring-your-own HTTP client. The proxy is plain OpenAI-compatible HTTP.
- **Multi-region replication is not implemented.** The proxy is stateful (Redis cache, Postgres telemetry). Run a single region, or accept that the cache hit rate will reset across regions.
- **The agent detector is heuristic, not perfect.** It uses User-Agent + system-prompt regex to identify Hermes, Claude Code, etc. An agent that masks its User-Agent as `python-requests` will be misclassified as a chat-direct user, which means L2 caching will be enabled (might corrupt agent state) or disabled (false miss on a non-agent). The error rate in practice is low but not zero.
- **The dashboard is closed-source.** If you self-host, you don't get the dashboard, the playground, the alert rules UI, the session history, the pricing coverage panel, or the model radar. You get `/healthz`, `/readyz`, `/metrics`, and the RequestLog table in Postgres. Anyone is welcome to build their own dashboard from the data; we are not gating the data behind a paywall.
- **No "we handle your Stripe, billing, alerts, and ops" claims.** This is a self-hosted proxy. The hosted SaaS is opt-in.

---

## Test artifacts

The `test/` directory is open source and ships the raw data from a real Hermes workload on 2026-06-18. Five identical requests, the proxy's logs, the BenchmarkLog rows, and the shell scripts to reproduce. No credentials in any of the files — virtual keys are replaced by `${VIRTUAL_KEY}` placeholders.

The validation in `data_proxy_log.txt` line 2 is the single most important data point in the project:

```
"prompt_tokens": 6564, "completion_tokens": 2, "prompt_tokens_details": { "cached_tokens": 6550 }
```

That 6550 is the provider saying "I served 99.8% of your prompt from cache". Without the cache-preserving L3 split, that number would be 0. The split is the load-bearing piece of the project.

---

## — Version française —

OptiToken est un proxy LLM open source qui transforme le trafic de vos agents en un flux mesurable et optimisable.

**Le problème.** Les agents répètent. Un agent qui parcourt un codebase va relire les mêmes fichiers, refaire les mêmes tool calls, et réémettre les mêmes blocs `<thought>` pendant des centaines de tours. Un proxy naïf relaie tout au provider, qui facture tout. OptiToken fait trois choses en un seul binaire : un cache à quatre niveaux (L0 dédup en vol → L1 exact → L2 sémantique → L3 compression), une télémétrie par requête, et un détecteur d'agent qui désactive le cache quand l'agent est en multi-tour (sans ça, on renverrait des réponses d'un tour précédent et on corromprait le contexte).

**Transparent.** Branché devant n'importe quel endpoint OpenAI-compatible, sans changer le code de l'app cliente. Virtual keys (`sk-opti-...`) qui résolvent via Redis vers le vrai provider key, chiffré AES-256-GCM au repos.

**Cache-Preserving L3.** La subtilité : les providers (Anthropic, OpenAI, MiniMax) ont un cache provider côté serveur, basé sur le hash byte-exact du préfixe. Si un compresseur naïf ré-encode tout le payload, c'est cache miss → 5× plus cher que sans compression. On a mesuré cet échec en conditions réelles le 2026-06-18. Le fix est en trois phases : encodeur JSON déterministe (sortie byte-exact pour input byte-exact), split préfixe/queue byte-exact (le préfixe n'est jamais touché, seule la queue est compressée), et enveloppe synthétique pour faire passer la queue dans le compresseur standard. Validation : 6 550 / 6 564 prompt tokens (99.8%) servis depuis le cache provider.

**Telemetry + Record Session + Benchmark.** Chaque requête log dans Postgres (cacheLevel, tokens, savings par classe, agent, payloadHash, sessionId). Le mode benchmark tire 3 requêtes upstream (control + opt + judge LLM) et score la similarité sur 0-100. Record Session est server-side : tu cliques Start, le dashboard écrit une clé Redis, le proxy tag automatiquement, tu cliques Stop, la clé disparaît. L'agent n'a rien à faire.

**Tech stack.** Go 1.21, Redis Stack avec RediSearch VSS, Postgres 15, ONNX (multilingual MiniLM), Caddy, Next.js 14 + Prisma. Prometheus à `/metrics`, hand-written (pas de `client_golang`). Le proxy est sous 250 lignes de dépendances tierces.

**Honnête.** On ne prétend pas avoir benchmarké Anthropic ou OpenAI (seul MiniMax-M3 dans `test/`). On ne prétend pas certifier quoi que ce soit (GDPR, EU residency) — c'est un choix de déploiement, pas une feature. On ne prétend pas avoir optimisé pour le multi-région. Le dashboard est closed-source SaaS, et si tu te self-hostes, tu obtiens les endpoints `/healthz`, `/readyz`, `/metrics` et la table `RequestLog` en Postgres, pas l'UI.

**MIT** pour le proxy. Le dashboard est commercial.

---

## License

MIT. The proxy is open source. The hosted dashboard at [optitoken.net](https://optitoken.net) is a separate commercial product.
