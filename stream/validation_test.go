package stream

import (
	"context"
	"errors"
	"strings"
	"testing"

	domain "github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
	"github.com/ClassMesh/classmesh/stream/sink"
	"github.com/ClassMesh/classmesh/stream/source"
)

type invalidStage struct{}

func (invalidStage) Name() string { return "invalid" }
func (invalidStage) Classify(context.Context, domain.Record) (domain.Classification, error) {
	return domain.Classification{Confidence: 1}, nil
}

func TestNewRejectsDuplicateStageNames(t *testing.T) {
	_, err := newTestEngine(testOptions{
		Source: source.NewInMemory(nil),
		Stages: []domain.Stage{stage.NewStatic("dup", nil), stage.NewStatic("dup", nil)},
		Sink:   sink.NewInMemory(),
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate stage name") {
		t.Fatalf("New() error = %v, want duplicate stage name", err)
	}
}

func TestRunFailsFastOnInvalidClassification(t *testing.T) {
	e, err := newTestEngine(testOptions{
		Source: source.NewInMemory(records("anything")),
		Stages: []domain.Stage{invalidStage{}},
		Sink:   sink.NewInMemory(),
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = e.Run(context.Background())
	var se *domain.StageError
	if !errors.As(err, &se) || se.Stage != "invalid" {
		t.Fatalf("Run() error = %v, want domain.StageError for \"invalid\"", err)
	}
}
