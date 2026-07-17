package classmesh

import "context"

// named wraps a Stage to report a different name, so one stage type can appear
// more than once in a cascade under distinct config-declared ids without
// colliding on the name that stats, logs, and validation key on.
type named struct {
	inner Stage
	name  string
}

var _ Stage = (*named)(nil)

// WithName wraps inner so Name reports name.
func WithName(inner Stage, name string) Stage {
	if isNil(inner) {
		panic("stage.WithName: nil inner stage")
	}
	return &named{inner: inner, name: name}
}

// Name implements Stage.
func (n *named) Name() string { return n.name }

// Classify implements Stage, delegating to the wrapped stage.
func (n *named) Classify(ctx context.Context, r Record) (Classification, error) {
	return n.inner.Classify(ctx, r)
}
