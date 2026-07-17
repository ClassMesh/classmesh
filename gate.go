package classmesh

import (
	"fmt"
	"math"
)

// Gate is a minimum confidence. A decision scoring below it is treated as
// undecided, so the cascade escalates to the next stage. The zero Gate admits
// everything, which turns gating off; deterministic stages emit 1, so they
// always pass.
type Gate struct {
	min float64
}

// NewGate returns a Gate for min, or an error if min is outside [0, 1].
func NewGate(min float64) (Gate, error) {
	if math.IsNaN(min) || min < 0 || min > 1 {
		return Gate{}, fmt.Errorf("min confidence must be within [0, 1], got %v", min)
	}
	return Gate{min: min}, nil
}

func (g Gate) admits(confidence float64) bool {
	return confidence >= g.min
}
