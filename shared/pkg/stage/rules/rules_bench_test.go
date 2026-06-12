package rules

import (
	"context"
	"fmt"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

// benchStage builds a realistic 20-rule set: a mix of substring and regex
// matchers, the shape of a production log-triage config.
func benchStage(b *testing.B) *Stage {
	b.Helper()
	rs := make([]Rule, 0, 20)
	rs = append(rs,
		Rule{Category: "noise", Contains: []string{"healthz", "readiness", "liveness", "ELB-HealthChecker"}},
		Rule{Category: "noise", Regex: []string{`GET /(ping|status) HTTP`}},
		Rule{Category: "billing", Regex: []string{`payment (failed|declined|expired)`}},
		Rule{Category: "auth", Contains: []string{"login failed", "invalid token"}, Regex: []string{`(?i)unauthorized`}},
		Rule{Category: "db", Regex: []string{`(connection refused|too many connections|deadlock detected)`}},
	)
	for i := 0; i < 15; i++ {
		rs = append(rs, Rule{
			Category: fmt.Sprintf("svc-%d", i),
			Contains: []string{fmt.Sprintf("service-%d-marker", i)},
		})
	}
	s, err := New(rs)
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	return s
}

func benchClassify(b *testing.B, payload string) {
	s := benchStage(b)
	r := domain.Record{ID: "bench", Data: []byte(payload)}
	ctx := context.Background()
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := s.Classify(ctx, r)
		if err != nil && err.Error() != "stage: unclassified" {
			b.Fatal(err)
		}
	}
}

func BenchmarkClassifyFirstRuleHit(b *testing.B) {
	benchClassify(b, `10.2.3.4 - - [12/Jun/2026:10:00:00] "GET /healthz HTTP/1.1" 200 2 "-" "kube-probe/1.29"`)
}

func BenchmarkClassifyLastRuleHit(b *testing.B) {
	benchClassify(b, `2026-06-12T10:00:00Z INFO service-14-marker request completed duration=12ms`)
}

func BenchmarkClassifyMiss(b *testing.B) {
	benchClassify(b, `2026-06-12T10:00:00Z INFO order shipped id=84712 warehouse=tlv carrier=ups`)
}
