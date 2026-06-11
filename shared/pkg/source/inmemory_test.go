package source

import (
	"context"
	"errors"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

func TestInMemoryYieldsInOrderThenDrains(t *testing.T) {
	src := NewInMemory([]domain.Record{
		{ID: "1", Data: []byte("a")},
		{ID: "2", Data: []byte("b")},
	})
	t.Cleanup(func() { _ = src.Close() })

	ctx := context.Background()
	for _, wantID := range []string{"1", "2"} {
		r, err := src.Next(ctx)
		if err != nil {
			t.Fatalf("Next() error = %v, want nil", err)
		}
		if r.ID != wantID {
			t.Fatalf("Next() ID = %q, want %q", r.ID, wantID)
		}
	}
	if _, err := src.Next(ctx); !errors.Is(err, ErrDrained) {
		t.Fatalf("Next() after drain error = %v, want ErrDrained", err)
	}
}

func TestInMemoryClosedSourceDrains(t *testing.T) {
	src := NewInMemory([]domain.Record{{ID: "1"}})
	if err := src.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if _, err := src.Next(context.Background()); !errors.Is(err, ErrDrained) {
		t.Fatalf("Next() after Close error = %v, want ErrDrained", err)
	}
}

func TestInMemoryHonorsContextCancellation(t *testing.T) {
	src := NewInMemory([]domain.Record{{ID: "1"}})
	t.Cleanup(func() { _ = src.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Next() with cancelled ctx error = %v, want context.Canceled", err)
	}
}
