package textfile

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "github.com/ClassMesh/classmesh"
	"github.com/ClassMesh/classmesh/shared/pkg/source"
)

// TestCloseUnblocksBlockedNext proves a Close (as the CLI does on context
// cancellation) frees a Next that is blocked on a reader with no data.
func TestCloseUnblocksBlockedNext(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	s := New(pr, "pipe")

	errc := make(chan error, 1)
	go func() {
		_, err := s.Next(context.Background())
		errc <- err
	}()

	_ = s.Close()

	select {
	case err := <-errc:
		if !errors.Is(err, source.ErrDrained) {
			t.Fatalf("Next() after Close error = %v, want ErrDrained", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Next() did not return after Close; it stayed blocked")
	}
}

func TestNewYieldsLinesWithProvenanceIDs(t *testing.T) {
	s := New(strings.NewReader("alpha\n\ngamma"), "test-stream")
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	want := []string{"alpha", "", "gamma"}
	ids := make([]string, 0, len(want))
	for i, payload := range want {
		r, err := s.Next(ctx)
		if err != nil {
			t.Fatalf("Next() #%d error = %v", i+1, err)
		}
		if string(r.Data) != payload {
			t.Fatalf("Next() #%d data = %q, want %q", i+1, r.Data, payload)
		}
		if r.Kind != domain.KindText {
			t.Fatalf("Next() #%d kind = %q, want %q", i+1, r.Kind, domain.KindText)
		}
		if r.Meta != nil {
			t.Fatalf("Next() #%d Meta = %v, want nil (provenance lives in the ID)", i+1, r.Meta)
		}
		ids = append(ids, r.ID)
	}
	for i, wantLine := range []string{"1", "2", "3"} {
		if ids[i] != "test-stream:"+wantLine {
			t.Fatalf("ID #%d = %q after later Next calls, want %q (IDs must not alias the scratch buffer)", i+1, ids[i], "test-stream:"+wantLine)
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
	if string(r.Data) != "GET /healthz 200" || r.ID != path+":1" {
		t.Fatalf("Next() = %q id=%q, want first line with id %s:1", r.Data, r.ID, path)
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
