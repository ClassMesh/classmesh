package classifier

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

type invalidStage struct{}

func (invalidStage) Name() string { return "invalid" }
func (invalidStage) Classify(context.Context, domain.Record) (domain.Classification, error) {
	return domain.Classification{Confidence: 1}, nil
}

func TestNewRejectsDuplicateStageNames(t *testing.T) {
	_, err := New(Deps{Stages: []stage.Stage{stage.NewStatic("dup", nil), stage.NewStatic("dup", nil)}})
	if err == nil || !strings.Contains(err.Error(), "duplicate stage name") {
		t.Fatalf("New() error = %v, want duplicate stage name", err)
	}
}

func TestClassifyFailsFastOnInvalidResult(t *testing.T) {
	c, err := New(Deps{Stages: []stage.Stage{invalidStage{}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = c.Classify(context.Background(), domain.Record{})
	var se *stage.Error
	if !errors.As(err, &se) || se.Stage != "invalid" {
		t.Fatalf("Classify() error = %v, want stage.Error for \"invalid\"", err)
	}
}
