package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFromConfig(t *testing.T) {
	dir := t.TempDir()
	rules := writeFile(t, dir, "rules.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n  - category: billing\n    regex: [\"payment (failed|declined)\"]\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: rules, type: rules, path: \""+rules+"\" }\nsink: { type: jsonl, stream: stdout }\n")

	var out, errOut bytes.Buffer
	in := strings.NewReader("GET /healthz 200\npayment declined order=7\nweird line\n")
	err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: in, Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "\"category\":\"noise\"") || !strings.Contains(got, "\"category\":\"billing\"") {
		t.Fatalf("stdout = %q, want noise + billing categories", got)
	}
	if !strings.Contains(errOut.String(), "processed=3 classified=2 review=1") {
		t.Fatalf("stats = %q, want processed=3 classified=2 review=1", errOut.String())
	}
}

func TestRunConfigRejectsSchemaStage(t *testing.T) {
	cfg := writeConfig(t, "version: 1\ninput: { type: text }\nstages: [{id: q, type: schema}]\nsink: { type: jsonl, stream: stdout }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "not yet runnable") {
		t.Fatalf("Run() error = %v, want schema-not-runnable", err)
	}
}

func TestRunConfigRoutesByCategory(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n  - category: billing\n    regex: [\"payment (failed|declined)\"]\n")
	noisePath := filepath.Join(dir, "noise.jsonl")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: rules, type: rules, path: \""+r+"\" }\nroutes:\n  noise: { type: jsonl, path: \""+noisePath+"\" }\nsink: { type: jsonl, stream: stdout }\n")

	var out, errOut bytes.Buffer
	in := strings.NewReader("GET /healthz 200\npayment declined order=7\n")
	if err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: in, Out: &out, Err: &errOut}); err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}
	noise, _ := os.ReadFile(noisePath)
	if !strings.Contains(string(noise), "\"category\":\"noise\"") {
		t.Fatalf("noise file = %q, want the routed noise record", noise)
	}
	if strings.Contains(out.String(), "\"category\":\"noise\"") {
		t.Fatalf("stdout = %q, noise should have been routed to its own sink", out.String())
	}
	if !strings.Contains(out.String(), "\"category\":\"billing\"") {
		t.Fatalf("stdout = %q, want billing on the default sink", out.String())
	}
}

func TestRunConfigDropRoute(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n  - category: billing\n    regex: [\"payment (failed|declined)\"]\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: rules, type: rules, path: \""+r+"\" }\nroutes:\n  noise: { type: drop }\nsink: { type: jsonl, stream: stdout }\n")

	var out, errOut bytes.Buffer
	in := strings.NewReader("GET /healthz 200\npayment declined order=7\n")
	if err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: in, Out: &out, Err: &errOut}); err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}
	if strings.Contains(out.String(), "\"category\":\"noise\"") {
		t.Fatalf("stdout = %q, noise routed to drop must not appear", out.String())
	}
	if !strings.Contains(out.String(), "\"category\":\"billing\"") {
		t.Fatalf("stdout = %q, want billing", out.String())
	}
}

func TestRunRejectsBothConfigAndRules(t *testing.T) {
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", "x.yaml", "--rules", "y.yml"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "exactly one of") {
		t.Fatalf("Run() error = %v, want mutual-exclusion error", err)
	}
}

func TestRunConfigMultipleStagesUseIDs(t *testing.T) {
	dir := t.TempDir()
	r1 := writeFile(t, dir, "r1.yml", "rules:\n  - category: a\n    contains: [\"alpha\"]\n")
	r2 := writeFile(t, dir, "r2.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: first, type: rules, path: \""+r1+"\" }\n  - { id: second, type: rules, path: \""+r2+"\" }\nsink: { type: jsonl, stream: stdout }\n")

	var out, errOut bytes.Buffer
	in := strings.NewReader("alpha thing\nGET /healthz 200\n")
	if err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: in, Out: &out, Err: &errOut}); err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}
	stats := errOut.String()
	if !strings.Contains(stats, "first:1") || !strings.Contains(stats, "second:1") {
		t.Fatalf("stats = %q, want per-stage counts keyed by config ids first + second", stats)
	}
}

func TestRunConfigReviewSink(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n")
	reviewPath := filepath.Join(dir, "review.jsonl")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: r, type: rules, path: \""+r+"\" }\nsink: { type: jsonl, stream: stdout }\nreview: { type: jsonl, path: \""+reviewPath+"\" }\n")

	var out, errOut bytes.Buffer
	in := strings.NewReader("GET /healthz 200\nunmatched line\n")
	if err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: in, Out: &out, Err: &errOut}); err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}
	data, rerr := os.ReadFile(reviewPath)
	if rerr != nil {
		t.Fatalf("read review file: %v", rerr)
	}
	if !strings.Contains(string(data), "unmatched line") {
		t.Fatalf("review file = %q, want the undecided record", data)
	}
}

