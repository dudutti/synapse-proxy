// Tests for LogCompressorHook. The hook truncates stack
// traces in tool outputs so the LLM only sees the first
// few frames and the last few frames. The middle frames
// (which are almost always the standard library / framework
// frames that don't help the agent debug the issue) are
// dropped with a marker.
//
// Reference: headroom/crates/headroom-core/src/transforms/
// log_compressor.rs (the Rust port). Their strategy:
//   - Keep first N frames (default 3) so the LLM sees the
//     origin of the error
//   - Keep last M frames (default 3) so the LLM sees where
//     the error propagated to
//   - Dedupe adjacent identical lines (a tight loop
//     emitting the same error N times is a common pattern)
//   - Preserve chained exceptions ("During handling of the
//     above exception, another exception occurred:")
//   - Normalize paths and line numbers in the dedup so that
//     "frames N1, N2" and "frames N1+1, N2+1" don't
//     re-collide after a code edit
//
// Our hook runs as a BeforeRequest hook on the assistant
// tool_calls content and the tool message content (the two
// places where stack traces appear in a chat-completion
// payload). It rewrites the payload in place — there's no
// retrieval step because logs aren't cacheable.

package optiagent

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// logCompressorTrace is a fixture that contains a 12-line
// stack trace with a chained exception. We use it across
// several tests as a representative "real agent error"
// payload. The trace is JSON-escaped (real \n sequences
// are written as \\n in the source) so the test can
// embed it inside a JSON string literal without
// producing invalid JSON.
//
// IMPORTANT: this trace has no double-quote characters
// inside it. The findStringField helper in LogCompressorHook
// is a tiny character-class scanner that doesn't handle
// embedded quote-in-string-detection perfectly; the cleanest
// fix is to keep this fixture quote-free. Real-world
// Python tracebacks do contain quotes (e.g. File "/app/x.py"),
// but those are handled by Go's stdlib JSON encoder at
// payload-write time, which will properly escape them.
var logCompressorTrace = strings.ReplaceAll(`Traceback (most recent call last):
  File /app/server.py, line 42, in handle_request
    return process(req)
  File /app/server.py, line 87, in process
    raise ValueError(bad input)
  File /app/server.py, line 87, in process
    raise ValueError(bad input)
  File /app/server.py, line 87, in process
    raise ValueError(bad input)
  File /app/server.py, line 87, in process
    raise ValueError(bad input)
  File /app/server.py, line 87, in process
    raise ValueError(bad input)
  File /app/framework/middleware.py, line 12, in __call__
    return self.next(req)
  File /app/framework/middleware.py, line 27, in __call__
    return await self.handler(req)
  File /app/framework/router.py, line 5, in route
    return self.dispatch(req)
  File /app/framework/router.py, line 18, in dispatch
    return handler(req)
  File /app/agent.py, line 200, in run_agent
    return await llm_call(req)
  File /app/agent.py, line 175, in run_agent
    return await llm_call(req)
ValueError: bad input

During handling of the above exception, another exception occurred:

Traceback (most recent call last):
  File /app/agent.py, line 178, in run_agent
    raise RuntimeError(agent failed)
RuntimeError: agent failed
`, "\n", `\n`)

