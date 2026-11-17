package lock

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// This file is `gotops lock --update` (GO_TOPS.md §10): a reviewed change to
// the pins, never an automatic one. The command refuses under CI=true before
// it gets here. An update downloads each binary for every platform and writes
// the SHA-256 of the download and, for an archive, of the executable inside it.
// When a hash changed, the trust mode becomes `tofu`: the new values were not
// compared with an upstream checksum file, so the lock must not claim they were.

// EnvError is an update that failed on the environment: a download that did not
// complete or hold the expected file, or a file that could not be written. An
// invalid lock, or a pin the update was asked to raise that does not exist, is
// not one (exit 2, not 3).
type EnvError struct{ Err error }

func (e *EnvError) Error() string { return e.Err.Error() }

// Unwrap exposes the cause.
func (e *EnvError) Unwrap() error { return e.Err }

// Fetcher returns the file at url.
type Fetcher func(url string) ([]byte, error)

// UpdateOptions are the inputs of Update.
type UpdateOptions struct {
	Dir string
	// Set raises a pin before it is re-hashed: "golangci-lint" -> "2.16.0", or
	// "toolchain" -> "go1.27.5".
	Set   map[string]string
	Fetch Fetcher
}

// Change is one hash an update rewrote. Kind is "archive" for sha256 (the
// download) and "executable" for executable_sha256.
type Change struct {
	Name, Platform, Kind, Old, New string
}

// ToolchainName is the key of Set that names the toolchain.
const ToolchainName = "toolchain"

const tofuNote = "SHA-256 values computed from the downloads by gotops lock --update; not compared with an upstream checksum file"

// Update rewrites go-tops.lock and returns the hashes that changed. The result
// is validated before it replaces the file, and the file is replaced whole or
// not at all.
func Update(o UpdateOptions) ([]Change, error) {
	file := filepath.Join(o.Dir, FileName)
	raw, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, &EnvError{err}
	}
	l, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := checkSetNames(l, o.Set); err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	doc := root.Content[0]
	if v, ok := o.Set[ToolchainName]; ok {
		setScalar(mapValue(doc, "toolchain"), v)
	}
	changes, err := rehash(doc, l, o)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 && len(o.Set) == 0 {
		return nil, nil // nothing changed and nothing was asked for: the file stays as it is
	}
	if len(changes) > 0 {
		trust := mapValue(doc, "trust")
		setScalar(mapValue(trust, "mode"), "tofu")
		setScalar(mapValue(trust, "note"), tofuNote)
	}
	out, err := encode(&root)
	if err != nil {
		return nil, err
	}
	if _, err := Parse(out); err != nil {
		return nil, fmt.Errorf("the updated lock does not validate, so it was not written: %w", err)
	}
	if err := writeWhole(file, out); err != nil {
		return nil, &EnvError{err}
	}
	return changes, nil
}

func encode(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func checkSetNames(l *Lock, set map[string]string) error {
	for name := range set {
		if name == ToolchainName {
			continue
		}
		if _, ok := l.Binary(name); !ok {
			return fmt.Errorf("%s is not pinned in %s, so it cannot be updated", name, FileName)
		}
	}
	return nil
}

// rehash raises each set version, then fetches every platform and records the
// hash of the download and, with an extract path, of the executable inside it.
func rehash(doc *yaml.Node, l *Lock, o UpdateOptions) ([]Change, error) {
	var changes []Change
	bins := mapValue(doc, "binaries")
	for i, b := range l.Binaries {
		node := bins.Content[i]
		if v, ok := o.Set[b.Name]; ok {
			b.Version = v
			setScalar(mapValue(node, "version"), v)
		}
		got, err := rehashBinary(node, b, o.Fetch)
		if err != nil {
			return nil, err
		}
		changes = append(changes, got...)
	}
	return changes, nil
}

func rehashBinary(node *yaml.Node, b Binary, fetch Fetcher) ([]Change, error) {
	var changes []Change
	sums, exes := mapValue(node, "sha256"), mapValue(node, "executable_sha256")
	for _, platform := range platformsOf(b) {
		osName, arch, _ := strings.Cut(platform, "/")
		url := expand(b.URL, b.Version, osName, arch)
		body, err := fetch(url)
		if err != nil {
			return nil, &EnvError{fmt.Errorf("%s %s: %w", b.Name, platform, err)}
		}
		changes = record(changes, b, platform, "archive", b.SHA256[platform], hexSHA256(body), mapValue(sums, platform))
		if b.Extract == "" {
			continue
		}
		exe, err := extractFile(body, expand(b.Extract, b.Version, osName, arch))
		if err != nil {
			return nil, &EnvError{fmt.Errorf("%s %s: %w", b.Name, platform, err)}
		}
		changes = record(changes, b, platform, "executable", b.ExecutableSHA256[platform], hexSHA256(exe), mapValue(exes, platform))
	}
	return changes, nil
}

func record(changes []Change, b Binary, platform, kind, old, sum string, n *yaml.Node) []Change {
	setScalar(n, sum)
	if old == sum {
		return changes
	}
	return append(changes, Change{b.Name, platform, kind, old, sum})
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// maxExecutable bounds how much of an archive entry is read into memory.
const maxExecutable = 512 << 20

// extractFile returns the entry named name from a gzipped tar archive. A
// leading "./" on an entry is ignored. It is an error for the entry to be
// absent or not a regular file.
func extractFile(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the download is not a gzipped tar archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("the archive has no entry %s", name)
		}
		if err != nil {
			return nil, err
		}
		if path.Clean(hdr.Name) != path.Clean(name) {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("the archive entry %s is not a regular file", name)
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxExecutable+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxExecutable {
			return nil, fmt.Errorf("the archive entry %s is larger than %d bytes", name, maxExecutable)
		}
		return data, nil
	}
}

// platformsOf lists a binary's platforms in the lock's own order.
func platformsOf(b Binary) []string {
	order := []string{"darwin/arm64", "darwin/amd64", "linux/amd64", "linux/arm64"}
	out := make([]string, 0, len(order))
	for _, p := range order {
		if _, ok := b.SHA256[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

func expand(tmpl, version, osName, arch string) string {
	return strings.NewReplacer("{version}", version, "{os}", osName, "{arch}", arch).Replace(tmpl)
}

// mapValue is the value node of key in a mapping node, or an empty node when
// the key is absent (Parse has already established that it is present).
func mapValue(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return &yaml.Node{}
}

func setScalar(n *yaml.Node, v string) { n.Value = v }

// writeWhole writes the file beside the target and renames it into place.
func writeWhole(file string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(file), ".go-tops-lock-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(name))
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, file)
}
