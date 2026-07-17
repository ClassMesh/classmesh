package classmesh

import (
	"context"
	"errors"
	"testing"
)

type invalidStage struct{}

func (invalidStage) Name() string { return "invalid" }
func (invalidStage) Classify(context.Context, Record) (Classification, error) {
	return Classification{Confidence: 1}, nil
}

func TestNewRejectsDuplicateStageNames(t *testing.T) {
	_, err := New(newStatic("dup", nil), newStatic("dup", nil))
	if err == nil || err.Error() != `classmesh: duplicate stage name "dup"` {
		t.Fatalf("New() error = %v, want duplicate stage name", err)
	}
}

func TestClassifyFailsFastOnInvalidResult(t *testing.T) {
	c, err := New(invalidStage{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = c.Classify(context.Background(), Record{})
	var se *StageError
	if !errors.As(err, &se) || se.Stage != "invalid" {
		t.Fatalf("Classify() error = %v, want Error for \"invalid\"", err)
	}
}
