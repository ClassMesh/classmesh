package rules

import (
	"context"
	"encoding/json"
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

func rec(fields map[string]any) domain.Record {
	return domain.Record{Kind: domain.KindJSON, Fields: fields}
}

func TestClassifyFieldMatchers(t *testing.T) {
	cases := []struct {
		name   string
		yml    string
		fields map[string]any
		want   string // "" means ErrUnclassified
	}{
		{
			"exact match",
			"rules:\n  - category: alert\n    fields:\n      - path: level\n        exact: error",
			map[string]any{"level": "error"}, "alert",
		},
		{
			"exact no match",
			"rules:\n  - category: alert\n    fields:\n      - path: level\n        exact: error",
			map[string]any{"level": "info"}, "",
		},
		{
			"exact on nested number is stringified",
			"rules:\n  - category: se\n    fields:\n      - path: http.status\n        exact: \"500\"",
			map[string]any{"http": map[string]any{"status": float64(500)}}, "se",
		},
		{
			"exact on a large integer id stays decimal, not scientific",
			"rules:\n  - category: hit\n    fields:\n      - path: id\n        exact: \"1234567\"",
			map[string]any{"id": float64(1234567)}, "hit",
		},
		{
			"exact on a decimal number",
			"rules:\n  - category: hit\n    fields:\n      - path: ratio\n        exact: \"0.5\"",
			map[string]any{"ratio": float64(0.5)}, "hit",
		},
		{
			"exact on a Go int value",
			"rules:\n  - category: se\n    fields:\n      - path: code\n        exact: \"500\"",
			map[string]any{"code": 500}, "se",
		},
		{
			"exact on a json.Number value",
			"rules:\n  - category: se\n    fields:\n      - path: code\n        exact: \"500\"",
			map[string]any{"code": json.Number("500")}, "se",
		},
		{
			"contains",
			"rules:\n  - category: t\n    fields:\n      - path: msg\n        contains: timeout",
			map[string]any{"msg": "connection timeout exceeded"}, "t",
		},
		{
			"regex on nested number",
			"rules:\n  - category: se\n    fields:\n      - path: http.status\n        regex: \"^5\"",
			map[string]any{"http": map[string]any{"status": float64(503)}}, "se",
		},
		{
			"exists true matches present",
			"rules:\n  - category: traced\n    fields:\n      - path: trace_id\n        exists: true",
			map[string]any{"trace_id": "abc"}, "traced",
		},
		{
			"exists true but absent",
			"rules:\n  - category: traced\n    fields:\n      - path: trace_id\n        exists: true",
			map[string]any{"other": "x"}, "",
		},
		{
			"exists false matches a structured record missing the field",
			"rules:\n  - category: untraced\n    fields:\n      - path: trace_id\n        exists: false",
			map[string]any{"other": "x"}, "untraced",
		},
		{
			"exists false matches an empty structured record",
			"rules:\n  - category: untraced\n    fields:\n      - path: trace_id\n        exists: false",
			map[string]any{}, "untraced",
		},
		{
			"exists false does not match a record with no fields",
			"rules:\n  - category: untraced\n    fields:\n      - path: trace_id\n        exists: false",
			nil, "",
		},
		{
			"non-scalar value never matches a string condition",
			"rules:\n  - category: t\n    fields:\n      - path: http\n        contains: status",
			map[string]any{"http": map[string]any{"status": float64(500)}}, "",
		},
		{
			"missing field does not match",
			"rules:\n  - category: alert\n    fields:\n      - path: level\n        exact: error",
			nil, "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustParse(t, tc.yml)
			c, err := s.Classify(context.Background(), rec(tc.fields))
			if tc.want == "" {
				if !errors.Is(err, stage.ErrUnclassified) {
					t.Fatalf("Classify() error = %v, want ErrUnclassified", err)
				}
				return
			}
			if err != nil || c.Category != tc.want || c.Confidence != 1 {
				t.Fatalf("Classify() = %+v, %v, want category %s confidence 1", c, err, tc.want)
			}
		})
	}
}

func TestClassifyMixedPayloadAndFields(t *testing.T) {
	yml := "rules:\n  - category: alert\n    contains: [\"PANIC\"]\n    fields:\n      - path: level\n        exact: error"
	s := mustParse(t, yml)

	// A text log matches on the payload, with no fields at all.
	c, err := s.Classify(context.Background(), domain.Record{Kind: domain.KindText, Data: []byte("PANIC goroutine stack")})
	if err != nil || c.Category != "alert" {
		t.Fatalf("text log Classify() = %+v, %v, want alert", c, err)
	}
	// A structured event matches on the field, with a payload that does not.
	c, err = s.Classify(context.Background(), rec(map[string]any{"level": "error"}))
	if err != nil || c.Category != "alert" {
		t.Fatalf("event Classify() = %+v, %v, want alert", c, err)
	}
	// Neither hits.
	if _, err := s.Classify(context.Background(), rec(map[string]any{"level": "info"})); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("no-match Classify() error = %v, want ErrUnclassified", err)
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
		{"field without path", "rules:\n  - category: a\n    fields:\n      - exact: x", "needs a path"},
		{"field no condition", "rules:\n  - category: a\n    fields:\n      - path: p", "exactly one of"},
		{"field two conditions", "rules:\n  - category: a\n    fields:\n      - path: p\n        exact: x\n        exists: true", "exactly one of"},
		{"field empty contains", "rules:\n  - category: a\n    fields:\n      - path: p\n        contains: \"\"", "empty contains"},
		{"field bad regex", "rules:\n  - category: a\n    fields:\n      - path: p\n        regex: \"(\"", "field \"p\""},
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
