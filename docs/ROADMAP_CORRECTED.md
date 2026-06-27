# Synapse Proxy — Roadmap corrigée 2026-06-24

> Stratégie d'enrichissement pour atteindre (et dépasser) le
> niveau de Headroom. Basé sur l'audit complet dans
> `docs/AUDIT_V2.md` (qui corrige `VS_HEADROOM.md` en montrant
> que Synapse a déjà beaucoup de features que le 1er audit
> avait sous-estimées).

## Stratégie : jouer nos forces, combler les vrais gaps

**On est déjà en avance sur** :
- la profondeur de cache (6 niveaux vs 2)
- le firewall agentique (absent chez Headroom)
- le multi-tenant (absent chez Headroom)
- le MCP server (14 tools vs 3)
- la sécurité (chiffrement clés, Redis auth, bearer tokens)
- la cache-preserving L3 (Headroom l'a retiré, on l'a)

**On doit combler** :
- les transforms de compression (1 vs 13 chez Headroom)
- le multi-provider live-zone (1/3 providers couverts)
- le library/SDK mode (absent)
- l'output token reduction (absent)
- la cross-agent memory (absent)

## P0 — Quick wins (1 semaine)

### P0.1 — CCR Retrieve + Store
**Pourquoi** : ferme la boucle CCR. Le CompressHook calcule le hash
mais on ne l'utilise jamais. Sans retrieve, CCR ne fait rien.

**Effort** : 3-4 jours
- `optiagent/hook_ccr_retrieve.go` (BeforeRequest, priority 750) :
  `GET ccr:<hash>` dans Redis, short-circuit 200 si hit
- `optiagent/hook_ccr_store.go` (AfterResponse, priority 850) :
  `SETEX ccr:<hash> 3600 <response>` si status 200
- Tests : 8 cas (hit, miss, redis error fail-open, TTL, store skip non-200, no-overwrite, end-to-end)

### P0.2 — LogCompressor per-language
**Pourquoi** : les tool outputs en stack traces sont 70% du traffic
agent. Les strip `<thinking>` actuel est trop basique. Le
LogCompressor Headroom (3 premiers + 3 derniers frames, dedup,
preserves traces chaînées) économise 70%.

**Effort** : 3-5 jours
- `optiagent/hook_log_compressor.go` (AfterResponse, priority 900)
- Détection : ligne contenant "Error" + "at " (Python/JS) ou
  "panicked at" (Rust) → mode stack trace
- Stratégies : keep first N + last M, dedup identical errors,
  preserve "During handling of the above exception, another
  exception occurred" chained traces
- Tests : 6 cas (Python trace, JS trace, Rust panic, dedup,
  chained, no-op sur log normal)

### P0.3 — Output token reduction
**Pourquoi** : Headroom économise 30%+ sur les OUTPUT tokens aussi,
pas juste l'input. C'est un **gain de 30% sur la facture totale**
qu'on rate. C'est aussi un **différenciateur mesurable** (vs les
estimations).

**Effort** : 1 semaine
- `internal/services/output_reduction.go` — mesure baseline
- Pour chaque réponse, compter les tokens de sortie (tiktoken
  déjà wire)
- Stockage : `synapse:stats:output_saved:<vk>:<day>` (HINCRBY)
- Reporting : nouvelle card dashboard "Output Tokens Saved" avec
  intervalle de confiance 95% (mesuré si holdout 10%, estimé
  sinon via `HEADROOM_OUTPUT_HOLDOUT` env equivalent)
- Tests : 5 cas (mesure, holdout control group, CI calculation,
  Redis counter, dashboard card)

### P0.4 — TagProtector
**Pourquoi** : quand le L3 compress tronque, il peut couper dans
un tag HTML important (`<important>`, `<system>`, `<thinking>`).
Le résultat est du HTML cassé. Le TagProtector Headroom protège
ces spans avant la compression.

**Effort** : 3-5 jours
- `optiagent/hook_tag_protector.go` (BeforeRequest, priority 750)
- Walk les content strings, détecter les tags "importants" :
  `<important>`, `<system>`, `<security>`, `<tool_result>`,
  `<antml:*>` (Anthropic-specific)
- Pour chaque span : générer un token de remplacement (sha256
  short), remplacer par `<<TAG_PROTECTED:<token>>`, stocker le
  span original dans Redis `tag:protected:<token>` avec TTL
- Après L3 compress : remplacer `<<TAG_PROTECTED:<token>>` par
  le span original (via un nouveau hook AfterL3 priority 950)
- Tests : 6 cas (protect single tag, nested tags, no tags,
  Anthropic antml, restore, no-overwrite)

## P1 — Compression avancée (1 mois)

### P1.1 — SmartCrusher simplifié
**Pourquoi** : Headroom a 11 fichiers pour SmartCrusher. On n'a
pas besoin de tout, juste 3 stratégies + anchor selector. C'est
le **plus gros potentiel de savings** sur les tool outputs JSON.

**Effort** : 2-3 semaines
- `optiagent/hook_smart_crusher.go` (BeforeRequest, priority 850,
  AVANT le cache-preserving split)
- **Stratégie 1 : Number array crush.** Si `len(array) > 50` et
  tous les éléments sont des nombres, garder
  `{count, min, max, avg, sum, sample: [first 5]}` et un tag
  `<<SMART_CRUSHED:NUMBERS:n_offloaded>>`
