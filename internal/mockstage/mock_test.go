package mockstage

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh"
)

func record(data string) classmesh.Record {
	return classmesh.Record{ID: "r1", Data: []byte(data)}
}

func TestFirstMatcherWins(t *testing.T) {
	s, err := New(Config{Matchers: []Matcher{
		{Contains: []string{"payment"}, Category: "billing", Confidence: 0.93},
		{Contains: []string{"payment declined"}, Category: "alert", Confidence: 0.99},
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c, err := s.Classify(context.Background(), record("payment declined order=7"))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if c.Category != "billing" || c.Confidence != 0.93 {
		t.Fatalf("Classify() = %+v, want billing at 0.93 (first matcher wins)", c)
	}
	if c.Stage != Name {
		t.Fatalf("Stage = %q, want %q", c.Stage, Name)
	}
	if len(c.Reasons) != 1 || c.Reasons[0].Code != Name {
		t.Fatalf("Reasons = %+v, want one mock reason", c.Reasons)
	}
}

func TestAnyNeedleInMatcherHits(t *testing.T) {
	s, err := New(Config{Matchers: []Matcher{{Contains: []string{"alpha", "beta"}, Category: "c", Confidence: 0.7}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c, err := s.Classify(context.Background(), record("only beta appears"))
	if err != nil || c.Category != "c" {
		t.Fatalf("Classify() = %+v, %v, want a hit via the second needle", c, err)
	}
}

func TestBoundaryConfidencesAccepted(t *testing.T) {
	for _, conf := range []float64{0, 1} {
		if _, err := New(Config{Default: &Outcome{Category: "c", Confidence: conf}}); err != nil {
			t.Fatalf("New(confidence=%v) error = %v, want accepted", conf, err)
		}
	}
}

func TestUnmatchedEscalatesWithoutDefault(t *testing.T) {
	s, err := New(Config{Matchers: []Matcher{{Contains: []string{"x"}, Category: "c", Confidence: 0.5}}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := s.Classify(context.Background(), record("nothing here")); !errors.Is(err, classmesh.ErrUnclassified) {
		t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
	}
}

func TestUnmatchedUsesDefault(t *testing.T) {
	s, err := New(Config{Default: &Outcome{Category: "unknown", Confidence: 0.3}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c, err := s.Classify(context.Background(), record("anything"))
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if c.Category != "unknown" || c.Confidence != 0.3 {
		t.Fatalf("Classify() = %+v, want the default outcome", c)
	}
}

func TestCancelledContext(t *testing.T) {
	s, err := New(Config{Default: &Outcome{Category: "c", Confidence: 0.5}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Classify(ctx, record("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Classify() error = %v, want context.Canceled", err)
	}
}

func TestNewRejectsBadConfigs(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"empty", Config{}, "at least one matcher or a default"},
		{"no contains", Config{Matchers: []Matcher{{Category: "c", Confidence: 0.5}}}, "contains is required"},
		{"empty needle", Config{Matchers: []Matcher{{Contains: []string{""}, Category: "c", Confidence: 0.5}}}, "empty string"},
		{"empty category", Config{Matchers: []Matcher{{Contains: []string{"x"}, Confidence: 0.5}}}, "invalid"},
		{"confidence above 1", Config{Matchers: []Matcher{{Contains: []string{"x"}, Category: "c", Confidence: 1.5}}}, "invalid"},
		{"negative confidence", Config{Matchers: []Matcher{{Contains: []string{"x"}, Category: "c", Confidence: -0.1}}}, "invalid"},
		{"NaN confidence", Config{Matchers: []Matcher{{Contains: []string{"x"}, Category: "c", Confidence: math.NaN()}}}, "invalid"},
		{"bad default", Config{Default: &Outcome{Category: "", Confidence: 0.5}}, "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("New() error = %v, want error containing %q", err, tt.want)
			}
		})
	}
}
