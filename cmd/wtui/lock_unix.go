//go:build unix

package main

import (
	"os"
	"syscall"
)

// lockPublication blocks until this process holds an exclusive flock on path; the kernel drops it
// if the process dies, so a crashed init never leaves a stale lock.
func lockPublication(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
