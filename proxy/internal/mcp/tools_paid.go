// Package mcp — paid-tier tools.
//
// These seven tools are gated by TierFull. In TierFree they are
// NOT advertised on `tools/list`, and any direct `tools/call` returns
// the standard JSON-RPC code CodeRequiresPaidPlan. In TierFull they
// forward to the SaaS dashboard, which performs the actual work and
// returns the result.
//
// The 7 paid tools:
//   1. synapse_run_benchmark        — A/B test with LLM-as-judge
//   2. synapse_list_virtual_keys   — manage a user's API keys
//   3. synapse_create_virtual_key  — mint a new key with a budget
//   4. synapse_get_quotas           — current usage vs monthly budget
//   5. synapse_list_alerts          — read triggered alert rules
//   6. synapse_set_alert_rule       — create/update alert thresholds
//   7. synapse_export_logs          — CSV/JSONL export for compliance
//
// We do NOT implement these tools locally. The proxy is a gateway;
// the dashboard owns the multi-user, billing, alerting, and judge-LLM
// logic. This keeps the open-source proxy small, auditable, and
// aligned with the open-core / SaaS model.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// registerPaidTools wires the seven tools that require a paid plan.
// In TierFree they are not advertised; in TierFull they forward to
// the SaaS dashboard via HTTP.
func (s *Server) registerPaidTools() {
	s.Register(Tool{
		Name: "synapse_run_benchmark",
		Description: "Run an A/B benchmark between two models with an LLM " +
			"as judge. Forwards to the SaaS dashboard which orchestrates " +
			"the control + optimized + judge requests. Returns the winner, " +
			"per-model scores, cache hit rate, and $ cost. Requires a paid " +
			"plan (the judge LLM call is not free).",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"models":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 2, "maxItems": 2},
				"prompt":      map[string]any{"type": "string"},
				"judge_model": map[string]any{"type": "string", "default": "gpt-4o-mini"},
				"runs":        map[string]any{"type": "integer", "default": 5, "minimum": 1, "maximum": 20},
			},
			[]string{"models", "prompt"},
		),
	}, s.toolRunBenchmark, true)

	s.Register(Tool{
		Name:        "synapse_list_virtual_keys",
		Description: "List the virtual keys owned by the calling user. Returns id, label, provider, model, monthlyBudget, currentUsage, status, createdAt.",
		InputSchema: MustToolInputSchema(map[string]any{}, []string{}),
	}, s.toolListVirtualKeys, true)

	s.Register(Tool{
		Name:        "synapse_create_virtual_key",
		Description: "Mint a new virtual key with a monthly budget. Returns the plaintext sk-opti-... key (shown only once).",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"label":          map[string]any{"type": "string"},
				"provider":       map[string]any{"type": "string", "enum": []string{"minimax", "openai", "anthropic"}},
				"model":          map[string]any{"type": "string"},
				"monthly_budget": map[string]any{"type": "number", "minimum": 0},
			},
			[]string{"label", "monthly_budget"},
		),
	}, s.toolCreateVirtualKey, true)

	s.Register(Tool{
		Name:        "synapse_get_quotas",
		Description: "Get current usage vs the monthly budget for each of the calling user's virtual keys. Useful for budget alerts and 'how much have I spent this month?' queries.",
		InputSchema: MustToolInputSchema(map[string]any{}, []string{}),
	}, s.toolGetQuotas, true)

	s.Register(Tool{
		Name:        "synapse_list_alerts",
		Description: "List alert rules (and their triggered history) for the calling user. Alerts are configured separately (synapse_set_alert_rule).",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"triggered_only": map[string]any{"type": "boolean", "default": false},
			},
			[]string{},
		),
	}, s.toolListAlerts, true)

	s.Register(Tool{
		Name:        "synapse_set_alert_rule",
		Description: "Create or update an alert rule. Triggers when the threshold is exceeded (e.g. 'alert me when monthly spend > $40').",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"rule_id":   map[string]any{"type": "string", "description": "Existing rule id to update; omit to create a new one."},
				"name":      map[string]any{"type": "string"},
				"metric":    map[string]any{"type": "string", "enum": []string{"monthly_spend_usd", "cache_hit_rate", "upstream_error_rate"}},
				"threshold": map[string]any{"type": "number"},
				"channel":   map[string]any{"type": "string", "enum": []string{"email", "slack", "webhook"}},
				"target":    map[string]any{"type": "string", "description": "Email address, Slack webhook URL, or generic webhook URL."},
				"enabled":   map[string]any{"type": "boolean", "default": true},
			},
			[]string{"name", "metric", "threshold", "channel", "target"},
		),
	}, s.toolSetAlertRule, true)

	s.Register(Tool{
		Name:        "synapse_export_logs",
		Description: "Export the RequestLog rows for the calling virtual key as CSV or JSONL. For compliance audits and ad-hoc analysis. Streams the response.",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"format": map[string]any{"type": "string", "enum": []string{"csv", "jsonl"}, "default": "jsonl"},
				"since":  map[string]any{"type": "string", "description": "ISO 8601 date, e.g. 2026-06-01."},
				"until":  map[string]any{"type": "string", "description": "ISO 8601 date, e.g. 2026-06-18."},
			},
			[]string{},
		),
	}, s.toolExportLogs, true)

	// Session tools (start/stop/list) — forwarded to the dashboard's
	// session recording endpoints. They live in registerPaidTools
	// because the session data lives in the dashboard's tables, not
	// in the proxy's local Postgres. The free tier simply does not
	// expose them at all (no stub, no error — they're hidden).
	s.Register(Tool{
		Name:        "synapse_start_session",
		Description: "Start a recording session that captures a slice of live traffic. Returns a session_id; pass it to synapse_stop_session when done. Forwards to the dashboard.",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"label":    map[string]any{"type": "string"},
				"group_by": map[string]any{"type": "string", "enum": interface{}([]string{"agent", "model", "session"}), "default": "agent"},
			},
			[]string{},
		),
	}, s.toolStartSession, true)

	s.Register(Tool{
		Name:        "synapse_stop_session",
		Description: "Stop a running session. The dashboard deletes the Redis keys associated with the session so subsequent traffic is no longer tagged. Forwards to the dashboard.",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"session_id": map[string]any{"type": "string"},
			},
			[]string{"session_id"},
		),
	}, s.toolStopSession, true)

	s.Register(Tool{
		Name:        "synapse_list_sessions",
		Description: "List the most recent recording sessions for the calling user. Forwards to the dashboard's analytics endpoint.",
		InputSchema: MustToolInputSchema(
			map[string]any{
				"limit": map[string]any{"type": "integer", "default": 20, "minimum": 1, "maximum": 100},
			},
			[]string{},
		),
	}, s.toolListSessions, true)
}

