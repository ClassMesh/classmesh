package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cm.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidateCommandOK(t *testing.T) {
	cfg := writeConfig(t, "version: 1\ninput: { type: text }\nstages: [{id: rules, type: rules, path: rules.yml, gate: 1.0}]\nsink: { type: jsonl, stream: stdout }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"validate", "--config", cfg}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err != nil {
		t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "structurally valid") {
		t.Fatalf("stdout = %q, want a validity confirmation", out.String())
	}
}

func TestValidateCommandRejectsBadConfig(t *testing.T) {
	cfg := writeConfig(t, "version: 1\ninput: { type: text }\nstages: [{id: r, type: onnx}]\nsink: { type: jsonl, stream: stdout }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"validate", "--config", cfg}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "is not one of") {
		t.Fatalf("Run() error = %v, want invalid-stage-type error", err)
	}
}

func TestValidateRejectsPositionalArg(t *testing.T) {
	cfg := writeConfig(t, "version: 1\ninput: { type: text }\nstages: [{id: r, type: rules}]\nsink: { type: jsonl, stream: stdout }\n")
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"validate", "--config", cfg, "extra"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("Run() error = %v, want unexpected-argument error", err)
	}
}

func TestValidateRequiresConfig(t *testing.T) {
	var out, errOut bytes.Buffer
	err := Run(context.Background(), []string{"validate"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if err == nil || !strings.Contains(err.Error(), "--config is required") {
		t.Fatalf("Run() error = %v, want --config required error", err)
	}
}
