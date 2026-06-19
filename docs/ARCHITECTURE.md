# Synapse Proxy â€” Architecture

This document is the technical reference for how Synapse Proxy is built. It mirrors the structure of the source code so a new contributor can navigate from here to `proxy/internal/...` and find what they're looking for.

This is the system that runs in production. Every section below is sourced from a real file. Where a feature is **not** implemented, the Limitations section at the bottom of `README.md` says so explicitly.

## 1. High-level topology

```mermaid
graph TB
    subgraph ClientSide[" "]
        Agent["Agent / SDK / curl (Hermes, Claude Code, LangChain, ...)"]
    end

    subgraph Edge["Edge layer"]
        Caddy["Caddy 2 :80 / :443 - TLS, auto-HTTPS"]
    end

    subgraph ProxyHost["Synapse Proxy host"]
        Proxy["Go binary (Synapse Proxy-server) :8080 - single binary, CGO_ENABLED=0"]
        ONNX["Python ONNX service :8000 - multilingual-MiniLM-L12-v2 384-dim embeddings"]
    end

    subgraph DataLayer["State"]
        Redis[("Redis Stack - VSS index idx:l2cache, L0 locks, L1 keys, L2 vectors, session tags, model radar")]
        Postgres[("PostgreSQL 15 - User, ApiKey, RequestLog, BenchmarkLog, AlertRule, AlertEvent, ProviderModel")]
    end

    subgraph Upstream["Upstream providers"]
        OpenAI["OpenAI / Anthropic / MiniMax / any OpenAI-compatible"]
    end

    subgraph SaaS["Hosted dashboard (Next.js - Open Source)"]
        Dashboard["Next.js 14 + Prisma - synapse-proxy.com"]
    end

    Agent -->|HTTPS + Bearer sk-opti-...| Caddy
    Caddy -->|HTTP /v1/...| Proxy
    Proxy -.->|/embed (HTTP)| ONNX
    Proxy <-->|HGETALL / SETNX / XADD / FT.SEARCH| Redis
    Proxy <-->|INSERT / SELECT| Postgres
    Proxy -->|HTTPS| OpenAI
    Dashboard <-->|Prisma| Postgres
    Dashboard <-->|LRange / GET / DEL| Redis
    Agent -.->|HTTPS (synapse-proxy.com)| Dashboard
```

The proxy is the only hot path. The dashboard, Postgres, Redis, and the ONNX service are all supporting infrastructure. The proxy can be deployed standalone (no dashboard) â€” it then exposes `/healthz`, `/readyz`, `/metrics`, and persists telemetry to Postgres if available.

## 2. The proxy

Source: `proxy/cmd/server/main.go`, `proxy/internal/handlers/`, `proxy/internal/optiagent/`, `proxy/internal/workers/`.

### 2.1 Request lifecycle

`handlers/proxy.go:ProxyHandler` is the single entry point. Pseudocode:

```
on request:
    recover() panics â†’ log + 502
    extract Authorization â†’ virtual key
    ValidateVirtualKey(ctx, "Bearer " + vk):
        HGETALL Synapse Proxy:keys:<vk>  (Redis, 1 RTT)
        parse real_key, provider, fallback, benchmark_mode,
               semantic_tolerance, cache_ttl, default_model,
               isolate_cache_by_user, zero_log
        decrypt real_key with AES-256-GCM (32-byte shared key)
    read body
    if zero_log:  zeroize the in-memory copy of real_key after use
    if X-Synapse Proxy-Session header or Redis session tag:
        set sessionID
    if X-Bypass-Cache: isBypass = true
    detectUpstreamAppError on any 200-with-error body â†’ 402
```

`Zero-Log Mode` is the only privacy guarantee: when set, no `originalPayload`, `optimizedPayload`, `responsePayload`, `originalPrompt`, or `optimizedResponse` is ever persisted.

### 2.2 The cache pipeline

The request flows through L0 â†’ L1 â†’ L2 â†’ L3. Each layer can either answer the request (cache hit) or pass through to the next layer.

#### 2.2.1 L0 â€” In-flight coalescing

`optiagent/dedup.go`.

