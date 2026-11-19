// Package fsx has file-system helpers ape's writes share.
package fsx

import (
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
)

// WriteAtomic writes data to path so that a reader, or a crash, sees either the old file or the whole new one:
// it writes a temporary file in path's directory, syncs it, and renames it over path. perm is applied as
// os.WriteFile applies it (less the umask), also when path already exists.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	f, err := createTemp(path, perm)
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// createTemp creates a new file next to path, with perm less the umask (os.CreateTemp would make it 0600).
func createTemp(path string, perm os.FileMode) (*os.File, error) {
	dir, base := filepath.Split(path)
	for {
		name := filepath.Join(dir, "."+base+".tmp"+strconv.FormatUint(rand.Uint64(), 36))
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if !errors.Is(err, os.ErrExist) {
			return f, err
		}
	}
}
