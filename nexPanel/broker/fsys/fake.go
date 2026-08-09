package fsys

import (
	"io/fs"
	"strings"
	"sync"
)

// Fake is an in-memory FS for tests. It records written files and can simulate
// pre-existing content (for backup/rollback paths).
type Fake struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
	// FailWrite, if set, makes WriteFile return an error for matching paths.
	FailWrite func(path string) error
}

// NewFake returns an empty Fake.
func NewFake() *Fake {
	return &Fake{files: map[string][]byte{}, dirs: map[string]bool{}}
}

func (f *Fake) MkdirAll(path string, _ fs.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirs[path] = true
	return nil
}

func (f *Fake) WriteFile(path string, data []byte, _ fs.FileMode) error {
	if f.FailWrite != nil {
		if err := f.FailWrite(path); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	f.files[path] = cp
	return nil
}

func (f *Fake) ReadFile(path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp, nil
}

func (f *Fake) Remove(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.files, path)
	return nil
}

func (f *Fake) RemoveAll(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.files {
		if k == path || strings.HasPrefix(k, path+"/") {
			delete(f.files, k)
		}
	}
	return nil
}

func (f *Fake) Exists(path string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[path]
	return ok, nil
}

// Written returns the content written to path and whether it exists (test helper).
func (f *Fake) Written(path string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.files[path]
	return string(b), ok
}
