# Synapse Proxy — Inventaire complet des capacités (audit corrigé 2026-06-24)

> Cet audit corrige les sous-estimations de `VS_HEADROOM.md`.
> Le code de Synapse est plus riche que la première analyse ne
> l'a montré. On a déjà beaucoup de features que Headroom n'a
> pas (ou que Headroom a explicitement retirées pour des
> raisons qu'on n'a pas).
>
> **Total : 19 511 lignes de Go** sur le proxy (vs ~14 000 Rust
> + ~35 000 Python pour Headroom).

## 1. Capabilities déjà en place (audit corrigé)

### Cache multi-niveaux (4 niveaux)
| Niveau | Fichier | Statut | Référence Headroom |
|--------|---------|--------|--------------------|
| **L0 dedup** | `optiagent/dedup.go` (collapsing in-flight) | ✅ | ❌ pas de L0 |
| **L1 exact hash** | `optiagent/engine.go` (SHA-256) | ✅ | ❌ pas de L1 |
| **L2 semantic** | `optiagent/engine.go` + Rust BERT embedder | ✅ | Kompress (ML) |
| **L3 compression** | `optiagent/compressor.go` (CoT strip, tool dedup) | ✅ | SmartCrusher |
| **L3 cache-preserving** | `optiagent/prefix_split.go` (byte-exact prefix) | ✅ | CacheAligner (retiré PR-A2) |
| **CCR canonical** | `optiagent/hook_ccr_compress.go` (whitespace/CRLF) | ✅ | CCR |

**Insight clé** : on a **6 niveaux de cache**, Headroom en a
**2** (CCR + L3 compressif). Le **L3 cache-preserving** est
notre **différenciateur majeur** : on split le payload en prefix
byte-exact + tail compressible, ce qui préserve le provider KV
cache. **Headroom l'a retiré** (PR-A2 dans leur changelog)
parce que leur version mutait le system prompt — la nôtre
mutate **seulement les 2 derniers messages** (le tail), pas
le system prompt. C'est un design plus sûr.

### Firewall agentique (4 hooks actifs)
| Hook | Fichier | Fonction |
|------|---------|----------|
| **Fingerprint** | `optiagent/hook_fingerprint.go` + `tool_fingerprint.go` | Compte les retries identiques (tool, args), 429 si >4 en 30s |
| **Loop detection** | `optiagent/loop_detect.go` + `hook_loop_detection.go` | ZSET counter, kill switch au 3e call, cached response |
| **Tool filter** | `optiagent/hook_tool_filter.go` | Denylist + allowlist par VK |
| **Tool dedup** | `optiagent/tool_dedup.go` + `hook_tool_dedup.go` | Détecte read_file répétés, suggère cache hit |
| **Session CB** | `optiagent/hook_session_circuit_breaker.go` | Rate limit par session |
| **Agent discovery** | `optiagent/hook_agent_discovery.go` | SADD tools dans Redis, dashboard reads |
| **Model radar** | `optiagent/hook_model_radar.go` | Flags new models, alerts |
| **CCR Compress** | `optiagent/hook_ccr_compress.go` | Canonicalize avant hash |
| **Cache aligner** | `optiagent/hook_cache_aligner.go` | Detector-only UUID/JWT/timestamp |

**Total : 9 hooks** (j'avais dit 7 dans le 1er audit, j'en avais
raté 2 : CacheAligner, et probablement Fingerprint qui est
séparé en 2 fichiers). Le pipeline est **vraiment** riche.

