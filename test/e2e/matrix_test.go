// This file carries no e2e build tag on purpose: it spawns no containers and
// must run in the unit job, which is where a drifted CI matrix has to be
// caught. Everything it needs it reads off disk.

package e2e

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// workflow is the slice of .github/workflows/ci.yml this test reads. The YAML
// is converted to JSON first (sigs.k8s.io/yaml), hence the json tags.
type workflow struct {
	Jobs struct {
		E2E struct {
			Strategy struct {
				Matrix struct {
					Scenario []struct {
						Name string `json:"name"`
						Test string `json:"test"`
					} `json:"scenario"`
				} `json:"matrix"`
			} `json:"strategy"`
		} `json:"e2e"`
	} `json:"jobs"`
}

// TestE2EMatrixIsComplete pins every e2e test to a CI scenario. The matrix
// names tests by hand, so without this a new e2e test looks like it is covered
// while never running anywhere — the failure mode that left five of them
// unrun. It also catches the reverse: a scenario naming a test that was
// renamed or deleted silently passes in CI, since `go test -run` matching
// nothing is a success.
func TestE2EMatrixIsComplete(t *testing.T) {
	inMatrix := map[string]string{} // test name -> scenario name
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	var wf workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}
	for _, s := range wf.Jobs.E2E.Strategy.Matrix.Scenario {
		inMatrix[s.Test] = s.Name
	}
	if len(inMatrix) == 0 {
		t.Fatal("no e2e scenarios found in ci.yml; did the workflow structure change?")
	}

	for _, name := range e2eTestNames(t) {
		if _, ok := inMatrix[name]; !ok {
			t.Errorf("e2e test %s runs nowhere in CI; add a scenario for it to "+
				".github/workflows/ci.yml", name)
		}
		delete(inMatrix, name)
	}
	for test, scenario := range inMatrix {
		t.Errorf("CI scenario %q runs %s, which does not exist; `go test -run` "+
			"matching nothing passes, so the scenario is silently empty", scenario, test)
	}
}

// e2eTestNames returns the Test functions declared in this package, this file
// excepted. The parser does not evaluate build constraints, so the e2e-tagged
// files are read here even though they are not compiled into this binary.
func e2eTestNames(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse test/e2e: %v", err)
	}
	var names []string
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			if filepath.Base(path) == "matrix_test.go" {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
					continue
				}
				names = append(names, fn.Name.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("no e2e tests found; did the package layout change?")
	}
	sort.Strings(names)
	return names
}
