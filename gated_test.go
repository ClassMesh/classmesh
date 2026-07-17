package classmesh

import (
	"context"
	"errors"
	"testing"
)

type scoredInner struct {
	name string
	conf float64
	err  error
}

func (s scoredInner) Name() string { return s.name }
func (s scoredInner) Classify(context.Context, Record) (Classification, error) {
	if s.err != nil {
		return Classification{}, s.err
	}
	return Classification{Category: "c", Confidence: s.conf}, nil
}

func TestWithGateClearsGate(t *testing.T) {
	g := WithGate(scoredInner{name: "model", conf: 0.9}, mustGate(t, 0.7))
	c, err := g.Classify(context.Background(), Record{})
	if err != nil || c.Category != "c" {
		t.Fatalf("Classify() = (%+v, %v), want the decision", c, err)
	}
	if g.Name() != "model" {
		t.Fatalf("Name() = %q, want the wrapped name", g.Name())
	}
}

func TestWithGateEscalatesBelowGate(t *testing.T) {
	g := WithGate(scoredInner{name: "model", conf: 0.3}, mustGate(t, 0.7))
	if _, err := g.Classify(context.Background(), Record{}); !errors.Is(err, ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
	}
}

func TestWithGatePassesInnerError(t *testing.T) {
	boom := errors.New("boom")
	g := WithGate(scoredInner{name: "model", err: boom}, Gate{})
	if _, err := g.Classify(context.Background(), Record{}); !errors.Is(err, boom) {
		t.Fatalf("Classify() error = %v, want boom", err)
	}
}

func TestWithGatePanicsOnNilInner(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithGate(nil, ...) should panic")
		}
	}()
	WithGate(nil, Gate{})
}

func TestWithGateEscalatesInnerUnclassified(t *testing.T) {
	g := WithGate(scoredInner{name: "model", err: ErrUnclassified}, Gate{})
	if _, err := g.Classify(context.Background(), Record{}); !errors.Is(err, ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
	}
}

type emptyCatInner struct{ conf float64 }

func (emptyCatInner) Name() string { return "bad" }
func (s emptyCatInner) Classify(context.Context, Record) (Classification, error) {
	return Classification{Confidence: s.conf}, nil
}

func TestWithGatePassesInvalidResultThrough(t *testing.T) {
	g := WithGate(emptyCatInner{conf: 0.1}, mustGate(t, 0.7))
	c, err := g.Classify(context.Background(), Record{})
	if err != nil {
		t.Fatalf("Classify() error = %v, want nil (invalid result passed through, not masked)", err)
	}
	if c.IsValid() {
		t.Fatalf("Classify() = %+v, want the invalid result passed through for the cascade to reject", c)
	}
}
