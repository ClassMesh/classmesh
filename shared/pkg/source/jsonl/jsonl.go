// Package jsonl implements source.Source over JSON Lines: one JSON object per
// line. Each line becomes a Record whose Data is the original bytes and whose
// Fields is the decoded object, tagged domain.KindJSON. Numbers are decoded as
// json.Number so large integer IDs keep their exact value instead of rounding
// through float64. Blank lines are skipped; a non-blank line that is not a
// single JSON object is a clear error.
package jsonl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
)

// maxLineBytes bounds a single line, matching the textfile source: JSON event
// lines can be large (nested payloads), so allow up to 1MiB.
const maxLineBytes = 1 << 20

// Source yields one Record per JSON object in the underlying reader.
type Source struct {
	rc      io.Closer
	scanner *bufio.Scanner
	name    string
	line    int
	closed  atomic.Bool
}

var _ source.Source = (*Source)(nil)

// New returns a Source reading JSON Lines from r. name labels the stream in
// record IDs and metadata (e.g. "stdin", a file path). If r is an io.Closer it
// is closed by Close.
func New(r io.Reader, name string) *Source {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	s := &Source{scanner: sc, name: name}
	if c, ok := r.(io.Closer); ok {
		s.rc = c
	}
	return s
}

// Open returns a Source reading JSON Lines from the file at path.
func Open(path string) (*Source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("jsonl: %w", err)
	}
	return New(f, path), nil
}

// Next implements source.Source. Line numbers are 1-based and count blank
// lines, so they line up with the file even though blanks are skipped.
func (s *Source) Next(ctx context.Context) (domain.Record, error) {
	if err := ctx.Err(); err != nil {
		return domain.Record{}, err
	}
	if s.closed.Load() {
		return domain.Record{}, source.ErrDrained
	}
	for s.scanner.Scan() {
		s.line++
		raw := s.scanner.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		line := strconv.Itoa(s.line)
		data := make([]byte, len(raw))
		copy(data, raw)
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		var fields map[string]any
		if err := dec.Decode(&fields); err != nil {
			return domain.Record{}, fmt.Errorf("jsonl: %s:%s: %w", s.name, line, err)
		}
		if fields == nil {
			return domain.Record{}, fmt.Errorf("jsonl: %s:%s: line is not a JSON object", s.name, line)
		}
		if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
			return domain.Record{}, fmt.Errorf("jsonl: %s:%s: unexpected data after JSON object", s.name, line)
		}
		return domain.Record{
			ID:     s.name + ":" + line,
			Kind:   domain.KindJSON,
			Data:   data,
			Fields: fields,
			Meta:   map[string]string{"source": s.name, "line": line},
		}, nil
	}
	if s.closed.Load() {
		if err := ctx.Err(); err != nil {
			return domain.Record{}, err
		}
		return domain.Record{}, source.ErrDrained
	}
	if err := s.scanner.Err(); err != nil {
		return domain.Record{}, fmt.Errorf("jsonl: %s: %w", s.name, err)
	}
	return domain.Record{}, source.ErrDrained
}

// Close implements source.Source. Safe to call more than once, and
// concurrently with Next: closing the reader unblocks a pending read so a
// cancelled run can stop instead of hanging on stdin or a pipe.
func (s *Source) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	if s.rc != nil {
		if err := s.rc.Close(); err != nil {
			return fmt.Errorf("jsonl: close %s: %w", s.name, err)
		}
	}
	return nil
}
