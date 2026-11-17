package lock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SHA-256 of "abc", from FIPS 180-2.
const abcSHA = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFileSHA256(t *testing.T) {
	got, err := FileSHA256(write(t, t.TempDir(), "f", "abc"))
	if err != nil || got != abcSHA {
		t.Fatalf("got %q err %v, want %s", got, err, abcSHA)
	}
	if _, err := FileSHA256(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("hashing an absent file succeeded")
	}
}

func lockFor(sha, extract string) *Lock {
	b := Binary{
		Name: "tool", Version: "1.0.0", URL: "https://example.com/{version}", Extract: extract,
		SHA256: map[string]string{"linux/amd64": sha, "darwin/arm64": sha},
	}
	return &Lock{Binaries: []Binary{b}}
}

func TestVerifyBinary(t *testing.T) {
	p := write(t, t.TempDir(), "tool", "abc")
	l := lockFor(abcSHA, "")
	if err := l.VerifyBinary("tool", p, "linux/amd64"); err != nil {
		t.Fatalf("a matching file failed: %v", err)
	}
	err := lockFor(strings.Repeat("0", 64), "").VerifyBinary("tool", p, "linux/amd64")
	if err == nil || !strings.Contains(err.Error(), abcSHA) || !strings.Contains(err.Error(), strings.Repeat("0", 64)) {
		t.Fatalf("a mismatch must name both hashes: %v", err)
	}
	if err := l.VerifyBinary("other", p, "linux/amd64"); err == nil || !strings.Contains(err.Error(), "not pinned") {
		t.Errorf("an unpinned binary: %v", err)
	}
	if err := l.VerifyBinary("tool", p, "linux/arm64"); err == nil || !strings.Contains(err.Error(), "linux/arm64") {
		t.Errorf("a platform with no hash: %v", err)
	}
	if err := l.VerifyBinary("tool", filepath.Join(t.TempDir(), "absent"), "linux/amd64"); err == nil {
		t.Error("an absent file verified")
	}
}

// An entry with an extract path holds the hash of the archive: the installed
// executable cannot be compared, and the check says so rather than passing.
func TestVerifyBinaryWithExtractIsNotVerifiable(t *testing.T) {
	p := write(t, t.TempDir(), "tool", "abc")
	err := lockFor(abcSHA, "tool-{version}/tool").VerifyBinary("tool", p, "linux/amd64")
	if !errors.Is(err, ErrNotVerifiable) {
		t.Fatalf("err = %v, want ErrNotVerifiable", err)
	}
}

func TestHostPlatformIsAKeyOfTheLock(t *testing.T) {
	switch HostPlatform() {
	case "darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64":
	default:
		t.Fatalf("host platform %q is not one of the four the lock pins", HostPlatform())
	}
}

// The verifier loads the lock on first use and fails every check when the lock
// is missing: a binary that cannot be checked is not run.
func TestNewVerifier(t *testing.T) {
	dir := t.TempDir()
	v := NewVerifier(dir)
	p := write(t, dir, "tool", "abc")
	if err := v("tool", p); err == nil {
		t.Fatal("verified with no lock file")
	}
	lockText := strings.Replace(goodLock, "    extract: golangci-lint-{version}-{os}-{arch}/golangci-lint\n", "", 1)
	lockText = strings.Replace(lockText, "name: golangci-lint", "name: tool", 1)
	for _, plat := range []string{"darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64"} {
		i := strings.Index(lockText, "      "+plat+": ")
		j := i + strings.Index(lockText[i:], "\n")
		lockText = lockText[:i] + "      " + plat + ": " + abcSHA + lockText[j:]
	}
	write(t, dir, FileName, lockText)
	v = NewVerifier(dir)
	if err := v("tool", p); err != nil {
		t.Fatalf("a matching binary failed: %v", err)
	}
	if err := v("tool", write(t, dir, "tool2", "abd")); err == nil {
		t.Fatal("a different file verified")
	}
}
