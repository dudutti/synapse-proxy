# Synapse Proxy — Tour utilisateur détaillé (session 2026-06-24)

> Documenté via browser_navigate + browser_snapshot sur le dashboard
> Next.js local (http://localhost:3000). Session de test authentifiée
> en admin@synapse.local (SUPERADMIN).
>
> ⚠️ Limite : le driver CUA (cua-driver 0.6.7) ne capture pas Chrome
> sur Windows (limitation UIA/UIPI). Toutes les "captures" sont
> donc des snapshots DOM textuels. Pour de vraies images, utiliser
> OBS ou Puppeteer avec --remote-debugging-port=9222.

## Routes testées

| Route | URL | Statut | Description |
|-------|-----|--------|-------------|
| Landing/Login | `/` | ✅ | Marketing + login |
| Dashboard principal | `/` (auth) | ✅ | Live telemetry, charts, key selector |
| Playground A/B | `/playground` | ⚠️ | UI OK mais bouton Send ne déclenche pas (bug React) |
| Benchmark | `/benchmark` | ✅ | "Live Benchmark" + warning "triples your costs" |
| Request Explorer | `/explorer` | ✅ | Tableau + filtres (8 champs) |
| Expensive Prompts | `/expensive` | ✅ | Top-N prompts, fenêtres 24h/7d/30d |
| Session History | `/sessions` | ✅ | Page vide (pas de sessions) |
| Pricing Coverage | `/pricing` | ✅ | Page vide |
| Alert Rules | `/alerts` | ✅ | "New Rule" button |
| Settings | `/settings` | ❌ | Session expirée |

> ⚠️ Le menu "Tools" dropdown ne s'ouvre pas en browser_click sur
> le bouton "Tools" — les sous-menus Request Explorer, Expensive
> Prompts, etc. ne sont accessibles que par URL directe (voir
> tableau ci-dessus). C'est un bug React/hydration probablement
> (le dropdown s'ouvre au hover, pas au clic, mais le CUA ne
> fait pas de hover).

## Page Playground — détail

**Layout** :
- Side-by-Side A/B (checkbox, par défaut coché)
- Linked button (synchronise les 2 panels)
- Export / Clear (disabled tant que pas de réponse)
- 2 panneaux :
  - Gauche : "Synapse Proxy (Optimized)" + combobox clé + combobox modèle
  - Droite : "Direct API (Control)" + même combos + checkbox BYPASS CACHE

**Workflow attendu** :
1. Taper prompt dans le bottom form
2. Bouton Send activé
3. Les 2 panels se remplissent avec les réponses (côté gauche optimisé via proxy, côté droite direct)
4. Comparaison côte-à-côte des tokens, latence, coût, cache hits

**Bug observé** : le bouton Send ne déclenche pas la requête.
Tant qu'il n'a pas été cliqué avec succès, Export/Clear sont disabled.
Cause probable : le form submit React ne se déclenche pas correctement
sans interaction utilisateur réelle (le clic CUA ne simule pas
correctement le focus + keydown).

**Workaround** : utiliser directement l'API via curl ou les tests bash
(documentés dans docs/HOOK_TESTS.md). Le proxy fonctionne, c'est
uniquement le frontend du playground qui a ce bug.

## Test A/B planifié (à faire manuellement)

1. Login en admin@synapse.local
2. Naviguer vers /playground
3. Sélectionner la clé MiniMax (***…bd5e)
4. Sélectionner le modèle MiniMax-M2.7
5. Taper "Say OK in one word" dans le prompt input
6. Cliquer Send
7. Observer les 2 panels :
   - Gauche (Synapse Proxy) : L1/L2/L3 hit + savings
   - Droite (Direct API) : appel direct, 0 cache
8. Refaire la même requête : gauche devrait être L1 hit (0 cost), droite pareil
9. Comparer les TOKENS IN/OUT et le $ SAVED dans le dashboard
   principal (/) après quelques requêtes

## Test Benchmark (à faire manuellement)

1. Naviguer vers /benchmark
2. Activer benchmark mode sur une clé (le warning "triples your costs"
   s'affiche, c'est intentionnel)
3. Faire 5-10 requêtes via Playground ou curl
4. Retourner sur /benchmark : voir les logs de comparaison

## Test Request Explorer

1. Faire des requêtes via Playground (ou curl)
2. Naviguer vers /explorer
3. Filtrer par modèle, agent, virtual key
4. Cliquer sur une ligne pour voir le payload original + optimized + response
5. Comparer les savings par cache level (L1/L2/L3)

## Test Alert Rules

1. Naviguer vers /alerts
2. Cliquer "New Rule"
3. Créer une règle : "kill_switch si loop_count > 5 en 60s"
4. Faire 6+ requêtes identiques → alerte devrait firer
5. Voir dans /expensive les requêtes concernées
