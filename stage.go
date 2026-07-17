package classmesh

import (
	"context"
	"errors"
)

// ErrUnclassified is returned by Classify when the stage cannot decide on a
// category for the record. The cascade then hands the record to the next
// stage; a record unclassified by every stage is routed for review.
var ErrUnclassified = errors.New("stage: unclassified")

// Stage classifies records. Implementations range from deterministic rule
// matching to in-process model inference to remote API calls.
type Stage interface {
	// Name identifies the stage in classifications, logs, and metrics.
	Name() string
	// Classify assigns a category to the record, or returns
	// ErrUnclassified when this stage cannot decide. It must not mutate
	// the record, which a concurrent engine shares with later consumers.
	Classify(ctx context.Context, r Record) (Classification, error)
}
