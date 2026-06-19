// Package mcp implements a Model Context Protocol (MCP) server for
// Synapse Proxy. The same binary can run as either:
//
//   ./synapse-proxy                          # HTTP API only (port 8080)
//   ./synapse-proxy --mcp --tier=free        # MCP server on stdio, 7 free tools
//   ./synapse-proxy --mcp --tier=full --dashboard-url=https://synapse-proxy.com
//                                          # MCP server on stdio, 12 tools, premium tools
//                                          # forwarded to the SaaS dashboard
//
// The MCP server is intentionally open source. The `free` tier refuses
// premium tools with a clear "requires paid plan" error rather than
// hiding them. The real authorization lives in the dashboard: even
// if a user patches the binary to force tier=full, the SaaS dashboard
// will reject the forwarded call if the virtual key lacks the
// `benchmark` permission. See README for the full threat model.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Version of the MCP server implementation. Bumped when tool
// signatures or JSON schemas change.
const Version = "0.1.0"

// Tier selects which tools are exposed by the MCP server.
//
//   TierFree  : 7 tools (chat, models, cache_stats, savings_summary, sessions)
//   TierFull  : 12 tools (7 free + 5 premium forwarded to the SaaS dashboard)
type Tier string

const (
	TierFree Tier = "free"
	TierFull Tier = "full"
)

// JSON-RPC 2.0 base types. Kept minimal and well-typed to make
// downstream debugging easy.

type Request struct {
	JSONRPC string      `json:"jsonrpc"` // always "2.0"
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
}

type ResponseError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC error codes, plus a few Synapse-specific ones.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	// Synapse-specific codes (range -32000 to -32099 reserved by JSON-RPC).
	CodeRequiresPaidPlan = -32001
	CodeUnauthorized      = -32002
	CodeUpstreamError     = -32003
)

// Tool describes a single MCP tool, used by the `tools/list` method
// to advertise what the server can do. The schema follows the JSON
// Schema 2020-12 dialect that Claude Code, Cursor, and Continue all
// understand.
//
// We embed the schema as a json.RawMessage so the server can keep
// the source of truth as a literal JSON string (no Go map/type
// gymnastics for nested JSON Schema fields like `items` inside an
// `array`). The trade-off: zero compile-time validation of the
// schema. The upside: the schemas are easy to read, copy, and
// compare with the MCP spec.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolInputSchema is a convenience constructor for the standard
// JSON Schema for a tool: `{"type":"object", "properties":{...},
// "required":[...]}`. Pass the schema as a Go-native map literal;
// it is marshalled to JSON internally so the caller doesn't have
// to embed raw JSON.
func ToolInputSchema(properties map[string]any, required []string) (json.RawMessage, error) {
	if properties == nil {
		properties = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	obj := map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("marshal input schema: %w", err)
	}
	return b, nil
}

// MustToolInputSchema is like ToolInputSchema but panics on error.
// Use this when the schema is a compile-time literal so any error
// is a programmer bug, not a runtime condition.
func MustToolInputSchema(properties map[string]any, required []string) json.RawMessage {
	b, err := ToolInputSchema(properties, required)
	if err != nil {
		panic(err)
	}
	return b
}

// ToolHandler is the function signature for a single tool. It
// receives the JSON-decoded params and returns either a result
// (any JSON-serializable value) or an error.
//
// Implementations should return a *ToolError when the failure is
// part of the normal tool contract (bad input, missing resource,
// requires paid plan). Plain errors are wrapped as InternalError.
type ToolHandler func(ctx context.Context, params json.RawMessage) (interface{}, error)

// Server is the MCP server. It is safe to register tools from
// multiple goroutines before calling Serve.
type Server struct {
	name         string
	version      string
	tier         Tier
	virtualKey   string
	dashboardURL string

	tools map[string]*registeredTool
}

type registeredTool struct {
	tool    Tool
	handler ToolHandler
	paid    bool // true for tools that require TierFull
}

// NewServer constructs an MCP server with the given tier. virtualKey
// is the user's sk-opti-... key; it is sent on every forwarded call
// to the dashboard. dashboardURL may be empty for TierFree (or for
// a self-hosted full tier pointed at a self-hosted dashboard).
func NewServer(tier Tier, virtualKey, dashboardURL string) *Server {
	return &Server{
		name:         "synapse-proxy",
		version:      Version,
		tier:         tier,
		virtualKey:   virtualKey,
		dashboardURL: dashboardURL,
		tools:        make(map[string]*registeredTool),
	}
}

// NewServerWithDefaults constructs an MCP server with all 12 tools
// registered (7 free, 5 paid). It is the entry point used by
// cmd/server/main.go when the --mcp flag is set.
func NewServerWithDefaults(tier Tier, virtualKey, dashboardURL string) *Server {
	s := NewServer(tier, virtualKey, dashboardURL)
	s.registerFreeTools()
	if tier == TierFull {
		s.registerPaidTools()
	}
	return s
}

