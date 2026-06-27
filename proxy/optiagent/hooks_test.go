package optiagent

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
)

// fakeHook is a minimal hook for runner tests.
type fakeHook struct {
	name      string
	priority  int
	enabled   bool
	beforeFn  func(ctx context.Context, hctx *HookContext) ([]byte, error)
	afterFn   func(ctx context.Context, hctx *HookContext) ([]byte, error)
	beforeCnt int32
	afterCnt  int32
}

func (f *fakeHook) Name() string          { return f.name }
func (f *fakeHook) Priority() int         { return f.priority }
func (f *fakeHook) IsEnabled(string) bool { return f.enabled }
func (f *fakeHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	atomic.AddInt32(&f.beforeCnt, 1)
	if f.beforeFn != nil {
		return f.beforeFn(ctx, hctx)
	}
	return nil, nil
}
func (f *fakeHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	atomic.AddInt32(&f.afterCnt, 1)
	if f.afterFn != nil {
		return f.afterFn(ctx, hctx)
	}
	return nil, nil
}

func newTestContext() *HookContext {
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	return &HookContext{
		VK:               "vk-test",
		Provider:         "anthropic",
		Model:            "claude-opus-4-5",
		RawPayload:       []byte(`{"model":"claude-opus-4-5","messages":[{"role":"user","content":"hi"}]}`),
		OptimizedPayload: []byte(`{"model":"claude-opus-4-5","messages":[{"role":"user","content":"hi"}]}`),
		ResponseHeaders:  hdr,
		Features:         map[string]interface{}{},
	}
}

func TestRunBeforeHooks_DisabledHookSkipped(t *testing.T) {
	ResetHooks()
	defer ResetHooks()
	h := &fakeHook{name: "skipper", enabled: false}
	RegisterHook(h)

	hctx := newTestContext()
	_, sc := RunBeforeHooks(context.Background(), hctx)
	if sc {
		t.Fatal("disabled hook should not short-circuit")
	}
	if atomic.LoadInt32(&h.beforeCnt) != 0 {
		t.Fatalf("disabled hook should not be called, got %d invocations", h.beforeCnt)
	}
}

func TestRunBeforeHooks_EnabledHookCalled(t *testing.T) {
	ResetHooks()
	defer ResetHooks()
	h := &fakeHook{name: "counter", enabled: true, priority: 100}
	RegisterHook(h)

	hctx := newTestContext()
	_, sc := RunBeforeHooks(context.Background(), hctx)
	if sc {
		t.Fatal("hook should not short-circuit")
	}
	if atomic.LoadInt32(&h.beforeCnt) != 1 {
		t.Fatalf("enabled hook should be called once, got %d", h.beforeCnt)
	}
}

func TestRunBeforeHooks_PayloadMutationChain(t *testing.T) {
	ResetHooks()
	defer ResetHooks()

	h1 := &fakeHook{
		name: "upper", enabled: true, priority: 100,
		beforeFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			return []byte("UPPER"), nil
		},
	}
	h2 := &fakeHook{
		name: "suffix", enabled: true, priority: 200,
		beforeFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			// hctx.OptimizedPayload was updated by the runner after
			// h1 returned "UPPER". Suffix it to prove the chain.
			prev := hctx.OptimizedPayload
			return append(prev, []byte("-CHAIN")...), nil
		},
	}
	RegisterHook(h1)
	RegisterHook(h2)

	hctx := newTestContext()
	payload, _ := RunBeforeHooks(context.Background(), hctx)
	want := "UPPER-CHAIN"
	if string(payload) != want {
		t.Fatalf("payload chain broken: got %q want %q", string(payload), want)
	}
}

func TestRunBeforeHooks_HookErrorFailOpen(t *testing.T) {
	ResetHooks()
	defer ResetHooks()

	badHook := &fakeHook{
		name: "bad", enabled: true, priority: 100,
		beforeFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			return nil, errors.New("boom")
		},
	}
	goodHook := &fakeHook{
		name: "good", enabled: true, priority: 200,
		beforeFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			return []byte(`{"ok":true}`), nil
		},
	}
	RegisterHook(badHook)
	RegisterHook(goodHook)

	hctx := newTestContext()
	payload, sc := RunBeforeHooks(context.Background(), hctx)
	if sc {
		t.Fatal("errors must not short-circuit")
	}
	// The bad hook returned nil+error, the runner falls through to
	// the next hook which transforms the original payload.
	if string(payload) != `{"ok":true}` {
		t.Fatalf("expected fallthrough payload, got %q", string(payload))
	}
}

