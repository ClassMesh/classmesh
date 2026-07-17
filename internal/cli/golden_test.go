package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// examplePath resolves a file under the repo's examples/ directory, anchored to
// this source file so it works regardless of the test's working directory.
func examplePath(t *testing.T, name string) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(self), "..", "..", "examples", name)
}

// TestExamplesGolden runs the shipped example inputs through the CLI and checks
// the output against recorded golden files, so the examples in the README stay
// runnable and their classifications stay stable.
func TestExamplesGolden(t *testing.T) {
	rules := examplePath(t, "rules.yml")
	cases := []struct {
		name   string
		args   []string
		input  string
		golden string
	}{
		{"text", []string{"run", "--rules", rules}, "logs.txt", "testdata/golden-text.jsonl"},
		{"jsonl", []string{"run", "--rules", rules, "--input", "jsonl"}, "events.jsonl", "testdata/golden-jsonl.jsonl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, err := os.ReadFile(examplePath(t, tc.input))
			if err != nil {
				t.Fatalf("read example input: %v", err)
			}
			want, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}

			var out, errOut bytes.Buffer
			if err := Run(context.Background(), tc.args, Streams{In: bytes.NewReader(in), Out: &out, Err: &errOut}); err != nil {
				t.Fatalf("Run() error = %v, stderr=%s", err, errOut.String())
			}
			if out.String() != string(want) {
				t.Fatalf("%s output does not match %s\n got:\n%s\nwant:\n%s", tc.name, tc.golden, out.String(), want)
			}
		})
	}
}