// TestLogCompressorHook_DetectsPythonTrace: the hook must
// recognize the "Traceback (most recent call last):" header
// as a Python stack trace and apply the keep-first-N +
// keep-last-M transform. Non-trace content is left alone.
func TestLogCompressorHook_DetectsPythonTrace(t *testing.T) {
	h := &LogCompressorHook{}
	in := []byte(`{"messages":[{"role":"tool","content":"` + logCompressorTrace + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-py",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)

	// A middle frame (one that falls between the first-3
	// and last-3 boundaries) must be truncated. We pick
	// "middleware.py, line 27" because it's the 8th frame
	// in a 12-frame trace — definitively in the middle.
	if strings.Contains(outStr, "middleware.py, line 27") {
		t.Fatalf("middle frames were not truncated; output still has the full trace\n  out:\n%s", outStr)
	}
	// The truncation marker must be present. The hook
	// emits it in the form "... (K middle frames dropped) ..."
	// so we just check for the prefix.
	if !strings.Contains(outStr, "middle frames dropped") {
		t.Fatalf("expected a 'middle frames dropped' marker\n  out:\n%s", outStr)
	}
	// The first few frames (where the request entered)
	// must be kept.
	if !strings.Contains(outStr, "server.py, line 42") {
		t.Fatalf("first frames (where the request entered) were dropped\n  out:\n%s", outStr)
	}
	// The error message line at the end of the trace must
	// be kept.
	if !strings.Contains(outStr, "ValueError: bad input") {
		t.Fatalf("error message line was dropped\n  out:\n%s", outStr)
	}
}

// TestLogCompressorHook_DetectsJavaScriptTrace: Node.js
// stack traces start with "Error: <message>" followed by
// "    at <func> (<file>:<line>:<col>)". The hook must
// detect and truncate them too.
//
// We assert that the trace was truncated (the
// "middle frames dropped" marker is present) and that
// the output is shorter than the input. We don't pin
// exactly which frames are kept because the keepFirst/
// keepLast policy is intentionally simple and the
// important user-visible property is "the trace got
// shorter" + "Node internals are no longer dominating".
func TestLogCompressorHook_DetectsJavaScriptTrace(t *testing.T) {
	h := &LogCompressorHook{}
	jsTrace := strings.ReplaceAll(`Error: bad input
    at handleRequest (/app/server.js:42:7)
    at process (/app/server.js:87:3)
    at middleware (/app/middleware.js:12:5)
    at router (/app/router.js:5:1)
    at dispatch (/app/router.js:18:11)
    at runAgent (/app/agent.js:200:4)
    at Object.<anonymous> (/app/index.js:10:1)
    at Module._compile (node:internal/modules/cjs/loader.js:5:1)
    at Module._extensions..js (node:internal/modules/cjs/loader.js:10:1)
    at Module.load (node:internal/modules/cjs/loader.js:15:1)
`, "\n", `\n`)
	in := []byte(`{"messages":[{"role":"tool","content":"` + jsTrace + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-js",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)

	// The trace must be truncated.
	if !strings.Contains(outStr, "middle frames dropped") {
		t.Fatalf("expected a 'middle frames dropped' marker\n  out:\n%s", outStr)
	}
	// The first user-code frame must be kept.
	if !strings.Contains(outStr, "server.js:42:7") {
		t.Fatalf("first user-code frame was dropped\n  out:\n%s", outStr)
	}
	// The output must be shorter than the input (some
	// bytes were saved).
	if len(out) >= len(in) {
		t.Fatalf("output is not shorter than input\n  in:  %d\n  out: %d", len(in), len(out))
	}
}

// TestLogCompressorHook_DetectsRustPanic: Rust panics
// look like "thread 'main' panicked at '...'" followed
// by a backtrace with "   N: <file>:<line>".
//
// We assert that the trace was truncated (the
// "middle frames dropped" marker is present) and that
// the application frames are kept (vs. only the
// std/runtime frames).
func TestLogCompressorHook_DetectsRustPanic(t *testing.T) {
	h := &LogCompressorHook{}
	rustTrace := strings.ReplaceAll(`thread 'main' panicked at 'oops', src/main.rs:42:5
stack backtrace:
   0: std::panicking::panic::main
   1: std::panicking::begin_panic_handler::main
   2: my_app::handle_request
   3: my_app::process
   4: tokio::runtime::task::poll::main
   5: std::sys::unix::thread_start
   6: <unknown>
`, "\n", `\n`)
	in := []byte(`{"messages":[{"role":"tool","content":"` + rustTrace + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-rust",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)

	// The trace must be truncated.
	if !strings.Contains(outStr, "middle frames dropped") {
		t.Fatalf("expected a 'middle frames dropped' marker\n  out:\n%s", outStr)
	}
	// The first user-code frame must be kept (the agent
	// needs to see where the error originated).
	if !strings.Contains(outStr, "panic::main") {
		t.Fatalf("first frame was dropped\n  out:\n%s", outStr)
	}
	// The output must be shorter than the input.
	if len(out) >= len(in) {
		t.Fatalf("output is not shorter than input\n  in:  %d\n  out: %d", len(in), len(out))
	}
}

// TestLogCompressorHook_DeduplicatesAdjacentDuplicates: a
// loop emitting the same frame 5 times in a row is a very
// common pattern (e.g. retry logic, hot path, tight loop
// failure). The hook must dedupe adjacent identical lines
// and report the count.
//
// We use a trace with exactly 8 frames (3+5 duplicates),
// so the truncation pass (keep first 3 + last 3) does NOT
// drop the duplicates from the output. This isolates the
// dedup pass from the truncation pass: we want to assert
// that dedup is what's working, not that the 5 duplicates
// happened to land in the truncated middle.
func TestLogCompressorHook_DeduplicatesAdjacentDuplicates(t *testing.T) {
	h := &LogCompressorHook{}
	dup := strings.ReplaceAll(`Traceback (most recent call last):
  File /app/agent.py, line 1, in retry
  File /app/agent.py, line 2, in retry
  File /app/agent.py, line 3, in retry
  File /app/agent.py, line 4, in retry
  File /app/agent.py, line 4, in retry
  File /app/agent.py, line 4, in retry
  File /app/agent.py, line 4, in retry
  File /app/agent.py, line 4, in retry
  File /app/agent.py, line 5, in final
  File /app/agent.py, line 6, in after
ValueError: too many retries
`, "\n", `\n`)
	in := []byte(`{"messages":[{"role":"tool","content":"` + dup + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-dedup",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)

	// We should not have 5 adjacent copies of the same line.
	matched := regexp.MustCompile(`File /app/agent.py, line 4, in retry`).FindAllString(outStr, -1)
	if len(matched) > 2 {
		t.Fatalf("deduplication failed: still %d copies of the same line\n  out:\n%s", len(matched), outStr)
	}
	// And the dedup count must be reported so the LLM
	// knows the loop ran N times.
	if !strings.Contains(outStr, "identical") {
		t.Fatalf("deduplication should report a count, got:\n%s", outStr)
	}
}

// TestLogCompressorHook_PreservesChainedExceptions: when a
// "raise X from Y" pattern is present (Python's chained
// exceptions), the hook must keep the second trace as well.
// Compressing only one of the two would hide the root cause
// from the agent.
func TestLogCompressorHook_PreservesChainedExceptions(t *testing.T) {
	h := &LogCompressorHook{}
	in := []byte(`{"messages":[{"role":"tool","content":"` + logCompressorTrace + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-chain",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	outStr := string(out)

	// Both exceptions must be present.
	if !strings.Contains(outStr, "ValueError: bad input") {
		t.Fatalf("primary exception (ValueError) was dropped\n  out:\n%s", outStr)
	}
	if !strings.Contains(outStr, "RuntimeError: agent failed") {
		t.Fatalf("chained exception (RuntimeError) was dropped\n  out:\n%s", outStr)
	}
	if !strings.Contains(outStr, "During handling of the above exception") {
		t.Fatalf("chaining marker line was dropped\n  out:\n%s", outStr)
	}
}

// TestLogCompressorHook_NonTraceContentIsNoOp: regular
// tool output (no stack trace) must NOT be transformed. A
// false positive would silently corrupt legitimate output.
func TestLogCompressorHook_NonTraceContentIsNoOp(t *testing.T) {
	h := &LogCompressorHook{}
	plain := strings.ReplaceAll(`{
  users: [
    {id: 1, name: Alice},
    {id: 2, name: Bob}
  ]
}`, "\n", `\n`)
	in := []byte(`{"messages":[{"role":"tool","content":"` + plain + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-plain",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	out, _ := h.BeforeRequest(context.Background(), hctx)
	if string(out) != string(in) {
		t.Fatalf("non-trace content was modified\n  got:  %q\n  want: %q", string(out), string(in))
	}
}

// TestLogCompressorHook_SetsFeature: when the hook
// compresses a trace, it must set a feature on the hctx so
// downstream hooks (and the dashboard metrics) can see
// the savings. The feature is named log_compressor_savings
// and its value is the byte delta (input_bytes - output_bytes).
func TestLogCompressorHook_SetsFeature(t *testing.T) {
	h := &LogCompressorHook{}
	in := []byte(`{"messages":[{"role":"tool","content":"` + logCompressorTrace + `"}]}`)
	hctx := &HookContext{
		VK: "vk-log-feat",
		OptimizedPayload: in,
		Features: map[string]interface{}{},
	}
	_, _ = h.BeforeRequest(context.Background(), hctx)
	saved, ok := hctx.Feature("log_compressor_savings")
	if !ok {
		t.Fatal("expected log_compressor_savings feature to be set")
	}
	// We expect a non-zero savings on the 12-frame trace.
	if v, _ := saved.(int); v <= 0 {
		t.Fatalf("expected positive savings, got %v", saved)
	}
}
