package textfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/source"
)

func TestNewYieldsLinesWithMetadata(t *testing.T) {
	s := New(strings.NewReader("alpha\n\ngamma"), "test-stream")
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	want := []string{"alpha", "", "gamma"}
	for i, payload := range want {
		r, err := s.Next(ctx)
		if err != nil {
			t.Fatalf("Next() #%d error = %v", i+1, err)
		}
		if string(r.Data) != payload {
			t.Fatalf("Next() #%d data = %q, want %q", i+1, r.Data, payload)
		}
		wantLine := []string{"1", "2", "3"}[i]
		if r.Meta["line"] != wantLine || r.Meta["source"] != "test-stream" {
			t.Fatalf("Next() #%d meta = %v, want line=%s source=test-stream", i+1, r.Meta, wantLine)
		}
		if r.ID != "test-stream:"+wantLine {
			t.Fatalf("Next() #%d ID = %q, want %q", i+1, r.ID, "test-stream:"+wantLine)
		}
	}
	if _, err := s.Next(ctx); !errors.Is(err, source.ErrDrained) {
		t.Fatalf("Next() after last line error = %v, want ErrDrained", err)
	}
}

func TestOpenReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("GET /healthz 200\npayment failed\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	r, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if string(r.Data) != "GET /healthz 200" || r.Meta["source"] != path {
		t.Fatalf("Next() = %q meta=%v, want first line with source=%s", r.Data, r.Meta, path)
	}
	if _, err := s.Next(ctx); err != nil {
		t.Fatalf("Next() #2 error = %v", err)
	}
	if _, err := s.Next(ctx); !errors.Is(err, source.ErrDrained) {
		t.Fatalf("Next() #3 error = %v, want ErrDrained", err)
	}
}

func TestOpenMissingFileFails(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "nope.log")); err == nil {
		t.Fatal("Open() error = nil, want error for missing file")
	}
}

func TestLongLinesUpToLimit(t *testing.T) {
	long := strings.Repeat("x", 200*1024) // 200KiB > bufio's 64KiB default
	s := New(strings.NewReader(long+"\nshort"), "long")
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	r, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error = %v, want long line to scan", err)
	}
	if len(r.Data) != len(long) {
		t.Fatalf("Next() len = %d, want %d", len(r.Data), len(long))
	}
	r, err = s.Next(ctx)
	if err != nil || string(r.Data) != "short" {
		t.Fatalf("Next() #2 = %q, %v, want short line", r.Data, err)
	}
}

func TestCloseDrainsAndClosesReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.log")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o600); err != nil {
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
	s := New(strings.NewReader("a\n"), "ctx")
	t.Cleanup(func() { _ = s.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Next() with cancelled ctx error = %v, want context.Canceled", err)
	}
}
