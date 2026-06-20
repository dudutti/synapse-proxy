# Changelog



All notable changes to **Synapse Proxy** are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



> **Open Source Project**: Both **Synapse Proxy** and its **Dashboard** are now fully open-source. While you are free to self-host and customize them for your own team or personal use, we politely ask that you do not sell, resell, or commercialize this product under a competing paid subscription model.


## [Unreleased]



### Added

- **SaaS Stripe Billing & Subscription Tiers**: Implemented dynamic pricing maps (Hobby Free 10M tokens, Pro €5/mo 20M tokens, Scale €15/mo 100M tokens) with Stripe checkout flow and robust webhook syncing (`checkout.session.completed`, `customer.subscription.updated`, `customer.subscription.deleted`).

- **Real-Time Proxy Token Tracking**: Telemetry worker now increments users' monthly token consumption in Postgres on each request log, dynamically toggling the `limit_exceeded` status in Redis when tier limits are breached.

- **Superadmin Plans CRUD & User Override Panels**: Created `/admin/plans` mapping dashboard and upgraded `/admin/users` to allow superadmins to manually change user tiers and reset token limits.

- **Dynamic Billing Settings UI**: Upgraded `/settings` to display live plans from the database, track monthly token quotas with an interactive progress bar, and trigger checkout upgrades.

- **Native CGO Go/Rust Embedding Engine**: Statically linked a Rust-based MiniLM embedder via CGO, replacing the external Python `onnx-embedder` API to eliminate inter-container latency and save ~1GB of server memory.

- **Sandbox Session Forking**: Added a "Fork in Playground" capability to the Request Explorer drawer to clone any conversation context directly into the Playground for debugging.

- **Unicode Encoding Standardisation**: Standardised unicode characters as JSX escapes (`\u...`) across 17 dashboard files to fix encoding issues (`???`, encoding bugs).

- Live telemetry widget on the dashboard header now auto-refreshes every 5s with smooth tween animation. The "Total Value Saved" figure and "Tokens Sent / Saved" counters move in real time as new `RequestLog` rows are written.

- Compaction hint injection at the start of the system message so the agent knows previous tool outputs may be summarized. See `proxy/optiagent/compaction_hint.go`.

