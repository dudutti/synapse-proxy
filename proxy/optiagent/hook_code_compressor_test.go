package optiagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestASTCodeCompressor_Python(t *testing.T) {
	pythonCode := "```python\nclass Model:\n    def __init__(self):\n        self.name = 'test'\n        self.val = 1\n        self.active = True\n        self.score = 99.9\n        print('Initialized successfully')\n```"

	hctx := &HookContext{
		VK:         "sk-test",
		RawPayload: []byte(`{"messages":[{"role":"user","content":` + jsonString(pythonCode) + `}]}`),
	}

	hook := &ASTCodeCompressorHook{}
	resPayload, err := hook.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("BeforeRequest returned error: %v", err)
	}

	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(resPayload, &body); err != nil {
		t.Fatalf("Failed to unmarshal result payload: %v", err)
	}

	contentStr, ok := body.Messages[0].Content.(string)
	if !ok {
		t.Fatalf("Result content is not string: %T", body.Messages[0].Content)
	}

	if !strings.Contains(contentStr, "# ... code elided ...") {
		t.Errorf("Expected python code to be compressed with elision comment, got: %s", contentStr)
	}
}

func TestASTCodeCompressor_Go(t *testing.T) {
	goCode := "```go\nfunc processRequest(ctx context.Context, req *Request) error {\n    log.Println(\"Handling request\")\n    err := doWork(ctx, req)\n    if err != nil {\n        return err\n    }\n    return nil\n}\n```"

	hctx := &HookContext{
		VK:         "sk-test",
		RawPayload: []byte(`{"messages":[{"role":"user","content":` + jsonString(goCode) + `}]}`),
	}

	hook := &ASTCodeCompressorHook{}
	resPayload, err := hook.BeforeRequest(context.Background(), hctx)
	if err != nil {
		t.Fatalf("BeforeRequest returned error: %v", err)
	}

	var body struct {
		Messages []Message `json:"messages"`
	}
	if err := json.Unmarshal(resPayload, &body); err != nil {
		t.Fatalf("Failed to unmarshal result payload: %v", err)
	}

	contentStr, ok := body.Messages[0].Content.(string)
	if !ok {
		t.Fatalf("Result content is not string: %T", body.Messages[0].Content)
	}

	if !strings.Contains(contentStr, "// ... code elided ...") {
		t.Errorf("Expected go code to be compressed with elision comment, got: %s", contentStr)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
