// Package optiagent — Git diff output compressor hook.
//
// Implements Phase 2: parsing and compressing unified git diffs in
// message contents by trimming context lines and offering CCR retrieval.
package optiagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// DiffCompressorHook is a BeforeRequest hook that detects and
// compresses git diff outputs in message contents.
type DiffCompressorHook struct{}

// Name returns the hook name.
func (h *DiffCompressorHook) Name() string { return "diff_compressor" }

// Priority is 730: runs after SmartCrusher (720) and before CCRRetrieve (750).
func (h *DiffCompressorHook) Priority() int { return 730 }

// IsEnabled always returns true.
func (h *DiffCompressorHook) IsEnabled(vk string) bool { return true }

// BeforeRequest parses the payload and applies DiffCompressor logic.
func (h *DiffCompressorHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
	if hctx == nil {
		return nil, nil
	}
	payload := hctx.OptimizedPayload
	if payload == nil {
		payload = hctx.RawPayload
	}
	if len(payload) == 0 {
		return nil, nil
	}

	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || len(body.Messages) == 0 {
		return nil, nil
	}

	modified := false
	for i, msg := range body.Messages {
		if contentStr, ok := msg.Content.(string); ok && len(contentStr) > 0 {
			compressed, ok := compressDiffsInText(contentStr)
			if ok {
				body.Messages[i].Content = compressed
				modified = true
				log.Printf("[DiffCompressor] compressed git diff in message %d", i)
			}
		}
	}

	if modified {
		newPayload, err := json.Marshal(body)
		if err == nil {
			return newPayload, nil
		}
	}

	return payload, nil
}

// AfterResponse is a no-op.
func (h *DiffCompressorHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	return nil, nil
}

// compressDiffsInText scans the text for unified git diff blocks,
// compresses them, and adds CCR markers if they are long.
func compressDiffsInText(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	var resultLines []string
	modified := false

	i := 0
	n := len(lines)
	for i < n {
		line := lines[i]
		if strings.HasPrefix(line, "diff --git ") {
			// Start of diff block. Collect all lines belonging to the diff.
			var diffBlock []string
			diffBlock = append(diffBlock, line)
			i++

			for i < n {
				l := lines[i]
				// A line belongs to a diff if it starts with recognized prefixes.
				if isDiffLine(l) {
					diffBlock = append(diffBlock, l)
					i++
				} else {
					break
				}
			}

			// Compress the collected diff block
			compressedDiff, cOk := compressSingleDiff(diffBlock)
			if cOk {
				resultLines = append(resultLines, compressedDiff)
				modified = true
			} else {
				resultLines = append(resultLines, diffBlock...)
			}
		} else {
			resultLines = append(resultLines, line)
			i++
		}
	}

	return strings.Join(resultLines, "\n"), modified
}

func isDiffLine(l string) bool {
	if strings.HasPrefix(l, "diff ") ||
		strings.HasPrefix(l, "index ") ||
		strings.HasPrefix(l, "similarity ") ||
		strings.HasPrefix(l, "rename ") ||
		strings.HasPrefix(l, "new file ") ||
		strings.HasPrefix(l, "deleted file ") ||
		strings.HasPrefix(l, "--- ") ||
		strings.HasPrefix(l, "+++ ") ||
		strings.HasPrefix(l, "@@ ") ||
		strings.HasPrefix(l, "+") ||
		strings.HasPrefix(l, "-") ||
		strings.HasPrefix(l, " ") ||
		strings.HasPrefix(l, "\\") ||
		l == "" {
		return true
	}
	return false
}

type fileDiff struct {
	headers []string
	hunks   []diffHunk
}

type diffHunk struct {
	header string
	lines  []string
}

func compressSingleDiff(diffLines []string) (string, bool) {
	// Parse the diff block into fileDiff structures
	var files []fileDiff
	var curFile *fileDiff
	var curHunk *diffHunk

	for _, line := range diffLines {
		if strings.HasPrefix(line, "diff --git ") {
			if curFile != nil {
				if curHunk != nil {
					curFile.hunks = append(curFile.hunks, *curHunk)
					curHunk = nil
				}
				files = append(files, *curFile)
			}
			curFile = &fileDiff{headers: []string{line}}
		} else if curFile != nil {
			if strings.HasPrefix(line, "@@ ") {
				if curHunk != nil {
					curFile.hunks = append(curFile.hunks, *curHunk)
				}
				curHunk = &diffHunk{header: line}
			} else if curHunk != nil {
				curHunk.lines = append(curHunk.lines, line)
			} else {
				curFile.headers = append(curFile.headers, line)
			}
		}
	}
	if curFile != nil {
		if curHunk != nil {
			curFile.hunks = append(curFile.hunks, *curHunk)
		}
		files = append(files, *curFile)
	}

	if len(files) == 0 {
		return "", false
	}

	// Compress each hunk in each file
	var sb strings.Builder
	for _, f := range files {
		for _, h := range f.headers {
			sb.WriteString(h + "\n")
		}
		for _, hunk := range f.hunks {
			sb.WriteString(hunk.header + "\n")
			compressedHunkLines := compressHunk(hunk.lines)
			for _, l := range compressedHunkLines {
				sb.WriteString(l + "\n")
			}
		}
	}

	compressedStr := strings.TrimSuffix(sb.String(), "\n")
	originalStr := strings.Join(diffLines, "\n")

	if len(compressedStr) >= len(originalStr) {
		return "", false
	}

	// CCR Storage check if original diff is long
	if len(diffLines) >= 50 { // min_lines_for_ccr
		hSum := sha256.Sum256([]byte(originalStr))
		droppedHash := hex.EncodeToString(hSum[:])[:12]

		store := GetGlobalCompressionStore()
		if store != nil {
			_, err := store.Save(droppedHash, []byte(originalStr))
			if err == nil {
				log.Printf("[DiffCompressor] saved original git diff (%d lines) under ccr key %s", len(diffLines), droppedHash)
				headerMsg := fmt.Sprintf("[Original git diff (%d lines) offloaded to <<ccr:%s>>]\n", len(diffLines), droppedHash)
				return headerMsg + compressedStr, true
			}
		}
	}

	return compressedStr, true
}

func compressHunk(lines []string) []string {
	// Identify modification indices
	modIndices := make(map[int]bool)
	for idx, line := range lines {
		// modified if starts with + or - but NOT header lines
		if (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")) &&
			!strings.HasPrefix(line, "+++") && !strings.HasPrefix(line, "---") {
			modIndices[idx] = true
		}
	}

	const maxContextLines = 2
	var output []string
	inElision := false

	for idx, line := range lines {
		if modIndices[idx] {
			output = append(output, line)
			inElision = false
			continue
		}

		// Check distance to nearest modified line
		keep := false
		for mIdx := range modIndices {
			dist := idx - mIdx
			if dist < 0 {
				dist = -dist
			}
			if dist <= maxContextLines {
				keep = true
				break
			}
		}

		if keep {
			output = append(output, line)
			inElision = false
		} else {
			if !inElision {
				output = append(output, "...")
				inElision = true
			}
		}
	}

	return output
}

func init() {
	RegisterHook(&DiffCompressorHook{})
}
