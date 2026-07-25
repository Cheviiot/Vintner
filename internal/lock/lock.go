// Package lock guards a destination directory against two `vintner
// download`/`install` runs mutating it at the same time.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// FileName is the advisory lock file `download` and `install` both take
// out against their (usually shared) destination directory before
// touching anything in it - concurrent download+install, or two
// downloads, against the same dest could otherwise interleave badly:
// combineDirTrees' merge logic assumes it's the only thing moving files
// into a given target at a time, and two `os.Rename` calls racing for the
// same destination path is exactly the kind of thing that corrupts a tree
// instead of erroring cleanly.
const FileName = ".vintner.lock"

// Acquire takes an exclusive, non-blocking lock on dest (creating dest if
// it doesn't exist yet) and returns a func to release it, which the caller
// must defer. If another vintner process already holds the lock, returns
// an error immediately instead of blocking - there's no reason a second
// invocation should silently queue up and wait for the first to finish
// touching the same directory; the caller should simply not have started
// it yet. Uses flock(2), so a crashed holder's lock is released
// automatically by the kernel when its file descriptor closes - never
// needs manual cleanup, unlike a plain "does a file exist" lock
// convention would.
func Acquire(dest string) (unlock func(), err error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dest, FileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another vintner download/install is already running against %s", dest)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
