package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hedronite/go-tops/internal/cli"
)

// runIn runs the gotops process in dir with extra environment variables.
func runIn(t *testing.T, dir string, env []string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
	cmd.Dir = dir
	// CI is cleared so a test does not depend on where it runs (the kit's own CI sets
	// CI=true, which changes what lock --update and gate do); a test that wants it sets it.
	cmd.Env = append(append(os.Environ(), asMain+"=1", "CI="), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("run gotops %v: %v", args, err)
	}
	return code, out.String(), errb.String()
}

func writeFile(t *testing.T, dir, rel, data string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o755); err != nil {
		t.Fatal(err)
	}
}

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- report

func TestReportRendersTheLastReport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".go-tops/report.json", repoFile(t, "config/gate-report.example.json"))
	code, stdout, stderr := runIn(t, dir, nil, "report")
	if code != 0 || !strings.Contains(stdout, "FAIL (exit 1)") || !strings.Contains(stdout, "failed: crap") || stderr != "" {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	code, stdout, _ = runIn(t, dir, nil, "report", "--format", "json")
	if code != 0 || !strings.HasPrefix(stdout, "{\n") || !strings.Contains(stdout, `"outcome": "fail"`) {
		t.Fatalf("json: exit %d\n%s", code, stdout)
	}
}

func TestReportJunitAndSarifExitTwoNamingM7(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".go-tops/report.json", repoFile(t, "config/gate-report.example.json"))
	for _, f := range []string{"junit", "sarif"} {
		code, stdout, stderr := runIn(t, dir, nil, "report", "--format", f)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "M7") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", f, code, stdout, stderr)
		}
	}
	if code, _, stderr := runIn(t, dir, nil, "report", "--format", "xml"); code != 2 || !strings.Contains(stderr, "text or json") {
		t.Errorf("xml: exit %d, %q", code, stderr)
	}
}

func TestReportWithoutAReportOrWithABadOneExitsTwo(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runIn(t, dir, nil, "report"); code != 2 || !strings.Contains(stderr, "run gotops gate first") {
		t.Errorf("no report: exit %d, %q", code, stderr)
	}
	writeFile(t, dir, ".go-tops/report.json", fmt.Sprintf(`{"protocol": %q}`, protocolID))
	if code, _, stderr := runIn(t, dir, nil, "report"); code != 2 || !strings.Contains(stderr, "not a valid gate report") {
		t.Errorf("bad report: exit %d, %q", code, stderr)
	}
}

func TestReportPathFlagAndUsage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "elsewhere.json", repoFile(t, "config/gate-report.example.json"))
	if code, stdout, _ := runIn(t, dir, nil, "report", "--path", "elsewhere.json"); code != 0 || !strings.Contains(stdout, "Go-TOPS gate report") {
		t.Errorf("--path: exit %d", code)
	}
	if code, _, stderr := runIn(t, dir, nil, "report", "extra"); code != 2 || !strings.Contains(stderr, "Usage: gotops report") {
		t.Errorf("argument: exit %d, %q", code, stderr)
	}
	if code, stdout, _ := runIn(t, dir, nil, "report", "--help"); code != 0 || !strings.Contains(stdout, "--format") {
		t.Errorf("--help: exit %d", code)
	}
}

// ---- lock