func TestRunConfigFileSink(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n")
	outPath := filepath.Join(dir, "out.jsonl")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: r, type: rules, path: \""+r+"\" }\nsink: { type: jsonl, path: \""+outPath+"\" }\n")

	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: strings.NewReader("GET /healthz 200\n"), Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}
	data, rerr := os.ReadFile(outPath)
	if rerr != nil {
		t.Fatalf("read sink file: %v", rerr)
	}
	if !strings.Contains(string(data), "\"category\":\"noise\"") {
		t.Fatalf("sink file = %q, want the classified record", data)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout should be empty when the sink is a file, got %q", out.String())
	}
}

func TestRunConfigRejectsDropDefault(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: r, type: rules, path: \""+r+"\" }\nsink: { type: drop }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "default sink cannot be drop") {
		t.Fatalf("Run() error = %v, want drop-default rejection", err)
	}
}

func TestRunConfigRejectsOutputCollision(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n")
	inFile := writeFile(t, dir, "in.log", "GET /healthz 200\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: r, type: rules, path: \""+r+"\" }\nsink: { type: jsonl, path: \""+inFile+"\" }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg, inFile}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "collides with") {
		t.Fatalf("Run() error = %v, want output/input collision error", err)
	}
	if data, _ := os.ReadFile(inFile); string(data) != "GET /healthz 200\n" {
		t.Fatalf("input file was truncated to %q; collision check must run before create", data)
	}
}

func TestRunConfigRejectsRouteInputCollision(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n")
	inFile := writeFile(t, dir, "in.log", "GET /healthz 200\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: r, type: rules, path: \""+r+"\" }\nroutes:\n  noise: { type: jsonl, path: \""+inFile+"\" }\nsink: { type: jsonl, stream: stdout }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg, inFile}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "collides with") {
		t.Fatalf("Run() error = %v, want a route/input collision error", err)
	}
	if data, _ := os.ReadFile(inFile); string(data) != "GET /healthz 200\n" {
		t.Fatalf("input file was truncated to %q; a route collision must be caught before create", data)
	}
}

func TestRunConfigRejectsTwoRoutesSameFile(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n")
	shared := writeFile(t, dir, "shared.jsonl", "old contents\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: r, type: rules, path: \""+r+"\" }\nroutes:\n  noise: { type: jsonl, path: \""+shared+"\" }\n  billing: { type: jsonl, path: \""+shared+"\" }\nsink: { type: jsonl, stream: stdout }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("Run() error = %v, want a two-routes-same-file rejection", err)
	}
	if data, _ := os.ReadFile(shared); string(data) != "old contents\n" {
		t.Fatalf("route file was truncated to %q; the collision must be caught before create", data)
	}
}

func TestRunConfigResolvesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rules.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: rules, type: rules, path: rules.yml }\nsink: { type: jsonl, stream: stdout }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: strings.NewReader("GET /healthz 200\n"), Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("Run() error = %v (a relative path must resolve against the config dir), stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "\"category\":\"noise\"") {
		t.Fatalf("stdout = %q, want noise", out.String())
	}
}

func TestRunConfigJSONLInput(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - id: error-level\n    category: alert\n    fields:\n      - path: level\n        exact: error\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: jsonl }\nstages:\n  - { id: rules, type: rules, path: \""+r+"\" }\nsink: { type: jsonl, stream: stdout }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: strings.NewReader("{\"level\":\"error\"}\n"), Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "\"category\":\"alert\"") {
		t.Fatalf("stdout = %q, want alert from a jsonl field match", out.String())
	}
}

func TestRunConfigRejectsStderrSink(t *testing.T) {
	dir := t.TempDir()
	r := writeFile(t, dir, "r.yml", "rules:\n  - category: noise\n    contains: [\"healthz\"]\n")
	cfg := writeFile(t, dir, "cm.yaml", "version: 1\ninput: { type: text }\nstages:\n  - { id: r, type: rules, path: \""+r+"\" }\nsink: { type: jsonl, stream: stderr }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "is not one of stdout") {
		t.Fatalf("Run() error = %v, want stderr rejected as a data sink", err)
	}
}

func TestRunConfigRejectsLegacyFlag(t *testing.T) {
	cfg := writeConfig(t, "version: 1\ninput: { type: text }\nstages: [{id: r, type: rules, path: nope.yml}]\nsink: { type: jsonl, stream: stdout }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run", "--config", cfg, "--input", "jsonl"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "declared in the config") {
		t.Fatalf("Run() error = %v, want legacy-flag rejection with --config", err)
	}
}
