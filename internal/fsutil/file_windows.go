package fsutil

import (
	"os"

	"golang.org/x/sys/windows"
)

func tryLock(f *os.File) error {
	// Windows also prevents reads of the locked range. Leave the PID at offset 0 readable.
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{Offset: 32})
	if err == windows.ERROR_LOCK_VIOLATION {
		return ErrLocked
	}
	return err
}

func syncDir(string) error { return nil }
