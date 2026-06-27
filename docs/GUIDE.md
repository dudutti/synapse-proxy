# Synapse Proxy — Guide Complet

> Document vivant. Dernière mise à jour : 2026-06-26.

---

## Table des matières

1. [Architecture générale](#1-architecture-générale)
2. [Stack technique](#2-stack-technique)
3. [Pipeline de requête](#3-pipeline-de-requête)
4. [Hook pipeline](#4-hook-pipeline)
5. [Configuration des clés virtuelles](#5-configuration-des-clés-virtuelles)
6. [Administration locale](#6-administration-locale)
7. [Administration serveur](#7-administration-serveur)
8. [Dashboard](#8-dashboard)
9. [Méthodologie TDD](#9-méthodologie-tdd)
10. [Troubleshooting](#10-troubleshooting)

---

## 1. Architecture générale

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────┐
│  Client SDK  │────▶│  Proxy Go    │────▶│  Provider LLM    │
│  (OpenAI)    │◀────│  :8080       │◀────│  (OpenAI/MiniMax) │
└──────────────┘     └──────┬───────┘     └──────────────────┘
                           │
                    ┌──────┴───────┐
                    │              │
              ┌─────▼─────┐  ┌────▼─────┐
              │  Redis     │  │ Postgres │
              │  :6379     │  │ :5432    │
              │  (cache +  │  │ (users,  │
              │   vectors) │  │  logs,   │
              └────────────┘  │  keys)   │
                              └──────────┘
                    │
              ┌─────▼──────┐
              │ Dashboard  │
              │ Next.js    │
              │ :3000      │
              └────────────┘
```

**4 services** composent le système :
- **Proxy** (Go + Rust embedder) — data plane, :8080
- **Dashboard** (Next.js 14 + Prisma) — control plane, :3000
- **Redis** (Redis Stack + RediSearch) — cache L1/L2/CCR, vecteurs, métriques
- **PostgreSQL** — users, virtual keys, request logs, sessions

---

## 2. Stack technique

| Composant | Technologie | Fichiers clés |
|-----------|------------|---------------|
| Proxy data plane | Go 1.21 | `proxy/cmd/server/main.go`, `proxy/internal/handlers/proxy.go` |
| Embedder L2 | Rust (candle + MiniLM-L12-v2) | `proxy/rust-embedder/` |
| Hook pipeline | Go | `proxy/optiagent/hooks.go`, `proxy/optiagent/hook_*.go` |
| Cache L1/L2/CCR | Redis Stack 7.4 + RediSearch | `proxy/internal/db/redis.go` |
| Dashboard | Next.js 14 + TailwindCSS | `dashboard/app/page.tsx` |
| ORM | Prisma | `dashboard/prisma/schema.prisma` |
| Auth | NextAuth.js | `dashboard/app/api/auth/` |
| Conteneurisation | Docker Compose | `docker-compose.local.yml` |
| MCP Server | Go (JSON-RPC 2.0) | `proxy/internal/mcp/server.go` |
| Métriques | Prometheus | `proxy/internal/metrics/metrics.go` |

### Arborescence proxy

```
proxy/
├── cmd/server/main.go          # Point d'entrée (HTTP :8080 ou MCP)
├── internal/
│   ├── handlers/proxy.go       # Handler principal (1700 lignes)
│   ├── services/auth.go        # Auth VK → real key
│   ├── services/crypto.go      # AES-GCM decrypt
│   ├── db/redis.go             # Connexion Redis
│   ├── db/postgres.go          # Connexion Postgres
│   ├── workers/telemetry.go    # Background telemetry
│   ├── workers/retention.go    # Cleanup selon tier
│   ├── metrics/metrics.go      # Prometheus counters
│   └── mcp/                    # MCP server (stdio + HTTP)
├── optiagent/
│   ├── hooks.go                # Interface Hook + Runner
│   ├── engine_switch.go        # Strangler Fig kill switch
│   ├── hook_fingerprint.go     # Agent fingerprinting
│   ├── hook_session_cb.go      # Session circuit breaker
│   ├── hook_tool_filter.go     # Tool dedup
│   ├── hook_loop_detection.go  # Loop detection
│   ├── hook_agent_discovery.go # Agent auto-discovery
│   ├── hook_ccr_compress.go    # CCR canonicalization (priority 740)
│   ├── hook_ccr_retrieve.go    # CCR cache lookup (priority 750)
│   ├── hook_ccr_store.go       # CCR cache store (AfterResponse)
│   ├── hook_ccr_tool_injection.go # synapse_retrieve tool
│   ├── hook_log_compressor.go  # Log/JSON compression
│   └── hook_tag_protector.go   # HTML/MD tag protection
└── rust-embedder/              # Rust FFI embedder
```

---

## 3. Pipeline de requête

Chaque requête `POST /v1/chat/completions` traverse ce pipeline :

```
Client → [1. Auth] → [2. Parse] → [3. CompactionHint]
       → [4. L0 Dedup] → [5. Hooks BeforeRequest]
       → [6. Engine/Bypass] → [7. L1/L2 Cache]
       → [8. Upstream Call] → [9. Tool Intercept Loop]
       → [10. Hooks AfterResponse] → [11. StreamResponse]
       → [12. Cache Store + Telemetry]
```

### Détail des étapes

1. **Auth** (`services/auth.go`) — Extrait le Bearer token `sk-opti-...`, cherche dans Redis `synapse:keys:<vk>`, décrypte la `real_key` avec AES-GCM.

2. **Parse** — Extrait `model`, `messages`, `stream`, `tools` du body JSON.

3. **CompactionHint** — Injecte un system message `(Earlier tool results may be truncated.)` pour préparer le LLM à la compression.

4. **L0 Dedup** — Si la même requête arrive en parallèle (même VK + même hash), une seule va upstream. Les autres attendent le résultat (coalescing).

5. **Hooks BeforeRequest** — Exécute tous les hooks enregistrés en ordre de `Priority()`. Peut short-circuiter (cache CCR hit) ou muter le payload.

6. **Engine/Bypass** — Si `EngineDisabled() == true` (défaut), utilise le payload tel quel. Sinon, passe par `ProcessRequest` (legacy).

7. **L1/L2 Cache** — L1 = byte-exact match Redis. L2 = cosine similarity via vecteur d'embedding (Rust embedder).

8. **Upstream Call** — Construit la requête HTTP vers le provider (OpenAI, MiniMax, Anthropic, etc.) avec la `real_key` décryptée.

9. **Tool Intercept Loop** — Si la réponse contient des `tool_calls` et que tous les résultats sont en cache, injecte les résultats et rappelle upstream automatiquement.

10. **Hooks AfterResponse** — Post-traitement (CCR store, métriques).

11. **StreamResponse** — Forward les chunks SSE au client, reconstruit le JSON complet pour le cache.

12. **Cache Store + Telemetry** — Stocke en L1/L2, pousse la télémétrie dans Postgres via worker async.

---

## 4. Hook pipeline

### Interface

```go
type Hook interface {
    Name() string                                          // Identifiant unique
    Priority() int                                         // Ordre d'exécution (plus bas = premier)
    IsEnabled(vk string) bool                              // Feature flag par VK
    BeforeRequest(ctx, hctx *HookContext) ([]byte, error)  // Avant upstream
    AfterResponse(ctx, hctx *HookContext) ([]byte, error)  // Après upstream
}
```

### Hooks enregistrés (ordre d'exécution)

| Priority | Hook | Phase | Rôle |
|----------|------|-------|------|
| 100 | `fingerprint` | Before | Identifie l'agent (Cursor, Claude, etc.) |
| 150 | `session_cb` | Before | Circuit breaker par session |
| 200 | `tool_filter` | Before | Déduplique les tools identiques |
| 250 | `loop_detection` | Before | Détecte les boucles d'appels |
| 300 | `agent_discovery` | Before | Auto-découverte des agents |
| 740 | `ccr_compress` | Before | Canonicalise le payload (whitespace, CRLF) |
| 750 | `ccr_retrieve` | Before | Cache lookup CCR (peut short-circuiter) |
| 800 | `log_compressor` | Before | Compresse les logs/JSON volumineux |
| 850 | `ccr_store` | After | Stocke la réponse dans le cache CCR |
| 900 | `ccr_tool_injection` | Before | Injecte l'outil `synapse_retrieve` |

### Contrat fail-open

Chaque hook **DOIT** traiter les erreurs comme non-fatales :
- Erreur Redis → on continue avec le payload précédent
- Panic → recovery automatique, compteur incrémenté
- Budget perf : 6ms p50 / 15ms p99 pour tout le pipeline

### HookContext

Chaque requête reçoit son propre `HookContext` avec :
- `VK`, `Provider`, `Model` — identité de la requête
- `RawPayload`, `OptimizedPayload`, `FinalOptimizedPayload` — chaîne de payloads
- `ShortCircuitStatus/Body` — pour court-circuiter (ex: cache hit)
- `Features map[string]interface{}` — état partagé entre hooks

---

## 5. Configuration des clés virtuelles

### Structure Redis

Chaque VK est un hash Redis `synapse:keys:<vk>` :

```
HGETALL synapse:keys:sk-opti-xxxx
  real_key              → <clé chiffrée AES-GCM du provider>
  provider              → openai | minimax | anthropic | groq | mistral
  default_model         → gpt-4o | MiniMax-M2.7 | ...
  fallback_model        → (optionnel) modèle de fallback
  fallback_provider     → (optionnel) provider de fallback
  cache_ttl             → 86400 (secondes)
  semantic_tolerance    → 0.15 (seuil cosine L2)
  zero_log              → false (ne pas stocker les payloads)
  benchmark_mode        → false
  limit_exceeded        → false (VK dépassée)
  isolate_cache_by_user → false
```

### Chiffrement des clés

Les `real_key` sont chiffrées avec **AES-256-GCM** :
- Dashboard chiffre avec `ENCRYPTION_KEY` lors de la création
- Proxy déchiffre à chaque requête via `services/crypto.go`
- La même `ENCRYPTION_KEY` doit être partagée entre proxy et dashboard

### Synchronisation dashboard → Redis

Le script `dashboard/sync_keys.js` lit les VK de Postgres et les écrit dans Redis :

```bash
# Depuis le container dashboard
node sync_keys.js
```

En production, les VK sont synchronisées automatiquement via l'API dashboard.

---

## 6. Administration locale

### Prérequis

- Docker Desktop (Windows/Mac) ou Docker Engine (Linux)
- Node.js 18+ (pour le dashboard en dev)
- Go 1.21+ (pour compiler le proxy localement)
- Rust (pour le Rust embedder, optionnel si Docker)

### Démarrage complet

```bash
cd G:/Optitoken
docker compose -f docker-compose.local.yml up --build
```

Les 4 services démarrent :
- **Redis** : `localhost:6380` (port 6380 pour éviter conflit avec Redis local)
- **Postgres** : `localhost:5432`
- **Proxy** : `localhost:8080`
- **Dashboard** : `localhost:3000`

### Rebuild du proxy seul

```bash
docker compose -f docker-compose.local.yml up --build proxy -d --no-deps --force-recreate
```

Temps de build :
- Premier build : ~7 minutes (compilation Rust embedder)
- Rebuilds suivants : ~25 secondes (Rust en cache, seul Go recompile)

### Variables d'environnement (docker-compose.local.yml)

| Variable | Service | Valeur locale |
|----------|---------|---------------|
| `REDIS_ADDR` | proxy | `redis:6379` |
| `REDIS_PASSWORD` | proxy/redis | `localdev-redis-pw` |
| `DATABASE_URL` | proxy/dashboard | `postgresql://optitoken:optitoken@postgres:5432/optitoken_db` |
| `ENCRYPTION_KEY` | proxy/dashboard | `5a8e1b...` (dev only) |
| `SEED_ADMIN_EMAIL` | dashboard | `admin@synapse.local` |
| `SEED_ADMIN_PASSWORD` | dashboard | `admin1234` |

### Connexion au dashboard local

1. Ouvrir `http://localhost:3000`
2. Login : `admin@synapse.local` / `admin1234`
3. Créer une Virtual Key dans Settings → Keys
4. Tester avec curl :

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-opti-votre-cle" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

### Commandes Redis utiles

```bash
# Lister les VK
docker exec synapse-redis redis-cli -a localdev-redis-pw KEYS "synapse:keys:*"

# Voir la config d'une VK
docker exec synapse-redis redis-cli -a localdev-redis-pw HGETALL "synapse:keys:sk-opti-xxxx"

# Voir les métriques d'un hook
docker exec synapse-redis redis-cli -a localdev-redis-pw KEYS "synapse:hook:*"

# Flush le cache L1 d'une VK
docker exec synapse-redis redis-cli -a localdev-redis-pw --scan --pattern "synapse:l1cache:sk-opti-xxxx:*" | xargs docker exec -i synapse-redis redis-cli -a localdev-redis-pw DEL
```

### Logs du proxy

```bash
docker logs synapse-proxy --tail 50 -f
```

### Tests Go

```bash
cd proxy
go test ./...                    # Tous les tests
go test ./optiagent/... -v       # Tests hooks avec verbose
go test -run TestCCRCompress     # Un test spécifique
```

---

## 7. Administration serveur (production)

### Accès SSH

```bash
# Depuis Windows (PuTTY/plink)
ssh root@synapse-proxy.com

# Ou via le .env
# SSH=4dk3m4kb12$#1234
```

### Stack production

La prod utilise `docker-compose.prod.yml` avec :
- **Caddy** comme reverse proxy TLS (auto HTTPS)
- Redis **sans** port exposé (réseau Docker interne)
- Postgres **sans** port exposé
- `ENCRYPTION_KEY` de prod (différente du dev)

### Déploiement

```bash
# Sur le serveur
cd /opt/synapse
git pull
docker compose -f docker-compose.prod.yml up --build -d
```

### Monitoring

| Endpoint | Usage |
|----------|-------|
| `GET /healthz` | Liveness probe (toujours 200) |
| `GET /readyz` | Readiness probe (200 si Redis ok, 503 sinon) |
| `GET /metrics` | Prometheus text exposition |

### Métriques Prometheus clés

```
synapse_requests_total{vk, provider, model, cache_level}
synapse_tokens_saved_total{vk}
synapse_upstream_latency_seconds{provider}
synapse_hook_latency_seconds{hook_name}
synapse_hook_errors_total{hook_name, error_type}
synapse_cache_hits_total{level}  # L0, L1, L2, CCR
```

### Backup

```bash
# Backup Postgres
docker exec synapse-postgres pg_dump -U optitoken_admin optitoken_db > backup.sql

# Backup Redis (snapshot)
docker exec synapse-redis redis-cli -a $REDIS_PASSWORD BGSAVE
docker cp synapse-redis:/data/dump.rdb ./redis-backup.rdb
```

---

## 8. Dashboard

### Pages principales

| Route | Rôle | Accès |
|-------|------|-------|
| `/` | Landing page publique | Public |
| `/login` | Connexion | Public |
| `/settings` | Gestion VK, profil | USER |
| `/explorer` | Request logs, live telemetry | USER |
| `/sessions` | Sessions agents | USER |
| `/benchmark` | Mode benchmark | USER |
| `/playground` | Test en direct | USER |
| `/admin` | Admin panel | SUPERADMIN |
| `/admin/blog` | Blog CMS | SUPERADMIN |
| `/plans` | Plans tarifaires | Public |

### API routes principales

| Route | Méthode | Rôle |
|-------|---------|------|
| `/api/auth/[...nextauth]` | GET/POST | NextAuth callbacks |
| `/api/keys` | GET/POST/DELETE | CRUD virtual keys |
| `/api/keys/sync` | POST | Sync VK → Redis |
| `/api/models` | GET | Liste des modèles (proxy-through) |
| `/api/requests` | GET | Historique des requêtes |
| `/api/stats` | GET | Statistiques agrégées |

### Rôles

- **USER** — peut créer des VK, voir ses logs, configurer
- **SUPERADMIN** — accès admin, gestion utilisateurs, blog

---

## 9. Méthodologie TDD

### Principe

Chaque nouvelle fonctionnalité du proxy suit le cycle **Red → Green → Refactor** :

1. **Red** — Écrire les tests AVANT le code. Ils doivent échouer.
2. **Green** — Écrire le minimum de code pour faire passer les tests.
3. **Refactor** — Nettoyer le code en gardant les tests au vert.

### Structure des tests

```
proxy/optiagent/
├── hook_ccr_compress.go           # Code
├── hook_ccr_compress_test.go      # Tests ← même package
```

### Écrire un nouveau hook — Template TDD

#### Étape 1 : Le fichier de test (RED)

```go
// hook_my_feature_test.go
package optiagent

import (
    "context"
    "testing"
)

func TestMyFeatureHook_Name(t *testing.T) {
    h := &MyFeatureHook{}
    if h.Name() != "my_feature" {
        t.Errorf("want my_feature, got %s", h.Name())
    }
}

func TestMyFeatureHook_Priority(t *testing.T) {
    h := &MyFeatureHook{}
    if h.Priority() < 100 || h.Priority() > 999 {
        t.Errorf("priority %d out of range", h.Priority())
    }
}

func TestMyFeatureHook_BeforeRequest_NilContext(t *testing.T) {
    h := &MyFeatureHook{}
    // DOIT être nil-safe (contrat fail-open)
    out, err := h.BeforeRequest(context.Background(), nil)
    if err != nil {
        t.Errorf("nil hctx should not error: %v", err)
    }
    if out != nil {
        t.Errorf("nil hctx should return nil payload")
    }
}

func TestMyFeatureHook_BeforeRequest_EmptyPayload(t *testing.T) {
    h := &MyFeatureHook{}
    hctx := &HookContext{
        VK:               "sk-opti-test",
        OptimizedPayload: []byte{},
        Features:         make(map[string]interface{}),
    }
    out, err := h.BeforeRequest(context.Background(), hctx)
    if err != nil {
        t.Errorf("empty payload should not error: %v", err)
    }
    // Empty payload = no-op
    if len(out) > 0 {
        t.Errorf("empty payload should return nil, got %d bytes", len(out))
    }
}

func TestMyFeatureHook_BeforeRequest_HappyPath(t *testing.T) {
    h := &MyFeatureHook{}
    payload := []byte(`{"messages":[{"role":"user","content":"test"}]}`)
    hctx := &HookContext{
        VK:               "sk-opti-test",
        OptimizedPayload: payload,
        Features:         make(map[string]interface{}),
    }
    out, err := h.BeforeRequest(context.Background(), hctx)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    // Vérifier la mutation attendue
    if out == nil {
        t.Error("expected mutated payload, got nil")
    }
}
```

#### Étape 2 : Le code (GREEN)

```go
// hook_my_feature.go
package optiagent

import (
    "context"
    "log"
)

type MyFeatureHook struct{}

func (h *MyFeatureHook) Name() string     { return "my_feature" }
func (h *MyFeatureHook) Priority() int    { return 350 }
func (h *MyFeatureHook) IsEnabled(_ string) bool { return true }

func (h *MyFeatureHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
    IncrementBefore(h.Name(), hctx.VK)
    if hctx == nil || len(hctx.OptimizedPayload) == 0 {
        return nil, nil
    }
    // ... logique métier ...
    return hctx.OptimizedPayload, nil
}

func (h *MyFeatureHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
    IncrementAfter(h.Name(), hctx.VK)
    return nil, nil
}

func init() {
    RegisterHook(&MyFeatureHook{})
    log.Printf("[hooks] registered MyFeatureHook at priority 350")
}
```

#### Étape 3 : Lancer les tests

```bash
cd proxy
go test ./optiagent/ -run TestMyFeature -v
```

### Tests obligatoires pour chaque hook

| # | Test | Pourquoi |
|---|------|----------|
| 1 | `Name()` retourne une string stable | Dashboards/métriques en dépendent |
| 2 | `Priority()` est dans la bonne plage | Ordre d'exécution critique |
| 3 | `BeforeRequest(nil)` → no-op | Contrat fail-open |
| 4 | Payload vide → no-op | Défensif |
| 5 | Payload valide → mutation correcte | Happy path |
| 6 | Payload malformé → no-op sans erreur | Fail-open |
| 7 | `IsEnabled("vk")` retourne le bon flag | Feature flag |

### Tests e2e

```bash
# Lancer le stack local
docker compose -f docker-compose.local.yml up --build -d

# Attendre que le proxy soit ready
curl -s http://localhost:8080/readyz

# Envoyer une requête de test
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-opti-xxxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}]}'

# Vérifier dans les logs
docker logs synapse-proxy --tail 20

# Vérifier dans le dashboard
# → http://localhost:3000/explorer
```

### Convention de nommage des tests

```
Test<HookName>_<Method>_<Scenario>

Exemples :
  TestCCRCompressHook_BeforeRequest_PreservesToolCallArguments
  TestLoopDetection_BeforeRequest_DetectsThirdRepeat
  TestFingerprint_BeforeRequest_IdentifiesCursor
```

---

## 10. Troubleshooting

### Erreurs communes

| Erreur | Cause | Fix |
|--------|-------|-----|
| `2013: missing required parameter` | Payload vide envoyé upstream | Bug shadowing `optResult` (fixé) |
| `2056: Token Plan usage limit` | Quota provider épuisé | Recharger le plan MiniMax |
| `upstream payload len=0` | Variable shadowing dans proxy.go | Vérifier pas de `var optResult` dans un `if` block |
| `redis: nil` | Cache miss (normal) | Pas une erreur, c'est le path normal |
| `hook PANIC` | Bug dans un hook | Vérifier les logs, le hook fail-open |
| `Streaming unsupported` | ResponseWriter ne supporte pas Flush | Bug infra rare |
| `ENCRYPTION_KEY mismatch` | Proxy et dashboard ont des clés différentes | Aligner dans docker-compose |

### Debug d'un hook

```bash
# 1. Vérifier que le hook est enregistré
docker logs synapse-proxy 2>&1 | grep "registered.*Hook"

# 2. Vérifier qu'il fire
docker logs synapse-proxy 2>&1 | grep "<hook_name>"

# 3. Vérifier ses métriques
docker exec synapse-redis redis-cli -a localdev-redis-pw \
  KEYS "synapse:hook:*<hook_name>*"
```

### Debug d'une VK qui ne marche pas

```bash
# 1. Vérifier que la VK existe dans Redis
docker exec synapse-redis redis-cli -a localdev-redis-pw \
  EXISTS "synapse:keys:sk-opti-xxxx"

# 2. Vérifier la real_key
docker exec synapse-redis redis-cli -a localdev-redis-pw \
  HGET "synapse:keys:sk-opti-xxxx" real_key

# 3. Vérifier le provider
docker exec synapse-redis redis-cli -a localdev-redis-pw \
  HGET "synapse:keys:sk-opti-xxxx" provider

# 4. Tester le decrypt
# Si real_key est vide ou "ERR", la sync_keys n'a pas tourné
```

### Performance

```bash
# Voir la latence des hooks
docker exec synapse-redis redis-cli -a localdev-redis-pw \
  KEYS "synapse:hook:latency:*"

# Voir les erreurs des hooks
docker exec synapse-redis redis-cli -a localdev-redis-pw \
  KEYS "synapse:hook:error:*"
```


