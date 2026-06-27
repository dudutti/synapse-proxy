// Package optiagent — dedupeByKeyword.
//
// Groups non-adjacent lines that share the same "keyword"
// (the first important keyword found in the line) and
// keeps the first occurrence, replacing subsequent ones
// with a count marker. This is the Headroom
// dedupe_warnings behavior, simplified to per-keyword
// rather than per-normalised-signature.
//
// The keyword extraction reuses the LogLineScorer's
// importantRE regex (timeout, refused, missing, failed,
// etc.). Lines without a matching keyword are passed
// through unchanged.

package optiagent

import (
	"regexp"
	"strings"
)

// dedupeByKeyword groups lines that share the same
// keyword (extracted via the scorer's importantRE) and
// keeps the first occurrence, replacing subsequent
// occurrences with a count marker.
//
// The output preserves the order of first occurrences.
// A group of N occurrences of the same keyword becomes
// 1 line + 1 marker line.
//
// Lines that don't match any keyword (INFO, DEBUG,
// regular output) are passed through unchanged.
func dedupeByKeyword(lines []string, scorer *LogLineScorer) []string {
	if len(lines) < 2 {
		return lines
	}
	// First pass: extract the keyword of each line.
	// The keyword is the first importantRE match.
	type indexed struct {
		text    string
		keyword string
	}
	indexedLines := make([]indexed, len(lines))
	for i, l := range lines {
		indexedLines[i] = indexed{text: l, keyword: extractKeyword(l, scorer.importantRE)}
	}
	// Group occurrences by keyword. We track the
	// position of the FIRST occurrence of each keyword
	// and the total count.
	type group struct {
		firstIdx int
		count    int
	}
	groups := make(map[string]*group)
	for i, il := range indexedLines {
		if il.keyword == "" {
			continue
		}
		if g, ok := groups[il.keyword]; !ok {
			groups[il.keyword] = &group{firstIdx: i, count: 1}
		} else {
			g.count++
		}
	}
	// Build output.
	out := make([]string, 0, len(lines))
	for i, il := range indexedLines {
		if il.keyword == "" {
			out = append(out, il.text)
			continue
		}
		g := groups[il.keyword]
		if g.firstIdx == i {
			// First occurrence of this keyword.
			out = append(out, il.text)
			if g.count > 1 {
				out = append(out, "... ("+itoa(g.count-1)+" identical \""+il.keyword+"\" warnings) ...")
			}
			continue
		}
		// Subsequent occurrence: drop.
	}
	return out
}

// extractKeyword returns the first important keyword
// found in the line (lowercased), or "" if no keyword
// matches.
func extractKeyword(line string, importantRE *regexp.Regexp) string {
	loc := importantRE.FindStringIndex(line)
	if loc == nil {
		return ""
	}
	return strings.ToLower(line[loc[0]:loc[1]])
}
