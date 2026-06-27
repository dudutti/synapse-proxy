// Integration test: does TagProtector fire in the
// hook pipeline? It's registered first (priority 650)
// but stays at 0 zones in prod.

package optiagent

import (
	"context"
	"testing"
)

func TestIntegration_TagProtectorFires(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"tool","content":"<p>hello</p><pre>code</pre>"}]}`)
	hctx := &HookContext{
		VK:               "vk-tp",
		RawPayload:       payload,
		OptimizedPayload: payload,
		Features:         map[string]interface{}{},
	}
	tp := &TagProtectorHook{}
	out, err := tp.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("TagProtector error: %v", err)
	}
	t.Logf("output: %s", string(out))
	zonesRaw := hctx.GetFeature("tag_protector_zones")
	if zonesRaw == nil {
		t.Fatal("tag_protector_zones feature not set")
	}
	zones, ok := zonesRaw.([]TagZone)
	if !ok {
		t.Fatalf("wrong type: %T", zonesRaw)
	}
	if len(zones) == 0 {
		t.Fatal("no zones detected (this is the prod bug)")
	}
	t.Logf("zones: %d", len(zones))
	for i, z := range zones {
		t.Logf("  zone %d: %q .. %q at %d-%d", i, z.OpenTag, z.CloseTag, z.Start, z.End)
	}
}