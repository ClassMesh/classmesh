package classmesh

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"testing"
)

type stub struct {
	name string
	c    Classification
	err  error
}

func (s stub) Name() string { return s.name }
func (s stub) Classify(context.Context, Record) (Classification, error) {
	return s.c, s.err
}

func decides(name, category string, confidence float64) stub {
	return stub{name: name, c: Classification{Category: category, Confidence: confidence}}
}

type ctxStage struct{ name string }

func (s ctxStage) Name() string { return s.name }
func (s ctxStage) Classify(ctx context.Context, _ Record) (Classification, error) {
	if err := ctx.Err(); err != nil {
		return Classification{}, err
	}
	return Classification{Category: "ok", Confidence: 1}, nil
}

func newWalk(t testing.TB, stages []Stage, minConfidence float64, logger *slog.Logger) *Cascade {
	t.Helper()
	cascade, err := NewWithOptions(Options{Stages: stages, MinConfidence: minConfidence, Logger: logger})
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}
	return cascade
}

func TestWalkClassifiesAndStampsStage(t *testing.T) {
	cascade := newWalk(t, []Stage{decides("rules", "noise", 1)}, 0, nil)
	classification, err := cascade.Classify(context.Background(), Record{})
	if err != nil {
		t.Fatalf("Classify() error = %v, want nil", err)
	}
	if classification.Category != "noise" || classification.Stage != "rules" {
		t.Fatalf("Classify() = %+v, want noise stamped rules", classification)
	}
}

func TestWalkExhaustedWhenAllAbstain(t *testing.T) {
	cascade := newWalk(t, []Stage{stub{name: "a", err: ErrUnclassified}}, 0, nil)
	if _, err := cascade.Classify(context.Background(), Record{}); !errors.Is(err, ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
	}
}

func TestWalkGateEscalatesToNextStage(t *testing.T) {
	cascade := newWalk(t, []Stage{decides("low", "x", 0.3), decides("high", "y", 0.9)}, 0.7, nil)
	classification, err := cascade.Classify(context.Background(), Record{})
	if err != nil || classification.Stage != "high" {
		t.Fatalf("Classify() = (%+v, %v), want high classified", classification, err)
	}
}

func TestWalkExhaustedWhenAllBelowGate(t *testing.T) {
	cascade := newWalk(t, []Stage{decides("low", "x", 0.3)}, 0.7, nil)
	if _, err := cascade.Classify(context.Background(), Record{}); !errors.Is(err, ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
	}
}

func TestWalkStageErrorStopsAndWraps(t *testing.T) {
	boom := errors.New("boom")
	cascade := newWalk(t, []Stage{stub{name: "bad", err: boom}}, 0, nil)
	_, err := cascade.Classify(context.Background(), Record{})
	var stageErr *StageError
	if !errors.As(err, &stageErr) || stageErr.Stage != "bad" || !errors.Is(err, boom) {
		t.Fatalf("Classify() error = %v, want *StageError(bad) wrapping boom", err)
	}
}

func TestWalkLogsGateEscalation(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cascade := newWalk(t, []Stage{decides("low", "x", 0.3)}, 0.7, logger)
	_, err := cascade.Classify(context.Background(), Record{ID: "r1"})
	if !errors.Is(err, ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
	}
	out := buf.String()
	for _, want := range []string{"below confidence gate", "record=r1", "stage=low", "confidence=0.3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("debug log = %q, missing %q", out, want)
		}
	}
}

func TestWalkPreflightCancellationIsBare(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cascade := newWalk(t, []Stage{ctxStage{name: "ctxs"}}, 0, nil)
	_, err := cascade.Classify(ctx, Record{})
	var stageErr *StageError
	if !errors.Is(err, context.Canceled) || errors.As(err, &stageErr) {
		t.Fatalf("Classify() error = %v, want bare context.Canceled", err)
	}
}

func TestWalkPerStageGateEscalates(t *testing.T) {
	low := WithGate(decides("low", "x", 0.3), mustGate(t, 0.7))
	cascade := newWalk(t, []Stage{low, decides("high", "y", 0.9)}, 0, nil)
	classification, err := cascade.Classify(context.Background(), Record{})
	if err != nil || classification.Stage != "high" {
		t.Fatalf("Classify() = (%+v, %v), want high classified after the per-stage gate", classification, err)
	}
}

func TestWalkPerStageGateStillValidates(t *testing.T) {
	bad := WithGate(stub{name: "bad", c: Classification{Confidence: 0.3}}, mustGate(t, 0.7))
	cascade := newWalk(t, []Stage{bad}, 0, nil)
	_, err := cascade.Classify(context.Background(), Record{})
	var stageErr *StageError
	if !errors.As(err, &stageErr) || stageErr.Stage != "bad" {
		t.Fatalf("Classify() error = %v, want *StageError(bad) for invalid output", err)
	}
}

func TestWalkRejectsInvalidResult(t *testing.T) {
	bad := stub{name: "bad", c: Classification{Confidence: math.NaN()}}
	cascade := newWalk(t, []Stage{bad}, 0, nil)
	_, err := cascade.Classify(context.Background(), Record{})
	var stageErr *StageError
	if !errors.As(err, &stageErr) || stageErr.Stage != "bad" {
		t.Fatalf("Classify() error = %v, want *StageError(bad) for invalid result", err)
	}
}
