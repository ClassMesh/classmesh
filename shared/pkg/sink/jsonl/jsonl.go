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

// Entry is the wire shape of one output line. Kind, Fields and Reasons appear
// only when present (and Kind only for structured payloads, not plain text),
// so a plain log record serializes exactly as before.
type Entry struct {
	ID         string            `json:"id"`
	Kind       domain.Kind       `json:"kind,omitempty"`
	Data       string            `json:"data"`
	Fields     map[string]any    `json:"fields,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
	Category   string            `json:"category"`
	Confidence float64           `json:"confidence"`
	Stage      string            `json:"stage,omitempty"`
	Reasons    []domain.Reason   `json:"reasons,omitempty"`
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
	// Plain text is the baseline payload, so omit it: kind annotates
	// structured records (json, event, ...) and a log line stays as it was.
	kind := r.Kind
	if kind == domain.KindText {
		kind = ""
	}
	e := Entry{
		ID:         r.ID,
		Kind:       kind,
		Data:       string(r.Data),
		Fields:     r.Fields,
		Meta:       r.Meta,
		Category:   c.Category,
		Confidence: c.Confidence,
		Stage:      c.Stage,
		Reasons:    c.Reasons,
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
