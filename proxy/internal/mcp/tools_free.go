// Package mcp — free-tier tools.
//
// All seven tools in this file are powered by data the proxy already
// computes locally: Redis (cache counters), Postgres (RequestLog,
// Session, ProviderModel), and the upstream provider list. They
// work in self-hosted mode without any dashboard subscription.
// Premium tools (in tools_paid.go) forward to the SaaS dashboard
// and are gated by the TierFull check in server.go.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"synapse-proxy/internal/db"
	"synapse-proxy/internal/handlers"
	"synapse-proxy/optiagent"
)

// ----------------------------------------------------------------------------
// Registration
// ----------------------------------------------------------------------------

// registerFreeTools wires the tools that are always available
// regardless of the tier. The caller (main.go) invokes this once
// at server construction.
func (s *Server) registerFreeTools() {
	s.Register(Tool{
		Name: "synapse_chat_completions",
		Description: "Send a chat completion through the Synapse Proxy. " +
			"Applies L1 (exact hash) -> L2 (semantic ONNX) -> L3 " +
			"(cache-preserving compression) before forwarding upstream. " +
			"Identical requests hit L1 and cost zero upstream tokens. " +
			"Returns the OpenAI-shaped response plus a synapse_enrichment " +
			"object with cache_level, tokens_saved, and cost_saved_usd.",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"messages": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"role":    map[string]any{"type": "string"},
							"content": map[string]any{"type": "string"},
							"name":    map[string]any{"type": "string"},
						},
						"required": []string{"role", "content"},
					},
				},
				"model":       map[string]any{"type": "string", "default": "gpt-4o-mini"},
				"temperature": map[string]any{"type": "number"},
				"max_tokens":  map[string]any{"type": "integer"},
				"top_p":       map[string]any{"type": "number"},
				"stop":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			[]string{"messages", "model"},
		),
	}, s.toolChatCompletions, false)

	s.Register(Tool{
		Name:        "synapse_list_models",
		Description: "List the model names this proxy knows how to serve. Read from the ProviderModel table; use one of these strings as the 'model' field in synapse_chat_completions.",
		InputSchema: MustToolInputSchema(map[string]any{}, []string{}),
	}, s.toolListModels, false)

	s.Register(Tool{
		Name: "synapse_cache_stats",
		Description: "L1/L2/L3 cache hit statistics. Returns total requests, " +
			"tokens saved, $ saved, and per-level breakdown (L1 exact, " +
			"L2 semantic, L3 cache-preserving, NONE miss). The 'days' " +
			"argument is reserved for a future per-window release; today " +
			"we return all-time counters.",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"days": map[string]any{"type": "integer", "default": 7, "minimum": 1, "maximum": 90},
			},
			[]string{},
		),
	}, s.toolCacheStats, false)

	s.Register(Tool{
		Name: "synapse_savings_summary",
		Description: "Total $ saved over the last N days, broken down by " +
			"token class (InputFresh, CacheRead) and provider. Aggregates " +
			"the RequestLog table in Postgres.",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"days": map[string]any{"type": "integer", "default": 30, "minimum": 1, "maximum": 365},
			},
			[]string{},
		),
	}, s.toolSavingsSummary, false)

	s.Register(Tool{
		Name:        "synapse_inspect_ccr_store",
		Description: "List all keys and payload sizes currently archived in the L3/CCR compression store.",
		InputSchema: MustToolInputSchema(map[string]any{}, []string{}),
	}, s.toolInspectCCRStore, false)

	s.Register(Tool{
		Name:        "synapse_get_ccr_value",
		Description: "Retrieve the original uncompressed payload corresponding to a specific CCR hash key.",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"key": map[string]any{"type": "string", "description": "The CCR hash key to lookup."},
			},
			[]string{"key"},
		),
	}, s.toolGetCCRValue, false)

	s.Register(Tool{
		Name:        "synapse_optimize_prompt",
		Description: "Simulate prompt optimization and compression. Runs all registered Synapse hooks (SmartCrusher, DiffCompressor, etc.) and returns the optimized prompt.",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"messages": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"role":    map[string]any{"type": "string"},
							"content": map[string]any{"type": "string"},
						},
						"required": []string{"role", "content"},
					},
				},
				"model": map[string]any{"type": "string", "default": "gpt-4o-mini"},
			},
			[]string{"messages", "model"},
		),
	}, s.toolOptimizePrompt, false)
}

