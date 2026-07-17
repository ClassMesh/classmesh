// Package source defines where Records come from. Anything that can yield
// Records (a text file, stdin, a CSV, a network stream) implements Source.
package source

import (
	"context"
	"errors"

	domain "github.com/ClassMesh/classmesh"
)

// ErrDrained is returned by Next when the source has no more records.
var ErrDrained = errors.New("source: drained")

// Source yields Records until it is drained or closed.
type Source interface {
	// Next returns the next record. It returns ErrDrained once the source
	// is exhausted, and any other error is a real failure. Returned records
	// must stay valid and unmodified across later Next calls, so a source
	// must not reuse buffers between records. The built-ins copy.
	Next(ctx context.Context) (domain.Record, error)
	// Close releases underlying resources. It is safe to call more than
	// once and concurrently with Next, and it must interrupt a blocked
	// Next. The engine closes a source to unwind a pending read.
	Close() error
}
