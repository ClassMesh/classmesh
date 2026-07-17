package classmesh

import "context"

// gated wraps a Stage with its own confidence gate: a valid decision that
// clears the gate is returned unchanged, a valid one below it becomes
// ErrUnclassified so the cascade escalates. A malformed decision is passed
// through untouched so the cascade's contract check still rejects it rather than
// having it masked as an escalation.
type gated struct {
	inner Stage
	gate  Gate
}

var _ Stage = (*gated)(nil)

// WithGate wraps inner so a decision below gate escalates instead of winning.
func WithGate(inner Stage, gate Gate) Stage {
	if isNil(inner) {
		panic("stage.WithGate: nil inner stage")
	}
	return &gated{inner: inner, gate: gate}
}

// Name implements Stage, reporting the wrapped stage's name.
func (g *gated) Name() string { return g.inner.Name() }

// Classify runs the wrapped stage and escalates a below-gate decision.
func (g *gated) Classify(ctx context.Context, r Record) (Classification, error) {
	c, err := g.inner.Classify(ctx, r)
	if err != nil {
		return c, err
	}
	if c.IsValid() && !g.gate.admits(c.Confidence) {
		return Classification{}, ErrUnclassified
	}
	return c, nil
}
