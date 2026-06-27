# Migration Engine → Hooks (Strangler Fig Pattern)

> **Date** : 2026-06-24
> **Statut** : ACTIF — à compléter avant P2
> **Owner** : équipe Synapse

## Contexte

Le proxy a **deux pipelines parallèles** pour la même
fonctionnalité (L1/L2/L3 cache + compression) :

1. **Ancien engine** (`optiagent.ProcessRequest`,
   `optiagent/engine.go`) — utilisé par le handler
   depuis le début. Alimente le dashboard (L1=4,
   L2=0, L3=0, etc.). **NE PAS TOUCHER** tant qu'on
   n'a pas tout migré.

2. **Nouveaux hooks** (6 hooks dans `optiagent/`) :
   - CacheAlignerHook (priority 700)
   - CCRRetrieveHook (priority 750)
   - LogCompressorHook (priority 770)
   - CCRCompressHook (priority 800)
   - CCRStoreHook (priority 850, AfterResponse)
   - CCRToolInjectionHook (priority 900)
   - TagProtectorHook (priority 650)
   Ces hooks sont appelés par `optiagent.RunBeforeHooks`
   dans le handler. Ils font **les mêmes choses en
   mieux** + des choses nouvelles (log compression,
   tool injection, protected zones).

**Le handler fait LES DEUX en série** (ligne 270 :
RunBeforeHooks, ligne 388 : ProcessRequest). **Le
dashboard ne voit que l'ancien engine** via la DB.

## Approche : Strangler Fig Pattern

On **court-circuite** l'ancien engine quand nos hooks
trouvent un hit. L'ancien engine reste en place
comme safety net. Quand tout est validé, on supprime
l'ancien engine.

### Étape 1 : Intercepter les hits (P1.6 — INTERCEPTION)

- **CCR Retrieve** : si la requête hit en cache CCR,
  court-circuiter avec `hctx.ShortCircuitStatus = 200`
  et `hctx.ShortCircuitBody = cached response`.
  **S'assurer que l'ancien engine n'est PAS appelé.**
- **L1 cache exact** : ajouter un hook `L1CacheHook`
  qui fait la même chose que l'ancien L1 mais via
  RunBeforeHooks. Court-circuite si hit.
- **L2 cache semantic** : ajouter un hook
  `L2CacheHook` (façon CCR Retrieve avec vecteur).
  Court-circuite si hit.
- **Loop detection** : déjà court-circuite via
  `LoopDetectionHook`. Vérifier qu'il n'est PAS suivi
  par l'ancien engine.

**Implémentation** :
- Les hooks qui trouvent un hit set
  `hctx.ShortCircuitStatus` + `hctx.ShortCircuitBody`
  + `hctx.ShortCircuitKind` (L1/L2/CCR).
- Le handler vérifie `hctx.ShortCircuited()` après
  `RunBeforeHooks` et **return early** si true.
- L'ancien engine (`ProcessRequest`) n'est appelé
  **que si** `!hctx.ShortCircuited()`.

### Étape 2 : Branche les features sur le dashboard (P1.5)

- Les savings des hooks (LogCompressor, etc.) sont
  dans `hctx.Features`. Le handler doit les lire et
  les écrire dans la DB (RequestLog) pour que le
  dashboard les voie.
- Ajouter des colonnes à la table RequestLog :
  - `log_compressor_bytes_saved`
  - `log_compressor_tokens_saved`
  - `output_reducer_bytes_saved`
  - `output_reducer_tokens_saved`
  - `ccr_compression_store_saved`
  - `ccr_tool_injected` (bool)
  - `tag_protector_zones_count`
  - `cache_kind` (enum : L1/L2/CCR/LOOP/TOOL_DEDUP)
- Le dashboard doit afficher ces colonnes dans la
  table LIVE TELEMETRY et dans le breakdown "DÉTAIL
  DES ÉCONOMIES".

### Étape 3 : Tests E2E (P1.7 — VALIDATION)

- **Tests parallèles** : envoyer la même requête 2
  fois. La 1ère fois, l'ancien engine tourne. La 2ème
  fois, nos hooks court-circuitent. **Les deux doivent
  donner le même résultat final** (même status, même
  body, même headers).
