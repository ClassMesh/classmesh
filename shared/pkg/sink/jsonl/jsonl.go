// Package jsonl implements sink.Sink as JSON Lines: one JSON object per
// classified record, the pipe-friendly lingua franca of log tooling.
package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
)

// Entry is the wire shape of one output line.
type Entry struct {
	ID         string            `json:"id"`
	Data       string            `json:"data"`
	Meta       map[string]string `json:"meta,omitempty"`
	Category   string            `json:"category"`
	Confidence float64           `json:"confidence"`
	Stage      string            `json:"stage,omitempty"`
}

// Sink writes one JSON object per record to an io.Writer. Output is
// buffered; Close flushes. The underlying writer is not closed — the caller
// owns it (it is usually stdout).
type Sink struct {
	bw  *bufio.Writer
	enc *json.Encoder
}

var _ sink.Sink = (*Sink)(nil)

// New returns a Sink writing JSON Lines to w.
func New(w io.Writer) *Sink {
	bw := bufio.NewWriter(w)
	return &Sink{bw: bw, enc: json.NewEncoder(bw)}
}

// Write implements sink.Sink.
func (s *Sink) Write(ctx context.Context, r domain.Record, c domain.Classification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e := Entry{
		ID:         r.ID,
		Data:       string(r.Data),
		Meta:       r.Meta,
		Category:   c.Category,
		Confidence: c.Confidence,
		Stage:      c.Stage,
	}
	if err := s.enc.Encode(e); err != nil {
		return fmt.Errorf("jsonl: encode %s: %w", r.ID, err)
	}
	return nil
}

// Close implements sink.Sink: flushes buffered output. Safe to call more
// than once.
func (s *Sink) Close() error {
	if err := s.bw.Flush(); err != nil {
		return fmt.Errorf("jsonl: flush: %w", err)
	}
	return nil
}
