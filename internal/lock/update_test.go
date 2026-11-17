package lock

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The oracle is GO_TOPS.md §10: lock --update is a reviewed change to the
// pins, never an automatic one. An update re-hashes each pinned download for
// every platform, and for an archive also the executable inside it. A changed
// hash means the trust mode is tofu, because the new values were not compared
// with an upstream checksum file. The file is valid before it replaces the old
// one, and it is replaced whole or not at all.

const exampleLock = "../../config/go-tops.example.lock"

func copyExample(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(exampleLock)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// tarGz builds a gzipped tar archive holding one file.
func tarGz(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(tw.Close(), gz.Close()); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// archiveFetcher answers each url with an archive that holds the golangci-lint
// executable at the example's extract path, its content derived from the url so
// every platform differs, and records the urls it was asked for.
func archiveFetcher(t *testing.T, urls *[]string) Fetcher {
	t.Helper()
	return func(url string) ([]byte, error) {
		*urls = append(*urls, url)
		// .../v2.15.0/golangci-lint-2.15.0-darwin-arm64.tar.gz -> the directory inside
		base := url[strings.LastIndex(url, "/")+1:]
		dir := strings.TrimSuffix(base, ".tar.gz")
		return tarGz(t, dir+"/golangci-lint", "executable of "+url), nil
	}
}

func readLock(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdateRehashesArchiveAndExecutableForEveryPlatform(t *testing.T) {
	dir := copyExample(t)
	var urls []string
	changes, err := Update(UpdateOptions{Dir: dir, Fetch: archiveFetcher(t, &urls)})
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 4 || !strings.Contains(urls[0], "/v2.15.0/golangci-lint-2.15.0-darwin-arm64.tar.gz") {
		t.Fatalf("fetched %v: want the four platforms at the pinned version", urls)
	}
	kinds := map[string]int{}
	for _, c := range changes {
		kinds[c.Kind]++
	}
	if kinds["archive"] != 4 || kinds["executable"] != 4 {
		t.Fatalf("changes by kind %v, want 4 archive and 4 executable", kinds)
	}
	l, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("the updated lock must load: %v", err)
	}
	if l.Trust.Mode != "tofu" || !strings.Contains(l.Trust.Note, "lock --update") {
		t.Errorf("trust %+v: changed hashes make the lock tofu", l.Trust)
	}
	for i, p := range []string{"darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64"} {
		archive := archiveBytes(t, urls[i])
		if got, want := l.Binaries[0].SHA256[p], sha256hex(archive); got != want {
			t.Errorf("%s archive hash %s, want %s", p, got, want)
		}
		if got, want := l.Binaries[0].ExecutableSHA256[p], sha256hex([]byte("executable of "+urls[i])); got != want {
			t.Errorf("%s executable hash %s, want %s", p, got, want)
		}
	}
}

func archiveBytes(t *testing.T, url string) []byte {
	t.Helper()
	var ignored []string
	b, err := archiveFetcher(t, &ignored)(url)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUpdateKeepsCommentsAndOtherFields(t *testing.T) {
	dir := copyExample(t)
	before := readLock(t, dir)
	var urls []string
	if _, err := Update(UpdateOptions{Dir: dir, Fetch: archiveFetcher(t, &urls)}); err != nil {
		t.Fatal(err)
	}
	after := readLock(t, dir)
	for _, keep := range []string{"# must match the schema protocol constant", "# go env GOVERSION must equal this", "extract: golangci-lint-{version}-{os}-{arch}/golangci-lint", "toolchain: go1.27.4"} {
		if !strings.Contains(before, keep) || !strings.Contains(after, keep) {
			t.Errorf("an update must keep %q", keep)
		}
	}
}

// Downloads that reproduce the pinned hashes leave the file alone.
func TestUpdateWithUnchangedHashesLeavesTheLockAlone(t *testing.T) {
	dir := copyExample(t)
	var urls []string
	fetch := archiveFetcher(t, &urls)
	if _, err := Update(UpdateOptions{Dir: dir, Fetch: fetch}); err != nil {
		t.Fatal(err)
	}
	before := readLock(t, dir)
	changes, err := Update(UpdateOptions{Dir: dir, Fetch: fetch})
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes %v, err %v", changes, err)
	}
	if readLock(t, dir) != before {
		t.Error("an update that changed nothing rewrote the file")
	}
}

func TestUpdateSetRaisesAVersionBeforeHashing(t *testing.T) {
	dir := copyExample(t)
	var urls []string
	if _, err := Update(UpdateOptions{Dir: dir, Fetch: archiveFetcher(t, &urls), Set: map[string]string{"golangci-lint": "2.16.0", ToolchainName: "go1.27.5"}}); err != nil {
		t.Fatal(err)
	}
	for _, u := range urls {
		if !strings.Contains(u, "/v2.16.0/golangci-lint-2.16.0-") {
			t.Errorf("fetched %s: want the raised version", u)
		}
	}
	l, err := LoadDir(dir)
	if err != nil || l.Binaries[0].Version != "2.16.0" || l.Toolchain != "go1.27.5" {
		t.Fatalf("version %q toolchain %q: %v", l.Binaries[0].Version, l.Toolchain, err)
	}
}

// A refused update leaves the file byte for byte as it was.
func TestUpdateFailuresLeaveTheFileUntouched(t *testing.T) {
	cases := map[string]func(t *testing.T) UpdateOptions{
		"unknown-name": func(*testing.T) UpdateOptions { return UpdateOptions{Set: map[string]string{"no-such-tool": "1.0.0"}} },
		"fetch-fails": func(*testing.T) UpdateOptions {
			return UpdateOptions{Fetch: func(string) ([]byte, error) { return nil, errors.New("HTTP 404") }}
		},
		"invalid-new-pin": func(*testing.T) UpdateOptions { return UpdateOptions{Set: map[string]string{ToolchainName: "go1.27"}} },
		"version-with-v": func(*testing.T) UpdateOptions {
			return UpdateOptions{Set: map[string]string{"golangci-lint": "v2.16.0"}}
		},
		"not-an-archive": func(*testing.T) UpdateOptions {
			return UpdateOptions{Fetch: func(string) ([]byte, error) { return []byte("plain bytes"), nil }}
		},
		"entry-missing": func(t *testing.T) UpdateOptions {
			return UpdateOptions{Fetch: func(string) ([]byte, error) { return tarGz(t, "elsewhere/other", "x"), nil }}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			dir := copyExample(t)
			before := readLock(t, dir)
			o := mk(t)
			o.Dir = dir
			if o.Fetch == nil {
				var urls []string
				o.Fetch = archiveFetcher(t, &urls)
			}
			if _, err := Update(o); err == nil {
				t.Fatal("want an error")
			}
			if readLock(t, dir) != before {
				t.Error("a failed update changed the file")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Errorf("a failed update left files behind: %v %v", entries, err)
			}
		})
	}
}

func TestUpdateRefusesALockThatDoesNotLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("protocol: \"0.4.2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(UpdateOptions{Dir: dir, Fetch: func(string) ([]byte, error) { return nil, nil }}); err == nil {
		t.Fatal("want an error: an invalid lock is not updated")
	}
}

