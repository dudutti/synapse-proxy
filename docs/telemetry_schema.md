# Synapse Proxy Telemetry & Database Schema

Every request intercepted by Synapse Proxy creates a row in the `RequestLog` table in PostgreSQL, turning your LLM traffic into a highly measurable flow.

## RequestLog Schema

| Column | Meaning |
|--------|---------|
| `cacheLevel` | `MISS`, `L0`, `L1`, `L2`, `L3`, `LOOP`, `BYPASS` |
| `promptTokensOrig` / `promptTokensOpt` | Token counts measured by the upstream |
| `completionTokensOrig` / `completionTokensOpt` | Same for completions |
| `savingsInputFresh` / `savingsCacheRead` / `savingsCacheCreation` / `savingsOutput` | Per-class dollar savings, computed against the `ProviderModel` table |
| `cacheCreationTokens` / `cacheReadTokens` / `cacheHitTokens` / `cacheMissTokens` | Read from the upstream response (`prompt_tokens_details.cached_tokens` for OpenAI, `cache_creation_input_tokens` and `cache_read_input_tokens` for Anthropic). 0 if the upstream doesn't expose them. |
| `durationMs` | Wall-clock end-to-end |
| `agentId` / `agentLabel` | From `proxy/internal/utils/agent_detector.go` — User-Agent + system-prompt heuristics. Resolved labels: `Hermes`, `Claude Code`, `OpenClaw`, `LangChain`, `chat-direct`, `tool-using-agent`, `curl`, `python-requests`, `node-fetch`, etc. |
| `sessionId` | Set by Record Session to group related interactions in Session Replay. |
| `payloadHash` | SHA-256 of the original payload — used to group identical prompts. |
| `originalPayload` / `optimizedPayload` / `responsePayload` | The actual JSON request and response payloads, stored unless `zeroLog=true` on the API key. |
| `toolCalls` | JSON array of detected tool calls to build the Agent Flow timeline. |
| `isSimulated` | Indicates if the Free Tier or Session limit was exceeded, tracking "potential" savings. |
| `killSwitchFired` | True if the request tripped the Agent Loop Kill Switch (HTTP 400). |

## Core Tables & Indexes

The DB schema is documented in `dashboard/prisma/schema.prisma` and the migration history under `prisma/migrations/`. 
The relevant tables are `User`, `ApiKey`, `RequestLog`, `BenchmarkLog`, `ProviderModel`. 

Indexes exist on:
- `RequestLog(apiKeyId, createdAt DESC)`
- `RequestLog(agentId)`
- `RequestLog(sessionId)`
- `RequestLog(payloadHash)`
- `RequestLog(agentId, createdAt DESC)`

There is also a model radar (`internal/workers/model_radar.go`) that auto-discovers new models the proxy sees for the first time and stores them in Redis with status `learning` -> `known` -> `mapped`. A field discoverer (`internal/workers/field_discoverer.go`) maps the upstream's response shape to the right `usage` fields per provider.

## Prometheus Metrics

Synapse Proxy exposes an endpoint at `/metrics` conforming to the Prometheus text format. It tracks:
- Panic counters per handler (ensuring robust monitoring)
- Cache-hit counters per level (L1/L2/L3)
- Loop blocks (Kill Switch triggers)
- Overall request totals. 

The metrics endpoint is hand-written in Go without the heavy `prometheus/client_golang` dependency.
