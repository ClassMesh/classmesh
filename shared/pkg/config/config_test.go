package config_test

import (
	"strings"
	"testing"

	"github.com/ClassMesh/classmesh/shared/pkg/config"
)

func base() config.Config {
	g := 1.0
	return config.Config{
		Version: 1,
		Input:   config.Input{Type: "text"},
		Stages:  []config.StageSpec{{ID: "rules", Type: "rules", Path: "rules.yml", Gate: &g}},
		Sink:    config.SinkSpec{Type: "jsonl", Stream: "stdout"},
	}
}

func TestValidate(t *testing.T) {
	badGate := 1.5
	tests := []struct {
		name             string
		mutate           func(c *config.Config)
		wantErrSubstring string
	}{
		{"valid", func(c *config.Config) {}, ""},
		{"bad version", func(c *config.Config) { c.Version = 2 }, "unsupported version"},
		{"bad input type", func(c *config.Config) { c.Input.Type = "xml" }, "input.type"},
		{"no stages", func(c *config.Config) { c.Stages = nil }, "at least one stage"},
		{"empty stage id", func(c *config.Config) { c.Stages[0].ID = "" }, "id is required"},
		{"duplicate stage id", func(c *config.Config) {
			c.Stages = append(c.Stages, config.StageSpec{ID: "rules", Type: "schema"})
		}, "duplicate stage id"},
		{"bad stage type", func(c *config.Config) { c.Stages[0].Type = "onnx" }, "is not one of"},
		{"gate out of range", func(c *config.Config) { c.Stages[0].Gate = &badGate }, "within [0, 1]"},
		{"bad sink type", func(c *config.Config) { c.Sink.Type = "kafka" }, "sink: type"},
		{"jsonl sink no target", func(c *config.Config) { c.Sink = config.SinkSpec{Type: "jsonl"} }, "needs a path or a stream"},
		{"jsonl sink two targets", func(c *config.Config) {
			c.Sink = config.SinkSpec{Type: "jsonl", Path: "a.jsonl", Stream: "stdout"}
		}, "either path or stream"},
		{"bad stream", func(c *config.Config) { c.Sink = config.SinkSpec{Type: "jsonl", Stream: "socket"} }, "stream"},
		{"drop sink with target", func(c *config.Config) { c.Sink = config.SinkSpec{Type: "drop", Path: "x"} }, "takes no path"},
		{"empty route category", func(c *config.Config) {
			c.Routes = map[string]config.SinkSpec{"": {Type: "drop"}}
		}, "route category must not be empty"},
		{"bad route sink", func(c *config.Config) {
			c.Routes = map[string]config.SinkSpec{"noise": {Type: "kafka"}}
		}, "route noise: type"},
		{"bad review sink", func(c *config.Config) { c.Review = &config.SinkSpec{Type: "kafka"} }, "review: type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			tt.mutate(&c)
			err := c.Validate()
			if tt.wantErrSubstring == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErrSubstring) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErrSubstring)
			}
			if !strings.HasPrefix(err.Error(), "config:") {
				t.Fatalf("Validate() error %q must be prefixed with \"config:\"", err.Error())
			}
		})
	}
}

func TestParseValid(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version: 1
input: { type: jsonl }
stages:
  - { id: quarantine, type: schema }
  - { id: rules, type: rules, path: rules.yml, gate: 1.0 }
routes:
  noise: { type: drop }
sink: { type: jsonl, stream: stdout }
review: { type: jsonl, path: review.jsonl }
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cfg.Stages) != 2 || cfg.Input.Type != "jsonl" {
		t.Fatalf("Parse() = %+v, want 2 stages, jsonl input", cfg)
	}
}

func TestParseRejectsUnknownKey(t *testing.T) {
	cases := map[string]string{
		"top level":    "version: 1\nbogus: 1\ninput: { type: text }\nstages: [{id: r, type: rules}]\nsink: { type: jsonl, stream: stdout }\n",
		"input":        "version: 1\ninput: { type: text, bogus: 1 }\nstages: [{id: r, type: rules}]\nsink: { type: jsonl, stream: stdout }\n",
		"stage":        "version: 1\ninput: { type: text }\nstages: [{id: r, type: rules, bogus: 1}]\nsink: { type: jsonl, stream: stdout }\n",
		"default sink": "version: 1\ninput: { type: text }\nstages: [{id: r, type: rules}]\nsink: { type: jsonl, stream: stdout, bogus: 1 }\n",
		"route sink":   "version: 1\ninput: { type: text }\nstages: [{id: r, type: rules}]\nroutes: { noise: { type: drop, bogus: 1 } }\nsink: { type: jsonl, stream: stdout }\n",
		"review sink":  "version: 1\ninput: { type: text }\nstages: [{id: r, type: rules}]\nsink: { type: jsonl, stream: stdout }\nreview: { type: drop, bogus: 1 }\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Parse([]byte(doc)); err == nil || !strings.Contains(err.Error(), "parse yaml") {
				t.Fatalf("Parse() = %v, want unknown-field parse error", err)
			}
		})
	}
}

func TestParseRejectsFractionalVersion(t *testing.T) {
	doc := "version: 1.5\ninput: { type: text }\nstages: [{id: r, type: rules}]\nsink: { type: jsonl, stream: stdout }\n"
	if _, err := config.Parse([]byte(doc)); err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("Parse() = %v, want integer-version error", err)
	}
}

func TestParseRejectsAliasVersion(t *testing.T) {
	doc := "input: { type: text }\nstages: [{id: r, type: rules, gate: &g 1.0}]\nversion: *g\nsink: { type: jsonl, stream: stdout }\n"
	if _, err := config.Parse([]byte(doc)); err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("Parse() = %v, want integer-version error", err)
	}
}

func TestParseRejectsTrailingDocument(t *testing.T) {
	doc := "version: 1\ninput: { type: text }\nstages: [{id: r, type: rules}]\nsink: { type: jsonl, stream: stdout }\n---\nbogus: 1\n"
	if _, err := config.Parse([]byte(doc)); err == nil || !strings.Contains(err.Error(), "single YAML document") {
		t.Fatalf("Parse() = %v, want single-document error", err)
	}
}

func TestParseEmpty(t *testing.T) {
	if _, err := config.Parse(nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("Parse(nil) = %v, want empty-document error", err)
	}
}
