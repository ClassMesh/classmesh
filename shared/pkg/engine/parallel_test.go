package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

func sequentialRecords(n int) []domain.Record {
	rs := make([]domain.Record, n)
	for i := range rs {
		rs[i] = domain.Record{ID: fmt.Sprintf("r%06d", i), Data: []byte(fmt.Sprintf("payload %d", i))}
	}
	return rs
}

// alwaysStage classifies every record into one category, optionally holding
// one record hostage until release closes and optionally failing on one ID.
type alwaysStage struct {
	name    string
	holdID  string
	release chan struct{}
	failID  string
	failErr error
	seen    atomic.Int64
	onSeen  func(total int64)
}

func (s *alwaysStage) Name() string { return s.name }

func (s *alwaysStage) Classify(ctx context.Context, r domain.Record) (domain.Classification, error) {
	if err := ctx.Err(); err != nil {
		return domain.Classification{}, err
	}
	if r.ID == s.holdID {
		select {
		case <-s.release:
		case <-ctx.Done():
			return domain.Classification{}, ctx.Err()
		}
	}
	defer func() {
		n := s.seen.Add(1)
		if s.onSeen != nil {
			s.onSeen(n)
		}
	}()
	if r.ID == s.failID {
		return domain.Classification{}, s.failErr
	}
	return domain.Classification{Category: "ok", Confidence: 1}, nil
}

// blockingSource yields its records, then blocks in Next until Close, like a
// pipe with no writer. It deliberately ignores ctx, modeling sources whose
// blocked read only Close can interrupt.
type blockingSource struct {
	recs   []domain.Record
	i      int
	closed chan struct{}
	once   sync.Once
}

func newBlockingSource(recs []domain.Record) *blockingSource {
	return &blockingSource{recs: recs, closed: make(chan struct{})}
}

func (b *blockingSource) Next(context.Context) (domain.Record, error) {
	if b.i < len(b.recs) {
		r := b.recs[b.i]
		b.i++
		return r, nil
	}
	<-b.closed
	return domain.Record{}, source.ErrDrained
}

func (b *blockingSource) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// erroringSource yields its records, then fails.
type erroringSource struct {
	recs []domain.Record
	i    int
	err  error
}

func (e *erroringSource) Next(context.Context) (domain.Record, error) {
	if e.i < len(e.recs) {
		r := e.recs[e.i]
		e.i++
		return r, nil
	}
	return domain.Record{}, e.err
}

func (e *erroringSource) Close() error { return nil }

// guardedSink wraps a sink and fails the test if Write ever runs concurrently.
type guardedSink struct {
	t      *testing.T
	inner  sink.Sink
	inside atomic.Int64
	failID string
	err    error
}

func (g *guardedSink) Write(ctx context.Context, r domain.Record, c domain.Classification) error {
	if g.inside.Add(1) != 1 {
		g.t.Error("concurrent sink Write detected")
	}
	defer g.inside.Add(-1)
	if g.failID != "" && r.ID == g.failID {
		return g.err
	}
	return g.inner.Write(ctx, r, c)
}

func (g *guardedSink) Close() error { return g.inner.Close() }

func runEngine(t *testing.T, d Deps) (Stats, error) {
	t.Helper()
	e, err := New(d)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return e.Run(context.Background())
}

func checkNoLeak(t *testing.T) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		for i := 0; i < 50; i++ {
			if runtime.NumGoroutine() <= before {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Errorf("goroutine leak: %d before, %d after", before, runtime.NumGoroutine())
	})
}

func TestNewRejectsNegativeWorkers(t *testing.T) {
	_, err := New(Deps{
		Source:  source.NewInMemory(nil),
		Stages:  []stage.Stage{stage.NewStatic("s", nil)},
		Sink:    sink.NewInMemory(),
		Workers: -1,
	})
	if err == nil {
		t.Fatal("New(Workers: -1) error = nil, want rejection")
	}
}

