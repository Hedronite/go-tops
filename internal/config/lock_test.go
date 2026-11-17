package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GO_TOPS.md §12.1: check validates go-tops.lock against schema/lock.schema.json
// and reports a binary name that appears twice (SPEC-m1 B4).

const lockName = "go-tops.lock"

func sha(c string) string { return strings.Repeat(c, 64) }

func binaryDoc(name string) doc {
	return doc{
		"name":    name,
		"version": "2.15.0",
		"url":     "https://example.com/v{version}/" + name + "-{version}-{os}-{arch}.tar.gz",
		"sha256": doc{
			"darwin/arm64": sha("a"),
			"darwin/amd64": sha("b"),
			"linux/amd64":  sha("c"),
			"linux/arm64":  sha("d"),
		},
	}
}

func sub(t *testing.T, m doc, key string) doc {
	t.Helper()
	v, ok := m[key].(doc)
	if !ok {
		t.Fatalf("%s is not a map", key)
	}
	return v
}

func lockDoc(t *testing.T) doc {
	t.Helper()
	return doc{
		"protocol":  protocolID(t),
		"toolchain": "go1.27.4",
		"trust":     doc{"mode": "checksum", "note": "copied from the upstream checksum file"},
		"binaries":  []any{binaryDoc("golangci-lint")},
	}
}

func lockModule(t *testing.T, lock doc) string {
	t.Helper()
	return writeModule(t, baseDoc(t), map[string]string{lockName: string(mustYAML(t, lock))})
}

func TestPlantedLocksFail(t *testing.T) {
	lockWant := func(rule, pointer string) want { return want{file: lockName, rule: rule, pointer: ptr(pointer)} }
	tests := []struct {
		name string
		mut  func(doc)
		want want
	}{
		{"unknown key", func(d doc) { d["extra"] = 1 }, lockWant("schema", "/extra")},
		{"wrong protocol", func(d doc) { d["protocol"] = "0.0.1" }, lockWant("schema", "/protocol")},
		{"toolchain is a language version", func(d doc) { d["toolchain"] = "go1.27" }, lockWant("schema", "/toolchain")},
		{"toolchain missing", func(d doc) { delete(d, "toolchain") }, lockWant("schema", "")},
		{"trust mode outside the enum", func(d doc) { d["trust"] = doc{"mode": "faith", "note": "x"} }, lockWant("schema", "/trust/mode")},
		{"trust note blank", func(d doc) { d["trust"] = doc{"mode": "tofu", "note": "   "} }, lockWant("schema", "/trust/note")},
		{"no binaries", func(d doc) { d["binaries"] = []any{} }, lockWant("schema", "/binaries")},
		{"short sha", func(d doc) {
			b := binaryDoc("golangci-lint")
			sub(t, b, "sha256")["darwin/arm64"] = "abc"
			d["binaries"] = []any{b}
		}, lockWant("schema", "/binaries/0/sha256/darwin~1arm64")},
		{"missing platform", func(d doc) {
			b := binaryDoc("golangci-lint")
			delete(sub(t, b, "sha256"), "linux/arm64")
			d["binaries"] = []any{b}
		}, lockWant("schema", "/binaries/0/sha256")},
		{"unknown platform", func(d doc) {
			b := binaryDoc("golangci-lint")
			sub(t, b, "sha256")["windows/amd64"] = sha("e")
			d["binaries"] = []any{b}
		}, lockWant("schema", "/binaries/0/sha256/windows~1amd64")},
		{"upper-case sha", func(d doc) {
			b := binaryDoc("golangci-lint")
			sub(t, b, "sha256")["linux/amd64"] = strings.ToUpper(sha("c"))
			d["binaries"] = []any{b}
		}, lockWant("schema", "/binaries/0/sha256/linux~1amd64")},
		{"version with a leading v", func(d doc) {
			b := binaryDoc("golangci-lint")
			b["version"] = "v2.15.0"
			d["binaries"] = []any{b}
		}, lockWant("schema", "/binaries/0/version")},
		{"url is not https", func(d doc) {
			b := binaryDoc("golangci-lint")
			b["url"] = "http://example.com/x"
			d["binaries"] = []any{b}
		}, lockWant("schema", "/binaries/0/url")},
		{"url with an unknown placeholder", func(d doc) {
			b := binaryDoc("golangci-lint")
			b["url"] = "https://example.com/{tag}/x"
			d["binaries"] = []any{b}
		}, lockWant("schema", "/binaries/0/url")},
		{"duplicate binary name", func(d doc) {
			d["binaries"] = []any{binaryDoc("golangci-lint"), binaryDoc("other"), binaryDoc("golangci-lint")}
		}, want{file: lockName, rule: "lock-duplicate", pointer: ptr("/binaries/2/name"), contains: "/binaries/0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := lockDoc(t)
			tt.mut(lock)
			expect(t, check(t, lockModule(t, lock)).Issues, tt.want)
		})
	}
}

