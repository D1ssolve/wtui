//go:build unix

package fsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLock(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
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
