package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// entry mirrors the documented wire shape for decoding in tests.
type entry struct {
	ID         string            `json:"id"`
	Kind       domain.Kind       `json:"kind,omitempty"`
	Data       string            `json:"data"`
	Fields     map[string]any    `json:"fields,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
	Category   string            `json:"category"`
	Confidence float64           `json:"confidence"`
	Stage      string            `json:"stage,omitempty"`
	Reasons    []domain.Reason   `json:"reasons,omitempty"`
}

func TestWriteEmitsOneJSONObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)
	ctx := context.Background()

	writes := []struct {
		r domain.Record
		c domain.Classification
	}{
		{
			domain.Record{ID: "app.log:1", Data: []byte("GET /healthz 200"), Meta: map[string]string{"line": "1"}},
			domain.Classification{Category: "noise", Confidence: 1, Stage: "rules"},
		},
		{
			domain.Record{ID: "app.log:2", Data: []byte("weird payload")},
			domain.Classification{}, // review entry: zero classification
		},
	}
	for _, w := range writes {
		if err := s.Write(ctx, w.r, w.c); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output lines = %d, want 2; output=%q", len(lines), buf.String())
	}

	var first entry
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 not valid JSON: %v", err)
	}
	if first.ID != "app.log:1" || first.Category != "noise" || first.Confidence != 1 || first.Stage != "rules" || first.Data != "GET /healthz 200" {
		t.Fatalf("line 1 = %+v, want full classified entry", first)
	}
	if first.Meta["line"] != "1" {
		t.Fatalf("line 1 meta = %v, want line=1", first.Meta)
	}

	var second entry
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("line 2 not valid JSON: %v", err)
	}
	if second.Category != "" || second.Confidence != 0 {
		t.Fatalf("line 2 = %+v, want zero classification preserved", second)
	}
}

func TestWriteGolden(t *testing.T) {
	cases := []struct {
		name string
		r    domain.Record
		c    domain.Classification
		want string
	}{
		{
			"classified event with kind, fields and reasons",
			domain.Record{ID: "e:1", Kind: domain.KindJSON, Data: []byte(`{"a":1}`), Fields: map[string]any{"a": float64(1)}, Meta: map[string]string{"line": "1"}},
			domain.Classification{Category: "x", Confidence: 1, Stage: "rules", Reasons: []domain.Reason{{Code: "r1", Detail: "d"}}},
			`{"id":"e:1","kind":"json","data":"{\"a\":1}","fields":{"a":1},"meta":{"line":"1"},"category":"x","confidence":1,"stage":"rules","reasons":[{"code":"r1","detail":"d"}]}` + "\n",
		},
		{
			// KindText is exactly what the textfile source produces, so this
			// is the default log path: kind must not appear on the wire.
			"plain log record (KindText) keeps the prior shape",
			domain.Record{ID: "app.log:1", Kind: domain.KindText, Data: []byte("GET /healthz 200"), Meta: map[string]string{"line": "1"}},
			domain.Classification{Category: "noise", Confidence: 1, Stage: "rules"},
			`{"id":"app.log:1","data":"GET /healthz 200","meta":{"line":"1"},"category":"noise","confidence":1,"stage":"rules"}` + "\n",
		},
		{
			"review entry with zero classification",
			domain.Record{ID: "app.log:2", Data: []byte("weird")},
			domain.Classification{},
			`{"id":"app.log:2","data":"weird","category":"","confidence":0}` + "\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := New(&buf)
			if err := s.Write(context.Background(), tc.r, tc.c); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			if got := buf.String(); got != tc.want {
				t.Fatalf("output mismatch\n got: %s want: %s", got, tc.want)
			}
		})
	}
}

// TestWriteMatchesEncodingJSON pins the hand-rolled encoder byte-for-byte to
// what encoding/json (HTML escaping off) produces for the same wire struct,
// across escaping and formatting edge cases.
func TestWriteMatchesEncodingJSON(t *testing.T) {
	cases := []struct {
		name string
		r    domain.Record
		c    domain.Classification
	}{
		{"escapes in data and id", domain.Record{ID: "a\"b\\c:1", Data: []byte("tab\there\nnewline\rret \x01ctl \"quoted\" back\\slash")}, domain.Classification{Category: "x", Confidence: 0.5}},
		{"invalid utf-8 payload", domain.Record{ID: "x", Data: []byte{'a', 0xFF, 0xFE, 'b'}}, domain.Classification{Category: "x", Confidence: 1}},
		{"line and paragraph separators", domain.Record{ID: "x", Data: []byte("a\u2028b\u2029c")}, domain.Classification{Category: "s\u2028p", Confidence: 1}},
		{"html-ish characters stay literal", domain.Record{ID: "x", Data: []byte(`<a href="x">&amp;</a>`)}, domain.Classification{Category: "a>b", Confidence: 1}},
		{"unicode passthrough", domain.Record{ID: "x", Data: []byte("héllo wörld ⚡ 日本語")}, domain.Classification{Category: "café", Confidence: 0.25}},
		{"structured record", domain.Record{ID: "e:1", Kind: domain.KindJSON, Data: []byte(`{"a":1}`), Fields: map[string]any{"z": "s", "a": json.Number("42"), "n": map[string]any{"x": true}}, Meta: map[string]string{"source": "f", "line": "9"}}, domain.Classification{Category: "x", Confidence: 0.93, Stage: "s", Reasons: []domain.Reason{{Code: "c"}, {Code: "c2", Detail: "d e"}}}},
		{"review entry", domain.Record{ID: "r", Data: []byte("weird")}, domain.Classification{}},
		{"tiny and huge confidences", domain.Record{ID: "x", Data: []byte("d")}, domain.Classification{Category: "x", Confidence: 1e-7}},
		{"smallest nonzero", domain.Record{ID: "x", Data: []byte("d")}, domain.Classification{Category: "x", Confidence: math.SmallestNonzeroFloat64}},
		{"every control byte plus DEL", domain.Record{ID: "x", Data: controlBytes()}, domain.Classification{Category: "x", Confidence: 1}},
		{"empty but non-nil maps are omitted", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{}, Meta: map[string]string{}}, domain.Classification{Category: "x", Confidence: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bytes.Buffer
			s := New(&got)
			if err := s.Write(context.Background(), tc.r, tc.c); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}

			kind := tc.r.Kind
			if kind == domain.KindText {
				kind = ""
			}
			ref := entry{ID: tc.r.ID, Kind: kind, Data: string(tc.r.Data), Fields: tc.r.Fields, Meta: tc.r.Meta,
				Category: tc.c.Category, Confidence: tc.c.Confidence, Stage: tc.c.Stage, Reasons: tc.c.Reasons}
			var want bytes.Buffer
			enc := json.NewEncoder(&want)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(ref); err != nil {
				t.Fatalf("reference encode: %v", err)
			}
			if got.String() != want.String() {
				t.Fatalf("encoder drift\n got: %q\nwant: %q", got.String(), want.String())
			}
		})
	}
}

func controlBytes() []byte {
	b := make([]byte, 0, 33)
	for c := byte(0); c < 0x20; c++ {
		b = append(b, c)
	}
	return append(b, 0x7f)
}

func TestWriteRejectsNonFiniteConfidence(t *testing.T) {
	for _, conf := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		var buf bytes.Buffer
		s := New(&buf)
		err := s.Write(context.Background(), domain.Record{ID: "x", Data: []byte("d")}, domain.Classification{Category: "c", Confidence: conf})
		if err == nil {
			t.Fatalf("Write(confidence=%v) error = nil, want unsupported-value error", conf)
		}
		if cerr := s.Close(); cerr != nil {
			t.Fatalf("Close() error = %v", cerr)
		}
		if buf.Len() != 0 {
			t.Fatalf("Write(confidence=%v) left partial output %q, want none", conf, buf.String())
		}
	}
}

func TestWriteFlushesWhenBufferFills(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)
	data := bytes.Repeat([]byte("x"), 8<<10)
	for i := 0; i < 12; i++ {
		if err := s.Write(context.Background(), domain.Record{ID: "big", Data: data}, domain.Classification{Category: "c", Confidence: 1}); err != nil {
			t.Fatalf("Write(%d) error = %v", i, err)
		}
	}
	if buf.Len() == 0 {
		t.Fatal("nothing flushed before Close despite writes exceeding the buffer size")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 12 {
		t.Fatalf("output lines = %d, want 12", len(lines))
	}
	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("line %d is not valid JSON (flush split a record?)", i+1)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteSurfacesFlushError(t *testing.T) {
	s := New(failingWriter{})
	data := bytes.Repeat([]byte("x"), writeBufferSize)
	err := s.Write(context.Background(), domain.Record{ID: "x", Data: data}, domain.Classification{Category: "c", Confidence: 1})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Write() error = %v, want the writer failure surfaced", err)
	}
}

func TestCloseFlushesAndIsIdempotent(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)
	if err := s.Write(context.Background(), domain.Record{ID: "x"}, domain.Classification{Category: "a", Confidence: 1}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("output before Close = %q, want buffered (empty)", buf.String())
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("output after Close is empty, want flushed line")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() twice error = %v, want nil", err)
	}
}

func TestWriteHonorsContextCancellation(t *testing.T) {
	s := New(&bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Write(ctx, domain.Record{}, domain.Classification{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Write() error = %v, want context.Canceled", err)
	}
}
