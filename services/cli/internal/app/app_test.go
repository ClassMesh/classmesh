package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRules = `
rules:
  - category: noise
    contains: ["healthz"]
  - category: billing
    regex: ["payment (failed|declined)"]
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
	return path
}

func decodeLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("invalid JSONL line %q: %v", line, err)
		}
		entries = append(entries, e)
	}
	return entries
}

func TestRunPipelineFromStdinWithReview(t *testing.T) {
	dir := t.TempDir()
	rulesPath := writeFile(t, dir, "rules.yml", testRules)
	reviewPath := filepath.Join(dir, "review.jsonl")

	in := strings.NewReader("GET /healthz 200\npayment declined order=7\ntotally unknown\n")
	var out, errOut bytes.Buffer

	err := Run(context.Background(),
		[]string{"run", "--rules", rulesPath, "--review", reviewPath},
		Streams{In: in, Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}

	entries := decodeLines(t, out.String())
	if len(entries) != 2 {
		t.Fatalf("stdout entries = %d, want 2; out=%s", len(entries), out.String())
	}
	if entries[0]["category"] != "noise" || entries[1]["category"] != "billing" {
		t.Fatalf("categories = %v,%v want noise,billing", entries[0]["category"], entries[1]["category"])
	}
	if entries[0]["id"] != "stdin:1" {
		t.Fatalf("id = %v, want stdin:1", entries[0]["id"])
	}

	reviewData, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatalf("read review file: %v", err)
	}
	review := decodeLines(t, string(reviewData))
	if len(review) != 1 || review[0]["data"] != "totally unknown" {
		t.Fatalf("review entries = %v, want the one unknown line", review)
	}

	if !strings.Contains(errOut.String(), "processed=3 classified=2 review=1") {
		t.Fatalf("stderr = %q, want stats summary", errOut.String())
	}
}

func TestRunPipelineFromFiles(t *testing.T) {
	dir := t.TempDir()
	rulesPath := writeFile(t, dir, "rules.yml", testRules)
	log1 := writeFile(t, dir, "a.log", "GET /healthz 200\n")
	log2 := writeFile(t, dir, "b.log", "payment failed user=2\n")

	var out, errOut bytes.Buffer
	err := Run(context.Background(),
		[]string{"run", "--rules", rulesPath, log1, log2},
		Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	entries := decodeLines(t, out.String())
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (one per file)", len(entries))
	}
	if !strings.Contains(errOut.String(), "processed=2 classified=2 review=0") {
		t.Fatalf("stderr = %q, want merged stats from both files", errOut.String())
	}
}

func TestRunEmitsReasonsInOutput(t *testing.T) {
	dir := t.TempDir()
	rulesPath := writeFile(t, dir, "rules.yml", "rules:\n  - id: health\n    category: noise\n    contains: [\"healthz\"]\n")

	var out, errOut bytes.Buffer
	err := Run(context.Background(),
		[]string{"run", "--rules", rulesPath},
		Streams{In: strings.NewReader("GET /healthz 200\n"), Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}

	entries := decodeLines(t, out.String())
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1; out=%s", len(entries), out.String())
	}
	reasons, ok := entries[0]["reasons"].([]any)
	if !ok || len(reasons) != 1 {
		t.Fatalf("reasons = %v, want one reason in JSONL output", entries[0]["reasons"])
	}
	if first, _ := reasons[0].(map[string]any); first["code"] != "health" {
		t.Fatalf("reason = %v, want code health", reasons[0])
	}
}

func TestRunRequiresRulesFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"run"},
		Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "--rules is required") {
		t.Fatalf("Run() error = %v, want --rules is required", err)
	}
}

func TestRunUnknownCommandFails(t *testing.T) {
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"frobnicate"},
		Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Run() error = %v, want unknown command", err)
	}
}

func TestVersionCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"version"},
		Streams{In: strings.NewReader(""), Out: &out, Err: &errOut}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(out.String(), "classmesh") {
		t.Fatalf("version output = %q", out.String())
	}
}

func TestRunMissingRulesFileFails(t *testing.T) {
	var out, errOut bytes.Buffer
	err := Run(context.Background(),
		[]string{"run", "--rules", filepath.Join(t.TempDir(), "missing.yml")},
		Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil {
		t.Fatal("Run() error = nil, want error for missing rules file")
	}
}

func TestRunRejectsInvalidMinConfidence(t *testing.T) {
	dir := t.TempDir()
	rulesPath := writeFile(t, dir, "rules.yml", testRules)
	var out, errOut bytes.Buffer
	err := Run(context.Background(),
		[]string{"run", "--rules", rulesPath, "--min-confidence", "1.5"},
		Streams{In: strings.NewReader("x\n"), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "min confidence") {
		t.Fatalf("Run() error = %v, want min confidence validation error", err)
	}
}