- Compute `SHA-256(normalize(payload))` â†’ `payloadHash`
- `SETNX Synapse Proxy:l0:lock:<vk>:<sha> <workerID> EX 30` returns `ok=true` for the first request, `ok=false` for subsequent concurrent requests with the same hash
- **Leader** (ok=true): processes normally, when done it `SET Synapse Proxy:l0:resp:<vk>:<sha> <json> EX 30` then releases the lock with a Lua CAS-delete
- **Followers** (ok=false): poll the response key every 50ms (capped at 30s), return the response tagged `cacheLevel=L0`
- L0 is **skipped for streaming** requests â€” the client already started receiving bytes
- Followers never reach the upstream, so `promptTokensOrig=0` in the telemetry row

#### 2.2.2 L1 â€” Exact SHA-256 match

`optiagent/engine.go:131`, `cache/l2_vector.go:CheckL1Cache`.

- Key: `synapse:l1cache:<vk>:<sha>` (or `<vk>:<user>:<sha>` when `isolateCacheByUser=true`)
- `GET` returns the cached body in <2ms
- TTL: per-key `cache_ttl` (default 86400s = 24h)
- Hit tagged `cacheLevel=L1`, no upstream call
- **TTL Validation**: Prior to returning a hit, `optiagent/engine.go` calls `ShouldReuseCache` to evaluate the cache entry's age against custom tool-specific TTL configurations (configured in the dashboard per key, stored as JSON in Redis `tool_ttls` field). A TTL of `0` disables caching for that tool name. If no custom TTL is configured, it falls back to default rules: infinite lifespan for read-only tools, and a maximum age of `60` seconds for stateful tools.

#### 2.2.3 L2 â€” Semantic vector search

`cache/l2_vector.go:CheckL2Cache`, `optiagent/engine.go:151`.

- The last user message is extracted (`optiagent/engine.go:extractTextForEmbedding`)
- A 384-dim vector is computed by the local ONNX service (`/embed`)
- `FT.SEARCH idx:l2cache '*' PARAMS 2 query_vec <bytes> RETURN 2 score response DIALECT 2` â€” k-NN on the Redis VSS index
- The cosine-similarity score must beat `semantic_tolerance` (per-key, default 0.15)
- The Redis VSS index is tagged by `(vk, user?)`, so per-tenant isolation is automatic
- **Auto-disabled** when the request looks like part of a multi-turn conversation (`nonSystemCount > 1`) or contains an image. Two consecutive agent turns have near-identical embeddings â€” returning a cached response from a *different* turn would corrupt the conversation state
- Hit tagged `cacheLevel=L2`
- **TTL Validation**: Like L1, the proxy validates the hit response age against granular tool TTL rules using `ShouldReuseCache` before returning the L2 match.

The Redis index is created at proxy startup if it does not exist:

```
FT.CREATE idx:l2cache ON HASH PREFIX 1 Synapse Proxy:l2cache:
  SCHEMA
    vk       TAG
    user     TAG
    vector   VECTOR FLAT 6 DIM 384 DISTANCE_METRIC COSINE
    response TEXT
    ts       NUMERIC
```

#### 2.2.4 L3 â€” Cache-preserving payload compression

`optiagent/compressor.go`, `optiagent/prefix_split.go`, `optiagent/marshal_deterministic.go`, `optiagent/compaction_hint.go`.

L3 is where the interesting work happens. The naive version (re-encode the whole payload) breaks the provider's byte-exact cache. Ours does not. Three pieces:

**Phase 1 â€” Idempotent encoder** (`marshal_deterministic.go`). The JSON encoder sorts keys alphabetically, emits no whitespace, disables HTML escaping, and uses deterministic float formatting. Same input â†’ same bytes, always. 6 unit tests in `compressor_test.go` lock this in.

**Phase 2 â€” Prefix-preserving split** (`prefix_split.go`). Before compressing, we walk the JSON byte by byte, find the messages array, count the top-level message elements, and locate the start of the **4th-from-last message**. Everything before that is the **static prefix**; the last 4 messages are the **dynamic tail**. The prefix is returned byte-exact. The tail is what gets compressed. 9 unit tests in `prefix_split_test.go`.

**Phase 3 â€” Tail compression** (`compressor.go`). The tail is wrapped in a synthetic `{"messages":[<tail>]}`, run through the standard L3 rules (see below), unwrapped, and re-attached to the byte-exact prefix. The output is a valid JSON document where the first N bytes are byte-exact identical to the input.