// ----------------------------------------------------------------------------
// Forwarding helpers
// ----------------------------------------------------------------------------

// forwardPost posts a JSON body to the SaaS dashboard and returns
// the parsed response. It is the single point of contact with the
// dashboard for all paid-tier tools. The dashboard validates the
// virtual key's `benchmark` / `billing` / `alerts` permission
// independently — we don't trust anything client-side.
func (s *Server) forwardPost(ctx context.Context, path string, body interface{}) (interface{}, error) {
	if s.dashboardURL == "" {
		return nil, NewToolError(CodeUpstreamError,
			"tier=full requires --dashboard-url=https://synapse-proxy.com", nil)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	url := strings.TrimRight(s.dashboardURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Per-request virtual key takes precedence over the server-
	// wide one. The HTTP transport extracts the bearer token
	// from the Authorization header and stashes it in the
	// context; the stdio transport sets it from the env var.
	vk := authKeyFromContext(ctx)
	if vk == "" {
		vk = s.virtualKey
	}
	if vk != "" {
		req.Header.Set("Authorization", "Bearer "+vk)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call dashboard: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, NewToolError(CodeRequiresPaidPlan,
			"the dashboard rejected the virtual key. Make sure it has the "+
				"required permission (benchmark / billing / alerts) for this "+
				"tool. See https://synapse-proxy.com/pricing for plan details.",
			map[string]any{"status": resp.StatusCode, "body": json.RawMessage(respBody)})
	}
	if resp.StatusCode >= 400 {
		return nil, NewToolError(CodeUpstreamError,
			fmt.Sprintf("dashboard returned %d", resp.StatusCode),
			json.RawMessage(respBody))
	}
	var result interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Body wasn't JSON; return it as a string payload.
		return string(respBody), nil
	}
	return result, nil
}

func (s *Server) forwardGet(ctx context.Context, path string, params map[string]string) (interface{}, error) {
	if s.dashboardURL == "" {
		return nil, NewToolError(CodeUpstreamError,
			"tier=full requires --dashboard-url=https://synapse-proxy.com", nil)
	}
	url := strings.TrimRight(s.dashboardURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	vk := authKeyFromContext(ctx)
	if vk == "" {
		vk = s.virtualKey
	}
	if vk != "" {
		req.Header.Set("Authorization", "Bearer "+vk)
	}
	q := req.URL.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call dashboard: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, NewToolError(CodeRequiresPaidPlan,
			"the dashboard rejected the virtual key. Make sure it has the "+
				"required permission for this tool.",
			map[string]any{"status": resp.StatusCode, "body": json.RawMessage(respBody)})
	}
	if resp.StatusCode >= 400 {
		return nil, NewToolError(CodeUpstreamError,
			fmt.Sprintf("dashboard returned %d", resp.StatusCode),
			json.RawMessage(respBody))
	}
	var result interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	return result, nil
}

// ----------------------------------------------------------------------------
// Tool implementations (all forward to the dashboard)
// ----------------------------------------------------------------------------

func (s *Server) toolRunBenchmark(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Models    []string `json:"models"`
		Prompt    string   `json:"prompt"`
		JudgeModel string  `json:"judge_model"`
		Runs      int      `json:"runs"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, NewToolError(CodeInvalidParams, "invalid arguments", err.Error())
	}
	if len(args.Models) != 2 {
		return nil, NewToolError(CodeInvalidParams, "models must contain exactly 2 entries", nil)
	}
	if args.Prompt == "" {
		return nil, NewToolError(CodeInvalidParams, "prompt is required", nil)
	}
	if args.Runs <= 0 {
		args.Runs = 5
	}
	return s.forwardPost(ctx, "/api/keys/session-benchmark", map[string]interface{}{
		"models":     args.Models,
		"prompt":     args.Prompt,
		"judge_model": args.JudgeModel,
		"runs":       args.Runs,
	})
}

func (s *Server) toolListVirtualKeys(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	return s.forwardGet(ctx, "/api/keys", nil)
}

func (s *Server) toolCreateVirtualKey(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Label         string  `json:"label"`
		Provider      string  `json:"provider"`
		Model         string  `json:"model"`
		MonthlyBudget float64 `json:"monthly_budget"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, NewToolError(CodeInvalidParams, "invalid arguments", err.Error())
	}
	return s.forwardPost(ctx, "/api/keys", map[string]interface{}{
		"label":          args.Label,
		"provider":       args.Provider,
		"model":          args.Model,
		"monthly_budget": args.MonthlyBudget,
	})
}

