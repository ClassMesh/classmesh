package stage

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

func TestValidateNames(t *testing.T) {
	tests := []struct {
		name    string
		stages  []Stage
		wantErr string
	}{
		{"unique", []Stage{NewStatic("a", nil), NewStatic("b", nil)}, ""},
		{"nil stage", []Stage{NewStatic("a", nil), nil}, "is nil"},
		{"typed-nil stage", []Stage{(*Static)(nil)}, "is nil"},
		{"empty name", []Stage{NewStatic("", nil)}, "must not be empty"},
		{"duplicate", []Stage{NewStatic("a", nil), NewStatic("a", nil)}, "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNames(tt.stages)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateNames() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateNames() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateResult(t *testing.T) {
	if err := ValidateResult("ok", domain.Classification{Category: "x", Confidence: 1}); err != nil {
		t.Fatalf("valid classification: ValidateResult() = %v, want nil", err)
	}
	for _, c := range []domain.Classification{
		{Category: "", Confidence: 1},
		{Category: "x", Confidence: -0.1},
		{Category: "x", Confidence: 1.1},
		{Category: "x", Confidence: math.NaN()},
	} {
		err := ValidateResult("model", c)
		if err == nil {
			t.Fatalf("ValidateResult(%+v) = nil, want error", c)
		}
		var se *Error
		if !errors.As(err, &se) || se.Stage != "model" {
			t.Fatalf("ValidateResult(%+v) error = %v, want *stage.Error for model", c, err)
		}
	}
}
