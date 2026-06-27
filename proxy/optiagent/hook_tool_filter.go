// Package optiagent — ToolFilterHook.
//
// Migrated from proxy.go (formerly lines ~244-278 inline block).
//
// Two-tier firewall for tool calls:
//
//  1. Denylist (always active when a backend is wired).
//     The operator-curated set of blocked tool names, stored in
//     Redis under `synapse:denied_tools:<vk>`. Consulted under
//     permissive mode (block_unknown_tools=false). A tool in this
//     set returns 403 to the agent.
//
//  2. Allowlist (opt-in via block_unknown_tools=true).
//     A CSV of tool names from VirtualKeyConfig.AllowedTools. When
//     block_unknown_tools is on AND allowed_tools is non-empty, any
//     tool NOT in the list returns 400.
//
// Precedence: denylist is checked FIRST. A tool that's both in the
// denylist and in the allowlist is blocked (the operator's veto
// wins). This matches the legacy inline behaviour and is verified
// by TestToolFilterHook_DenylistWinsOverAllowlist.
//
// Fail-open: any backend error on the denylist lookup is swallowed.
// The hook would rather let a tool through than break the agent on
// a transient Redis blip. This is the same contract as
// SessionCircuitBreakerHook.

package optiagent

import (
	"context"
	"log"
	"net/http"
	"strings"
)

// ToolFilterHook enforces the per-VK tool allow/deny policy.
type ToolFilterHook struct{}

// Name returns the stable hook identifier.
func (h *ToolFilterHook) Name() string { return "tool_filter" }

// Priority 130 = runs after fingerprint (100) and circuit breaker
// (110), before payload-mutating hooks (200+). A blocked tool
// should short-circuit BEFORE we spend cycles on L3 compression.
func (h *ToolFilterHook) Priority() int { return 130 }

// IsEnabled always returns true. The hook's work is gated by the
// presence of tool calls in the payload and the per-VK feature
// flags, both of which are checked in BeforeRequest.
func (h *ToolFilterHook) IsEnabled(vk string) bool { return vk != "" }

// BeforeRequest enforces the firewall. See file docstring.
//
// Returns (nil, nil) on the happy path or on backend failure
// (fail-open). On block: ShortCircuitStatus and Body are set; the
// runner returns the body without forwarding upstream.
func (h *ToolFilterHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	IncrementBefore(h.Name(), hctx.VK)

	payload := currentPayload(hctx)
	if len(payload) == 0 {
		return nil, nil
	}

	// No work if there are no tool calls in this payload.
	calls := ExtractAllToolCalls(payload)
	if len(calls) == 0 {
		return nil, nil
	}

	// Read the per-VK feature flags. Missing/zero values = permissive
	// mode (denylist-only, allowlist disabled). This matches the
	// legacy inline semantics exactly.
	blockUnknown, _ := hctx.Feature("block_unknown_tools")
	allowedStr, _ := hctx.Feature("allowed_tools")
	blockUnknownB, _ := blockUnknown.(bool)

	// --- Denylist (always consulted when a backend is wired) ---
	if blocked := h.checkDenylist(ctx, hctx, calls, blockUnknownB); blocked {
		// Short-circuit already set on hctx by checkDenylist.
		return nil, nil
	}

	// --- Allowlist (only in strict mode with a non-empty list) ---
	if blockUnknownB {
		if allowed, ok := allowedStr.(string); ok && strings.TrimSpace(allowed) != "" {
			if h.checkAllowlist(hctx, calls, allowed) {
				return nil, nil
			}
		}
	}

	return nil, nil
}

// checkDenylist returns true iff the request was blocked (caller
// should stop processing). The actual short-circuit is set on hctx
// when a block occurs. Returns false on no-match OR on fail-open.
//
// Invariant: when block_unknown_tools is true, the legacy inline
// code SKIPS the denylist (allowing the allowlist to be the sole
// gate). We preserve that behaviour to keep this hook drop-in
// equivalent to the legacy code path.
func (h *ToolFilterHook) checkDenylist(ctx context.Context, hctx *HookContext, calls []ToolCall, blockUnknown bool) bool {
	backend := currentSessionCBBackend()
	if backend == nil {
		// No backend wired: fail-open. No denylist enforcement.
		return false
	}

	denyKey := "synapse:denied_tools:" + hctx.VK
	denied, err := backend.SMembers(ctx, denyKey)
	if err != nil {
		log.Printf("[tool-filter] denylist SMembers failed for vk=%s: %v (fail-open)", hctx.VK, err)
		return false
	}
	if len(denied) == 0 {
		return false
	}

	denyMap := make(map[string]bool, len(denied))
	for _, d := range denied {
		denyMap[d] = true
	}
	for _, tc := range calls {
		if denyMap[tc.ToolName] {
			// Legacy semantics: when strict mode is on, the inline
			// code skips the denylist. Match that.
			if blockUnknown {
				return false
			}
			body := `{"error":{"message":"Agent Firewall denied tool call: ` + tc.ToolName + `","type":"tool_filter","code":403}}`
			hctx.ShortCircuitStatus = http.StatusForbidden
			hctx.ShortCircuitBody = []byte(body)
			log.Printf("[tool-filter] DENYLIST HIT: tool=%s vk=%s", tc.ToolName, hctx.VK)
			return true
		}
	}
	return false
}

// checkAllowlist returns true iff the request is OK (all calls
// in the allowlist). Returns false (with short-circuit set) iff
// any call is outside the list. The allowlist is a CSV; entries
// are trimmed of surrounding whitespace.
func (h *ToolFilterHook) checkAllowlist(hctx *HookContext, calls []ToolCall, allowedCSV string) bool {
	allowed := strings.Split(allowedCSV, ",")
	allowedMap := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		if t := strings.TrimSpace(a); t != "" {
			allowedMap[t] = true
		}
	}
	for _, tc := range calls {
		if !allowedMap[tc.ToolName] {
			body := `{"error":{"message":"Agent Firewall blocked unauthorized tool call: ` + tc.ToolName + `","type":"tool_filter","code":400}}`
			hctx.ShortCircuitStatus = http.StatusBadRequest
			hctx.ShortCircuitBody = []byte(body)
			log.Printf("[tool-filter] ALLOWLIST BLOCK: tool=%s vk=%s", tc.ToolName, hctx.VK)
			return false
		}
	}
	return true
}

// AfterResponse is a no-op.
func (h *ToolFilterHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

func init() {
	RegisterHook(&ToolFilterHook{})
}