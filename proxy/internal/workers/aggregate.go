// Package workers — aggregate per-hook savings from
// the perHookSavings JSON blob persisted in RequestLog.
//
// Each RequestLog row has a perHookSavings column
// holding a JSON object like:
//   {"logCompressor":{"bytesSaved":1000,"tokensSaved":250},
//    "outputReducer":{"bytesSaved":50,"tokensSaved":12},
//    "ccrCache":{"hits":1},
//    "tagProtector":{"zones":2},
//    "synapseRetrieve":{"toolsInjected":1}}
//
// AggregateHookSavings sums the values across rows
// so the dashboard can show "LogCompressor saved X
// bytes and Y tokens" without doing the parse in JS.
//
// TDD: covered by aggregate_test.go.
package workers

import "encoding/json"

// HookSavings is the aggregated counters for a single
// hook across N request rows. Bytes/Tokens are sums;
// Count is the number of rows where this hook fired.
type HookSavings struct {
	Bytes  int `json:"bytes"`
	Tokens int `json:"tokens"`
	Count  int `json:"count"`
}

// perHookRow is the JSON shape we accept per row.
// All fields optional (zero-value if missing).
type perHookRow struct {
	LogCompressor *struct {
		BytesSaved  int `json:"bytesSaved"`
		TokensSaved int `json:"tokensSaved"`
	} `json:"logCompressor"`
	OutputReducer *struct {
		BytesSaved  int `json:"bytesSaved"`
		TokensSaved int `json:"tokensSaved"`
	} `json:"outputReducer"`
	CCRCache *struct {
		Hits int `json:"hits"`
	} `json:"ccrCache"`
	TagProtector *struct {
		Zones int `json:"zones"`
	} `json:"tagProtector"`
	SynapseRetrieve *struct {
		ToolsInjected int `json:"toolsInjected"`
	} `json:"synapseRetrieve"`
}

// AggregateHookSavings takes a slice of perHookSavings
// JSON strings (one per RequestLog row) and returns
// the per-hook totals. Malformed or empty rows are
// silently skipped (a row with perHookSavings="" or
// "{}" contributes zeros).
func AggregateHookSavings(rows []string) map[string]HookSavings {
	out := map[string]HookSavings{
		"logCompressor":   {},
		"outputReducer":   {},
		"ccrCache":        {},
		"tagProtector":    {},
		"synapseRetrieve": {},
	}
	for _, raw := range rows {
		if raw == "" || raw == "{}" {
			continue
		}
		var r perHookRow
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			continue
		}
		if r.LogCompressor != nil {
			lc := out["logCompressor"]
			lc.Bytes += r.LogCompressor.BytesSaved
			lc.Tokens += r.LogCompressor.TokensSaved
			lc.Count++
			out["logCompressor"] = lc
		}
		if r.OutputReducer != nil {
			or := out["outputReducer"]
			or.Bytes += r.OutputReducer.BytesSaved
			or.Tokens += r.OutputReducer.TokensSaved
			or.Count++
			out["outputReducer"] = or
		}
		if r.CCRCache != nil {
			ccr := out["ccrCache"]
			ccr.Count += r.CCRCache.Hits
			out["ccrCache"] = ccr
		}
		if r.TagProtector != nil {
			tp := out["tagProtector"]
			tp.Count += r.TagProtector.Zones
			out["tagProtector"] = tp
		}
		if r.SynapseRetrieve != nil {
			sr := out["synapseRetrieve"]
			sr.Count += r.SynapseRetrieve.ToolsInjected
			out["synapseRetrieve"] = sr
		}
	}
	return out
}