- **Stratégie 2 : String array crush.** Si `len(array) > 50` et
  tous des strings, catégoriser par similarité (premier char,
  longueur, présence d'URL/UUID), garder 5 par catégorie + count
- **Stratégie 3 : Object array crush.** Si `len(array) > 50` et
  tous des objets avec les mêmes keys, garder les 5 plus
  "pertinents" (par hash du `id`/`name`/`key`, ou par défaut
  le SHA256 de la serialization) + count
- **AnchorSelector** intégré : ne JAMAIS crush un champ dont la
  key est dans {id, name, key, type, status, role, hash,
  created_at, updated_at, error}
- **CCR marker** : `{_ccr_dropped: "<<ccr:HASH N_rows_offloaded>>"}`
  sur la dernière position pour retrieval (cf Headroom)
- **Parity tests** : 12 cas contre les fixtures Headroom (compat
  byte-pour-byte quand le mode est désactivé)

### P1.2 — LogCompressor extensions
- Détection multi-language (Python, JS, Rust, Go)
- Pattern matching pour les timestamps ISO dans les logs
  (garder le 1er et le dernier, drop les intermédiaires)
- Tests : 8 cas

## P2 — Multi-provider live-zone (2 mois)

### P2.1 — Provider detection
**Pourquoi** : la cache-preserving actuelle est générique
(split sur les 2 derniers messages). Le live-zone Headroom est
**provider-aware** : il connaît la structure Anthropic
(`system` en dehors de `messages`, `tools` immuable) vs OpenAI
Chat (`system` dans `messages[0]`, `tools` au top level).

**Effort** : 2 semaines
- `optiagent/provider_shape.go` — détection + struct
  `ProviderShape { Kind, SystemBlock, ToolsBlock, LiveZoneStart }`
- `Kind` = OpenAIChat | OpenAIResponses | AnthropicMessages | MiniMaxChat
- Tests : 10 fixtures (4 providers × 3 variations)

### P2.2 — Live-zone par provider
- `optiagent/hook_live_zone.go` (BeforeRequest, priority 770)
- Pour Anthropic : `messages[0..N-K]` = immuable (cache hit),
  `messages[N-K..]` = live zone
- Pour OpenAI Chat : idem mais `messages[0]` peut être system
- Pour OpenAI Responses : utiliser `input` au lieu de
  `messages`, items séparés par type
- Pour MiniMax : équivalent OpenAI Chat
- Tests : 12 cas (3 par provider : immuable byte-exact, live
  zone compressée, edge cases)

## P3 — SDK + CLI (3 mois)

### P3.1 — SDK Go
- `sdk/` package (nouveau) avec API simple :
  ```go
  client, _ := sdk.New("sk-opti-...")
  resp, _ := client.Chat(ctx, messages, model)
  stats, _ := client.Stats(ctx)
  ```
- Tests : 6 cas

### P3.2 — SDK Python (gRPC)
- Bindings gRPC via `protoc`
- `pip install synapse-proxy-sdk` (pré-built wheel)
- Tests : 4 cas

### P3.3 — CLI `synapse wrap`
- Lance le proxy en local, injecte `OPENAI_BASE_URL=http://localhost:8080/v1`
- Pour Claude Code : modifie le settings.json
- Pour Codex : wrap via `codex --proxy http://localhost:8080`
- Tests : manuels (sortie du scope CI)

## P4 — Cross-agent memory + WebSocket (4 mois)

### P4.1 — Cross-agent memory
- `optiagent/memory_store.go` — Redis-backed dedup
- Hash SHA256 du prompt final → `mem:agent1:sha256(...)` = response
- Lookup avant l'appel upstream
- TTL configurable (default 1h)
- 4 MCP tools : `synapse_memory_store`, `synapse_memory_recall`,
  `synapse_memory_list`, `synapse_memory_clear`
- Tests : 8 cas

### P4.2 — WebSocket proxy
- Port de `headroom-proxy/websocket.rs` (246 lines) en Go
- Nécessite gorilla/websocket ou nhooyr.io/websocket
- Tests : 6 cas (handshake, message streaming, close, ping/pong,
  binary frames, errors)

## Métriques de succès

| Sprint | Métrique | Cible |
|--------|----------|-------|
| P0 (1 sem) | CCR hit rate (Retrieve+Store) | ≥ 30% |
| P0 (1 sem) | Output tokens saved | ≥ 10% |
| P0 (1 sem) | Log compression rate | ≥ 70% sur tool outputs |
| P0 (1 sem) | Tag protection rate | 100% (no false negatives) |
| P1 (1 mois) | SmartCrusher sur JSON arrays | ≥ 50% reduction |
| P2 (2 mois) | Provider live-zone hit rate | ≥ 80% byte-exact |
| P3 (3 mois) | SDK adoption | 1+ intégration tierce |
| P4 (4 mois) | Memory hit rate | ≥ 20% (cross-agent) |
| P4 (4 mois) | WebSocket streaming | latency p99 < 100ms |

## Ce qu'on NE FAIT PAS (out of scope)

- **CodeCompressor (tree-sitter)** : trop complexe pour le ROI.
  Le tool output contenant du code source est <5% du traffic.
- **Kompress ML model** : 500MB à télécharger, ré-entraînement
  nécessaire. Le L2 ONNX suffit pour 80% des cas.
- **Bedrock / Vertex** : pas la cible client actuelle.
- **Image compression** : hors scope B2B SaaS pour l'instant.
- **TOIN (cross-user pattern learning)** : nécessite une
  infrastructure de privacy (PII, RGPD) qu'on n'a pas.

## Décision immédiate

**Cette semaine** : focus sur **P0.1 (CCR Retrieve/Store)** et
**P0.2 (LogCompressor)**. Les 2 quick wins qui ferment la boucle
CCR et économisent 70% sur les tool outputs logs. Le P0.3
(output reduction) et P0.4 (tag protector) peuvent attendre la
semaine prochaine.

**Ce mois-ci** : P1.1 SmartCrusher (le plus gros potentiel).
**Ce trimestre** : P2.1-P2.2 live-zone multi-provider.
**Ce semestre** : P3 SDK + P4 memory.