// ----------------------------------------------------------------------------
// Tool implementations
// ----------------------------------------------------------------------------

var proxyHandlerFunc = handlers.ProxyHandler

// toolChatCompletions forwards a chat completion to the proxy's handler
// directly in-process using httptest.NewRecorder(). This avoids any network dependency
// and works even if the proxy is running solely as an MCP server without a TCP listener.
func (s *Server) toolChatCompletions(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Name    string `json:"name"`
		} `json:"messages"`
		Model       string   `json:"model"`
		Temperature *float64 `json:"temperature"`
		MaxTokens   *int     `json:"max_tokens"`
		TopP        *float64 `json:"top_p"`
		Stop        []string `json:"stop"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, NewToolError(CodeInvalidParams, "invalid arguments", err.Error())
	}
	if len(args.Messages) == 0 {
		return nil, NewToolError(CodeInvalidParams, "messages cannot be empty", nil)
	}
	if args.Model == "" {
		args.Model = "gpt-4o-mini"
	}

	payload := map[string]interface{}{"model": args.Model, "messages": args.Messages}
	if args.Temperature != nil {
		payload["temperature"] = *args.Temperature
	}
	if args.MaxTokens != nil {
		payload["max_tokens"] = *args.MaxTokens
	}
	if args.TopP != nil {
		payload["top_p"] = *args.TopP
	}
	if len(args.Stop) > 0 {
		payload["stop"] = args.Stop
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", "/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.virtualKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.virtualKey)
	}

	rec := httptest.NewRecorder()
	proxyHandlerFunc(rec, req)

	respBody := rec.Body.Bytes()
	if rec.Code >= 400 {
		var errBody map[string]any
		_ = json.Unmarshal(respBody, &errBody)
		return nil, NewToolError(CodeUpstreamError,
			fmt.Sprintf("upstream returned %d", rec.Code), errBody)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	enrichment := map[string]any{}
	if cl := rec.Header().Get("X-SynapseProxy-Cache"); cl != "" {
		enrichment["cache_level"] = cl
	}
	if tk := rec.Header().Get("X-SynapseProxy-Tokens-Saved"); tk != "" {
		enrichment["tokens_saved"] = tk
	}
	if cs := rec.Header().Get("X-SynapseProxy-Cost-Saved"); cs != "" {
		enrichment["cost_saved_usd"] = cs
	}
	if len(enrichment) > 0 {
		result["synapse_enrichment"] = enrichment
	}
	return result, nil
}

// toolListModels returns the model names registered in Postgres.
// Same data the /v1/models HTTP endpoint serves. We list every
// row in the ProviderModel table (no status filter) because the
// schema has provider / modelName / cost columns but no "status"
// column; the cost columns being non-zero is the implicit
// "available" signal.
func (s *Server) toolListModels(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	rows, err := db.GetDB().QueryContext(ctx, `
		SELECT "modelName", provider, "costPromptPer1M", "costCompletionPer1M"
		FROM "ProviderModel"
		ORDER BY provider ASC, "modelName" ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query models: %w", err)
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name, provider string
		var costPrompt, costCompletion float64
		if err := rows.Scan(&name, &provider, &costPrompt, &costCompletion); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		out = append(out, map[string]any{
			"name":                name,
			"provider":            provider,
			"cost_prompt_per_1m":   costPrompt,
			"cost_completion_per_1m": costCompletion,
		})
	}
	return map[string]any{"models": out, "count": len(out)}, nil
}

