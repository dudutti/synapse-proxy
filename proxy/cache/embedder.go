//go:build local_windows
// +build local_windows

// Package cache exposes the global L2 semantic-cache embedder.
//
// The actual embedding model is implemented in Rust (rust-embedder/) and
// compiled into a static library (librust_embedder.a) by the proxy
// Dockerfile. This Go file uses CGo to call into that library and
// exposes the embedder as a singleton via the GlobalEmbedder variable
// consumed by optiagent/engine.go and optiagent/tool_dedup.go.
//
// History: the proxy originally used an external Python ONNX embedder
// sidecar. We replaced it with a Rust + CGo binding so the embedder
// runs in-process (no IPC, no sidecar container, no model download at
// runtime). The shape of the public API was preserved so optiagent/
// code paths did not have to change.
//
// This file is the local-MinGW-gcc variant. The Rust std pulls in
// Win32 system symbols (BCryptGenRandom, WSA*, NtReadFile, ole32,
// etc.) that musl on Linux/musl does not have, so the LDFLAGS here
// include the Win32 libraries. The Docker/Linux/macOS build uses
// the default embedder.go (no build tag) which has minimal LDFLAGS.
package cache

/*
#cgo CFLAGS: -I${SRCDIR}/../rust-embedder
#cgo LDFLAGS: ${SRCDIR}/../rust-embedder/target/release/librust_embedder.a -ldl -lpthread -lm -lws2_32 -luserenv -lbcrypt -lntdll -ldbghelp -lole32 -loleaut32 -luuid -ladvapi32 -lkernel32 -lnormaliz

#include <stdlib.h>
#include <stdint.h>

// Mirrors the signatures declared in rust-embedder/src/lib.rs.
extern void *rust_embedder_init(const char *model_dir);
extern void  rust_embedder_free(void *ctx);
extern int   rust_embedder_embed(void *ctx, const char *text, float *out_vector);
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"
)

// embedderDim is the output vector size of paraphrase-multilingual-MiniLM-L12-v2.
// Must match the constant in rust-embedder/src/lib.rs.
const embedderDim = 384

// Embedder is the public interface consumed by optiagent code. Any
// implementation (Rust native, mock, ONNX via Python sidecar, etc.)
// satisfies it; the engine only ever reads GlobalEmbedder.
type Embedder interface {
	GenerateEmbedding(text string) ([]float32, error)
	Close() error
}

// GlobalEmbedder is the singleton read by the L2 / tool-cache hot path.
// InitGlobalEmbedder assigns it; until then it stays nil and the L2
// path is a no-op.
var (
	GlobalEmbedder Embedder
	initOnce       sync.Once
	initErr        error
)

// InitGlobalEmbedder initialises the Rust native embedder. Safe to
// call multiple times; only the first call has an effect.
//
// modelDir must point to a directory containing config.json,
// tokenizer.json and model.safetensors (or pytorch_model.bin). The
// Dockerfile copies these to /app/models/<model-name> in the runtime
// image; the default value below matches that layout.
func InitGlobalEmbedder() error {
	initOnce.Do(func() {
		modelDir := os.Getenv("EMBEDDER_MODEL_DIR")
		if modelDir == "" {
			modelDir = "/app/models/paraphrase-multilingual-MiniLM-L12-v2"
		}

		cDir := C.CString(modelDir)
		defer C.free(unsafe.Pointer(cDir))

		raw := C.rust_embedder_init(cDir)
		if raw == nil {
			initErr = fmt.Errorf(
				"rust_embedder_init failed for model dir %q (check config.json, tokenizer.json, model.safetensors)",
				modelDir,
			)
			return
		}

		GlobalEmbedder = &rustEmbedder{ctx: raw}
	})
	return initErr
}

// CloseGlobalEmbedder releases the underlying Rust context. After
// this call, GlobalEmbedder is nil and the L2 path becomes a no-op
// again. Safe to call multiple times.
func CloseGlobalEmbedder() {
	if GlobalEmbedder == nil {
		return
	}
	if err := GlobalEmbedder.Close(); err != nil {
		fmt.Printf("[cache] CloseGlobalEmbedder: %v\n", err)
	}
	GlobalEmbedder = nil
}

// rustEmbedder is the CGo-backed Embedder implementation backed by
// the Rust static library.
type rustEmbedder struct {
	ctx unsafe.Pointer
}

// GenerateEmbedding returns a 384-dim float32 vector for the given
// text. The vector is mean-pooled + L2-normalised in the Rust layer
// (matching the reference BERT sentence-transformer behaviour).
func (r *rustEmbedder) GenerateEmbedding(text string) ([]float32, error) {
	if r.ctx == nil {
		return nil, errors.New("rust embedder context is nil")
	}
	if text == "" {
		// Empty input would break the tokenizer; return a zero vector
		// so callers still receive a well-shaped 384-dim result.
		out := make([]float32, embedderDim)
		return out, nil
	}

	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	buf := make([]float32, embedderDim)
	ret := C.rust_embedder_embed(r.ctx, cText, (*C.float)(unsafe.Pointer(&buf[0])))
	if ret != 0 {
		return nil, fmt.Errorf("rust_embedder_embed returned %d for text %q", ret, text)
	}
	return buf, nil
}

// Close releases the Rust context. Calling GenerateEmbedding after
// Close is undefined; the engine is expected to flush the L2 path
// before shutdown.
func (r *rustEmbedder) Close() error {
	if r.ctx == nil {
		return nil
	}
	C.rust_embedder_free(r.ctx)
	r.ctx = nil
	return nil
}