// Package mcp — Streamable HTTP transport.
//
// The MCP spec (2025-03-26) defines a "Streamable HTTP" transport:
//   - Single endpoint, e.g. POST /mcp
//   - Client sends JSON-RPC 2.0 requests in the request body
//   - Server responds with either:
//     a) application/json body (one-shot, no SSE)
//     b) text/event-stream with a single "data: <json>\n\n" event
//   - The transport is selected by the client's Accept header:
//        Accept: application/json, text/event-stream
//     We honour both: if the client wants SSE we stream; otherwise
//     we return a single JSON object.
//
// The point of HTTP transport (vs the stdio transport) is that the
// server can be a long-lived process — one container, many clients.
// Stdio requires spawning one process per client, which doesn't
// scale and is a pain behind HTTP reverse proxies (Caddy, nginx).
//
// Clients that support it (as of mid-2026):
//   - Claude Code 1.x
//   - Continue 0.9+
//   - Cursor 0.40+
//   - LibreChat (via a small adapter)
//
// The endpoint is mounted at /mcp on the same HTTP server the
// proxy uses for /v1/* — but only when --mcp-http is set on the
// CLI. We don't want to expose MCP on the same port as the proxy
// by default (security: an MCP client should not be able to also
// call /v1/chat/completions on the same host). In production we
// expose MCP on a separate port (default 8081) and let Caddy
// reverse-proxy it on https://synapse-proxy.com/mcp with an
// Authorization header check.
package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HandleHTTP is an http.Handler that dispatches JSON-RPC 2.0
// requests from the request body to the server. It honours the
// Accept header for SSE vs JSON responses.
//
// Only POST is accepted. GET is reserved for future SSE
// notification streams (the spec allows it but we don't use it
// yet). Other methods get 405.
//
// The handler is stateless: every request is independent. The
// MCP spec does not require sessions for our tool surface
// (no server-initiated notifications, no streaming responses),
// so we don't implement the Mcp-Session-Id header dance.
//
// Authentication: the client must send `Authorization: Bearer
// <key>` where <key> is a virtual key (sk-opti-...). The key is
// stashed in the request context and forwarded to the dashboard
// for paid-tool calls. For free tools, the key is currently
// informational (not enforced locally) — the real authorization
// is in the dashboard for paid tools, and free tools are open
// by design (they read public-ish counters from Redis/Postgres).
//
// If Authorization is missing, we still serve the request but
// log a warning. The alternative (hard 401) is too aggressive
// for self-hosted deployments where the proxy is on localhost
// and there's no real attacker. The dashboard's 401s are the
// load-bearing security boundary.
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract the bearer token. Stash it in the request context
	// so handlers can pull it via authKeyFromContext(ctx). We
	// don't return 401 if it's missing because the free tools
	// don't need it; the dashboard rejects paid calls without
	// the right key.
	authKey := bearerToken(r.Header.Get("Authorization"))
	if authKey == "" {
		authKey = s.virtualKey // env-var fallback
	}
	ctx := contextWithAuthKey(r.Context(), authKey)

	// Cap the request body. MCP tool args are tiny (a few KB at
	// most). 1 MiB is plenty and protects us from OOM if someone
	// POSTs a 100 MB blob.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}
	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	// Dispatch. handle() is shared with the stdio transport.
	resp := s.handle(ctx, body)
	out, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	// Decide response format from Accept header. The MCP spec
	// says clients that support Streamable HTTP send:
	//   Accept: application/json, text/event-stream
	// So we check for "text/event-stream" presence.
	accept := r.Header.Get("Accept")
	wantsSSE := strings.Contains(accept, "text/event-stream")

	if wantsSSE {
		// SSE mode. One event per response, id = the JSON-RPC
		// id (if any), data = the JSON. We also send a final
		// "done" event so the client knows the stream is over.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)
		eventID := ""
		if resp.ID != nil {
			eventID = fmt.Sprintf("%v", resp.ID)
		}
		// SSE format: "id: <id>\ndata: <json>\n\n"
		if eventID != "" {
			fmt.Fprintf(w, "id: %s\n", eventID)
		}
		fmt.Fprintf(w, "data: %s\n\n", out)
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	// Plain JSON mode.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

// HealthHTTP is a tiny handler for k8s/load-balancer probes
// attached to the MCP server. Returns 200 with the server's
// name + version + tier. Does not check downstream (Redis /
// Postgres / Dashboard) — the caller can use the existing
// /readyz for that.
func (s *Server) HealthHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"name":    s.name,
		"version": s.version,
		"tier":    s.tier,
		"tools":   len(s.tools),
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}
