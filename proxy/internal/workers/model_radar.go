package workers

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// ModelRadarStatus represents the learning state of a model.
type ModelRadarStatus string

const (
	RadarStatusLearning ModelRadarStatus = "learning"
	RadarStatusKnown    ModelRadarStatus = "known"
	RadarStatusMapped   ModelRadarStatus = "mapped"
)

// RadarEntry is a single model entry stored under Synapse Proxy:radar:models:{modelID}.
type RadarEntry struct {
	ModelID    string           `json:"model_id"`
	Provider   string           `json:"provider"`
	Status     ModelRadarStatus `json:"status"`
	FirstSeen  time.Time        `json:"first_seen"`
	LastSeen   time.Time        `json:"last_seen"`
	SampleCnt  int              `json:"sample_count"`
	UsageMap   json.RawMessage  `json:"usage_map,omitempty"`
	Notes      string           `json:"notes,omitempty"`
}

const (
	radarKnownSetKey = "synapse:radar:known_models" // SET of "provider:modelID"
	radarEntryPrefix = "synapse:radar:models:"      // STRING per model, JSON-encoded RadarEntry
	radarSamplesKey  = "synapse:radar:samples:"     // LIST of raw responses
	maxSamplesPerMod = 10
	radarEntryTTL    = 30 * 24 * time.Hour
)

// CheckAndFlagNewModel returns true if the (provider, modelID) pair is
// not in the known models set. In that case it also creates a radar entry
// with status=learning so we can start collecting samples.
func CheckAndFlagNewModel(ctx context.Context, rdb *redis.Client, provider, modelID string) bool {
	if rdb == nil || modelID == "" {
		return false
	}
	key := provider + ":" + modelID
	isMember, err := rdb.SIsMember(ctx, radarKnownSetKey, key).Result()
	if err != nil {
		log.Printf("[ModelRadar] SISMEMBER failed: %v", err)
		return false
	}
	if isMember {
		return false
	}

	// Not known. Try to create a learning entry.
	entryKey := radarEntryPrefix + modelID
	exists, _ := rdb.Exists(ctx, entryKey).Result()
	now := time.Now().UTC()
	if exists == 0 {
		entry := RadarEntry{
			ModelID:   modelID,
			Provider:  provider,
			Status:    RadarStatusLearning,
			FirstSeen: now,
			LastSeen:  now,
			SampleCnt: 0,
			Notes:     "auto-flagged by proxy",
		}
		entryJSON, _ := json.Marshal(entry)
		if err := rdb.Set(ctx, entryKey, entryJSON, radarEntryTTL).Err(); err != nil {
			log.Printf("[ModelRadar] failed to write entry: %v", err)
		}
		log.Printf("[ModelRadar] NEW MODEL DETECTED: %s (provider=%s)", modelID, provider)
	} else {
		// Update last-seen.
		raw, err := rdb.Get(ctx, entryKey).Bytes()
		if err == nil {
			var e RadarEntry
			if json.Unmarshal(raw, &e) == nil {
				e.LastSeen = now
				updated, _ := json.Marshal(e)
				rdb.Set(ctx, entryKey, updated, radarEntryTTL)
			}
		}
	}
	return true
}

// CollectSample stores a raw response body for later offline analysis.
// Uses LPUSH + LTRIM to keep only the most recent N samples.
func CollectSample(ctx context.Context, rdb *redis.Client, modelID string, rawResponse []byte) {
	if rdb == nil || modelID == "" || len(rawResponse) == 0 {
		return
	}
	key := radarSamplesKey + modelID
	pipe := rdb.Pipeline()
	pipe.LPush(ctx, key, rawResponse)
	pipe.LTrim(ctx, key, 0, maxSamplesPerMod-1)
	pipe.Expire(ctx, key, radarEntryTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[ModelRadar] failed to collect sample for %s: %v", modelID, err)
	}
}

// RegisterKnownModels seeds the known-models set from a successful
// /v1/providers/models fetch. Safe to call concurrently; uses SAdd.
func RegisterKnownModels(ctx context.Context, rdb *redis.Client, provider string, modelIDs []string) {
	if rdb == nil || provider == "" || len(modelIDs) == 0 {
		return
	}
	members := make([]interface{}, 0, len(modelIDs))
	for _, m := range modelIDs {
		if m == "" {
			continue
		}
		members = append(members, provider+":"+m)
	}
	if len(members) == 0 {
		return
	}
	if err := rdb.SAdd(ctx, radarKnownSetKey, members...).Err(); err != nil {
		log.Printf("[ModelRadar] SADD failed for provider=%s: %v", provider, err)
		return
	}
	log.Printf("[ModelRadar] Registered %d known models for provider=%s", len(members), provider)
}

// PromoteKnown adds a (provider, modelID) pair to the known-models set
// and upgrades the radar entry from "learning" to "known". Called by the
// proxy when a previously-unknown model returns a usage block we can
// parse â€” this prevents entries from getting stuck in "learning" forever
// just because the user never visited /v1/providers/models.
func PromoteKnown(ctx context.Context, rdb *redis.Client, provider, modelID string) {
	if rdb == nil || modelID == "" || provider == "" {
		return
	}
	key := provider + ":" + modelID
	if err := rdb.SAdd(ctx, radarKnownSetKey, key).Err(); err != nil {
		log.Printf("[ModelRadar] PromoteKnown SADD failed: %v", err)
		return
	}
	entryKey := radarEntryPrefix + modelID
	raw, err := rdb.Get(ctx, entryKey).Bytes()
	if err != nil {
		return // no entry to upgrade
	}
	var e RadarEntry
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	if e.Status == RadarStatusLearning {
		e.Status = RadarStatusKnown
		e.LastSeen = time.Now().UTC()
		updated, _ := json.Marshal(e)
		rdb.Set(ctx, entryKey, updated, radarEntryTTL)
		log.Printf("[ModelRadar] Promoted %s from learning to known", modelID)
	}
}
