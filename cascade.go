package classmesh

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

const chunkSize = 64

// Options configures a Cascade.
type Options struct {
	Stages        []Stage
	MinConfidence float64
	Logger        *slog.Logger
}

// Cascade classifies records through an ordered set of stages.
type Cascade struct {
	stages []Stage
	gate   Gate
	logger *slog.Logger
}

// New constructs a Cascade with no confidence gate or logger.
func New(stages ...Stage) (*Cascade, error) {
	return NewWithOptions(Options{Stages: stages})
}

// NewWithOptions validates options and constructs a Cascade.
func NewWithOptions(options Options) (*Cascade, error) {
	if len(options.Stages) == 0 {
		return nil, errors.New("classmesh: at least one stage is required")
	}
	if err := validateStages(options.Stages); err != nil {
		return nil, fmt.Errorf("classmesh: %w", err)
	}
	gate, err := NewGate(options.MinConfidence)
	if err != nil {
		return nil, fmt.Errorf("classmesh: %w", err)
	}
	return &Cascade{
		stages: append([]Stage(nil), options.Stages...),
		gate:   gate,
		logger: options.Logger,
	}, nil
}

// Classify returns the first admitted stage decision.
func (c *Cascade) Classify(ctx context.Context, r Record) (Classification, error) {
	if err := ctx.Err(); err != nil {
		return Classification{}, err
	}
	for _, st := range c.stages {
		classification, err := st.Classify(ctx, r)
		if errors.Is(err, ErrUnclassified) {
			continue
		}
		if err != nil {
			return Classification{}, &StageError{Stage: st.Name(), Err: err}
		}
		if err := validateResult(st.Name(), classification); err != nil {
			return Classification{}, err
		}
		if !c.gate.admits(classification.Confidence) {
			if c.logger != nil && c.logger.Enabled(ctx, slog.LevelDebug) {
				c.logger.Debug("classification below confidence gate, escalating",
					"record", r.ID, "stage", st.Name(), "category", classification.Category,
					"confidence", classification.Confidence, "gate", c.gate.min)
			}
			continue
		}
		classification.Stage = st.Name()
		return classification, nil
	}
	return Classification{}, ErrUnclassified
}

// Result pairs a Classification with its classification error.
type Result struct {
	Classification Classification
	Err            error
}

// ClassifyBatch classifies records sequentially in input order.
func (c *Cascade) ClassifyBatch(ctx context.Context, records []Record) []Result {
	results := make([]Result, len(records))
	for i, r := range records {
		classification, err := c.Classify(ctx, r)
		results[i] = Result{Classification: classification, Err: err}
	}
	return results
}

// ClassifyBatchConcurrent classifies records concurrently in input order.
func (c *Cascade) ClassifyBatchConcurrent(ctx context.Context, records []Record, workers int) []Result {
	if workers < 2 || len(records) < 2 {
		return c.ClassifyBatch(ctx, records)
	}
	chunks := (len(records) + chunkSize - 1) / chunkSize
	if workers > chunks {
		workers = chunks
	}
	results := make([]Result, len(records))
	var nextChunk int64 = -1
	var wg sync.WaitGroup
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wg.Done()
			for {
				start := int(atomic.AddInt64(&nextChunk, 1)) * chunkSize
				if start >= len(records) {
					return
				}
				end := start + chunkSize
				if end > len(records) {
					end = len(records)
				}
				for i := start; i < end; i++ {
					classification, err := c.Classify(ctx, records[i])
					results[i] = Result{Classification: classification, Err: err}
				}
			}
		}()
	}
	wg.Wait()
	return results
}
