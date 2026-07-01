// Package cascade is the one stage-cascade walk the engine and the classifier
// share: stages run in order, abstentions and below-gate decisions escalate, and
// the first confident decision (or Exhausted) is returned -- one place, no drift.
package cascade

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

// Result is the cascade-level outcome for a record. It is meaningful only when
// Run returns a nil error.
type Result int

const (
	// Exhausted means no stage decided the record: every stage abstained or
	// scored below the gate. The caller routes it to review or drops it.
	Exhausted Result = iota
	// Classified means a stage decided the record at or above the gate.
	Classified
)

// Run returns the first stage decision at or above the gate as Classified (Stage
// stamped), or Exhausted when none decides. ErrUnclassified escalates; any other
// stage error stops the walk as a bare *stage.Error for the caller to prefix. A
// non-nil logger gets a debug line per gate escalation.
func Run(ctx context.Context, stages []stage.Stage, gate stage.Gate, r domain.Record, logger *slog.Logger) (domain.Classification, Result, error) {
	for _, st := range stages {
		c, err := st.Classify(ctx, r)
		if errors.Is(err, stage.ErrUnclassified) {
			continue
		}
		if err != nil {
			return domain.Classification{}, Exhausted, &stage.Error{Stage: st.Name(), Err: err}
		}
		if err := stage.ValidateResult(st.Name(), c); err != nil {
			return domain.Classification{}, Exhausted, err
		}
		if !gate.Admits(c.Confidence) {
			if logger != nil && logger.Enabled(ctx, slog.LevelDebug) {
				logger.Debug("classification below confidence gate, escalating",
					"record", r.ID, "stage", st.Name(), "category", c.Category,
					"confidence", c.Confidence, "gate", gate)
			}
			continue
		}
		c.Stage = st.Name()
		return c, Classified, nil
	}
	return domain.Classification{}, Exhausted, nil
}