// Register adds a tool to the server. If paid is true, the tool is
// only invokable under TierFull; under TierFree, calls return a
// `requires paid plan` error with the JSON-RPC code CodeRequiresPaidPlan.
//
// The handler is called with the raw JSON params; it must json.Unmarshal
// them itself. The tool's InputSchema is advertised verbatim to
// clients on `tools/list`, so keep it in sync with what the handler
// actually accepts.
func (s *Server) Register(tool Tool, handler ToolHandler, paid bool) {
	s.tools[tool.Name] = &registeredTool{
		tool:    tool,
		handler: handler,
		paid:    paid,
	}
}

// Serve runs the server on stdin/stdout. JSON-RPC 2.0 messages are
// line-delimited (one JSON object per line). It blocks until ctx
// is cancelled or stdin reaches EOF.
//
// This is the canonical MCP transport. Clients (Claude Code, Cursor,
// etc.) spawn the process as a subprocess and pipe requests to its
// stdin.
func (s *Server) Serve(ctx context.Context, read <-chan []byte, write chan<- []byte) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case raw, ok := <-read:
			if !ok {
				return nil
			}
			resp := s.handle(ctx, raw)
			out, err := json.Marshal(resp)
			if err != nil {
				continue
			}
			select {
			case write <- out:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (s *Server) handle(ctx context.Context, raw []byte) Response {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorResponse(nil, CodeParseError, "parse error", err.Error())
	}
	if req.JSONRPC != "2.0" {
		return errorResponse(req.ID, CodeInvalidRequest, "jsonrpc must be 2.0", nil)
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "ping":
		return successResponse(req.ID, map[string]string{"status": "ok"})
	default:
		return errorResponse(req.ID, CodeMethodNotFound,
			fmt.Sprintf("method not found: %s", req.Method), nil)
	}
}

func (s *Server) handleInitialize(req Request) Response {
	return successResponse(req.ID, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]string{
			"name":    s.name,
			"version": s.version,
		},
		"capabilities": map[string]interface{}{
			"tools": map[string]string{},
		},
	})
}

func (s *Server) handleToolsList(req Request) Response {
	list := make([]Tool, 0, len(s.tools))
	for _, rt := range s.tools {
		// In tier free, hide paid tools entirely from the listing
		// so the LLM doesn't even know they exist. The error on
		// `tools/call` is a defense-in-depth in case the client
		// guesses a tool name from documentation.
		if rt.paid && s.tier != TierFull {
			continue
		}
		list = append(list, rt.tool)
	}
	return successResponse(req.ID, map[string]interface{}{"tools": list})
}

func (s *Server) handleToolsCall(ctx context.Context, req Request) Response {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := remarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "invalid params", err.Error())
	}
	rt, ok := s.tools[params.Name]
	if !ok {
		return errorResponse(req.ID, CodeMethodNotFound,
			fmt.Sprintf("unknown tool: %s", params.Name), nil)
	}
	if rt.paid && s.tier != TierFull {
		return errorResponse(req.ID, CodeRequiresPaidPlan,
			"this tool requires a paid plan. See https://synapse-proxy.com/pricing for details, "+
				"or implement it yourself in your fork (the code is open source under MIT).",
			map[string]string{"tier": string(s.tier)})
	}
	result, err := rt.handler(ctx, params.Arguments)
	if err != nil {
		if te, ok := err.(*ToolError); ok {
			return errorResponse(req.ID, te.Code, te.Message, te.Data)
		}
		return errorResponse(req.ID, CodeInternalError, err.Error(), nil)
	}
	return successResponse(req.ID, map[string]interface{}{"content": result})
}

// successResponse wraps a result in a standard JSON-RPC response.
func successResponse(id interface{}, result interface{}) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}

// errorResponse wraps an error in a standard JSON-RPC response.
func errorResponse(id interface{}, code int, msg string, data interface{}) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &ResponseError{
		Code: code, Message: msg, Data: data,
	}}
}

// remarshal unmarshals src into dst. We use this to avoid a
// json.RawMessage -> map -> struct roundtrip on every call.
func remarshal(src, dst interface{}) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// ToolError is the structured error type that tool handlers may
// return. It carries a JSON-RPC code so the client sees a useful
// failure mode (e.g. invalid input, requires paid plan) rather
// than a generic "internal error".
type ToolError struct {
	Code    int
	Message string
	Data    interface{}
}

func (e *ToolError) Error() string { return e.Message }

// NewToolError is a convenience constructor.
func NewToolError(code int, msg string, data interface{}) *ToolError {
	return &ToolError{Code: code, Message: msg, Data: data}
}
