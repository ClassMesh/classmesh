package jsonl

import (
	"bytes"
	"context"
	"testing"
)

// loopReader replays its data forever, so a benchmark can drive one Source
// through arbitrarily many lines without recreating it inside the timed loop.
type loopReader struct {
	data []byte
	off  int
}

func (r *loopReader) Read(p []byte) (int, error) {
	if r.off == len(r.data) {
		r.off = 0
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

// BenchmarkNext measures the per-line cost of the structured input path: the
// ownership copy, the provenance ID, and the JSON decode into Fields (the
// dominant share).
func BenchmarkNext(b *testing.B) {
	line := []byte(`{"level":"error","http":{"status":503},"msg":"upstream timeout","user_id":"u1234"}` + "\n")
	s := New(&loopReader{data: bytes.Repeat(line, 1024)}, "bench")

	ctx := context.Background()
	b.ReportAllocs()
	b.SetBytes(int64(len(line)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Next(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
