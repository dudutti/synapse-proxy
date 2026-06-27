package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeEmbedder(t *testing.T) {
	// Skip if MODEL_DIR is not set or model files do not exist.
	// The test exercises the public InitGlobalEmbedder → Embedder
	// path (the legacy NewNativeEmbedder constructor was removed
	// when the duplicate cache/embedder_cgo.go file was cleaned up
	// in Sprint 0).
	modelDir := os.Getenv("EMBEDDER_MODEL_DIR")
	if modelDir == "" {
		modelDir = "/app/models/paraphrase-multilingual-MiniLM-L12-v2"
	}

	if _, err := os.Stat(filepath.Join(modelDir, "config.json")); os.IsNotExist(err) {
		t.Skipf("Skipping test because model dir %q config.json is not present", modelDir)
	}

	if err := InitGlobalEmbedder(); err != nil {
		t.Fatalf("Failed to initialize GlobalEmbedder: %v", err)
	}
	defer CloseGlobalEmbedder()

	if GlobalEmbedder == nil {
		t.Fatal("GlobalEmbedder is nil after InitGlobalEmbedder")
	}

	testTexts := []string{
		"Hello world, testing the native Go/Rust CGO text embedder.",
		"Bonjour le monde, test de l'embeddeur natif Go/Rust CGO.",
		"Short text",
	}

	for _, text := range testTexts {
		vector, err := GlobalEmbedder.GenerateEmbedding(text)
		if err != nil {
			t.Errorf("Failed to generate embedding for text %q: %v", text, err)
			continue
		}

		if len(vector) != embedderDim {
			t.Errorf("Expected vector dimension to be %d, got %d", embedderDim, len(vector))
		}

		// Verify non-zero vectors
		allZero := true
		for _, val := range vector {
			if val != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Errorf("Generated embedding vector is all zeros for text %q", text)
		}
	}
}

