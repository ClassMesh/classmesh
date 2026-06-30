package stage

import (
	"fmt"
	"reflect"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// ValidateNames rejects a cascade whose stages are not individually
// identifiable: every Name must be non-empty and unique, so stats, logs, and
// per-stage policy can key on it unambiguously. A nil stage, including a
// typed-nil pointer, is rejected rather than dereferenced.
func ValidateNames(stages []Stage) error {
	seen := make(map[string]struct{}, len(stages))
	for i, s := range stages {
		if isNil(s) {
			return fmt.Errorf("stage %d is nil", i)
		}
		name := s.Name()
		if name == "" {
			return fmt.Errorf("stage name must not be empty")
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate stage name %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// ValidateResult guards the boundary between a stage and the cascade: a stage
// that returns no error is promising a well-formed Classification. When it is
// not (empty category, or confidence outside [0, 1]), it is reported as a stage
// Error so the cascade fails fast instead of emitting a malformed decision.
func ValidateResult(name string, c domain.Classification) error {
	if c.IsValid() {
		return nil
	}
	return &Error{Stage: name, Err: fmt.Errorf("invalid classification: category=%q confidence=%v", c.Category, c.Confidence)}
}

// isNil reports whether s is a nil interface or holds a nil dynamic value (a
// typed-nil pointer, map, slice, channel, or function), either of which would
// panic when a method runs.
func isNil(s Stage) bool {
	if s == nil {
		return true
	}
	switch v := reflect.ValueOf(s); v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return v.IsNil()
	default:
		return false
	}
}
