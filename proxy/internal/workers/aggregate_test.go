// Tests for aggregateHookSavings: parses the JSON blob
// in perHookSavings (one row per request) and sums
// the bytes/tokens per hook.

package workers

import (
	"testing"
)

func TestAggregateHookSavings_Empty(t *testing.T) {
	out := AggregateHookSavings(nil)
	if out == nil {
		t.Fatal("expected non-nil output")
	}
	for _, v := range out {
		if v.Bytes != 0 || v.Tokens != 0 || v.Count != 0 {
			t.Fatalf("expected zero sums for empty input, got %+v", v)
		}
	}
}

func TestAggregateHookSavings_LogCompressor(t *testing.T) {
	rows := []string{
		`{"logCompressor":{"bytesSaved":1000,"tokensSaved":250}}`,
		`{"logCompressor":{"bytesSaved":500,"tokensSaved":125}}`,
		`{}`,
		`{"logCompressor":{"bytesSaved":250,"tokensSaved":62}}`,
	}
	out := AggregateHookSavings(rows)
	lc, ok := out["logCompressor"]
	if !ok {
		t.Fatal("missing logCompressor")
	}
	if lc.Bytes != 1750 {
		t.Errorf("bytes: expected 1750, got %d", lc.Bytes)
	}
	if lc.Tokens != 437 {
		t.Errorf("tokens: expected 437, got %d", lc.Tokens)
	}
	if lc.Count != 3 {
		t.Errorf("count: expected 3, got %d", lc.Count)
	}
}

func TestAggregateHookSavings_AllHooks(t *testing.T) {
	rows := []string{
		`{"logCompressor":{"bytesSaved":100},"outputReducer":{"bytesSaved":50,"tokensSaved":12},"ccrCache":{"hits":1},"tagProtector":{"zones":2},"synapseRetrieve":{"toolsInjected":1}}`,
		`{"logCompressor":{"bytesSaved":200},"outputReducer":{"bytesSaved":100,"tokensSaved":25},"ccrCache":{"hits":1},"tagProtector":{"zones":1}}`,
	}
	out := AggregateHookSavings(rows)
	if out["logCompressor"].Bytes != 300 {
		t.Errorf("lc bytes: %d", out["logCompressor"].Bytes)
	}
	if out["outputReducer"].Bytes != 150 || out["outputReducer"].Tokens != 37 {
		t.Errorf("or: %+v", out["outputReducer"])
	}
	if out["ccrCache"].Count != 2 {
		t.Errorf("ccr count: %d", out["ccrCache"].Count)
	}
	if out["tagProtector"].Count != 3 {
		t.Errorf("tp count: %d", out["tagProtector"].Count)
	}
	if out["synapseRetrieve"].Count != 1 {
		t.Errorf("sr count: %d", out["synapseRetrieve"].Count)
	}
}

func TestAggregateHookSavings_IgnoresMalformed(t *testing.T) {
	rows := []string{
		`{not json`,
		`{"logCompressor":{"bytesSaved":100}}`,
		``,
	}
	out := AggregateHookSavings(rows)
	if out["logCompressor"].Bytes != 100 {
		t.Errorf("malformed rows should be skipped, got %d", out["logCompressor"].Bytes)
	}
}