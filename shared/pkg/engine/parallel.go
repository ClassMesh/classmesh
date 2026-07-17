package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	domain "github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
)

// batchSize is how many records travel the pipeline together.
const batchSize = 64

// batch is one pipeline unit. The terminal batch ends the stream and carries
// the source's final error, sequenced after the last record.
type batch struct {
	seq      int64
	records  []domain.Record
	outcomes []outcome
	terminal bool
	srcErr   error
}

// outcome is one record's result. An empty name means no stage decided it.
type outcome struct {
	c    domain.Classification
	name string
	err  error
}

// runParallel runs one reader, N workers, and one ordered writer. Admission
// credits cap the batches in flight. The writer alone touches stats and
// sinks, so ordering, errors, and stats match the serial loop exactly.
// cap(results) must stay workers+1: one slot per credit plus one reserved
// for the terminal batch, so no send into results ever blocks.
func (e *Engine) runParallel(ctx context.Context) (Stats, error) {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	work := make(chan *batch, e.workers)
	results := make(chan *batch, e.workers+1)
	credits := make(chan struct{}, e.workers)
	for i := 0; i < e.workers; i++ {
		credits <- struct{}{}
	}

	raw := make(chan rawItem, batchSize)
	var wg sync.WaitGroup
	wg.Add(2 + e.workers)
	go func() {
		defer wg.Done()
		e.drainSource(workCtx, raw)
	}()
	go func() {
		defer wg.Done()
		(&batcher{ctx: workCtx, raw: raw, work: work, results: results, credits: credits}).run()
	}()
	for i := 0; i < e.workers; i++ {
		go func() {
			defer wg.Done()
			e.classifyBatches(workCtx, work, results)
		}()
	}

	stats, err, stoppedEarly := e.commitOrdered(ctx, results, credits)
	cancel()
	if stoppedEarly {
		_ = e.source.Close()
	}
	wg.Wait()
	return stats, err
}

// rawItem is one record off the source, or its final error.
type rawItem struct {
	rec domain.Record
	err error
}

