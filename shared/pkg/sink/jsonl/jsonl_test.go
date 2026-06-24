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

func TestWriteOutputUnchangedByReasons(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)
	c := domain.Classification{
		Category:   "noise",
		Confidence: 1,
		Stage:      "rules",
		Reasons:    []domain.Reason{{Code: "r1", Detail: "matched contains"}},
	}
	if err := s.Write(context.Background(), domain.Record{ID: "x", Data: []byte("hi")}, c); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if _, ok := got["reasons"]; ok {
		t.Fatalf("output has reasons key %v, want existing shape preserved", got)
	}
	if got["category"] != "noise" || got["stage"] != "rules" {
		t.Fatalf("output = %v, want category=noise stage=rules", got)
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
