// Package stage keeps the old engine and CLI compiling during migration.
package stage

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/ClassMesh/classmesh"
)

type Stage = classmesh.Stage
type Error = classmesh.StageError
type Gate = classmesh.Gate

var ErrUnclassified = classmesh.ErrUnclassified

func NewGate(min float64) (Gate, error) {
	return classmesh.NewGate(min)
}

func ValidateNames(stages []Stage) error {
	seen := make(map[string]struct{}, len(stages))
	for i, current := range stages {
		if nilStage(current) {
			return fmt.Errorf("stage %d is nil", i)
		}
		name := current.Name()
		if name == "" {
			return errors.New("stage name must not be empty")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate stage name %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func nilStage(stage Stage) bool {
	if stage == nil {
		return true
	}
	value := reflect.ValueOf(stage)
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return value.IsNil()
	default:
		return false
	}
}
