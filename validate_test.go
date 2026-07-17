package classmesh

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestValidateNames(t *testing.T) {
	tests := []struct {
		name    string
		stages  []Stage
		wantErr string
	}{
		{"unique", []Stage{newStatic("a", nil), newStatic("b", nil)}, ""},
		{"nil stage", []Stage{newStatic("a", nil), nil}, "is nil"},
		{"typed-nil stage", []Stage{(*staticStage)(nil)}, "is nil"},
		{"empty name", []Stage{newStatic("", nil)}, "must not be empty"},
		{"duplicate", []Stage{newStatic("a", nil), newStatic("a", nil)}, "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStages(tt.stages)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateStages() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateStages() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateResult(t *testing.T) {
	if err := validateResult("ok", Classification{Category: "x", Confidence: 1}); err != nil {
		t.Fatalf("valid classification: validateResult() = %v, want nil", err)
	}
	for _, c := range []Classification{
		{Category: "", Confidence: 1},
		{Category: "x", Confidence: -0.1},
		{Category: "x", Confidence: 1.1},
		{Category: "x", Confidence: math.NaN()},
	} {
		err := validateResult("model", c)
		if err == nil {
			t.Fatalf("validateResult(%+v) = nil, want error", c)
		}
		var se *StageError
		if !errors.As(err, &se) || se.Stage != "model" {
			t.Fatalf("validateResult(%+v) error = %v, want *StageError for model", c, err)
		}
	}
}
