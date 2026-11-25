// Package fsutil provides durable replacement and process-scoped file locks.
package fsutil

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrLocked = errors.New("file is locked")

// PID is the last recorded PID, not a kernel-verified owner. Zero means unknown.
type LockedError struct{ PID int }

func (e *LockedError) Error() string {
	if e.PID <= 0 {
		return "locked"
	}
	return fmt.Sprintf("locked (last recorded pid %d)", e.PID)
}

func (e *LockedError) Unwrap() error { return ErrLocked }

// Lock takes a non-blocking exclusive lock. Never unlink the sidecar: an opener
// racing unlink could otherwise acquire a different inode for the same state.
func Lock(path string) (func(), error) { return lock(path, false) }

// LockShared takes a non-blocking shared lock without claiming the PID record.
func LockShared(path string) (func(), error) { return lock(path, true) }

func lock(path string, shared bool) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := tryLock(f, shared); err != nil {
		if errors.Is(err, ErrLocked) {
			var record [32]byte
			n, _ := f.ReadAt(record[:], 0)
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(record[:n])))
			if parseErr != nil || pid < 0 {
				pid = 0
			}
			err = &LockedError{PID: pid}
		}
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	// One fixed-size write, so a shorter PID never leaves digits from a previous owner.
	if !shared {
		if _, err := fmt.Fprintf(f, "%20d\n", os.Getpid()); err != nil {
			f.Close()
			return nil, err
		}
	}
	return func() { f.Close() }, nil
}

// WriteFileAtomic replaces path only after the complete new file is synced.
// A directory sync error is reported after replacement; the new file is visible.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	// Pass permissions to open, not chmod: the kernel must also apply the umask.
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		perm &= info.Mode().Perm()
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".wtui-state-"+rand.Text()), os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm.Perm())
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(data)
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
