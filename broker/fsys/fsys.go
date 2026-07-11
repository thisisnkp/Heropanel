// Package fsys is the broker's filesystem abstraction for capabilities that
// write configuration files (web-server vhosts, PHP-FPM pools, ...). Like
// exec.Runner, it has a real OS implementation and a Fake for tests, so config
// writes are auditable and testable without touching the real filesystem.
package fsys

import (
	"errors"
	"io/fs"
	"os"
)

// FS is the set of filesystem operations capabilities may perform.
type FS interface {
	MkdirAll(path string, mode fs.FileMode) error
	WriteFile(path string, data []byte, mode fs.FileMode) error
	ReadFile(path string) ([]byte, error)
	Remove(path string) error
	RemoveAll(path string) error
	Exists(path string) (bool, error)
}

// OS is the real, os-backed implementation.
type OS struct{}

func (OS) MkdirAll(path string, mode fs.FileMode) error { return os.MkdirAll(path, mode) }
func (OS) WriteFile(path string, data []byte, mode fs.FileMode) error {
	return os.WriteFile(path, data, mode)
}
func (OS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (OS) Remove(path string) error             { return os.Remove(path) }
func (OS) RemoveAll(path string) error          { return os.RemoveAll(path) }
func (OS) Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
