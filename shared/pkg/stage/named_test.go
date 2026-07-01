package stage

import (
	"context"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

func TestWithNamePanicsOnNilInner(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithName(nil, ...) should panic")
		}
	}()
	WithName(nil, "x")
}

func TestWithName(t *testing.T) {
	n := WithName(NewStatic("rules", map[string]string{"x": "hit"}), "quarantine")
	if n.Name() != "quarantine" {
		t.Fatalf("Name() = %q, want quarantine", n.Name())
	}
	c, err := n.Classify(context.Background(), domain.Record{Data: []byte("x")})
	if err != nil || c.Category != "hit" {
		t.Fatalf("Classify() = (%+v, %v), want the wrapped stage's decision", c, err)
	}
}