// drainSource reads the source into raw. A read blocked in the source then
// never traps records that are already buffered.
func (e *Engine) drainSource(ctx context.Context, raw chan<- rawItem) {
	for {
		r, err := e.source.Next(ctx)
		select {
		case raw <- rawItem{rec: r, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

// batcher turns the raw feed into sequenced batches. A credit is an
// admission slot that the writer returns after committing a batch.
type batcher struct {
	ctx     context.Context
	raw     <-chan rawItem
	work    chan<- *batch
	results chan<- *batch
	credits <-chan struct{}
	seq     int64
	cur     *batch
}

// run consumes raw until it ends. New records take priority over dispatch.
// An idle feed flushes its partial batch instead of letting records stall.
func (b *batcher) run() {
	defer close(b.work)
	b.cur = &batch{records: make([]domain.Record, 0, batchSize)}
	for {
		if len(b.cur.records) == 0 {
			select {
			case it, ok := <-b.raw:
				if !b.append(it, ok) {
					return
				}
			case <-b.ctx.Done():
				b.finish(b.ctx.Err())
				return
			}
			continue
		}
		select {
		case it, ok := <-b.raw:
			if !b.append(it, ok) {
				return
			}
			continue
		default:
		}
		select {
		case it, ok := <-b.raw:
			if !b.append(it, ok) {
				return
			}
		case <-b.credits:
			if !b.dispatch() {
				b.finish(b.ctx.Err())
				return
			}
		case <-b.ctx.Done():
			b.finish(b.ctx.Err())
			return
		}
	}
}

// append adds one record and dispatches the batch when it fills. It returns
// false once the stream ends, after flushing what it can and emitting the
// terminal event.
func (b *batcher) append(it rawItem, ok bool) bool {
	if !ok || it.err != nil {
		err := it.err
		if !ok {
			err = b.ctx.Err()
		} else if errors.Is(err, source.ErrDrained) {
			err = nil
		}
		if len(b.cur.records) > 0 {
			sent := false
			select {
			case <-b.credits:
				sent = b.dispatch()
			case <-b.ctx.Done():
			}
			if !sent && err == nil {
				err = b.ctx.Err()
			}
		}
		b.finish(err)
		return false
	}
	b.cur.records = append(b.cur.records, it.rec)
	if len(b.cur.records) < batchSize {
		return true
	}
	select {
	case <-b.credits:
	case <-b.ctx.Done():
		b.finish(b.ctx.Err())
		return false
	}
	if !b.dispatch() {
		b.finish(b.ctx.Err())
		return false
	}
	return true
}

// dispatch hands the current batch to the workers. The caller already holds
// a credit. It returns false during shutdown.
func (b *batcher) dispatch() bool {
	b.cur.seq = b.seq
	select {
	case b.work <- b.cur:
	case <-b.ctx.Done():
		return false
	}
	b.seq++
	b.cur = &batch{records: make([]domain.Record, 0, batchSize)}
	return true
}

// finish emits the terminal event. The results channel reserves a slot for
// it, so the send never blocks.
func (b *batcher) finish(err error) {
	b.results <- &batch{seq: b.seq, terminal: true, srcErr: err}
}

// classifyBatches classifies each batch and always delivers it to results.
// Dropping one would deadlock the writer. The send cannot block because
// credits cap the batches in flight below the channel capacity.
func (e *Engine) classifyBatches(ctx context.Context, work <-chan *batch, results chan<- *batch) {
	for b := range work {
		b.outcomes = make([]outcome, len(b.records))
		for i := range b.records {
			c, name, err := e.classify(ctx, b.records[i])
			b.outcomes[i] = outcome{c: c, name: name, err: err}
			if err != nil {
				break
			}
		}
		results <- b
	}
}

// writer commits batches in sequence order. It alone touches stats and sinks.
type writer struct {
	e     *Engine
	ctx   context.Context
	stats Stats
}

// commitOrdered commits batches in order, returning one credit per commit.
// The first error in sequence order wins. The bool reports an early halt so
// the caller closes the source.
func (e *Engine) commitOrdered(ctx context.Context, results <-chan *batch, credits chan<- struct{}) (Stats, error, bool) {
	w := &writer{e: e, ctx: ctx, stats: Stats{ByStage: make(map[string]int)}}
	pending := make(map[int64]*batch, e.workers)
	var next int64
	for {
		b := <-results
		pending[b.seq] = b
		for {
			cur, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			if cur.terminal {
				if cur.srcErr != nil {
					return w.stats, fmt.Errorf("engine: source: %w", cur.srcErr), true
				}
				return w.stats, nil, false
			}
			if err := w.commit(cur); err != nil {
				return w.stats, err, true
			}
			credits <- struct{}{}
			next++
		}
	}
}

// commit applies one batch at the serial loop's exact commit points.
func (w *writer) commit(b *batch) error {
	for i := range b.records {
		w.stats.Processed++
		o := &b.outcomes[i]
		switch {
		case o.err != nil:
			return o.err
		case o.name == "":
			if err := w.review(b.records[i]); err != nil {
				return err
			}
		default:
			if err := w.e.sink.Write(w.ctx, b.records[i], o.c); err != nil {
				return fmt.Errorf("engine: sink: %w", err)
			}
			w.stats.Classified++
			w.stats.ByStage[o.name]++
		}
	}
	return nil
}

// review routes one undecided record, or counts and drops it.
func (w *writer) review(r domain.Record) error {
	w.stats.Reviewed++
	if w.e.review == nil {
		if w.stats.Reviewed == 1 {
			w.e.logger.Warn("records unclassified and dropped; logging the first, see stats for the total", "record", r.ID)
		} else if w.e.logger.Enabled(w.ctx, slog.LevelDebug) {
			w.e.logger.Debug("record unclassified and dropped", "record", r.ID)
		}
		return nil
	}
	if err := w.e.review.Write(w.ctx, r, domain.Classification{}); err != nil {
		return fmt.Errorf("engine: review sink: %w", err)
	}
	return nil
}
