package logging

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
)

const filePerm = 0o600

// rotatingFile is an io.WriteCloser that renames the file to path.1 (shifting
// older backups up to path.N) once the next write would exceed maxBytes.
//
// It is implemented here instead of pulling a dependency because the logic is
// small and every dependency is attack surface the project has to keep updated.
type rotatingFile struct {
	mu         sync.Mutex
	path       string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

func openRotatingFile(path string, maxBytes int64, maxBackups int) (*rotatingFile, error) {
	r := &rotatingFile{path: path, maxBytes: maxBytes, maxBackups: maxBackups}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

// Write appends p, rotating first when needed. A single write larger than
// maxBytes is still written whole: a log line is never split.
func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil {
		return 0, fs.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			return 0, fmt.Errorf("rotate log file: %w", err)
		}
	}

	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

// Close flushes and closes the current file. Further writes fail.
func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

// rotate always tries to reopen the main file, even when shifting backups
// failed, so a rotation problem never silences logging for good.
func (r *rotatingFile) rotate() error {
	// Windows cannot rename a file that is still open.
	closeErr := r.file.Close()
	r.file = nil

	var shiftErr error
	if closeErr == nil {
		shiftErr = r.shiftBackups()
	}

	return errors.Join(closeErr, shiftErr, r.open())
}

func (r *rotatingFile) shiftBackups() error {
	if r.maxBackups == 0 {
		return ignoreNotExist(os.Remove(r.path))
	}
	// os.Rename replaces the destination, so the oldest backup is dropped.
	for i := r.maxBackups - 1; i >= 1; i-- {
		if err := ignoreNotExist(os.Rename(backupPath(r.path, i), backupPath(r.path, i+1))); err != nil {
			return err
		}
	}
	return ignoreNotExist(os.Rename(r.path, backupPath(r.path, 1)))
}

func (r *rotatingFile) open() error {
	file, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filePerm)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(fmt.Errorf("stat log file: %w", err), file.Close())
	}

	r.file = file
	r.size = info.Size()
	return nil
}

func backupPath(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}

func ignoreNotExist(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
