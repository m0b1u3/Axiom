//go:build windows

package storage

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

func Space(path string) (int64, int64, error) {
	at := path
	for {
		_, e := os.Stat(at)
		if e == nil {
			break
		}
		if !os.IsNotExist(e) {
			return 0, 0, e
		}
		next := filepath.Dir(at)
		if next == at {
			return 0, 0, e
		}
		at = next
	}
	p, e := windows.UTF16PtrFromString(at)
	if e != nil {
		return 0, 0, e
	}
	var free, total, all uint64
	if e = windows.GetDiskFreeSpaceEx(p, &free, &total, &all); e != nil {
		return 0, 0, e
	}
	if free > uint64(MaxBytes) || total > uint64(MaxBytes) {
		return 0, 0, ErrBoundary
	}
	return int64(free), int64(total), nil
}
