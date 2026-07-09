package engine

import (
	"context"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
	"github.com/ClassMesh/classmesh/shared/pkg/stage/rules"
)

// benchSource yields the same record n times; it measures the pipeline, not
// record generation.
type benchSource struct {
	record domain.Record
	n      int
	i      int
}

var _ source.Source = (*benchSource)(nil)

func (s *benchSource) Next(ctx context.Context) (domain.Record, error) {
	if s.i >= s.n {
		return domain.Record{}, source.ErrDrained
	}
	s.i++
	return s.record, nil
}

func (s *benchSource) Close() error { return nil }

// discardSink accepts everything; it measures the pipeline, not output I/O.
type discardSink struct{}

var _ sink.Sink = discardSink{}

func (discardSink) Write(context.Context, domain.Record, domain.Classification) error { return nil }
func (discardSink) Close() error                                                      { return nil }

// BenchmarkEngineEndToEnd measures full per-record pipeline cost: engine
// loop + a realistic rules stage + sink write, with the record matching a
// mid-list rule.
func BenchmarkEngineEndToEnd(b *testing.B) {
	ruleStage, err := rules.New([]rules.Rule{
		{Category: "noise", Contains: []string{"healthz", "readiness"}},
		{Category: "billing", Regex: []string{`payment (failed|declined)`}},
		{Category: "auth", Contains: []string{"login failed"}, Regex: []string{`(?i)unauthorized`}},
		{Category: "db", Regex: []string{`(connection refused|deadlock detected)`}},
	})
	if err != nil {
		b.Fatal(err)
	}

	payload := []byte(`2026-06-12T10:00:00Z WARN payment declined order=84712 user=991 amount=49.90`)
	src := &benchSource{record: domain.Record{ID: "bench", Data: payload}, n: b.N}

	e, err := New(Deps{Source: src, Stages: []stage.Stage{ruleStage}, Sink: discardSink{}})
	if err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	stats, err := e.Run(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	if stats.Processed != b.N {
		b.Fatalf("processed = %d, want %d", stats.Processed, b.N)
	}
}
