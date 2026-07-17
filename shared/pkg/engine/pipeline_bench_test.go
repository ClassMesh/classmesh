package engine_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	domain "github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/rules"
	"github.com/ClassMesh/classmesh/shared/pkg/engine"
	"github.com/ClassMesh/classmesh/shared/pkg/sink"
	jsonlsink "github.com/ClassMesh/classmesh/shared/pkg/sink/jsonl"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
	jsonlsrc "github.com/ClassMesh/classmesh/shared/pkg/source/jsonl"
	"github.com/ClassMesh/classmesh/shared/pkg/source/textfile"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

// loopReader replays its data forever so a bounded source can draw an
// arbitrary number of lines without holding them all in memory.
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

func lineReader(line string) *loopReader {
	return &loopReader{data: bytes.Repeat([]byte(line+"\n"), 1024)}
}

// boundedSource caps an underlying source at n records so engine.Run drains
// after exactly n records.
type boundedSource struct {
	inner source.Source
	n, i  int
}

func newBounded(inner source.Source, n int) *boundedSource {
	return &boundedSource{inner: inner, n: n}
}

func (s *boundedSource) Next(ctx context.Context) (domain.Record, error) {
	if s.i >= s.n {
		return domain.Record{}, source.ErrDrained
	}
	s.i++
	return s.inner.Next(ctx)
}

func (s *boundedSource) Close() error { return s.inner.Close() }

// realisticRules is the shape of a log-triage config: a health-check noise
// rule the text sample hits, plus regex rules a record walks past on a miss.
func realisticRules(tb testing.TB) *rules.Stage {
	tb.Helper()
	s, err := rules.New([]rules.Rule{
		{Category: "noise", Contains: []string{"healthz", "readiness", "liveness"}},
		{Category: "billing", Regex: []string{`payment (failed|declined)`}},
		{Category: "auth", Contains: []string{"login failed"}, Regex: []string{`(?i)unauthorized`}},
		{Category: "db", Regex: []string{`(connection refused|deadlock detected)`}},
	})
	if err != nil {
		tb.Fatalf("rules.New() error = %v", err)
	}
	return s
}

func runEngine(b *testing.B, src source.Source, st stage.Stage, out sink.Sink) {
	b.Helper()
	e, err := engine.New(engine.Deps{Source: src, Stages: []stage.Stage{st}, Sink: out})
	if err != nil {
		b.Fatal(err)
	}
	stats, err := e.Run(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	if stats.Processed != b.N {
		b.Fatalf("processed = %d, want %d", stats.Processed, b.N)
	}
}

// BenchmarkPipelineTextRulesJSONL is text source -> rules -> JSONL sink, the
// single-tier CLI path (./classmesh run --rules ... file.txt) end to end. The
// sample hits the first rule, so a classified record is encoded every time.
func BenchmarkPipelineTextRulesJSONL(b *testing.B) {
	line := `10.2.3.4 - - [12/Jun/2026:10:00:00] "GET /healthz HTTP/1.1" 200 2 "-" "kube-probe/1.29"`
	src := newBounded(textfile.New(lineReader(line), "bench"), b.N)
	sink := jsonlsink.New(io.Discard)
	defer func() { _ = sink.Close() }()
	st := realisticRules(b)

	b.SetBytes(int64(len(line) + 1))
	b.ReportAllocs()
	b.ResetTimer()
	runEngine(b, src, st, sink)
}

// BenchmarkPipelineJSONLFieldsJSONL is JSONL source -> field rules -> JSONL
// sink, the structured path (./classmesh run --input jsonl ...) end to end:
// decode into Fields, match on a field, re-encode with the decoded Fields.
func BenchmarkPipelineJSONLFieldsJSONL(b *testing.B) {
	line := `{"level":"error","http":{"status":503},"msg":"upstream timeout","user_id":"u1234"}`
	gte := 500.0
	st, err := rules.New([]rules.Rule{
		{Category: "alert", Fields: []rules.FieldMatcher{{Path: "http.status", Gte: &gte}}},
	})
	if err != nil {
		b.Fatal(err)
	}
	src := newBounded(jsonlsrc.New(lineReader(line), "bench"), b.N)
	sink := jsonlsink.New(io.Discard)
	defer func() { _ = sink.Close() }()

	b.SetBytes(int64(len(line) + 1))
	b.ReportAllocs()
	b.ResetTimer()
	runEngine(b, src, st, sink)
}

// BenchmarkPipelineSink contrasts the same text pipeline draining into
// io.Discard against a real file. The file case measures buffered writes into
// the OS page cache, not durable disk latency: nothing calls fsync.
func BenchmarkPipelineSink(b *testing.B) {
	line := `10.2.3.4 - - [12/Jun/2026:10:00:00] "GET /healthz HTTP/1.1" 200 2 "-" "kube-probe/1.29"`
	cases := []struct {
		name   string
		writer func(b *testing.B) io.Writer
	}{
		{"discard", func(b *testing.B) io.Writer { return io.Discard }},
		{"file", func(b *testing.B) io.Writer {
			f, err := os.Create(filepath.Join(b.TempDir(), "out.jsonl"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = f.Close() })
			return f
		}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			src := newBounded(textfile.New(lineReader(line), "bench"), b.N)
			sink := jsonlsink.New(tc.writer(b))
			defer func() { _ = sink.Close() }()
			st := realisticRules(b)

			b.SetBytes(int64(len(line) + 1))
			b.ReportAllocs()
			b.ResetTimer()
			runEngine(b, src, st, sink)
		})
	}
}