L3 rules (in order, applied to non-recent assistant messages only):
1. **CoT pruning**: `<thought>â€¦</thought>` blocks â†’ `[Pruned Thought Process]`. Hermes-style block tags only.
2. **`reasoning_content` stripping**: removed entirely on old assistant turns (DeepSeek-R1, Qwen QwQ, MiniMax thinking).
3. **Tool output truncation**: results >200 chars â†’ first 200 + `[â€¦truncated by Synapse Proxy L3â€¦]`. The marker is plain text, not a synthetic stub, so the agent's safety filter doesn't reject it.
4. **Repeated tool-call collapsing**: 3rd+ identical `name + arguments` invocation â†’ `[compacted_repeated_tool]`.
5. **Compaction hint** (`compaction_hint.go`): prepend `(Earlier tool results in this transcript may be truncated.)` to the system prompt so the LLM is not surprised.

The compressed payload is only used if it actually shrinks in **both bytes and tokens**. Otherwise the original is sent untouched (no re-encoding inflation).

#### 2.2.5 Semantic Tool Deduplication

`optiagent/tool_dedup.go`, `handlers/proxy.go`.

This mechanism intercepts tool execution requests from the agent and bypasses client-side tool runs if cached results exist:
- **Cache Storage**: The proxy captures completed tool outputs from incoming request payloads (turns where `role` is `tool` or `function` matching preceding assistant `tool_calls` by ID) and persists them in the Redis `idx:toolcache` search index along with an ONNX-computed embedding of their arguments.
- **Cache Retrieval**: When the LLM outputs a response containing tool calls, the proxy intercepts it before returning it to the client. It queries `idx:toolcache` using exact matching on the arguments and a VSS search (cosine similarity >90%).
- **Recursive Resolution**: If cached tool responses are found for all tool calls in the response, the proxy appends the cached results to the message sequence and recursively invokes the upstream LLM. It repeats this resolution loop until the LLM returns a final text completion, transparently bypassing client-side tool execution.

### 2.3 Fallback routing

`optiagent/engine.go` (and the `defaultModel` / `fallbackProvider` / `fallbackKey` config).

If the upstream returns 429, 500, 502, 503, 504, or 408, the proxy retries once with exponential backoff. If that fails, it transparently fails over to the user's configured fallback provider and key, re-running the L3 pipeline on the new payload.

### 2.4 Streaming

`handlers/proxy.go:streamResponse`. SSE output is forwarded byte-for-byte. Three LLM-judge-style "X-Synapse Proxy-*" headers are added to the response:

- `X-Synapse Proxy-Cache` â€” the level that answered (L0/L1/L2/L3/BYPASS)
- `X-Synapse Proxy-Tokens-In` / `X-Synapse Proxy-Tokens-Out` â€” what the upstream billed
- `X-Synapse Proxy-Cost-Saved` / `X-Synapse Proxy-Cost-Without` / `X-Synapse Proxy-Cost-With`

When a downstream caller cares about the response shape (e.g. the playground), the headers are enough â€” no second fetch needed.

The dashboard's `/api/playground/chat` route (`dashboard/app/api/playground/chat/route.ts`) wraps the SSE stream in a `TransformStream` that injects an `event: stats` line right before the final `data: [DONE]`, so the client attaches the per-message metadata to the right bubble.

### 2.5 Upstream app-error detection

`handlers/proxy.go:detectUpstreamAppError`. Some providers (notably MiniMax) return HTTP 200 with an application-level error in the body:

```json
{ "base_resp": { "status_code": 2056, "status_msg": "quota exhausted" } }
```

`detectUpstreamAppError` parses both this and the OpenAI-style `{ "error": { "message": ..., "type": ... } }` format. If the message contains any of `quota`, `credit`, `usage limit`, `billing`, `insufficient`, `payment`, `exhausted`, the proxy returns **HTTP 402 Payment Required** instead of forwarding a 200 with a poison body that hangs the agent.

### 2.6 Workers

Three background goroutines started from `cmd/server/main.go`:

- **`telemetry.go` ConsumeTelemetry** â€” reads from the `synapse:telemetry:logs` Redis stream group `telemetry_group`, writes one row to `RequestLog` per event, ACKs.
- **`benchmark.go` ConsumeBenchmarkWorker** â€” same pattern for `synapse:benchmark_logs` / `benchmark_group`, writes `BenchmarkLog` rows.
- **`model_radar.go` workers** â€” detect new models, run the field discoverer, sync provider pricing from Postgres into Redis.