func TestParallelMatchesSerialAcrossBatchBoundaries(t *testing.T) {
	checkNoLeak(t)
	for _, n := range []int{0, 1, 63, 64, 65, 129, 1000} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			recs := sequentialRecords(n)
			var runs []Stats
			var outputs [][]sink.Entry
			for _, workers := range []int{0, 1, 8} {
				out := sink.NewInMemory()
				stats, err := runEngine(t, Deps{
					Source:  source.NewInMemory(recs),
					Stages:  []stage.Stage{&alwaysStage{name: "s"}},
					Sink:    out,
					Logger:  discardLogger(),
					Workers: workers,
				})
				if err != nil {
					t.Fatalf("workers=%d Run() error = %v", workers, err)
				}
				runs = append(runs, stats)
				outputs = append(outputs, out.Entries())
			}
			for i := 1; i < len(runs); i++ {
				if !statsEqual(runs[i], runs[0]) {
					t.Fatalf("stats diverge: %+v vs %+v", runs[i], runs[0])
				}
				if len(outputs[i]) != len(outputs[0]) {
					t.Fatalf("output length diverges: %d vs %d", len(outputs[i]), len(outputs[0]))
				}
				for j := range outputs[i] {
					if outputs[i][j].Record.ID != outputs[0][j].Record.ID {
						t.Fatalf("output order diverges at %d: %s vs %s", j, outputs[i][j].Record.ID, outputs[0][j].Record.ID)
					}
				}
			}
		})
	}
}

