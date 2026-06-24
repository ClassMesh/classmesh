package jsonl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
)

func TestNextYieldsObjectsWithFieldsAndMetadata(t *testing.T) {
	in := `{"level":"error","msg":"boom"}
{"level":"info","n":42}`
	s := New(strings.NewReader(in), "events")
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	first, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if first.ID != "events:1" || first.Kind != domain.KindJSON {
		t.Fatalf("Next() ID=%q Kind=%q, want events:1 / json", first.ID, first.Kind)
	}
	if string(first.Data) != `{"level":"error","msg":"boom"}` {
		t.Fatalf("Next() Data = %q, want the original line", first.Data)
	}
	if first.Fields["level"] != "error" || first.Fields["msg"] != "boom" {
		t.Fatalf("Next() Fields = %v, want level=error msg=boom", first.Fields)
	}
	if first.Meta["source"] != "events" || first.Meta["line"] != "1" {
		t.Fatalf("Next() Meta = %v, want source=events line=1", first.Meta)
	}

	second, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("Next() #2 error = %v", err)
	}
	// JSON numbers decode as float64.
	if second.Fields["n"] != float64(42) {
		t.Fatalf("Next() #2 Fields[n] = %v (%T), want 42", second.Fields["n"], second.Fields["n"])
	}
	if _, err := s.Next(ctx); !errors.Is(err, source.ErrDrained) {
		t.Fatalf("Next() after last error = %v, want ErrDrained", err)
	}
}

func TestSkipsBlankLinesAndKeepsTrueLineNumbers(t *testing.T) {
	in := "\n{\"a\":1}\n   \n{\"b\":2}\n"
	s := New(strings.NewReader(in), "f")
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	r1, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if r1.Meta["line"] != "2" || r1.Fields["a"] != float64(1) {
		t.Fatalf("Next() = line %s fields %v, want line 2 a=1", r1.Meta["line"], r1.Fields)
	}
	r2, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("Next() #2 error = %v", err)
	}
	if r2.Meta["line"] != "4" || r2.Fields["b"] != float64(2) {
		t.Fatalf("Next() #2 = line %s fields %v, want line 4 b=2", r2.Meta["line"], r2.Fields)
	}
	if _, err := s.Next(ctx); !errors.Is(err, source.ErrDrained) {
		t.Fatalf("Next() after blanks error = %v, want ErrDrained", err)
	}
}

func TestMalformedLineFailsWithLocation(t *testing.T) {
	s := New(strings.NewReader("{\"ok\":1}\nnot json\n"), "bad")
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	if _, err := s.Next(ctx); err != nil {
		t.Fatalf("Next() #1 error = %v, want the good line first", err)
	}
	_, err := s.Next(ctx)
	if err == nil || errors.Is(err, source.ErrDrained) {
		t.Fatalf("Next() #2 error = %v, want a malformed-line error", err)
	}
	if !strings.Contains(err.Error(), "bad:2") {
		t.Fatalf("Next() #2 error = %v, want source:line in message", err)
	}
}

func TestNonObjectLineFails(t *testing.T) {
	// Arrays, numbers and strings fail at unmarshal; top-level null unmarshals
	// to a nil map and must be rejected explicitly.
	for _, in := range []string{"[1,2,3]", "5", `"str"`, "null"} {
		t.Run(in, func(t *testing.T) {
			s := New(strings.NewReader(in+"\n"), "x")
			t.Cleanup(func() { _ = s.Close() })
			if _, err := s.Next(context.Background()); err == nil {
				t.Fatalf("Next(%q) error = nil, want error for a non-object line", in)
			}
		})
	}
}

func TestOpenReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(`{"k":"v"}`+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	r, err := s.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if r.Fields["k"] != "v" || r.Meta["source"] != path {
		t.Fatalf("Next() = fields %v meta %v, want k=v source=%s", r.Fields, r.Meta, path)
	}
}

func TestOpenMissingFileFails(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Fatal("Open() error = nil, want error for missing file")
	}
}

func TestCloseDrainsAndClosesReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.jsonl")
	if err := os.WriteFile(path, []byte(`{"a":1}`+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() twice error = %v, want nil", err)
	}
	if _, err := s.Next(context.Background()); !errors.Is(err, source.ErrDrained) {
		t.Fatalf("Next() after Close error = %v, want ErrDrained", err)
	}
}

func TestNextHonorsContextCancellation(t *testing.T) {
	s := New(strings.NewReader(`{"a":1}`+"\n"), "ctx")
	t.Cleanup(func() { _ = s.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Next() with cancelled ctx error = %v, want context.Canceled", err)
	}
}
