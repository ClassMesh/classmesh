package stage

import (
	"context"
	"errors"
	"testing"

	domain "github.com/ClassMesh/classmesh"
)

func TestStaticClassify(t *testing.T) {
	s := NewStatic("static", map[string]string{
		"GET /healthz 200": "noise",
		"payment failed":   "billing",
	})
	if s.Name() != "static" {
		t.Fatalf("Name() = %q, want %q", s.Name(), "static")
	}

	cases := []struct {
		name         string
		payload      string
		wantCategory string
		wantErr      error
	}{
		{"known payload", "GET /healthz 200", "noise", nil},
		{"another known payload", "payment failed", "billing", nil},
		{"unknown payload", "something new", "", ErrUnclassified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := s.Classify(context.Background(), domain.Record{Data: []byte(tc.payload)})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Classify() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if c.Category != tc.wantCategory {
				t.Fatalf("Classify() category = %q, want %q", c.Category, tc.wantCategory)
			}
			if c.Confidence != 1 {
				t.Fatalf("Classify() confidence = %v, want 1", c.Confidence)
			}
			if c.Stage != "static" {
				t.Fatalf("Classify() stage = %q, want %q", c.Stage, "static")
			}
			if !c.IsValid() {
				t.Fatalf("Classify() returned invalid classification: %+v", c)
			}
		})
	}
}

func TestStaticHonorsContextCancellation(t *testing.T) {
	s := NewStatic("static", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Classify(ctx, domain.Record{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Classify() with cancelled ctx error = %v, want context.Canceled", err)
	}
}
