package stage

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorUnwrapsCause(t *testing.T) {
	boom := errors.New("boom")
	err := &Error{Stage: "rules", Err: boom}

	if got := err.Error(); got != "stage rules: boom" {
		t.Fatalf("Error() = %q, want %q", got, "stage rules: boom")
	}
	if !errors.Is(err, boom) {
		t.Fatal("errors.Is(err, boom) = false, want true")
	}
	var se *Error
	if !errors.As(err, &se) {
		t.Fatal("errors.As(err, &se) = false, want true")
	}
	if se.Stage != "rules" {
		t.Fatalf("se.Stage = %q, want rules", se.Stage)
	}
}

func TestErrorRecoverableThroughWrapping(t *testing.T) {
	boom := errors.New("boom")
	wrapped := fmt.Errorf("classifier: %w", &Error{Stage: "model", Err: boom})

	var se *Error
	if !errors.As(wrapped, &se) {
		t.Fatal("errors.As(wrapped, &se) = false, want true")
	}
	if se.Stage != "model" {
		t.Fatalf("se.Stage = %q, want model", se.Stage)
	}
	if !errors.Is(wrapped, boom) {
		t.Fatal("errors.Is(wrapped, boom) = false, want true")
	}
}
