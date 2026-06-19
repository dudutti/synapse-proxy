// Package mcp — request-scoped auth helpers.
//
// The HTTP transport extracts the client's bearer token from the
// Authorization header and stashes it in the request context.
// Handlers (in tools_paid.go) read it back to forward to the
// dashboard. The stdio transport stashes the key from the
// env var at server construction time.
//
// Why pass the key in the context rather than as a handler
// argument? Because the JSON-RPC 2.0 dispatch layer
// (handleToolsCall) doesn't know the key — it just routes to a
// registered handler. The handler is the only place that knows
// the key matters. So we plumb it through ctx.
package mcp

import (
	"context"
	"strings"
)

// authKeyCtxKey is unexported so other packages can't collide
// with our context key. Use authKeyFromContext / contextWithAuthKey.
type authKeyCtxKey struct{}

// contextWithAuthKey returns a copy of ctx carrying the given
// virtual key.
func contextWithAuthKey(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, authKeyCtxKey{}, key)
}

// authKeyFromContext returns the virtual key previously stored
// with contextWithAuthKey, or "" if none was set.
func authKeyFromContext(ctx context.Context) string {
	v, _ := ctx.Value(authKeyCtxKey{}).(string)
	return v
}

// bearerToken extracts the bearer token from an Authorization
// header value. It returns the empty string if the header is
// missing, malformed, or uses a different scheme.
//
// Examples:
//
//	"Bearer sk-opti-abc123" -> "sk-opti-abc123"
//	"bearer sk-opti-abc123" -> "sk-opti-abc123"  (case-insensitive scheme)
//	"Basic dXNlcjpwYXNz"     -> ""                (not a bearer)
//	""                       -> ""
func bearerToken(headerVal string) string {
	if headerVal == "" {
		return ""
	}
	const scheme = "bearer "
	if len(headerVal) < len(scheme) {
		return ""
	}
	if !strings.EqualFold(headerVal[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(headerVal[len(scheme):])
}
