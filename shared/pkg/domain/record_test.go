package domain

import "testing"

func TestClassificationIsValid(t *testing.T) {
	cases := []struct {
		name string
		c    Classification
		want bool
	}{
		{"valid deterministic", Classification{Category: "noise", Confidence: 1, Stage: "rules"}, true},
		{"valid uncertain", Classification{Category: "billing", Confidence: 0.42, Stage: "model"}, true},
		{"valid with reasons", Classification{Category: "noise", Confidence: 1, Stage: "rules", Reasons: []Reason{{Code: "r1", Detail: "matched contains \"healthz\""}}}, true},
		{"valid without reasons", Classification{Category: "noise", Confidence: 1, Stage: "rules", Reasons: nil}, true},
		{"zero confidence", Classification{Category: "noise", Confidence: 0}, true},
		{"empty category", Classification{Confidence: 1}, false},
		{"confidence above one", Classification{Category: "noise", Confidence: 1.5}, false},
		{"negative confidence", Classification{Category: "noise", Confidence: -0.1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsValid(); got != tc.want {
				t.Fatalf("IsValid() = %v, want %v", got, tc.want)
			}
		})
	}
}
