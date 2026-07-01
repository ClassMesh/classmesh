package stage

import (
	"context"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// Named wraps a Stage to report a different name, so one stage type can appear
// more than once in a cascade under distinct config-declared ids without
// colliding on the name that stats, logs, and validation key on.
type Named struct {
	inner Stage
	name  string
}

var _ Stage = (*Named)(nil)

// WithName wraps inner so Name reports name.
func WithName(inner Stage, name string) *Named {
	if isNil(inner) {
		panic("stage.WithName: nil inner stage")
	}
	return &Named{inner: inner, name: name}
}

// Name implements Stage.
func (n *Named) Name() string { return n.name }

// Classify implements Stage, delegating to the wrapped stage.
func (n *Named) Classify(ctx context.Context, r domain.Record) (domain.Classification, error) {
	return n.inner.Classify(ctx, r)
}