- **Tests de divergence** : comparer les savings
  calculés par l'ancien engine vs par nos hooks pour
  un échantillon de 100 requêtes. La différence doit
  être <= 5% (nos hooks sont mieux, jamais pires).
- **Tests de non-régression** : 0 fail sur la suite
  complète (cache, internal/mcp, internal/utils,
  optiagent) après chaque étape.

### Étape 4 : Suppression de l'ancien engine (P1.8 — SUPPRESSION)

- **Condition** : 3 conditions doivent être vraies
  pendant au moins 1 semaine en prod :
  1. 0 fail sur les tests E2E parallèles
  2. Divergence < 5% sur l'échantillon
  3. Dashboard fonctionne avec les nouvelles colonnes
- **Action** : supprimer `optiagent.ProcessRequest` +
  `OptimizationResult` + le code associé dans
  `engine.go`. Supprimer la branche dans le handler
  qui appelle `ProcessRequest`.
- **Vérification** : la suite complète reste verte, le
  dashboard reste fonctionnel.

## Garanties

- **À chaque étape** : le dashboard ne doit PAS
  régresser. Si une métrique disparaît, c'est un bug.
- **À chaque étape** : les tests doivent être verts.
  Si un test fail, on ne passe pas à l'étape suivante.
- **À chaque étape** : la prod doit continuer à
  fonctionner. Si on observe une régression en
  prod, on rollback.

## Fichiers concernés

- `proxy/optiagent/engine.go` — à supprimer
- `proxy/optiagent/ProcessRequest` — à supprimer
- `proxy/internal/handlers/proxy.go` — lignes 388
  (ProcessRequest call) à supprimer
- `proxy/optiagent/hooks.go` — déjà bon
- `proxy/optiagent/hook_*.go` — déjà bons
- `proxy/internal/services/metrics.go` — à étendre
  pour lire hctx.Features
- `dashboard/prisma/schema.prisma` — à étendre pour
  les nouvelles colonnes

## Notes importantes

- **Ne PAS supprimer** l'ancien engine tant que les
  4 étapes ne sont pas validées. C'est notre safety
  net.
- **Logger** toutes les divergences entre l'ancien
  engine et les nouveaux hooks pour avoir une trace
  en cas de bug.
- **Mesurer** les savings des nouveaux hooks vs
  l'ancien engine pour avoir une baseline de comparaison.
- **Documenter** chaque étape dans ce fichier (date,
  résultats tests, observations prod).

## Prochaine action

**IMPORTANT — vision corrigée 2026-06-24** :
"Plateforme d'observabilité d'abord, plateforme
de compression bonus mais efficace et réelle".
L'observabilité est la PRIORITÉ 1. La compression
est la PRIORITÉ 2.

**Nouvelle ordre** :
1. **P1.5 DASHBOARD FIRST** : brancher TOUT ce qu'on
   a fait (LogCompressor, CCR Compress, CCR Retrieve,
   CCR Store, OutputReducer, TagProtector,
   CCRToolInjection) sur le dashboard. **Avant**
   d'ajouter de nouvelles features de compression.
   - Lire hctx.Features dans le handler
   - Écrire dans la DB (RequestLog)
   - Ajouter les colonnes au schema Prisma
   - Mettre à jour le dashboard Next.js (table
     LIVE TELEMETRY + breakdown DÉTAIL DES ÉCONOMIES)
2. **P1.6 INTERCEPTION** : Strangler Fig — migrer
   L1/L2 dans les hooks, court-circuiter l'ancien
   engine, garder en safety net
3. **P1.7+** : nouvelles compressions, mais chaque
   ajout a D'ABORD son entrée dashboard (entry-point
   dans le breakdown, colonne dans la DB, ligne dans
   le dashboard Next.js)

**Règle absolue** : on ne développe AUCUNE feature
qui n'est pas d'abord visible et lisible sur le
dashboard. Le dashboard est truth, le code est moyen.

## Status précédent

- P1.6 INTERCEPTION (L1CacheHook) a été
  commencé puis arrêté après la correction de
  vision. Les tests L1CacheHook ont été créés
  mais l'implémentation est en pause.
