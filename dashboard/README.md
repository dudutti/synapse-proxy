# Synapse Proxy Dashboard (private SaaS)

The Next.js frontend for Synapse Proxy. Connects to the OSS Go proxy + Redis + Postgres stack. Closed-source while we stabilise the public offering — see [Synapse Proxy.net](https://Synapse Proxy.net) for the live product.

## What's here

```
app/
├── page.tsx                  # main dashboard (live telemetry + record session)
├── playground/page.tsx       # Playground v3 (see below)
├── api/
│   ├── analytics/            # /api/analytics + /api/analytics/session
│   ├── keys/                 # POST /api/keys (AES-256-GCM encryption at write)
│   ├── models/route.ts       # POST /api/models (lists provider models for a key)
│   ├── playground/chat/      # SSE proxy to the Go data plane
│   ├── telemetry/            # /api/telemetry/[id] + /api/telemetry/[id]/payload
│   └── admin/telemetry/      # globe + heatmap data
└── ...
components/
├── LiveTelemetryGrouped.tsx  # grouped live view + DiffModal (LCS + intra-line)
└── ...
```

## Playground v3 (`/playground`)

The Playground is the dashboard's most powerful debugging tool. It is a 2-panel chat UI that fires the **same prompt twice in parallel** — once through Synapse Proxy (cache hits allowed), once directly to the upstream provider (cache forced off via `X-Bypass-Cache: true`).

### Features

- **Per-bubble stats footer** — every assistant reply gets a coloured cache-level badge (`L0` cyan, `L1` blue, `L2` emerald, `L3` purple, `LOOP` amber, `MISS` zinc) plus inline token counts (`in` / `out`), `latency (ms)`, and `$ saved` vs. the direct path.
- **3-up A vs B comparison bar** above the chat panels — `cost saved %`, `latency delta`, `token delta`. Emerald when Synapse Proxy wins, amber when it loses, zinc on a tie.
- **SVG sparklines strip** — inline polyline charts for Opti latency, Direct latency, and cumulative `$ saved` over the last 50 messages. Zero JS chart library, pure inline `<svg>`.
- **Artifact Renderer** — auto-detects ```` ```html ````, ```` ```python ````, ```` ```js ````, etc. in the assistant reply:
  - **HTML artifacts** render live in a sandboxed `<iframe sandbox>` (no `allow-same-origin` — the artifact can't read your cookies / DOM). Buttons: `Copy` (source), `Open` (new tab via Blob URL — works in all browsers, unlike `data:` URIs that Chrome silently ignores and Safari blocks), `Download` (`.html` file), `Source` (toggle between rendered preview and raw HTML).
  - **Code artifacts** get `Copy` and `Download` (`.py` / `.js` / `.ts` / etc.); language tag comes from the markdown fence.
- **Linked / Independent panels** — by default both panels share the same key+model (true A/B). Click `Unlink` in the header to give each panel its own (key, model) — compare two providers side by side, or pin a cheap model on the right and a frontier model on the left.
- **Export session as JSON** — one click downloads `{settings, messages, sparklines, stats}`. Paste into a PR, share with a teammate, or diff two sessions to find regressions.
- **Clear** button resets both panels and sparkline history.

### Architecture

The frontend reads per-call stats from the proxy via custom response headers exposed on every upstream call (regardless of cache hit or miss):

| Header | Source |
|---|---|
| `X-Synapse Proxy-Cache` | `L0` / `L1` / `L2` / `L3` / `LOOP` / `L0-coalesced` / empty |
| `X-Synapse Proxy-Tokens-In` | original input token count |
| `X-Synapse Proxy-Tokens-Out` | optimised input token count (post-L3) |
| `X-Synapse Proxy-Cost-Saved` | quick single-class $ saved estimate |
| `X-Latency-Ms` | end-to-end latency (proxy measured) |

The `/api/playground/chat` route is a thin wrapper that injects an SSE `event: stats` line right before the upstream's `data: [DONE]`, so the client attaches the metadata to the correct bubble without a second fetch.

## Local development

```bash
npm install
npm run dev
```

Point at your local proxy via `.env.local`:

```
PROXY_URL=http://localhost:8080
REDIS_URL=redis://localhost:6379
ENCRYPTION_KEY=<32 bytes hex, must match the proxy>
```

## Build & deploy

```bash
docker compose -f docker-compose.prod.yml build dashboard
docker compose -f docker-compose.prod.yml up -d dashboard
```
