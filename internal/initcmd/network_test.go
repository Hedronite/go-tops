//go:build network

package initcmd

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Run with: go test -tags network -run Network ./internal/initcmd
// It needs the network and the go toolchain, so it is kept out of the unit run.

// The pins are real: init's tools/go.mod tidies and every module tool builds
// and runs from it (§10).
func TestNetworkToolsModResolvesAndRuns(t *testing.T) {
	dir := newModule(t, "github.com/acme/demo", "1.26")
	o := opts(dir, "binary-cli")
	o.Offline, o.Tidy, o.KitVersion = false, nil, "" // the real go mod tidy; gotops has no release yet
	if code, out, errs := run(o); code != ExitOK {
		t.Fatalf("exit %d\n%s%s", code, out, errs)
	}
	if got := read(t, dir, "tools/go.sum"); got == "" {
		t.Fatal("tools/go.sum is empty")
	}
	for _, tool := range [][]string{{"gotestsum", "--version"}, {"govulncheck", "-version"}, {"go-licenses", "--help"}} {
		cmd := exec.Command("go", append([]string{"tool", "-modfile=tools/go.mod"}, tool...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("go tool %s: %v\n%s", tool[0], err, out)
		}
	}
}

// The embedded SHA-256 of golangci-lint for this host is the digest of the
// file its URL serves.
func TestNetworkLockChecksumMatchesTheDownload(t *testing.T) {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	want, ok := golangciLint.SHA256[platform]
	if !ok {
		t.Skipf("no pin for %s", platform)
	}
	r := strings.NewReplacer("{version}", golangciLint.Version, "{os}", runtime.GOOS, "{arch}", runtime.GOARCH)
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(r.Replace(golangciLint.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	h := sha256.New()
	if _, err := io.Copy(h, resp.Body); err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); resp.StatusCode != 200 || got != want {
		t.Fatalf("status %d, sha256 %s, pinned %s", resp.StatusCode, got, want)
	}
}
