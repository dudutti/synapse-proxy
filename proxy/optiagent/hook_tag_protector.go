// Package optiagent — TagProtectorHook.
//
// The other compression hooks (CacheAligner, LogCompressor,
// CCR Compress, etc.) walk the payload and may mutate
// strings that contain code, markup, or other structured
// content. We don't want a compression pass to break a
// JavaScript snippet, an HTML structure, a Markdown code
// fence, or an XML CDATA section.
//
// This hook is a small safety net. It runs FIRST in the
// BeforeRequest chain (priority 650, before CacheAligner
// at 700) and records the positions of all "protected
// zones" — content that must be left untouched by
// downstream hooks — in the hook context:
//
//   hctx.Features["tag_protector_zones"] = []TagZone{...}
//
// The downstream hooks can read this list and refuse to
// mutate text inside a protected zone. The hook itself
// does not modify the payload; it's pure information for
// the other hooks.
//
// Reference: headroom/headroom/transforms/tag_protector.py.
// The Python version is more elaborate (it can rewrite
// the structure to make protected zones "stick out" to
// the other transforms). We just publish the positions
// here and let the consumer hooks respect them.

package optiagent

import (
	"context"
	"log"
	"regexp"

	"synapse-proxy/internal/metrics"
)

// TagZone describes a span of the payload that must be
// left untouched by compression hooks. Start and End are
// absolute byte offsets into the payload; the span
// [Start, End) is the protected content. OpenTag and
// CloseTag are the literal strings that delimit the
// zone (e.g. "<script>" / "</script>"); they're kept for
// debugging and for the dashboard's "what was protected"
// view.
type TagZone struct {
	OpenTag  string
	CloseTag string
	Start    int
	End      int
}

// TagProtectorHook is a BeforeRequest hook that scans
// the payload for protected zones.
type TagProtectorHook struct{}

// Name returns the hook name.
func (h *TagProtectorHook) Name() string { return "tag_protector" }

// Priority is 650, which runs BEFORE CacheAligner (700)
// and any other compression hook. The protected-zone
// map must be populated before the other hooks see the
// payload, so the order is: TagProtector → CacheAligner
// → LogCompressor → CCR Compress → CCR Retrieve.
func (h *TagProtectorHook) Priority() int { return 650 }

// protectedTagPairs lists the open/close tag pairs we
// treat as protected. Each entry is a regex that matches
// the opening tag, paired with the matching closing tag
// (the closing tag is a literal — we look for it after
// the opening tag). The order matters for nested tags
// (e.g. <code> inside <pre>); the first match wins.
var protectedTagPairs = []struct {
	OpenRE  *regexp.Regexp
	OpenLit string
	CloseLit string
}{
	// CDATA must come first because the inner content
	// can contain anything including <script> etc.
	{regexp.MustCompile(`<!\[CDATA\[`), "<![CDATA[", "]]>"},
	// HTML <script>...</script>
	{regexp.MustCompile(`<script[\s>]`), "<script", "</script>"},
	// HTML <style>...</style>
	{regexp.MustCompile(`<style[\s>]`), "<style", "</style>"},
	// HTML <pre>...</pre>
	{regexp.MustCompile(`<pre[\s>]`), "<pre", "</pre>"},
	// HTML <code>...</code>
	{regexp.MustCompile(`<code[\s>]`), "<code", "</code>"},
	// Markdown fenced code block (3+ backticks, optional
	// language). We match the first fence as the open and
	// the next 3+ backticks as the close.
	{regexp.MustCompile("```[a-zA-Z0-9_]*"), "```", "```"},
}

// BeforeRequest scans the payload for protected zones
// and stores them in hctx.Features. It does not mutate
// the payload.
func (h *TagProtectorHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementBefore(h.Name(), hctx.VK)
	if hctx == nil || len(hctx.OptimizedPayload) == 0 {
		return hctx.OptimizedPayload, nil
	}
	payload := hctx.OptimizedPayload
	zones := findProtectedZones(payload)
	hctx.SetFeature("tag_protector_zones", zones)
	if len(zones) > 0 {
		log.Printf("[%s] %d protected zone(s) vk=%s", h.Name(), len(zones), hctx.VK)
		// P1.5 DASHBOARD FIRST: bump the per-hook metric
		// so the dashboard shows the number of zones
		// protected per request.
		metrics.RecordTagProtector(len(zones))
	}
	return payload, nil
}

// AfterResponse is a no-op.
func (h *TagProtectorHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	IncrementAfter(h.Name(), hctx.VK)
	return nil, nil
}

// IsEnabled returns true. The hook is essentially free
// (one linear scan) and provides a safety guarantee for
// every other hook downstream.
func (h *TagProtectorHook) IsEnabled(vk string) bool { return true }

