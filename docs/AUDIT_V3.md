# Synapse Proxy vs Headroom — Audit corrigé v3 (2026-06-24)

> Audit v2 révisé après l'observation de l'utilisateur :
> "CCR est vraiment équivalent ? Headroom avait pas 12 niveaux ?"
>
> Réponse honnête : NON. L'audit v2 sous-estimait
> massivement le CCR de Headroom. Ce document corrige.
> L'audit v1 (VS_HEADROOM.md) et l'audit v2 (AUDIT_V2.md)
> doivent être lus avec ce corrigendum en tête.

## Le CCR de Headroom n'est PAS un cache de hash

Le CCR (Compress-Cache-Retrieve) de Headroom est un **système
multi-tour avec tool injection**, pas un simple cache de
hash canonique. C'est une architecture complète en 5
composants interconnectés :

| Composant | Fichier Headroom | Lignes | Rôle |
|-----------|------------------|--------|------|
| Tool Injection | `ccr/tool_injection.py` | 517 | Injecte un tool `headroom_retrieve` dans la request quand compression > seuil. Le LLM peut appeler le tool pour récupérer le contenu original. |
| Response Handler | `ccr/response_handler.py` | 896 | Intercepte les tool calls `headroom_retrieve` dans la réponse du LLM, retourne automatiquement le contenu décompressé, fait un follow-up call. |
| Context Tracker | `ccr/context_tracker.py` | 660 | Suit le contenu compressé à travers les tours. Détecte proactivement si une nouvelle query du user a besoin d'expansion (ex: user dit "what about the auth middleware?" → expand la zone compressée qui contenait auth_middleware.py). |
| Batch Processor | `ccr/batch_processor.py` | 562 | Pour les batch APIs (Anthropic, OpenAI, Google async). Stocke le contexte au submit, expansion au résultat. |
| MCP Server | `ccr/mcp_server.py` | (à lire) | Expose `headroom_retrieve` via MCP pour les agents MCP-native. |
| Total CCR | 5 fichiers Python | 2,635+ | |

**Ce qu'on a (Synapse)** : 1 cache hash canonique (3 hooks,
~480 lignes : CCR Compress + CCR Retrieve + CCR Store).
**Couverture CCR : 18% de Headroom**.

## Les 4 composants CCR qu'on n'a PAS

1. **Tool injection** : on n'injecte pas `headroom_retrieve`
   dans les requests. Quand on compresse un tool output,
   le LLM n'a aucun moyen de demander le contenu original
   (si on l'a tronqué, c'est perdu pour le LLM). C'est
   l'inconvénient majeur de notre implémentation actuelle.

2. **Response handler** : on n'intercepte pas les tool calls
   `headroom_retrieve`. Si on injectait le tool, il faudrait
   détecter l'appel et répondre automatiquement.

3. **Context tracker** : on n'a pas de suivi multi-tour.
   Quand le user dit "what about X" 5 tours après la
   compression, on ne détecte pas que X est dans une zone
   compressée et on n'expansion pas. C'est une feature
   différenciante forte de Headroom.

4. **Batch processor** : on ne supporte pas les batch APIs.
   Anthropic Batch, OpenAI Batch, Google Batch sont
   utilisés par les agents long-running.

## Pourquoi c'est important

Headroom peut dire à ses clients : "Vous pouvez faire
tourner des agents sur 100k tokens de tool output, et le
LLM pourra toujours demander le détail si besoin." Nous
on dit : "Vous pouvez faire tourner des agents, mais si
on compresse, l'info est perdue pour le LLM."

C'est une **différence architecturale majeure**, pas une
amélioration incrémentale. Le CCR de Headroom fait du
proxy un **partenaire actif** de l'agent ; le nôtre est un
**compresseur passif**.

## Le reste de l'audit v2 est-il exact ?

Pour les autres comparaisons (LogCompressor, TagProtector,
etc.) l'audit v2 est correct. Pour SmartCrusher, CodeCompressor,
Live-zone, l'écart est encore plus grand (on n'a rien de
tout ça).

## Le mot "équivalent" qu'on a utilisé

Dans la session précédente j'ai écrit :
- "✅ CCR Compress/Cache/Retrieve (P0.1) — équivalent du CCR
  Headroom"

C'est **trompeux**. Le CCR Compress + CCR Retrieve + CCR
Store est l'équivalent du **sous-ensemble cache** du CCR
Headroom, pas du CCR Headroom complet. On devrait dire :

- "✅ CCR cache (P0.1) — équivalent du sous-ensemble cache
  du CCR Headroom (18% de la surface)"
- "❌ CCR tool injection (P1 requis, ~1 mois)"
- "❌ CCR response handler (P1, ~1 mois)"
- "❌ CCR context tracker (P2, ~2 mois, gros différenciateur)"
- "❌ CCR batch processor (P3, ~1 mois)"

## Le CCR de Headroom = "12 niveaux" ?

L'utilisateur a mentionné "12 niveaux". Je n'ai pas
compté exactement 12 dans le module CCR de Headroom (j'ai
compté 5 composants principaux). Les "12 niveaux" pourraient
faire référence aux 12 transforms du module `headroom/
transforms/` (compression, content_detector, anchor_selector,
etc.) qui sont **séparés du CCR**. Le CCR utilise les
transforms en interne.

Si l'utilisateur faisait référence à 12 niveaux de cache
(comme on a 6 niveaux), je n'ai pas vu de comptage
explicite de 12 dans le code Headroom. Le CCR a 5
composants ; les transforms sont 13 (Python) ou 12 (Rust
ports). Confusion possible entre les deux axes.

## Action immédiate

- Corriger la roadmap et l'audit pour refléter la réalité
  (pas de "équivalent" trompeur)
- P1 = CCR tool injection + response handler (les deux
  composants qui rendent le CCR utilisable côté agent)
- P2 = CCR context tracker (gros différenciateur)
- P3 = CCR batch processor
