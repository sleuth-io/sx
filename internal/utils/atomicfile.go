package utils

import (
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to target via a UNIQUE temp file + rename,
// with the final mode applied to the temp file BEFORE the rename — the
// target never exists partially written, and never with wider
// permissions than requested (secret-bearing files rely on that
// ordering). CreateTemp gives each concurrent writer its own file; last
// rename wins, nobody errors.
func AtomicWriteFile(target string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