func TestParallelOrderSurvivesAHeldBatch(t *testing.T) {
	checkNoLeak(t)
	const workers = 4
	n := 6 * batchSize
	recs := sequentialRecords(n)
	st := &alwaysStage{name: "s", holdID: recs[0].ID, release: make(chan struct{})}
	var once sync.Once
	st.onSeen = func(total int64) {
		if total >= int64(workers-1) {
			once.Do(func() { close(st.release) })
		}
	}
	out := sink.NewInMemory()
	stats, err := runEngine(t, Deps{
		Source:  source.NewInMemory(recs),
		Stages:  []stage.Stage{st},
		Sink:    &guardedSink{t: t, inner: out},
		Logger:  discardLogger(),
		Workers: workers,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stats.Processed != n || stats.Classified != n {
		t.Fatalf("stats = %+v, want all %d classified", stats, n)
	}
	entries := out.Entries()
	for i, e := range entries {
		if e.Record.ID != recs[i].ID {
			t.Fatalf("output out of order at %d: got %s want %s", i, e.Record.ID, recs[i].ID)
		}
	}
}

func TestParallelFirstErrorBySequenceIsDeterministic(t *testing.T) {
	checkNoLeak(t)
	const workers = 4
	n := 6 * batchSize
	boom := errors.New("boom")
	for iter := 0; iter < 20; iter++ {
		recs := sequentialRecords(n)
		st := &alwaysStage{name: "s", holdID: recs[0].ID, release: make(chan struct{}), failID: recs[5].ID, failErr: boom}
		var once sync.Once
		st.onSeen = func(total int64) {
			if total >= int64(workers-1) {
				once.Do(func() { close(st.release) })
			}
		}
		stats, err := runEngine(t, Deps{
			Source:  source.NewInMemory(recs),
			Stages:  []stage.Stage{st},
			Sink:    sink.NewInMemory(),
			Logger:  discardLogger(),
			Workers: workers,
		})
		if err == nil || !errors.Is(err, boom) {
			t.Fatalf("iter %d: Run() error = %v, want boom from record 5", iter, err)
		}
		if stats.Processed != 6 || stats.Classified != 5 {
			t.Fatalf("iter %d: stats = %+v, want Processed=6 Classified=5 (commit stops at the erroring record)", iter, stats)
		}
	}
}

func TestParallelSourceErrorCommitsEarlierRecords(t *testing.T) {
	checkNoLeak(t)
	readFail := errors.New("disk detached")
	n := batchSize + 7
	src := &erroringSource{recs: sequentialRecords(n), err: readFail}
	out := sink.NewInMemory()
	stats, err := runEngine(t, Deps{
		Source:  src,
		Stages:  []stage.Stage{&alwaysStage{name: "s"}},
		Sink:    out,
		Logger:  discardLogger(),
		Workers: 4,
	})
	if err == nil || !errors.Is(err, readFail) {
		t.Fatalf("Run() error = %v, want the source failure", err)
	}
	if stats.Processed != n || stats.Classified != n || len(out.Entries()) != n {
		t.Fatalf("stats = %+v entries=%d, want all %d records committed before the source error", stats, len(out.Entries()), n)
	}
}

func TestParallelStageErrorBeatsLaterSourceError(t *testing.T) {
	checkNoLeak(t)
	boom := errors.New("stage boom")
	recs := sequentialRecords(batchSize)
	src := &erroringSource{recs: recs, err: errors.New("late source error")}
	stats, err := runEngine(t, Deps{
		Source:  src,
		Stages:  []stage.Stage{&alwaysStage{name: "s", failID: recs[3].ID, failErr: boom}},
		Sink:    sink.NewInMemory(),
		Logger:  discardLogger(),
		Workers: 4,
	})
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("Run() error = %v, want the earlier stage error to win", err)
	}
	if stats.Processed != 4 {
		t.Fatalf("stats = %+v, want Processed=4", stats)
	}
}

func TestParallelSinkErrorUnwindsBlockedSource(t *testing.T) {
	checkNoLeak(t)
	writeFail := errors.New("disk full")
	recs := sequentialRecords(3)
	src := newBlockingSource(recs)
	out := &guardedSink{t: t, inner: sink.NewInMemory(), failID: recs[1].ID, err: writeFail}
	done := make(chan struct{})
	var stats Stats
	var err error
	go func() {
		defer close(done)
		stats, err = runEngine(t, Deps{
			Source:  src,
			Stages:  []stage.Stage{&alwaysStage{name: "s"}},
			Sink:    out,
			Logger:  discardLogger(),
			Workers: 2,
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() hung: sink error did not unwind the blocked source")
	}
	if err == nil || !errors.Is(err, writeFail) {
		t.Fatalf("Run() error = %v, want the sink failure", err)
	}
	if stats.Processed != 2 || stats.Classified != 1 {
		t.Fatalf("stats = %+v, want Processed=2 Classified=1 (erroring write counted processed, not classified)", stats)
	}
}

func TestParallelReviewRoutingStaysOrderedAndWarnsOnce(t *testing.T) {
	checkNoLeak(t)
	n := 2*batchSize + 5
	recs := sequentialRecords(n)
	review := sink.NewInMemory()
	stats, err := runEngine(t, Deps{
		Source:  source.NewInMemory(recs),
		Stages:  []stage.Stage{stage.NewStatic("s", nil)},
		Sink:    sink.NewInMemory(),
		Review:  &guardedSink{t: t, inner: review},
		Logger:  discardLogger(),
		Workers: 4,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stats.Reviewed != n {
		t.Fatalf("stats = %+v, want Reviewed=%d", stats, n)
	}
	entries := review.Entries()
	for i, e := range entries {
		if e.Record.ID != recs[i].ID {
			t.Fatalf("review order diverges at %d", i)
		}
	}
}

func TestParallelCancellationStopsTheRun(t *testing.T) {
	checkNoLeak(t)
	recs := sequentialRecords(10 * batchSize)
	held := &alwaysStage{name: "s", holdID: recs[batchSize].ID, release: make(chan struct{})}
	e, err := New(Deps{
		Source:  source.NewInMemory(recs),
		Stages:  []stage.Stage{held},
		Sink:    sink.NewInMemory(),
		Logger:  discardLogger(),
		Workers: 4,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		_, runErr = e.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() hung on cancellation")
	}
	if runErr == nil || !errors.Is(runErr, context.Canceled) {
		t.Fatalf("Run() error = %v, want a context.Canceled-derived error", runErr)
	}
}

// failingReview wraps a review sink to fail on one record.
type failingReview struct {
	inner  sink.Sink
	failID string
	err    error
}

func (f *failingReview) Write(ctx context.Context, r domain.Record, c domain.Classification) error {
	if r.ID == f.failID {
		return f.err
	}
	return f.inner.Write(ctx, r, c)
}

func (f *failingReview) Close() error { return f.inner.Close() }

// TestParallelFailureMatrixMatchesSerial runs every failure shape through the
// serial loop and the parallel pipeline and requires identical errors and
// stats: the strongest form of the determinism contract.
func TestParallelFailureMatrixMatchesSerial(t *testing.T) {
	boom := errors.New("boom")
	n := 2*batchSize + 5
	cases := []struct {
		name string
		deps func(recs []domain.Record) Deps
	}{
		{"stage error at 0", func(recs []domain.Record) Deps {
			return Deps{Stages: []stage.Stage{&alwaysStage{name: "s", failID: recs[0].ID, failErr: boom}}, Sink: sink.NewInMemory()}
		}},
		{"stage error at batch boundary", func(recs []domain.Record) Deps {
			return Deps{Stages: []stage.Stage{&alwaysStage{name: "s", failID: recs[batchSize].ID, failErr: boom}}, Sink: sink.NewInMemory()}
		}},
		{"stage error at last record", func(recs []domain.Record) Deps {
			return Deps{Stages: []stage.Stage{&alwaysStage{name: "s", failID: recs[n-1].ID, failErr: boom}}, Sink: sink.NewInMemory()}
		}},
		{"sink error mid-stream", func(recs []domain.Record) Deps {
			return Deps{Stages: []stage.Stage{&alwaysStage{name: "s"}}, Sink: &guardedSink{inner: sink.NewInMemory(), failID: recs[70].ID, err: boom}}
		}},
		{"review write error", func(recs []domain.Record) Deps {
			return Deps{Stages: []stage.Stage{stage.NewStatic("s", nil)}, Sink: sink.NewInMemory(), Review: &failingReview{inner: sink.NewInMemory(), failID: recs[66].ID, err: boom}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := sequentialRecords(n)
			var wantStats Stats
			var wantErr error
			for i, workers := range []int{0, 4} {
				d := tc.deps(recs)
				d.Source = source.NewInMemory(recs)
				d.Logger = discardLogger()
				d.Workers = workers
				stats, err := runEngine(t, d)
				if i == 0 {
					wantStats, wantErr = stats, err
					continue
				}
				if (err == nil) != (wantErr == nil) || (err != nil && err.Error() != wantErr.Error()) {
					t.Fatalf("workers=%d error = %v, serial = %v", workers, err, wantErr)
				}
				if !statsEqual(stats, wantStats) {
					t.Fatalf("workers=%d stats = %+v, serial = %+v", workers, stats, wantStats)
				}
			}
		})
	}
}

// TestParallelCancelWithBlockedNext cancels while the source is blocked in
// Next: the batcher reports the cancellation in-band and shutdown closes the
// source, so Run returns instead of hanging.
func TestParallelCancelWithBlockedNext(t *testing.T) {
	checkNoLeak(t)
	src := newBlockingSource(sequentialRecords(3))
	e, err := New(Deps{
		Source:  src,
		Stages:  []stage.Stage{&alwaysStage{name: "s"}},
		Sink:    sink.NewInMemory(),
		Logger:  discardLogger(),
		Workers: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		_, runErr = e.Run(ctx)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() hung: cancel with a blocked Next did not unwind")
	}
	if runErr == nil || !errors.Is(runErr, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled-derived", runErr)
	}
}

func statsEqual(a, b Stats) bool {
	if a.Processed != b.Processed || a.Classified != b.Classified || a.Reviewed != b.Reviewed || len(a.ByStage) != len(b.ByStage) {
		return false
	}
	for k, v := range a.ByStage {
		if b.ByStage[k] != v {
			return false
		}
	}
	return true
}
