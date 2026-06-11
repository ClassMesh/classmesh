// Package source defines where Records come from. Anything that can yield
// Records — a text file, stdin, a CSV, a network stream — implements Source.
package source

import (
	"context"
	"errors"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// ErrDrained is returned by Next when the source has no more records.
var ErrDrained = errors.New("source: drained")

// Source yields Records until it is drained or closed.
type Source interface {
	// Next returns the next record. It returns ErrDrained once the source
	// is exhausted; any other error is a real failure.
	Next(ctx context.Context) (domain.Record, error)
	// Close releases underlying resources. Safe to call more than once.
	Close() error
}
