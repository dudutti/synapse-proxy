# Synapse Proxy SDK

Drop-in OpenAI-compatible client for [Synapse Proxy](https://synapse-proxy.com) with extensions for session recording, cache statistics, savings reports, and A/B benchmark.

Available in **Python** and **TypeScript** (Node 18+). MIT licensed.

## Why this SDK?

The Synapse Proxy is **already OpenAI-compatible**: any client that supports a custom `base_url` works out of the box (the official `openai` SDK in Python and Node, the `anthropic` SDK with an OpenAI-shaped adapter, the Vercel AI SDK, LangChain, LlamaIndex, etc.).

The SDK adds **three things** on top of that:

1. **Convenience** — pre-configured `base_url`, env-var fallback for the API key, a typed `complete()` shortcut.
2. **Type safety** — `ChatCompletion`, `CacheStats`, `SavingsReport`, `SessionRecording`, `BenchmarkResult` are proper dataclasses / interfaces, not raw dictionaries.
3. **Synapse-specific operations** that don't exist in the standard OpenAI API:
   - `client.sessions.start({ groupBy: "agent" })` — record a live traffic session.
   - `client.cache.stats({ days: 7 })` — L1/L2/L3 cache hit rate.
   - `client.savings.summary({ days: 30 })` — $ saved by class and provider.
   - `client.benchmark.run({ models, prompt, judgeModel })` — A/B test with an LLM judge.

## Installation

### Python

```bash
pip install synapse-proxy
```

Or with the dev extras (for running the test suite):

```bash
pip install "synapse-proxy[dev]"
```

### TypeScript / Node

```bash
npm install @synapse-proxy/sdk
# or
pnpm add @synapse-proxy/sdk
# or
yarn add @synapse-proxy/sdk
```

## Quick start

### Standard OpenAI usage

The SDK re-exports the standard `chat`, `completions`, `embeddings`, and `models` namespaces, so anything you can do with the OpenAI SDK works identically.

```python
# Python
from synapse_proxy import SynapseProxy

sp = SynapseProxy(api_key="sk-opti-...")
chat = sp.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Hello, world!"}],
)
print(chat.choices[0].message.content)
```

```ts
// TypeScript
import { SynapseProxy } from "@synapse-proxy/sdk";

const sp = new SynapseProxy({ apiKey: "sk-opti-..." });
const chat = await sp.chat.completions.create({
  model: "gpt-4o-mini",
  messages: [{ role: "user", content: "Hello, world!" }],
});
console.log(chat.choices[0].message.content);
```

The API key can also come from the `SYNAPSE_PROXY_API_KEY` environment variable. The base URL can be overridden with `SYNAPSE_PROXY_BASE_URL` (handy for self-hosted or staging instances).

### One-shot helper with cache info

```python
# Python — returns a typed ChatCompletion with cache_level populated
from synapse_proxy import SynapseProxy

sp = SynapseProxy(api_key="sk-opti-...")
chat = sp.complete(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Hello"}],
)
print(chat.cache_level)  # "L1" | "L2" | "L3" | ""
print(chat.usage.cached_tokens)
```

### Record a session of live traffic

```python
session = sp.sessions.start(group_by="agent", label="evening-test")
# ... traffic flows through the proxy ...
ended = sp.sessions.stop(session.id)
print(f"Captured {ended.record_count} requests, saved ${ended.estimated_cost_saved:.4f}")

# List recent sessions
for s in sp.sessions.list(limit=10):
    print(s.id, s.started_at, s.record_count)
```

### Read cache hit statistics

```python
stats = sp.cache.stats(days=7)
print(f"Total requests: {stats.total_requests}")
for level in stats.by_level:
    print(f"  {level.cache_level}: {level.hit_rate:.1%} hit rate")
```

### Read the savings report

```python
report = sp.savings.summary(days=30)
print(f"Total $ saved: ${report.total_cost_saved:.2f}")
print("By class:", report.by_class)
print("By provider:", report.by_provider)
```

### Run an A/B benchmark

```python
result = sp.benchmark.run(
    models=["gpt-4o-mini", "minimax-m3"],
    prompt="Explain TCP slow start in 3 sentences.",
    judge_model="gpt-4o-mini",
    runs=3,
)
print(f"Winner: {result.winner}")
print(f"Cache hit rate: A={result.cache_hit_rate_a:.1%}, B={result.cache_hit_rate_b:.1%}")
print(f"Cost: A=${result.cost_a:.4f}, B=${result.cost_b:.4f}")
print(f"Judge reason: {result.judge_reason}")
```

### Streaming

Streaming works exactly like the OpenAI SDK:

```python
for chunk in sp.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Tell me a story"}],
    stream=True,
):
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="")
```

```ts
const stream = await sp.chat.completions.create({
  model: "gpt-4o-mini",
  messages: [{ role: "user", content: "Tell me a story" }],
  stream: true,
});
for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? "");
}
```

### Function calling, vision, tools

All standard OpenAI features pass through transparently because the SDK wraps the official `openai` client. See the OpenAI docs for usage patterns.

## Configuration

| Option | Env var | Default | Description |
| --- | --- | --- | --- |
| `apiKey` | `SYNAPSE_PROXY_API_KEY` | _(required)_ | Virtual key starting with `sk-opti-` |
| `baseUrl` | `SYNAPSE_PROXY_BASE_URL` | `https://synapse-proxy.com/v1` | Override for self-hosted or staging |
| `timeoutMs` | — | `60000` | Request timeout |
| `maxRetries` | — | `2` | Retries on transient errors |
| `organization` | — | _(none)_ | OpenAI org ID (for OpenAI-shaped usage) |
| `project` | — | _(none)_ | OpenAI project ID |
| `defaultHeaders` | — | `{}` | Extra headers on every request |

## Error handling

The SDK raises a small exception hierarchy:

- `SynapseProxyError` — base class
- `AuthenticationError` — HTTP 401/403, raised on missing / invalid / revoked API keys
- `APIError` — any other non-2xx response
- `SynapseProxyError` with a network message — DNS / TCP / TLS failures

```python
from synapse_proxy import SynapseProxy, AuthenticationError, APIError

sp = SynapseProxy(api_key="sk-opti-...")
try:
    sp.cache.stats()
except AuthenticationError as e:
    print("Your key is invalid or revoked:", e)
except APIError as e:
    print("Proxy error", e.status, e.body)
```

## How it works

```
┌─────────────────┐         ┌──────────────────────┐         ┌─────────────┐
│ Your app        │ ──HTTP──▶  Synapse Proxy           │ ──HTTP──▶ │ OpenAI /    │
│ (Python or Node) │         │ - 4-level cache        │         │ Anthropic / │
│                 │         │ - Session recording   │         │ MiniMax /   │
│                 │         │ - Agent detection     │         │ ...         │
│                 │         │ - Cost & savings calc  │         └─────────────┘
│                 │ ◀─HTTP── │ - A/B benchmark judge │ ◀─HTTP──┘
└─────────────────┘         └──────────────────────┘
       │                                │
       │                                ▼
       │                       ┌──────────────────────┐
       └────analytics API────▶ │ Next.js Dashboard     │
                               │ (SaaS, separate repo) │
                               └──────────────────────┘
```

The SDK re-exports the standard OpenAI namespace (`chat`, `completions`, `embeddings`, `models`) via the official `openai` package, so it inherits every feature that the OpenAI SDK supports: streaming, function calling, vision, structured outputs, etc.

The `client.sessions.*`, `client.cache.*`, `client.savings.*`, `client.benchmark.*` namespaces are thin HTTP wrappers over the dashboard's analytics API at `/api/sessions/*`, `/api/analytics/cache`, `/api/analytics/savings`, and `/api/keys/session-benchmark`.

## Self-hosted proxy

If you're running your own Synapse Proxy instance (the open-source Go engine is available at <https://github.com/dudutti/synapse-proxy>), point the SDK at it:

```python
sp = SynapseProxy(
    api_key="sk-opti-...",
    base_url="https://my-proxy.internal/v1",
)
```

```ts
const sp = new SynapseProxy({
  apiKey: "sk-opti-...",
  baseUrl: "https://my-proxy.internal/v1",
});
```

The Synapse extensions work as long as the dashboard (or a compatible API) is reachable at the same host.

## License

MIT. See [LICENSE](./LICENSE).

## Contributing

Bug reports and PRs welcome at <https://github.com/dudutti/synapse-proxy-sdk>.
