package sink

import (
	"context"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// Entry is one record/classification pair captured by InMemory.
type Entry struct {
	Record         domain.Record
	Classification domain.Classification
}

// InMemory is a Sink that collects everything written to it, for tests and
// wiring examples.
type InMemory struct {
	entries []Entry
	closed  bool
}

var _ Sink = (*InMemory)(nil)

// NewInMemory returns an empty collecting Sink.
func NewInMemory() *InMemory {
	return &InMemory{}
}

// Write implements Sink.
func (s *InMemory) Write(ctx context.Context, r domain.Record, c domain.Classification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.entries = append(s.entries, Entry{Record: r, Classification: c})
	return nil
}

// Close implements Sink.
func (s *InMemory) Close() error {
	s.closed = true
	return nil
}

// Entries returns a copy of everything written so far, in order, so callers
// cannot mutate the sink's state.
func (s *InMemory) Entries() []Entry {
	return append([]Entry(nil), s.entries...)
}
