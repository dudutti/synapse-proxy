# Synapse Proxy vs Headroom — Audit comparatif (2026-06-24)

> Document d'analyse comparative pour éclairer les décisions
> d'évolution de Synapse Proxy. Le code de Headroom est dans
> `G:/Optitoken/headroom/` (clone local de l'open source).
>
> Date de l'audit : 2026-06-24

## 1. Positionnement

| Aspect | Synapse Proxy | Headroom |
|--------|---------------|----------|
| **Positionnement** | Cache + Firewall + Optimization LLM gateway | Context compression layer |
| **Pipeline principal** | L1 hash → L2 semantic → L3 cache → CCR canonicalization | ContentRouter → SmartCrusher/CodeCompressor/Kompress-base → CCR (originals) |
| **Hook pipeline** | 7 hooks (Fingerprint, ToolFilter, AgentDiscovery, ToolDedup, LoopDetection, ModelRadar, CCRCompress) | Pas de hook pipeline, transforms fixes |
| **Cible** | Entreprises (multi-tenant, virtual keys, billing) | Developers (CLI, library, MCP) |
| **Souveraineté** | Local-first + cloud-ready | Local-first (data stays here) |
| **License** | Propriétaire (code source interne) | Apache 2.0 |
| **Langage** | Go (proxy) + Next.js (dashboard) + Rust (embedder) | Rust (core) + Python (binding) + TypeScript (npm) |
| **Lignes de code** | ~10k Go + ~5k Next.js + ~500 Rust | ~10k Rust + ~30k Python + ~5k TS |

## 2. Comparatif fonctionnel

### Cache (L1 / L2 / L3)
| Feature | Synapse | Headroom |
|---------|---------|----------|
| L1 exact hash | ✅ Redis | ❌ Pas de L1 (focus compression) |
| L2 semantic (embedding) | ✅ Rust BERT embedder | ❌ |
| L3 compression (CCR-like) | ✅ CCR CompressHook (8 tests) | ✅ CCR réversible (retrouve l'original) |
| CacheAligner (KV cache stability) | ❌ | ✅ `crates/headroom-core/src/cache_control.rs` |

**Gap pour Synapse** : implémenter un **CacheAligner** qui détecte le drift
de la structure du prompt (ajout/retrait d'outils, changement de modèle)
et invalide sélectivement le L2 cache. Le `cache_stabilization::drift_detector`
de Headroom est une référence (voir `crates/headroom-core/src/cache_stabilization/drift_detector.rs`).

### Compression
| Algo | Synapse | Headroom |
|------|---------|----------|
| Whitespace canonicalization | ✅ CCR CompressHook (3 transforms) | ✅ (implicite dans tous) |
| JSON smart compression | ❌ | ✅ `smart_crusher/` (11 fichiers) |
| Code AST compression | ❌ | ✅ `code_compressor.py` |
| Diff compression | ❌ | ✅ `diff_compressor` |
| Log compression | ❌ | ✅ `log_compressor` |
| Search/RAG compression | ❌ | ✅ `search_compressor` |
| HTML extraction | ❌ | ✅ `html_extractor` |
| Spreadsheet ingest | ❌ | ✅ `spreadsheet_ingest` |
| Kompress ML model | ❌ | ✅ `kompress-v2-base` (HuggingFace) |
| Live-zone dispatcher | ❌ | ✅ `live_zone.rs` (Anthropic-specific) |

**Gap majeur** : on a fait **1/9 algos de compression**. Le
**SmartCrusher** de Headroom est une référence : il classifie le contenu,
sélectionne les ancres importantes, protège les tags HTML, applique des
contraintes de contexte. C'est **~3000 lignes** de code.

### Output reduction
| Feature | Synapse | Headroom |
|---------|---------|----------|
| Output token reduction | ❌ | ✅ `headroom_learn --verbosity` (mesuré) |
| Confidence interval reporting | ❌ | ✅ (CI 95% sur les savings) |
| Holdout control group | ❌ | ✅ `HEADROOM_OUTPUT_HOLDOUT=0.1` |

**Gap** : ajouter un module d'output reduction qui :
1. Mesure les savings input ET output séparément
2. Apprend la verbosity préférée par user/agent
3. Reporte avec intervalle de confiance (mesuré ou estimé)

### Memory & Learning
| Feature | Synapse | Headroom |
|---------|---------|----------|
| Cross-agent memory | ❌ | ✅ (Claude ↔ Codex ↔ Gemini, dedup auto) |
| `headroom learn` (mine failures) | ❌ | ✅ écrit dans `CLAUDE.md` / `AGENTS.md` |
| Failed-session analysis | ❌ | ✅ `audit/maturation.py` |

**Gap** : pas critique pour le proxy d'entreprise mais **différenciateur**
sur le marché. À considérer en V2.

### Firewall / Security
| Feature | Synapse | Headroom |
|---------|---------|----------|
| Loop detection (kill switch) | ✅ `LoopDetectionHook` (kill au 3e call) | ❌ (pas le scope) |
| Tool filter (denylist/allowlist) | ✅ `ToolFilterHook` | ❌ |
| Session circuit breaker | ✅ `SessionCBHook` | ❌ |
| Tool dedup | ✅ `ToolDedupHook` | ❌ |
| Agent discovery | ✅ `AgentDiscoveryHook` (SADD tools) | ❌ |
| Model radar (new model detection) | ✅ `ModelRadarHook` | ❌ |
| Provider-agnostic | ✅ | ✅ |

**Avantage Synapse** : on a **un vrai firewall agentique** que Headroom
n'a pas (et c'est le USP de la landing page).

### Agent Compatibility
| Agent | Synapse | Headroom |
|-------|---------|----------|
| Claude Code | ✅ (via MCP + `/v1/chat/completions`) | ✅ `headroom wrap claude` |
| Codex | ✅ (idem) | ✅ |
| Cursor | ✅ | ✅ |
| Aider | ✅ | ✅ |
| OpenAI SDK | ✅ | ✅ |
| LangChain / LlamaIndex | ✅ | ✅ (`agno`, `langchain` extras) |
| GitHub Copilot subscription | ❌ | ✅ `headroom copilot-auth login` |
| Cortex Code | ❌ | ✅ |

### Dashboard & UX
| Feature | Synapse | Headroom |
|---------|---------|----------|
| Live dashboard | ✅ (5s auto-refresh, charts Canvas) | ✅ `headroom dashboard` |
| Playground A/B test | ✅ | ❌ |
| Request Explorer | ✅ (`/explorer`) | ❌ |
| Alert Rules | ✅ (`/alerts`) | ❌ |
| Multi-tenant (virtual keys) | ✅ | ❌ |
| Pricing Coverage matrix | ✅ (`/pricing`) | ❌ |
| Session History | ✅ (`/sessions`) | ❌ |

**Avantage Synapse** : dashboard d'entreprise complet. Headroom a
un dashboard mais sans le multi-tenant.

### Deployment
| Feature | Synapse | Headroom |
|---------|---------|----------|
| Docker Compose | ✅ | ✅ `docker-compose.yml` |
| Embedded binary | ❌ (proxy + dashboard séparés) | ✅ (1 binaire `headroom`) |
| Library mode | ❌ | ✅ `from headroom import compress` |
| MCP server | ❌ | ✅ `headroom_compress`, `headroom_retrieve` |
| Per-agent wrapping | ❌ | ✅ `headroom wrap <agent>` |

## 3. Architecture

### Synapse Proxy
- **Proxy** : Go monolithique (1705 lignes pour le router principal)
- **Embedder** : Rust (MiniLM) compilé en `.a` static linké au binaire Go
- **Dashboard** : Next.js 14 (React, App Router) + Prisma
- **Storage** : Redis 7.4 (L1/L2 cache) + Postgres (metadata) + Rust VSS (vector)
- **Auth** : NextAuth (bcrypt) pour le dashboard, Bearer token pour le proxy
- **Encryption** : AES-256-GCM pour les clés API upstream

### Headroom
- **Core** : Rust (parity-bound avec Python)
- **Python binding** : `headroom-py` (PyO3)
- **Proxy** : Rust (axum) avec WebSocket support
- **CLI** : Python (Click)
- **Storage** : SQLite (default) + Anthropic cache
- **Multi-provider** : `anyllm`, `litellm` backends
- **Multi-modal** : ContentRouter détecte le type et route

## 4. Roadmap d'amélioration Synapse (par priorité)

### P0 (à faire cette semaine) — Sécurité prod
- [x] Sécuriser Redis local (requirepass + bind 127.0.0.1) ✅ commit `837ce2ea`
- [x] Fix sync_keys.js pour lire le password ✅ commit `bbf1ba4d`
- [x] Documenter SECURITY.md ✅
- [ ] **Créer `docker-compose.prod.yml`** : Redis sans port mapping (Docker-internal only), POSTGRES bind 127.0.0.1
- [ ] **Procédure de déploiement** : `docs/DEPLOY.md` avec checklist (firewall host, requirepass, bind, etc.)
- [ ] **Monitoring** : alerte si Redis bind sur 0.0.0.0 ou si requirepass absent

### P1 (à faire ce mois) — Manques fonctionnels
- [ ] **CCR RetrieveHook** : lookup en Redis sous la clé `ccr:<hash>`, short-circuit si hit
- [ ] **CCR StoreHook** : store la réponse dans `ccr:<hash>` avec TTL
- [ ] **CacheAligner** : implémenter `internal/handlers/cache_stabilization.rs` (port Rust de Headroom) ou Go equivalent
- [ ] **SmartCrusher** : portage simplifié (3-5 catégories JSON au lieu de 11 fichiers). Classifie et compresse.
- [ ] **Output token reduction** : mesurer + reporter (méthodologie Headroom)

### P2 (à faire ce trimestre) — Différenciateurs
- [ ] **Live-zone dispatcher** (Anthropic) : ne compresser que les blocs mutables
- [ ] **Cross-agent memory** : Redis-backed, déduplication SHA256
- [ ] **headroom_learn equivalent** : miner les failed sessions (logs RequestLog avec status >= 500), proposer corrections dans l'admin
- [ ] **MCP server** : exposer `synapse_compress`, `synapse_retrieve` comme tools MCP
- [ ] **GitHub Copilot subscription mode** : `synapse wrap copilot` (nécessite OAuth flow)

### P3 (à faire ce semestre) — Marché
- [ ] **Embedder ML upgrade** : passer de `MiniMax-M2.7` à `kompress-v2-base` de Headroom (meilleure qualité)
- [ ] **WebSocket support** (Headroom l'a, Synapse non)
- [ ] **Per-agent wrapping CLI** : `synapse wrap claude|codex|cursor`
- [ ] **Bedrock / Vertex** : multi-provider routing
- [ ] **LangChain / LlamaIndex extras** : intégrations officielles

## 5. Forces uniques de Synapse à préserver

1. **Hook pipeline** (7 hooks, extensible) : aucun concurrent n'a ça
2. **Multi-tenant virtual keys** : essentiel pour le marché entreprise
3. **Dashboard enterprise** (Request Explorer, Pricing Coverage, Alert Rules) : Headroom a juste un dashboard simple
4. **Firewall agentique** (loop detection, tool dedup, kill switch) : USP clairement identifié
5. **Souveraineté** : local-first + Docker self-hostable

## 6. Forces uniques de Headroom à importer

1. **Compression multi-algo** (SmartCrusher, CodeCompressor, Kompress) : 25 transforms vs 1
2. **CCR réversible** : le LLM peut `retrieve` l'original via MCP
3. **Output token reduction** : économie mesurée
4. **Cross-agent memory** : différenciateur marché
5. **Library mode** : `from synapse import compress` (Python/TS inline)

## 7. Estimation de l'effort

| Phase | Effort | Risque | ROI |
|-------|--------|--------|-----|
| P0 sécu | 1 jour | Faible (changement conf) | Critique (incident prod) |
| P1 CCR Retrieve/Store | 1 semaine | Faible (TDD strict) | Élevé (5-15% savings additionnel) |
| P1 SmartCrusher | 2 semaines | Moyen (logique complexe) | Élevé (10-30% sur JSON-heavy) |
| P2 Live-zone | 1 mois | Élevé (Anthropic-specific) | Moyen (5-10% sur Anthropic) |
| P2 MCP server | 1 semaine | Faible | Élevé (nouveau channel) |
| P3 ML upgrade | 2 mois | Élevé (re-train) | Moyen (qualité > quantité) |

**Recommandation** : focus sur **P0 + P1** ce mois-ci. C'est là qu'on
récupère le plus de valeur avec le moins de risque. Le **P2** peut
attendre le retour client sur les features P1.