// findProtectedZones walks the payload and returns the
// list of protected zones. The walk is linear: for each
// unprotected byte, we look for the next opening tag
// from protectedTagPairs; when we find one, we record
// the open position, then search for the matching close
// tag. Unclosed tags are skipped (and logged).
func findProtectedZones(payload []byte) []TagZone {
	var zones []TagZone
	i := 0
	for i < len(payload) {
		// Find the next opening tag.
		next, openLiteral := findNextOpenTag(payload, i)
		if next < 0 {
			break
		}
		openIdx := next
		openTag := protectedTagPairs[findOpenPairIndex(payload, openIdx)]
		openEnd := openIdx + len(openLiteral)
		closeIdx := bytesIndex(payload[openEnd:], []byte(openTag.CloseLit))
		if closeIdx < 0 {
			// Unclosed tag — log and stop scanning
			// from here. We don't fail the request; the
			// downstream hooks will see the unclosed
			// tag as regular content.
			log.Printf("[tag_protector] unclosed %q at byte %d, skipping", openLiteral, openIdx)
			i = openEnd
			continue
		}
		closeIdx += openEnd
		endIdx := closeIdx + len(openTag.CloseLit)
		zones = append(zones, TagZone{
			OpenTag:  openLiteral,
			CloseTag: openTag.CloseLit,
			Start:    openIdx,
			End:      endIdx,
		})
		i = endIdx
	}
	return zones
}

// findNextOpenTag returns the byte offset of the next
// opening tag starting at or after `from`, or -1 if no
// opening tag is found before end-of-payload. It also
// returns the actual literal that matched (which may
// include attributes or language suffixes — e.g. for
// Markdown fences the matched literal includes the
// language: "```python", not just "```").
func findNextOpenTag(payload []byte, from int) (int, string) {
	type match struct {
		idx  int
		literal string
	}
	best := match{-1, ""}
	for _, p := range protectedTagPairs {
		loc := p.OpenRE.FindIndex(payload[from:])
		if loc == nil {
			continue
		}
		absIdx := from + loc[0]
		// The literal is the bytes from absIdx up to
		// the closing of the opening tag (whitespace, '>',
		// or, for Markdown, the end of the language).
		literal := string(payload[absIdx:openTagEnd(payload, absIdx, p.OpenLit)])
		if best.idx < 0 || absIdx < best.idx {
			best = match{absIdx, literal}
		}
	}
	return best.idx, best.literal
}

// openTagEnd returns the index just past the end of the
// opening tag literal. For HTML tags (which end at '>'
// or whitespace), the literal is everything up to AND
// including the first '>' (so "<code>" is the literal,
// not "<code"). For CDATA, the literal is "<![CDATA["
// exactly (we stop at the first character that's not
// '[' or 'C' or 'D' or 'A' or 'T' or ']'). For Markdown
// fences, the literal is the run of backticks plus the
// language identifier.
func openTagEnd(payload []byte, idx int, baseLit string) int {
	// CDATA: stop at the first character that isn't
	// part of "<![CDATA[".
	if baseLit == "<![CDATA[" {
		// baseLit is exactly 9 chars: <![CDATA[
		// We've already matched the regex. Stop here.
		return idx + 9
	}
	// For non-Markdown tags, the literal includes the
	// '>' if present.
	if baseLit != "```" {
		// Find the first '>' (the closing of the
		// opening tag). If no '>' is found before
		// whitespace or EOF, the tag is malformed and
		// we just include everything up to the next
		// whitespace.
		for i := idx + len(baseLit); i < len(payload); i++ {
			c := payload[i]
			if c == '>' {
				return i + 1
			}
			if c == ' ' || c == '	' || c == '\n' || c == '\r' {
				return i
			}
		}
		return len(payload)
	}
	// Markdown: literal is "```" + optional language
	// (a-z, A-Z, 0-9, _, -). Find the end of the language.
	for i := idx + 3; i < len(payload); i++ {
		c := payload[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return i
		}
	}
	return len(payload)
}

// findOpenPairIndex returns the index of the entry in
// protectedTagPairs whose OpenRE matched at byte idx.
// We use a re-scan of the regexes (cheap: at most 7
// regexes, each compiled once).
func findOpenPairIndex(payload []byte, idx int) int {
	for i, p := range protectedTagPairs {
		if bytesIndex(payload[idx:], []byte(p.OpenLit)) == 0 {
			return i
		}
	}
	return -1
}

// init registers the hook.
func init() {
	RegisterHook(&TagProtectorHook{})
	log.Printf("[hooks] registered TagProtectorHook at priority 650")
}
