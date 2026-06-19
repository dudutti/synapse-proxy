# ðŸš€ Synapse Proxy Strategic Roadmap



> **Status:** **v1.x** (production, see [`CHANGELOG.md`](./CHANGELOG.md))

> This document tracks what we have shipped, what we have decided **not** to build, and what is next.



---



## âœ… Shipped in v1 (live on synapse-proxy.com)



| Feature | Commit / Doc | Notes |

|---|---|---|

| L1 exact cache (SHA-256 in Redis) | `proxy.go` | Sub-millisecond hit, poison-response guard |

| L2 semantic cache (ONNX + Redis) | `proxy.go`, `optiagent/engine.go` | Cosine tolerance per key, isolated per `virtualKey` |

| L3 structural compression | `optiagent/compressor.go` | Strips `<thought>`, minifies tool outputs, recurses on `messages[]` |

| Live telemetry with agent detection | `proxy.go` | Hermes, OpenClaw, Claude Code, LangChain, curl, Python SDK |

| Smart model aliasing | `proxy.go` | gemmaâ†’MiniMax-M3 silent route + re-stamp |

| Loop detection (3+ in 60s) | `optiagent/loop_detect.go` | With poison-cache guard |

| Tool-call dedup (file path detection) | `optiagent/tool_dedup.go` | Logs re-reads, surfaces redundancy metric |

| Compaction hint injection | `optiagent/compaction_hint.go` | Teaches the agent to work on summaries |

| Model Radar v1 (detect + sample) | `proxy/internal/workers/model_radar.go` | Flags new models, stores 10 raw samples in Redis |

| **FieldDiscoverer v1 (auto-detect fields)** | `proxy/internal/workers/field_discoverer.go` | â­ **Shipped.** Recursive JSON walk discovers `prompt_tokens` / `completion_tokens` paths without any LLM in the loop |

| Per-class cost savings (4 classes) | `proxy/internal/utils/savings.go` | input fresh / cache read / cache creation / output |

| Per-request `costSaved` persisted | `telemetry.go` | The widget reads this |

| Cache-poisoning guard (L1 + loop) | `proxy/internal/utils/cache_validation.go` | Empty content / `status_code != 0` rejected from both caches |

| Upstream app-error â†’ HTTP 4xx | `proxy.go` (in `streamResponse`) | Stops the agent from timing out on a poison body |

| Scope-aware multi-tenant cache | `proxy.go` (in `ProcessRequest`) | `personal` / `business` / `generic` auto-classification |

| `isolateCacheByUser` flag | `proxy.go` | User-scoped cache namespace |

| `benchmarkMode` per key | `proxy.go` | Side-by-side eval, with the dashboard's "Record session" |

| Zero-Log Mode (per-key) | `proxy/internal/utils/redactor.go` | Bodies redacted in-place, L1/L2/loop disabled, telemetry metadata-only |

| Zero-Log UI toggle in Settings | `dashboard/app/settings/page.tsx` | Confirms before activation, instant feedback |

| Auto-redis-seed on key create (with rollback) | `dashboard/app/api/keys/route.ts` | If Redis fails, DB row is deleted; no orphan keys |

| `MODEL_RADAR` SUPERADMIN API endpoint | `dashboard/app/api/admin/model-radar/route.ts` | Lists all radar entries, sample counts, discovered mappings |

| Pricing data per provider | `proxy/internal/db/pricing.go` | Used to compute the 4 per-class savings in USD |

| Redis hardening | `docker-compose.prod.yml` | `--maxmemory 512mb --maxmemory-policy allkeys-lru --bind 0.0.0.0 --protected-mode no` |

| Stream MAXLEN on telemetry | `telemetry.go` | Capped at 100k entries, O(1) amortized |

| **Playground v3** (side-by-side A/B) | `dashboard/app/playground/page.tsx` + `components/{MessageStats,Artifact,Sparkline}.tsx` | Stats per bubble (cache-level badge, tokens, latency, $ saved), 3-up A vs B comparison bar, inline SVG sparklines, Artifact Renderer (sandboxed iframe for HTML + Copy/Open/Download for code), Linked/Independent panels, Export session as JSON |

| AES-256-GCM encryption at rest | `proxy/internal/services/crypto.go` + `dashboard/app/api/{keys,models}/route.ts` | Real provider keys encrypted before write to Postgres + Redis, decrypted in-memory by the Go proxy at request time. Legacy plaintext keys in Redis still readable via GCM-open failure fallback |

| L0 in-flight request deduplication | `proxy/optiagent/dedup.go` + `proxy.go` | SETNX lock + Blob publish + Lua-script atomic release; followers tagged `cacheLevel=L0` in telemetry |

