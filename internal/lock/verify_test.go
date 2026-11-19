package lock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The oracle is GO_TOPS.md §10 and §12.1: lock --verify re-checks every pin.
// A module tool is checked by building it and reading go version -m (never a
// --version text); a downloaded binary by its SHA-256; the toolchain by
// go env GOVERSION. A pin present at another version or hash is a mismatch
// (exit 2); an absent tool is tool-missing (exit 1) (§12.2).

const platform = "linux/amd64"

// writeLock writes a lock with one downloaded binary whose SHA-256 for the
// platform is the given hash, and returns the repository directory.
func writeLock(t *testing.T, toolchain, name, hash string, extract bool) string {
	t.Helper()
	dir := t.TempDir()
	ex := ""
	if extract {
		ex = "    extract: " + name + "-{version}/" + name + "\n"
	}
	doc := "protocol: \"" + protocolID + "\"\ntoolchain: " + toolchain + "\ntrust:\n  mode: checksum\n  note: copied from the release checksum file\nbinaries:\n" +
		"  - name: " + name + "\n    version: \"1.2.3\"\n    url: https://example.com/v{version}/" + name + "-{os}-{arch}\n" + ex + "    sha256:\n"
	archive := hash
	if extract {
		archive = strings.Repeat("c", 64) // the archive hash is never the executable hash
	}
	doc += "      darwin/arm64: " + strings.Repeat("a", 64) + "\n      darwin/amd64: " + strings.Repeat("b", 64) + "\n" +
		"      linux/amd64: " + archive + "\n      linux/arm64: " + strings.Repeat("d", 64) + "\n"
	if extract {
		doc += "    executable_sha256:\n      darwin/arm64: " + strings.Repeat("e", 64) + "\n      darwin/amd64: " + strings.Repeat("f", 64) + "\n" +
			"      linux/amd64: " + hash + "\n      linux/arm64: " + strings.Repeat("1", 64) + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func sha(t *testing.T, data string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(p, []byte(data), 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := FileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func place(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, ".go-tops", "bin", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakeRun answers go env GOVERSION with goVersion and refuses every other command.
func fakeRun(goVersion string) Runner {
	return func(_ string, argv ...string) (string, error) {
		if strings.Join(argv, " ") == "go env GOVERSION" {
			return goVersion + "\n", nil
		}
		return "", errors.New("unexpected command: " + strings.Join(argv, " "))
	}
}

func noPath(string) (string, error) { return "", errors.New("not on PATH") }

func find(fs []Finding, subject string) (Finding, bool) {
	for _, f := range fs {
		if f.Subject == subject {
			return f, true
		}
	}
	return Finding{}, false
}

func TestVerifyPassesWhenEveryPinMatches(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "good binary"), false)
	place(t, dir, "tool", "good binary")
	fs := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.27.4"), LookPath: noPath})
	if code := ExitCode(fs); code != 0 {
		t.Fatalf("exit %d: %+v", code, fs)
	}
	for _, subject := range []string{"toolchain", "tool"} {
		if f, ok := find(fs, subject); !ok || f.Status != StatusOK {
			t.Errorf("%s: %+v", subject, f)
		}
	}
}

// A binary replaced by another build has another SHA-256: a mismatch, exit 2.
func TestVerifyStaleBinaryIsAMismatch(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "pinned build"), false)
	place(t, dir, "tool", "an older build")
	fs := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.27.4"), LookPath: noPath})
	f, _ := find(fs, "tool")
	if f.Status != StatusMismatch || ExitCode(fs) != 2 || !strings.Contains(f.Detail, "SHA-256") {
		t.Fatalf("exit %d, finding %+v", ExitCode(fs), f)
	}
}

func TestVerifyAWrongHashInTheLockIsAMismatch(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", strings.Repeat("0", 64), false)
	place(t, dir, "tool", "good binary")
	if code := ExitCode(Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.27.4"), LookPath: noPath})); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

// The binary is found in .go-tops/bin first and on PATH after.
func TestVerifyFindsTheBinaryOnPath(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "good binary"), false)
	other := t.TempDir()
	p := filepath.Join(other, "tool")
	if err := os.WriteFile(p, []byte("good binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	look := func(string) (string, error) { return p, nil }
	if code := ExitCode(Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.27.4"), LookPath: look})); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	place(t, dir, "tool", "shadow in .go-tops/bin")
	if code := ExitCode(Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.27.4"), LookPath: look})); code != 2 {
		t.Fatalf(".go-tops/bin comes first, so the shadow must mismatch: exit %d", code)
	}
}

// §12.2: an absent tool is tool-missing, exit 1; a mismatch outranks it.
func TestVerifyAbsentToolExitsOne(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "good binary"), false)
	fs := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.27.4"), LookPath: noPath})
	if f, _ := find(fs, "tool"); f.Status != StatusMissing || ExitCode(fs) != 1 {
		t.Fatalf("exit %d, finding %+v", ExitCode(fs), f)
	}
	bad := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.26.1"), LookPath: noPath})
	if ExitCode(bad) != 2 {
		t.Fatalf("a mismatch outranks a missing tool: exit %d", ExitCode(bad))
	}
}

func TestVerifyToolchainMismatchExitsTwo(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "x"), false)
	place(t, dir, "tool", "x")
	fs := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.27.3"), LookPath: noPath})
	f, _ := find(fs, "toolchain")
	if f.Status != StatusMismatch || ExitCode(fs) != 2 || !strings.Contains(f.Detail, "go1.27.3") || !strings.Contains(f.Detail, "go1.27.4") {
		t.Fatalf("exit %d, finding %+v", ExitCode(fs), f)
	}
}

