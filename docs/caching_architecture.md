# Synapse Proxy Caching Architecture

This document provides a deep technical dive into how Synapse Proxy intercepts, deduplicates, and compresses requests before they hit the upstream provider.

## How the cache pipeline works

```
                     your app / agent / SDK
                              ¦
                              ¦  HTTP, Authorization: Bearer sk-opti-...
                              ?
              +--------------------------------+
              ¦       Synapse Proxy (Go)     ¦
              ¦                                ¦
              ¦  +-------+  +--------------+   ¦
              ¦  ¦  L0   ¦-?¦      L1      ¦   ¦
              ¦  ¦ in-fl.¦  ¦ exact match  ¦   ¦
              ¦  ¦ dedup ¦  ¦  (SHA-256)  ¦   ¦
              ¦  +-------+  +--------------+   ¦
              ¦                    ¦ miss     ¦
              ¦                    ?          ¦
              ¦              +----------+     ¦
              ¦              ¦    L2    ¦     ¦
              ¦              ¦ semantic ¦     ¦
              ¦              ¦ (ONNX)   ¦     ¦
              ¦              +----------+     ¦
              ¦                   ¦ miss     ¦
              ¦                   ?          ¦
              ¦             +----------+      ¦
              ¦             ¦    L3    ¦      ¦
              ¦             ¦  (tail   ¦      ¦
              ¦             ¦compress) ¦      ¦
              ¦             +----------+      ¦
              ¦                  ¦           ¦
              +------------------+-----------+
                                 ¦
                                 ?
                +----------------------------+
                ¦  upstream provider          ¦
                ¦  (OpenAI, Anthropic, etc.)  ¦
                +----------------------------+
```

The four caches, in detail:

| Cache | What it does | When it kicks in | Source |
|-------|--------------|------------------|--------|
| **L0** In-flight dedup | Two identical requests (same virtual key, same SHA-256 payload) arrive concurrently. The first acquires a Redis SETNX lock with a 30-second TTL and processes normally. The second **blocks and waits** on the lock. | Race conditions, agent retries, parallel curl, fan-out from a parent agent. | `optiagent/dedup.go` |
| **L1** Exact match | The full SHA-256 of the normalized request payload is the cache key. Hit returns the cached response in <2 ms. | Cron jobs, scripts that retry the same query, identical tool calls across agent turns. Per virtual key. | `optiagent/engine.go` |
| **L2** Semantic | The last user message is embedded by a local ONNX model (multilingual MiniLM, 384-dim) and a KNN search is run against a Redis VSS index (`FT.SEARCH idx:l2cache`). The cosine-similarity threshold is the per-key `semantic_tolerance` (default 0.15). | "How do I reset my password?" matches "Forgot password, what now?". **Auto-disabled** if the request is multi-turn (`nonSystemCount > 1`) or contains an image — two consecutive turns of an agent have near-identical embeddings and returning a cached response from a *different* turn would corrupt the conversation state. | `cache/l2_vector.go` |
| **L3** Compression | The system prompt, tool declarations, and older history are byte-exact preserved. Only the last 4 messages are rewritten: stale `<thought>` blocks pruned, repeated tool calls collapsed, `reasoning_content` stripped, oversized tool outputs truncated, the LLM gets a `(Earlier tool results in this transcript may be truncated.)` hint prepended to its system prompt. | Long agent sessions with redundant chain-of-thought, repeated tool calls, stale tool outputs. | `optiagent/compressor.go` |

**When the cache is bypassed** (`CacheLevel: BYPASS`): the client sends `X-Bypass-Cache: true` (curl, manual debugging, or the `isBypass` flag in the playground). The proxy forwards the request as-is, persists the row to telemetry, but skips every cache layer.

**When the cache is bypassed for correctness**: L2 is auto-disabled for multi-turn and image payloads, as described above. L1 still runs.

---

## Cache-Preserving L3 — keeping the provider's prompt cache alive

This is the part of the design that is the easiest to get wrong and the most expensive when you do.

Anthropic, OpenAI, and MiniMax all hash your request bytes and serve the same prefix from a server-side cache for ~90% off on subsequent calls. But the hash is **byte-exact** — change a whitespace, reorder a JSON key, escape a `<` to `\u003c`, and the cache miss happens and you pay full price.

The naive mistake most compression libraries make: re-encode the entire payload, which changes all of the above. With the naive compressor, the provider's `cached_tokens` field drops to 0 on the second request.

Synapse Proxy's implementation solves this in three layers:

1. **Phase 1 — Idempotent encoder.** A deterministic JSON encoder (`proxy/optiagent/marshal_deterministic.go`) guarantees that two compressions of the same payload produce byte-identical output. Sorted keys alphabetically, no whitespace, no HTML escaping, deterministic float formatting.
2. **Phase 2 — Prefix-preserving split.** Before compressing, the proxy walks the JSON payload character by character, finds the boundary between the static prefix (system prompt, tool declarations, history older than 4 messages back) and the dynamic tail (recent user/assistant turns), and splits. The prefix is left byte-exact. Only the tail is rewritten.
3. **Phase 3 — Co-located compression.** The tail is wrapped in a synthetic envelope (`{"messages":[<tail>]}`), passed through the standard L3 rules, unwrapped, and re-attached. The result is a valid JSON document where the first N bytes are byte-exact identical to the input.

*Result:* 99.8% of prompt tokens continue to hit the upstream provider's native cache, even when L3 actively compresses the recent context window.
