package source

import (
	"context"
	"sync/atomic"

	domain "github.com/ClassMesh/classmesh"
)

// InMemory is a Source backed by a fixed slice of records, for tests and
// wiring examples.
type InMemory struct {
	records []domain.Record
	pos     int
	closed  atomic.Bool
}

var _ Source = (*InMemory)(nil)

// NewInMemory returns a Source that yields the given records in order.
func NewInMemory(records []domain.Record) *InMemory {
	return &InMemory{records: records}
}

// Next implements Source.
func (s *InMemory) Next(ctx context.Context) (domain.Record, error) {
	if err := ctx.Err(); err != nil {
		return domain.Record{}, err
	}
	if s.closed.Load() || s.pos >= len(s.records) {
		return domain.Record{}, ErrDrained
	}
	r := s.records[s.pos]
	s.pos++
	return r, nil
}

// Close implements Source. Safe to call concurrently with Next, matching
// the real sources: the engine closes a source to unwind a blocked read.
func (s *InMemory) Close() error {
	s.closed.Store(true)
	return nil
}
