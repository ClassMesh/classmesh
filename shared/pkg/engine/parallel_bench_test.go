package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

// spinStage burns CPU per record, standing in for model-tier inference.
type spinStage struct{ iters int }

func (s *spinStage) Name() string { return "spin" }

func (s *spinStage) Classify(ctx context.Context, r domain.Record) (domain.Classification, error) {
	if err := ctx.Err(); err != nil {
		return domain.Classification{}, err
	}
	h := uint64(14695981039346656037)
	for i := 0; i < s.iters; i++ {
		for _, c := range r.Data {
			h = (h ^ uint64(c)) * 1099511628211
		}
	}
	if h == 0 {
		return domain.Classification{}, stage.ErrUnclassified
	}
	return domain.Classification{Category: "ok", Confidence: 1}, nil
}

type replaySource struct {
	recs []domain.Record
	i    int
}

func (s *replaySource) Next(context.Context) (domain.Record, error) {
	if s.i >= len(s.recs) {
		return domain.Record{}, source.ErrDrained
	}
	r := s.recs[s.i]
	s.i++
	return r, nil
}

func (s *replaySource) Close() error { return nil }

// BenchmarkEngineWorkers compares the serial loop against the parallel
// pipeline on a cheap rules-like stage and a CPU-heavy model-like stage.
func BenchmarkEngineWorkers(b *testing.B) {
	const n = 10000
	recs := sequentialRecords(n)
	cases := []struct {
		name  string
		stage func() stage.Stage
	}{
		{"cheap-stage", func() stage.Stage { return &spinStage{} }},
		{"cpu-heavy-stage", func() stage.Stage { return &spinStage{iters: 400} }},
	}
	for _, tc := range cases {
		for _, workers := range []int{0, 1, 2, 4, 8, 16} {
			b.Run(fmt.Sprintf("%s/workers=%d", tc.name, workers), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					e, err := New(Deps{
						Source:  &replaySource{recs: recs},
						Stages:  []stage.Stage{tc.stage()},
						Sink:    discardSink{},
						Logger:  discardLogger(),
						Workers: workers,
					})
					if err != nil {
						b.Fatal(err)
					}
					if _, err := e.Run(context.Background()); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
