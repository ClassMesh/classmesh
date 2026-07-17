package jsonl

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	domain "github.com/ClassMesh/classmesh"
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

// BenchmarkWriteStructured measures a record carrying decoded Fields through
// the fast path that proves byte-equality with encoding/json.
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

// BenchmarkWriteStructuredNumbers mirrors BenchmarkWriteStructured but its
// Fields carry json.Number values (the jsonl source's real output type via
// dec.UseNumber), so the proof reflects production data rather than float64.
func BenchmarkWriteStructuredNumbers(b *testing.B) {
	s := New(io.Discard)
	defer func() { _ = s.Close() }()

	r := domain.Record{
		ID:     "events:1",
		Kind:   domain.KindJSON,
		Data:   []byte(`{"level":"error","http":{"status":503},"msg":"upstream timeout"}`),
		Fields: map[string]any{"level": "error", "http": map[string]any{"status": json.Number("503")}, "msg": "upstream timeout"},
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
