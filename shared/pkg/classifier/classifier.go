// Package classifier runs records through an ordered stage cascade, the
// library counterpart to the streaming engine. Build one with New and call
// Classify for a single record or ClassifyBatch for a slice; reach for engine
// when you want to drain a source into a sink instead.
package classifier

import (
	"context"
	"errors"
	"fmt"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

// Deps bundles what a Classifier needs.
type Deps struct {
	// Stages is the cascade, cheapest first. At least one is required.
	Stages []stage.Stage
	// MinConfidence gates a decision: anything below it escalates to the
	// next stage. Zero turns gating off.
	MinConfidence float64
}

// Classifier decides a single record by walking the cascade.
type Classifier struct {
	stages []stage.Stage
	gate   stage.Gate
}

// New checks deps and returns a ready Classifier.
func New(d Deps) (*Classifier, error) {
	if len(d.Stages) == 0 {
		return nil, errors.New("classifier: at least one stage is required")
	}
	gate, err := stage.NewGate(d.MinConfidence)
	if err != nil {
		return nil, fmt.Errorf("classifier: %w", err)
	}
	return &Classifier{stages: append([]stage.Stage(nil), d.Stages...), gate: gate}, nil
}

// Classify walks the cascade and returns the first decision at or above the
// confidence gate. It returns stage.ErrUnclassified when no stage decides, or
// when every decision falls below the gate. Any other stage error stops the
// walk and comes back wrapped.
func (c *Classifier) Classify(ctx context.Context, r domain.Record) (domain.Classification, error) {
	if err := ctx.Err(); err != nil {
		return domain.Classification{}, err
	}
	for _, st := range c.stages {
		cl, err := st.Classify(ctx, r)
		if errors.Is(err, stage.ErrUnclassified) {
			continue
		}
		if err != nil {
			return domain.Classification{}, fmt.Errorf("classifier: %w", &stage.Error{Stage: st.Name(), Err: err})
		}
		if !c.gate.Admits(cl.Confidence) {
			continue
		}
		cl.Stage = st.Name()
		return cl, nil
	}
	return domain.Classification{}, stage.ErrUnclassified
}

// Result pairs a record's Classification with the error from classifying it:
// nil on success, stage.ErrUnclassified when no stage decided, or a wrapped
// stage error.
type Result struct {
	Classification domain.Classification
	Err            error
}

// ClassifyBatch classifies records and returns one Result per input, in the
// same order, reusing the same cascade and gate as Classify. It is
// synchronous; running records concurrently is a later optimization. On a
// cancelled context it does not stop early: every remaining record gets a
// Result whose Err is the context error, so len(results) always equals
// len(records).
func (c *Classifier) ClassifyBatch(ctx context.Context, records []domain.Record) []Result {
	results := make([]Result, len(records))
	for i, r := range records {
		cl, err := c.Classify(ctx, r)
		results[i] = Result{Classification: cl, Err: err}
	}
	return results
}
