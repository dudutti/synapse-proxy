# ðŸ§  Model Radar "” Auto-adaptation aux nouveaux modèles LLM



> **Statut** : **v1 shippée** (voir [`ROADMAP.md`](./ROADMAP.md))  

> **Version** : v1.0  

> **Date** : 2026-06-16



---



## Contexte & Problème



Synapse Proxy utilise des structs Go typées pour parser le token usage depuis les réponses des providers. Quand un provider sort un **nouveau modèle qui change son format** de réponse (ex : `input_tokens` au lieu de `prompt_tokens`, comme Anthropic l'a déjÃ  fait), l'extraction échoue silencieusement :



- Télémétrie faussée

- Ã‰conomies non calculées

- Métriques dashboard corrompues



Puisqu'on dispose déjÃ  d'un **dropdown de modèles par provider** (via `FetchModelsHandler`), on connaît exactement la liste des modèles existants. On peut donc détecter qu'un modèle **jamais vu** commence Ã  passer par le proxy, et s'y adapter automatiquement.



---



## Approche : 3 couches (v1 shipped)



```

Couche 1 "” Détection       : Detect unknown model on first request (âœ… shipped)

Couche 2 "” Collecte        : Accumulate up to 10 response samples in Redis (âœ… shipped)

Couche 3 "” Auto-diagnostic : Recursive JSON walk to auto-detect usage field paths (âœ… shipped)

```



> **Insight clé** : On n'a PAS besoin d'un LLM embarqué pour résoudre le problème core (parsing usage). Le format des réponses JSON LLM est structuré "” un traversal récursif suffit largement. Un LLM local serait utile pour des cas avancés (v2).



---



## Architecture (v1)



### Flow



```

Request â†’ ProxyHandler

              â†“

         [Model Radar] â† vérifie si model âˆˆ known_models[provider]

              â†“ si NOUVEAU modèle

         Flag "unseen_model" dans Redis

              â†“

         â†’ upstream API â†’ streamResponse â†’ ExtractUsage

                                              â†“

                                    Si usage = 0 ET modèle flaggé "unseen"

                                              â†“

                                    [SampleCollector] "” stocke réponse brute (LPUSH + LTRIM 10)

                                              â†“

                               [TryDiscoverForModel] "” lance le FieldDiscoverer

                                              â†“

                                    [FieldDiscoverer] "” analyse structure JSON

                                              â†“

                               Génère nouveau "usage_mapping" pour ce modèle

                                              â†“

                               Persiste dans Redis (Synapse Proxy:radar:usage_mappings)

                                              â†“

                               Update radar entry status=mapped

```



### Stockage Redis



| Clé | Type | Contenu |

|-----|------|---------|

| `synapse:radar:known_models` | Set | Modèles connus, seedés depuis `FetchModels` |

| `synapse:radar:models:<modelID>` | String (JSON) | `RadarEntry` : `firstSeen`, `lastSeen`, `status` (`learning` / `mapped`), `UsageMap`, `SampleCnt` |

| `synapse:radar:samples:<modelID>` | List | Dernières 10 réponses brutes (LPUSH + LTRIM) |

| `synapse:radar:usage_mappings` | Hash | `{modelID}` â†’ JSON `{prompt_field, completion_field, confidence_score, sample_count, discovered_at}` |



### API Dashboard



`GET /api/admin/model-radar` (role `SUPERADMIN` requis) "” retourne :



```json

{

  "entries": [

    {

      "model_id": "google/gemma-4-26b-a4b-qat",

      "provider": "minimax",

      "status": "mapped",

      "first_seen": "2026-06-16T14:30:00Z",

      "last_seen": "2026-06-16T15:15:00Z",

      "sample_count": 8,

      "has_usage_map": true,

      "prompt_field": "usage.prompt_tokens",

      "completion_field": "usage.completion_tokens",

      "confidence": 0.92

    }

  ],

  "known_models_count": 8

}

```



---



## Composants Go (v1 shipped)



### `proxy/internal/workers/model_radar.go`



- `CheckAndFlagNewModel(ctx, rdb, provider, modelID) bool` "” appelé au début de chaque `ProxyHandler`, retourne `true` si le modèle est inconnu. Crée l'entrée `synapse:radar:models:<modelID>` avec `status: "learning"`.

- `CollectSample(ctx, rdb, modelID, rawResponse)` "” `LPUSH` dans `synapse:radar:samples:<modelID>` + `LTRIM 0 9` (max 10 samples).

- `PromoteKnown(ctx, rdb, provider, modelID)` "” appelé quand `ExtractUsage` retourne des tokens > 0. Marque le modèle comme "known" pour ne plus le flagguer.

- `RegisterKnownModels(...)` "” appelé au boot par `RegisterKnownModel` depuis le seed initial.



### `proxy/internal/workers/field_discoverer.go`



Algorithme d'auto-détection des champs par traversal récursif du JSON.



**Stratégie** : chercher les champs dont le **nom** contient `prompt`, `input`, `completion`, `output`, `token`, `usage` et dont la **valeur** est un entier > 0. Score = `hitRatio Ã— (1 si valeur > 0, sinon 0.3)`. Seuil : `confidence â‰¥ 0.5`.



```go

type UsageMapping struct {

    PromptField     string  `json:"prompt_field"`     // ex: "usage.prompt_tokens"

    CompletionField string  `json:"completion_field"` // ex: "usage.completion_tokens"

    ConfidenceScore float64 `json:"confidence_score"` // 0..1

    SampleCount     int     `json:"sample_count"`

    DiscoveredAt    string  `json:"discovered_at"`

}



func TryDiscoverForModel(ctx, rdb, modelID) (UsageMapping, bool, error)

```



**Exemples de cas couverts automatiquement :**



| Provider | Prompt field | Completion field |

|----------|-------------|------------------|

| OpenAI standard | `usage.prompt_tokens` | `usage.completion_tokens` |

| Anthropic | `usage.input_tokens` | `usage.output_tokens` |

| Google | `usageMetadata.promptTokenCount` | `usageMetadata.candidatesTokenCount` |

| Fallback | `usage.total_tokens` | *(mono-field)* |



### Intégration dans `proxy.go`



```go

// 1. Détection au début du handler

isNew := workers.CheckAndFlagNewModel(ctx, rdb, provider, realModel)



// 2. Skip Model Radar si Zero-Log

if isNew && !zeroLog {

    if usage.Source != "estimated" && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {

        go workers.PromoteKnown(context.Background(), db.GetRedis(), provider, realModel)

    } else if usage.PromptTokens == 0 && usage.CompletionTokens == 0 {

        go workers.CollectSample(context.Background(), db.GetRedis(), realModel, cacheableResponse)

        go workers.TryDiscoverForModel(context.Background(), db.GetRedis(), realModel)

    }

}

```



---



## Décisions de design (v1)



### Niveau d'automatisme : **Option A (conservative)**



- L'auto-discovery tourne et persiste le mapping dans Redis **automatiquement** (confidence â‰¥ 0.5).

- Le `RadarEntry.status` passe Ã  `"mapped"`.

- **Pas de validation manuelle** en v1 "” la confiance est un signal de qualité, mais le mapping est consommé par `ExtractUsage` dès qu'il est persisté.

- On garde la porte ouverte pour un workflow "Approve before activate" (Option A original) si on observe des faux positifs en prod.



### Comportement pendant la phase "learning"



- **Option 2b (best-effort)** choisie : `ExtractUsage` retourne 0 tant que `usage_mappings` ne contient pas le modèle. Le radar entry est en `learning`.

- Le dashboard montre un `costSaved` Ã  0 pour ces requêtes "” c'est honnête (on n'invente pas un chiffre).

- Dès que `FieldDiscoverer` trouve un mapping avec confiance â‰¥ 0.5, il est persisté et toutes les requêtes suivantes utilisent le nouveau mapping.



### Seed des modèles connus au démarrage



- `RegisterKnownModels` est appelé au boot avec une liste statique des modèles les plus courants par provider (8 modèles MiniMax seedés par défaut).

- Le seed est mis Ã  jour dans le code au fil des releases.

- `FetchModelsHandler` (déjÃ  shippé) ajoute dynamiquement les modèles que le provider expose via son endpoint `/v1/models`.



---



## Horizon v2 "” LLM local embarqué (NOT YET)



Pour des cas plus complexes (changement de format `tool_calls`, nouveau champ `reasoning_content`, nouveau style de streaming SSE), un **petit LLM local** pourrait analyser les samples et proposer du code d'adaptation.



**Modèles candidats sur Hetzner sans GPU :**



| Modèle | RAM | Latence /req | Usage |

|--------|-----|-------------|-------|

| `phi-3-mini` (3.8B) | ~2.5 GB | ~200ms | Analyse de structure |

| `gemma-2-2b` (2B) | ~1.5 GB | ~150ms | Classification de format |

| `smollm2-1.7b` (1.7B) | ~1.1 GB | ~100ms | Extraction de patterns |



â†’ Ã€ évaluer selon la charge Hetzner actuelle. **Non prioritaire** tant que le FieldDiscoverer JSON couvre les cas courants (en pratique, c'est ~95% des cas).



---



## Tests



```bash

# Unit test field discoverer avec fixtures JSON de chaque provider

go test ./internal/workers/... -run TestFieldDiscoverer -v

```



**Vérification manuelle (v1 shipped) :**

1. âœ… Créer une clé avec un modèle non référencé (ex: `gpt-5-hypothetical`)

2. âœ… Envoyer 5 requêtes

3. âœ… Vérifier Redis : `synapse:radar:models:*` créé, `synapse:radar:samples:*` rempli (max 10)

4. âœ… Vérifier dashboard : modèle en statut `learning`

5. âœ… Après 3+ samples â†’ mapping proposé avec confidence score

6. âš ï¸ Le panneau dashboard visuel **n'est pas encore implémenté** "” utiliser `GET /api/admin/model-radar`



---



## Limitations connues



- **JSON très mal formé** : si un provider renvoie un `data:` SSE mal formé, le sample est collecté mais le FieldDiscoverer peut échouer. Solution : le radar entry reste en `learning` indéfiniment (visible dans le dashboard).

- **Confiance trop basse sur certains providers** : si le provider a des fields qui ne matchent aucun de nos keywords (ex: `input_text_tokens` au lieu de `input_tokens`), on retourne `confidence < 0.5` et on abandonne. Le radar entry reste en `learning` jusqu'Ã  intervention manuelle.

- **Pas de retry sur le promote** : si `PromoteKnown` est appelé pour un modèle qui a été flaggé `learning` mais que `ExtractUsage` retourne quand même 0 (rate limit, etc.), le modèle reste en `learning` et on accumule des samples. C'est conservateur mais ça veut dire qu'on peut accumuler 10 samples pour rien si le modèle est juste temporairement cassé.



---



## Plan d'implémentation



- [x] `model_radar.go` "” détection + collecte (~2h) **shipped**

- [x] `field_discoverer.go` "” algorithme d'analyse JSON (~3h) **shipped**

- [x] Modifier `proxy.go` "” intégration (~1h) **shipped**

- [x] Modifier `telemetry.go` "” appel Ã  `TryDiscoverForModel` après `CollectSample` (~30min) **shipped**

- [x] `GET /api/admin/model-radar` (~1h) **shipped**

- [ ] Dashboard panneau visuel (~2h)

- [ ] Tests unitaires FieldDiscoverer (~1h) "” tests manuels OK, unitaires Ã  ajouter



---



*Dernière mise Ã  jour : 2026-06-16 "” Model Radar v1 shipped in prod*


