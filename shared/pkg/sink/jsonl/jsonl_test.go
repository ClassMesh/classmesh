package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

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

	var first Entry
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 not valid JSON: %v", err)
	}
	if first.ID != "app.log:1" || first.Category != "noise" || first.Confidence != 1 || first.Stage != "rules" || first.Data != "GET /healthz 200" {
		t.Fatalf("line 1 = %+v, want full classified entry", first)
	}
	if first.Meta["line"] != "1" {
		t.Fatalf("line 1 meta = %v, want line=1", first.Meta)
	}

	var second Entry
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