func TestRunBeforeHooks_HookPanicRecovers(t *testing.T) {
	ResetHooks()
	defer ResetHooks()

	panicker := &fakeHook{
		name: "panicker", enabled: true, priority: 100,
		beforeFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			panic("kaboom")
		},
	}
	good := &fakeHook{
		name: "good", enabled: true, priority: 200,
		beforeFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			return []byte(`{"survived":true}`), nil
		},
	}
	RegisterHook(panicker)
	RegisterHook(good)

	hctx := newTestContext()
	payload, sc := RunBeforeHooks(context.Background(), hctx)
	if sc {
		t.Fatal("panic must not short-circuit")
	}
	if string(payload) != `{"survived":true}` {
		t.Fatalf("expected post-panic hook payload, got %q", string(payload))
	}
}

func TestRunBeforeHooks_PriorityOrder(t *testing.T) {
	ResetHooks()
	defer ResetHooks()

	var order []string

	h1 := &fakeHook{name: "first", enabled: true, priority: 100,
		beforeFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			order = append(order, "first")
			return nil, nil
		}}
	h2 := &fakeHook{name: "second", enabled: true, priority: 200,
		beforeFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			order = append(order, "second")
			return nil, nil
		}}
	h3 := &fakeHook{name: "third", enabled: true, priority: 300,
		beforeFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			order = append(order, "third")
			return nil, nil
		}}
	// Register out of order to verify sort.
	RegisterHook(h3)
	RegisterHook(h1)
	RegisterHook(h2)

	hctx := newTestContext()
	RunBeforeHooks(context.Background(), hctx)
	if len(order) != 3 || order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Fatalf("hooks ran in wrong order: %v", order)
	}
}

func TestRunAfterHooks_PassthroughOnError(t *testing.T) {
	ResetHooks()
	defer ResetHooks()

	h := &fakeHook{name: "after-bad", enabled: true, priority: 100,
		afterFn: func(ctx context.Context, hctx *HookContext) ([]byte, error) {
			return nil, errors.New("after boom")
		},
	}
	RegisterHook(h)

	hctx := newTestContext()
	hctx.UpstreamResponse = []byte(`{"resp":"original"}`)
	body := RunAfterHooks(context.Background(), hctx)
	if string(body) != `{"resp":"original"}` {
		t.Fatalf("error must not mutate body, got %q", string(body))
	}
}

func TestHookContext_SetFeatureAndFetch(t *testing.T) {
	hctx := newTestContext()
	if !hctx.SetFeature("foo", 42) {
		t.Fatal("SetFeature should succeed")
	}
	v, ok := hctx.Feature("foo")
	if !ok {
		t.Fatal("Feature should return true")
	}
	if v.(int) != 42 {
		t.Fatalf("expected 42, got %v", v)
	}
}

func TestHookContext_SetHeader(t *testing.T) {
	hctx := newTestContext()
	if !hctx.SetHeader("X-Test", "yes") {
		t.Fatal("SetHeader should succeed")
	}
	if hctx.ResponseHeaders.Get("X-Test") != "yes" {
		t.Fatal("header not set")
	}
}

func TestRegistry_UnregisterHook(t *testing.T) {
	ResetHooks()
	defer ResetHooks()
	h := &fakeHook{name: "removeme", enabled: true, priority: 100}
	RegisterHook(h)
	if len(GetHooks()) != 1 {
		t.Fatal("expected 1 hook")
	}
	if !UnregisterHook("removeme") {
		t.Fatal("unregister should succeed")
	}
	if len(GetHooks()) != 0 {
		t.Fatal("expected 0 hooks after unregister")
	}
}

func TestRegistry_DuplicateNameReplaces(t *testing.T) {
	ResetHooks()
	defer ResetHooks()
	h1 := &fakeHook{name: "dup", enabled: true, priority: 100}
	h2 := &fakeHook{name: "dup", enabled: true, priority: 200}
	RegisterHook(h1)
	RegisterHook(h2)
	all := GetHooks()
	if len(all) != 1 {
		t.Fatalf("expected 1 hook (last wins), got %d", len(all))
	}
	if all[0].Priority() != 200 {
		t.Fatalf("expected priority 200, got %d", all[0].Priority())
	}
}
