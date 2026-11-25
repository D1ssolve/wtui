// Package fsutil provides durable replacement and process-scoped file locks.
package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrLocked = errors.New("file is locked")

type LockedError struct{ PID int }

func (e *LockedError) Error() string {
	if e.PID == 0 {
		return "locked (owner not yet recorded)"
	}
	return fmt.Sprintf("locked (pid %d)", e.PID)
}

func (e *LockedError) Unwrap() error { return ErrLocked }

// Lock takes a non-blocking exclusive lock. Never unlink the sidecar: an opener
// racing unlink could otherwise acquire a different inode for the same state.
func Lock(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := tryLock(f); err != nil {
		if errors.Is(err, ErrLocked) {
			var record [32]byte
			n, _ := f.ReadAt(record[:], 0)
			pid, _ := strconv.Atoi(strings.TrimSpace(string(record[:n])))
			err = &LockedError{PID: pid}
		}
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	// One fixed-size write, so a shorter PID never leaves digits from a previous owner.
	if _, err := fmt.Fprintf(f, "%20d\n", os.Getpid()); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// WriteFileAtomic replaces path only after the complete new file is synced.
// A directory sync error is reported after replacement; the new file is visible.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".wtui-state-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(perm)
	}
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(dir)
}
