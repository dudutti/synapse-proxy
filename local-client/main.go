package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"synapse-local/internal/dashboard"
	"synapse-local/internal/db"
	"synapse-local/internal/license"
	"synapse-local/internal/proxy"
)

func main() {
	fmt.Println("====================================================")
	fmt.Println("  Synapse Proxy - Starting Local Client daemon...   ")
	fmt.Println("====================================================")

	// 1. Initialize SQLite Database
	if err := db.InitDB(); err != nil {
		fmt.Printf("Fatal database init error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[DB] SQLite database initialized successfully.")

	// 2. Load License details
	license.LoadLicenseFromDB()
	fmt.Printf("[DRM] Stored License parsed. Current Tier: %s (Quota limit: %d tokens)\n", license.ActiveTier, license.QuotaLimit)

	// 3. Start sync background workers
	license.StartQuotaSyncWorker()
	fmt.Println("[DRM] Cloud Quota sync heartbeat worker started.")

	// 4. Start local proxy router server (Port 8080)
	proxy.StartProxyServer("8080")
	fmt.Println("[Proxy] HTTP Proxy server started on port :8080")

	// Register Dashboard API routes
	http.HandleFunc("/api/keys", proxy.HandleKeysRoute)
	http.HandleFunc("/api/keys/", proxy.HandleKeysRoute)
	http.HandleFunc("/api/user", proxy.HandleUserRoute)
	http.HandleFunc("/api/plans", proxy.HandlePlansRoute)
	http.HandleFunc("/api/auth/session", proxy.HandleSessionRoute)
	http.HandleFunc("/api/analytics", proxy.HandleAnalyticsRoute)
	http.HandleFunc("/api/analytics/stream", proxy.HandleAnalyticsStreamRoute)
	http.HandleFunc("/api/license/activate", proxy.HandleActivateLicenseRoute)

	// 5. Start embedded Dashboard server (Port 4321)
	dashboard.StartDashboardServer("4321", proxy.HandleListModels)
	fmt.Println("[Dashboard] Next.js Dashboard server started on port :4321")

	fmt.Println("====================================================")
	fmt.Println(" -> Local Dashboard: http://localhost:4321")
	fmt.Println(" -> HTTP LLM Proxy : http://localhost:8080")
	fmt.Println(" -> SQLite Store   : synapse_local.db")
	fmt.Println("====================================================")
	fmt.Println("Daemon running. Press Ctrl+C to terminate...")

	// Wait for terminate signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nTerminating Synapse Proxy Local Client. Goodbye!")
}
