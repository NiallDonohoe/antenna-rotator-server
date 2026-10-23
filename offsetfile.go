package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// loadOffset reads a calibration offset written by saveOffset. ok is false
// when nothing has been saved yet. A file that exists but can't be parsed is
// an error: starting with the wrong offset would point the antenna the
// wrong way without any sign that something is off.
func loadOffset(path string) (offset int, ok bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("could not read offset file: %v", err)
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, false, fmt.Errorf("invalid offset in %s: %q is not an integer", path, strings.TrimSpace(string(b)))
	}
	return v, true, nil
}

// saveOffset writes the offset to a temp file and renames it into place, so
// a power cut mid-write can never leave a truncated file behind.
func saveOffset(path string, offset int) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".offset-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once the rename has succeeded

	if _, err := fmt.Fprintf(tmp, "%d\n", offset); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
