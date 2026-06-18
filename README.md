# OptiToken

**An open-source LLM proxy that turns agent traffic into a measurable, optimizable flow.**

OptiToken sits between your application and your LLM provider (OpenAI, Anthropic, MiniMax, any OpenAI-compatible endpoint). It deduplicates identical requests, prunes redundant context, and gives you a per-request telemetry stream so you can see what your agents are actually doing.

> **Status:** v1.5 — running in production. Repo ships the proxy (Go, MIT). The dashboard at [optitoken.net](https://optitoken.net) is closed-source SaaS.

---

## What problem does this solve?

Agentic workloads are repetitive. An agent looping through a codebase will read the same files, make the same tool calls, and re-emit the same `<thought>` blocks for hundreds of turns. A naive reverse proxy forwards every byte to the provider. The provider charges for every byte.

OptiToken gives you three things in one binary:

1. **A four-tier cache** (L0 in-flight dedup → L1 exact match → L2 semantic → L3 compression) so the provider sees less traffic.
2. **Per-request telemetry** so you can see what hit/missed, why, and at what cost.
3. **An agent detector** so the cache can disable itself when the workload is a long-running agent (where caching would corrupt the context).

The proxy is **transparent** — drop it in front of any OpenAI-compatible endpoint, no SDK changes.

---

## Architecture

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
              │                   │ miss      │
              │                   ▼           │
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

  Side channel: every decision is logged to Postgres (RequestLog) and
  an admin dashboard at optitoken.net for inspection.
```

**Tech stack:** Go 1.21 (proxy) · Redis Stack with RediSearch VSS (L2 vector cache, idempotency locks) · Postgres 15 (telemetry, sessions, virtual keys) · Python ONNX embedder (multilingual MiniLM) · Caddy (TLS termination) · Next.js 14 + Prisma (closed-source dashboard).

---

## The four caches, in plain English

| Cache | What it does | When it kicks in | Cost saved |
|-------|--------------|------------------|-----------|
| **L0** In-flight dedup | Two identical requests arrive at the same time. The first one processes normally. The second one **blocks and waits** for the first one's response (up to 30s). | Race conditions, agent retries, parallel curl. | Full upstream cost on the follower. |
| **L1** Exact match | The full SHA-256 of the normalized request payload is the cache key. Hit returns the cached response in <2ms. | Cron jobs, scripts that retry the same query, identical tool calls across turns. | Full upstream cost. |
| **L2** Semantic | Local ONNX model embeds the last user message into a 384-dim vector. If the cosine similarity with any cached vector is above `semantic_tolerance` (default 0.15), that cached response is returned. | "How do I reset my password?" matches "Forgot password, what now?". | Full upstream cost. |
| **L3** Compression | The system prompt and old tool-call outputs are byte-exact preserved (so the provider's own prompt cache can still hit). Only the last 4 messages — recent user/assistant turns and any old chain-of-thought — are compressed. | Long agent sessions with redundant `<thought>` blocks, repeated tool calls, and stale tool outputs. | 30–70% of the dynamic-tail tokens. |

**When L2 disables itself:** if a request has more than one non-system message (i.e. the agent is mid-conversation), L2 is skipped. Two consecutive turns of an agent have near-identical embeddings, and returning a cached response from a *different* turn would corrupt the conversation state. L1 still runs.

**When L3 is destructive-free:** L3 only ever touches the last 4 messages. Everything before is byte-exact preserved. The provider's prompt cache (Anthropic, OpenAI, MiniMax) keeps hitting across calls. See [Cache-Preserving L3](#cache-preserving-l3--how-the-cache-still-hits) for the design.

---

## What you actually see

Every request gets a per-request row in `RequestLog` with the cache hit level, original vs optimized token counts, per-class savings, agent ID, model, latency, payload hash, and session ID. Aggregates are exposed at three endpoints on the proxy itself:

| Endpoint | Purpose |
|----------|---------|
| `GET /healthz` | Liveness check. Returns `{"status":"ok"}`. |
| `GET /readyz`  | Readiness check. Pings Postgres + Redis. |
| `GET /metrics` | Prometheus-format metrics (request count, cache hit rate, token savings, p50/p95/p99 latency). |

For the full visual experience — drill-down by agent, session replay, billing analytics, alert rules — use the hosted dashboard at [optitoken.net](https://optitoken.net). It's free during the beta.

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
```

Point your client at `http://localhost:8080/v1` instead of `https://api.openai.com/v1`. The Authorization header is your virtual key (`sk-opti-...`) — not the upstream provider's key. Virtual keys are configured through the dashboard.

---

## What "virtual keys" mean

You give the proxy a virtual key (`sk-opti-...`). The proxy looks that key up in Redis to find:

- The real provider key (stored AES-256-GCM-encrypted at rest, decrypted in-memory only at request time)
- Provider + default model + fallback provider
- Per-key cache TTL and semantic tolerance
- Whether to bypass cache for benchmarking

Virtual keys let you issue a key to a single agent or a single customer, then revoke it without rotating the upstream provider key. The dashboard at optitoken.net is the management UI for this.

---

## Cache-Preserving L3 — how the cache still hits

This is the most non-obvious part of the design, so it gets its own section.

**The problem:** Anthropic, OpenAI, and MiniMax all have a *provider-side* prompt cache. They hash your request bytes and serve the same prefix from cache for ~90% off on subsequent calls. But this only works if the prefix bytes are byte-exact. If anything in the prefix changes (a timestamp, whitespace, key reorder), the cache miss happens and you pay full price.

**The naive mistake:** most "compression" libraries re-encode the entire payload, which changes whitespace, key order, and Unicode escaping. The provider's cache key sees a different hash → cache miss → 5× more expensive than no compression at all. We benchmarked this exact failure mode on 2026-06-18 (see `test/ab_benchmark_2026_06_18/`).

**What we do instead:**

1. **Phase 1 — Idempotent encoder.** The L3 compressor emits byte-exact output for byte-exact input. Keys are sorted alphabetically, no whitespace, no HTML escaping, deterministic float formatting. See `proxy/optiagent/marshal_deterministic.go` and the 6 unit tests in `compressor_test.go`.

2. **Phase 2 — Prefix-preserving split.** Before compressing, the proxy walks the JSON payload, finds the boundary between the static prefix (system prompt, tool declarations, history older than 4 messages back) and the dynamic tail (recent user/assistant turns). The prefix is left **byte-exact** — the compressor's hands-off. Only the tail is rewritten. The split is implemented as a single-pass character scanner in `proxy/optiagent/prefix_split.go` with 9 unit tests in `prefix_split_test.go`.

3. **Phase 3 — Co-located compression.** The tail is wrapped in a synthetic envelope (`{"messages":[<tail>]`), passed through the standard L3 rules (CoT pruning, tool-output truncation, repeated-tool-call collapsing), unwrapped, and re-attached to the byte-exact prefix. The result is a valid JSON document where the first N bytes are byte-exact identical to the input.

**The validation run** (2026-06-18, see `test/ab_benchmark_2026_06_18/data_proxy_log.txt`): we sent 5 identical Hermes-style requests to a MiniMax-M3 upstream. On the 4th request, the provider's response was:

```json
"usage": {
  "prompt_tokens": 6564,
  "completion_tokens": 2,
  "prompt_tokens_details": { "cached_tokens": 6550 }
}
```

6 550 of the 6 564 prompt tokens (99.8%) were served from the provider's own cache. Without the prefix-preserving split, `cached_tokens` would have been 0.

**What this isn't:** we don't pad the prefix to fake the cache into activating. We don't inject `cache_control` markers on behalf of the user. We don't re-write the payload on the fly. The split is conservative (last 4 messages) — the agent's safety filters re-checking the recent turns still see the same content as the user.

---

## Security model

- **Real provider keys** are stored AES-256-GCM-encrypted (authenticated encryption, 12-byte random IV, 16-byte auth tag) under a shared `ENCRYPTION_KEY` (32 bytes hex) configured in `.env`. They are decrypted only in-memory at request time, and only on the goroutine handling the matching virtual key.
- **Virtual keys** are the only thing client code ever sees. They are checked against Redis with a single HGETALL — no database round-trip on the hot path.
- **Multi-tenant isolation:** the L2 semantic cache is segmented by the `user` field in the OpenAI payload when the `isolate_cache_by_user` flag is on for a key. Without that flag, two different end-users of the same virtual key share the L2 cache (typical for an internal tool where one human uses it).
- **No telemetry by default** for keys marked `zero_log`. The proxy still routes the request, still applies the cache, but writes nothing to Postgres for that request.

We do not claim GDPR-readiness or any specific compliance certification. The proxy runs on whatever infrastructure you deploy it to. EU data residency is a deployment choice, not a feature.

---

## Telemetry

Per request, persisted to `RequestLog`:

| Column | Meaning |
|--------|---------|
| `cacheLevel` | `MISS`, `L0`, `L1`, `L2`, `L3`, `LOOP`, `BYPASS` |
| `promptTokensOrig` / `promptTokensOpt` | Token counts measured by the upstream |
| `savingsInputFresh` / `savingsCacheRead` / `savingsCacheCreation` / `savingsOutput` | Per-class dollar savings, computed against the `ProviderModel` pricing table |
| `cacheCreationTokens` / `cacheReadTokens` / `cacheHitTokens` / `cacheMissTokens` | When the upstream exposes them (Anthropic, OpenAI) |
| `durationMs` | Wall-clock end-to-end |
| `agentId` / `agentLabel` | Detected from User-Agent and system prompt heuristics |
| `sessionId` | Set by the dashboard's Record Session feature (see below) |
| `payloadHash` | SHA-256 of the original payload — useful for grouping identical requests in `Most Expensive Prompts` |

---

## Record Session

A feature on the hosted dashboard that lets you tag every request made with one of your virtual keys during a window of time, so you can later review the full per-class breakdown for that window.

The proxy implements the tagging with a Redis lookup: when the dashboard starts a session, it writes `optitoken:session:vk:<vk>` to Redis with a 24h TTL. The proxy checks this on every request. If a tag is present, it overrides the per-request `sessionId` for that RequestLog row. When the session stops, the dashboard deletes the key. There is no need to touch the agent — Hermes, Claude Code, raw curl, anything that uses the virtual key is recorded transparently.

To use it, log in to [optitoken.net](https://optitoken.net), click **Record Session**, run your agent workload, click **Stop**. The full session summary includes L0/L1/L2/L3/LOOP/MISS counts, per-class savings, by-provider / by-model / by-agent breakdowns, and the total cost impact. Every session is saved and revisitable from `Admin → Session History`.

The implementation lives in:
- `proxy/internal/services/auth.go` — `LookupSessionTag` (the Redis read)
- `proxy/internal/handlers/proxy.go` — fallback from header to Redis
- The dashboard route `app/api/sessions/record/route.ts` (start/stop) — closed source.

---

## Limitations and known gaps

We are rigorous about what this project does and does not do.

- **Anthropic and OpenAI are not in our test loop yet.** The MiniMax-M3 benchmark in `test/ab_benchmark_2026_06_18/` is the only real-provider A/B we have. The 99.8% cache hit number is from MiniMax specifically. We expect similar behavior on Anthropic Claude (which exposes `cache_creation_input_tokens` / `cache_read_input_tokens`) and OpenAI (which exposes `cached_tokens`), but the data isn't here yet.
- **Padding to force cache activation is not implemented.** Some providers need 1024+ tokens of prefix to activate their cache. We considered injecting dummy tool calls or filler text, but rejected it: it pollutes the agent's context window. It's an opt-in feature flag reserved for users who know their agent loops frequently enough to amortize the `cache_write` cost. The flag is not exposed yet.
- **Streaming is partially measured.** The benchmark in `test/` uses `stream: false` for both control and optimized. SSE streaming cache behavior is not characterized.
- **The "L0 Coalesced" leader does not share its result with cross-process peers.** Each Go process holds its own in-flight dedup map. Behind a load balancer with N replicas, you can have N copies of the same in-flight request. This is fine for the single-binary docker-compose setup. A distributed lock (Redis SETNX) would be the next step, but we have not implemented it because the single-replica setup is what we run.
- **No SSE-through cache.** Streaming responses are not cached. They bypass L1/L2/L3 and go straight upstream. This is by design — partial streams are awkward to cache, and most streaming workloads (chat UX) are inherently one-shot.
- **Multi-region replication is not implemented.** The proxy is stateful (Redis cache, Postgres telemetry). Run a single region, or accept that the cache hit rate will reset across regions.
- **No Python or Node SDK.** Bring-your-own HTTP client. The proxy is plain OpenAI-compatible HTTP.

---

## Repository layout

```
Optitoken/
├── proxy/                          ← Open source (MIT). This is the binary.
│   ├── cmd/server/                 ← main.go entry point
│   ├── internal/
│   │   ├── handlers/proxy.go       ← the request pipeline
│   │   ├── optiagent/              ← L3 compression, agent detection, session split
│   │   │   ├── engine.go
│   │   │   ├── compressor.go
│   │   │   ├── compressor_test.go
│   │   │   ├── marshal_deterministic.go
│   │   │   ├── prefix_split.go
│   │   │   ├── prefix_split_test.go
│   │   │   └── agent_detector.go
│   │   ├── services/
│   │   │   ├── auth.go             ← virtual key lookup + AES-GCM decrypt + session tag
│   │   │   ├── pricing.go
│   │   │   ├── savings.go
│   │   │   └── redis.go
│   │   ├── workers/
│   │   │   ├── telemetry.go        ← RequestLog writer
│   │   │   ├── stats.go
│   │   │   ├── pricing.go
│   │   │   └── ...
│   │   └── db/                     ← postgres + redis pools
│   ├── onnx-embedder/              ← Python service: MiniLM embedding
│   ├── seeds/                       ← SQL seed data (models, default keys)
│   ├── docker-compose.yml
│   ├── Dockerfile
│   └── README.md                   ← proxy-specific build instructions
│
├── test/                            ← Open source. Reproducible test data.
│   ├── README.md
│   └── ab_benchmark_2026_06_18/    ← Hermes workload, 5 identical requests, raw logs
│       ├── data_benchmarklog.csv
│       ├── data_benchmarklog.json
│       ├── data_proxy_log.txt
│       └── 01..07 *.sh             ← reproducible shell scripts (no creds in repo)
│
└── README.md                        ← this file
```

The hosted dashboard at [optitoken.net](https://optitoken.net) is closed-source SaaS. It is not in this repository.

---

## Commit history (recent)

```
3c9f771 feat(proxy): server-side session recording via Redis tag lookup
92d0c89 docs(test): add A/B benchmark data validating Phase 2 cache-preserving L3
333c4a1 feat(proxy): prefix-preserving L3 split (Phase 2 of cache-preserving L3)
545b6df docs(README): document the cache-preserving L3 architecture
08a36f9 feat(proxy): idempotent L3 + redis persistence
cda3239 fix(proxy): persist payloadHash + sessionId + agentId via two-query INSERT
28865f5 feat(proxy): P1.4 — /healthz, /readyz, /metrics endpoints + observability
00c6977 docs: clarify defaultModel requirement per agent SDK (Hermes, OpenClaw)
a6a57d8 feat(proxy): P0.2 panic recovery + P1 logOnce on missing pricing
9b5c1bc Initial commit: OptiToken proxy v1.5
```

The first 6 commits on the list are the work that shipped the cache-preserving L3 architecture end-to-end: idempotent encoder, prefix split, server-side session recording, and the A/B benchmark that validates the design.

---

## License

MIT. The proxy is open source. The hosted dashboard at [optitoken.net](https://optitoken.net) is a separate commercial product.

---

## — Version française —

### OptiToken, en une phrase

Un proxy LLM open source qui transforme le trafic de vos agents en un flux mesurable et optimisable.

### Le problème

Les agents répètent. Beaucoup. Un agent qui parcourt un codebase va relire les mêmes fichiers, refaire les mêmes tool calls, et réémettre les mêmes blocs `<thought>` pendant des centaines de tours. Un proxy naïf relaie tout au provider, qui facture tout.

OptiToken fait trois choses en un seul binaire :

1. **Un cache à 4 niveaux** (L0 dédup en vol → L1 exact → L2 sémantique → L3 compression) pour réduire le trafic vers le provider.
2. **Une télémétrie par requête** pour voir ce qui a hit, ce qui a miss, et à quel coût.
3. **Un détecteur d'agent** qui désactive le cache quand la charge est un agent long-running — sans ça, on renverrait des réponses d'un tour précédent et on corromprait le contexte.

Le proxy est **transparent** : on le branche devant n'importe quel endpoint OpenAI-compatible, sans changer le code de l'app cliente.

### Les quatre caches

| Cache | Ce qu'il fait | Quand il s'active | Coût économisé |
|-------|---------------|-------------------|----------------|
| **L0** Déduplication en vol | Deux requêtes identiques arrivent en même temps. La première traite normalement, la seconde **se met en attente** (jusqu'à 30s) et récupère la réponse de la première. | Retries, courses entre agents, curl en parallèle. | Coût upstream complet sur la suiveuse. |
| **L1** Exact | Le SHA-256 du payload normalisé est la clé de cache. Hit en <2ms. | Cron jobs, scripts qui relancent la même requête, tool calls identiques entre tours. | Coût upstream complet. |
| **L2** Sémantique | Modèle ONNX local qui vectorise le dernier message user. Si la similarité cosinus dépasse `semantic_tolerance` (défaut 0.15), la réponse cachée est renvoyée. | "Comment réinitialiser mon mot de passe ?" matche "J'ai oublié mon mot de passe, que faire ?". | Coût upstream complet. |
| **L3** Compression | Le system prompt et les anciens tool outputs sont préservés byte-exact (pour que le cache provider continue de fonctionner). Seuls les 4 derniers messages sont compressés. | Sessions d'agent longues avec des blocs `<thought>` redondants, tool calls répétés, tool outputs devenus obsolètes. | 30–70% des tokens de la queue dynamique. |

### Cache-Preserving L3

C'est la partie la moins évidente du design, et la plus subtile. Anthropic, OpenAI et MiniMax ont tous un **cache provider** : ils hashent les bytes de votre requête et servent le préfix identique depuis leur cache pour ~90% de réduction sur les requêtes suivantes. Mais ce cache ne fonctionne que si le préfix est **byte-exact**. Si quoi que ce soit change dans le préfix (un timestamp, un whitespace, un ré-ordre de clés JSON), c'est cache miss et vous payez plein pot.

L'erreur classique : un middleware de compression naïf ré-encode le payload complet, ce qui change les whitespaces et l'ordre des clés. Le provider voit un hash différent → cache miss → vous payez **5× plus cher** que sans compression. Nous avons mesuré cet échec en conditions réelles le 2026-06-18 (voir `test/ab_benchmark_2026_06_18/`).

Ce qu'on fait à la place :
- **Phase 1** : un encodeur JSON déterministe. Le payload ré-encodé est byte-exact pour un input byte-exact. (Voir `proxy/optiagent/marshal_deterministic.go` et les 6 tests unitaires dans `compressor_test.go`.)
- **Phase 2** : un split qui sépare le préfixe statique (system prompt, history ancienne) de la queue dynamique (tours récents). Le préfixe est laissé byte-exact. Seule la queue est compressée. (Voir `proxy/optiagent/prefix_split.go` et les 9 tests dans `prefix_split_test.go`.)
- **Phase 3** : la queue est enveloppée dans un `{"messages":[<queue>]` synthétique, passée dans le compresseur standard, puis ré-assemblée. Le résultat est un document JSON valide où les N premiers octets sont byte-exact identiques à l'input.

**La mesure** (logs proxy, 4e requête sur 5) : `prompt_tokens: 6564, cached_tokens: 6550`. 99.8% du prompt servi depuis le cache provider.

### Ce qu'on ne fait PAS

- On ne pad pas le préfixe pour forcer le cache. Ça pollue la fenêtre d'attention de l'agent. C'est un opt-in désactivé par défaut.
- On n'injecte pas de `cache_control` markers à la place de l'utilisateur. L'agent décide.
- On ne ré-écrit pas le payload en stream. Le streaming bypasse L1/L2/L3.
- On ne supporte pas encore Anthropic/OpenAI dans nos benchmarks A/B. Seul MiniMax-M3 a été mesuré. On attend des résultats similaires, mais on ne l'affirme pas.

### Record Session

Une feature du dashboard hosted qui permet de tagger toutes les requêtes faites avec une de vos virtual keys pendant une fenêtre de temps, pour revoir ensuite la décomposition complète par classe.

Le tagging se fait côté proxy via un lookup Redis : quand le dashboard démarre une session, il écrit `optitoken:session:vk:<vk>` dans Redis avec un TTL de 24h. Le proxy vérifie cette clé à chaque requête. Si elle est présente, elle écrase le `sessionId` de la RequestLog. Quand la session se termine, le dashboard supprime la clé. Pas besoin de toucher à l'agent — Hermes, Claude Code, curl brut, tout ce qui utilise la virtual key est enregistré de façon transparente.

L'implémentation est dans `proxy/internal/services/auth.go` (`LookupSessionTag`) et `proxy/internal/handlers/proxy.go` (fallback header → Redis).

### Limitations assumées

- **Pas de benchmark A/B sur Anthropic/OpenAI** : seul MiniMax-M3 a été mesuré en conditions réelles. Le 99.8% cache hit vient de ce seul provider.
- **Pas de padding forcé** : risque d'hallucination de l'agent, opt-in désactivé.
- **Pas de cache SSE** : les streams bypasse le cache, par design.
- **Pas de SDK Python/Node** : bring-your-own HTTP client.
- **Pas de multi-région** : le proxy est stateful (Redis + Postgres). Run single region.
- **Pas de certification GDPR** : on n'affirme rien. Le proxy tourne sur l'infra que vous choisissez. EU data residency est un choix de déploiement.

### Démarrage rapide

```bash
git clone https://github.com/dudutti/Optitoken
cd Optitoken
cp .env.example .env
docker compose up -d --build proxy
```

Vérification :

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}
```

### Licence

MIT. Le proxy est open source. Le dashboard hosted sur [optitoken.net](https://optitoken.net) est un produit commercial séparé.
