// Package ingest reads files named by untrusted input, confined to one directory.
//
// Go-POIRE conditions cured here: GO-10 path escape, GO-09 unbounded input,
// GO-08 ignored error (a Close error is returned, not dropped).
package ingest

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// MaxFile is the largest file [Read] returns, in bytes.
const MaxFile = 1 << 20

// ErrTooLarge is returned for a file larger than [MaxFile].
var ErrTooLarge = errors.New("ingest: file too large")

// Read returns the contents of name inside dir.
//
// name comes from outside. [os.Root] refuses a name that leaves dir, through
// ".." or through a symbolic link, at the moment of the open, so there is no
// window between a check and the open for the file to be swapped.
func Read(dir, name string) (data []byte, err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("ingest %s: %w", dir, err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	f, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("ingest %s: %w", name, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	data, err = io.ReadAll(io.LimitReader(f, MaxFile+1))
	if err != nil {
		return nil, fmt.Errorf("ingest %s: %w", name, err)
	}
	if len(data) > MaxFile {
		return nil, ErrTooLarge
	}
	return data, nil
}
