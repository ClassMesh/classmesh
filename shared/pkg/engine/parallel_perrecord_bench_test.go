package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

func BenchmarkEnginePerRecordAllocs(b *testing.B) {
	payload := []byte(`2026-06-12T10:00:00Z WARN payment declined order=84712 user=991 amount=49.90`)
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
				src := &benchSource{record: domain.Record{ID: "bench", Data: payload}, n: b.N}
				e, err := New(Deps{
					Source:  src,
					Stages:  []stage.Stage{tc.stage()},
					Sink:    discardSink{},
					Logger:  discardLogger(),
					Workers: workers,
				})
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
			})
		}
	}
}
