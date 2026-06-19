# Schéma de Base de Données et Télémétrie de Synapse Proxy

Chaque requête interceptée par Synapse Proxy crée une ligne dans la table `RequestLog` dans PostgreSQL, transformant votre trafic LLM en un flux hautement mesurable.

## Schéma de `RequestLog`

| Colonne | Signification |
|---------|---------------|
| `cacheLevel` | `MISS`, `L0`, `L1`, `L2`, `L3`, `LOOP`, `BYPASS` |
| `promptTokensOrig` / `promptTokensOpt` | Comptage des tokens mesuré par le fournisseur amont (upstream). |
| `completionTokensOrig` / `completionTokensOpt` | Idem pour les tokens générés. |
| `savingsInputFresh` / `savingsCacheRead` / `savingsCacheCreation` / `savingsOutput` | Économies monétaires calculées en temps réel grâce à la table `ProviderModel`. |
| `cacheCreationTokens` / `cacheReadTokens` / `cacheHitTokens` / `cacheMissTokens` | Lu depuis la réponse du fournisseur (ex. `prompt_tokens_details.cached_tokens` pour OpenAI, ou `cache_creation_input_tokens` pour Anthropic). 0 s'il ne les expose pas. |
| `durationMs` | Latence d'exécution de la requête. |
| `agentId` / `agentLabel` | Inféré depuis `proxy/internal/utils/agent_detector.go` — heuristiques du User-Agent + prompt système. Labels connus : `Hermes`, `Claude Code`, `OpenClaw`, `LangChain`, `chat-direct`, `tool-using-agent`, `curl`, etc. |
| `sessionId` | Défini par l'enregistrement de session pour grouper les requêtes de l'Agent Flow et le Session Replay. |
| `payloadHash` | SHA-256 de la requête d'origine — utilisé pour lister les "Requêtes les plus chères". |
| `originalPayload` / `optimizedPayload` / `responsePayload` | Les JSON complets de la requête et réponse, conservés sauf si `zeroLog=true` est activé sur la clé API. |
| `toolCalls` | Tableau JSON des appels d'outils détectés pour bâtir la timeline de flux de l'Agent. |
| `isSimulated` | Indique si la limite gratuite (Free Tier) ou la limite de session a été dépassée, comptabilisant l'économie de façon théorique. |
| `killSwitchFired` | Vrai si la requête a déclenché le pare-feu anti-boucle (HTTP 400). |

## Tables et Index Principaux

Le schéma de base de données est géré par Prisma dans `dashboard/prisma/schema.prisma` avec des migrations dans `prisma/migrations/`.
Les tables principales sont : `User`, `ApiKey`, `RequestLog`, `BenchmarkLog`, `ProviderModel`.

Des index sont placés sur :
- `RequestLog(apiKeyId, createdAt DESC)`
- `RequestLog(agentId)`
- `RequestLog(sessionId)`
- `RequestLog(payloadHash)`
- `RequestLog(agentId, createdAt DESC)`

Un découvreur de modèles (`internal/workers/model_radar.go`) identifie automatiquement les nouveaux modèles non reconnus par le proxy et les stocke dans Redis avec le statut `learning` -> `known` -> `mapped`.

## Métriques Prometheus

Synapse Proxy expose un point de terminaison à `/metrics` qui respecte le format texte de Prometheus. Il permet le suivi de :
- Les compteurs de panique de l'application (Robustesse)
- Le niveau de Hits des caches (L1/L2/L3)
- Les blocages dus au Kill Switch (Pare-feu)
- Le volume total des requêtes.

L'endpoint est développé à la main sans la lourde dépendance `prometheus/client_golang` pour plus de légèreté.
