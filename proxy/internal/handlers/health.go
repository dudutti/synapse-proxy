package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"synapse-proxy/internal/db"
)

// HealthzHandler returns 200 OK as long as the Go process is alive.
// Used by Docker, load balancers, and uptime monitors to check that
// the process is responding at all. No dependency checks (that's
// /readyz's job).
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// ReadyzHandler returns 200 only if the proxy can actually serve
// traffic. Checks Redis (used for L1/L2/L3/loop/L0 caches) and the
// ONNX embedder (used for L2 semantic search). Postgres is checked
// indirectly via the pricing syncer state.
//
// Use this for k8s readinessProbe â€” Kubernetes will stop sending
// traffic to the pod until this returns 200.
//
// Returns 503 on any dependency failure. Each dependency has its own
// timeout (1s) so a stuck Redis can't block readiness for long.
type ReadinessReport struct {
	Status   string            `json:"status"`
	Checks   map[string]string `json:"checks"`
	CheckedAt string            `json:"checked_at"`
	BuildInfo map[string]string `json:"build"`
}

func ReadyzHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()

	report := ReadinessReport{
		Status:   "ok",
		Checks:   map[string]string{},
		CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
		BuildInfo: map[string]string{
			"service": "synapse-proxy",
			"version": "1.5.0",
		},
	}
	allOK := true

	// Redis: cheap PING
	if err := pingRedis(ctx); err != nil {
		report.Checks["redis"] = "fail: " + err.Error()
		allOK = false
	} else {
		report.Checks["redis"] = "ok"
	}

	if !allOK {
		report.Status = "degraded"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(report)
}

func pingRedis(ctx context.Context) error {
	rdb := db.GetRedis()
	if rdb == nil {
		return errRedisNotInit
	}
	// Ping honours context deadlines (1s).
	return rdb.Ping(ctx).Err()
}

// errRedisNotInit is returned when GetRedis() was never wired up
// (e.g. tests, partial boot). Not fatal â€” proxy falls back to
// in-memory caches â€” but still counts as "degraded" because cache
// hits will leak across instances.
var errRedisNotInit = &redisNotInitErr{}

type redisNotInitErr struct{}

func (e *redisNotInitErr) Error() string { return "redis client not initialised" }