// toolCacheStats reads the cache hit statistics from the proxy's
// `synapse:global_stats` Redis key. This key is maintained by the
// proxy's GlobalStatsWorker (every 5 minutes in production) and
// contains a JSON object with the per-level distribution,
// per-provider breakdown, totals, and the last update timestamp.
//
// The "days" argument is accepted for forward compatibility; today
// the global stats are all-time (the worker hasn't been running long
// enough to have meaningful per-window data).
func (s *Server) toolCacheStats(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Days int `json:"days"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Days <= 0 {
		args.Days = 7
	}

	raw2, err := db.GetRedis().Get(ctx, "synapse:global_stats").Bytes()
	if err != nil {
		return nil, NewToolError(CodeUpstreamError,
			"global_stats not available yet — the GlobalStatsWorker hasn't run for the first time. "+
				"Wait 5 minutes after first boot, or trigger a sync via the dashboard.", nil)
	}
	var stats map[string]any
	if err := json.Unmarshal(raw2, &stats); err != nil {
		return nil, fmt.Errorf("parse global_stats: %w", err)
	}

	// Distribution is a {BYPASS, L0, L1, L2, L3, MISS, NONE} map.
	dist, _ := stats["cacheDistribution"].(map[string]any)
	byLevel := []map[string]any{}
	var totalHits, totalReq int64
	for _, lvl := range []string{"L1", "L2", "L3"} {
		hits, _ := dist[lvl].(float64)
		byLevel = append(byLevel, map[string]any{
			"cache_level": lvl,
			"hits":        int64(hits),
		})
		totalHits += int64(hits)
	}
	for _, lvl := range []string{"NONE", "MISS", "BYPASS", "L0"} {
		v, _ := dist[lvl].(float64)
		totalReq += int64(v)
	}
	totalReq += totalHits

	return map[string]any{
		"window_days":         args.Days,
		"total_requests":      totalReq,
		"total_cache_hits":     totalHits,
		"overall_hit_rate":     safeRatio(totalHits, totalReq),
		"total_tokens_saved":   int64(stats["tokensOptimized"].(float64)),
		"total_cost_saved_usd": stats["totalCostSaved"],
		"by_level":             byLevel,
		"full_distribution":    dist,
		"by_provider":          stats["cacheByProvider"],
		"last_updated":         stats["lastUpdated"],
	}, nil
}

// toolSavingsSummary aggregates $ saved from the RequestLog table
// in Postgres, grouped by token class and provider. Same query
// shape as the dashboard's /api/analytics/savings route.
//
// The RequestLog schema has explicit per-class savings columns
// (savingsInputFresh, savingsCacheRead, savingsCacheCreation,
// savingsOutput) and a single `cacheLevel` field per row.
func (s *Server) toolSavingsSummary(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Days int `json:"days"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Days <= 0 {
		args.Days = 30
	}
	if args.Days > 365 {
		args.Days = 365
	}

	since := time.Now().AddDate(0, 0, -args.Days)
	byClass := map[string]float64{
		"InputFresh":     0,
		"CacheRead":      0,
		"CacheCreation":  0,
		"Output":         0,
	}
	byProvider := map[string]float64{}
	var totalSaved float64
	var totalInputTokens, totalOutputTokens int64

	rows, err := db.GetDB().QueryContext(ctx, `
		SELECT
			provider,
			"savingsInputFresh",
			"savingsCacheRead",
			"savingsCacheCreation",
			"savingsOutput",
			"promptTokensOrig",
			"completionTokensOrig"
		FROM "RequestLog"
		WHERE "createdAt" >= $1
	`, since)
	if err != nil {
		return nil, fmt.Errorf("query savings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var provider string
		var sIF, sCR, sCC, sO float64
		var pTO, cTO int64
		if err := rows.Scan(&provider, &sIF, &sCR, &sCC, &sO, &pTO, &cTO); err != nil {
			return nil, fmt.Errorf("scan savings row: %w", err)
		}
		rowSaved := sIF + sCR + sCC + sO
		byClass["InputFresh"] += sIF
		byClass["CacheRead"] += sCR
		byClass["CacheCreation"] += sCC
		byClass["Output"] += sO
		byProvider[provider] += rowSaved
		totalSaved += rowSaved
		totalInputTokens += pTO
		totalOutputTokens += cTO
	}

	return map[string]any{
		"window_days":          args.Days,
		"total_cost_saved_usd": totalSaved,
		"total_input_tokens":   totalInputTokens,
		"total_output_tokens":  totalOutputTokens,
		"by_class_usd":         byClass,
		"by_provider_usd":      byProvider,
		"since":                since.Format(time.RFC3339),
	}, nil
}

// ----------------------------------------------------------------------------
// Session tools (tier=full only — forwarded to the dashboard)
// ----------------------------------------------------------------------------
//
// Note: the original implementation had start/stop/list_session as
// "free" tools that tried to read/write a Session table locally.
// That table doesn't exist in the prod schema (sessions are a
// dashboard feature). The free tools are now reduced to 4
// (chat_completions, list_models, cache_stats, savings_summary),
// and session tools are in tools_paid.go where they forward to
// the SaaS dashboard's /api/sessions/record and
// /api/analytics/sessions endpoints.
//
// We keep no local session implementation here. If/when the
// dashboard exposes a /v1/internal/sessions/* HTTP namespace we
// can move these tools back to the free tier and proxy them via
// loopback (the same pattern used for chat_completions).
// ----------------------------------------------------------------------------

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func safeRatio(num, denom int64) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) / float64(denom)
}

