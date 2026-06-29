package license

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"synapse-local/internal/db"
)

var (
	licenseMu sync.RWMutex
	ActiveTier string = "FREE"
	QuotaLimit int64  = 10000000 // 10M
	QuotaUsed  int64  = 0
	IsActive   bool   = true
)

type HeartbeatPayload struct {
	LicenseKey string `json:"licenseKey"`
	QuotaUsed  int64  `json:"quotaUsed"`
}

type HeartbeatResponse struct {
	Valid      bool   `json:"valid"`
	Tier       string `json:"tier"`
	QuotaLimit int64  `json:"quotaLimit"`
	QuotaUsed  int64  `json:"quotaUsed"`
}

// LoadLicenseFromDB loads the stored license configuration on startup
func LoadLicenseFromDB() {
	licenseMu.Lock()
	defer licenseMu.Unlock()

	var key string
	var limit, used int64
	err := db.DB.QueryRow("SELECT license_key, tier, quota_limit, quota_used FROM license_info ORDER BY id DESC LIMIT 1").Scan(&key, &ActiveTier, &limit, &used)
	if err == nil {
		QuotaLimit = limit
		QuotaUsed = used
		resolveTierAndLimits(key)
	}
}

// SaveLicenseToDB saves the current active license details
func SaveLicenseToDB(key, tier string, limit, used int64) {
	_, _ = db.DB.Exec(`
		INSERT INTO license_info (license_key, tier, quota_limit, quota_used, expires_at, verified_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, key, tier, limit, used, time.Now().AddDate(1, 0, 0), time.Now())
}

// IncrementQuota records token savings locally
func IncrementQuota(savedTokens int64) {
	licenseMu.Lock()
	defer licenseMu.Unlock()

	QuotaUsed += savedTokens
	_, _ = db.DB.Exec("UPDATE license_info SET quota_used = quota_used + ?", savedTokens)
}

// CheckQuota returns true if consumption is within limits
func CheckQuota() bool {
	licenseMu.RLock()
	defer licenseMu.RUnlock()

	if !IsActive {
		return false
	}
	if ActiveTier == "ENTERPRISE" {
		return true
	}
	return QuotaUsed < QuotaLimit
}

// ValidateLicense contacts the licensing server or performs local fallback
func ValidateLicense(key string) (bool, error) {
	licenseMu.Lock()
	defer licenseMu.Unlock()

	// Local verification based on prefixes
	valid := resolveTierAndLimits(key)
	if !valid {
		return false, fmt.Errorf("invalid license key signature or format")
	}

	SaveLicenseToDB(key, ActiveTier, QuotaLimit, QuotaUsed)
	return true, nil
}

// Parse tier from license prefixes
func resolveTierAndLimits(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}

	if strings.HasPrefix(key, "FREE-") || key == "FREE-TRIAL-KEY" {
		ActiveTier = "FREE"
		QuotaLimit = 10000000 // 10M
		IsActive = true
		return true
	} else if strings.HasPrefix(key, "PRO-") {
		ActiveTier = "PRO"
		QuotaLimit = 50000000 // 50M
		IsActive = true
		return true
	} else if strings.HasPrefix(key, "ENT-") {
		ActiveTier = "ENTERPRISE"
		QuotaLimit = -1 // Unlimited
		IsActive = true
		return true
	}

	return false
}

// StartQuotaSyncWorker runs the background heartbeat synchronization
func StartQuotaSyncWorker() {
	ticker := time.NewTicker(10 * time.Minute)
	go func() {
		for range ticker.C {
			syncQuotaWithCloud()
		}
	}()
}

func syncQuotaWithCloud() {
	licenseMu.RLock()
	key := ""
	used := int64(0)
	_ = db.DB.QueryRow("SELECT license_key, quota_used FROM license_info ORDER BY id DESC LIMIT 1").Scan(&key, &used)
	licenseMu.RUnlock()

	if key == "" || key == "FREE-TRIAL-KEY" {
		return // Skip cloud synchronization for offline trial key
	}

	payload := HeartbeatPayload{
		LicenseKey: key,
		QuotaUsed:  used,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("POST", "https://synapse-proxy.com/api/license/heartbeat", bytes.NewBuffer(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// Tolerant to offline: allow client to continue locally
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var res HeartbeatResponse
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil {
			licenseMu.Lock()
			IsActive = res.Valid
			ActiveTier = res.Tier
			QuotaLimit = res.QuotaLimit
			// Sync used count if cloud tells us so
			if res.QuotaUsed > QuotaUsed {
				QuotaUsed = res.QuotaUsed
				_, _ = db.DB.Exec("UPDATE license_info SET quota_used = ?", QuotaUsed)
			}
			licenseMu.Unlock()
		}
	}
}
