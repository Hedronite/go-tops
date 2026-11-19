package lock

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodLockTemplate = `protocol: "@PROTOCOL@"
toolchain: go1.27.4
trust:
  mode: checksum
  note: copied from the upstream release checksum file
binaries:
  - name: golangci-lint
    version: "2.15.0"
    url: https://example.com/download/v{version}/golangci-lint-{version}-{os}-{arch}.tar.gz
    extract: golangci-lint-{version}-{os}-{arch}/golangci-lint
    sha256:
      darwin/arm64: d1e409481a8ca846c4bc97e781eb5cb1bd5c23353a0b3977a1cbda1b1b6447d5
      darwin/amd64: 767b899a28285e88ded2823d5dcc4bc5ef681173eb2d80f029a7b17ee4ece3b8
      linux/amd64: 9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea
      linux/arm64: b4d8f17068b35ee6060cc2f69a16fb48740d706fbba81661c318649c114a6998
    executable_sha256:
      darwin/arm64: 71b89744ef6dd92dad9026b70489d1ad660da79ef90dfb5aabe995d916bec8a9
      darwin/amd64: e04962742507a5f37a4002a7a8b2b549f6864f17807eb5d534d947f6311a051f
      linux/amd64: d9af4564c05beb2f5e130119aec45e5e89d28e5017d558d3ca382659f2427894
      linux/arm64: 14680d7674eb1fed0405446670e15dcffb74e979ef0dcd41f998c088fd6babfb
`

var goodLock = strings.Replace(goodLockTemplate, "@PROTOCOL@", protocolID, 1)

// §10: the example lock of the protocol loads into the types the gate reads.
func TestLoadTheExampleLock(t *testing.T) {
	l, err := Load(filepath.Join("..", "..", "config", "go-tops.example.lock"))
	if err != nil {
		t.Fatalf("the protocol's own example lock does not load: %v", err)
	}
	if l.Protocol != protocolID || l.Toolchain != "go1.27.4" || l.Trust.Mode != "checksum" || l.Trust.Note == "" {
		t.Errorf("header %+v", l)
	}
	b, ok := l.Binary("golangci-lint")
	if !ok || b.Version != "2.15.0" || b.Extract == "" || len(b.SHA256) != 4 {
		t.Fatalf("golangci-lint entry %+v (found %v)", b, ok)
	}
	if _, ok := l.Binary("nope"); ok {
		t.Error("Binary found a name that is not pinned")
	}
}

func parseOK(t *testing.T, text string) *Lock {
	t.Helper()
	l, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return l
}

func parseBad(t *testing.T, text string) string {
	t.Helper()
	_, err := Parse([]byte(text))
	if err == nil {
		t.Fatalf("a bad lock loaded:\n%s", text)
	}
	var le *Error
	if !errors.As(err, &le) {
		t.Fatalf("err %T is not a *lock.Error", err)
	}
	return err.Error()
}

func TestGoodLockLoads(t *testing.T) {
	l := parseOK(t, goodLock)
	if l.Binaries[0].SHA256["linux/amd64"] != "9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea" {
		t.Errorf("sha %v", l.Binaries[0].SHA256)
	}
}

// Each break of lock.schema.json is refused, and the message carries the JSON
// pointer of what is wrong (RFC 6901: the "/" of a platform key is "~1").
func TestSchemaBreaksAreRefusedWithAPointer(t *testing.T) {
	cases := []struct {
		name, from, to, pointer string
	}{
		{"short sha", "9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea", "9b7fe6a6", "/binaries/0/sha256/linux~1amd64"},
		{"upper-case sha", "d1e409481a8ca846c4bc97e781eb5cb1bd5c23353a0b3977a1cbda1b1b6447d5", "D1E409481A8CA846C4BC97E781EB5CB1BD5C23353A0B3977A1CBDA1B1B6447D5", "/binaries/0/sha256/darwin~1arm64"},
		{"missing platform", "      linux/arm64: b4d8f17068b35ee6060cc2f69a16fb48740d706fbba81661c318649c114a6998\n", "", "/binaries/0/sha256"},
		{"toolchain not exact", "toolchain: go1.27.4", "toolchain: go1.27", "/toolchain"},
		{"toolchain with v", "toolchain: go1.27.4", "toolchain: v1.27.4", "/toolchain"},
		{"trust mode", "mode: checksum", "mode: guess", "/trust/mode"},
		{"blank trust note", "note: copied from the upstream release checksum file", "note: \" \"", "/trust/note"},
		{"version with v", `version: "2.15.0"`, `version: "v2.15.0"`, "/binaries/0/version"},
		{"http url", "url: https://example.com", "url: http://example.com", "/binaries/0/url"},
		{"unknown placeholder", "{os}-{arch}.tar.gz", "{platform}.tar.gz", "/binaries/0/url"},
		{"protocol", "protocol: \"" + protocolID + "\"", `protocol: "0.4.1"`, "/protocol"},
		{"short executable sha", "e04962742507a5f37a4002a7a8b2b549f6864f17807eb5d534d947f6311a051f", "e0496274", "/binaries/0/executable_sha256/darwin~1amd64"},
		{"executable sha with no extract", "    extract: golangci-lint-{version}-{os}-{arch}/golangci-lint\n", "", "/binaries/0"},
		{"upper-case name", "name: golangci-lint", "name: GolangciLint", "/binaries/0/name"},
	}
	for _, c := range cases {
		if !strings.Contains(goodLock, c.from) {
			t.Fatalf("%s: the anchor %q is not in the fixture", c.name, c.from)
		}
		msg := parseBad(t, strings.Replace(goodLock, c.from, c.to, 1))
		if !strings.Contains(msg, c.pointer) {
			t.Errorf("%s: message %q lacks the pointer %s", c.name, msg, c.pointer)
		}
	}
}

