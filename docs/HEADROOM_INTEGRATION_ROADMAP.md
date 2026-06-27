# Roadmap d'intégration Headroom → Synapse Proxy

> Stratégie pour intégrer les features Headroom manquantes dans
> Synapse Proxy. Basé sur l'audit `VS_HEADROOM.md` et l'analyse
> approfondie du code source de Headroom dans `G:/Optitoken/headroom/`.

## Vue d'ensemble — 9 quick wins identifiés

| # | Feature | Source Headroom | Effort | Impact | Risque |
|---|---------|-----------------|--------|--------|--------|
| 1 | **CacheAligner** (détection UUID/timestamp/JWT) | `headroom/transforms/cache_aligner.py` | 1-2 jours | Élevé | Faible |
| 2 | **CCR Retrieve/Store** (cache hit) | `crates/headroom-core/src/ccr/` | 3-4 jours | Élevé | Faible |
| 3 | **LogCompressor** (logs Python/JS/Rust) | `crates/headroom-core/src/transforms/log_compressor.rs` | 3-5 jours | Moyen | Moyen |
| 4 | **DiffCompressor** (git diffs) | `crates/headroom-core/src/transforms/diff_compressor.rs` | 2-3 jours | Moyen | Faible |
| 5 | **SmartCrusher simplifié** (3 stratégies au lieu de 11) | `crates/headroom-core/src/transforms/smart_crusher/` | 1-2 sem | Très élevé | Élevé |
| 6 | **Output token reduction** (mesure + CI) | `docs/proposals/output-token-reduction.md` | 1 sem | Élevé | Faible |
| 7 | **Cross-agent memory** (Redis dedup) | `headroom/memory/` | 1 sem | Moyen | Faible |
| 8 | **Live-zone dispatcher** (Anthropic) | `crates/headroom-core/src/transforms/live_zone.rs` | 1 mois | Élevé | Élevé |
| 9 | **MCP server** | `headroom/mcp_server.py` | 3-5 jours | Élevé | Faible |

## Priorisation recommandée

### Sprint 1 (cette semaine) — 4 quick wins

**Objectif** : combler le fossé sur les **détections** et le **cache hit**.

#### Quick Win #1 : CacheAligner Hook (1-2 jours)

**Pourquoi** : Headroom a détecté que la **system prompt** contient souvent
du contenu dynamique (UUIDs, timestamps, JWTs) qui invalide le KV cache
provider. Un simple détecteur + log warning est 90% de la valeur.

**Implémentation** :
- Créer `optiagent/hook_cache_aligner.go` (BeforeRequest, priority 700)
- Détecter dans `messages[0].content` (system) :
  - UUIDs : regex `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`
  - ISO 8601 : `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`
  - JWTs : `[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`
  - Hex hashes (MD5/SHA1/SHA256) : `\b[a-f0-9]{32,64}\b`
- Si détecté : `log.Warn` + `hctx.SetFeature("cache_aligner_warning", true)`
- NE PAS muter le prompt (leçon Headroom : ça invalide le cache hot zone)

**Tests TDD** (5 tests) :
- `TestCacheAlignerHook_DetectsUUIDInSystemPrompt`
- `TestCacheAlignerHook_DetectsISO8601Timestamp`
- `TestCacheAlignerHook_DetectsJWT`
- `TestCacheAlignerHook_NoFalsePositiveOnStaticPrompt`
- `TestCacheAlignerHook_DoesNotMutateSystemPrompt`

**Référence Headroom** : `headroom/transforms/cache_aligner.py:38-80`

#### Quick Win #2 : CCR Retrieve + Store (3-4 jours)

**Pourquoi** : on a CCR CompressHook (calcul le hash). Il manque le
**lookup Redis** (Retrieve) et le **stockage de la réponse** (Store).
Sans ça, CCR ne fait rien.

**Implémentation** :
- Créer `optiagent/hook_ccr_retrieve.go` (BeforeRequest, priority 750)
  - Si `hctx.Feature("ccr_hash")` existe → `GET ccr:<hash>` dans Redis
  - Si hit : `hctx.ShortCircuitStatus = 200`, `hctx.ShortCircuitBody = value`
  - TTL : 1h par défaut (configurable)
