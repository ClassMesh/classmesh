// Package sink defines where classified records go: stdout, files, review
// queues, downstream pipelines.
package sink

import (
	"context"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// Sink consumes records together with their classification.
type Sink interface {
	// Write delivers one classified record.
	Write(ctx context.Context, r domain.Record, c domain.Classification) error
	// Close flushes and releases underlying resources. Safe to call more
	// than once.
	Close() error
}
