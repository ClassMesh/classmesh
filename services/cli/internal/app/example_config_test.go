package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The committed cascade example must stay runnable from a fresh clone.
func TestExampleCascadeConfig(t *testing.T) {
	t.Run("validate", func(t *testing.T) {
		var out, errOut bytes.Buffer
		err := Run(context.Background(), []string{"validate", "--config", examplePath(t, "classmesh.yaml")},
			Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if err != nil {
			t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
		}
		if !strings.Contains(out.String(), "structurally valid") {
			t.Fatalf("stdout = %q, want a validity confirmation", out.String())
		}
	})

	t.Run("run", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"classmesh.yaml", "prod-rules.yml", "mock.yml"} {
			data, err := os.ReadFile(examplePath(t, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}

		tests := []struct {
			line     string
			category string // expected category for classified records
			stage    string // expected deciding stage
			drop     bool   // routed to drop: classified but absent from stdout
			review   bool   // neither tier decided: lands in review.jsonl
		}{
			{line: "GET /healthz 200", category: "noise", drop: true},
			{line: "2026-07-10T08:00:00Z info service=gateway http request method=GET path=/api/v1/orders/1 status=200 latency_ms=5 ip=10.0.0.1", category: "traffic", stage: "rules"},
			{line: "2026-07-10T08:00:00Z info service=payments payment declined for order 7 amount=10.00 currency=USD", category: "billing", stage: "rules"},
			{line: "2026-07-10T08:00:01Z blocked: unauthorized token for user 3", category: "auth", stage: "rules"},
			{line: "2026-07-10T08:00:02Z error service=api upstream timeout after 3000ms trace=00000001", category: "alert", stage: "rules"},
			{line: "2026-07-10T08:00:03Z warn service=db slow query duration_ms=1900 table=orders_7", category: "anomaly", stage: "model"},
			{line: "2026-07-10T08:00:04Z shipment 84712 dispatched from tlv warehouse=A", review: true},
			{line: "tick 8912 drift 3ms", review: true},
		}

		var input strings.Builder
		for _, tc := range tests {
			input.WriteString(tc.line)
			input.WriteByte('\n')
		}
		inPath := filepath.Join(dir, "input.log")
		if err := os.WriteFile(inPath, []byte(input.String()), 0o644); err != nil {
			t.Fatal(err)
		}

		var out, errOut bytes.Buffer
		err := Run(context.Background(), []string{"run", "--config", filepath.Join(dir, "classmesh.yaml"), inPath},
			Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if err != nil {
			t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
		}
		if !strings.Contains(errOut.String(), "processed=8 classified=6 review=2") {
			t.Fatalf("stats = %q, want processed=8 classified=6 review=2", errOut.String())
		}

		reviewData, err := os.ReadFile(filepath.Join(dir, "review.jsonl"))
		if err != nil {
			t.Fatalf("review sink: %v", err)
		}
		stdout := out.String()
		for _, tc := range tests {
			switch {
			case tc.drop:
				if strings.Contains(stdout, tc.line) {
					t.Errorf("dropped line %q reached stdout", tc.line)
				}
			case tc.review:
				if !strings.Contains(string(reviewData), tc.line) {
					t.Errorf("line %q missing from review.jsonl", tc.line)
				}
				if strings.Contains(stdout, tc.line) {
					t.Errorf("review line %q reached stdout", tc.line)
				}
			default:
				rec := ""
				for _, r := range strings.Split(strings.TrimSpace(stdout), "\n") {
					if strings.Contains(r, tc.line) {
						rec = r
						break
					}
				}
				if rec == "" {
					t.Errorf("line %q missing from stdout", tc.line)
					continue
				}
				if !strings.Contains(rec, `"category":"`+tc.category+`"`) {
					t.Errorf("line %q: record %s, want category %q", tc.line, rec, tc.category)
				}
				if !strings.Contains(rec, `"stage":"`+tc.stage+`"`) {
					t.Errorf("line %q: record %s, want stage %q", tc.line, rec, tc.stage)
				}
			}
		}
	})
}
