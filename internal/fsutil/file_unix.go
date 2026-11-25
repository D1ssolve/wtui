//go:build unix

package fsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLock(f *os.File, shared bool) error {
	mode := unix.LOCK_EX
	if shared {
		mode = unix.LOCK_SH
	}
	for {
		err := unix.Flock(int(f.Fd()), mode|unix.LOCK_NB)
		switch err {
		case unix.EINTR:
			continue
		case unix.EWOULDBLOCK:
			return ErrLocked
		default:
			return err
		}
	}
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = f.Sync()
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) {
		err = nil
	}
	return errors.Join(err, f.Close())
}
