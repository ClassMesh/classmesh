package classmesh_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cleanGoEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
}

func fixtureGo(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	cleanGoEnv(t)
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("fixtures", "consumer")
	return cmd.CombinedOutput()
}

func TestExternalConsumerFixture(t *testing.T) {
	out, err := fixtureGo(t, "test", "-count=1", "./...")
	if err != nil {
		t.Fatalf("fixture go test: %v\n%s", err, out)
	}
}

func TestInternalPackageBoundary(t *testing.T) {
	out, err := fixtureGo(t, "build", "./testdata/internalimport")
	if err == nil || !strings.Contains(string(out), "use of internal package github.com/ClassMesh/classmesh/internal/config not allowed") {
		t.Fatalf("wrong internal package failure:\n%s", out)
	}
}

func TestMinimalConsumerLinkage(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "minimal")
	out, err := fixtureGo(t, "build", "-o", binary, "./cmd/minimal")
	if err != nil {
		t.Fatalf("minimal go build: %v\n%s", err, out)
	}
	metadata, err := exec.Command("go", "version", "-m", binary).CombinedOutput()
	if err != nil {
		t.Fatalf("go version -m: %v\n%s", err, metadata)
	}
	for _, forbidden := range []string{"gopkg.in/yaml.v3", "golang.org/x/text"} {
		if strings.Contains(string(metadata), forbidden) {
			t.Errorf("minimal binary links %s:\n%s", forbidden, metadata)
		}
	}
}
