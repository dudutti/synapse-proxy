// Package optiagent — Syntax code compressor hook.
//
// Implements Phase 2: parsing and eliding body lines of long functions/methods
// in source code blocks (Python, Go, JS/TS) in prompt messages.
package optiagent

import (
	"context"
	"encoding/json"
	"log"
	"strings"
)

// ASTCodeCompressorHook is a BeforeRequest hook that detects and
// elides long code function bodies in prompt markdown code blocks.
type ASTCodeCompressorHook struct{}

// Name returns the hook name.
func (h *ASTCodeCompressorHook) Name() string { return "code_compressor" }

// Priority is 760: runs after CCRRetrieve (750).
func (h *ASTCodeCompressorHook) Priority() int { return 760 }

// IsEnabled always returns true.
func (h *ASTCodeCompressorHook) IsEnabled(vk string) bool { return true }

// BeforeRequest parses the payload and applies ASTCodeCompressor logic.
func (h *ASTCodeCompressorHook) BeforeRequest(ctx context.Context, hctx *HookContext) ([]byte, error) {
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
			compressed, ok := compressCodeBlocksInText(contentStr)
			if ok {
				body.Messages[i].Content = compressed
				modified = true
				log.Printf("[ASTCodeCompressor] compressed code blocks in message %d", i)
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
func (h *ASTCodeCompressorHook) AfterResponse(ctx context.Context, hctx *HookContext) ([]byte, error) {
	return nil, nil
}

// compressCodeBlocksInText parses Markdown fenced code blocks and applies
// compression on eligible programming languages (Python, Go, JS, TS).
func compressCodeBlocksInText(text string) (string, bool) {
	var sb strings.Builder
	modified := false
	idx := 0
	n := len(text)

	for idx < n {
		startFence := strings.Index(text[idx:], "```")
		if startFence == -1 {
			sb.WriteString(text[idx:])
			break
		}

		startFence += idx
		sb.WriteString(text[idx:startFence])

		// Find end of language identifier (up to newline)
		langStart := startFence + 3
		lineEnd := strings.Index(text[langStart:], "\n")
		if lineEnd == -1 {
			sb.WriteString(text[startFence:])
			break
		}
		lineEnd += langStart

		lang := strings.TrimSpace(text[langStart:lineEnd])
		langLower := strings.ToLower(lang)

		// Find ending fence
		endFence := strings.Index(text[lineEnd+1:], "```")
		if endFence == -1 {
			sb.WriteString(text[startFence:])
			break
		}
		endFence += lineEnd + 1

		codeContent := text[lineEnd+1 : endFence]
		compCode, cOk := compressCodeContent(codeContent, langLower)

		sb.WriteString("```" + lang + "\n")
		if cOk {
			sb.WriteString(compCode)
			modified = true
		} else {
			sb.WriteString(codeContent)
		}
		sb.WriteString("```")

		idx = endFence + 3
	}

	return sb.String(), modified
}

func compressCodeContent(code string, lang string) (string, bool) {
	switch lang {
	case "python", "py":
		return compressPython(code)
	case "go", "golang", "javascript", "js", "typescript", "ts":
		return compressBraceLang(code)
	}
	return "", false
}

// compressPython elides bodies of classes and functions that are long in Python.
func compressPython(code string) (string, bool) {
	lines := strings.Split(code, "\n")
	var output []string
	modified := false
	i := 0
	n := len(lines)

	for i < n {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Start of class or function def
		if strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "class ") {
			// Measure current line indentation
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			
			// Find function body lines (subsequent lines indented deeper)
			bodyStart := i + 1
			bodyEnd := bodyStart
			for bodyEnd < n {
				l := lines[bodyEnd]
				if strings.TrimSpace(l) == "" {
					bodyEnd++
					continue
				}
				lIndent := len(l) - len(strings.TrimLeft(l, " \t"))
				if lIndent > indent {
					bodyEnd++
				} else {
					break
				}
			}

			bodyLen := bodyEnd - bodyStart
			if bodyLen > 5 {
				// Elide body
				output = append(output, line)
				indentStr := line[:indent] + "    "
				output = append(output, indentStr+"# ... code elided ...")
				modified = true
				i = bodyEnd
			} else {
				output = append(output, line)
				i++
			}
		} else {
			output = append(output, line)
			i++
		}
	}

	return strings.Join(output, "\n"), modified
}

// compressBraceLang elides bodies of classes and functions using braces { ... }
func compressBraceLang(code string) (string, bool) {
	lines := strings.Split(code, "\n")
	var output []string
	modified := false
	i := 0
	n := len(lines)

	for i < n {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Detect function declaration line
		isFuncStart := strings.HasPrefix(trimmed, "func ") ||
			strings.HasPrefix(trimmed, "function ") ||
			strings.Contains(trimmed, "class ") ||
			(strings.Contains(trimmed, "(") && strings.Contains(trimmed, ")") && strings.HasSuffix(trimmed, "{"))

		if isFuncStart && strings.Contains(line, "{") {
			// Find opening brace and track brace matching to find closing brace
			braceCount := 0
			funcStartLine := i
			funcEndLine := -1

			for j := i; j < n; j++ {
				l := lines[j]
				braceCount += strings.Count(l, "{")
				braceCount -= strings.Count(l, "}")
				if braceCount == 0 {
					funcEndLine = j
					break
				}
			}

			if funcEndLine != -1 && (funcEndLine-funcStartLine) > 5 {
				// Replace everything between opening line and closing line with elision comment
				output = append(output, line)
				
				// Keep matching indentation of signature line
				indent := len(line) - len(strings.TrimLeft(line, " \t"))
				indentStr := line[:indent] + "    "
				output = append(output, indentStr+"// ... code elided ...")
				output = append(output, lines[funcEndLine])
				
				modified = true
				i = funcEndLine + 1
			} else {
				output = append(output, line)
				i++
			}
		} else {
			output = append(output, line)
			i++
		}
	}

	return strings.Join(output, "\n"), modified
}

func init() {
	RegisterHook(&ASTCodeCompressorHook{})
}
