// Package optiagent — no-op hook template.
//
// This file ships a no-op hook ("noop") that is auto-registered at
// init() time. It exists for two reasons:
//
//  1. It validates that the registry + runner plumbing works end-to-end
//     in the absence of any real hook (used by tests/hooks_test.go).
//  2. It serves as a copy-paste template for future hooks (CCR,
//     ContentRouter, OutputShaper).
//
// Real hooks MUST NOT inherit from this — they should implement
// Hook directly. The no-op is intentionally the simplest possible
// correct hook.

package optiagent

import (
	"context"
)

// noopHook is the reference no-op hook.
type noopHook struct{}

// Name returns the stable hook identifier.
func (n *noopHook) Name() string { return "noop" }

// Priority returns the execution order. 1000 = run last (default).
func (n *noopHook) Priority() int { return 1000 }

// IsEnabled always returns true (no VK filter).
func (n *noopHook) IsEnabled(vk string) bool { return true }

// BeforeRequest is a pass-through. Returns the optimized payload
// unchanged.
func (n *noopHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	if hctx.OptimizedPayload != nil {
		return hctx.OptimizedPayload, nil
	}
	return hctx.RawPayload, nil
}

// AfterResponse is a pass-through. Returns the upstream response
// unchanged.
func (n *noopHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	return hctx.UpstreamResponse, nil
}

func init() {
	RegisterHook(&noopHook{})
}
