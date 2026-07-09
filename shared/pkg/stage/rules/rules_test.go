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
			"gte on a number matches",
			"rules:\n  - category: se\n    fields:\n      - path: http.status\n        gte: 500",
			map[string]any{"http": map[string]any{"status": float64(503)}}, "se",
		},
		{
			"json pointer addresses a key containing a dot",
			"rules:\n  - category: se\n    fields:\n      - path: /http.status\n        gte: 500",
			map[string]any{"http.status": float64(503)}, "se",
		},
		{
			"gte below threshold does not match",
			"rules:\n  - category: se\n    fields:\n      - path: http.status\n        gte: 500",
			map[string]any{"http": map[string]any{"status": float64(404)}}, "",
		},
		{
			"lt on a number matches",
			"rules:\n  - category: fast\n    fields:\n      - path: latency_ms\n        lt: 100",
			map[string]any{"latency_ms": float64(12)}, "fast",
		},
		{
			"numeric comparison on a string field does not match",
			"rules:\n  - category: se\n    fields:\n      - path: status\n        gte: 500",
			map[string]any{"status": "503"}, "",
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

func TestAnyGroup(t *testing.T) {
	yml := "rules:\n  - category: hit\n    any:\n      - contains: \"alpha\"\n      - field: {path: level, exact: error}"
	s := mustParse(t, yml)

	c, err := s.Classify(context.Background(), domain.Record{Data: []byte("see alpha here")})
	if err != nil || c.Category != "hit" {
		t.Fatalf("payload branch: Classify() = %+v, %v, want hit", c, err)
	}
	c, err = s.Classify(context.Background(), rec(map[string]any{"level": "error"}))
	if err != nil || c.Category != "hit" {
		t.Fatalf("field branch: Classify() = %+v, %v, want hit", c, err)
	}
	if _, err := s.Classify(context.Background(), domain.Record{Data: []byte("nothing here")}); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("no branch: Classify() error = %v, want ErrUnclassified", err)
	}
}

func TestAllGroup(t *testing.T) {
	yml := "rules:\n  - category: se\n    all:\n      - field: {path: level, exact: error}\n      - field: {path: http.status, regex: \"^5\"}"
	s := mustParse(t, yml)

	both := rec(map[string]any{"level": "error", "http": map[string]any{"status": float64(503)}})
	c, err := s.Classify(context.Background(), both)
	if err != nil || c.Category != "se" {
		t.Fatalf("both: Classify() = %+v, %v, want se", c, err)
	}
	onlyOne := rec(map[string]any{"level": "error"})
	if _, err := s.Classify(context.Background(), onlyOne); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("one: Classify() error = %v, want ErrUnclassified", err)
	}
}

func TestBaseAndGroupsCombine(t *testing.T) {
	// Top-level and group blocks are ANDed: both must hold.
	yml := "rules:\n  - category: x\n    contains: [\"PANIC\"]\n    all:\n      - field: {path: level, exact: error}"
	s := mustParse(t, yml)

	c, err := s.Classify(context.Background(), domain.Record{Data: []byte("PANIC now"), Fields: map[string]any{"level": "error"}})
	if err != nil || c.Category != "x" {
		t.Fatalf("both blocks: Classify() = %+v, %v, want x", c, err)
	}
	if _, err := s.Classify(context.Background(), domain.Record{Data: []byte("PANIC now")}); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("payload only: Classify() error = %v, want ErrUnclassified", err)
	}
	if _, err := s.Classify(context.Background(), rec(map[string]any{"level": "error"})); !errors.Is(err, stage.ErrUnclassified) {
		t.Fatalf("field only: Classify() error = %v, want ErrUnclassified", err)
	}
}

