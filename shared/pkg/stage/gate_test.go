package stage

import (
	"math"
	"testing"
)

func TestNewGateValidation(t *testing.T) {
	for _, good := range []float64{0, 0.5, 1} {
		if _, err := NewGate(good); err != nil {
			t.Fatalf("NewGate(%v) error = %v, want nil", good, err)
		}
	}
	for _, bad := range []float64{-0.1, 1.5, math.NaN()} {
		if _, err := NewGate(bad); err == nil {
			t.Fatalf("NewGate(%v) error = nil, want error", bad)
		}
	}
}

func TestGateAdmits(t *testing.T) {
	cases := []struct {
		name       string
		gate       float64
		confidence float64
		want       bool
	}{
		{"zero gate admits zero", 0, 0, true},
		{"zero gate admits low confidence", 0, 0.01, true},
		{"at threshold is admitted", 0.7, 0.7, true},
		{"just below is rejected", 0.7, 0.6999, false},
		{"above threshold is admitted", 0.7, 0.9, true},
		{"full gate admits one", 1, 1, true},
		{"full gate rejects just below one", 1, 0.999, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := NewGate(tc.gate)
			if err != nil {
				t.Fatalf("NewGate(%v) error = %v", tc.gate, err)
			}
			if got := g.Admits(tc.confidence); got != tc.want {
				t.Fatalf("Gate(%v).Admits(%v) = %v, want %v", tc.gate, tc.confidence, got, tc.want)
			}
		})
	}
}
