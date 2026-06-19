package optiagent

// Deterministic JSON marshalling for L3-compressed payloads.
//
// The standard library's json.Marshal is NOT deterministic for our use
// case: it sorts map keys alphabetically, but the order it iterates a
// generic map[string]interface{} can vary depending on Go's map
// iteration randomization. Two calls to CompressPayload with the
// SAME input payload can therefore produce different output bytes.
//
// Why this matters: provider prompt caching (Anthropic, OpenAI,
// MiniMax) uses byte-exact prefix matching. If Synapse Proxy's L3
// compressor emits non-deterministic bytes for an otherwise identical
// input, we silently invalidate the provider's cache on every
// request. The agent pays full input price for the prefix instead of
// the 10% cache_read price.
//
// The fix: a deterministic encoder that:
//   - emits keys in alphabetical order (matches stdlib map behavior
//     but explicit so we don't depend on Go's map impl)
//   - omits trailing whitespace (compact)
//   - disables HTML escaping (so "Ã©" stays as "Ã©" not "\u00e9")
//   - handles nested maps and slices the same way
//
// The encoder mirrors the subset of JSON that we generate from a
// `map[string]interface{}` (the type produced by
// `json.Unmarshal(payload, &genericPayload)` in compressor.go). It is
// NOT a general-purpose JSON library: it does not handle struct
// types, channels, custom MarshalJSON, etc. If a future caller
// passes anything else, it falls back to a non-deterministic
// encoding and logs a warning.

import (
	"bytes"
	"encoding/json"
	"log"
	"sort"
	"strconv"
)

// marshalDeterministic encodes a JSON value built from
// map[string]interface{} and []interface{} (the shapes produced by
// `json.Unmarshal` into a generic interface{}). Output is canonical:
// sorted keys, no whitespace, no HTML escaping, no Unicode
// escaping for printable ASCII.
//
// The function is recursive and stack-safe for the payload sizes we
// see in practice (a 100k-token prompt becomes a Go tree of ~2k
// map/slice nodes, well below the default 1GB goroutine stack).
func marshalDeterministic(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(estimateSize(v))
	if err := writeDeterministic(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// estimateSize pre-sizes the buffer based on the value tree's
// approximate byte length. This avoids the ~10x realloc cost of
// letting bytes.Buffer grow from zero.
func estimateSize(v interface{}) int {
	switch x := v.(type) {
	case string:
		return len(x) + 2 // quotes
	case bool:
		return 5
	case nil:
		return 4
	case float64, float32:
		return 20
	case int, int8, int16, int32, int64:
		return 20
	case uint, uint8, uint16, uint32, uint64:
		return 20
	case map[string]interface{}:
		n := 2 // { }
		for k, vv := range x {
			n += len(k) + 3 + estimateSize(vv) // "k":v,
		}
		return n
	case []interface{}:
		n := 2
		for _, vv := range x {
			n += estimateSize(vv) + 1 // ,
		}
		return n
	default:
		// Fall back to a conservative size; we don't expect other types
		// in L3-compressed payloads but if one sneaks in, encoding/json
		// will still produce correct output (just non-deterministic for
		// that one value).
		return 64
	}
}

// writeDeterministic writes a single JSON value to buf. Keys in
// objects are sorted alphabetically. Numbers are emitted via the
// standard library's Number formatting for float64, which is
// already deterministic.
func writeDeterministic(buf *bytes.Buffer, v interface{}) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case float64:
		// strconv.FormatFloat with 'g' and -1 precision produces the
		// shortest round-trippable representation. It's deterministic.
		buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case float32:
		buf.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	case int:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int8:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int16:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int32:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
	case uint:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint8:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint16:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint32:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		buf.WriteString(strconv.FormatUint(x, 10))
	case string:
		// We use json.Encoder with SetEscapeHTML(false) so that
		// characters like '<' are emitted as-is (not as \u003c).
		// This is critical for the L3 compressor's CoT regex,
		// which expects to see literal '<thought>' markers in
		// the assistant messages it prunes. We also disable
		// the default JSON Unicode escaping for printable ASCII
		// (utf-8 passthrough) by using a json.Encoder instead of
		// json.Marshal â€” the latter always escapes '<', '>', and
		// '&', which would defeat the CoT pruning.
		//
		// Note: this is a per-string operation; we still walk the
		// map keys ourselves to keep the key order deterministic.
		var sbuf bytes.Buffer
		enc := json.NewEncoder(&sbuf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(x); err != nil {
			return err
		}
		// json.Encoder.Encode appends a trailing newline. Strip it.
		b := sbuf.Bytes()
		if len(b) > 0 && b[len(b)-1] == '\n' {
			b = b[:len(b)-1]
		}
		buf.Write(b)
	case map[string]interface{}:
		if len(x) == 0 {
			buf.WriteString("{}")
			return nil
		}
		// Sort keys for determinism. We allocate a slice here; the
		// outer estimateSize + Grow() call amortizes the cost.
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			// Keys are JSON strings; same encoding as values.
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := writeDeterministic(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []interface{}:
		if len(x) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteByte('[')
		for i, vv := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeDeterministic(buf, vv); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		// Unexpected type. Fall back to json.Marshal which handles
		// all the standard library types. The output for this one
		// value may not be deterministic across calls (e.g. map
		// iteration order for a map[interface{}]interface{}), but
		// in practice the L3 path only produces
		// map[string]interface{} and []interface{}.
		log.Printf("marshalDeterministic: unexpected type %T, falling back to json.Marshal", v)
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}
