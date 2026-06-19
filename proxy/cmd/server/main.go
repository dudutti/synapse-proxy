package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"synapse-proxy/internal/db"
	"synapse-proxy/internal/handlers"
	"synapse-proxy/internal/mcp"
	"synapse-proxy/internal/metrics"
	"synapse-proxy/internal/utils"
	"synapse-proxy/internal/workers"
)

func main() {
	// The --mcp flag switches the binary from "HTTP proxy on :8080"
	// (default, the historical behavior) to "MCP server on stdio"
	// (used by Claude Code, Cursor, Continue, and any other MCP
	// client). The two modes are mutually exclusive: you either run
	// the proxy for end-user SDK traffic, or as an MCP tool server
	// for an agent.
	//
	// Examples:
	//   ./synapse-proxy                                # HTTP proxy on :8080
	//   ./synapse-proxy --mcp --tier=free              # MCP server, 7 free tools
	//   ./synapse-proxy --mcp --tier=full --dashboard-url=https://synapse-proxy.com
	//                                                # MCP server, 12 tools, premium forwarded
	mcpFlag := flag.Bool("mcp", false, "run as an MCP server on stdin/stdout (for Claude Code, Cursor, etc.). Mutually exclusive with --mcp-http.")
	mcpHTTP := flag.Bool("mcp-http", false, "run as an MCP server over Streamable HTTP on a dedicated port (so the server can stay up and serve many clients). Mutually exclusive with --mcp.")
	mcpHTTPPort := flag.Int("mcp-http-port", 8081, "port to listen on when --mcp-http is set. Default 8081 (separate from the proxy's :8080).")
	mcpTier := flag.String("mcp-tier", "free", "tier when --mcp or --mcp-http is set: 'free' (4 tools) or 'full' (14 tools, requires --dashboard-url)")
	mcpDashboardURL := flag.String("dashboard-url", "", "SaaS dashboard URL, required when --mcp-tier=full")
	flag.Parse()

	// MCP mode. Stdio transport; we still initialize DB so the
	// free-tier tools can read cache stats and savings.
	if *mcpFlag {
		runMCP(*mcpTier, *mcpDashboardURL)
		return
	}

	// MCP-over-HTTP mode. Long-lived process serving many clients
	// at the same endpoint, so the user doesn't have to spawn a
	// docker run per agent session. The port defaults to 8081 to
	// keep the proxy's :8080 isolated.
	if *mcpHTTP {
		runMCPHTTP(*mcpTier, *mcpDashboardURL, *mcpHTTPPort)
		return
	}

	log.Println("Synapse Proxy initializing...")

	// 1. Initialize Utils (Tiktoken)
	utils.InitTiktoken()

	// 2. Initialize Database Connections
	db.InitRedis()
	db.InitPostgres()

	// 3. Run idempotent schema migrations (called once, not from worker loops)
	workers.RunTelemetryMigrations()

	// 4. Setup Redis Vector Index
	go db.InitRedisIndex()

	// 5. Start Background Workers
	go workers.ConsumeTelemetryWorker()
	go workers.ConsumeBenchmarkWorker()
	go workers.GlobalStatsWorker() // Aggregates global stats into Redis every 5min (public login page)
	go db.InitPricingSyncer()     // Fetches model pricing from ProviderModel every hour (cost-saved accuracy)
	go workers.RetentionWorker()  // Cleans up RequestLog based on FREE/PRO tier policies

	// 5. Mount Routes
	http.HandleFunc("/v1/chat/completions", handlers.ProxyHandler)
	http.HandleFunc("/v1/cache/purge", handlers.CachePurgeHandler)
	http.HandleFunc("/v1/keys/tools", handlers.DiscoveredToolsHandler)
	// /v1/models is the OpenAI-standard list-models endpoint. The dashboard
	// playground and any OpenAI SDK client hits this path to enumerate
	// available models. We also expose /v1/providers/models as the
	// extended Synapse-Proxy-specific listing (with cache metadata).
	http.HandleFunc("/v1/models", handlers.FetchModelsHandler)
	http.HandleFunc("/v1/providers/models", handlers.FetchModelsHandler)

	// 6. Observability — health checks for k8s / load balancers, and
	// /metrics for Prometheus scraping.
	//
	//   /healthz — 200 if the process is alive (no dep checks)
	//   /readyz  — 200 only if Redis is reachable; 503 otherwise
	//   /metrics — Prometheus text exposition (cache hits, tokens
	//              saved, $ saved, panics, upstream latency buckets)
	http.HandleFunc("/healthz", handlers.HealthzHandler)
	http.HandleFunc("/readyz", handlers.ReadyzHandler)
	http.Handle("/metrics", metrics.Handler())

	// 6. Start Server
	fmt.Println("Synapse Proxy Data Plane listening on :8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// runMCP starts the proxy in MCP-server mode on stdin/stdout. The
// caller (Claude Code, Cursor, etc.) launches the binary as a
// subprocess and pipes JSON-RPC 2.0 messages over stdio.
//
// In tier=free we still need Redis + Postgres to back the free-tier
// tools (cache_stats reads Redis counters, savings_summary reads
// RequestLog, sessions read/write the Session table). In tier=full
// the paid tools additionally need --dashboard-url to forward.
//
// We init DB unconditionally because the free tools rely on it.
// The HTTP listener on :8080 is NOT started in MCP mode: the binary
// is single-purpose.
func runMCP(tier, dashboardURL string) {
	// Validate args.
	if tier != string(mcp.TierFree) && tier != string(mcp.TierFull) {
		log.Fatalf("invalid --mcp-tier: %q (must be 'free' or 'full')", tier)
	}
	if tier == string(mcp.TierFull) && dashboardURL == "" {
		log.Fatalf("--mcp-tier=full requires --dashboard-url=https://synapse-proxy.com")
	}

	// Init DB for free-tier tools.
	utils.InitTiktoken()
	db.InitRedis()
	db.InitPostgres()

	// Resolve the virtual key: prefer the flag/env, fall back to a
	// bootstrap key. The user passes their sk-opti-... in the MCP
	// client config; the proxy uses it both for forwarding to the
	// dashboard (paid tools) and to scope Postgres queries to the
	// calling user (free tools).
	vk := os.Getenv("SYNAPSE_PROXY_API_KEY")

	// Build the server with the appropriate tier.
	s := mcp.NewServerWithDefaults(mcp.Tier(tier), vk, dashboardURL)

	// Context that cancels on SIGINT / SIGTERM.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.SetOutput(os.Stderr) // MCP transports reserve stdout
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	fmt.Fprintf(os.Stderr, "[mcp] starting synapse-proxy MCP server tier=%s dashboard=%s\n", tier, dashboardURL)
	if err := mcp.ServeStdio(ctx, s); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "[mcp] server error: %v\n", err)
		os.Exit(1)
	}
}

