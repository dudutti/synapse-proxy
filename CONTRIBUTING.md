# Contributing to OptiToken

Thank you for your interest in contributing! This document covers how to set up a dev environment, the structure of the codebase, and how to submit a Pull Request.

> **Important:** OptiToken follows an **Open Core** model. The proxy (`proxy/`) is open-source under MIT. The dashboard (`dashboard/`) and the marketing website are **closed-source, proprietary products** and **not** in this repository. This document only covers contributing to the proxy.

---

## 🏗️ Repository Structure

```
Optitoken/
├── proxy/                    # The Go reverse proxy (open-source, MIT)
│   ├── cmd/server/           # Main entry point
│   ├── internal/
│   │   ├── handlers/         # HTTP request handlers (proxy.go, models.go, ...)
│   │   ├── workers/          # Background workers (telemetry, benchmark, model_radar, ...)
│   │   ├── optiagent/        # L1/L2/L3/loop/tool-dedup/compaction-hint logic
│   │   ├── services/         # Auth, key validation
│   │   ├── utils/            # Tokens, savings, redactor (Zero-Log), cache validation, agent detector
│   │   └── db/               # Pricing data
│   ├── go.mod
│   └── Dockerfile
├── docker-compose.yml        # Local dev stack (Redis + ONNX + proxy)
├── docker-compose.prod.yml   # Production stack (Hetzner: + Postgres + Caddy)
├── Caddyfile, Caddyfile.prod # Reverse-proxy configs
├── README.md                 # Public-facing docs
├── DEPLOYMENT.md             # Production deployment guide
├── MODEL_RADAR.md            # Model Radar design + API
├── ROADMAP.md                # Strategic roadmap
├── CHANGELOG.md              # Version history
├── docs/                      # Additional architecture docs
└── dashboard/                # ⚠️ CLOSED-SOURCE — not part of the open-core contribution
```

The dashboard directory **exists** in the repository only for the maintainer's own deployment. **Do not submit PRs against it** — they will be rejected.

---

## 🛠️ Local Development Setup

### Prerequisites
- **Go 1.21+**
- **Docker & Docker Compose v2** (for Redis Stack + ONNX service)
- A real LLM provider key (OpenAI, Anthropic, etc.) — for end-to-end tests

### 1. Clone and start external services

```bash
git clone https://github.com/dudutti/Optitoken.git
cd Optitoken

# Start Redis Stack (VSS) and the ONNX embedder
docker compose up -d optitoken-redis optitoken-onnx
```

### 2. Run the Go proxy locally

```bash
cd proxy
go mod tidy
go run ./cmd/server
```

The proxy listens on `http://localhost:8080`. Set the `REDIS_URL` env var to point at your local Redis container if it's not on the default `optitoken_default` network.

### 3. Run the test suite

```bash
cd proxy
go test ./...
```

The test suite covers:
- Token usage extraction (`utils/tokens_test.go`)
- Per-class savings computation (`utils/savings_test.go`)
- Cache-poisoning detection (manual via the dashboard's "Purge cache" button)

### 4. End-to-end test

```bash
# A real key you got from your provider's dashboard
export REAL_KEY="sk-..."
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-opti-test" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"gpt-4o-mini\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"
```

The `sk-opti-test` virtual key needs to exist in Redis (`optitoken:keys:sk-opti-test`) and the real `sk-...` must be stored under the `apiKey` field. To skip the dashboard, write the hash directly with `docker exec optitoken-redis redis-cli HSET optitoken:keys:sk-opti-test apiKey $REAL_KEY provider openai default_model gpt-4o-mini cache_ttl 86400 semantic_tolerance 0.15 benchmark_mode false isolate_cache_by_user false zero_log false`.

---

## 💡 Contribution Guidelines

### Submitting Issues
Open a GitHub issue with as much context as possible:
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs from `docker logs optitoken-proxy`
- Provider name + model

### Submitting Pull Requests
1. **Fork** the repository and create a branch from `main`.
2. **Branch naming**: `feat/...`, `fix/...`, `docs/...`, `refactor/...`
3. **Format**: `gofmt` and `go vet` must pass.
4. **Tests**: Add unit tests for any new utility. The benchmark/playground code paths don't need tests.
5. **Commits**: Imperative mood, present tense ("add loop detection" not "added loop detection").
6. **PR description**: What problem it solves, how to test, screenshots if the change is visible in the dashboard.

### Adding a new LLM provider
1. Add the provider to the `models` switch in `proxy/internal/handlers/models.go`
2. Add the base URL routing in `proxy/internal/handlers/proxy.go` (look for `MiniMax`, `OpenAI`, `Anthropic`)
3. Add a model alias map in `proxy/internal/utils/provider_models.go` (for smart-aliasing)
4. Add pricing data in `proxy/internal/db/pricing.go`
5. Test with `curl -X POST http://localhost:8080/v1/chat/completions -H "Authorization: Bearer sk-opti-test" -H "Content-Type: application/json" -d '{"model":"<your-model>","messages":[]}`

### Adding a new detection rule
- For **agent detection**: add a regex in `proxy/internal/utils/agent_detector.go`
- For **upstream error detection**: add a pattern in `proxy/internal/utils/cache_validation.go`
- For **tool-call dedup**: add the tool name in `fileReadTools` in `proxy/internal/optiagent/tool_dedup.go`

---

## 🧪 Performance Guidelines

- **Don't** add blocking I/O in the hot path (`ProxyHandler`). Cache lookups are OK if they use `context.WithTimeout`. Avoid synchronous DB reads.
- **Do** use goroutines for telemetry, sample collection, and `PromoteKnown` / `TryDiscoverForModel` (fire-and-forget).
- **Do** respect `zeroLog`: any new persistence path that touches prompt/response content must be guarded by `if !zeroLog`.

---

## ⚖️ Code of Conduct

Be respectful, be welcoming. We are all here to make AI agent traffic more affordable and more observable.

---

*Happy Optimizing! 🚀*
