package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// rotatingFile is a log file that starts over when it grows past maxBytes,
// keeping the previous `keep` files beside it as name.1 (newest) … name.keep.
//
// A copy of the log on disk outlives the process that wrote it. In Docker the
// container's own log goes when the container is replaced — which is what an
// upgrade does — and the minutes before an upgrade are the ones most often
// wanted afterwards. So the file lives on the data volume, bounded in size.
type rotatingFile struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int
	file     *os.File
	size     int64
}

func openRotating(path string, maxBytes int64, keep int) (*rotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	r := &rotatingFile{path: path, maxBytes: maxBytes, keep: keep}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.file, r.size = f, info.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > r.maxBytes && r.size > 0 {
		if err := r.rotate(); err != nil {
			// Losing the file is better than losing the line: it still goes to
			// stdout, and the next write tries again.
			return len(p), nil
		}
	}
	if r.file == nil {
		return len(p), nil
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) rotate() error {
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
	for i := r.keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
	}
	if r.keep >= 1 {
		_ = os.Rename(r.path, r.path+".1")
	} else {
		_ = os.Remove(r.path)
	}
	return r.open()
}
