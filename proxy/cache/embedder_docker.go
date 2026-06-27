//go:build !local_windows
// +build !local_windows

// Package cache exposes the global L2 semantic-cache embedder.
//
// The actual embedding model is implemented in Rust (rust-embedder/)
// and compiled into a static library (librust_embedder.a) by the
// proxy Dockerfile. This Go file uses CGo to call into that library
// and exposes the embedder as a singleton via the GlobalEmbedder
// variable consumed by optiagent/engine.go and optiagent/tool_dedup.go.
//
// History: the proxy originally used an external Python ONNX embedder
// sidecar. We replaced it with a Rust + CGo binding so the embedder
// runs in-process (no IPC, no sidecar container, no model download
// at runtime). The shape of the public API was preserved so
// optiagent/ code paths did not have to change.
//
// This is the default build variant (Docker/Linux/macOS/Windows-MSVC).
// The Rust std on these toolchains does NOT pull in Win32-only
// symbols, so the LDFLAGS are minimal (dl + pthread + m). The
// local-MinGW variant lives in embedder_local_windows.go.
package cache

/*
#cgo CFLAGS: -I${SRCDIR}/../rust-embedder
#cgo LDFLAGS: ${SRCDIR}/../rust-embedder/target/release/librust_embedder.a -ldl -lpthread -lm

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

const embedderDim = 384

type Embedder interface {
	GenerateEmbedding(text string) ([]float32, error)
	Close() error
}

var (
	GlobalEmbedder Embedder
	initOnce       sync.Once
	initErr        error
)

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
			initErr = fmt.Errorf("rust_embedder_init failed for model dir %q (check config.json, tokenizer.json, model.safetensors)", modelDir)
			return
		}
		GlobalEmbedder = &rustEmbedder{ctx: raw}
	})
	return initErr
}

func CloseGlobalEmbedder() {
	if GlobalEmbedder == nil {
		return
	}
	if err := GlobalEmbedder.Close(); err != nil {
		fmt.Printf("[cache] CloseGlobalEmbedder: %v\n", err)
	}
	GlobalEmbedder = nil
}

type rustEmbedder struct {
	ctx unsafe.Pointer
}

func (r *rustEmbedder) GenerateEmbedding(text string) ([]float32, error) {
	if r.ctx == nil {
		return nil, errors.New("rust embedder context is nil")
	}
	if text == "" {
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

func (r *rustEmbedder) Close() error {
	if r.ctx == nil {
		return nil
	}
	C.rust_embedder_free(r.ctx)
	r.ctx = nil
	return nil
}
