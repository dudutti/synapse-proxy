# Architecture de Cache Synapse Proxy

Ce document fournit une plongée technique détaillée sur la façon dont Synapse Proxy intercepte, déduplique et compresse les requêtes avant qu'elles n'atteignent le fournisseur LLM en amont.

## Fonctionnement du pipeline de cache

```
                     Votre app / agent / SDK
                              ¦
                              ¦  HTTP, Authorization: Bearer sk-opti-...
                              ?
              +--------------------------------+
              ¦       Synapse Proxy (Go)     ¦
              ¦                                ¦
              ¦  +-------+  +--------------+   ¦
              ¦  ¦  L0   ¦-?¦      L1      ¦   ¦
              ¦  ¦ in-fl.¦  ¦ exact match  ¦   ¦
              ¦  ¦ dedup ¦  ¦  (SHA-256)  ¦   ¦
              ¦  +-------+  +--------------+   ¦
              ¦                    ¦ miss     ¦
              ¦                    ?          ¦
              ¦              +----------+     ¦
              ¦              ¦    L2    ¦     ¦
              ¦              ¦ semantic ¦     ¦
              ¦              ¦ (ONNX)   ¦     ¦
              ¦              +----------+     ¦
              ¦                   ¦ miss     ¦
              ¦                   ?          ¦
              ¦             +----------+      ¦
              ¦             ¦    L3    ¦      ¦
              ¦             ¦  (tail   ¦      ¦
              ¦             ¦compress) ¦      ¦
              ¦             +----------+      ¦
              ¦                  ¦           ¦
              +------------------+-----------+
                                 ¦
                                 ?
                +----------------------------+
                ¦  Fournisseur upstream       ¦
                ¦  (OpenAI, Anthropic, etc.)  ¦
                +----------------------------+
```

Détail des quatre caches :

| Cache | Rôle | Quand s'active-t-il ? | Fichier Source |
|-------|------|-----------------------|----------------|
| **L0** In-flight dedup | Deux requêtes identiques (même clé virtuelle, même SHA-256 du payload) arrivent simultanément. La première acquiert un verrou Redis SETNX avec un TTL de 30 secondes et se traite normalement. La deuxième **bloque et attend** le déverrouillage. | Conditions de concurrence (Race conditions), tentatives répétées d'agents, curl parallèles, fan-out depuis un agent parent. | `optiagent/dedup.go` |
| **L1** Exact match | Le SHA-256 complet du payload normalisé sert de clé de cache. Un hit (succès) renvoie la réponse en cache en <2 ms. | Tâches CRON, scripts réessayant la même requête, appels d'outils identiques entre les tours d'agents. Limité par clé virtuelle. | `optiagent/engine.go` |
| **L2** Semantic | Le dernier message de l'utilisateur est transformé en vecteurs par un modèle ONNX local (MiniLM multilingue, 384 dimensions) et une recherche KNN est exécutée sur un index Redis VSS (`FT.SEARCH idx:l2cache`). Le seuil de similarité cosinus est le `semantic_tolerance` de la clé. | "Comment réinitialiser mon mot de passe ?" correspond à "Mot de passe oublié, que faire ?". **Auto-désactivé** si la requête est multi-tours (`nonSystemCount > 1`) ou contient une image. | `cache/l2_vector.go` |
| **L3** Compression | Le prompt système, les déclarations d'outils, et l'historique plus ancien sont conservés intacts à l'octet près. Seuls les 4 derniers messages sont réécrits : les anciens blocs `<thought>` sont supprimés, les appels d'outils répétés effondrés, le contenu de raisonnement supprimé. | Longues sessions d'agents avec redondances de pensée, appels d'outils répétés, sorties d'outils obsolètes. | `optiagent/compressor.go` |

**Bypass du Cache :** si le client envoie `X-Bypass-Cache: true`, le proxy transfère la requête telle quelle sans utiliser le cache, tout en l'enregistrant pour la télémétrie.

---

## Compression L3 avec préservation du préfixe - préserver le cache du fournisseur

C'est la partie de la conception la plus complexe et la plus coûteuse lorsqu'elle est mal implémentée.

Anthropic, OpenAI, et MiniMax hachent les octets de votre requête et renvoient le même préfixe depuis un cache côté serveur, permettant jusqu'à ~90% de réduction sur les appels subséquents. Mais le hachage est **exact à l'octet près** — modifier un espace, réorganiser une clé JSON, échapper un `<` en `\u003c`, et le cache est raté, vous faisant payer le plein tarif.

Synapse Proxy résout cela en 3 phases :

1. **Phase 1 — Encodeur idempotent.** Un encodeur JSON déterministe garantit que deux compressions du même payload produisent un résultat identique à l'octet près (clés triées, sans espace, sans échappement HTML, formatage déterministe des flottants).
2. **Phase 2 — Découpage (Split) préservant le préfixe.** Avant la compression, le proxy sépare le préfixe statique (prompt système, outils, historique ancien) et la traîne dynamique (derniers messages). Le préfixe est laissé intact à l'octet près.
3. **Phase 3 — Compression co-localisée.** La "traîne" dynamique est compressée, puis recollée au préfixe intact. Le résultat est un document JSON valide où les N premiers octets sont identiques à l'entrée originale.

*Résultat :* 99,8% des prompt tokens continuent d'utiliser le cache natif du fournisseur, même si L3 compresse activement la fenêtre de contexte récente.
