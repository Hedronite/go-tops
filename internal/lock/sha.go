package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
)

// ErrNotVerifiable is wrapped by VerifyBinary for an entry that has an extract
// path but no executable_sha256 for the platform: the lock then holds only the
// hash of the archive, and the installed file is the executable inside it. The
// schema requires executable_sha256 with extract, so a lock that loads does not
// reach this.
var ErrNotVerifiable = errors.New("the lock holds the hash of the archive, not of the installed executable")

// ErrBinaryNotPinned means the lock has no entry for the requested binary name.
var ErrBinaryNotPinned = errors.New("not pinned in lock file")

// ErrPlatformHashMissing means the lock has no hash for the requested platform.
var ErrPlatformHashMissing = errors.New("platform hash missing in lock file")

// SHA256MismatchError is returned when an installed file's hash differs from the lock.
type SHA256MismatchError struct {
	Name, Path, Got, Want, Field, Platform string
}

func (e *SHA256MismatchError) Error() string {
	return fmt.Sprintf("%s at %s has SHA-256 %s, the lock's %s says %s for %s", e.Name, e.Path, e.Got, e.Field, e.Want, e.Platform)
}

// HostPlatform is the lock's platform key for this host, such as "darwin/arm64".
func HostPlatform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// FileSHA256 is the hex SHA-256 of the file at path.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, f)
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Binary returns the lock entry for name.
func (l *Lock) Binary(name string) (*Binary, bool) {
	for i := range l.Binaries {
		if l.Binaries[i].Name == name {
			return &l.Binaries[i], true
		}
	}
	return nil, false
}

// VerifyBinary checks the installed file at path against the lock for name on
// platform: against executable_sha256 when the entry has an extract path (the
// file is the executable inside the archive), and against sha256 otherwise (the
// download is the executable). It is the one check the gate (before it runs a
// downloaded binary, GO_TOPS.md §10, §12.2) and `gotops lock --verify` share. A
// binary the lock does not list, a platform it has no hash for and a hash that
// differs are all errors.
func (l *Lock) VerifyBinary(name, path, platform string) error {
	b, ok := l.Binary(name)
	if !ok {
		return fmt.Errorf("%s %w (%s)", name, ErrBinaryNotPinned, FileName)
	}
	field, hashes := "sha256", b.SHA256
	if b.Extract != "" {
		field, hashes = "executable_sha256", b.ExecutableSHA256
	}
	want, ok := hashes[platform]
	if !ok {
		err := fmt.Errorf("%s %w (%s has no %s for %s)", name, ErrPlatformHashMissing, name, field, platform)
		if b.Extract != "" {
			err = fmt.Errorf("%w: %w", err, ErrNotVerifiable)
		}
		return err
	}
	got, err := FileSHA256(path)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if got != want {
		return &SHA256MismatchError{Name: name, Path: path, Got: got, Field: field, Want: want, Platform: platform}
	}
	return nil
}

// NewVerifier returns the gate's verify hook for the repository at dir: it
// loads go-tops.lock when first asked and checks each binary for the host
// platform. A missing or invalid lock fails every check, because a binary that
// cannot be checked is not run.
func NewVerifier(dir string) func(name, path string) error {
	var l *Lock
	var loadErr error
	loaded := false
	return func(name, path string) error {
		if !loaded {
			l, loadErr = LoadDir(dir)
			loaded = true
		}
		if loadErr != nil {
			return loadErr
		}
		return l.VerifyBinary(name, path, HostPlatform())
	}
}