// A lock that does not load fails the run with exit 2, by the schema.
func TestVerifyMissingOrInvalidLockExitsTwo(t *testing.T) {
	empty := t.TempDir()
	if code := ExitCode(Verify(VerifyOptions{Dir: empty, Run: fakeRun("go1.27.4"), LookPath: noPath})); code != 2 {
		t.Errorf("no lock: exit %d, want 2", code)
	}
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "x"), false)
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("protocol: \""+protocolID+"\"\nbogus_key: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := ExitCode(Verify(VerifyOptions{Dir: dir, Run: fakeRun("go1.27.4"), LookPath: noPath})); code != 2 {
		t.Errorf("invalid lock: exit %d, want 2", code)
	}
}

// With an extract path the lock holds the hash of the archive in sha256 and of
// the executable in executable_sha256: the installed file is the executable, so
// that is the one compared (§10).
func TestVerifyAnArchiveEntryComparesTheExecutableHash(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "the executable inside the archive"), true)
	place(t, dir, "tool", "the executable inside the archive")
	fs := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.27.4"), LookPath: noPath})
	if f, _ := find(fs, "tool"); f.Status != StatusOK || ExitCode(fs) != 0 {
		t.Fatalf("exit %d, finding %+v", ExitCode(fs), f)
	}
	place(t, dir, "tool", "an older build of the executable")
	fs = Verify(VerifyOptions{Dir: dir, Platform: platform, Run: fakeRun("go1.27.4"), LookPath: noPath})
	if f, _ := find(fs, "tool"); f.Status != StatusMismatch || ExitCode(fs) != 2 {
		t.Fatalf("a stale executable must mismatch: exit %d, finding %+v", ExitCode(fs), f)
	}
}

const toolsMod = `module example.com/demo/tools

go 1.26

tool (
	github.com/google/go-licenses/v2
	golang.org/x/vuln/cmd/govulncheck
)

tool gotest.tools/gotestsum

require (
	github.com/google/go-licenses/v2 v2.0.1
	golang.org/x/vuln v1.9.0
	gotest.tools/gotestsum v1.13.0 // indirect
)
`

func TestParseToolsMod(t *testing.T) {
	got, err := ParseToolsMod([]byte(toolsMod))
	if err != nil {
		t.Fatal(err)
	}
	want := []ModuleTool{
		{"go-licenses", "github.com/google/go-licenses/v2", "github.com/google/go-licenses/v2", "v2.0.1"},
		{"govulncheck", "golang.org/x/vuln/cmd/govulncheck", "golang.org/x/vuln", "v1.9.0"},
		{"gotestsum", "gotest.tools/gotestsum", "gotest.tools/gotestsum", "v1.13.0"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := ParseToolsMod([]byte("module m\n\ntool example.com/x/cmd/y\n")); err == nil {
		t.Error("a tool with no require line must be an error")
	}
}

// moduleRun fakes go build and go version -m for the module tools: version[name]
// is what the built binary reports for its module.
func moduleRun(goVersion string, reports map[string]string) Runner {
	return func(dir string, argv ...string) (string, error) {
		cmd := strings.Join(argv, " ")
		switch {
		case cmd == "go env GOVERSION":
			return goVersion + "\n", nil
		case argv[1] == "build":
			return "", os.WriteFile(argv[len(argv)-2], []byte("built"), 0o755) // -o <file> <pkg>
		case argv[1] == "version":
			out := argv[len(argv)-1]
			return reports[filepath.Base(out)], nil
		}
		return "", errors.New("unexpected command: " + cmd)
	}
}

func buildInfo(module, version string) string {
	return "tool: go1.27.4\n\tpath\texample.com/cmd\n\tmod\texample.com/demo/tools\t(devel)\n\tdep\t" + module + "\t" + version + "\th1:xyz=\n"
}

func writeTools(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools", "go.mod"), []byte(toolsMod), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyModuleToolsReadGoVersionM(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "x"), false)
	place(t, dir, "tool", "x")
	writeTools(t, dir)
	reports := map[string]string{
		"go-licenses": buildInfo("github.com/google/go-licenses/v2", "v2.0.1"),
		"govulncheck": buildInfo("golang.org/x/vuln", "v1.9.0"),
		"gotestsum":   buildInfo("gotest.tools/gotestsum", "v1.13.0"),
	}
	fs := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: moduleRun("go1.27.4", reports), LookPath: noPath})
	if code := ExitCode(fs); code != 0 {
		t.Fatalf("exit %d: %+v", code, fs)
	}
	reports["govulncheck"] = buildInfo("golang.org/x/vuln", "v1.8.0")
	fs = Verify(VerifyOptions{Dir: dir, Platform: platform, Run: moduleRun("go1.27.4", reports), LookPath: noPath})
	f, _ := find(fs, "govulncheck")
	if f.Status != StatusMismatch || ExitCode(fs) != 2 || !strings.Contains(f.Detail, "v1.8.0") || !strings.Contains(f.Detail, "v1.9.0") {
		t.Fatalf("exit %d, finding %+v", ExitCode(fs), f)
	}
}

