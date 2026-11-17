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

// ErrNotVerifiable is wrapped by VerifyBinary for a binary whose lock entry
// has an extract path. The lock then holds the SHA-256 of the downloaded
// archive, and the installed file is the executable inside it, so the two can
// never be compared.
var ErrNotVerifiable = errors.New("the lock holds the hash of the archive, not of the installed executable")

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

// VerifyBinary checks the file at path against the lock's SHA-256 for name on
// platform. It is the one check the gate (before it runs a downloaded binary,
// GO_TOPS.md §10, §12.2) and `gotops lock --verify` share. A binary the lock
// does not list, a platform it has no hash for and a hash that differs are
// all errors.
func (l *Lock) VerifyBinary(name, path, platform string) error {
	b, ok := l.Binary(name)
	if !ok {
		return fmt.Errorf("%s is not pinned in %s", name, FileName)
	}
	want, ok := b.SHA256[platform]
	if !ok {
		return fmt.Errorf("%s has no SHA-256 for %s in %s", name, platform, FileName)
	}
	if b.Extract != "" {
		return fmt.Errorf("%s (%s): %w", name, path, ErrNotVerifiable)
	}
	got, err := FileSHA256(path)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if got != want {
		return fmt.Errorf("%s at %s has SHA-256 %s, the lock says %s for %s", name, path, got, want, platform)
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
