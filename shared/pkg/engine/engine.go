// Package engine runs the classification cascade: records flow from a
// Source through an ordered list of Stages into a Sink. Each record exits at
// the first stage that can decide it, so cheap stages shield expensive ones.
// Records no stage can classify are routed to an optional review sink.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

// Deps bundles everything an Engine needs.
type Deps struct {
	// Source yields the records to classify. Required.
	Source source.Source
	// Stages is the cascade, cheapest first. At least one is required.
	Stages []stage.Stage
	// Sink receives every classified record. Required.
	Sink sink.Sink
	// Review receives records no stage could classify, with a zero
	// Classification. Optional; when nil such records are dropped.
	Review sink.Sink
	// Logger defaults to slog.Default.
	Logger *slog.Logger
	// MinConfidence is the gate: a stage's classification below this
	// threshold is treated as undecided and the record escalates to the
	// next stage. Zero disables gating. Deterministic stages emit 1, so
	// they always pass.
	MinConfidence float64
}

// Stats summarizes one Run.
type Stats struct {
	// Processed counts records read from the source.
	Processed int
	// Classified counts records some stage decided.
	Classified int
	// Reviewed counts records no stage decided, whether routed to the
	// review sink or dropped.
	Reviewed int
	// ByStage counts classifications per stage name.
	ByStage map[string]int
}

// Engine drives records through the cascade.
type Engine struct {
	source source.Source
	stages []stage.Stage
	sink   sink.Sink
	review sink.Sink
	logger *slog.Logger
	gate   stage.Gate
}

// New validates deps and returns a ready Engine.
func New(d Deps) (*Engine, error) {
	if d.Source == nil {
		return nil, errors.New("engine: source is required")
	}
	if len(d.Stages) == 0 {
		return nil, errors.New("engine: at least one stage is required")
	}
	if d.Sink == nil {
		return nil, errors.New("engine: sink is required")
	}
	gate, err := stage.NewGate(d.MinConfidence)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return &Engine{
		source: d.Source,
		stages: d.Stages,
		sink:   d.Sink,
		review: d.Review,
		logger: d.Logger,
		gate:   gate,
	}, nil
}

// Run drains the source through the cascade. It returns when the source is
// drained, the context is cancelled, or a source, stage, or sink fails.
// Stats reflect everything processed up to the point of return.
func (e *Engine) Run(ctx context.Context) (Stats, error) {
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
				e.logger.Warn("record unclassified and dropped", "record", r.ID)
				continue
			}
			if err := e.review.Write(ctx, r, domain.Classification{}); err != nil {
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

// classify runs r through the cascade. It returns the classification and
// the deciding stage's name, or an empty name when every stage returned
// ErrUnclassified or decided below the confidence gate.
func (e *Engine) classify(ctx context.Context, r domain.Record) (domain.Classification, string, error) {
	for _, st := range e.stages {
		c, err := st.Classify(ctx, r)
		if errors.Is(err, stage.ErrUnclassified) {
			continue
		}
		if err != nil {
			return domain.Classification{}, "", fmt.Errorf("engine: %w", &stage.Error{Stage: st.Name(), Err: err})
		}
		if !e.gate.Admits(c.Confidence) {
			e.logger.Debug("classification below confidence gate, escalating",
				"record", r.ID, "stage", st.Name(), "category", c.Category,
				"confidence", c.Confidence, "gate", e.gate)
			continue
		}
		return c, st.Name(), nil
	}
	return domain.Classification{}, "", nil
}
