package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	domain "github.com/ClassMesh/classmesh"
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
			// KindText is exactly what the text source produces, so this
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
		{"confidence exactly zero", domain.Record{ID: "x", Data: []byte("d")}, domain.Classification{Category: "x", Confidence: 0}},
		{"confidence exactly one fast path pin", domain.Record{ID: "x", Data: []byte("d")}, domain.Classification{Category: "x", Confidence: 1}},
		{"negative zero confidence", domain.Record{ID: "x", Data: []byte("d")}, domain.Classification{Category: "x", Confidence: math.Copysign(0, -1)}},

		// Broad Fields corpus: every value the fast path recognizes, plus the
		// edges (malformed/empty json.Number, non-finite float, unsupported
		// type) that must rewind and defer to encoding/json for identical bytes,
		// or error when the reference errors (messages are not compared).
		{"fields json.Number ints and signs", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"a": json.Number("0"), "b": json.Number("-0"), "c": json.Number("42"), "d": json.Number("-42")}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields json.Number decimals and exponents", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"a": json.Number("3.14"), "b": json.Number("-3.14"), "c": json.Number("1e10"), "d": json.Number("1E-10"), "e": json.Number("-2.5e+3")}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields empty json.Number becomes zero", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"a": json.Number("")}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields malformed json.Number falls back to error", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"a": json.Number("1.2.3")}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields malformed json.Number leading zero", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"a": json.Number("01")}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields bool and nil", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"t": true, "f": false, "n": nil}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields nested maps and arrays", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"outer": map[string]any{"inner": map[string]any{"leaf": json.Number("7")}}, "list": []any{json.Number("1"), "two", true, nil}}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields array of mixed scalars and objects", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"xs": []any{map[string]any{"k": "v"}, json.Number("3"), []any{false, nil}, "s"}}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields keys needing escaping", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"q\"q": 1.0, "ctl\x01byte": 2.0, "uni\u65e5\u672c": 3.0, "sep\u2028par\u2029": 4.0, "<html>&amp;": 5.0, "": 6.0}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields float64 special values", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"zero": float64(0), "negzero": math.Copysign(0, -1), "one": float64(1), "tiny": 1e-9, "huge": 1e21, "big": 1.5e300, "frac": 3.5}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields NaN float surfaces error", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"bad": math.NaN()}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields Inf float surfaces error", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"bad": math.Inf(1)}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields unsupported type falls back", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"i": int(5), "f32": float32(1.5)}}, domain.Classification{Category: "x", Confidence: 1}},
		{"fields empty nested map and array", domain.Record{ID: "x", Data: []byte("d"), Fields: map[string]any{"m": map[string]any{}, "a": []any{}}}, domain.Classification{Category: "x", Confidence: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bytes.Buffer
			s := New(&got)
			writeErr := s.Write(context.Background(), tc.r, tc.c)
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
			refErr := enc.Encode(ref)
			if refErr != nil {
				if writeErr == nil {
					t.Fatalf("reference encoder errored (%v) but sink succeeded: %q", refErr, got.String())
				}
				return
			}
			if writeErr != nil {
				t.Fatalf("Write() error = %v, but reference encoder succeeded", writeErr)
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

// TestStructuredFastPathZeroAllocs pins the hand-rolled Fields encoder to 0
// heap allocations per record over a nested structured record, mirroring the
// plain path's 0-alloc guarantee.
func TestStructuredFastPathZeroAllocs(t *testing.T) {
	s := New(io.Discard)
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	r := domain.Record{
		ID:     "events:1",
		Kind:   domain.KindJSON,
		Data:   []byte(`{"level":"error"}`),
		Fields: map[string]any{"level": "error", "http": map[string]any{"status": json.Number("503")}, "tags": []any{"a", "b"}},
		Meta:   map[string]string{"source": "events", "line": "1"},
	}
	c := domain.Classification{Category: "alert", Confidence: 1, Stage: "rules"}
	if n := testing.AllocsPerRun(200, func() {
		if err := s.Write(ctx, r, c); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}); n != 0 {
		t.Fatalf("structured fast path allocs = %v, want 0", n)
	}
}

// fieldsGen builds a map[string]any from fuzz bytes, drawing from the confirmed
// Fields type universe (string, json.Number, bool, nil, nested map, slice) plus
// a few unsupported types (int, float32) and non-finite floats to exercise the
// encoding/json fallback and its error path.
type fieldsGen struct {
	b []byte
	i int
}

func (g *fieldsGen) next() byte {
	if g.i >= len(g.b) {
		return 0
	}
	v := g.b[g.i]
	g.i++
	return v
}

func (g *fieldsGen) str() string {
	n := int(g.next() % 8)
	out := make([]byte, n)
	for i := range out {
		out[i] = g.next()
	}
	return string(out)
}

func (g *fieldsGen) key() string {
	specials := []string{"a", "b", "q\"q", "ctl\x01", "sep  ", "<h>&amp;", "日本", ""}
	if g.next()%2 == 0 {
		return specials[int(g.next())%len(specials)]
	}
	return g.str()
}

func (g *fieldsGen) number() json.Number {
	choices := []string{"0", "-0", "1", "-1", "42", "-42", "3.14", "-3.14", "1e10", "1E-10", "-2.5e+3", "", "1.", "01", "abc", "-", "1e", "+5", "1.2.3"}
	return json.Number(choices[int(g.next())%len(choices)])
}

func (g *fieldsGen) float() float64 {
	choices := []float64{0, math.Copysign(0, -1), 1, -1, 3.5, 1e-9, 1e21, 1.5e300, math.NaN(), math.Inf(1), math.Inf(-1)}
	return choices[int(g.next())%len(choices)]
}

func (g *fieldsGen) value(depth int) any {
	sel := g.next()
	if depth >= 6 {
		sel %= 6 // stop nesting: only scalars past this depth
	}
	switch sel % 10 {
	case 0:
		return g.str()
	case 1, 9:
		return g.number()
	case 2:
		return g.next()%2 == 0
	case 3:
		return nil
	case 4:
		return g.float()
	case 5:
		return int(int8(g.next())) // unsupported by fast path -> fallback
	case 6:
		return g.object(depth + 1)
	case 7:
		return g.array(depth + 1)
	case 8:
		return float32(g.next()) // unsupported by fast path -> fallback
	}
	return nil
}

func (g *fieldsGen) object(depth int) map[string]any {
	n := int(g.next() % 5)
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		m[g.key()] = g.value(depth)
	}
	return m
}

func (g *fieldsGen) array(depth int) []any {
	n := int(g.next() % 5)
	a := make([]any, n)
	for i := range a {
		a[i] = g.value(depth)
	}
	return a
}

// FuzzFieldsMatchEncodingJSON is the guard behind the byte-for-byte claim: for
// any generated Fields map the sink output must equal encoding/json
// (SetEscapeHTML(false)) exactly, or both must surface an error.
func FuzzFieldsMatchEncodingJSON(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	f.Add([]byte{6, 2, 0, 65, 1, 7, 3, 4, 5, 9, 8, 6, 6})
	f.Add(bytes.Repeat([]byte{4, 1, 9, 6, 7}, 8))

	f.Fuzz(func(t *testing.T, data []byte) {
		g := &fieldsGen{b: data}
		fields := g.object(0)
		if len(fields) == 0 {
			return // empty Fields is omitted from the wire; nothing to compare
		}
		r := domain.Record{ID: "x", Data: []byte("d"), Fields: fields}
		c := domain.Classification{Category: "x", Confidence: 1}

		var got bytes.Buffer
		s := New(&got)
		writeErr := s.Write(context.Background(), r, c)
		if err := s.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		ref := entry{ID: r.ID, Data: string(r.Data), Fields: fields, Category: c.Category, Confidence: c.Confidence}
		var want bytes.Buffer
		enc := json.NewEncoder(&want)
		enc.SetEscapeHTML(false)
		refErr := enc.Encode(ref)

		if refErr != nil {
			if writeErr == nil {
				t.Fatalf("reference errored (%v) but sink succeeded: %q\nfields=%#v", refErr, got.String(), fields)
			}
			return
		}
		if writeErr != nil {
			t.Fatalf("sink errored (%v) but reference succeeded\nfields=%#v", writeErr, fields)
		}
		if got.String() != want.String() {
			t.Fatalf("encoder drift\n got: %q\nwant: %q\nfields=%#v", got.String(), want.String(), fields)
		}
	})
}

func nestedMapChain(depth int) map[string]any {
	v := map[string]any{"leaf": "end"}
	for i := 0; i < depth; i++ {
		v = map[string]any{"k": v}
	}
	return v
}

func nestedArrayChain(depth int) []any {
	v := []any{"end"}
	for i := 0; i < depth; i++ {
		v = []any{v}
	}
	return v
}

// TestFieldsCyclesAndDepth pins the fallback boundary: cyclic Fields must
// return an error without crashing, and deep acyclic Fields must stay
// byte-identical to encoding/json on both sides of maxFastDepth.
func TestFieldsCyclesAndDepth(t *testing.T) {
	selfMap := map[string]any{}
	selfMap["self"] = selfMap
	selfSlice := []any{nil}
	selfSlice[0] = selfSlice
	mutualMap := map[string]any{}
	mutualSlice := []any{mutualMap}
	mutualMap["back"] = mutualSlice

	cyclic := []struct {
		name   string
		fields map[string]any
	}{
		{"self-referential map", selfMap},
		{"self-referential slice", map[string]any{"a": selfSlice}},
		{"mutual map and slice cycle", mutualMap},
	}
	for _, tc := range cyclic {
		t.Run(tc.name, func(t *testing.T) {
			var got bytes.Buffer
			s := New(&got)
			err := s.Write(context.Background(), domain.Record{ID: "x", Data: []byte("d"), Fields: tc.fields},
				domain.Classification{Category: "x", Confidence: 1})
			if err == nil {
				t.Fatalf("Write() = nil error for cyclic Fields, want the encoding/json cycle error")
			}
		})
	}

	deep := []struct {
		name   string
		fields map[string]any
	}{
		{"map chain below the limit", map[string]any{"root": nestedMapChain(maxFastDepth - 3)}},
		{"map chain at the limit", map[string]any{"root": nestedMapChain(maxFastDepth)}},
		{"map chain past the limit", map[string]any{"root": nestedMapChain(maxFastDepth + 1)}},
		{"map chain far past the limit", map[string]any{"root": nestedMapChain(200)}},
		{"array chain past the limit", map[string]any{"root": nestedArrayChain(maxFastDepth + 10)}},
		{"mixed chain past the limit", map[string]any{"root": []any{nestedMapChain(maxFastDepth)}}},
	}
	for _, tc := range deep {
		t.Run(tc.name, func(t *testing.T) {
			var got bytes.Buffer
			s := New(&got)
			if err := s.Write(context.Background(), domain.Record{ID: "x", Data: []byte("d"), Fields: tc.fields},
				domain.Classification{Category: "x", Confidence: 1}); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			ref := entry{ID: "x", Data: "d", Fields: tc.fields, Category: "x", Confidence: 1}
			var want bytes.Buffer
			enc := json.NewEncoder(&want)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(ref); err != nil {
				t.Fatalf("reference encode error = %v", err)
			}
			if got.String() != want.String() {
				t.Fatalf("encoder drift\n got: %q\nwant: %q", got.String(), want.String())
			}
		})
	}
}

func TestReleaseKeysClearsReferences(t *testing.T) {
	s := New(io.Discard)
	fields := map[string]any{
		"outer": map[string]any{"inner": map[string]any{"leaf": "v"}},
		"other": "x",
	}
	if err := s.Write(context.Background(), domain.Record{ID: "x", Data: []byte("d"), Fields: fields},
		domain.Classification{Category: "c", Confidence: 1}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(s.keyStack) == 0 {
		t.Fatal("keyStack unused; expected the nested write to exercise it")
	}
	for depth, keys := range s.keyStack {
		if len(keys) != 0 {
			t.Fatalf("keyStack[%d] len = %d after Write, want 0", depth, len(keys))
		}
		full := keys[:cap(keys)]
		for i, k := range full {
			if k != "" {
				t.Fatalf("keyStack[%d][%d] retains %q after release", depth, i, k)
			}
		}
	}

	fields["outer"].(map[string]any)["inner"].(map[string]any)["bad"] = struct{}{}
	if err := s.Write(context.Background(), domain.Record{ID: "y", Data: []byte("d"), Fields: fields},
		domain.Classification{Category: "c", Confidence: 1}); err != nil {
		t.Fatalf("Write() with fallback error = %v", err)
	}
	for depth, keys := range s.keyStack {
		if len(keys) != 0 {
			t.Fatalf("keyStack[%d] len = %d after fallback unwind, want 0", depth, len(keys))
		}
		full := keys[:cap(keys)]
		for i, k := range full {
			if k != "" {
				t.Fatalf("keyStack[%d][%d] retains %q after fallback unwind", depth, i, k)
			}
		}
	}
}

func TestIsValidNumber(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"0", true},
		{"-0", true},
		{"42", true},
		{"-42", true},
		{"3.14", true},
		{"1e10", true},
		{"1E-10", true},
		{"-2.5e+3", true},
		{"0.5", true},
		{"", false},
		{"-", false},
		{"01", false},
		{"1.", false},
		{".5", false},
		{"1e", false},
		{"1e+", false},
		{"+5", false},
		{"1.2.3", false},
		{"abc", false},
		{"0x10", false},
		{"NaN", false},
		{"Infinity", false},
	}
	for _, tc := range cases {
		if got := isValidNumber(tc.in); got != tc.want {
			t.Errorf("isValidNumber(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
