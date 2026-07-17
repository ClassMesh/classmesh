// Package text implements source.Source over line-oriented text: files
// and stdin. Every line becomes one Record, losslessly, blank lines
// included, so downstream stages see exactly what the input contained.
package text

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync/atomic"

	domain "github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/stream/source"
)

// maxLineBytes bounds a single line. Log lines routinely exceed bufio's
// 64KiB default (stack traces, embedded JSON), so allow up to 1MiB.
const maxLineBytes = 1 << 20

// Source yields one Record per line of the underlying reader.
type Source struct {
	rc      io.Closer
	scanner *bufio.Scanner
	name    string
	line    int
	idBuf   []byte
	closed  atomic.Bool
}

var _ source.Source = (*Source)(nil)

// New returns a Source reading lines from r. name labels the stream in
// record IDs (e.g. "stdin", a file path). If r is an io.Closer it is closed
// by Close.
func New(r io.Reader, name string) *Source {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	s := &Source{scanner: sc, name: name}
	if c, ok := r.(io.Closer); ok {
		s.rc = c
	}
	return s
}

// Open returns a Source reading lines from the file at path.
func Open(path string) (*Source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("textfile: %w", err)
	}
	return New(f, path), nil
}

// Next implements source.Source. Line numbers are 1-based.
func (s *Source) Next(ctx context.Context) (domain.Record, error) {
	if err := ctx.Err(); err != nil {
		return domain.Record{}, err
	}
	if s.closed.Load() {
		return domain.Record{}, source.ErrDrained
	}
	if !s.scanner.Scan() {
		if s.closed.Load() {
			if err := ctx.Err(); err != nil {
				return domain.Record{}, err
			}
			return domain.Record{}, source.ErrDrained
		}
		if err := s.scanner.Err(); err != nil {
			return domain.Record{}, fmt.Errorf("textfile: %s: %w", s.name, err)
		}
		return domain.Record{}, source.ErrDrained
	}
	s.line++
	data := make([]byte, len(s.scanner.Bytes()))
	copy(data, s.scanner.Bytes())
	return domain.Record{
		ID:   s.recordID(),
		Kind: domain.KindText,
		Data: data,
	}, nil
}

// recordID builds "name:line" through a reused scratch buffer, so the ID is
// the only string allocated per record. The ID is the record's provenance;
// no Meta map is attached.
func (s *Source) recordID() string {
	s.idBuf = append(s.idBuf[:0], s.name...)
	s.idBuf = append(s.idBuf, ':')
	s.idBuf = strconv.AppendInt(s.idBuf, int64(s.line), 10)
	return string(s.idBuf)
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
			return fmt.Errorf("textfile: close %s: %w", s.name, err)
		}
	}
	return nil
}