func validGroupBy(g string) bool {
	switch strings.ToLower(g) {
	case "agent", "model", "session":
		return true
	}
	return false
}

// keyLooksValid returns true if the configured virtual key has the
// shape of any supported Synapse Proxy or upstream provider key.
func (s *Server) keyLooksValid() bool {
	k := strings.TrimSpace(s.virtualKey)
	if k == "" {
		return false
	}
	return strings.HasPrefix(k, "sk-opti-") ||
		strings.HasPrefix(k, "sk-ant-") ||
		strings.HasPrefix(k, "sk-")
}

// toolInspectCCRStore lists all keys archived in the global CCR CompressionStore.
func (s *Server) toolInspectCCRStore(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	store := optiagent.GetGlobalCompressionStore()
	if store == nil {
		return nil, NewToolError(CodeInternalError, "compression store not initialized", nil)
	}

	entries := store.Entries()
	var out []map[string]any
	for _, entry := range entries {
		out = append(out, map[string]any{
			"key":        entry.Key,
			"size_bytes": len(entry.Value),
		})
	}
	return map[string]any{"entries": out, "count": len(out)}, nil
}

// toolGetCCRValue retrieves the original payload corresponding to a CCR key.
func (s *Server) toolGetCCRValue(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, NewToolError(CodeInvalidParams, "invalid arguments", err.Error())
	}
	if args.Key == "" {
		return nil, NewToolError(CodeInvalidParams, "key is required", nil)
	}

	store := optiagent.GetGlobalCompressionStore()
	if store == nil {
		return nil, NewToolError(CodeInternalError, "compression store not initialized", nil)
	}

	val, err := store.Lookup(args.Key)
	if err != nil {
		return nil, fmt.Errorf("lookup ccr key: %w", err)
	}
	if val == nil {
		return nil, NewToolError(CodeInvalidParams, "key not found in compression store", nil)
	}

	return map[string]any{"value": string(val)}, nil
}

// toolOptimizePrompt runs all BeforeRequest hooks on a prompt payload without calling the LLM provider.
func (s *Server) toolOptimizePrompt(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Messages []optiagent.Message `json:"messages"`
		Model    string              `json:"model"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, NewToolError(CodeInvalidParams, "invalid arguments", err.Error())
	}
	if len(args.Messages) == 0 {
		return nil, NewToolError(CodeInvalidParams, "messages cannot be empty", nil)
	}
	if args.Model == "" {
		args.Model = "gpt-4o-mini"
	}

	payload := map[string]interface{}{"model": args.Model, "messages": args.Messages}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	hctx := &optiagent.HookContext{
		VK:         s.virtualKey,
		RawPayload: payloadBytes,
		Features:   make(map[string]interface{}),
	}

	optimizedPayload, _ := optiagent.RunBeforeHooks(ctx, hctx)

	var optimizedBody struct {
		Messages []optiagent.Message `json:"messages"`
	}
	if err := json.Unmarshal(optimizedPayload, &optimizedBody); err != nil {
		return nil, fmt.Errorf("unmarshal optimized payload: %w", err)
	}

	warnings := []string{}
	for k, v := range hctx.Features {
		if strings.HasSuffix(k, "_warning") {
			if wStr, ok := v.(string); ok {
				warnings = append(warnings, wStr)
			}
		}
	}

	return map[string]any{
		"optimized_messages": optimizedBody.Messages,
		"warnings":           warnings,
	}, nil
}
