# Hook Pipeline

> Status: **Sprint 0 (shipped)** — Fingerprint migrated. CCR, ContentRouter and
> OutputShaper planned for Sprint 1-6.

The hook pipeline is the extension seam used by `internal/handlers/proxy.go` to
plug in cross-cutting request/response transformations without growing the
handler beyond its current size. It was introduced in Sprint 0 of the
Headroom-integration plan (see `.kilo/plans/headroom-integration/`) so that CCR,
ContentRouter and OutputShaper could land in subsequent sprints as
self-contained units instead of inline blocks.

## Why

`proxy.go` was 1685 lines as of mid-2026. Adding the planned features
inline would have pushed it past 3000 lines. The hook pipeline replaces
the stragger pattern used elsewhere in the codebase: each behavioural
unit becomes a Hook registered through `optiagent.RegisterHook(...)`
and called by `optiagent.RunBeforeHooks(...)` / `optiagent.RunAfterHooks(...)`
at well-defined points in the request lifecycle.

## Concepts

### Hook interface

```go
// proxy/optiagent/hooks.go
type Hook interface {
    Name() string                                       // stable identifier
    Priority() int                                      // lower = earlier
    IsEnabled(vk string) bool                           // per-VK feature flag
    BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error)
    AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error)
}
```

* `Name` MUST be stable across releases (used in metric labels).
* `Priority` controls execution order. Suggested buckets:

  | Range | Purpose                                              |
  |-------|------------------------------------------------------|
  | 100   | Observation / counting (Fingerprint, Dedup)          |
  | 200   | Payload transformation (CCR store, ContentRouter)    |
  | 300   | Prompt shaping (OutputShaper)                        |
  | 400   | Telemetry-only / post-processing                     |

* `IsEnabled` consults the per-VK feature flag. Return `false` to skip.
* `BeforeRequest` runs after auth+parse, before L1/L2/L3 cache lookup.
  Hooks MAY return a new payload (or `nil` to keep the runner-local
  one). Returning `(nil, err)` is treated as fail-open: the runner
  logs the error and uses the prior payload.
* `AfterResponse` runs after the upstream response is received.
  Same contract.

### HookContext

```go
type HookContext struct {
    VK, Provider, Model     string
    RawPayload              []byte
    OptimizedPayload        []byte // mutated across the chain
    FinalOptimizedPayload   []byte // set by the runner at the end
    UpstreamResponse        []byte
    UpstreamStatus          int
    ResponseHeaders         http.Header // mutated to add X-Synapse-*
    Features                map[string]interface{}
    ShortCircuitStatus      int
    ShortCircuitBody        []byte
    // ... agent/session metadata
}
```

`SetHeader` and `SetFeature` are safe helpers. `SetFeature` keys are
documented per-hook (e.g. `ccr_store`, `compression_strategy`,
`shaper_level`).

### Registry

```go
optiagent.RegisterHook(&MyHook{})  // typically from init()
optiagent.UnregisterHook("name")   // tests only
hooks := optiagent.GetHooks()      // snapshot
```

Hooks register themselves via `init()` in their source file. The
proxy.go handler calls the runner exactly twice — once before
`ProcessRequest` and once after the upstream response — and never
imports a hook directly.

## Fail-open contract

Every hook is expected to:

1. Never panic. The runner wraps every hook call with a `recover()`.
2. Never return an error that should abort the request. The runner
   logs the error, increments `synapse_hook_errors_total`, and
   continues with the prior payload.
3. Behave well when Redis is nil (`fingerprintRDB.Load() == nil`).
   This matters at startup before `db.InitRedis()` completes.

This contract is what lets us add new hooks to the pipeline without
risk: a misbehaving hook becomes a logged warning + an error counter,
not a 5xx storm.

## Performance budget

| Hook          | p50 budget | p99 budget |
|---------------|-----------:|-----------:|
| Fingerprint   |    0.5 ms  |     2 ms   |
| CCR (store)   |    1 ms    |     5 ms   |
| CCR (retrieve)|    2 ms    |    10 ms   |
| ContentRouter |    2 ms    |     8 ms   |
| OutputShaper  |    0.2 ms  |     1 ms   |
| **TOTAL**     |  **<6 ms** | **<15 ms** |

The runner records per-hook latency via `RecordHookLatency`. Hooks
that exceed 100ms emit a `slow` log line so we notice regressions
in CI.

## Observability

Three Prometheus series are emitted per hook, per VK:

```
synapse_hook_invocations_total{hook="...",vk="...",phase="before|after"}
synapse_hook_errors_total{hook="...",vk="...",kind="..."}
synapse_hook_latency_avg_us{hook="...",vk="..."}
```

Hook-specific counters are added by the hook itself (e.g. CCR will
add `synapse_ccr_retrievals_total`).

## Adding a new hook

1. Create `proxy/optiagent/hook_<name>.go`:

   ```go
   package optiagent

   type MyHook struct{}

   func (h *MyHook) Name() string                                   { return "myhook" }
   func (h *MyHook) Priority() int                                  { return 250 }
   func (h *MyHook) IsEnabled(vk string) bool                      { return isMyHookEnabled(vk) }
   func (h *MyHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
       // ... read hctx.OptimizedPayload, mutate, return new bytes or nil ...
   }
   func (h *MyHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
       return nil, nil
   }

   func init() { RegisterHook(&MyHook{}) }
   ```

2. Wire any per-VK feature flag in `internal/services/auth.go` and
   set it via `optiagent.SetMyHookEnabled(vk, true)` from
   `internal/handlers/proxy.go`.

3. Add unit tests under `proxy/optiagent/hook_<name>_test.go`
   following the patterns in `hook_fingerprint_test.go`.

4. Add a doc section here.

5. (Optional) Add a metric counter under `synapse_<hook>_*`.

## Sprint 0 migration: Fingerprint Loop Detect

The first hook to migrate was the inline fingerprint loop detection
that lived in `proxy.go` lines 282-297 (observation) and lines
496-510 (soft-loop injection). Both were extracted into
`optiagent/hook_fingerprint.go`, and the legacy `injectSystemWarning`
function was replaced by `optiagent.InjectSystemWarningCompat` (see
`proxy/optiagent/inject_helpers.go`).

Behavioural parity is verified by:

* `TestInjectSystemWarning_*` (byte stability, graceful failure modes)
* `TestFingerprintHook_*` (priority, IsEnabled, nil-Redis safe)
* `TestRunBeforeHooks_*` (runner semantics: priority order,
  fail-open, panic recovery, payload chain)

A golden test (`tests/golden/proxy.go.fingerprint_test.json`) checks
byte-equivalent behaviour end-to-end. It is expected to be added
in Sprint 1 alongside the CCR migration.

## What's next

Sprints 1-2 will add the CCR hook (`hook_ccr.go`), which:

1. Inspects tool-result blocks in `hctx.OptimizedPayload`.
2. Stores originals in Redis under `synapse:ccr:<vk>:<hash>`.
3. Injects a `synapse_retrieve` tool definition when compression
   exceeds a threshold.
4. Intercepts `tool_use(synapse_retrieve, ...)` in the upstream
   response and continues the conversation with the original.

See `.kilo/plans/headroom-integration/part-3-ccr.md` for the full
Sprint 1-2 design.