- Créer `optiagent/hook_ccr_store.go` (AfterResponse, priority 850)
  - Si `hctx.CCRCompressedPayload` non vide ET `hctx.UpstreamStatus == 200` :
    - `SETEX ccr:<hash> 3600 <response_bytes>`
  - Ne pas écraser si la clé existe (race condition)
- Mettre à jour le câblage dans `proxy.go` (ajouter 2 hooks à `RunBeforeHooks` et `RunAfterHooks`)

**Tests TDD** (8 tests) :
- `TestCCRRetrieveHook_HitShortCircuits`
- `TestCCRRetrieveHook_MissIsNoOp`
- `TestCCRRetrieveHook_RedisErrorFailsOpen`
- `TestCCRRetrieveHook_HonorsTTL`
- `TestCCRStoreHook_PersistsResponse`
- `TestCCRStoreHook_SkipsNon200Responses`
- `TestCCRStoreHook_DoesNotOverwriteExisting`
- `TestCCRPipeline_EndToEnd` (compress → retrieve miss → upstream → store → retrieve hit)

**Référence Headroom** : `crates/headroom-core/src/ccr/`

#### Quick Win #3 : Output Token Reduction (1 sem)

**Pourquoi** : Headroom économise 30%+ sur les **output tokens** aussi,
pas juste l'input. On n'a rien. C'est un **gain de 30% sur la facture
totale** qu'on rate complètement.

**Implémentation** :
- Créer `internal/services/output_reduction.go`
- Mesure baseline : pour chaque réponse, compter les tokens de sortie
- Stratégie v1 (simple) : tronquer les réponses au-delà de N tokens
  (N = ceil(max_output * 0.7))
- Reporting : ajouter une card "Output Tokens Saved" au dashboard
- Stats : `synapse:stats:output_saved:<vk>:<day>` (HINCRBY)

**Tests TDD** (5 tests) :
- `TestOutputReduction_TruncatesLongResponses`
- `TestOutputReduction_PreservesShortResponses`
- `TestOutputReduction_CountsTokensAccurately`
- `TestOutputReduction_StoresMetricsInRedis`
- `TestOutputReduction_ReportsConfidenceInterval` (estimé vs mesuré)

**Référence Headroom** : `docs/proposals/output-token-reduction.md` (la
méthodologie du holdout group à 10%)

#### Quick Win #4 : MCP Server (3-5 jours)

**Pourquoi** : Headroom a un MCP server. Synapse non. **C'est un
différenciateur de positionnement** : permettre à Claude Code, Cursor,
etc. d'appeler `synapse_compress` directement.

**Implémentation** :
- Créer `cmd/mcp-server/main.go` (binaire séparé)
- 4 tools MCP :
  - `synapse_compress(payload)` : retourne le CCR hash
  - `synapse_retrieve(hash)` : retourne la réponse cachée
  - `synapse_stats()` : retourne les savings
  - `synapse_denylist(tool)` : ajoute à la denylist
- Utiliser le SDK Go MCP officiel
- Dockerfile séparé dans `docker/Dockerfile.mcp`

**Tests** : manuels via un client MCP test (script Python avec `mcp` lib)

**Référence Headroom** : `headroom/mcp_server.py` (3 tools: compress, retrieve, stats)

### Sprint 2 (semaine prochaine) — 3 medium wins

#### Quick Win #5 : LogCompressor (3-5 jours)

**Détecte et compresse** les stack traces Python, JS, Rust :
- Drop les frames du milieu de la stack (garde les 3 premiers et 3 derniers)
- Déduplique les erreurs récurrentes (garder 1 exemplaire + count)
- Préserve les exceptions chaînées (Python `raise X from Y`)
- Compresse les timestamps ISO en delta

**Référence Headroom** : `crates/headroom-core/src/transforms/log_compressor.rs`

#### Quick Win #6 : DiffCompressor (2-3 jours)

