package stage

import (
	"context"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// Gated wraps a Stage with its own confidence gate: a valid decision that
// clears the gate is returned unchanged, a valid one below it becomes
// ErrUnclassified so the cascade escalates. A malformed decision is passed
// through untouched so the cascade's contract check still rejects it rather than
// having it masked as an escalation.
type Gated struct {
	inner Stage
	gate  Gate
}

var _ Stage = (*Gated)(nil)

// WithGate wraps inner so a decision below gate escalates instead of winning.
func WithGate(inner Stage, gate Gate) *Gated {
	if isNil(inner) {
		panic("stage.WithGate: nil inner stage")
	}
	return &Gated{inner: inner, gate: gate}
}

// Name implements Stage, reporting the wrapped stage's name.
func (g *Gated) Name() string { return g.inner.Name() }

// Classify runs the wrapped stage and escalates a below-gate decision.
func (g *Gated) Classify(ctx context.Context, r domain.Record) (domain.Classification, error) {
	c, err := g.inner.Classify(ctx, r)
	if err != nil {
		return c, err
	}
	if c.IsValid() && !g.gate.Admits(c.Confidence) {
		return domain.Classification{}, ErrUnclassified
	}
	return c, nil
}