| `InitPricingSyncer` boot worker | `proxy/cmd/server/main.go` | Loads `ProviderModel` into in-memory cache on boot, refreshed every hour. Eliminates silent `$1/MTok` fallback that was applied to every cost-saving calculation since v1.4.0 |



---



## ðŸŸ¡ In progress / partially shipped



| Feature | Status | What's left |

|---|---|---|

| Dashboard "Model Radar" panel | API done, UI not started | Build a visual panel listing each model with `learning` / `ready` / `unknown` badges and an "Approve mapping" button for `SUPERADMIN` |

| Header `X-Synapse Proxy-Scope` for client-controlled scope override | Spec'd in `USE_CASES.md` (Cas #2), not yet implemented | ~1 day of work. Lets the chatbot decide `personal` / `business` per request instead of relying on the regex classifier. |



---



## ðŸš« Pistes écartées (et pourquoi)



| Piste | Raison |

|---|---|

| **Active L3 compression par LLM local** (phi-3-mini on CPU) | Adds +15s to TTFT. Ruins voice-to-voice. Already documented in `MODEL_RADAR.md` "Horizon v2" but the realistic horizon is "only if we get a GPU node". |

| **Visual semantic cache (CLIP for images)** | CPU-bound encoding blocks worker threads. |

| **gRPC for the ONNX embedder** | The HTTP overhead is ~2-3ms; not worth the deploy complexity. |

| **Auth-by-cookie for the SSE telemetry stream** | Currently uses NextAuth cookies correctly. The bug we hit (cookie missing in curl) was just our test, not a prod issue. |



---



## ðŸŽ¯ Next priorities (v1.5 / v2.0)



### ðŸ” Transparent Provider Fallback (self-healing)



If OpenAI returns 503 or 429, the proxy reroutes to Anthropic, translating the SSE stream on the fly to keep the OpenAI wire format. The client never knows the provider changed. ~2 days of work, only valuable once we have â‰¥2 provider backends wired in.



### ðŸš¦ Agent Kill-Switch & Budget Ceilings



If a key does more than N requests/hour, or the per-token cost exceeds X, the proxy starts returning HTTP 429 with a `Retry-After`. Today we only have `monthlyBudget`; the per-hour circuit-breaker is missing. ~2 days.



### ðŸ§  Sub-agent fan-out deduplication



