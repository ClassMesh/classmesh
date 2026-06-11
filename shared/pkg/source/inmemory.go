package source

import (
	"context"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// InMemory is a Source backed by a fixed slice of records, for tests and
// wiring examples.
type InMemory struct {
	records []domain.Record
	pos     int
	closed  bool
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
	if s.closed || s.pos >= len(s.records) {
		return domain.Record{}, ErrDrained
	}
	r := s.records[s.pos]
	s.pos++
	return r, nil
}

// Close implements Source.
func (s *InMemory) Close() error {
	s.closed = true
	return nil
}
