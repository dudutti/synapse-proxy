// Package mcp — stdio transport.
//
// MCP clients (Claude Code, Cursor, Continue, etc.) launch the
// proxy as a subprocess and pipe JSON-RPC 2.0 messages over
// stdin/stdout. This transport reads line-delimited JSON from
// stdin, dispatches each line to the server, and writes the
// response (one JSON object per line) to stdout.
//
// No logging to stdout: that would corrupt the JSON-RPC stream.
// All logs go to stderr.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ServeStdio runs the MCP server on stdin/stdout until EOF or
// context cancellation. This is the canonical entry point used by
// Claude Code, Cursor, Continue, and any other MCP client.
func ServeStdio(ctx context.Context, s *Server) error {
	// Line-delimited JSON-RPC 2.0: one JSON object per line.
	// Stdin -> server, stdout -> client. Logs go to stderr.
	in := make(chan []byte, 32)
	out := make(chan []byte, 32)
	errCh := make(chan error, 2)

	// Reader goroutine: read stdin line by line and push raw bytes
	// to the in channel. The server reads raw bytes and unmarshals
	// them; we keep this layer transport-agnostic.
	go func() {
		reader := bufio.NewReaderSize(os.Stdin, 64*1024)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				// Strip the trailing newline so the JSON
				// parser is happy, then push.
				if line[len(line)-1] == '\n' {
					line = line[:len(line)-1]
				}
				if len(line) > 0 {
					select {
					case in <- line:
					case <-ctx.Done():
						return
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					errCh <- err
				}
				close(in)
				return
			}
		}
	}()

	// Writer goroutine: take JSON-RPC responses from the out
	// channel and write them to stdout with a trailing newline.
	go func() {
		writer := bufio.NewWriterSize(os.Stdout, 64*1024)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-out:
				if !ok {
					return
				}
				if _, err := writer.Write(msg); err != nil {
					errCh <- err
					return
				}
				if _, err := writer.Write([]byte("\n")); err != nil {
					errCh <- err
					return
				}
				if err := writer.Flush(); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	// Server loop. Run until stdin is closed (EOF) or context is
	// cancelled. Any error from the reader or writer goroutine
	// propagates up.
	go func() {
		errCh <- s.Serve(ctx, in, out)
		close(out)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// Debugf writes a debug line to stderr. MCP transports reserve
// stdout for the JSON-RPC stream, so logs must go to stderr.
func Debugf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[mcp] "+format+"\n", args...)
}

// jsonLine is a small helper to pretty-print a response to stderr
// during local debugging. Not used in production.
func jsonLine(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
