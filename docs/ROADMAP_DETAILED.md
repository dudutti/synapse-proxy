# Synapse Proxy — Roadmap détaillée P-par-P

> **Objectif** : être au moins aussi bon que Headroom
> sur tous les axes mesurables, jamais en dessous.
> Chaque P a un score de couverture vs Headroom, des
> critères d'acceptation, des tests, et un effort
> estimé. La roadmap est mise à jour à chaque P
> complétée.

## Score global actuel (post-session 2026-06-24)

| Composant | Avant | Après | Headroom | Effort pour 100% |
|-----------|-------|-------|----------|-----------------|
| **CCR** cache | 18% | 18% | 100% | 2-3 mois (P0.6-P0.8) |
| **LogCompressor** | 20% | **~80%** | 100% | 1-2 sem (caps) |
| **TagProtector** | 50% | 50% | 100% | 1-2 sem (rewrite+restore) |
| **SmartCrusher** | 0% | 0% | 100% | 2-3 sem |
| **CodeCompressor** | 0% | 0% | 100% | 2-3 sem |
| **Live-zone** | 0% | 0% | 100% | 1-2 mois |
| **Batch processor** | 0% | 0% | 100% | 1 mois |
| **Output reduction** | 0% | 100% (nôtre, déterministe) | n/a | n/a |

**Score moyen pondéré** : ~50% (avant) → **~60% (après)**.

## Tests

- **Total** : 381 lignes de log optiagent (RUN + PASS + status)
- **Packages with tests** : 4/4
- **Fail** : 0
- **Stability (10 runs)** : OK
- **Regressions** : 0

## P par P — Roadmap détaillée

### P0 — Quick wins (DONE ✅)

- **P0.1** CCR Retrieve/Store (9 tests) — 1 jour ✅
- **P0.2** LogCompressor base (5 tests) — 1 jour ✅
- **P0.3** OutputReducer (5 tests) — 1 jour ✅
- **P0.4** TagProtector (7 tests) — 1 jour ✅
- **P0.5** LogCompressor scoring + structured log (5 tests) — 1 jour ✅
- **P0.5b** LogCompressor format-specific + error context (5 tests) — 1 jour ✅
- **P0.5c** LogCompressor summary lines (5 tests) — 1 jour ✅
- **P0.5d** LogCompressor dedup by keyword (2 tests) — 1 jour ✅
- **P0.5e** LogCompressor CCR store (2 tests) — 1 jour ✅

**Total P0** : 9 sub-phases, 45 tests, 9 jours effectifs

### P1 — CCR Tool Injection + Response Handler (à faire)

**Objectif** : passer CCR de 18% (cache passif) à 70%
(outil actif que l'agent peut appeler pour récupérer
le contenu original).

**Composants** :
- **P1.1** : Tool injection — ajouter `headroom_retrieve`
  au tools[] du payload quand une compression a eu
  lieu. 1-2 jours. **Tests** : 4-5.
- **P1.2** : Response handler — intercepter les tool
  calls `headroom_retrieve` dans la réponse, répondre
  automatiquement, faire un follow-up call. 3-4 jours.
  **Tests** : 6-8.
- **P1.3** : Redis backend pour CompressionStore —
  remplacer l'in-memory par Redis. 2-3 jours. **Tests** : 3-4.
- **P1.4** : Tool injection control — ne pas injecter le
  tool si le modèle n'a pas activé les tools, ou si la
  compression a été minimale. 1-2 jours. **Tests** : 3.

**Effort total P1** : 2-3 semaines. **Score CCR** : 18% → 70%.

### P2 — Context Tracker + SmartCrusher (à faire)

**Objectif** : tracking multi-tour + compression JSON
intelligente.

**Composants** :
- **P2.1** : Context tracker — détecter sur 5 tours que
  "what about X" doit expanser la zone compressée.
  2-3 semaines. **Tests** : 8-10.
- **P2.2** : SmartCrusher (JSON arrays) — détecter les
  arrays de nombres/strings/objects et les crush.
  2-3 semaines. **Tests** : 10-12.
- **P2.3** : CodeCompressor (AST) — utiliser tree-sitter
  pour compressor le code intelligemment. 2-3 semaines.
  **Tests** : 8-10.

**Effort total P2** : 2-3 mois. **Score global** : 60% → 80%.

### P3 — Live-zone + Multi-provider (à faire)

**Objectif** : cache-control sur Anthropic, prompt_cache_key
sur OpenAI, ephemeral cache sur Google.

**Composants** :
- **P3.1** : Live-zone Anthropic (cache_control +
  ephemeral cache). 2-3 semaines. **Tests** : 8-10.
- **P3.2** : Live-zone OpenAI Chat (prompt_cache_key). 1-2
  semaines. **Tests** : 6-8.
- **P3.3** : Live-zone OpenAI Responses (safety_identifier).
  1-2 semaines. **Tests** : 6-8.
- **P3.4** : Live-zone Google (caching). 1-2 semaines.
  **Tests** : 4-6.

**Effort total P3** : 1-2 mois. **Score global** : 80% → 90%.

### P4 — Batch + Polish (à faire)

**Objectif** : support des batch APIs et configuration
complète.

**Composants** :
- **P4.1** : Batch processor (Anthropic/OpenAI/Google
  batch APIs). 3-4 semaines. **Tests** : 10-12.
- **P4.2** : Config struct complète (11 paramètres
  Headroom). 1 semaine. **Tests** : 6-8.
- **P4.3** : TagProtector rewrite+restore mode
  (optionnel, P0.4 est suffisant). 1-2 semaines.
  **Tests** : 4-6.

**Effort total P4** : 1-2 mois. **Score global** : 90% → 100%.

## Métriques cibles

À la fin de chaque P, on mesure :

1. **Couverture fonctionnelle vs Headroom** (%)
2. **Tests passants** (count)
3. **Pas de régression** (0 fail sur suite complète)
4. **Stabilité** (10 runs identiques)
5. **Mesure réelle** (savings observés en prod)

## Critère "au moins aussi bon, jamais en dessous"

- Sur chaque axe mesurable, Synapse doit être **>=**
  Headroom. Pas de régression tolérée.
- Si on ne peut pas être >= (manque de feature), on
  documente pourquoi et on planifie.
- Les tests comparent le comportement de Synapse à
  celui de Headroom quand c'est possible (même input,
  même output attendu).

## Commits trackés (session 2026-06-24)

1. `30911f17` hook pipeline + flaky fix
2. `abb6f8cb` wire setters in main.go
3. `d19e0c03` CCR CompressHook
4. `837ce2ea` Redis auth
5. `bbf1ba4d` sync_keys password
6. `857aa1ed` CCR Retrieve/Store + CacheAligner fix
7. `6d6cbf52` DATABASE_URL fix
8. `fabc785b` LogCompressorHook base
9. `088707ac` OutputReducer
10. `0840ad33` TagProtectorHook
11. `2a60a8ce` AUDIT_V3
12. `e480ed28` AUDIT_V4
13. `ae5f0ce8` LogCompressor scoring (P0.5)
14. `540b0de5` LogCompressor formats + error context (P0.5b)
15. `440a2ae9` LogCompressor quick wins (P0.5c+d+e)
16. `00ac762d` docs AUDIT_V2

**Total** : 16 commits, 9 sous-phases P0 complétées, 45 tests, 0 fail.
