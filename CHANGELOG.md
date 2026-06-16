# Changelog

All notable changes to **OptiToken** are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> **Two products** ship from this repository:
> - **OptiToken Proxy** — open-source under MIT, in `proxy/`
> - **OptiToken Dashboard** — closed-source SaaS at [optitoken.net](https://optitoken.net), not in this repo. Dashboard changes are listed here when they require proxy-side changes.

## [Unreleased]

### Added
- Live telemetry widget on the dashboard header now auto-refreshes every 5s with smooth tween animation. The "Total Value Saved" figure and "Tokens Sent / Saved" counters move in real time as new `RequestLog` rows are written.
- Compaction hint injection at the start of the system message so the agent knows previous tool outputs may be summarized. See `proxy/optiagent/compaction_hint.go`.

## [1.4.0] — 2026-06-16

### Added
- **Model Radar v1** with auto-detect (`proxy/internal/workers/model_radar.go`) and **FieldDiscoverer** (`proxy/internal/workers/field_discoverer.go`). When a never-seen model starts flowing through the proxy, we collect up to 10 raw response samples in Redis, run a recursive JSON walk to find the `prompt_tokens` / `completion_tokens` field paths, and persist the mapping. No embedded LLM required. See `MODEL_RADAR.md`.
- `GET /api/admin/model-radar` (closed-source dashboard) — lists all radar entries, sample counts, and discovered mappings. Requires `SUPERADMIN` role.
- **Loop detection** (`proxy/optiagent/loop_detect.go`) — when 3+ identical requests land in 60s, the 3rd+ is served from a loop cache (saves upstream calls on runaway agents). ZSET-based rolling window in Redis. Skipped when `zeroLog` is on.
- **Cache-poisoning guard** (`proxy/internal/utils/cache_validation.go`) — both L1 and Loop cache now refuse to store responses that are empty (`content:""` with `finish_reason: stop`), upstream application errors (`base_resp.status_code != 0`), or contain obvious `error` keys. Prevents a single poison response from being replayed to future requests.
- **Tool-call dedup** (`proxy/optiagent/tool_dedup.go`) — extracts file paths from OpenAI tool_calls and Anthropic tool_use blocks (`read_file`, `cat`, `Read`, `load_file`, …), detects re-reads across requests, and exposes a `optitoken:tools:<vk>:<pathHash>` Redis namespace for downstream caching.
- **Auto agent detection** in `proxy.go` — identifies the originating agent (Hermes, OpenClaw, Claude Code, LangChain, curl, Python SDK, …) from the User-Agent, system-prompt signature, and tool-name patterns. No client cooperation required. The `agentId` and `agentLabel` are persisted with every `RequestLog` row.
- **Smart model aliasing** in `proxy.go` — when a client requests a model that the key's provider doesn't advertise (e.g. `google/gemma-...` on a MiniMax-backed key), the proxy silently routes to the key's `defaultModel` and re-stamps the response `model` field so the client sees the name it asked for. The agent never breaks on a misconfigured client.
- **Compaction hint** (`proxy/optiagent/compaction_hint.go`) — prepends a system-prompt note: *"Previous tool outputs in this conversation may have been summarized to save tokens. If you need the full original content of a specific tool result, end your message with [EXPAND <id>] and the relevant tool_id will be restored in the next turn."*
- **Zero-Log Mode** (`proxy/internal/utils/redactor.go`) — per-key privacy flag. When `zeroLog=true`, the body is redacted in-place before the L1/L2/loop/compression path runs, and `originalPayload` / `optimizedPayload` are written as empty strings in `RequestLog`. L1, L2, loop, and Model Radar sample collection are all skipped. Token counts, latency, cost, model, agent, and sessionId are still recorded.
- **Upstream app-error → HTTP 4xx** in `streamResponse` — when the first SSE chunk from upstream contains `base_resp.status_code != 0` (e.g. MiniMax `1004 login fail`, `2056 Token Plan limit reached`), the proxy returns a clean HTTP 402 with a JSON error body instead of forwarding the 200-with-poison. Stops agents from timing out on a poison stream.
- **Scope-aware multi-tenant cache** in `proxy.go` — when `isolateCacheByUser=true`, the cache key includes the OpenAI `user` field. Sets the foundation for a future `X-Optitoken-Scope` header (personal / business / generic).

### Changed
- **Telemetry stream is now bounded** (`XAdd MaxLen=100000 Approx=true` in `telemetry.go`). The Redis Stream `optitoken:telemetry:logs` is capped at 100k entries with O(1) amortized trimming. No more unbounded growth.
- **Per-request `costSaved`** is now persisted as the sum of the four per-class savings (input fresh + cache read + cache creation + output), using the per-provider pricing in `proxy/internal/db/pricing.go`. The dashboard's "Total Value Saved" widget reads this.
- **Pricing data** expanded with MiniMax and Anthropic rates.
- **L3 compression** is now more conservative on the most recent tool outputs to avoid breaking agent loops. See commit `15ccdce`.

### Security
- **Redis hardening** in `docker-compose.prod.yml` — `--maxmemory 512mb --maxmemory-policy allkeys-lru --bind 0.0.0.0 --protected-mode no`. Caps memory usage and ensures cached entries are evicted on pressure rather than crashing the process.

## [1.3.0] — 2026-06-12

### Added
- **Dynamic model listing endpoint** (`/v1/providers/models`) — the dashboard's key-creation form fetches the live list of models from each provider so the user picks from real, current IDs.
- **L3 fix**: scaling local token estimate to provider-billed ratio so the `savings*` metrics in the dashboard reflect what the provider actually bills, not the local token count.

## [1.2.0] — 2026-06-08

### Added
- **Per-class savings breakdown** (input fresh / cache read / cache creation / output) persisted on every `RequestLog` row. The dashboard renders this as the bottom-row of the "Cost cumulé & économies par classe" chart.

## [1.1.0] — 2026-05-30

### Added
- **Zero-Log Mode** (initial implementation): per-key privacy flag with auto-rejection of the cache path.

## [1.0.0] — 2026-05-15

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