All three fail open: if Postgres is down, the worker logs and keeps going. The hot request path is never blocked on telemetry.

### 2.7 Virtual keys and encryption

`internal/services/auth.go`, `internal/services/crypto.go`.

- Virtual keys are `sk-opti-...` strings, validated by Redis `HGETALL Synapse Proxy:keys:<vk>`
- Real provider keys are stored AES-256-GCM encrypted (12-byte random IV, 16-byte auth tag) in the same hash, under the field `real_key`
- Decryption happens once at the top of `ProxyHandler` and is dropped from memory after the request returns (Go's GC handles the rest)
- A `DEFAULT_VIRTUAL_KEY` env var lets local apps (LMStudio, raw curl) hit the proxy without sending auth, mimicking OpenAI's "no auth" behavior

### 2.8 Record Session (server-side)

`internal/services/auth.go:LookupSessionTag`.

- The dashboard's `POST /api/sessions/record { enable: true, sessionId }` writes `synapse:session:vk:<vk> = <sessionId>` to Redis with `EX 86400` (24h TTL safety net)
- The proxy checks this key on every request â€” if present, it overrides the per-request `sessionId` for that `RequestLog` row
- `POST /api/sessions/record { enable: false, sessionId }` deletes the key on Stop
- No client-side coordination is needed â€” Hermes, Claude Code, raw curl, anything using the virtual key is recorded transparently

## 3. A/B benchmark with an LLM judge

`handlers/proxy.go:runBenchmarkEvaluation`, `workers/benchmark.go:ConsumeBenchmarkWorker`.

When `benchmarkMode=true` is set on a virtual key, the proxy fires **three upstream calls per user request**:

1. **Control** â€” the original payload, forwarded as-is.
2. **Optimized** â€” the L3-compressed payload.
3. **Judge** â€” a third call to the same model (or `FORCE_MODEL` if set), with this prompt:

```
Compare Response A and Response B. Rate how semantically similar
they are from 0 to 100. Return ONLY a valid JSON object with
{"score": <integer>, "feedback": "<1 sentence explanation>"}.
```

The judge parses the response and writes a `BenchmarkLog` row with:
- `originalPrompt` / `optimizedPrompt` (full bodies)
- `originalResponse` / `optimizedResponse` (full bodies)
- `latencyOriginalMs` / `latencyOptimizedMs`
- `promptTokensOrig` / `completionTokensOrig` / `promptTokensOpt` / `completionTokensOpt`
- `aiReliabilityScore` (0-100)
- `aiFeedback` (1 sentence)

**Cost warning**: this triples your upstream token spend. Use it for measuring quality, not for production traffic. The dashboard's `/benchmark` page says this in red.

If the judge call fails (network error, malformed JSON, etc.), the score falls back to **95** with `feedback = "Fallback mocked score"`, so a benchmark row is still recorded.

## 4. The dashboard

Located in `./dashboard` in this repository. The dashboard is a Next.js application that provides the visual control plane and analytics portal for the proxy gateway. It reads and writes the Postgres database and mirrors configurations to Redis in real time.

### 4.1 Pages

| Route | Purpose |
|-------|---------|
| `/` | Landing + live telemetry + record session control |
| `/playground` | Side-by-side A/B chat with the proxy |
| `/benchmark` | Browse `BenchmarkLog` rows, side-by-side comparison |
| `/keys` | Manage virtual keys |
| `/admin` | Global admin dashboard |
| `/admin/expensive` | Most Expensive Prompts (grouped by `payloadHash`) |
| `/admin/explorer` | Request Explorer with filters |
| `/admin/pricing` | Pricing coverage â€” models used in prod without a `ProviderModel` row |
| `/admin/sessions` | Session history drilldown |
| `/admin/users` | User management (SUPERADMIN only) |
| `/admin/emails` | Email send log |
| `/admin/prospects` | Waitlist / leads |
| `/settings` | Profile, password, API key, billing |
| `/status` | Public status page |

### 4.2 Components (each is a real `.tsx` file in `dashboard/components/`)

- `LiveTelemetryGrouped.tsx` â€” main live feed (SSE from `/api/analytics/stream`)
- `TelemetryGlobe.tsx` + `GlobeWrapper.tsx` â€” 3D rotating globe (`react-globe.gl`)
- `GlobalCommandCenter.tsx` â€” Cmd-K command palette
- `AlertRulesPanel.tsx` â€” alert builder with 9 metric kinds
- `ExpensivePromptsPanel.tsx` â€” top-N prompts by cost
- `LiveLogConsole.tsx` â€” live stream of proxy `stderr`
- `PricingCoveragePanel.tsx` â€” pricing coverage
- `PublicStatusCard.tsx` â€” public status widget
- `RequestExplorer.tsx` â€” filterable request list
- `ServerHealthCard.tsx` â€” CPU/RAM/disk/services
- `ParticleBackground.tsx` â€” animated background

### 4.3 API surface (closed-source)

`app/api/auth/[...nextauth]/route.ts` â€” NextAuth credentials provider
`app/api/auth/register/route.ts` â€” registration + email verification
`app/api/keys/route.ts` â€” virtual key CRUD
`app/api/keys/[id]/route.ts` â€” virtual key update/delete
`app/api/keys/[id]/session-benchmark/route.ts` â€” toggle benchmark mode per key
`app/api/playground/chat/route.ts` â€” SSE proxy to Go data plane + `event: stats`
`app/api/analytics/route.ts` â€” aggregate stats
`app/api/analytics/session/route.ts` â€” single-session detail
`app/api/analytics/sessions/route.ts` â€” list all sessions
`app/api/analytics/stream/route.ts` â€” SSE stream of new requests
`app/api/analytics/export/route.ts` â€” CSV export
`app/api/telemetry/[id]/route.ts` â€” single request detail
`app/api/telemetry/[id]/payload/route.ts` â€” full prompt/response body
`app/api/admin/alerts/route.ts` + `[id]/route.ts` â€” alert rule CRUD
`app/api/admin/alerts/events/route.ts` â€” fired events log
`app/api/admin/expensive-prompts/route.ts` â€” top-N by cost
`app/api/admin/explorer/route.ts` + `[id]/route.ts` â€” request list + detail
`app/api/admin/logs/stream/route.ts` â€” proxy log stream
`app/api/admin/model-radar/route.ts` â€” new model detection
`app/api/admin/models/route.ts` â€” known models list
`app/api/admin/pricing-coverage/route.ts` â€” coverage report
`app/api/admin/status/route.ts` â€” service health
`app/api/admin/telemetry/route.ts` â€” admin telemetry view
`app/api/admin/emails/route.ts` â€” email log
`app/api/sessions/record/route.ts` â€” start/stop a Record Session
`app/api/cache/purge/route.ts` â€” clear all caches for a key
`app/api/stripe/checkout/route.ts` + `webhook/route.ts` â€” billing
`app/api/public/global-stats/route.ts` â€” public stats, unauthenticated
`app/api/waitlist/route.ts` â€” pre-registration leads
`app/api/models/route.ts` â€” public list of models the proxy can serve
`app/api/config/route.ts` â€” public config (feature flags, version)
`app/api/auth/forgot-password/route.ts` + `reset-password/route.ts` + `verify-email/route.ts` â€” credential recovery flow

## 5. Storage layer

### 5.1 Redis

| Key | TTL | Purpose |
|-----|-----|---------|
| `synapse:keys:<vk>` | none | Per-virtual-key config: real_key (encrypted), provider, fallback, semantic_tolerance, cache_ttl, default_model, isolate_cache_by_user, zero_log, benchmark_mode, monthly_budget, current_usage |
| `synapse:l0:lock:<vk>:<sha>` | 30s | L0 in-flight dedup lock (SETNX with worker UUID) |
| `synapse:l0:resp:<vk>:<sha>` | 30s | L0 follower poll target |
| `synapse:l1cache:<vk>:<sha>` | per-key cache_ttl | L1 exact match, JSON body |
| `synapse:l2cache:<vk>:<user?>:<sha>` | per-key cache_ttl | L2 semantic match, vector + response |
| `synapse:session:vk:<vk>` | 24h (TTL safety) | Record Session tag |
| `synapse:modelradar:<provider>:<model>` | 30d | Model radar entry, status: learning/known/mapped |
| `synapse:global_stats` | none | Cached aggregate (used by `/api/public/global-stats`) |
| `synapse:telemetry:logs` (stream) | trim by MAXLEN | Telemetry events waiting for the worker to consume |
| `synapse:benchmark_logs` (stream) | trim by MAXLEN | Benchmark events waiting for the worker to consume |

### 5.2 Postgres

Schema lives in `dashboard/prisma/schema.prisma`. Migrations in `dashboard/prisma/migrations/`.

- `User` â€” credentials (bcrypt-hashed password), role, Stripe customer
- `ApiKey` â€” virtual key + encrypted real key + per-key config
- `RequestLog` â€” one row per request, every metric we persist (see README "Telemetry" section)
- `BenchmarkLog` â€” one row per A/B pair, with full prompt/response bodies
- `AlertRule` â€” user-defined alert thresholds
- `AlertEvent` â€” fired alerts history
- `ProviderModel` â€” per-provider pricing table, refreshed hourly by the worker

Indexes: `RequestLog(apiKeyId, createdAt DESC)`, `RequestLog(agentId)`, `RequestLog(sessionId)`, `RequestLog(payloadHash)`, `RequestLog(agentId, createdAt DESC)`, `BenchmarkLog(apiKeyId, createdAt DESC)`.

## 6. Observability

| Endpoint | Format | Purpose |
|----------|--------|---------|
| `GET /healthz` | JSON | Liveness. 200 if Go process is alive. |
| `GET /readyz` | JSON | Readiness. Pings Postgres + Redis. 200 only if both respond. |
| `GET /metrics` | Prometheus text | Cache hit/miss counters per level, panic counters per handler, request totals. Hand-written (`internal/metrics/metrics.go`), no `prometheus/client_golang` dependency. |

Live counters in `/metrics`:
- `Synapse Proxy_panics_total{handler="ProxyHandler"}` â€” panic recoveries since process start
- `Synapse Proxy_cache_hits_total{level="L0|L1|L2|L3|LOOP"}` â€” cumulative cache hits per level
- `Synapse Proxy_upstream_requests_total` â€” total requests forwarded to upstream
- `Synapse Proxy_upstream_errors_total` â€” 4xx/5xx from upstream

## 7. Tech stack

| Component | Tech | Where |
|-----------|------|-------|
| Proxy core | Go 1.21, single binary, CGO_ENABLED=0 | `proxy/cmd/server/` |
| L2 embedder | Python FastAPI + ONNX Runtime + `paraphrase-multilingual-MiniLM-L12-v2` | `proxy/onnx-embedder/` |
| Tokenizer | `pkoukk/tiktoken-go` (`cl100k_base`) | `proxy/internal/utils/tokens.go` |
| Encryption | `crypto/aes` (AES-256-GCM, 12-byte IV, 16-byte tag) | `proxy/internal/services/crypto.go` |
| L1/L2 cache | `redis/go-redis/v9` + `FT.SEARCH` (RediSearch VSS) | `proxy/cache/l2_vector.go` |
| Persistence | Postgres 15 + Prisma 5.22 (dashboard) | `dashboard/prisma/` |
| TLS | Caddy 2 (auto-HTTPS via Let's Encrypt) | `docker-compose.yml` |
| Dashboard | Next.js 14 + NextAuth + Tailwind + react-globe.gl | `dashboard/app/`, `dashboard/components/` |
| Email | SMTP (Nodemailer client) | `dashboard/lib/email.ts` |
| Billing | Stripe (Checkout + Webhook) | `dashboard/app/api/stripe/` |

Go third-party deps are minimal: `redis/go-redis/v9`, `google/uuid`, `pkoukk/tiktoken-go`. The Prometheus exporter is hand-written in ~250 lines.

## 8. Deployment

See `docs/DEPLOYMENT.md`. Quick reference:

- **Dev / self-host:** `docker compose up -d --build proxy`
- **Production (Hetzner):** see `DEPLOYMENT_HETZNER.md` for the custom Redis entrypoint (`redis-start.sh`) and the docker-compose production overrides
- **TLS:** Caddy 2 with auto-HTTPS; the production stack exposes `:80` and `:443` only
- **Backups:** `postgres_data` and `redis_data` are Docker named volumes; snapshot strategy is left to the operator
- **Health probe:** `/healthz` for liveness, `/readyz` for readiness, both used by Docker restart policy in production

## 9. See also

- `README.md` â€” high-level overview, limitations, quick start
- `docs/DEPLOYMENT.md` â€” deployment guide
- `docs/TELEMETRY.md` â€” telemetry schema reference
- `docs/USER_GUIDE.md` â€” end-user guide for the dashboard
- `test/README.md` and `test/ab_benchmark_2026_06_18/` â€” reproducible benchmark data validating the cache-preserving L3 design
