package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

var DB *sql.DB

func InitDB() error {
	exePath, err := os.Executable()
	if err != nil {
		exePath = "."
	} else {
		exePath = filepath.Dir(exePath)
	}

	dbPath := filepath.Join(exePath, "synapse_local.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open sqlite DB: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite works best with 1 writer to avoid lock conflicts

	// Create tables if they do not exist
	schema := `
	CREATE TABLE IF NOT EXISTS request_logs (
		id TEXT PRIMARY KEY,
		cache_level TEXT,
		model TEXT,
		provider TEXT,
		tokens_in INTEGER,
		tokens_out INTEGER,
		tokens_in_opt INTEGER,
		tokens_out_opt INTEGER,
		duration_ms INTEGER,
		cost_saved REAL,
		agent_id TEXT,
		agent_label TEXT,
		session_id TEXT,
		api_key_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS cache_entries (
		id TEXT PRIMARY KEY,
		hash TEXT UNIQUE,
		cache_level TEXT,
		prompt_text TEXT,
		response_text TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS license_info (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		license_key TEXT,
		tier TEXT,
		quota_limit INTEGER,
		quota_used INTEGER,
		expires_at DATETIME,
		verified_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS virtual_keys (
		id TEXT PRIMARY KEY,
		virtual_key TEXT UNIQUE,
		provider TEXT,
		real_key TEXT,
		default_model TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err = db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to initialize SQLite schema: %w", err)
	}

	// Insert default license info if table is empty
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM license_info").Scan(&count)
	if err == nil && count == 0 {
		_, _ = db.Exec(`
			INSERT INTO license_info (license_key, tier, quota_limit, quota_used, expires_at, verified_at)
			VALUES ('FREE-TRIAL-KEY', 'FREE', 10000000, 0, ?, ?)
		`, time.Now().AddDate(1, 0, 0), time.Now())
	}

	// Insert default virtual key if table is empty
	var keysCount int
	err = db.QueryRow("SELECT COUNT(*) FROM virtual_keys").Scan(&keysCount)
	if err == nil && keysCount == 0 {
		_, _ = db.Exec(`
			INSERT INTO virtual_keys (id, virtual_key, provider, real_key, default_model)
			VALUES ('local-key-id', 'sk-opti-local-key', 'ollama', 'http://localhost:11434', '')
		`)
	}

	DB = db
	return nil
}
