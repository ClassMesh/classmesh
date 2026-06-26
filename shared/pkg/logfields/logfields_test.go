package logfields

import (
	"reflect"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/domain"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		line string
		want map[string]any
	}{
		{
			"logfmt pairs",
			`time=2026-06-25T07:53:07Z level=error service=api msg="boom"`,
			map[string]any{"time": "2026-06-25T07:53:07Z", "level": "error", "service": "api", "msg": "boom"},
		},
		{
			"leading timestamp and level",
			"2026-06-25T07:53:07Z INFO request done",
			map[string]any{"timestamp": "2026-06-25T07:53:07Z", "level": "info"},
		},
		{
			"bracketed level",
			"[ERROR] disk full",
			map[string]any{"level": "error"},
		},
		{
			"timestamp, level and pairs",
			"2026-06-25T07:53:07Z WARN user=42 latency=12ms",
			map[string]any{"timestamp": "2026-06-25T07:53:07Z", "level": "warn", "user": "42", "latency": "12ms"},
		},
		{
			"quoted value with spaces",
			`msg="payment failed" code=500`,
			map[string]any{"msg": "payment failed", "code": "500"},
		},
		{
			"escaped quote in value",
			`msg="say \"hi\""`,
			map[string]any{"msg": `say "hi"`},
		},
		{
			"unsupported backslash escapes are preserved",
			`path="C:\tmp\app" pattern="\d+"`,
			map[string]any{"path": `C:\tmp\app`, "pattern": `\d+`},
		},
		{
			"explicit level wins over a bare token",
			"2026-06-25T07:53:07Z INFO level=debug",
			map[string]any{"timestamp": "2026-06-25T07:53:07Z", "level": "debug"},
		},
		{
			"unstructured line yields nil",
			"just some random text here",
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse([]byte(tc.line))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestEnrichTextRecord(t *testing.T) {
	r := domain.Record{ID: "app.log:1", Kind: domain.KindText, Data: []byte("2026-06-25T07:53:07Z ERROR svc=api")}
	got := Enrich(r)

	if got.Kind != domain.KindLog {
		t.Fatalf("Kind = %q, want %q", got.Kind, domain.KindLog)
	}
	if string(got.Data) != "2026-06-25T07:53:07Z ERROR svc=api" {
		t.Fatalf("Data = %q, want it untouched", got.Data)
	}
	if got.Fields["level"] != "error" || got.Fields["svc"] != "api" || got.Fields["timestamp"] != "2026-06-25T07:53:07Z" {
		t.Fatalf("Fields = %v, want level/svc/timestamp extracted", got.Fields)
	}
}

func TestEnrichUnparsedRecordIsUnchanged(t *testing.T) {
	r := domain.Record{ID: "x", Kind: domain.KindText, Data: []byte("just a plain message")}
	got := Enrich(r)

	if got.Kind != domain.KindText || got.Fields != nil || string(got.Data) != "just a plain message" {
		t.Fatalf("Enrich() = %+v, want the record unchanged", got)
	}
}

func TestEnrichExistingFieldsWin(t *testing.T) {
	r := domain.Record{Data: []byte("level=info"), Fields: map[string]any{"level": "override"}}
	got := Enrich(r)

	if got.Fields["level"] != "override" {
		t.Fatalf("Fields[level] = %v, want existing value to win", got.Fields["level"])
	}
	if got.Kind != domain.KindLog {
		t.Fatalf("Kind = %q, want KindLog", got.Kind)
	}
}
