package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

type goldenCase struct {
	Name    string            `json:"name"`
	Args    []string          `json:"args"`
	Stdin   string            `json:"stdin"`
	Created map[string]string `json:"created"`
}

var slogTime = regexp.MustCompile(`time=\S+ `)

const goldenLDFlags = "-X github.com/ClassMesh/classmesh/internal/version.Version=golden -X github.com/ClassMesh/classmesh/internal/version.Commit=golden -X github.com/ClassMesh/classmesh/internal/version.Date=golden"

// normalizeLogTime removes the variable slog timestamp from golden stderr.
func normalizeLogTime(data []byte) []byte {
	return slogTime.ReplaceAll(data, []byte("time=<normalized> "))
}

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

func copyGoldenExamples(t *testing.T, root string) {
	t.Helper()
	if err := os.CopyFS(filepath.Join(root, "examples"), os.DirFS(examplePath(t, "."))); err != nil {
		t.Fatal(err)
	}
}

func readGolden(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCLIGoldens(t *testing.T) {
	repo := filepath.Clean(filepath.Join(examplePath(t, "."), ".."))
	binary := filepath.Join(t.TempDir(), "classmesh")
	build := exec.Command("go", "build", "-ldflags", goldenLDFlags, "-o", binary, "./cmd/classmesh")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	goldenDir := filepath.Join("testdata", "goldens")
	var cases []goldenCase
	if err := json.Unmarshal(readGolden(t, goldenDir, "manifest.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			work := t.TempDir()
			copyGoldenExamples(t, work)
			cmd := exec.Command(binary, tc.Args...)
			cmd.Dir = work
			if tc.Stdin != "" {
				cmd.Stdin = bytes.NewReader(readGolden(t, work, tc.Stdin))
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("run CLI: %v", err)
				}
				code = exitErr.ExitCode()
			}
			wantCode, err := strconv.Atoi(strings.TrimSpace(string(readGolden(t, goldenDir, tc.Name+".code"))))
			if err != nil {
				t.Fatal(err)
			}
			if code != wantCode {
				t.Errorf("exit code = %d, want %d", code, wantCode)
			}
			if want := readGolden(t, goldenDir, tc.Name+".out"); !bytes.Equal(stdout.Bytes(), want) {
				t.Errorf("stdout mismatch\ngot:\n%s\nwant:\n%s", stdout.Bytes(), want)
			}
			wantErr := normalizeLogTime(readGolden(t, goldenDir, tc.Name+".err"))
			if gotErr := normalizeLogTime(stderr.Bytes()); !bytes.Equal(gotErr, wantErr) {
				t.Errorf("stderr mismatch\ngot:\n%s\nwant:\n%s", gotErr, wantErr)
			}
			for path, golden := range tc.Created {
				got := readGolden(t, work, path)
				if want := readGolden(t, goldenDir, golden); !bytes.Equal(got, want) {
					t.Errorf("created file %s mismatch\ngot:\n%s\nwant:\n%s", path, got, want)
				}
			}
		})
	}
}