// runMCPHTTP starts the proxy in MCP-server mode over Streamable
// HTTP. The server is a long-lived process that accepts JSON-RPC
// 2.0 POST requests at /mcp. It is designed to live behind a
// reverse proxy (Caddy, nginx) that terminates TLS and checks an
// Authorization header.
//
// Why a separate flag and not just reusing --mcp? Stdio and HTTP
// are mutually exclusive: stdio expects a subprocess per client
// (one container, one client), HTTP is the long-lived process
// model (one container, many clients). Splitting the flags keeps
// each mode's failure modes obvious.
//
// The HTTP server binds on --mcp-http-port (default 8081), a
// separate port from the proxy's :8080. This is intentional:
//   - The /v1/* proxy API and the /mcp endpoint have different
//     threat models (one is for SDK traffic, the other for
//     agents). Putting them on the same port would conflate
//     the authn/authz.
//   - Caddy routes them under different hostnames or paths:
//        synapse-proxy.com           -> proxy :8080 (v1/*)
//        mcp.synapse-proxy.com       -> proxy :8081 (mcp)
//     or with a path:
//        synapse-proxy.com/mcp       -> proxy :8081 (mcp)
//        synapse-proxy.com/v1/*      -> proxy :8080
func runMCPHTTP(tier, dashboardURL string, port int) {
	// Validate args.
	if tier != string(mcp.TierFree) && tier != string(mcp.TierFull) {
		log.Fatalf("invalid --mcp-tier: %q (must be 'free' or 'full')", tier)
	}
	if tier == string(mcp.TierFull) && dashboardURL == "" {
		log.Fatalf("--mcp-tier=full requires --dashboard-url=https://synapse-proxy.com")
	}

	// Init DB for free-tier tools. The HTTP transport doesn't
	// need anything else.
	utils.InitTiktoken()
	db.InitRedis()
	db.InitPostgres()

	vk := os.Getenv("SYNAPSE_PROXY_API_KEY")
	s := mcp.NewServerWithDefaults(mcp.Tier(tier), vk, dashboardURL)

	// Mount the handlers. We use the stdlib mux because we
	// only have two routes.
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.HandleHTTP)
	mux.HandleFunc("/mcp/health", s.HealthHTTP)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: SSE responses can be long if a
		// tool is slow. Read timeout is capped to 30s so a
		// stuck connection doesn't pin a goroutine forever.
		ReadTimeout: 30 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Printf("[mcp-http] starting synapse-proxy MCP server tier=%s dashboard=%s on :%d", tier, dashboardURL, port)
	log.Printf("[mcp-http] POST /mcp for JSON-RPC 2.0 requests (Content-Type: application/json)")
	log.Printf("[mcp-http] GET /mcp/health for liveness probe")

	go func() {
		<-ctx.Done()
		log.Printf("[mcp-http] shutdown signal received, draining...")
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("[mcp-http] shutdown error: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[mcp-http] server failed: %v", err)
	}
}
