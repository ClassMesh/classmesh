package classifier

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

func rec(s string) domain.Record { return domain.Record{ID: s, Data: []byte(s)} }

func TestClassifyReturnsFirstConfidentDecision(t *testing.T) {
	c, err := New(Deps{Stages: []stage.Stage{
		stage.NewStatic("rules", map[string]string{"ping": "noise"}),
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got, err := c.Classify(context.Background(), rec("ping"))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if got.Category != "noise" || got.Stage != "rules" || got.Confidence != 1 {
		t.Fatalf("Classify() = %+v, want noise/rules/1", got)
	}
}

func TestClassifyEscalatesPastUnclassifiedStages(t *testing.T) {
	c, err := New(Deps{Stages: []stage.Stage{
		stage.NewStatic("first", nil),
		stage.NewStatic("second", map[string]string{"x": "hit"}),
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got, err := c.Classify(context.Background(), rec("x"))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if got.Category != "hit" || got.Stage != "second" {
		t.Fatalf("Classify() = %+v, want hit/second", got)
	}
}

func TestClassifyUnclassifiedWhenNoStageDecides(t *testing.T) {
	c, err := New(Deps{Stages: []stage.Stage{stage.NewStatic("rules", nil)}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.Classify(context.Background(), rec("anything")); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want stage.ErrUnclassified", err)
	}
}

func TestClassifyGateEscalatesLowConfidence(t *testing.T) {
	c, err := New(Deps{
		Stages:        []stage.Stage{scored{name: "model", conf: 0.4}},
		MinConfidence: 0.7,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.Classify(context.Background(), rec("x")); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want stage.ErrUnclassified (gated)", err)
	}
}

func TestClassifyGatedThenConfident(t *testing.T) {
	c, err := New(Deps{
		Stages:        []stage.Stage{scored{name: "a", conf: 0.4}, scored{name: "b", conf: 0.9}},
		MinConfidence: 0.7,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got, err := c.Classify(context.Background(), rec("x"))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if got.Stage != "b" {
		t.Fatalf("Classify() = %+v, want the confident stage b", got)
	}
}

func TestClassifyPropagatesStageError(t *testing.T) {
	boom := errors.New("boom")
	c, err := New(Deps{Stages: []stage.Stage{failing{err: boom}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = c.Classify(context.Background(), rec("x"))
	if !errors.Is(err, boom) {
		t.Fatalf("Classify() error = %v, want wrapped boom", err)
	}
	if !strings.Contains(err.Error(), "failing") {
		t.Fatalf("Classify() error = %v, want stage name in message", err)
	}
	var se *stage.Error
	if !errors.As(err, &se) || se.Stage != "failing" {
		t.Fatalf("Classify() error = %v, want *stage.Error with Stage=failing", err)
	}
}

func TestClassifyHonorsContextCancellation(t *testing.T) {
	c, err := New(Deps{Stages: []stage.Stage{stage.NewStatic("rules", nil)}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Classify(ctx, rec("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Classify() error = %v, want context.Canceled", err)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(Deps{}); err == nil {
		t.Fatal("New() with no stages error = nil, want error")
	}
	for _, bad := range []float64{-0.1, 1.5} {
		_, err := New(Deps{Stages: []stage.Stage{stage.NewStatic("s", nil)}, MinConfidence: bad})
		if err == nil {
			t.Fatalf("New(MinConfidence=%v) error = nil, want error", bad)
		}
	}
}

type scored struct {
	name string
	conf float64
}

func (s scored) Name() string { return s.name }
func (s scored) Classify(ctx context.Context, _ domain.Record) (domain.Classification, error) {
	if err := ctx.Err(); err != nil {
		return domain.Classification{}, err
	}
	return domain.Classification{Category: "cat-" + s.name, Confidence: s.conf, Stage: s.name}, nil
}

type failing struct{ err error }

func (f failing) Name() string { return "failing" }
func (f failing) Classify(_ context.Context, _ domain.Record) (domain.Classification, error) {
	return domain.Classification{}, f.err
}