// A stale copy of a module tool in .go-tops/bin is read the same way.
func TestVerifyStaleModuleToolBinaryInGoTopsBin(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "x"), false)
	place(t, dir, "tool", "x")
	writeTools(t, dir)
	place(t, dir, "govulncheck", "stale")
	reports := map[string]string{
		"go-licenses": buildInfo("github.com/google/go-licenses/v2", "v2.0.1"),
		"gotestsum":   buildInfo("gotest.tools/gotestsum", "v1.13.0"),
	}
	run := func(d string, argv ...string) (string, error) {
		if argv[1] == "version" && strings.Contains(argv[len(argv)-1], ".go-tops") {
			return buildInfo("golang.org/x/vuln", "v1.7.0"), nil
		}
		if argv[1] == "version" && filepath.Base(argv[len(argv)-1]) == "govulncheck" {
			return buildInfo("golang.org/x/vuln", "v1.9.0"), nil
		}
		return moduleRun("go1.27.4", reports)(d, argv...)
	}
	fs := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: run, LookPath: noPath})
	if f, ok := find(fs, "govulncheck (.go-tops/bin)"); !ok || f.Status != StatusMismatch || ExitCode(fs) != 2 {
		t.Fatalf("exit %d, findings %+v", ExitCode(fs), fs)
	}
}

func TestVerifyAToolThatCannotBeBuiltIsMissing(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "x"), false)
	place(t, dir, "tool", "x")
	writeTools(t, dir)
	run := func(_ string, argv ...string) (string, error) {
		if argv[1] == "env" {
			return "go1.27.4\n", nil
		}
		return "", errors.New("module not in cache")
	}
	fs := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: run, LookPath: noPath})
	if f, _ := find(fs, "govulncheck"); f.Status != StatusMissing || ExitCode(fs) != 1 {
		t.Fatalf("exit %d, finding %+v", ExitCode(fs), f)
	}
}

func TestPinsListToolchainBinariesAndModuleTools(t *testing.T) {
	dir := writeLock(t, "go1.27.4", "tool", sha(t, "x"), false)
	writeTools(t, dir)
	var got []string
	for _, p := range Pins(dir) {
		got = append(got, p.Name+" "+p.Version+" "+p.Source)
	}
	want := []string{
		"toolchain go1.27.4 go-tops.lock", "tool 1.2.3 go-tops.lock",
		"go-licenses v2.0.1 tools/go.mod", "govulncheck v1.9.0 tools/go.mod", "gotestsum v1.13.0 tools/go.mod",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("pins %v, want %v", got, want)
	}
	if p := Pins(t.TempDir()); len(p) != 0 {
		t.Errorf("a directory with no pins lists none, got %v", p)
	}
}

// A tool whose build fails because go.sum disagrees is a custody mismatch (exit
// 2); one that fails on the network or the compiler is a missing tool (exit 1).
func TestVerifyBuildFailureKinds(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   Status
	}{
		{"checksum-mismatch", "verifying module: checksum mismatch\n\tdownloaded: h1:aaa\n\tgo.sum:     h1:bbb\n\nSECURITY ERROR", StatusMismatch},
		{"missing-go-sum-entry", "missing go.sum entry for module providing package gotest.tools/gotestsum", StatusMismatch},
		{"updates-needed", "go: updates to go.mod needed; to update it:\n\tgo mod tidy", StatusMismatch},
		{"network", "dial tcp: lookup proxy.golang.org: no such host", StatusMissing},
		{"compile", "internal/x.go:4:2: undefined: y", StatusMissing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := writeLock(t, "go1.27.4", "tool", sha(t, "x"), false)
			place(t, dir, "tool", "x")
			writeTools(t, dir)
			run := func(_ string, argv ...string) (string, error) {
				if argv[1] == "env" {
					return "go1.27.4\n", nil
				}
				return "", errors.New(strings.Join(argv, " ") + ": exit status 1: " + c.stderr)
			}
			fs := Verify(VerifyOptions{Dir: dir, Platform: platform, Run: run, LookPath: noPath})
			f, _ := find(fs, "govulncheck")
			wantCode := 1
			if c.want == StatusMismatch {
				wantCode = 2
			}
			if f.Status != c.want || ExitCode(fs) != wantCode {
				t.Fatalf("exit %d, finding %+v, want status %d and exit %d", ExitCode(fs), f, c.want, wantCode)
			}
		})
	}
}
