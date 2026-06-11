package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

func records(payloads ...string) []domain.Record {
	rs := make([]domain.Record, len(payloads))
	for i, p := range payloads {
		rs[i] = domain.Record{ID: p, Data: []byte(p)}
	}
	return rs
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestNewValidatesDeps(t *testing.T) {
	src := source.NewInMemory(nil)
	st := stage.NewStatic("s", nil)
	snk := sink.NewInMemory()

	cases := []struct {
		name    string
		deps    Deps
		wantErr string
	}{
		{"missing source", Deps{Stages: []stage.Stage{st}, Sink: snk}, "source"},
		{"missing stages", Deps{Source: src, Sink: snk}, "stage"},
		{"missing sink", Deps{Source: src, Stages: []stage.Stage{st}}, "sink"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.deps)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("New() error = %v, want mention of %q", err, tc.wantErr)
			}
		})
	}
}

func TestRunCascadesAndRoutesReview(t *testing.T) {
	src := source.NewInMemory(records("healthz", "payment failed", "garbage"))
	stage1 := stage.NewStatic("rules", map[string]string{"healthz": "noise"})
	stage2 := stage.NewStatic("model", map[string]string{"payment failed": "billing"})
	classified := sink.NewInMemory()
	review := sink.NewInMemory()

	e, err := New(Deps{
		Source: src,
		Stages: []stage.Stage{stage1, stage2},
		Sink:   classified,
		Review: review,
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stats, err := e.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if stats.Processed != 3 || stats.Classified != 2 || stats.Reviewed != 1 {
		t.Fatalf("stats = %+v, want Processed=3 Classified=2 Reviewed=1", stats)
	}
	if stats.ByStage["rules"] != 1 || stats.ByStage["model"] != 1 {
		t.Fatalf("ByStage = %v, want rules=1 model=1", stats.ByStage)
	}

	got := classified.Entries()
	if len(got) != 2 {
		t.Fatalf("classified entries = %d, want 2", len(got))
	}
	if got[0].Classification.Category != "noise" || got[0].Classification.Stage != "rules" {
		t.Fatalf("entry 0 = %+v, want noise via rules", got[0].Classification)
	}
	if got[1].Classification.Category != "billing" || got[1].Classification.Stage != "model" {
		t.Fatalf("entry 1 = %+v, want billing via model", got[1].Classification)
	}

	rev := review.Entries()
	if len(rev) != 1 || rev[0].Record.ID != "garbage" {
		t.Fatalf("review entries = %+v, want only the garbage record", rev)
	}
}

func TestRunWithoutReviewSinkDropsUnclassified(t *testing.T) {
	src := source.NewInMemory(records("garbage"))
	e, err := New(Deps{
		Source: src,
		Stages: []stage.Stage{stage.NewStatic("rules", nil)},
		Sink:   sink.NewInMemory(),
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stats, err := e.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stats.Processed != 1 || stats.Classified != 0 || stats.Reviewed != 1 {
		t.Fatalf("stats = %+v, want Processed=1 Classified=0 Reviewed=1", stats)
	}
}

type failingStage struct{ err error }

func (f failingStage) Name() string { return "failing" }
func (f failingStage) Classify(context.Context, domain.Record) (domain.Classification, error) {
	return domain.Classification{}, f.err
}

func TestRunPropagatesStageError(t *testing.T) {
	boom := errors.New("boom")
	src := source.NewInMemory(records("a"))
	e, err := New(Deps{
		Source: src,
		Stages: []stage.Stage{failingStage{err: boom}},
		Sink:   sink.NewInMemory(),
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	stats, err := e.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Run() error = %v, want wrapped boom", err)
	}
	if !strings.Contains(err.Error(), "failing") {
		t.Fatalf("Run() error = %v, want stage name in message", err)
	}
	if stats.Processed != 1 {
		t.Fatalf("stats.Processed = %d, want 1", stats.Processed)
	}
}

func TestRunHonorsContextCancellation(t *testing.T) {
	src := source.NewInMemory(records("a"))
	e, err := New(Deps{
		Source: src,
		Stages: []stage.Stage{stage.NewStatic("rules", nil)},
		Sink:   sink.NewInMemory(),
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
}