### Compression L3 (déjà implémentée)
| Transform | Fichier | Équivalent Headroom |
|-----------|---------|---------------------|
| Strip `<thinking>`, `<thought>`, `<scratchpad>` | `compressor.go:thoughtRegex` | Partiel (LogCompressor) |
| Tool output dedup | `compressor.go` repeated-tool compaction | SmartCrusher (3 stratégies) |
| CoT pruning | `compressor.go` | SmartCrusher |
| Cache-preserving split | `prefix_split.go` | Live-zone dispatcher (Anthropic) |
| Marshal déterministe | `marshal_deterministic.go` | (implicite dans Rust) |
| Compaction hint injection | `compaction_hint.go` | (pas d'équivalent) |

**Insight** : on a une vraie compression L3, pas juste CCR. Le
`CompressPayload` est l'équivalent simplifié du SmartCrusher de
Headroom (sans la classification par type de tableau, mais
avec le split cache-preserving qu'ils n'ont plus).

### MCP server (14 tools)
| Tier | Tools | Fichier |
|------|-------|---------|
| **Free** (4) | `synapse_chat_completions`, `synapse_list_models`, `synapse_cache_stats`, `synapse_savings_summary` | `internal/mcp/tools_free.go` |
| **Paid** (10) | `synapse_run_benchmark`, `synapse_list_virtual_keys`, `synapse_create_virtual_key`, `synapse_get_quotas`, `synapse_list_alerts`, `synapse_set_alert_rule`, `synapse_export_logs`, `synapse_start_session`, `synapse_stop_session`, `synapse_list_sessions` | `internal/mcp/tools_paid.go` |

**Insight** : on a un MCP server complet, **stdio + HTTP**, 2
tiers (free + paid). Headroom a 3 tools MCP (compress, retrieve,
stats). **On a 14 tools vs 3**.

### Multi-agent awareness
| Feature | Fichier | Équivalent Headroom |
|---------|---------|---------------------|
| Agent auto-discovery (SADD tools) | `optiagent/hook_agent_discovery.go` | ❌ |
| Tool fingerprint (par agent) | `optiagent/tool_fingerprint.go` | ❌ |
| Model radar (new model detection) | `optiagent/hook_model_radar.go` | ❌ |
| Per-VK rate limit (session CB) | `optiagent/hook_session_circuit_breaker.go` | ❌ |

### Sécurité
| Feature | Statut | Référence |
|---------|--------|-----------|
| AES-256-GCM key encryption | ✅ `internal/services/crypto.go` | ❌ (headroom ne stocke pas de clés) |
| Bearer token auth (sk-opti-...) | ✅ `internal/handlers/proxy.go` | ❌ |
| Redis avec `requirepass` (corrigé aujourd'hui) | ✅ `internal/db/redis.go` | ❌ |
| Bcrypt password dashboard | ✅ | ❌ |

### Storage
| Type | Usage | Fichier |
|------|-------|---------|
| Redis (L1/L2 cache, counters, denylist) | ✅ | `internal/db/redis.go` |
| Postgres (RequestLog, Session, ApiKey, ProviderModel) | ✅ | `internal/db/postgres.go` |
| Redis Stack (VSS vector index) | ✅ | `docker-compose.local.yml` |
| SQLite (alternative local) | ❌ | `headroom/cache/backends/sqlite.py` |
| Anthropic cache API | ❌ | `headroom/cache/anthropic.py` |

## 2. Ce qu'on a en MOINS que Headroom (audit corrigé)

### Compression avancée
| Algo | Statut Synapse | Source Headroom | Effort |
|------|----------------|-----------------|--------|
| **SmartCrusher** (JSON array crushing) | ❌ (1 algo basique) | `crates/headroom-core/src/transforms/smart_crusher/` (11 fichiers) | 2-3 sem |
| **CodeCompressor** (tree-sitter AST) | ❌ | `headroom/transforms/code_compressor.py` | 2-3 sem (complexe) |
| **Kompress ML** (kompress-v2-base) | ❌ | HuggingFace model | 1 mois |
| **LogCompressor** (per-language traces) | ⚠️ (partiel : strip des tags seulement) | `crates/headroom-core/src/transforms/log_compressor.rs` | 1-2 sem |
| **DiffCompressor** (git diff dedup) | ❌ | `crates/headroom-core/src/transforms/diff_compressor.rs` | 1 sem |
| **SearchCompressor** (RAG results) | ❌ | `headroom/transforms/search_compressor.py` | 1-2 sem |
| **HtmlExtractor** | ❌ | `headroom/transforms/html_extractor.py` | 1 sem |
| **SpreadsheetIngest** | ❌ | `headroom/transforms/spreadsheet_ingest.py` | 1 sem |
| **TabularIngest** | ❌ | `headroom/transforms/tabular_ingest.py` | 1 sem |
| **AnchorSelector** | ❌ | `headroom/transforms/anchor_selector.py` | 1 sem |
| **AdaptiveSizer** | ❌ | `headroom/transforms/adaptive_sizer.py` | 1 sem |
| **TagProtector** | ❌ | `headroom/transforms/tag_protector.py` | 3-5 jours |
| **ErrorDetection** | ❌ | `headroom/transforms/error_detection.py` | 1 sem |

**Total** : 13 algos Headroom qu'on n'a pas. Mais le plus
**impactant** est **SmartCrusher** (40-70% savings sur JSON
arrays d'agent). C'est le **P1 critique**.

### Live-zone dispatcher
| Provider | Headroom | Synapse | Effort |
|----------|----------|---------|--------|
| **Anthropic Messages** | ✅ `compress_anthropic_live_zone` | ⚠️ (L3 cache-preserving partial) | 1 mois (refacto complète) |
| **OpenAI Chat Completions** | ✅ `compress_openai_chat_live_zone` | ❌ | 1 mois |
| **OpenAI Responses** | ✅ `compress_openai_responses_live_zone` | ❌ | 1 mois |

**Insight** : on a **1/3 des dispatchers** (le cache-preserving
fait partie du travail mais n'est pas provider-aware). Le port
Rust de Headroom fait **3 providers**, on est générique.

### Cross-agent memory
| Feature | Headroom | Synapse | Effort |
|---------|----------|---------|--------|
| **Shared store** between agents | ✅ `headroom/memory/` | ❌ | 2-3 sem |
| **Auto-dedup** | ✅ | ❌ | inclus |
| **MCP tool** (`headroom_memory_*`) | ✅ 4 tools | ❌ | 1 sem |

### Output token reduction
| Feature | Headroom | Synapse | Effort |
|---------|----------|---------|--------|
| **Measure output savings** | ✅ (avec holdout 10%) | ❌ | 1 sem |
| **`headroom_learn --verbosity`** | ✅ (mine failures) | ❌ | 2 sem |
| **Confidence interval reporting** | ✅ (95% CI) | ❌ | 1 sem |

### Realtime / WebSocket
| Feature | Headroom | Synapse | Effort |
|---------|----------|---------|--------|
| **WebSocket proxy** | ✅ `crates/headroom-proxy/src/websocket.rs` (246 lines) | ❌ | 1-2 sem |

### CLI / Library mode
| Feature | Headroom | Synapse | Effort |
|---------|----------|---------|--------|
| **Library: `from headroom import compress`** | ✅ Python + TypeScript | ❌ | 2 sem |
| **CLI: `headroom wrap claude|codex|cursor`** | ✅ | ❌ | 1-2 sem |
| **Copilot subscription** | ✅ `headroom copilot-auth login` | ❌ | 2 sem |

### Other
| Feature | Headroom | Synapse | Effort |
|---------|----------|---------|--------|
| **Bedrock / Vertex** | ✅ | ❌ | 2-3 sem |
| **Image compression** | ✅ `headroom/image/` | ❌ | 2 sem |
| **Per-tool compression hints** | ✅ | ❌ | 1 sem |
| **TOIN (cross-user pattern learning)** | ✅ | ❌ | 3-4 sem |

## 3. Forces uniques de Synapse (à préserver et amplifier)

1. **6 niveaux de cache** (L0→L3 cache-preserving + CCR) vs 2
   chez Headroom. Le **L0 dedup** est unique.
2. **9 hooks** dans un pipeline extensible (Headroom a un
   pipeline fixe de transforms).
3. **Firewall agentique** : 4 hooks dédiés (fingerprint, loop,
   tool filter, tool dedup). Headroom n'a **rien** pour ça.
4. **14 MCP tools** (vs 3 chez Headroom).
5. **AES-256-GCM** pour les clés upstream (Headroom ne stocke
   pas de clés).
6. **Multi-tenant virtual keys** avec quotas, alertes, billing
   (Headroom est mono-utilisateur).
7. **Cache-preserving L3** : on l'a, Headroom l'a retiré.
8. **Marshal déterministe** : explicite et testé, pas un effet
   de bord du compilateur Rust.
9. **Session circuit breaker** : rate limit par session.

## 4. Roadmap d'enrichissement (révisée, par valeur)

### P0 — Quick wins à fort impact (< 1 sem chacun)
- [ ] **CCR Retrieve/Store** : ferme la boucle CCR (sans ça, on
      calcule le hash mais on ne l'utilise pas). 3-4 jours.
- [ ] **LogCompressor per-language** : port simplifié du
      LogCompressor Headroom (garder 3 premiers + 3 derniers
      frames, dédupe messages identiques, préserver traces
      chaînées). 3-5 jours.
- [ ] **Output token reduction** : mesure + reporting (le
      différenciateur Headroom le plus simple à porter). 1 sem.
- [ ] **TagProtector** : protège les tags HTML/Markdown
      importants pendant la compression (évite la troncature de
      `<system>` ou `<important>`). 3-5 jours.

### P1 — SmartCrusher simplifié (2-3 sem)
- [ ] **3 stratégies de base** (au lieu des 11 fichiers
      Headroom) :
  - Number array crush (>50 nombres → min/max/avg/count)
  - String array crush (catégorisation par similarité)
  - Object array crush (par hash, garder N représentatifs)
- [ ] **AnchorSelector** intégré (champs `id`/`name`/`key` ne
      sont jamais crushés)
- [ ] Tests contre les fixtures Headroom (parity check)

### P2 — Multi-provider live-zone (1-2 mois)
- [ ] **Provider detection** : OpenAI Chat / OpenAI Responses /
      Anthropic Messages / MiniMax
- [ ] **Live-zone par provider** : identifier le bloc mutable
      sans toucher au system prompt ni au tools
- [ ] **Compression sélective** : ne compresser que le bloc
      mutable, préserver le reste byte-exact

### P3 — Library + CLI mode (2-3 mois)
- [ ] **SDK Go** : `import "synapse-proxy/sdk"` avec
      `compress(payload)`, `retrieve(hash)`, `stats()`
- [ ] **SDK Python** : wrapper via gRPC
- [ ] **CLI `synapse wrap claude|codex|cursor`** : injection
      d'env vars + config file
- [ ] **Copilot subscription** : OAuth flow

### P4 — Cross-agent memory + WebSocket (3-4 mois)
- [ ] **Memory store Redis-backed** : déduplication SHA256
- [ ] **Memory MCP tools** : `synapse_memory_store`,
      `synapse_memory_recall`
- [ ] **WebSocket proxy** : port de `headroom-proxy/websocket.rs`
- [ ] **Realtime streaming compression** : compresser les
      chunks au fil de l'eau

## 5. Métriques de succès (révisées)

| Sprint | Métrique | Cible |
|--------|----------|-------|
| P0 (1 sem) | CCR hit rate (Retriev+Store) | ≥ 30% sur traffic réel |
| P0 (1 sem) | Output tokens saved | ≥ 10% |
| P0 (1 sem) | Log compression rate (sur tool outputs) | ≥ 70% |
| P1 (3 sem) | SmartCrusher sur JSON arrays | ≥ 50% token reduction |
| P2 (2 mois) | Live-zone cache hit (provider) | ≥ 80% préservés |
| P3 (3 mois) | SDK adoption (>= 1 intégration tierce) | 1+ |
| P4 (4 mois) | WebSocket support | en place |

## 6. Conclusion révisée

**Synapse Proxy est plus avancé que Headroom sur** :
- la **profondeur de cache** (6 niveaux vs 2)
- le **firewall agentique** (absent chez Headroom)
- le **multi-tenant** (absent chez Headroom)
- le **MCP server** (14 tools vs 3)
- la **sécurité** (chiffrement clés, Redis auth, bearer)

**Headroom est plus avancé que Synapse sur** :
- la **largeur de compression** (13 transforms vs 1)
- le **multi-provider live-zone** (3 providers vs 0)
- le **library mode** (Python/TS inline)
- le **CLI wrapping** d'agents tiers
- la **cross-agent memory**

**Le positionnement stratégique** : Synapse est le **gateway
d'entreprise** (sécurité, multi-tenant, firewall) ; Headroom est
le **tooling developer** (compression riche, library). Notre
**complémentarité** est possible (SDK Synapse pourrait appeler
Headroom en backend, ou vice versa).

**Action immédiate** : P0 en 1 sem (CCR ferme la boucle, Log
compression, Output reduction, Tag protector). C'est là qu'on
récupère le plus de valeur ajoutée avec le moins d'effort, sur
des briques qui sont déjà en place.
