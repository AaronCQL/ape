// Package lock serialises ape's write commands with flock (SPEC.md "Writes").
package lock

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// poll is how often Acquire retries a held lock.
const poll = 2 * time.Millisecond

// Acquire takes an exclusive flock on <apeDir>/.git/ape.lock, waiting up to timeout.
// It returns ErrTimeout if the lock is still held after timeout.
func Acquire(apeDir string, timeout time.Duration) (release func(), err error) {
	f, err := os.OpenFile(filepath.Join(apeDir, ".git", "ape.lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, &os.PathError{Op: "flock", Path: f.Name(), Err: err}
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, ErrTimeout
		}
		time.Sleep(min(poll, time.Until(deadline)))
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
		})
	}, nil
}

// ErrTimeout means another ape process held the lock for the whole timeout.
var ErrTimeout = errTimeout{}

type errTimeout struct{}

func (errTimeout) Error() string { return "lock timeout" }