- **Playground v3** (`/playground`) "” side-by-side A/B chat upgraded from a basic latency comparison into a full analytics workbench:

  - **Per-bubble stats footer**: colored cache-level badge (`L0` cyan, `L1` blue, `L2` emerald, `L3` purple, `LOOP` amber, `MISS` zinc) + inline token counters (`in` / `out`), `latency (ms)`, and `$ saved` vs. the direct path.

  - **3-up A vs B comparison bar** above the chat panels: `cost saved %`, `latency delta`, `token delta` "” emerald when Synapse Proxy wins, amber when it loses, zinc on a tie.

  - **SVG sparklines strip**: inline polyline charts for Opti latency, Direct latency, and cumulative `$ saved` over the last 50 messages. Zero JS chart library, pure inline `<svg>`.

  - **Artifact Renderer** (`components/Artifact.tsx`): auto-detects ```` ```html ````, ```` ```python ````, ```` ```js ````, etc. in the assistant reply. HTML artifacts render live in a sandboxed `<iframe sandbox>` (no `allow-same-origin`) with `Copy` / `Open` (Blob URL, works cross-browser) / `Download` / `Source` toggle. Code artifacts get `Copy` + `Download` with language tag from the markdown fence.

  - **Linked / Independent panels**: by default both panels share the same (key, model) for true A/B. Click `Unlink` in the header to give each panel its own "” compare two providers side by side.

  - **Export session as JSON**: one click downloads `{settings, messages, sparklines, stats}`. Paste into a PR, share with a teammate, diff two sessions.

- **L0 in-flight request deduplication** (`proxy/optiagent/dedup.go` + `proxy.go`). Two identical requests arriving at the proxy at the same time (same SHA-256 payload + same virtual key) are collapsed into a single upstream call. The first acquires a Redis `SETNX` lock with 30s TTL and processes normally; the second blocks and polls the response key every 50ms. Lock release is atomic via a Lua script that only deletes if the value matches the worker's UUID. Followers are tagged `cacheLevel=L0` in telemetry. Skipped for streaming (the client already started receiving).

- **AES-256-GCM encryption at rest** for real provider API keys (`proxy/internal/services/crypto.go`, `dashboard/app/api/{keys,models}/route.ts`). Keys are encrypted before being written to **both** PostgreSQL and Redis, using a shared `ENCRYPTION_KEY` (32-byte hex) configured via `.env`. The proxy decrypts at request time, in-memory. Legacy plaintext keys seeded in Redis before encryption was enabled remain readable via the GCM-open-failure fallback in `services.DecryptRealKey`.

- **`InitPricingSyncer` boot worker** (`proxy/cmd/server/main.go`). Loads the `ProviderModel` table into an in-memory cache refreshed every hour. Eliminates the silent `$1/MTok` fallback that was applied to every cost-saving calculation since v1.4.0. Log line on boot: `PricingSyncer: Successfully synced 46 models from database.`

- Proxy now exposes custom headers on every response for the Playground's per-bubble stats: `X-Synapse Proxy-Cache` (L0/L1/L2/L3/LOOP), `X-Synapse Proxy-Tokens-In`, `X-Synapse Proxy-Tokens-Out`, `X-Synapse Proxy-Cost-Saved`. The `/api/playground/chat` route injects them as an SSE `event: stats` line right before the upstream's `data: [DONE]` so the client attaches metadata to the correct bubble without a second fetch.



### Changed

- L3 compression no longer replaces tool outputs with synthetic stubs (which the agent's safety filter treats as prompt injection). Truncated tool results keep the original head + a neutral `["¦truncated by Synapse Proxy L3"¦]` marker. The compaction hint is reduced to a short, dry sentence.

- L3 compression is only applied if it actually shrinks the payload in **both** bytes AND tokens "” never inflates.

- L2 semantic cache is automatically disabled when `nonSystemCount > 1` (multi-turn agentic context) OR when a Record Session is active (X-Synapse Proxy-Session header) OR when the request body contains `tool_calls`. Prevents stale-turn responses from corrupting agent state during long agentic trajectories.

- README, ARCHITECTURE and proxy/README updated to reflect the actual 4-tier cache implementation (was promised in docs but only 3 were coded).



## [1.4.0] "” 2026-06-16



### Added

- **Model Radar v1** with auto-detect (`proxy/internal/workers/model_radar.go`) and **FieldDiscoverer** (`proxy/internal/workers/field_discoverer.go`). When a never-seen model starts flowing through the proxy, we collect up to 10 raw response samples in Redis, run a recursive JSON walk to find the `prompt_tokens` / `completion_tokens` field paths, and persist the mapping. No embedded LLM required. See `MODEL_RADAR.md`.

- `GET /api/admin/model-radar` (closed-source dashboard) "” lists all radar entries, sample counts, and discovered mappings. Requires `SUPERADMIN` role.

- **Loop detection** (`proxy/optiagent/loop_detect.go`) "” when 3+ identical requests land in 60s, the 3rd+ is served from a loop cache (saves upstream calls on runaway agents). ZSET-based rolling window in Redis. Skipped when `zeroLog` is on.

- **Cache-poisoning guard** (`proxy/internal/utils/cache_validation.go`) "” both L1 and Loop cache now refuse to store responses that are empty (`content:""` with `finish_reason: stop`), upstream application errors (`base_resp.status_code != 0`), or contain obvious `error` keys. Prevents a single poison response from being replayed to future requests.

- **Tool-call dedup** (`proxy/optiagent/tool_dedup.go`) "” extracts file paths from OpenAI tool_calls and Anthropic tool_use blocks (`read_file`, `cat`, `Read`, `load_file`, "¦), detects re-reads across requests, and exposes a `synapse:tools:<vk>:<pathHash>` Redis namespace for downstream caching.

- **Auto agent detection** in `proxy.go` "” identifies the originating agent (Hermes, OpenClaw, Claude Code, LangChain, curl, Python SDK, "¦) from the User-Agent, system-prompt signature, and tool-name patterns. No client cooperation required. The `agentId` and `agentLabel` are persisted with every `RequestLog` row.

- **Smart model aliasing** in `proxy.go` "” when a client requests a model that the key's provider doesn't advertise (e.g. `google/gemma-...` on a MiniMax-backed key), the proxy silently routes to the key's `defaultModel` and re-stamps the response `model` field so the client sees the name it asked for. The agent never breaks on a misconfigured client.

- **Compaction hint** (`proxy/optiagent/compaction_hint.go`) "” prepends a system-prompt note: *"Previous tool outputs in this conversation may have been summarized to save tokens. If you need the full original content of a specific tool result, end your message with [EXPAND <id>] and the relevant tool_id will be restored in the next turn."*

- **Zero-Log Mode** (`proxy/internal/utils/redactor.go`) "” per-key privacy flag. When `zeroLog=true`, the body is redacted in-place before the L1/L2/loop/compression path runs, and `originalPayload` / `optimizedPayload` are written as empty strings in `RequestLog`. L1, L2, loop, and Model Radar sample collection are all skipped. Token counts, latency, cost, model, agent, and sessionId are still recorded.

- **Upstream app-error â†’ HTTP 4xx** in `streamResponse` "” when the first SSE chunk from upstream contains `base_resp.status_code != 0` (e.g. MiniMax `1004 login fail`, `2056 Token Plan limit reached`), the proxy returns a clean HTTP 402 with a JSON error body instead of forwarding the 200-with-poison. Stops agents from timing out on a poison stream.

- **Scope-aware multi-tenant cache** in `proxy.go` "” when `isolateCacheByUser=true`, the cache key includes the OpenAI `user` field. Sets the foundation for a future `X-Synapse Proxy-Scope` header (personal / business / generic).



### Changed

- **Telemetry stream is now bounded** (`XAdd MaxLen=100000 Approx=true` in `telemetry.go`). The Redis Stream `synapse:telemetry:logs` is capped at 100k entries with O(1) amortized trimming. No more unbounded growth.

- **Per-request `costSaved`** is now persisted as the sum of the four per-class savings (input fresh + cache read + cache creation + output), using the per-provider pricing in `proxy/internal/db/pricing.go`. The dashboard's "Total Value Saved" widget reads this.

- **Pricing data** expanded with MiniMax and Anthropic rates.

- **L3 compression** is now more conservative on the most recent tool outputs to avoid breaking agent loops. See commit `15ccdce`.



### Security

- **Redis hardening** in `docker-compose.prod.yml` "” `--maxmemory 512mb --maxmemory-policy allkeys-lru --bind 0.0.0.0 --protected-mode no`. Caps memory usage and ensures cached entries are evicted on pressure rather than crashing the process.



## [1.3.0] "” 2026-06-12



### Added

- **Dynamic model listing endpoint** (`/v1/providers/models`) "” the dashboard's key-creation form fetches the live list of models from each provider so the user picks from real, current IDs.

- **L3 fix**: scaling local token estimate to provider-billed ratio so the `savings*` metrics in the dashboard reflect what the provider actually bills, not the local token count.



## [1.2.0] "” 2026-06-08



### Added

- **Per-class savings breakdown** (input fresh / cache read / cache creation / output) persisted on every `RequestLog` row. The dashboard renders this as the bottom-row of the "Cost cumulé & économies par classe" chart.



## [1.1.0] "” 2026-05-30



### Added

- **Zero-Log Mode** (initial implementation): per-key privacy flag with auto-rejection of the cache path.



## [1.0.0] "” 2026-05-15



### Added

- **L1 exact cache** (SHA-256 + Redis, sub-ms hit)

- **L2 semantic cache** (ONNX embedder + Redis VSS)

- **L3 structural compression** for agent prompts

- **Open-core proxy** in Go

- **Multi-tenant cache isolation** by `virtualKey`

- **Live Telemetry** (server-sent events to the dashboard)

- **Virtual key provisioning** with AES-256-CBC encryption of the real provider key

- **EU data residency** (Hetzner Frankfurt)



---



*Format inspired by [Keep a Changelog](https://keepachangelog.com/). The "Unreleased" section is where new dashboard-visible changes accumulate until the next proxy release.*


