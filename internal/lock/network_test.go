//go:build network

package lock

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run with: go test -tags network -run Network ./internal/lock
// These need the network and the go toolchain, so the unit run leaves them out.

func goRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, out)
	}
}

func toolsRepo(t *testing.T, dir, file, version string) {
	t.Helper()
	mod := "module example.com/demo/tools\n\ngo 1.26\n\ntool gotest.tools/gotestsum\n\nrequire gotest.tools/gotestsum " + version + "\n"
	// -modfile needs a main module to replace the go.mod of
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools", file), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	goRun(t, dir, "mod", "tidy", "-modfile=tools/"+file)
}

// A module tool is checked by building it and reading go version -m: the pinned
// build passes, and an older build left in .go-tops/bin is a mismatch (exit 2).
func TestNetworkModuleToolBuildAndStaleBinary(t *testing.T) {
	out, err := execRunner(t.TempDir(), "go", "env", "GOVERSION")
	if err != nil {
		t.Fatal(err)
	}
	dir := writeLock(t, strings.TrimSpace(out), "tool", sha(t, "x"), false)
	place(t, dir, "tool", "x")
	toolsRepo(t, dir, "go.mod", "v1.13.0")
	verify := func() []Finding { return Verify(context.Background(), VerifyOptions{Dir: dir, Platform: platform}) }
	fs := verify()
	if f, ok := find(fs, "gotestsum"); !ok || f.Status != StatusOK || ExitCode(fs) != 0 {
		t.Fatalf("the pinned build must verify: exit %d, %+v", ExitCode(fs), fs)
	}
	// build v1.12.3 from a second modfile into .go-tops/bin
	toolsRepo(t, dir, "old.mod", "v1.12.3")
	goRun(t, dir, "build", "-modfile=tools/old.mod", "-o", filepath.Join(dir, ".go-tops", "bin", "gotestsum"), "gotest.tools/gotestsum")
	fs = verify()
	if f, ok := find(fs, "gotestsum (.go-tops/bin)"); !ok || f.Status != StatusMismatch || ExitCode(fs) != 2 {
		t.Fatalf("an older build in .go-tops/bin must mismatch (exit 2): exit %d, %+v", ExitCode(fs), fs)
	}
}

// lock --update re-hashes the real downloads: for the pinned golangci-lint the
// hashes equal the ones recorded from its release checksum file, so nothing
// changes and the trust mode stays checksum.
func TestNetworkUpdateReproducesTheExampleHashes(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads four archives")
	}
	dir := copyExample(t)
	fetch := func(url string) ([]byte, error) {
		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Get(url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	changes, err := Update(UpdateOptions{Dir: dir, Fetch: fetch})
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes %v, err %v", changes, err)
	}
	if l, err := LoadDir(dir); err != nil || l.Trust.Mode != "checksum" {
		t.Fatalf("%+v %v", l, err)
	}
}
