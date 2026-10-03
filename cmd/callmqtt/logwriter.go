package main

import (
	"os"
	"sync"
	"time"
)

// rotateRetryInterval throttles rotation attempts after a failed rename
// (Windows refuses to rename a file another program holds open), so a stuck
// rotation costs one attempt a minute rather than one per log line.
const rotateRetryInterval = time.Minute

// rotatingFile is an io.WriteCloser that rotates path to path+".1" in place
// once it would grow past limit, so a process that stays up for weeks keeps
// the same bound that rotateLogIfLarge gives at startup.
type rotatingFile struct {
	path  string
	limit int64
	now   func() time.Time

	mu        sync.Mutex
	f         *os.File
	size      int64
	nextRetry time.Time
}

func newRotatingFile(path string, limit int64) (*rotatingFile, error) {
	r := &rotatingFile{path: path, limit: limit, now: time.Now}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, info.Size()
	return nil
}

// rotate moves the live file aside and opens a fresh one. On any failure it
// keeps appending to whatever file it still has: dropping log lines or
// failing writes would be worse than an oversized log.
func (r *rotatingFile) rotate() {
	if r.now().Before(r.nextRetry) {
		return
	}
	_ = r.f.Close()
	if err := os.Rename(r.path, r.path+".1"); err != nil {
		r.nextRetry = r.now().Add(rotateRetryInterval)
	}
	if err := r.open(); err != nil {
		// Could not reopen; fall back to the original path handle state.
		r.nextRetry = r.now().Add(rotateRetryInterval)
		if f, err2 := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err2 == nil {
			r.f = f
		}
	}
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 && r.size+int64(len(p)) > r.limit {
		r.rotate()
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}
