package llm

import (
	"io"
	"time"
)

// defaultHTTPTimeout is a backstop on LLM HTTP calls. Completions can be slow,
// so it is generous — but a finite timeout prevents a hung TLS connection from
// blocking a caller forever when only a context.Background() was supplied.
const defaultHTTPTimeout = 5 * time.Minute

// maxResponseBytes caps how much of an HTTP response body the LLM clients read
// into memory, guarding against a malicious or malfunctioning endpoint
// returning an unbounded body (memory exhaustion).
const maxResponseBytes = 16 << 20 // 16 MiB

// limitedReadAll reads up to maxResponseBytes from r. It mirrors io.ReadAll but
// is bounded, so an oversized body is truncated rather than exhausting memory.
func limitedReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxResponseBytes))
}
