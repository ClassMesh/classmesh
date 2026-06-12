package rules

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
	"github.com/ClassMesh/classmesh/shared/pkg/stage"
)

const sampleYAML = `
rules:
  - category: noise
    contains: ["healthz", "readiness"]
  - category: billing
    regex: ["payment (failed|declined)"]
  - category: auth
    contains: ["login"]
    regex: ["(?i)unauthorized"]
`

func mustParse(t *testing.T, yml string) *Stage {
	t.Helper()
	s, err := Parse([]byte(yml))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return s
}

func TestClassifyFirstMatchWins(t *testing.T) {
	s := mustParse(t, sampleYAML)
	if s.Name() != "rules" {
		t.Fatalf("Name() = %q, want rules", s.Name())
	}

	cases := []struct {
		name         string
		payload      string
		wantCategory string
		wantErr      error
	}{
		{"contains match", "GET /healthz 200", "noise", nil},
		{"second contains alternative", "readiness probe ok", "noise", nil},
		{"regex match", "payment declined for order 7", "billing", nil},
		{"contains in later rule", "user login from 10.0.0.1", "auth", nil},
		{"case-insensitive regex", "UNAUTHORIZED access", "auth", nil},
		{"rule order decides", "healthz login", "noise", nil},
		{"no match escalates", "something unrelated", "", stage.ErrUnclassified},
		{"case-sensitive contains", "HEALTHZ", "", stage.ErrUnclassified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := s.Classify(context.Background(), domain.Record{Data: []byte(tc.payload)})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Classify(%q) error = %v, want %v", tc.payload, err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if c.Category != tc.wantCategory || c.Confidence != 1 || c.Stage != "rules" {
				t.Fatalf("Classify(%q) = %+v, want {%s 1 rules}", tc.payload, c, tc.wantCategory)
			}
		})
	}
}

func TestValidation(t *testing.T) {
	cases := []struct {
		name    string
		yml     string
		wantSub string
	}{
		{"no rules", "rules: []", "at least one rule"},
		{"missing category", "rules:\n  - contains: [x]", "category is required"},
		{"no matchers", "rules:\n  - category: a", "at least one matcher"},
		{"empty contains", "rules:\n  - category: a\n    contains: [\"\"]", "empty contains"},
		{"bad regex", "rules:\n  - category: a\n    regex: [\"(\"]", "rule 1 (a)"},
		{"bad yaml", ":\nnot yaml at all\n  x", "parse yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yml))
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("Parse() error = %v, want mention of %q", err, tc.wantSub)
			}
		})
	}
}

func TestLoadFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.yml")
	if err := os.WriteFile(path, []byte(sampleYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	c, err := s.Classify(context.Background(), domain.Record{Data: []byte("GET /healthz")})
	if err != nil || c.Category != "noise" {
		t.Fatalf("Classify() = %+v, %v, want noise", c, err)
	}
}

func TestLoadMissingFileFails(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yml")); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestClassifyHonorsContextCancellation(t *testing.T) {
	s := mustParse(t, sampleYAML)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Classify(ctx, domain.Record{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Classify() error = %v, want context.Canceled", err)
	}
}