func TestMatchReasons(t *testing.T) {
	withID := mustParse(t, "rules:\n  - id: noise-health\n    category: noise\n    contains: [\"healthz\"]")
	c, err := withID.Classify(context.Background(), domain.Record{Data: []byte("GET /healthz 200")})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if len(c.Reasons) != 1 || c.Reasons[0].Code != "noise-health" {
		t.Fatalf("Reasons = %+v, want one with Code noise-health", c.Reasons)
	}
	if !strings.Contains(c.Reasons[0].Detail, "healthz") {
		t.Fatalf("Reason Detail = %q, want it to mention the matched substring", c.Reasons[0].Detail)
	}

	noID := mustParse(t, "rules:\n  - category: noise\n    contains: [\"healthz\"]")
	c, err = noID.Classify(context.Background(), domain.Record{Data: []byte("healthz")})
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if len(c.Reasons) != 1 || c.Reasons[0].Code != "noise" {
		t.Fatalf("Reasons = %+v, want Code to fall back to the category", c.Reasons)
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
		{"empty regex", "rules:\n  - category: a\n    regex: [\"\"]", "empty regex"},
		{"unknown field", "rules:\n  - category: a\n    contains: [x]\n    bogus: true", "bogus"},
		{"bad yaml", ":\nnot yaml at all\n  x", "parse yaml"},
		{"field without path", "rules:\n  - category: a\n    fields:\n      - exact: x", "needs a path"},
		{"field no condition", "rules:\n  - category: a\n    fields:\n      - path: p", "exactly one of"},
		{"field two conditions", "rules:\n  - category: a\n    fields:\n      - path: p\n        exact: x\n        exists: true", "exactly one of"},
		{"field two numeric conditions", "rules:\n  - category: a\n    fields:\n      - path: p\n        gte: 1\n        lt: 9", "exactly one of"},
		{"field empty contains", "rules:\n  - category: a\n    fields:\n      - path: p\n        contains: \"\"", "empty contains"},
		{"field bad regex", "rules:\n  - category: a\n    fields:\n      - path: p\n        regex: \"(\"", "field \"p\""},
		{"any matcher empty", "rules:\n  - category: a\n    any:\n      - {}", "exactly one of contains/regex/field"},
		{"all matcher two conditions", "rules:\n  - category: a\n    all:\n      - contains: x\n        regex: y", "exactly one of contains/regex/field"},
		{"group bad regex names rule id", "rules:\n  - id: r9\n    category: a\n    any:\n      - regex: \"(\"", "rule 1 (r9)"},
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

func TestLookupStringKinds(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
		ok    bool
	}{
		{"string", "hit", "hit", true},
		{"bool", true, "true", true},
		{"float64", 3.5, "3.5", true},
		{"json number", json.Number("17"), "17", true},
		{"int", int(-42), "-42", true},
		{"int8", int8(-8), "-8", true},
		{"int16", int16(-16), "-16", true},
		{"int32", int32(-32), "-32", true},
		{"int64", int64(-64), "-64", true},
		{"uint", uint(42), "42", true},
		{"uint8", uint8(8), "8", true},
		{"uint16", uint16(16), "16", true},
		{"uint32", uint32(32), "32", true},
		{"uint64", uint64(64), "64", true},
		{"float32", float32(1.5), "1.5", true},
		{"nil", nil, "", false},
		{"object", map[string]any{"x": 1}, "", false},
		{"array", []any{"x"}, "", false},
		{"complex", complex(1, 2), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{"k": tc.value}
			got, ok := lookupString(fields, []string{"k"})
			if ok != tc.ok || got != tc.want {
				t.Fatalf("lookupString() = %q, %v, want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
	if _, ok := lookupString(map[string]any{}, []string{"k"}); ok {
		t.Fatal("lookupString() ok = true for a missing path, want false")
	}
}

func TestLookupNumberKinds(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  float64
		ok    bool
	}{
		{"float64", 3.5, 3.5, true},
		{"json number", json.Number("17"), 17, true},
		{"json number invalid", json.Number("nope"), 0, false},
		{"int", int(-42), -42, true},
		{"int8", int8(-8), -8, true},
		{"int16", int16(-16), -16, true},
		{"int32", int32(-32), -32, true},
		{"int64", int64(-64), -64, true},
		{"uint", uint(42), 42, true},
		{"uint8", uint8(8), 8, true},
		{"uint16", uint16(16), 16, true},
		{"uint32", uint32(32), 32, true},
		{"uint64", uint64(64), 64, true},
		{"float32", float32(1.5), 1.5, true},
		{"numeric string", "12", 0, false},
		{"bool", true, 0, false},
		{"nil", nil, 0, false},
		{"object", map[string]any{"x": 1}, 0, false},
		{"complex", complex(1, 2), 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{"k": tc.value}
			got, ok := lookupNumber(fields, []string{"k"})
			if ok != tc.ok || got != tc.want {
				t.Fatalf("lookupNumber() = %v, %v, want %v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
	if _, ok := lookupNumber(map[string]any{}, []string{"k"}); ok {
		t.Fatal("lookupNumber() ok = true for a missing path, want false")
	}
}
