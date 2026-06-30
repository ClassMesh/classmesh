package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

type invalidStage struct{}

func (invalidStage) Name() string { return "invalid" }
func (invalidStage) Classify(context.Context, domain.Record) (domain.Classification, error) {
	return domain.Classification{Confidence: 1}, nil
}

func TestNewRejectsDuplicateStageNames(t *testing.T) {
	_, err := New(Deps{
		Source: source.NewInMemory(nil),
		Stages: []stage.Stage{stage.NewStatic("dup", nil), stage.NewStatic("dup", nil)},
		Sink:   sink.NewInMemory(),
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate stage name") {
		t.Fatalf("New() error = %v, want duplicate stage name", err)
	}
}

func TestRunFailsFastOnInvalidClassification(t *testing.T) {
	e, err := New(Deps{
		Source: source.NewInMemory(records("anything")),
		Stages: []stage.Stage{invalidStage{}},
		Sink:   sink.NewInMemory(),
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = e.Run(context.Background())
	var se *stage.Error
	if !errors.As(err, &se) || se.Stage != "invalid" {
		t.Fatalf("Run() error = %v, want stage.Error for \"invalid\"", err)
	}
}
