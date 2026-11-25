package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestLockRecordIsOnlyAdvisory(t *testing.T) {
	for _, record := range []string{"", "invalid", "-1", strings.Repeat("9", 32), "123"} {
		t.Run("record="+record, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state")
			f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.WriteString(record); err != nil {
				t.Fatal(err)
			}
			if err := tryLock(f); err != nil {
				t.Fatal(err)
			}
			_, err = Lock(path)
			var busy *LockedError
			if !errors.As(err, &busy) {
				t.Fatalf("not locked: %v", err)
			}
			if record == "123" {
				if !strings.Contains(err.Error(), "last recorded pid 123") {
					t.Fatal(err)
				}
			} else if strings.Contains(err.Error(), "pid") {
				t.Fatalf("claimed unknown owner: %v", err)
			}
		})
	}
}

func TestReplacementCannotWidenPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	p := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(p, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(p, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("private replacement: %v, %v", st, err)
	}
}
