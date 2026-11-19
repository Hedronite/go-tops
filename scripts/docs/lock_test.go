package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The lock schema and its example are the protocol's own files, two levels up.
// Each plant edits the example once and the schema must refuse the result.
const lockExample = "../../config/go-tops.example.lock"

func validateLock(t *testing.T, data string) error {
	t.Helper()
	r := &repo{Root: filepath.Join("..", "..")}
	sch, err := r.schema("schema/lock.schema.json", map[string]*jsonschema.Schema{})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := decodeInstance("go-tops.lock", []byte(data))
	if err != nil {
		return err
	}
	return sch.Validate(inst)
}

func TestLockExampleValidates(t *testing.T) {
	data, err := os.ReadFile(lockExample)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateLock(t, string(data)); err != nil {
		t.Fatalf("the example must validate: %v", err)
	}
}

func TestLockSchemaRefusesPlantedDefects(t *testing.T) {
	raw, err := os.ReadFile(lockExample)
	if err != nil {
		t.Fatal(err)
	}
	ex := string(raw)
	const sha = "d1e409481a8ca846c4bc97e781eb5cb1bd5c23353a0b3977a1cbda1b1b6447d5"
	cases := []struct{ name, old, new string }{
		{"unknown-top-level-key", "toolchain: go1.27.4", "toolchain: go1.27.4\nbogus_key: 1"},
		{"unknown-binary-key", "    version:", "    bogus_key: 1\n    version:"},
		{"short-sha", sha, sha[:40]},
		{"uppercase-sha", sha, strings.ToUpper(sha)},
		{"missing-platform", "      linux/arm64: b4d8f17068b35ee6060cc2f69a16fb48740d706fbba81661c318649c114a6998\n", ""},
		{"unknown-platform", "      linux/arm64:", "      windows/amd64: " + sha + "\n      linux/arm64:"},
		{"no-sha256", "    sha256:", "    sha256x:"},
		{"toolchain-not-exact", "go1.27.4", "go1.27"},
		{"toolchain-range", "go1.27.4", ">=go1.27.4"},
		{"toolchain-missing", "toolchain: go1.27.4             # go env GOVERSION must equal this\n", ""},
		{"version-has-v", `version: "2.15.0"`, `version: "v2.15.0"`},
		{"version-not-exact", `version: "2.15.0"`, `version: "2.15"`},
		{"http-url", "url: https://", "url: http://"},
		{"unknown-placeholder", "{os}-{arch}.tar.gz", "{os}-{cpu}.tar.gz"},
		{"trust-mode", "mode: checksum", "mode: trust-me"},
		{"trust-note-empty", "note: archive SHA-256 values copied from the upstream release checksum file; executable SHA-256 values computed from those verified archives", `note: ""`},
		{"trust-missing", "trust:\n  mode: checksum\n  note: archive SHA-256 values copied from the upstream release checksum file; executable SHA-256 values computed from those verified archives\n", ""},
		{"protocol-const", `protocol: "0.4.4"`, `protocol: "0.4.1"`},
		{"trust-note-blank", "note: archive SHA-256 values copied from the upstream release checksum file; executable SHA-256 values computed from those verified archives", `note: "   "`},
		{"executable-hash-without-extract", "    extract: golangci-lint-{version}-{os}-{arch}/golangci-lint\n", ""},
		{"short-executable-hash", "darwin/arm64: 71b89744ef6dd92dad9026b70489d1ad660da79ef90dfb5aabe995d916bec8a9", "darwin/arm64: 71b89744ef6d"},
		{"executable-hash-missing-platform", "      linux/arm64: 14680d7674eb1fed0405446670e15dcffb74e979ef0dcd41f998c088fd6babfb\n", ""},
		{"executable-hash-unknown-platform", "      linux/arm64: 14680d7674eb1fed0405446670e15dcffb74e979ef0dcd41f998c088fd6babfb", "      windows/amd64: 14680d7674eb1fed0405446670e15dcffb74e979ef0dcd41f998c088fd6babfb\n      linux/arm64: 14680d7674eb1fed0405446670e15dcffb74e979ef0dcd41f998c088fd6babfb"},
		{"binaries-not-a-list", "binaries:\n  - name:", "binaries:\n    name:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if strings.Count(ex, c.old) < 1 {
				t.Fatalf("the example lacks %q", c.old)
			}
			if err := validateLock(t, strings.Replace(ex, c.old, c.new, 1)); err == nil {
				t.Fatal("the planted lock validated")
			}
		})
	}
}

// An entry with an extract path pins an archive, so it must pin the executable
// too: the gate and lock --verify compare the installed executable with it.
func TestLockExtractNeedsTheExecutableHash(t *testing.T) {
	raw, err := os.ReadFile(lockExample)
	if err != nil {
		t.Fatal(err)
	}
	ex := string(raw)
	cut := strings.Index(ex, "    executable_sha256:")
	if cut < 0 {
		t.Fatal("the example has no executable_sha256")
	}
	if err := validateLock(t, ex[:cut]); err == nil {
		t.Error("an extract entry with no executable_sha256 validated")
	}
	for _, p := range []string{"darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64"} {
		i := strings.Index(ex[cut:], "      "+p+": ")
		if i < 0 {
			t.Fatalf("no %s hash after executable_sha256", p)
		}
		start := cut + i
		end := start + strings.Index(ex[start:], "\n") + 1
		if err := validateLock(t, ex[:start]+ex[end:]); err == nil {
			t.Errorf("an executable_sha256 without %s validated", p)
		}
	}
}

// A lock with no binary pins nothing: the whole list, absent or empty, fails.
func TestLockWithNoBinariesFails(t *testing.T) {
	head := "protocol: \"0.4.3\"\ntoolchain: go1.27.4\ntrust:\n  mode: tofu\n  note: none yet\n"
	for name, doc := range map[string]string{"empty-list": head + "binaries: []\n", "absent": head} {
		if err := validateLock(t, doc); err == nil {
			t.Errorf("%s validated", name)
		}
	}
}

func TestLockSchemaAcceptsTofuAndBareDownloads(t *testing.T) {
	raw, err := os.ReadFile(lockExample)
	if err != nil {
		t.Fatal(err)
	}
	ex := string(raw)
	tofu := strings.Replace(ex, "mode: checksum", "mode: tofu", 1)
	tofu = strings.Replace(tofu, "note: archive SHA-256 values copied from the upstream release checksum file; executable SHA-256 values computed from those verified archives", "note: the release is unsigned; first download recorded", 1)
	bare := strings.Replace(tofu, "    extract: golangci-lint-{version}-{os}-{arch}/golangci-lint\n", "", 1)
	bare = bare[:strings.Index(bare, "    executable_sha256:")]
	for name, doc := range map[string]string{"tofu": tofu, "no-extract": bare} {
		if err := validateLock(t, doc); err != nil {
			t.Errorf("%s must validate: %v", name, err)
		}
	}
}