func TestExtractFileIgnoresALeadingDotSlashAndRefusesNonFiles(t *testing.T) {
	data, err := extractFile(tarGz(t, "./dir/tool", "hello"), "dir/tool")
	if err != nil || string(data) != "hello" {
		t.Fatalf("%q %v", data, err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "dir/tool", Typeflag: tar.TypeSymlink, Linkname: "elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(tw.Close(), gz.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := extractFile(buf.Bytes(), "dir/tool"); err == nil {
		t.Error("a symlink entry is not an executable")
	}
}

// An update with no changed hash and nothing asked for leaves the file alone,
// byte for byte, whatever its formatting (SPEC-m1 B19).
func TestUpdateWithNothingToChangeDoesNotRewriteTheFile(t *testing.T) {
	dir := copyExample(t)
	var urls []string
	fetch := archiveFetcher(t, &urls)
	if _, err := Update(UpdateOptions{Dir: dir, Fetch: fetch}); err != nil {
		t.Fatal(err)
	}
	// a hand-formatted file: an extra blank line and trailing spaces that the YAML encoder would drop
	mangled := strings.Replace(readLock(t, dir), "\nbinaries:", "\n\n\nbinaries:   ", 1)
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(mangled), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := Update(UpdateOptions{Dir: dir, Fetch: fetch})
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes %v, err %v", changes, err)
	}
	if readLock(t, dir) != mangled {
		t.Error("a no-op update rewrote the file")
	}
	// asking for a raise is an explicit change: the file is written
	if _, err := Update(UpdateOptions{Dir: dir, Fetch: fetch, Set: map[string]string{"golangci-lint": "2.15.1"}}); err != nil {
		t.Fatal(err)
	}
	if readLock(t, dir) == mangled {
		t.Error("an explicit raise left the file as it was")
	}
}

// A download or a write that fails is an environment error (exit 3); an invalid
// lock, a missing one or a pin that is not there is not (exit 2).
func TestUpdateFailureKinds(t *testing.T) {
	isEnv := func(err error) bool {
		var e *EnvError
		return errors.As(err, &e)
	}
	var urls []string
	dir := copyExample(t)
	_, err := Update(UpdateOptions{Dir: dir, Fetch: func(string) ([]byte, error) { return nil, errors.New("connection refused") }})
	if !isEnv(err) {
		t.Errorf("a failed download is an environment error: %v", err)
	}
	_, err = Update(UpdateOptions{Dir: copyExample(t), Fetch: func(string) ([]byte, error) { return []byte("not an archive"), nil }})
	if !isEnv(err) {
		t.Errorf("a download that is not the expected archive is an environment error: %v", err)
	}
	_, err = Update(UpdateOptions{Dir: copyExample(t), Fetch: archiveFetcher(t, &urls), Set: map[string]string{"no-such-tool": "1.0.0"}})
	if err == nil || isEnv(err) {
		t.Errorf("an unknown pin is a configuration error: %v", err)
	}
	_, err = Update(UpdateOptions{Dir: t.TempDir(), Fetch: archiveFetcher(t, &urls)})
	if err == nil || isEnv(err) {
		t.Errorf("a missing lock is a configuration error: %v", err)
	}
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, FileName), []byte("protocol: \"0.4.2\"\nbogus_key: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Update(UpdateOptions{Dir: bad, Fetch: archiveFetcher(t, &urls)})
	if err == nil || isEnv(err) {
		t.Errorf("an invalid lock is a configuration error: %v", err)
	}
}

func TestUpdateWriteFailureIsAnEnvironmentError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory does not stop root")
	}
	dir := copyExample(t)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Error(err)
		}
	})
	var urls []string
	_, err := Update(UpdateOptions{Dir: dir, Fetch: archiveFetcher(t, &urls)})
	var e *EnvError
	if !errors.As(err, &e) {
		t.Fatalf("a file that cannot be written is an environment error: %v", err)
	}
}
