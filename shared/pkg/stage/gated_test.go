package stage

import (
	"context"
	"errors"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

type scoredInner struct {
	name string
	conf float64
	err  error
}

func (s scoredInner) Name() string { return s.name }
func (s scoredInner) Classify(context.Context, domain.Record) (domain.Classification, error) {
	if s.err != nil {
		return domain.Classification{}, s.err
	}
	return domain.Classification{Category: "c", Confidence: s.conf}, nil
}

func TestWithGateClearsGate(t *testing.T) {
	g := WithGate(scoredInner{name: "model", conf: 0.9}, Gate(0.7))
	c, err := g.Classify(context.Background(), domain.Record{})
	if err != nil || c.Category != "c" {
		t.Fatalf("Classify() = (%+v, %v), want the decision", c, err)
	}
	if g.Name() != "model" {
		t.Fatalf("Name() = %q, want the wrapped name", g.Name())
	}
}

func TestWithGateEscalatesBelowGate(t *testing.T) {
	g := WithGate(scoredInner{name: "model", conf: 0.3}, Gate(0.7))
	if _, err := g.Classify(context.Background(), domain.Record{}); !errors.Is(err, ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
	}
}

func TestWithGatePassesInnerError(t *testing.T) {
	boom := errors.New("boom")
	g := WithGate(scoredInner{name: "model", err: boom}, Gate(0))
	if _, err := g.Classify(context.Background(), domain.Record{}); !errors.Is(err, boom) {
		t.Fatalf("Classify() error = %v, want boom", err)
	}
}

func TestWithGatePanicsOnNilInner(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithGate(nil, ...) should panic")
		}
	}()
	WithGate(nil, Gate(0))
}

func TestWithGateEscalatesInnerUnclassified(t *testing.T) {
	g := WithGate(scoredInner{name: "model", err: ErrUnclassified}, Gate(0))
	if _, err := g.Classify(context.Background(), domain.Record{}); !errors.Is(err, ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
	}
}

type emptyCatInner struct{ conf float64 }

func (emptyCatInner) Name() string { return "bad" }
func (s emptyCatInner) Classify(context.Context, domain.Record) (domain.Classification, error) {
	return domain.Classification{Confidence: s.conf}, nil
}

func TestWithGatePassesInvalidResultThrough(t *testing.T) {
	g := WithGate(emptyCatInner{conf: 0.1}, Gate(0.7))
	c, err := g.Classify(context.Background(), domain.Record{})
	if err != nil {
		t.Fatalf("Classify() error = %v, want nil (invalid result passed through, not masked)", err)
	}
	if c.IsValid() {
		t.Fatalf("Classify() = %+v, want the invalid result passed through for the cascade to reject", c)
	}
}
