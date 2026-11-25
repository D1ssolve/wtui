package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLockExcludesOtherOpenAndReportsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	result := make(chan error, 1)
	go func() {
		other, err := Lock(path)
		if other != nil {
			other()
		}
		result <- err
	}()
	err = <-result
	var owner *LockedError
	if !errors.Is(err, ErrLocked) || !errors.As(err, &owner) || owner.PID != os.Getpid() {
		t.Fatalf("second Lock: %v; want pid %d", err, os.Getpid())
	}
}

func TestWriteFileAtomicReplacesCompleteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	for _, s := range []string{"original state", "new"} {
		if err := WriteFileAtomic(path, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Fatalf("read = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %v", info.Mode())
	}
}

func TestWriteFileAtomicFailedRenameCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	err := WriteFileAtomic(path, []byte("state"), 0600)
	if err == nil {
		t.Fatal("replaced directory")
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 1 || !entries[0].IsDir() || entries[0].Name() != "target" {
		t.Fatalf("unexpected entries: %v", entries)
	}
	t.Logf("injected failure: %v; only original target directory remains", err)
}
