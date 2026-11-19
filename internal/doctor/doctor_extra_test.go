package doctor

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `doctor` is the command an adopter runs to see whether the gate will start. The gate
// refuses a downloaded binary whose SHA-256 the lock does not hold (exit 2), so
// doctor must not say ok for one; and it checks the Go release against the floor the
// configuration names (toolchain.go_floor), not only the protocol's.

const pinnedBinary = "the pinned golangci-lint"

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// lockFor writes a lock that pins golangci-lint to the SHA-256 of content on every platform.
func lockFor(t *testing.T, dir, toolchain, content string) {
	t.Helper()
	h := sum(content)
	doc := "protocol: \"" + protocolID + "\"\ntoolchain: " + toolchain + "\ntrust:\n  mode: checksum\n  note: copied from the release checksum file\nbinaries:\n" +
		"  - name: golangci-lint\n    version: \"2.15.0\"\n    url: https://example.com/{version}/{os}-{arch}\n    sha256:\n" +
		"      darwin/arm64: " + h + "\n      darwin/amd64: " + h + "\n      linux/amd64: " + h + "\n      linux/arm64: " + h + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go-tops.lock"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func binaryAt(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".go-tops", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".go-tops", "bin", "golangci-lint"), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestBinaryTheLockRefusesIsNotOK(t *testing.T) {
	dir := t.TempDir()
	lockFor(t, dir, "go1.27.4", pinnedBinary)
	binaryAt(t, dir, "a golangci-lint from go install")
	cs := Run(env(dir, nil, "go1.27.4"))
	c := get(cs, "golangci-lint")
	if c.Status != Fail || c.Exit != 2 || ExitCode(cs) != 2 {
		t.Fatalf("exit %d, %+v: a binary the lock refuses is a pin mismatch, exit 2", ExitCode(cs), c)
	}
	if !strings.Contains(c.Detail, sum("a golangci-lint from go install")) || !strings.Contains(c.Detail, sum(pinnedBinary)) {
		t.Errorf("the detail should show both hashes: %s", c.Detail)
	}
}

func TestBinaryTheLockHoldsIsOK(t *testing.T) {
	dir := t.TempDir()
	lockFor(t, dir, "go1.27.4", pinnedBinary)
	binaryAt(t, dir, pinnedBinary)
	cs := Run(env(dir, nil, "go1.27.4"))
	if c := get(cs, "golangci-lint"); c.Status != OK || ExitCode(cs) != 0 {
		t.Fatalf("exit %d, %+v", ExitCode(cs), c)
	}
}

func writeConfigFloor(t *testing.T, dir, floor string) {
	t.Helper()
	doc := "protocol: \"0.4.3\"\nversion: \"0.1.0\"\nmodule: example.com/m\nclass: binary-cli\nenforce: ratchet\ntoolchain:\n  go_floor: \"" + floor + "\"\n  go_current: \"1.27\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go-tops.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The configured floor is the adopter's: a toolchain below it fails although it is
// above the protocol's floor (go1.26), and the message names the one that was used.
func TestGoVersionAgainstTheConfiguredFloor(t *testing.T) {
	for _, c := range []struct {
		floor, goVersion string
		ok               bool
	}{
		{"1.27", "go1.26.9", false}, {"1.27", "go1.27.4", true}, {"1.27.0", "go1.26.9", false}, {"1.27.0", "go1.27.0", true}, {"1.26", "go1.26.9", true},
	} {
		t.Run(c.floor+" on "+c.goVersion, func(t *testing.T) {
			dir := t.TempDir()
			writeConfigFloor(t, dir, c.floor)
			cs := Run(env(dir, nil, c.goVersion))
			got := get(cs, "go")
			if c.ok != (got.Status != Fail) {
				t.Fatalf("%+v", got)
			}
			if !c.ok && (got.Exit != 3 || !strings.Contains(got.Detail, "go_floor")) {
				t.Errorf("below the configured floor is an environment error naming go_floor: %+v", got)
			}
			if c.ok && !strings.Contains(got.Detail, "go"+strings.TrimSuffix(c.floor, ".0")) {
				t.Errorf("ok should name the floor it checked: %+v", got)
			}
		})
	}
}
