# Synapse Proxy — Audit LogCompressor + TagProtector (corrigé v4, 2026-06-24)

> L'utilisateur a challengé mes affirmations "équivalent
> du log_compressor Headroom" et "équivalent du
> tag_protector Headroom". Cet audit corrige honnêtement.

## LogCompressor : PAS équivalent

**Headroom** (`headroom/transforms/log_compressor.py`,
520 lignes, Rust-backed via PyO3) a :

- **Détection multi-format** : `LogFormat` enum (PYTEST,
  NPM, CARGO, MAKE, JEST, GENERIC). On ne détecte que
  Python/JS/Rust stack traces.
- **Niveaux de log** : `LogLevel` enum (ERROR, FAIL, WARN,
  INFO, DEBUG, TRACE, UNKNOWN) avec scoring. On a zéro
  notion de niveau.
- **11 paramètres de config** : `max_errors`,
  `error_context_lines`, `keep_first_error`,
  `keep_last_error`, `max_stack_traces`,
  `stack_trace_max_lines`, `max_warnings`,
  `dedupe_warnings`, `keep_summary_lines`,
  `max_total_lines`, `enable_ccr`, `min_lines_for_ccr`.
  On a 2 (keepFirstFrames, keepLastFrames).
- **Scoring de lignes** : chaque ligne a un score, le
  compressor garde les high-score lines. On a une troncature
  first/last aveugle.
- **CCR integration** : pousse l'original dans le
  `CompressionStore` Headroom avec un `cache_key`. Notre
  CCR Store est découplé.
- **Dédupe conservatrice** : split sur `:`/`=` avant
  normaliser, pour ne pas collapse des erreurs distinctes.
  Notre dédupe est naive (adjacent exact match).
- **State machine per-language** : pour gérer les chained
  exceptions correctement (blank lines mid-trace,
  "During handling of...", etc.). On a une version
  simplifiée qui marche pour 70% des cas.

**Notre LogCompressor** (P0.2, 592 lignes Go) :
- Détecte Python/JS/Rust stack traces (3 formats vs 6)
- Tronque first 3 + last 3 frames avec marker
- Dedupe adjacent identical lines
- Pas de notion de niveau de log
- Pas de scoring, pas de budget total_lines
- Pas d'intégration CCR
- Pas de support pytest/npm/cargo/make/jest

**Couverture réelle** : **~20% de Headroom**.

Le mot "équivalent" est faux. Le mot "premier sous-ensemble
utilisable" serait honnête.

## TagProtector : architecturalement DIFFÉRENT

**Headroom** (`headroom/transforms/tag_protector.py`,
131 lignes shim + Rust port, 5 bug fixes documentés) fait
un **rewrite + restore** :

1. `protect_tags(text)` → remplace `<tag>...</tag>` par
   des placeholders `{{HEADROOM_TAG_0}}` etc., retourne
   `(cleaned_text, [(placeholder, original), ...])`
2. Le compressor voit `cleaned_text` (sans tags), peut
   compresser librement
3. `restore_tags(text, blocks)` → remet les originaux
   aux positions des placeholders

**Notre TagProtector** (P0.4, 257 lignes Go) fait un
**read-only marker** :

1. `BeforeRequest` scan le payload
2. Trouve les zones (script, style, pre, code, CDATA,
   ```)
3. Stocke les positions dans `hctx.Features["tag_protector_zones"]`
4. **Compte sur les autres hooks** pour lire cette map
   et refuser de muter dans les zones

**Les deux approches sont valides mais différentes** :

| Aspect | Headroom | Synapse |
|--------|----------|---------|
| Detect HTML/MD/CDATA | ✅ | ✅ |
| Replace by placeholder | ✅ | ❌ |
| Restore after compression | ✅ | ❌ |
| Read-only marker for other hooks | ❌ | ✅ |
| `compress_tagged_content` mode | ✅ (proteger tag mais pas contenu) | ❌ |
| Self-closing tag handling | ✅ (Rust bug fix #4) | ❌ |
| Placeholder collision detection | ✅ (Rust bug fix #5) | ❌ |
| `KNOWN_HTML_TAGS` (HTML5 canonical) | ✅ (from Rust) | ❌ (6 hardcoded) |
| Exported to other hooks via hctx | ❌ (round-trip internal) | ✅ |

**Couverture fonctionnelle** : 4 features communes sur 8.
Architecturalement incompatible (round-trip vs read-only).

## Le mot "équivalent" est inadéquat

Quand on dit "équivalent", on devrait dire :

- **LogCompressor** : "premier sous-ensemble (20%) —
  couvre le cas commun stack trace mais pas les logs
  structurés, le scoring, ou l'intégration CCR."
- **TagProtector** : "approche architecturale différente
  (read-only marker vs rewrite+restore) — couvre la
  détection mais pas la protection active du contenu."

## Le CCR de Headroom (rappel du corrigendum v3)

Voir `AUDIT_V3.md`. Notre CCR (P0.1) couvre ~18% du CCR
Headroom (5 composants : on n'a que le cache, pas
tool injection / response handler / context tracker /
batch processor).

## Bilan honnête final

| Feature | Notre couverture du concurrent | Verdict |
|---------|-------------------------------|---------|
| CCR cache | 18% | P0.1 a fait le sous-ensemble cache seulement |
| LogCompressor | 20% | P0.2 a fait le cas commun seulement |
| TagProtector | 50% (4/8 features communes, arch différente) | P0.4 a fait l'approche read-only |
| SmartCrusher | 0% | P1 |
| CodeCompressor | 0% | P1 |
| Live-zone | 0% | P2 |
| Batch processor | 0% | P3 |

**On n'est pas "équivalent" de Headroom. On est "premier
sous-ensemble utilisable avec une approche architecturale
différente pour TagProtector."**

Pour être équivalent à 100% de Headroom, il faudrait
probablement 6-12 mois de travail sur les composants
manquants. Le présent audit est une baseline honnête.