func TestDuplicateNameIsReportedAlongsideOtherFaults(t *testing.T) {
	lock := lockDoc(t)
	bad := binaryDoc("golangci-lint")
	bad["version"] = "nope"
	lock["binaries"] = []any{binaryDoc("golangci-lint"), bad}
	issues := check(t, lockModule(t, lock)).Issues
	expect(t, issues, want{file: lockName, rule: "lock-duplicate", pointer: ptr("/binaries/1/name")})
	expect(t, issues, want{file: lockName, rule: "schema", pointer: ptr("/binaries/1/version")})
}

func TestLockFileEmptyIsAnIssue(t *testing.T) {
	dir := writeModule(t, baseDoc(t), map[string]string{lockName: ""})
	expect(t, check(t, dir).Issues, want{file: lockName, rule: "yaml", contains: "empty"})
}

func TestLocksThatPass(t *testing.T) {
	tests := []struct {
		name string
		mut  func(doc)
	}{
		{"one binary", func(doc) {}},
		{"tofu", func(d doc) { d["trust"] = doc{"mode": "tofu", "note": "the upstream release is mutable"} }},
		{"two names", func(d doc) { d["binaries"] = []any{binaryDoc("golangci-lint"), binaryDoc("go-licenses")} }},
		{"extract template", func(d doc) {
			b := binaryDoc("golangci-lint")
			b["extract"] = "golangci-lint-{version}-{os}-{arch}/golangci-lint"
			b["executable_sha256"] = doc{
				"darwin/arm64": sha("e"),
				"darwin/amd64": sha("f"),
				"linux/amd64":  sha("1"),
				"linux/arm64":  sha("2"),
			}
			d["binaries"] = []any{b}
		}},
		{"pre-release version", func(d doc) {
			b := binaryDoc("golangci-lint")
			b["version"] = "2.16.0-rc.1"
			d["binaries"] = []any{b}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := lockDoc(t)
			tt.mut(lock)
			requirePass(t, lockModule(t, lock))
		})
	}
}

// The toolchain pin is the release in the lock (§2): no lock, no gate.
func TestMissingLockIsAnIssue(t *testing.T) {
	for _, enforce := range []string{"hard", "ratchet", "advisory"} {
		t.Run(enforce, func(t *testing.T) {
			d := baseDoc(t)
			d["enforce"] = enforce
			if enforce == "advisory" {
				d["advisory"] = doc{"reason": "the laws layer lands at M5", "owner": "x", "expires": "2027-06-01"}
			}
			dir := writeModule(t, d, nil)
			if err := os.Remove(filepath.Join(dir, lockName)); err != nil {
				t.Fatal(err)
			}
			expect(t, check(t, dir).Issues, want{file: lockName, rule: "lock-missing", pointer: ptr(""), contains: "no gate"})
		})
	}
}

func TestAPresentLockIsNotMissing(t *testing.T) {
	requirePass(t, writeModule(t, baseDoc(t), nil))
}

func TestLockIsCheckedEvenWhenTheConfigIsInvalid(t *testing.T) {
	d := baseDoc(t)
	d["bogus"] = 1
	lock := lockDoc(t)
	lock["extra"] = 1
	dir := writeModule(t, d, map[string]string{lockName: string(mustYAML(t, lock))})
	issues := check(t, dir).Issues
	expect(t, issues, want{rule: "schema", pointer: ptr("/bogus")})
	expect(t, issues, want{file: lockName, rule: "schema", pointer: ptr("/extra")})
}

// The lock the repo ships as the §10 example is a lock check accepts.
func TestTheShippedExampleLockPasses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "go-tops.example.lock"))
	if err != nil {
		t.Fatal(err)
	}
	requirePass(t, writeModule(t, baseDoc(t), map[string]string{lockName: string(data)}))
}