When an orchestrator spawns N sub-agents with the same meta-prompt, all N identical meta-prompts hit the proxy. Today L1 catches them. A real "sub-agent routing" feature (like Token-Optimizer's) would have the orchestrator reuse a single completion across sub-agents. ~3 days, **only relevant for orchestrators that send >50 sub-agent calls/session**.



### ðŸ” BYOC (Bring Your Own Cloud)



The "install Synapse Proxy on your own infrastructure, send only logs to our dashboard" tier. Today we have the open-core proxy; the dashboard is SaaS-only. A stripped-down "telemetry sink" mode where the dashboard only ingests pre-aggregated metrics (no request bodies) would let us serve on-prem customers. ~5 days.



### ðŸ“œ Redis Lua Scripting for atomic cache writes



Replace the current "check then write" pattern with a single Lua call. Saves 1 TCP round-trip per cache miss. ~1 day, **only worth it if we measure 10ms+ Redis latency** (we don't today, Redis is on the same docker network).



### ðŸŽ›ï¸ Client-controlled scope override (header)



`X-Synapse Proxy-Scope: personal|business|generic` lets a chatbot platform control the cache namespace explicitly. Replaces the regex classifier. ~1 day.



---



## ðŸ›¡ï¸ Mitigating SWOT Weaknesses (recap)



| Weakness | Mitigation today | Planned |

|---|---|---|

| **L3 fragility** (regex-based) | Model Radar v1 + FieldDiscoverer auto-adapts to new models in 1-5 samples | Horizon v2: AST-based parser for L3 |

| **Zero-latency** (proxy adds ~100ms) | Bypass mode (`X-Bypass-Cache: true` per key) | Edge deployment via Cloudflare Workers for L1 only |

| **Privacy / BYOC** | Zero-Log Mode (auditable in source) | A pre-aggregated log shipping mode for the SaaS dashboard |

| **Single point of failure** (proxy) | Redis HA via Docker Swarm; ONNX service in standby | Multi-region active/active (out of scope before $1M ARR) |

| **Provider lock-in risk** (caching becomes redundant) | Smart routing + aliasing already cross-provider | Nothing to do; the L3 value is provider-agnostic |



---



## ðŸ§  Model Radar "” Auto-adaptation aux nouveaux modèles LLM



> Voir [MODEL_RADAR.md](./MODEL_RADAR.md) pour le plan technique complet. **v1 (detect + sample + FieldDiscoverer) is shipped.** The "dashboard panel" and the "auto-approve admin workflow" are still on the to-do list.



**Problème :** quand un provider LLM sort un nouveau modèle avec un format de réponse légèrement différent (ex: `input_tokens` au lieu de `prompt_tokens`), l'extraction du token usage échoue silencieusement â†’ télémétrie fausse, économies non calculées.



**Solution v1 (shipped) :** since we already have a dropdown of models per provider, we can detect that a never-seen model starts flowing through the proxy, collect N response samples, and auto-discover the correct JSON field paths by recursive traversal "” without an embedded LLM, without human intervention for the common cases.



- [x] `model_radar.go` "” detection + collection (LPUSH + LTRIM to 10 samples)

- [x] `field_discoverer.go` "” recursive JSON walk, scoring per field by name + value > 0

- [x] Integration in `proxy.go` (after `streamResponse` and in `TelemetryWorker`)

- [x] `GET /api/admin/model-radar` for `SUPERADMIN` (lists entries + sample counts + discovered mappings)

- [ ] Dashboard "Model Radar" panel with ðŸŸ¡ / ðŸŸ¢ / ðŸ”´ badges (UI not started)

- [ ] Manual approval flow (admin reviews the auto-discovered mapping before it goes live in production)



---



## ðŸŒ± Green AI "” Impact Environnemental



> *"Each token saved, that's less GPU running, less COâ‚‚ emitted. Synapse Proxy rationalizes over-consuming AI: the more tokens we compress, the more we reduce the global AI energy footprint "” and we plant trees."*



Ce n'est pas du greenwashing : réduire les tokens envoyés aux datacenters réduit réellement la consommation électrique des serveurs GPU. C'est structurellement vrai et vérifiable.



### Mécanique de contribution



- **1â‚¬ reversé tous les 50 000 requêtes optimisées** â†’ plantation d'arbres

- Partenaire cible : **Reforest'Action** (français, crédible, API disponible, ~0,50-1â‚¬/arbre en volume)

- Alternatives : One Tree Planted (1$ = 1 arbre, API + badge), Ecologi, Tree Nation (widget intégrable)



### Intégration dans le Dashboard (closed-source)



- Sur le **Globe terrestre** déjÃ  déployé : ajouter des **pictogrammes d'arbres ðŸŒ³** géolocalisés au fur et Ã  mesure des plantations

- Un compteur global : **"X arbres plantés grâce Ã  la communauté Synapse Proxy"**

- Contenu hautement partageable sur LinkedIn/Twitter â†’ **growth organique intégré au produit**

- Chaque utilisateur voit sa contribution personnelle : *"Grâce Ã  tes 12 000 requêtes optimisées ce mois, tu as contribué Ã  planter 0,24 arbres ðŸŒ±"*



### Actions concrètes



- [ ] Contacter Reforest'Action (reforestraction.com/entreprises) pour partenariat API

- [ ] Définir le seuil de déclenchement (ex: toutes les 50 000 req optimisées cumulées sur toute la plateforme)

- [ ] Ajouter compteur "tokens sauvés = COâ‚‚ évité (estimé en kg)" dans le dashboard

- [ ] Ajouter pictogrammes d'arbres sur le globe existant

- [ ] Créer page publique "/impact" : arbre planté + tokens sauvés + COâ‚‚ évité en temps réel



---



## â¸ï¸ Pistes écartées ou reportées



Ces idées sont excellentes en théorie mais nécessiteraient des instances GPU très coûteuses (AWS/GCP), ruinant la philosophie "Lean & Cheap" d'Synapse Proxy :



- âŒ **Active L3 Compression par LLM :** Utiliser un modèle local massif pour "résumer" le contexte avant de l'envoyer Ã  GPT-4. Sur un CPU classique, la génération prendrait +15 secondes, ruinant totalement le TTFT (Time To First Token).

- âŒ **Visual Semantic Cache (L2 Multi-modal) :** Analyser des images uploadées pour voir si elles sont similaires via `CLIP`. L'encodage visuel sur CPU bloque les threads du serveur.

- â¸ï¸ **LLM embarqué pour Model Radar v2 :** Un phi-3-mini (~2GB RAM) pour analyser des changements de format de `tool_calls` ou `reasoning_content`. Ã€ réévaluer selon la charge Hetzner une fois les autres features stables.


