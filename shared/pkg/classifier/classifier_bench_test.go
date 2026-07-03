package classifier

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

func BenchmarkClassifyBatchConcurrent(b *testing.B) {
	c, err := New(Deps{Stages: []stage.Stage{
		stage.NewStatic("rules", map[string]string{"ping": "noise"}),
	}})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	records := make([]domain.Record, 100_000)
	for i := range records {
		records[i] = domain.Record{ID: fmt.Sprintf("r%d", i), Data: []byte("ping")}
	}
	workers := runtime.GOMAXPROCS(0)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.ClassifyBatchConcurrent(ctx, records, workers)
	}
}
