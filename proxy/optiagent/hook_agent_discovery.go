// Package optiagent — AgentDiscoveryHook.
//
// Migrated from proxy.go (formerly lines ~215-236 inline block).
//
// Observation-only: when a request carries tool calls, record every
// tool name in the per-VK Redis set `synapse:discovered_tools:<vk>`
// (refreshed with a 30-day TTL). The dashboard reads this set and
// renders it as a checkable list under the Agent Firewall; tools the
// operator unchecks land in the denylist consulted by ToolFilterHook.
//
// Behaviour preserved from the legacy inline code:
//   - SADD all unique tool names (Redis set is dedup'd automatically).
//   - EXPIRE 30 days on the key so abandoned agents are eventually
//     forgotten without operator intervention.
//   - Skip tool calls with empty names (would clutter the dashboard).
//   - Fail open on backend errors.
//
// Priority 105: runs AFTER FingerprintHook (100, may inject a soft-
// loop system warning) and BEFORE ToolFilterHook (130) so the
// denylist consulted by the latter reflects the latest tool set.

package optiagent

import (
	"context"
	"log"
	"time"
)

// discoveredToolsTTL is how long the discovered-tools set lives
// before being forgotten. 30 days matches the legacy inline value.
const discoveredToolsTTL = 30 * 24 * time.Hour

// AgentDiscoveryHook records tool names that the agent invokes.
type AgentDiscoveryHook struct{}

// Name returns the stable hook identifier.
func (h *AgentDiscoveryHook) Name() string { return "agent_discovery" }

// Priority 105: between FingerprintHook (100) and ToolFilterHook (130).
func (h *AgentDiscoveryHook) Priority() int { return 105 }

// IsEnabled gates on a non-empty VK. The hook does no work when
// there are no tool calls anyway; BeforeRequest is the real gate.
func (h *AgentDiscoveryHook) IsEnabled(vk string) bool { return vk != "" }

// BeforeRequest records tool names. See file docstring.
//
// Returns (nil, nil) on the happy path and on backend failure
// (fail-open). The hook never short-circuits — it is purely
// observation.
func (h *AgentDiscoveryHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	IncrementBefore(h.Name(), hctx.VK)

	payload := currentPayload(hctx)
	if len(payload) == 0 {
		return nil, nil
	}

	calls := ExtractAllToolCalls(payload)
	if len(calls) == 0 {
		return nil, nil
	}

	// Dedupe + filter empty names in a single pass. The legacy
	// inline code did this loop too, with the same semantics.
	seen := make(map[string]bool, len(calls))
	unique := make([]string, 0, len(calls))
	for _, tc := range calls {
		name := tc.ToolName
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
	}
	if len(unique) == 0 {
		return nil, nil
	}

	backend := currentSessionCBBackend()
	if backend == nil {
		// No backend wired: nothing to do. This is the same
		// fail-open path as the other hooks.
		return nil, nil
	}

	key := "synapse:discovered_tools:" + hctx.VK
	if err := backend.SAddSet(ctx, key, unique, discoveredToolsTTL); err != nil {
		log.Printf("[agent-discovery] SAddSet failed for vk=%s: %v (fail-open)", hctx.VK, err)
		return nil, nil
	}
	return nil, nil
}

// AfterResponse is a no-op.
func (h *AgentDiscoveryHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

func init() {
	RegisterHook(&AgentDiscoveryHook{})
}