func goVersion(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOVERSION").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// lockRepo writes a lock pinning the running toolchain and one binary whose
// SHA-256 is that of content, for every platform.
func lockRepo(t *testing.T, content string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	h := hex.EncodeToString(sum[:])
	dir := t.TempDir()
	writeFile(t, dir, "go-tops.lock", "protocol: \""+protocolID+"\"\ntoolchain: "+goVersion(t)+"\ntrust:\n  mode: checksum\n  note: copied from the release checksum file\nbinaries:\n"+
		"  - name: gotools\n    version: \"1.0.0\"\n    url: https://example.com/{version}/{os}-{arch}\n    sha256:\n"+
		"      darwin/arm64: "+h+"\n      darwin/amd64: "+h+"\n      linux/amd64: "+h+"\n      linux/arm64: "+h+"\n")
	return dir
}

func TestLockVerifyPassesThenFailsOnAStaleBinary(t *testing.T) {
	dir := lockRepo(t, "pinned build")
	writeFile(t, dir, ".go-tops/bin/gotools", "pinned build")
	code, stdout, stderr := runIn(t, dir, nil, "lock", "--verify")
	if code != 0 || !strings.Contains(stdout, "toolchain") || !strings.Contains(stdout, "gotools") {
		t.Fatalf("exit %d\n%s%s", code, stdout, stderr)
	}
	writeFile(t, dir, ".go-tops/bin/gotools", "an older build")
	code, stdout, _ = runIn(t, dir, nil, "lock", "--verify")
	if code != 2 || !strings.Contains(stdout, "MISMATCH") {
		t.Fatalf("a stale binary must exit 2: exit %d\n%s", code, stdout)
	}
}

func TestLockVerifyWrongHashAndMissingToolAndNoLock(t *testing.T) {
	dir := lockRepo(t, "pinned build")
	if code, stdout, _ := runIn(t, dir, []string{"PATH=" + t.TempDir()}, "lock", "--verify"); code != 1 || !strings.Contains(stdout, "MISSING") {
		t.Errorf("an absent tool exits 1: exit %d\n%s", code, stdout)
	}
	writeFile(t, dir, ".go-tops/bin/gotools", "something else entirely")
	if code, _, _ := runIn(t, dir, nil, "lock", "--verify"); code != 2 {
		t.Errorf("a wrong SHA exits 2, got %d", code)
	}
	if code, stdout, _ := runIn(t, t.TempDir(), nil, "lock", "--verify"); code != 2 || !strings.Contains(stdout, "go-tops.lock") {
		t.Errorf("no lock exits 2: %d\n%s", code, stdout)
	}
}

func TestLockToolchainMismatchExitsTwo(t *testing.T) {
	dir := lockRepo(t, "x")
	writeFile(t, dir, ".go-tops/bin/gotools", "x")
	data, err := os.ReadFile(filepath.Join(dir, "go-tops.lock"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go-tops.lock", strings.Replace(string(data), "toolchain: "+goVersion(t), "toolchain: go1.20.1", 1))
	if code, stdout, _ := runIn(t, dir, nil, "lock", "--verify"); code != 2 || !strings.Contains(stdout, "go1.20.1") {
		t.Errorf("exit %d\n%s", code, stdout)
	}
}

// §10: --update is a reviewed change, refused when CI=true (policy, exit 4).
func TestLockUpdateIsRefusedInCI(t *testing.T) {
	dir := lockRepo(t, "x")
	before, err := os.ReadFile(filepath.Join(dir, "go-tops.lock"))
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runIn(t, dir, []string{"CI=true"}, "lock", "--update")
	if code != 4 || stdout != "" || !strings.Contains(stderr, "CI=true") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	after, err := os.ReadFile(filepath.Join(dir, "go-tops.lock"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("a refused update changed the lock")
	}
	// refused even where there is no lock to update
	if code, _, _ := runIn(t, t.TempDir(), []string{"CI=true"}, "lock", "--update"); code != 4 {
		t.Errorf("CI refusal comes first, exit %d", code)
	}
}

func TestLockNeedsExactlyOneMode(t *testing.T) {
	dir := lockRepo(t, "x")
	for _, args := range [][]string{{"lock"}, {"lock", "--verify", "--update"}, {"lock", "--verify", "extra"}, {"lock", "--bogus"}, {"lock", "--update", "no-at-sign"}} {
		if code, _, stderr := runIn(t, dir, nil, args...); code != 2 || !strings.Contains(stderr, "Usage: gotops lock") {
			t.Errorf("%v: exit %d, %q", args, code, stderr)
		}
	}
}

// ---- doctor

func TestDoctorWithoutACCompilerExitsThree(t *testing.T) {
	code, stdout, _ := runIn(t, t.TempDir(), []string{"CC=/nonexistent"}, "doctor")
	if code != 3 || !strings.Contains(stdout, "c-compiler") || !strings.Contains(stdout, "/nonexistent") {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

func TestDoctorHealthyAndPinned(t *testing.T) {
	dir := lockRepo(t, "x")
	writeFile(t, dir, ".go-tops/bin/gotools", "x")
	code, stdout, _ := runIn(t, dir, []string{"GOTOOLCHAIN=local"}, "doctor")
	if code != 0 || !strings.Contains(stdout, "ok") || !strings.Contains(stdout, "GOTOOLCHAIN") {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

func TestDoctorMissingToolAndPinMismatch(t *testing.T) {
	dir := lockRepo(t, "x")
	if code, stdout, _ := runIn(t, dir, nil, "doctor"); code != 1 || !strings.Contains(stdout, "tool-missing") {
		t.Errorf("a missing tool exits 1: %d\n%s", code, stdout)
	}
	data, err := os.ReadFile(filepath.Join(dir, "go-tops.lock"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "go-tops.lock", strings.Replace(string(data), "toolchain: "+goVersion(t), "toolchain: go1.20.1", 1))
	if code, stdout, _ := runIn(t, dir, nil, "doctor"); code != 2 || !strings.Contains(stdout, "go1.20.1") {
		t.Errorf("a pin mismatch exits 2: %d\n%s", code, stdout)
	}
}

func TestDoctorRefusesArguments(t *testing.T) {
	if code, _, stderr := runIn(t, t.TempDir(), nil, "doctor", "extra"); code != 2 || !strings.Contains(stderr, "Usage: gotops doctor") {
		t.Errorf("exit %d, %q", code, stderr)
	}
}

// ---- version

// §12.1, §13: version adds the pinned tool versions, read from go-tops.lock and
// tools/go.mod in the working directory; without them it prints just the two lines.
func TestVersionListsThePinnedTools(t *testing.T) {
	dir := lockRepo(t, "x")
	writeFile(t, dir, "tools/go.mod", "module example.com/m/tools\n\ngo 1.26\n\ntool gotest.tools/gotestsum\n\nrequire gotest.tools/gotestsum v1.13.0\n")
	code, stdout, stderr := runIn(t, dir, nil, "version")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"protocol " + protocolID + "\npackage 0.0.0-dev\n", "toolchain " + goVersion(t) + " (go-tops.lock)\n", "tool gotools 1.0.0 (go-tops.lock)\n", "tool gotestsum v1.13.0 (tools/go.mod)\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("version lacks %q:\n%s", want, stdout)
		}
	}
	_, bare, _ := runIn(t, t.TempDir(), nil, "version")
	if strings.Count(bare, "\n") != 2 {
		t.Errorf("outside a repo version prints two lines:\n%s", bare)
	}
}

// lock --update: a download that fails (here a refused connection, so the test
// needs no network) is an environment error, exit 3; an invalid lock is exit 2;
// a lock whose hashes are already as pinned is left alone (SPEC-m1 B19).
func TestLockUpdateExitCodes(t *testing.T) {
	dir := lockRepo(t, "x")
	data, err := os.ReadFile(filepath.Join(dir, "go-tops.lock"))
	if err != nil {
		t.Fatal(err)
	}
	refused := strings.Replace(string(data), "https://example.com/{version}/{os}-{arch}", "https://127.0.0.1:1/{version}/{os}-{arch}", 1)
	writeFile(t, dir, "go-tops.lock", refused)
	code, _, stderr := runIn(t, dir, nil, "lock", "--update")
	if code != 3 || !strings.Contains(stderr, "gotools") {
		t.Errorf("a failed download: exit %d, %q", code, stderr)
	}
	after, err := os.ReadFile(filepath.Join(dir, "go-tops.lock"))
	if err != nil || string(after) != refused {
		t.Errorf("a failed download changed the lock: %v", err)
	}
	writeFile(t, dir, "go-tops.lock", "protocol: \""+protocolID+"\"\nbogus_key: 1\n")
	if code, _, stderr := runIn(t, dir, nil, "lock", "--update"); code != 2 {
		t.Errorf("an invalid lock: exit %d, %q", code, stderr)
	}
	if code, _, _ := runIn(t, t.TempDir(), nil, "lock", "--update"); code != 2 {
		t.Errorf("no lock: exit %d, want 2", code)
	}
}

// updateIn runs `gotops lock --update` in dir, in this process, with every
// download answered by body, so the command runs end to end without the network.
func updateIn(t *testing.T, dir, body string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	old := fetchLock
	t.Cleanup(func() { fetchLock = old })
	fetchLock = func(string) ([]byte, error) { return []byte(body), nil }
	t.Setenv("CI", "") // --update is refused under CI; the gate's own test run sets it
	t.Chdir(dir)
	var o, e bytes.Buffer
	code = runLock(cli.NewEnv(&o, &e), append([]string{"--update"}, args...))
	return code, o.String(), e.String()
}

// A lock whose downloads hash as pinned is left alone, and the command says so.
func TestLockUpdateSaysWhenTheLockIsCurrent(t *testing.T) {
	dir := lockRepo(t, "x")
	before := fileBytes(t, dir, "go-tops.lock")
	code, stdout, stderr := updateIn(t, dir, "x")
	if code != 0 || !strings.Contains(stdout, "is current") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if fileBytes(t, dir, "go-tops.lock") != before {
		t.Error("a current lock was rewritten")
	}
}

// A download that hashes differently rewrites the lock and prints each hash that
// moved, then how many.
func TestLockUpdatePrintsEachHashItChanged(t *testing.T) {
	dir := lockRepo(t, "x")
	code, stdout, stderr := updateIn(t, dir, "a different download")
	if code != 0 || !strings.Contains(stdout, "gotools") || !strings.Contains(stdout, "->") || !strings.Contains(stdout, "hashes changed") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(fileBytes(t, dir, "go-tops.lock"), "tofu") {
		t.Error("the lock was not rewritten with the trust mode tofu")
	}
}

// A pin to raise must be name@version.
func TestLockUpdateRefusesAPinThatIsNotNameAtVersion(t *testing.T) {
	code, _, stderr := updateIn(t, lockRepo(t, "x"), "x", "gotools")
	if code != 2 || !strings.Contains(stderr, "name@version") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}
