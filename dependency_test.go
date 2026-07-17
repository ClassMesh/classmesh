package classmesh_test

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/ClassMesh/classmesh"

type listedPackage struct {
	path     string
	standard bool
}

func goList(t *testing.T, args ...string) []listedPackage {
	t.Helper()
	cleanGoEnv(t)
	cmd := exec.Command("go", append([]string{"list", "-f", `{{.ImportPath}}|{{.Standard}}`}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %v: %v", args, err)
	}
	var packages []listedPackage
	for _, line := range strings.Fields(string(out)) {
		path, standard, _ := strings.Cut(line, "|")
		packages = append(packages, listedPackage{path: path, standard: standard == "true"})
	}
	return packages
}

func TestDependencyMatrix(t *testing.T) {
	for _, dep := range goList(t, "-deps", ".") {
		if !dep.standard && dep.path != modulePath {
			t.Errorf("root has non-standard dependency %s", dep.path)
		}
	}

	forbidden := map[string]bool{"gopkg.in/yaml.v3": true, "golang.org/x/text": true}
	for _, pkg := range goList(t, "./...") {
		rel := strings.TrimPrefix(pkg.path, modulePath)
		if rel == pkg.path || strings.HasPrefix(rel, "/internal/") || strings.HasPrefix(rel, "/cmd/") || strings.HasPrefix(rel, "/examples/") {
			continue
		}
		for _, dep := range goList(t, "-deps", pkg.path) {
			if forbidden[dep.path] {
				t.Errorf("%s depends on %s", pkg.path, dep.path)
			}
			allowed := (pkg.path == modulePath+"/rules" || pkg.path == modulePath+"/schema") && dep.path == modulePath+"/internal/fieldpath"
			if strings.HasPrefix(dep.path, modulePath+"/internal/") && !allowed {
				t.Errorf("%s depends on forbidden internal package %s", pkg.path, dep.path)
			}
		}
	}
}
