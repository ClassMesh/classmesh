package rules

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/ClassMesh/classmesh"
)

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

// FuzzRegexRuleParity pins regex fast paths to plain regexp behavior.
func FuzzRegexRuleParity(f *testing.F) {
	patterns := []string{
		"^PING$", `\APING\z`, `\bPING\b`, `\BING\B`, "PING", "^PING", "PING$",
		"^GET /health$", "p[io]ng", "(warn|error) disk", "payment (failed|declined)",
	}
	stages := make([]classmesh.Stage, len(patterns))
	regexps := make([]*regexp.Regexp, len(patterns))
	for i, pattern := range patterns {
		stage, err := New([]Rule{{Category: "hit", Regex: []string{pattern}}})
		if err != nil {
			f.Fatalf("New(%q): %v", pattern, err)
		}
		stages[i] = stage
		regexps[i] = regexp.MustCompile(pattern)
	}
	f.Add("PING")
	f.Add("a PING b")
	f.Add("aPINGb")
	f.Add("GET /health 200")
	f.Add("payment declined for order 7")
	f.Add("")
	f.Fuzz(func(t *testing.T, data string) {
		record := classmesh.Record{ID: "f", Data: []byte(data)}
		for i, stage := range stages {
			_, err := stage.Classify(context.Background(), record)
			got := err == nil
			want := regexps[i].MatchString(data)
			if got != want {
				t.Fatalf("pattern %q on %q: rule matched=%v, regexp matched=%v", patterns[i], data, got, want)
			}
			if err != nil && !errors.Is(err, classmesh.ErrUnclassified) {
				t.Fatalf("pattern %q on %q: unexpected error %v", patterns[i], data, err)
			}
		}
	})
}
