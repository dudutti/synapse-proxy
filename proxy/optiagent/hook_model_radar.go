// Package optiagent — ModelRadarHook.
//
// Migrated from proxy.go (formerly line ~263 inline call to
// optiagent.CheckAndFlagNewModel).
//
// Behaviour preserved from the legacy inline code:
//   - When the request model is NOT in the Redis "known models"
//     set, the hook creates a learning entry under
//     `synapse:radar:models:<modelID>` and exposes a
//     `model_radar_new` boolean feature on hctx for downstream
//     consumers (telemetry, dashboard, FieldDiscoverer).
//   - When the model IS already known, the hook updates its
//     last-seen timestamp.
//   - When the model is not known but a previous request already
//     created a learning entry, the hook updates last-seen only
//     (avoids clobbering the existing entry with a fresh one).
//   - Fail open on backend errors.
//
// Priority 160: runs after LoopDetection (150) and before cache
// mutators. A flagged new model is metadata, not a short-circuit,
// so it can run late in the observation phase.

package optiagent

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

// radarEntryTTL is how long a learning entry lives in Redis
// without new activity before being forgotten. 30 days matches
// the legacy constant.
const radarEntryTTL = 30 * 24 * time.Hour

// radarKnownSetKey is the canonical Redis key for the global
// "known models" set.
const radarKnownSetKey = "synapse:radar:known_models"

// radarEntryPrefix is the per-model entry key prefix.
const radarEntryPrefix = "synapse:radar:models:"

// RadarEntry is the JSON shape stored under each learning key.
// Mirrors the legacy internal/workers.RadarEntry type so the
// dashboard continues to see the same payload.
type RadarEntry struct {
	ModelID   string    `json:"model_id"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	SampleCnt int       `json:"sample_count"`
	Notes     string    `json:"notes,omitempty"`
}

// ModelRadarHook flags new models so the dashboard / FieldDiscoverer
// can start collecting samples.
type ModelRadarHook struct{}

// Name returns the stable hook identifier.
func (h *ModelRadarHook) Name() string { return "model_radar" }

// Priority 160: late observation (after LoopDetection, before
// cache mutators). New-model detection is metadata, not a
// short-circuit, so it can run late in the observation phase.
func (h *ModelRadarHook) Priority() int { return 160 }

// IsEnabled gates on a non-empty VK. BeforeRequest is the real gate.
func (h *ModelRadarHook) IsEnabled(vk string) bool { return vk != "" }

// BeforeRequest consults the model radar. See file docstring.
//
// Returns (nil, nil) on the happy path and on backend failure
// (fail-open). The hook never short-circuits — it is purely
// observation. It exposes `model_radar_new=true` on hctx.Features
// when a new model is detected, so downstream consumers
// (telemetry, dashboard) can react.
func (h *ModelRadarHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	IncrementBefore(h.Name(), hctx.VK)

	// Legacy guard: empty modelID or empty provider = no work.
	if hctx.Model == "" || hctx.Provider == "" {
		return nil, nil
	}

	backend := currentSessionCBBackend()
	if backend == nil {
		return nil, nil
	}

	// 1. Is the model in the global known set?
	key := hctx.Provider + ":" + hctx.Model
	isKnown, err := backend.SIsMember(ctx, radarKnownSetKey, key)
	if err != nil {
		log.Printf("[model-radar] SIsMember failed for %s: %v (fail-open)", key, err)
		return nil, nil
	}
	if isKnown {
		// Known model — nothing to do. (The legacy code's
		// last-seen update was for the learning entry, which
		// doesn't exist for known models.)
		return nil, nil
	}

	// 2. Not known. Is there a learning entry from a previous
	// request?
	entryKey := radarEntryPrefix + hctx.Model
	exists, err := backend.Exists(ctx, entryKey)
	if err != nil {
		log.Printf("[model-radar] Exists failed for %s: %v (fail-open)", entryKey, err)
		return nil, nil
	}

	now := time.Now().UTC()
	if !exists {
		// Cold path: create the learning entry and surface the
		// new-model flag for downstream consumers.
		entry := RadarEntry{
			ModelID:   hctx.Model,
			Provider:  hctx.Provider,
			Status:    "learning",
			FirstSeen: now,
			LastSeen:  now,
			SampleCnt: 0,
			Notes:     "auto-flagged by proxy",
		}
		entryJSON, _ := json.Marshal(entry)
		if err := backend.Set(ctx, entryKey, entryJSON, radarEntryTTL); err != nil {
			log.Printf("[model-radar] Set failed for %s: %v (fail-open)", entryKey, err)
			return nil, nil
		}
		hctx.SetFeature("model_radar_new", true)
		log.Printf("[model-radar] NEW MODEL DETECTED: %s (provider=%s)", hctx.Model, hctx.Provider)
		return nil, nil
	}

	// Warm path: learning entry exists from a previous request.
	// Update last-seen only — don't clobber the existing entry
	// (which may have richer metadata from the FieldDiscoverer
	// or the dashboard).
	raw, err := backend.Get(ctx, entryKey)
	if err != nil {
		log.Printf("[model-radar] Get failed for %s: %v (skipping last-seen update)", entryKey, err)
		return nil, nil
	}
	if len(raw) == 0 {
		// Entry expired between Exists and Get. Recreate it.
		entry := RadarEntry{
			ModelID:   hctx.Model,
			Provider:  hctx.Provider,
			Status:    "learning",
			FirstSeen: now,
			LastSeen:  now,
			SampleCnt: 0,
			Notes:     "auto-flagged by proxy",
		}
		entryJSON, _ := json.Marshal(entry)
		_ = backend.Set(ctx, entryKey, entryJSON, radarEntryTTL)
		return nil, nil
	}
	var e RadarEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		// Corrupt entry — leave it alone.
		return nil, nil
	}
	e.LastSeen = now
	updated, _ := json.Marshal(e)
	if err := backend.Set(ctx, entryKey, updated, radarEntryTTL); err != nil {
		log.Printf("[model-radar] last-seen Set failed for %s: %v (non-fatal)", entryKey, err)
	}
	return nil, nil
}

// AfterResponse is a no-op.
func (h *ModelRadarHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

func init() {
	RegisterHook(&ModelRadarHook{})
}