// A lock with no binary pins nothing (G1's minItems), and an empty file is no lock.
func TestNoBinariesAndEmptyFile(t *testing.T) {
	head, _, _ := strings.Cut(goodLock, "binaries:")
	if msg := parseBad(t, head+"binaries: []\n"); !strings.Contains(msg, "/binaries") {
		t.Errorf("empty binaries: %q", msg)
	}
	if msg := parseBad(t, head); !strings.Contains(msg, "/binaries") {
		t.Errorf("no binaries key: %q", msg)
	}
	parseBad(t, "")
	parseBad(t, "# nothing but a comment\n")
}

// An unknown key at any depth is refused: the schema is closed everywhere.
func TestUnknownKeyAtAnyDepth(t *testing.T) {
	for _, edit := range []struct{ from, to, key string }{
		{"toolchain: go1.27.4", "toolchain: go1.27.4\nextra: 1", "extra"},
		{"  mode: checksum", "  mode: checksum\n  who: me", "who"},
		{"    extract:", "    flavour: x\n    extract:", "flavour"},
		{"      linux/arm64:", "      windows/amd64: 0000000000000000000000000000000000000000000000000000000000000000\n      linux/arm64:", "windows/amd64"},
	} {
		msg := parseBad(t, strings.Replace(goodLock, edit.from, edit.to, 1))
		if !strings.Contains(msg, edit.key) {
			t.Errorf("unknown key %q: message %q does not name it", edit.key, msg)
		}
	}
}

// B4: the loader rejects a lock that pins one binary twice, and names both entries.
func TestDuplicateBinaryNamesAreRejected(t *testing.T) {
	_, rest, ok := strings.Cut(goodLock, "  - name:")
	if !ok {
		t.Fatal("no binary entry in the fixture")
	}
	entry := "  - name:" + rest
	msg := parseBad(t, goodLock+entry)
	if !strings.Contains(msg, "golangci-lint") || !strings.Contains(msg, "/binaries/1") || !strings.Contains(msg, "/binaries/0") {
		t.Errorf("message %q does not name the repeated entry and the first", msg)
	}
	// two different names are fine
	other := strings.Replace(entry, "name: golangci-lint", "name: other-tool", 1)
	l := parseOK(t, goodLock+other)
	if len(l.Binaries) != 2 {
		t.Errorf("%d binaries", len(l.Binaries))
	}
}

func TestMissingFileIsNotExist(t *testing.T) {
	_, err := LoadDir(t.TempDir())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestLoadDirReadsTheFileAtTheRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(goodLock), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err != nil {
		t.Fatal(err)
	}
}

// 0.4.2: an entry with an extract path carries executable_sha256 for every
// platform, and the loader exposes it.
func TestExecutableSHA256IsLoadedAndRequiredWithExtract(t *testing.T) {
	l := parseOK(t, goodLock)
	b := l.Binaries[0]
	if b.ExecutableSHA256["linux/amd64"] != "d9af4564c05beb2f5e130119aec45e5e89d28e5017d558d3ca382659f2427894" || len(b.ExecutableSHA256) != 4 {
		t.Fatalf("executable_sha256 %v", b.ExecutableSHA256)
	}
	i := strings.Index(goodLock, "    executable_sha256:")
	if i < 0 {
		t.Fatal("no executable_sha256 block in the fixture")
	}
	msg := parseBad(t, goodLock[:i])
	if !strings.Contains(msg, "/binaries/0") || !strings.Contains(msg, "executable_sha256") {
		t.Errorf("extract without executable_sha256: %q", msg)
	}
	// an entry with no extract carries no executable_sha256: the download is the executable
	plain := strings.Replace(goodLock[:i], "    extract: golangci-lint-{version}-{os}-{arch}/golangci-lint\n", "", 1)
	if l := parseOK(t, plain); l.Binaries[0].ExecutableSHA256 != nil {
		t.Errorf("executable_sha256 %v on an entry without extract", l.Binaries[0].ExecutableSHA256)
	}
}
