package stream

import (
	"context"
	"errors"
	"fmt"
	"testing"

	domain "github.com/ClassMesh/classmesh"
	stage "github.com/ClassMesh/classmesh/internal/testkit"
	"github.com/ClassMesh/classmesh/stream/sink"
	"github.com/ClassMesh/classmesh/stream/source"
)

// TestCommitOrderedReordersFullWindow drives the writer directly with the
// worst-case arrival order (the entire admission window lands before seq 0),
// proving deterministically that the reorder buffer holds W-1 batches, output
// stays in sequence order, and exactly one credit returns per commit.
func TestCommitOrderedReordersFullWindow(t *testing.T) {
	const workers = 4
	e, err := newTestEngine(testOptions{
		Source:  source.NewInMemory(nil),
		Stages:  []domain.Stage{stage.NewStatic("s", nil)},
		Sink:    sink.NewInMemory(),
		Logger:  discardLogger(),
		Workers: workers,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	out := sink.NewInMemory()
	e.sink = out

	results := make(chan *batch, workers+1)
	credits := make(chan struct{}, workers)
	mkBatch := func(seq int64, ids ...string) *batch {
		b := &batch{seq: seq}
		for _, id := range ids {
			b.records = append(b.records, domain.Record{ID: id})
			b.outcomes = append(b.outcomes, outcome{c: domain.Classification{Category: "ok", Confidence: 1}, name: "s"})
		}
		return b
	}
	for seq := int64(workers - 1); seq >= 0; seq-- {
		results <- mkBatch(seq, fmt.Sprintf("b%d-r0", seq), fmt.Sprintf("b%d-r1", seq))
	}
	results <- &batch{seq: int64(workers), terminal: true}

	stats, runErr, stopped := e.commitOrdered(context.Background(), results, credits)
	if runErr != nil || stopped {
		t.Fatalf("commitOrdered() = err %v stopped %v, want clean end", runErr, stopped)
	}
	if stats.Processed != 2*workers || stats.Classified != 2*workers {
		t.Fatalf("stats = %+v, want %d processed and classified", stats, 2*workers)
	}
	if got := len(credits); got != workers {
		t.Fatalf("credits released = %d, want exactly %d (one per committed batch)", got, workers)
	}
	entries := out.Entries()
	for i, e := range entries {
		want := fmt.Sprintf("b%d-r%d", i/2, i%2)
		if e.Record.ID != want {
			t.Fatalf("output %d = %s, want %s (sequence order)", i, e.Record.ID, want)
		}
	}
}

// TestCommitOrderedFirstErrorBySequence feeds two errored batches arriving in
// reverse order; the lower sequence's error must win and commits must stop at
// its exact record.
func TestCommitOrderedFirstErrorBySequence(t *testing.T) {
	early := errors.New("early boom")
	late := errors.New("late boom")
	e, err := newTestEngine(testOptions{
		Source:  source.NewInMemory(nil),
		Stages:  []domain.Stage{stage.NewStatic("s", nil)},
		Sink:    sink.NewInMemory(),
		Logger:  discardLogger(),
		Workers: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	results := make(chan *batch, 3)
	credits := make(chan struct{}, 2)
	lateBatch := &batch{seq: 1,
		records:  []domain.Record{{ID: "b1-r0"}},
		outcomes: []outcome{{err: late}}}
	earlyBatch := &batch{seq: 0,
		records: []domain.Record{{ID: "b0-r0"}, {ID: "b0-r1"}, {ID: "b0-r2"}},
		outcomes: []outcome{
			{c: domain.Classification{Category: "ok", Confidence: 1}, name: "s"},
			{err: early},
			{},
		}}
	results <- lateBatch
	results <- earlyBatch

	stats, runErr, stopped := e.commitOrdered(context.Background(), results, credits)
	if !errors.Is(runErr, early) || !stopped {
		t.Fatalf("commitOrdered() = %v stopped %v, want the lower-sequence error", runErr, stopped)
	}
	if stats.Processed != 2 || stats.Classified != 1 {
		t.Fatalf("stats = %+v, want Processed=2 Classified=1 (stop at the erroring record)", stats)
	}
}
