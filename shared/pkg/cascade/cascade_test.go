package cascade_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/cascade"
	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

type stub struct {
	name string
	c    domain.Classification
	err  error
}

func (s stub) Name() string { return s.name }
func (s stub) Classify(context.Context, domain.Record) (domain.Classification, error) {
	return s.c, s.err
}

func decides(name, category string, conf float64) stub {
	return stub{name: name, c: domain.Classification{Category: category, Confidence: conf}}
}

type ctxStage struct{ name string }

func (s ctxStage) Name() string { return s.name }
func (s ctxStage) Classify(ctx context.Context, _ domain.Record) (domain.Classification, error) {
	if err := ctx.Err(); err != nil {
		return domain.Classification{}, err
	}
	return domain.Classification{Category: "ok", Confidence: 1}, nil
}

func TestRunClassifiesAndStampsStage(t *testing.T) {
	c, res, err := cascade.Run(context.Background(), []stage.Stage{decides("rules", "noise", 1)}, 0, domain.Record{}, nil)
	if err != nil || res != cascade.Classified {
		t.Fatalf("Run() = (%+v, %v, %v), want Classified", c, res, err)
	}
	if c.Category != "noise" || c.Stage != "rules" {
		t.Fatalf("Run() classification = %+v, want noise stamped rules", c)
	}
}

func TestRunExhaustedWhenAllAbstain(t *testing.T) {
	abstain := stub{name: "a", err: stage.ErrUnclassified}
	if _, res, err := cascade.Run(context.Background(), []stage.Stage{abstain}, 0, domain.Record{}, nil); err != nil || res != cascade.Exhausted {
		t.Fatalf("Run() = (%v, %v), want Exhausted, nil", res, err)
	}
}

func TestRunGateEscalatesToNextStage(t *testing.T) {
	low := decides("low", "x", 0.3)
	high := decides("high", "y", 0.9)
	c, res, err := cascade.Run(context.Background(), []stage.Stage{low, high}, 0.7, domain.Record{}, nil)
	if err != nil || res != cascade.Classified || c.Stage != "high" {
		t.Fatalf("Run() = (%+v, %v, %v), want high classified", c, res, err)
	}
}

func TestRunExhaustedWhenAllBelowGate(t *testing.T) {
	if _, res, err := cascade.Run(context.Background(), []stage.Stage{decides("low", "x", 0.3)}, 0.7, domain.Record{}, nil); err != nil || res != cascade.Exhausted {
		t.Fatalf("Run() = (%v, %v), want Exhausted", res, err)
	}
}

func TestRunStageErrorStopsAndWraps(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := cascade.Run(context.Background(), []stage.Stage{stub{name: "bad", err: boom}}, 0, domain.Record{}, nil)
	var se *stage.Error
	if !errors.As(err, &se) || se.Stage != "bad" || !errors.Is(err, boom) {
		t.Fatalf("Run() err = %v, want *stage.Error(bad) wrapping boom", err)
	}
}

func TestRunLogsGateEscalation(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	_, res, err := cascade.Run(context.Background(), []stage.Stage{decides("low", "x", 0.3)}, 0.7, domain.Record{ID: "r1"}, logger)
	if err != nil || res != cascade.Exhausted {
		t.Fatalf("Run() = (%v, %v), want Exhausted", res, err)
	}
	out := buf.String()
	for _, want := range []string{"below confidence gate", "record=r1", "stage=low", "confidence=0.3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("debug log = %q, missing %q", out, want)
		}
	}
}

func TestRunWrapsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := cascade.Run(ctx, []stage.Stage{ctxStage{name: "ctxs"}}, 0, domain.Record{}, nil)
	var se *stage.Error
	if !errors.As(err, &se) || se.Stage != "ctxs" || !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() err = %v, want *stage.Error(ctxs) wrapping context.Canceled", err)
	}
}

func TestRunRejectsInvalidResult(t *testing.T) {
	bad := stub{name: "bad", c: domain.Classification{Confidence: math.NaN()}}
	_, _, err := cascade.Run(context.Background(), []stage.Stage{bad}, 0, domain.Record{}, nil)
	var se *stage.Error
	if !errors.As(err, &se) || se.Stage != "bad" {
		t.Fatalf("Run() err = %v, want *stage.Error(bad) for invalid result", err)
	}
}