func (s *Server) toolGetQuotas(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	return s.forwardGet(ctx, "/api/keys/quotas", nil)
}

func (s *Server) toolListAlerts(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		TriggeredOnly bool `json:"triggered_only"`
	}
	_ = json.Unmarshal(raw, &args)
	params := map[string]string{}
	if args.TriggeredOnly {
		params["triggered_only"] = "true"
	}
	return s.forwardGet(ctx, "/api/alerts", params)
}

func (s *Server) toolSetAlertRule(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		RuleID    string  `json:"rule_id"`
		Name      string  `json:"name"`
		Metric    string  `json:"metric"`
		Threshold float64 `json:"threshold"`
		Channel   string  `json:"channel"`
		Target    string  `json:"target"`
		Enabled   bool    `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, NewToolError(CodeInvalidParams, "invalid arguments", err.Error())
	}
	return s.forwardPost(ctx, "/api/alerts", map[string]interface{}{
		"rule_id":   args.RuleID,
		"name":      args.Name,
		"metric":    args.Metric,
		"threshold": args.Threshold,
		"channel":   args.Channel,
		"target":    args.Target,
		"enabled":   args.Enabled,
	})
}

func (s *Server) toolExportLogs(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Format string `json:"format"`
		Since  string `json:"since"`
		Until  string `json:"until"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Format == "" {
		args.Format = "jsonl"
	}
	return s.forwardPost(ctx, "/api/logs/export", map[string]interface{}{
		"format": args.Format,
		"since":  args.Since,
		"until":  args.Until,
		"requested_at": time.Now().Format(time.RFC3339),
	})
}

// toolStartSession forwards a "Start recording" call to the
// dashboard's /api/sessions/record route with {enable: true}.
//
// The dashboard writes `synapse:session:vk:<virtualKey>` Redis
// keys with a 24h TTL; the proxy reads these keys on every
// request and tags the resulting RequestLog row with the session
// id. This makes session recording transparent: any agent
// (Hermes, curl, Playground) using any of the user's virtual keys
// is recorded without the agent having to know about the session.
//
// Requires tier=full (SaaS dashboard subscription).
func (s *Server) toolStartSession(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Label   string `json:"label"`
		GroupBy string `json:"group_by"`
	}
	_ = json.Unmarshal(raw, &args)
	body := map[string]interface{}{"enable": true}
	if args.Label != "" {
		body["label"] = args.Label
	}
	if args.GroupBy != "" {
		body["group_by"] = args.GroupBy
	}
	return s.forwardPost(ctx, "/api/sessions/record", body)
}

// toolStopSession forwards a "Stop recording" call to the
// dashboard. The dashboard deletes the Redis session keys
// associated with the user's virtual keys (or with a specific
// session_id when provided).
//
// Requires tier=full.
func (s *Server) toolStopSession(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(raw, &args)
	body := map[string]interface{}{"enable": false}
	if args.SessionID != "" {
		body["sessionId"] = args.SessionID
	}
	return s.forwardPost(ctx, "/api/sessions/record", body)
}

// toolListSessions forwards a "List sessions" call to the
// dashboard's analytics endpoint. The dashboard reads the
// recent rows from its own session table (which the proxy
// doesn't keep locally) and returns the summary.
//
// Requires tier=full.
func (s *Server) toolListSessions(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Limit <= 0 {
		args.Limit = 20
	}
	if args.Limit > 100 {
		args.Limit = 100
	}
	return s.forwardGet(ctx, "/api/analytics/sessions", map[string]string{
		"limit": fmt.Sprintf("%d", args.Limit),
	})
}
