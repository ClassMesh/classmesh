package rules

import (
	"context"
	"errors"
	"fmt"
	"testing"

	domain "github.com/ClassMesh/classmesh"
)

func runClassify(b *testing.B, s *Stage, payload string) {
	b.Helper()
	r := domain.Record{ID: "bench", Data: []byte(payload)}
	ctx := context.Background()
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Classify(ctx, r)
		if err != nil && !errors.Is(err, domain.ErrUnclassified) {
			b.Fatal(err)
		}
	}
}

// containsRuleset builds n substring rules whose markers never appear in the
// benchmark payload, so a record walks the whole set before missing.
func containsRuleset(b *testing.B, n int) *Stage {
	b.Helper()
	rs := make([]Rule, 0, n)
	for i := 0; i < n; i++ {
		rs = append(rs, Rule{
			Category: fmt.Sprintf("svc-%d", i),
			Contains: []string{fmt.Sprintf("service-%d-unique-marker", i)},
		})
	}
	s, err := New(rs)
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	return s
}

// BenchmarkRulesetScaling shows the linear cost of the miss walk as the
// ruleset grows: order rules by expected volume so the hot path exits early.
func BenchmarkRulesetScaling(b *testing.B) {
	payload := `2026-06-12T10:00:00Z INFO order shipped id=84712 warehouse=tlv carrier=ups`
	for _, n := range []int{20, 100, 1000} {
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			runClassify(b, containsRuleset(b, n), payload)
		})
	}
}

// BenchmarkClassifyMiddleRuleHit fills the gap between first- and last-rule
// hit: the record matches rule 10 of the realistic 20-rule set.
func BenchmarkClassifyMiddleRuleHit(b *testing.B) {
	runClassify(b, benchStage(b), `2026-06-12T10:00:00Z INFO service-4-marker request completed duration=12ms`)
}

// unprefilterableRegexRuleset is 20 regex rules that all begin with an
// alternation or wildcard, so no literal prefix can be extracted and the
// prefilter cannot skip any of them: the true worst-case miss.
func unprefilterableRegexRuleset(b *testing.B) *Stage {
	b.Helper()
	rs := make([]Rule, 0, 20)
	for i := 0; i < 20; i++ {
		rs = append(rs, Rule{
			Category: fmt.Sprintf("re-%d", i),
			Regex:    []string{fmt.Sprintf(`(?i)(alpha%d|beta%d|gamma%d)\d+`, i, i, i)},
		})
	}
	s, err := New(rs)
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	return s
}

// BenchmarkClassifyRegexMissUnprefilterable is the miss the literal prefilter
// cannot help: every regex is run against the payload.
func BenchmarkClassifyRegexMissUnprefilterable(b *testing.B) {
	runClassify(b, unprefilterableRegexRuleset(b), `2026-06-12T10:00:00Z INFO order shipped id=84712 warehouse=tlv carrier=ups`)
}
