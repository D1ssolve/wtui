package main

import (
	"errors"
	"syscall"
	"time"
)

const errorSharingViolation syscall.Errno = 32

// lockPublication opens path with no sharing, which excludes every other opener until the handle
// is closed; Windows closes it if the process dies, so a crashed init never leaves a stale lock.
func lockPublication(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	for range 200 { // publication takes milliseconds; give up after about 10s
		h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
		if err == nil {
			return func() { syscall.CloseHandle(h) }, nil
		}
		if !errors.Is(err, errorSharingViolation) {
			return nil, err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, errors.New("another wtui init is still writing this config")
}
