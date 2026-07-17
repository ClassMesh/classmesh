// Package stream moves records from a Source through a Cascade into a Sink.
// Unclassified records go to an optional review sink. A nil review sink drops them.
package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/stream/sink"
	"github.com/ClassMesh/classmesh/stream/source"
)

// Options configures an Engine.
type Options struct {
	// Source yields records and is required.
	Source source.Source
	// Cascade classifies records and is required.
	Cascade *classmesh.Cascade
	// Sink receives classified records and is required.
	Sink sink.Sink
	// Review receives unclassified records with a zero Classification.
	// A nil Review drops those records.
	Review sink.Sink
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// Workers controls concurrent classification. Zero or one uses the serial loop.
	// Larger values require concurrent-safe stages and preserve serial outcomes.
	Workers int
}

// Engine drives records through the cascade.
type Engine struct {
	source  source.Source
	cascade *classmesh.Cascade
	sink    sink.Sink
	review  sink.Sink
	logger  *slog.Logger
	workers int
}

// New validates options and returns a ready Engine.
func New(options Options) (*Engine, error) {
	if options.Source == nil {
		return nil, errors.New("engine: source is required")
	}
	if options.Cascade == nil {
		return nil, errors.New("engine: cascade is required")
	}
	if options.Sink == nil {
		return nil, errors.New("engine: sink is required")
	}
	if options.Workers < 0 {
		return nil, errors.New("engine: workers must not be negative")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Engine{
		source:  options.Source,
		cascade: options.Cascade,
		sink:    options.Sink,
		review:  options.Review,
		logger:  options.Logger,
		workers: options.Workers,
	}, nil
}

// Run drains the source through the cascade. It returns when the source is
// drained, the context is cancelled, or a source, stage, or sink fails.
// Stats reflect everything processed up to the point of return.
func (e *Engine) Run(ctx context.Context) (Stats, error) {
	if e.workers > 1 {
		return e.runParallel(ctx)
	}
	stats := Stats{ByStage: make(map[string]int)}
	for {
		r, err := e.source.Next(ctx)
		if errors.Is(err, source.ErrDrained) {
			return stats, nil
		}
		if err != nil {
			return stats, fmt.Errorf("engine: source: %w", err)
		}
		stats.Processed++

		c, name, err := e.classify(ctx, r)
		if err != nil {
			return stats, err
		}
		if name == "" {
			stats.Reviewed++
			if e.review == nil {
				if stats.Reviewed == 1 {
					e.logger.Warn("records unclassified and dropped; logging the first, see stats for the total", "record", r.ID)
				} else if e.logger.Enabled(ctx, slog.LevelDebug) {
					e.logger.Debug("record unclassified and dropped", "record", r.ID)
				}
				continue
			}
			if err := e.review.Write(ctx, r, classmesh.Classification{}); err != nil {
				return stats, fmt.Errorf("engine: review sink: %w", err)
			}
			continue
		}
		if err := e.sink.Write(ctx, r, c); err != nil {
			return stats, fmt.Errorf("engine: sink: %w", err)
		}
		stats.Classified++
		stats.ByStage[name]++
	}
}

// classify runs r through the cascade. It returns the classification and the
// deciding stage's name, or an empty name when no stage decided the record.
func (e *Engine) classify(ctx context.Context, r classmesh.Record) (classmesh.Classification, string, error) {
	if err := ctx.Err(); err != nil {
		return classmesh.Classification{}, "", fmt.Errorf("engine: %w", err)
	}
	c, err := e.cascade.Classify(ctx, r)
	if errors.Is(err, classmesh.ErrUnclassified) {
		return classmesh.Classification{}, "", nil
	}
	if err != nil {
		return classmesh.Classification{}, "", fmt.Errorf("engine: %w", err)
	}
	return c, c.Stage, nil
}
