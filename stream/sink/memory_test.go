package sink

import (
	"context"
	"errors"
	"testing"

	domain "github.com/ClassMesh/classmesh"
)

func TestInMemoryCollectsInOrder(t *testing.T) {
	s := NewInMemory()
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	writes := []Entry{
		{
			Record:         domain.Record{ID: "1", Data: []byte("a")},
			Classification: domain.Classification{Category: "noise", Confidence: 1, Stage: "rules"},
		},
		{
			Record:         domain.Record{ID: "2", Data: []byte("b")},
			Classification: domain.Classification{Category: "billing", Confidence: 0.8, Stage: "model"},
		},
	}
	for _, w := range writes {
		if err := s.Write(ctx, w.Record, w.Classification); err != nil {
			t.Fatalf("Write() error = %v, want nil", err)
		}
	}

	got := s.Entries()
	if len(got) != len(writes) {
		t.Fatalf("Entries() len = %d, want %d", len(got), len(writes))
	}
	for i := range writes {
		if got[i].Record.ID != writes[i].Record.ID {
			t.Fatalf("Entries()[%d].Record.ID = %q, want %q", i, got[i].Record.ID, writes[i].Record.ID)
		}
		if got[i].Classification.Category != writes[i].Classification.Category {
			t.Fatalf("Entries()[%d].Category = %q, want %q", i, got[i].Classification.Category, writes[i].Classification.Category)
		}
	}
}

func TestInMemoryHonorsContextCancellation(t *testing.T) {
	s := NewInMemory()
	t.Cleanup(func() { _ = s.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Write(ctx, domain.Record{}, domain.Classification{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Write() with cancelled ctx error = %v, want context.Canceled", err)
	}
	if len(s.Entries()) != 0 {
		t.Fatalf("Entries() len = %d after failed write, want 0", len(s.Entries()))
	}
}
