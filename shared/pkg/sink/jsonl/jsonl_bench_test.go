package jsonl

import (
	"context"
	"io"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// BenchmarkWrite measures the per-record cost of encoding a classified record
// (with fields and a reason) as one JSON line, to a discarding writer.
func BenchmarkWrite(b *testing.B) {
	s := New(io.Discard)
	defer func() { _ = s.Close() }()

	r := domain.Record{
		ID:   "events:1",
		Kind: domain.KindJSON,
		Data: []byte(`{"level":"error","http":{"status":503},"msg":"upstream timeout"}`),
		Meta: map[string]string{"source": "events", "line": "1"},
	}
	c := domain.Classification{
		Category:   "alert",
		Confidence: 1,
		Stage:      "rules",
		Reasons:    []domain.Reason{{Code: "server-error", Detail: "field http.status >= 500"}},
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.SetBytes(int64(len(r.Data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Write(ctx, r, c); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWriteStructured measures a record carrying decoded Fields, whose
// free-form map still serializes through encoding/json and so allocates.
func BenchmarkWriteStructured(b *testing.B) {
	s := New(io.Discard)
	defer func() { _ = s.Close() }()

	r := domain.Record{
		ID:     "events:1",
		Kind:   domain.KindJSON,
		Data:   []byte(`{"level":"error","http":{"status":503},"msg":"upstream timeout"}`),
		Fields: map[string]any{"level": "error", "http": map[string]any{"status": float64(503)}, "msg": "upstream timeout"},
		Meta:   map[string]string{"source": "events", "line": "1"},
	}
	c := domain.Classification{
		Category:   "alert",
		Confidence: 1,
		Stage:      "rules",
		Reasons:    []domain.Reason{{Code: "server-error", Detail: "field http.status >= 500"}},
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.SetBytes(int64(len(r.Data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Write(ctx, r, c); err != nil {
			b.Fatal(err)
		}
	}
}
