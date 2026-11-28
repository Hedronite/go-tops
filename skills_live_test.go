//go:build skills && adoption

package gotops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The skills against the real gotops on a fixture module: adopt writes the first
// ledger, check passes once it is committed, and the gate skill judges a real
// report. Then a test is broken and the gate skill says so. It reads the network
// (init tidies tools/go.mod) and needs golangci-lint on PATH, so it runs with
// both tags: go test -tags "skills adoption" -run TestSkillsRun .

const liveSource = `package main

import "fmt"

func Classify(n int) string {
	switch {
	case n < 0:
		return "neg"
	case n == 0:
		return "zero"
	}
	return "pos"
}

func main() { fmt.Println(Classify(3)) }
`

const liveTest = `package main

import "testing"

func TestClassify(t *testing.T) {
	if Classify(-1) != "neg" || Classify(0) != "zero" || Classify(2) != "pos" {
		t.Fatal("wrong class")
	}
}
`

func TestSkillsRunTheRealGotopsOnAFixtureModule(t *testing.T) {
	needNu(t)
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		t.Skip("golangci-lint is not on PATH; the weekly adoption job installs it")
	}
	bin := filepath.Join(t.TempDir(), "gotops")
	if out, err := exec.CommandContext(context.Background(), "go", "build", "-o", bin, "./cmd/gotops").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	r := newRepo(t)
	r.write("go.mod", "module github.com/example/fx\n\ngo 1.27.0\n")
	r.write("cmd/fx/main.go", liveSource)
	r.write("cmd/fx/main_test.go", liveTest)
	r.commit("fixture")
	r.git("update-ref", "refs/remotes/origin/main", "HEAD")

	adopt := runSkill(t, "go-tops-adopt", r.dir, nil, "adopt", "--class", "binary-cli", "--owner", "acme", "--allow-host-ledger", "--gotops", bin)
	if !adopt.OK || adopt.Data["commit_required"] != true {
		t.Fatalf("adopt: ok = %v, commit_required = %v: %s", adopt.OK, adopt.Data["commit_required"], adopt.Error)
	}
	r.commit("adopt")
	r.git("update-ref", "refs/remotes/origin/main", "HEAD")
	if got := runSkill(t, "go-tops-adopt", r.dir, nil, "check", "--gotops", bin); !got.OK {
		t.Fatalf("check after the commit: %s", got.Error)
	}

	args := []string{"--gotops", bin}
	gate := runSkill(t, "go-tops-gate", r.dir, nil, args...)
	if !gate.OK {
		t.Fatalf("the gate skill on a clean fixture: %s", gate.Error)
	}

	r.write("cmd/fx/main_test.go", strings.Replace(liveTest, `"pos"`, `"negative"`, 1))
	broken := runSkill(t, "go-tops-gate", r.dir, nil, args...)
	if broken.OK || !strings.Contains(broken.Error, "tests") {
		t.Fatalf("a failing test gave ok = %v, error %q", broken.OK, broken.Error)
	}
	if _, err := os.Stat(filepath.Join(r.dir, ".go-tops", "report.json")); err != nil {
		t.Fatal(err)
	}
}
