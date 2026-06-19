package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"

	"synapse-proxy/internal/db"
)

// DiscoveredToolsHandler — dashboard-facing endpoint to list and
// manage the set of tools the proxy has seen a given virtual key
// invoke, plus the operator-curated denylist.
//
// Routes:
//
//	GET  /v1/keys/tools?vk=sk-opti-...
//	     -> { discovered: ["search_web","read_file",...],
//	          denied:     ["shell_exec"] }
//
//	POST /v1/keys/tools?vk=sk-opti-...
//	     body: { "tool": "shell_exec", "deny": true }
//	     -> { denied: [...] }
//
//	DELETE /v1/keys/tools?vk=sk-opti-...
//	        body: { "tool": "shell_exec" }
//	        -> { denied: [...] }  (un-deny)
//
// The auto-discovery happens on every chat-completion request in
// proxy.go. The proxy SADDs each tool name into
// `synapse:discovered_tools:<vk>` with a 30-day TTL. This handler
// is the dashboard's read/write surface for that set plus the
// operator denylist (`synapse:denied_tools:<vk>`).
//
// Why a denylist and not an allowlist? The previous design used an
// explicit allowlist (AllowedTools + BlockUnknownTools). That
// required operators to enumerate every tool their agents would
// ever call — a maintenance burden, especially when agents
// dynamically add new tools (Hermes does this). The denylist is
// the inverse: everything is allowed by default, the operator only
// acts when they want to BLOCK a specific tool. This matches the
// "auto-discover" UX: tools appear in the list as the agent uses
// them, and the operator clicks to deny.
//
// Authentication: this endpoint is reachable from the dashboard
// backend (Next.js) which sits behind Caddy. We do NOT re-validate
// the operator session here — the dashboard already does. If you
// expose this endpoint publicly, add an API-key check.
func DiscoveredToolsHandler(w http.ResponseWriter, r *http.Request) {
	vk := r.URL.Query().Get("vk")
	if vk == "" {
		http.Error(w, "missing vk query param", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	rdb := db.GetRedis()

	discoverKey := "synapse:discovered_tools:" + vk
	denyKey := "synapse:denied_tools:" + vk

	switch r.Method {
	case http.MethodGet:
		discovered, err := rdb.SMembers(ctx, discoverKey).Result()
		if err != nil {
			log.Printf("[DiscoveredTools] SMembers failed (vk=%s): %v", vk, err)
			http.Error(w, "redis error", http.StatusInternalServerError)
			return
		}
		denied, err := rdb.SMembers(ctx, denyKey).Result()
		if err != nil {
			log.Printf("[DiscoveredTools] SMembers denied failed (vk=%s): %v", vk, err)
			http.Error(w, "redis error", http.StatusInternalServerError)
			return
		}
		sort.Strings(discovered)
		sort.Strings(denied)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"discovered": discovered,
			"denied":     denied,
		})

	case http.MethodPost:
		var body struct {
			Tool string `json:"tool"`
			Deny bool   `json:"deny"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		if body.Tool == "" {
			http.Error(w, "missing tool field", http.StatusBadRequest)
			return
		}
		// If the operator is denying a tool we ALSO add it to
		// the discovered set so it shows up in the UI even if
		// the agent never re-invokes it. This keeps the list
		// self-consistent: "denied X" implies "I have seen X".
		rdb.SAdd(ctx, discoverKey, body.Tool)
		rdb.Expire(ctx, discoverKey, 30*24*60*60*1e9) // 30d
		if body.Deny {
			rdb.SAdd(ctx, denyKey, body.Tool)
		} else {
			rdb.SRem(ctx, denyKey, body.Tool)
		}
		denied, _ := rdb.SMembers(ctx, denyKey).Result()
		sort.Strings(denied)
		log.Printf("[DiscoveredTools] vk=%s tool=%s deny=%v", vk, body.Tool, body.Deny)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"denied": denied,
		})

	case http.MethodDelete:
		var body struct {
			Tool string `json:"tool"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		if body.Tool == "" {
			http.Error(w, "missing tool field", http.StatusBadRequest)
			return
		}
		rdb.SRem(ctx, denyKey, body.Tool)
		denied, _ := rdb.SMembers(ctx, denyKey).Result()
		sort.Strings(denied)
		log.Printf("[DiscoveredTools] vk=%s UNDENIED tool=%s", vk, body.Tool)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"denied": denied,
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}