**Compresse les sorties de `git diff`** :
- Drop les hunks vides (0/+0, +0/0)
- Compresse les hunks identiques (garder 1 + count)
- Détecte les "context lines" redondantes
- Préserve les hunks avec mots-clés critiques (TODO, FIXME, BUG)

**Référence Headroom** : `crates/headroom-core/src/transforms/diff_compressor.rs`

#### Quick Win #7 : Cross-agent memory (1 sem)

**Mémoire partagée** entre agents/keys :
- Hash SHA256 du prompt final → `mem:agent1:sha256(...)` = response
- Lookup avant l'appel upstream (gain L0/L1 supplémentaire)
- Déduplication automatique (si 2 prompts ont le même hash, partager le cache)
- TTL configurable (default 1h)

**Référence Headroom** : `headroom/memory/`

### Sprint 3 (mois prochain) — 2 gros morceaux

#### Quick Win #8 : SmartCrusher simplifié (1-2 sem)

**Port simplifié du SmartCrusher** (3 stratégies au lieu de 11) :
- **Number array** : si >50 nombres consécutifs → garder min/max/avg/count
- **String array** : si >50 strings identiques (catégorisation) → garder 5 représentatifs + count
- **Object array** : si >50 objets avec les mêmes keys → garder les 5 plus "pertinents" (par hash) + count
- Garder les anchors (champs avec `id`, `name`, `key` qui match une query)

**Référence Headroom** : `crates/headroom-core/src/transforms/smart_crusher/`

#### Quick Win #9 : Live-zone dispatcher Anthropic (1 mois)

**C'est le Saint Graal** : ne compresser QUE les blocs mutables de la
requête, en préservant le provider KV cache.

Anthropic Messages API :
- `system` : immuable (cache hit)
- `tools` : immuable (cache hit)
- `messages[0..N-K]` : ancien (immuable, cache hit)
- `messages[N-K..]` : "live zone" (mutable, peut compresser)

**Implémentation** : port direct de `crates/headroom-core/src/transforms/live_zone.rs` → `optiagent/hook_live_zone.go`

## Métriques de succès

**Sprint 1** (à la fin de la semaine) :
- Cache hit rate (L1+L2+CCR) ≥ 60% sur traffic réel
- Output tokens saved ≥ 10%
- MCP server démarré et 1 client MCP connecté

**Sprint 2** (à la fin de la semaine prochaine) :
- Log/diff compression réduit les tool outputs de 70%
- Cross-agent memory hit rate ≥ 20% (beaucoup de prompts redondants entre agents)

**Sprint 3** (à la fin du mois) :
- SmartCrusher réduit les tool outputs JSON de 50%+
- Live-zone préserve le cache hit provider à 90%+ (mesure indirecte : latence)

## Ce qu'on NE FAIT PAS (out of scope)

- **CodeCompressor (tree-sitter)** : trop complexe, ROI limité (peu d'agents qui collent du code source en tool output)
- **Kompress ML model** : 500MB de modèle, ré-entraînement nécessaire, ROI pas clair
- **Bedrock/Vertex integration** : pas la cible client (focus OpenAI/MiniMax pour l'instant)
- **LangChain/LlamaIndex extras** : les intégrations officielles ne sont pas un différenciateur (OpenAI-compatible suffit)
- **`headroom learn`** (mine failures) : le Request Explorer du dashboard fait déjà 80% du job

## Architecture cible (Sprint 3 fini)

```
 Hook pipeline (par ordre de priorité)
   600: CacheAligner (détection)
   700: Live-zone dispatcher (Anthropic) — NOUVEAU
   750: CCR Retrieve (lookup) — NOUVEAU
   800: CCR Compress (canonicalization) — EXISTANT
   850: CCR Store (after response) — NOUVEAU
   900: SmartCrusher (lossy array compression) — NOUVEAU
   1000: LogCompressor (log truncation) — NOUVEAU
   1100: DiffCompressor (diff dedup) — NOUVEAU

 MCP server (binaire séparé)
   - synapse_compress
   - synapse_retrieve
   - synapse_stats
   - synapse_denylist

 Dashboard additions
   - "Output Tokens Saved" card
   - "Cache Aligner warnings" widget
   - "Live zone hit rate" graph
```
