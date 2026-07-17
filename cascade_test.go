package classmesh

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func rec(s string) Record { return Record{ID: s, Data: []byte(s)} }

func TestClassifyReturnsFirstConfidentDecision(t *testing.T) {
	c, err := New(newStatic("rules", map[string]string{"ping": "noise"}))
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
	c, err := NewWithOptions(Options{Stages: []Stage{
		newStatic("first", nil),
		newStatic("second", map[string]string{"x": "hit"}),
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
	c, err := NewWithOptions(Options{Stages: []Stage{newStatic("rules", nil)}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.Classify(context.Background(), rec("anything")); err != ErrUnclassified {
		t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
	}
}

func TestClassifyGateEscalatesLowConfidence(t *testing.T) {
	c, err := NewWithOptions(Options{
		Stages:        []Stage{scored{name: "model", conf: 0.4}},
		MinConfidence: 0.7,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.Classify(context.Background(), rec("x")); !errors.Is(err, ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want ErrUnclassified (gated)", err)
	}
}

func TestClassifyGatedThenConfident(t *testing.T) {
	c, err := NewWithOptions(Options{
		Stages:        []Stage{scored{name: "a", conf: 0.4}, scored{name: "b", conf: 0.9}},
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
	c, err := NewWithOptions(Options{Stages: []Stage{failing{err: boom}}})
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
	if err.Error() != "stage failing: boom" {
		t.Fatalf("Classify() error = %q, want bare stage error", err)
	}
	var se *StageError
	if !errors.As(err, &se) || se.Stage != "failing" {
		t.Fatalf("Classify() error = %v, want *StageError with Stage=failing", err)
	}
}

func TestClassifyHonorsContextCancellation(t *testing.T) {
	c, err := NewWithOptions(Options{Stages: []Stage{newStatic("rules", nil)}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Classify(ctx, rec("x")); err != context.Canceled {
		t.Fatalf("Classify() error = %v, want context.Canceled", err)
	}
}

func TestClassifyBatchPreservesOrderAndMix(t *testing.T) {
	c, err := NewWithOptions(Options{Stages: []Stage{
		newStatic("rules", map[string]string{"ping": "noise", "pay": "billing"}),
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	results := c.ClassifyBatch(context.Background(), []Record{rec("ping"), rec("nope"), rec("pay")})
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	if results[0].Err != nil || results[0].Classification.Category != "noise" {
		t.Fatalf("results[0] = %+v, want noise", results[0])
	}
	if !errors.Is(results[1].Err, ErrUnclassified) {
		t.Fatalf("results[1].Err = %v, want ErrUnclassified", results[1].Err)
	}
	if results[2].Err != nil || results[2].Classification.Category != "billing" {
		t.Fatalf("results[2] = %+v, want billing", results[2])
	}
}

func TestClassifyBatchEmpty(t *testing.T) {
	c, err := NewWithOptions(Options{Stages: []Stage{newStatic("rules", nil)}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := c.ClassifyBatch(context.Background(), nil); len(got) != 0 {
		t.Fatalf("ClassifyBatch(nil) = %v, want empty", got)
	}
}

func TestClassifyBatchPropagatesStageError(t *testing.T) {
	boom := errors.New("boom")
	c, err := NewWithOptions(Options{Stages: []Stage{failing{err: boom}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	results := c.ClassifyBatch(context.Background(), []Record{rec("x")})
	if len(results) != 1 || !errors.Is(results[0].Err, boom) {
		t.Fatalf("results = %+v, want one wrapping boom", results)
	}
}

func TestClassifyBatchHonorsCancellation(t *testing.T) {
	c, err := NewWithOptions(Options{Stages: []Stage{newStatic("rules", map[string]string{"ping": "noise"})}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := c.ClassifyBatch(ctx, []Record{rec("ping"), rec("ping")})
	for i, r := range results {
		if !errors.Is(r.Err, context.Canceled) {
			t.Fatalf("results[%d].Err = %v, want context.Canceled", i, r.Err)
		}
	}
}

func TestClassifyBatchConcurrentPreservesOrder(t *testing.T) {
	c, err := NewWithOptions(Options{Stages: []Stage{
		newStatic("rules", map[string]string{"ping": "noise", "pay": "billing"}),
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var records []Record
	for i := 0; i < 50; i++ {
		records = append(records, rec("ping"), rec("nope"), rec("pay"))
	}

	results := c.ClassifyBatchConcurrent(context.Background(), records, 8)
	if len(results) != len(records) {
		t.Fatalf("len(results) = %d, want %d", len(results), len(records))
	}
	for i, r := range results {
		switch i % 3 {
		case 0:
			if r.Err != nil || r.Classification.Category != "noise" {
				t.Fatalf("results[%d] = %+v, want noise", i, r)
			}
		case 1:
			if !errors.Is(r.Err, ErrUnclassified) {
				t.Fatalf("results[%d].Err = %v, want ErrUnclassified", i, r.Err)
			}
		case 2:
			if r.Err != nil || r.Classification.Category != "billing" {
				t.Fatalf("results[%d] = %+v, want billing", i, r)
			}
		}
	}
}

func TestClassifyBatchConcurrentRunsSequentialBelowTwoWorkers(t *testing.T) {
	c, err := NewWithOptions(Options{Stages: []Stage{newStatic("rules", map[string]string{"ping": "noise"})}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, w := range []int{0, 1} {
		got := c.ClassifyBatchConcurrent(context.Background(), []Record{rec("ping")}, w)
		if len(got) != 1 || got[0].Classification.Category != "noise" {
			t.Fatalf("workers=%d: %+v, want one noise result", w, got)
		}
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New(); err == nil || err.Error() != "classmesh: at least one stage is required" {
		t.Fatalf("New() with no stages error = %v, want classmesh validation error", err)
	}
	for _, bad := range []float64{-0.1, 1.5} {
		_, err := NewWithOptions(Options{Stages: []Stage{newStatic("s", nil)}, MinConfidence: bad})
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
func (s scored) Classify(ctx context.Context, _ Record) (Classification, error) {
	if err := ctx.Err(); err != nil {
		return Classification{}, err
	}
	return Classification{Category: "cat-" + s.name, Confidence: s.conf, Stage: s.name}, nil
}

type failing struct{ err error }

func (f failing) Name() string { return "failing" }
func (f failing) Classify(_ context.Context, _ Record) (Classification, error) {
	return Classification{}, f.err
}
