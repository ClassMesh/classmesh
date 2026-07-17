package classmesh

import (
	"context"
	"fmt"
	"runtime"
	"testing"
)

func BenchmarkClassifyBatchConcurrent(b *testing.B) {
	c, err := New(newStatic("rules", map[string]string{"ping": "noise"}))
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	records := make([]Record, 100_000)
	for i := range records {
		records[i] = Record{ID: fmt.Sprintf("r%d", i), Data: []byte("ping")}
	}
	workers := runtime.GOMAXPROCS(0)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.ClassifyBatchConcurrent(ctx, records, workers)
	}
}
