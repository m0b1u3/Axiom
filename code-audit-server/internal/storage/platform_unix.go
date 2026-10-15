//go:build linux || darwin

package storage

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"syscall"
)

const CleanupSupported = true

func sameDevice(a, b os.FileInfo) bool {
	left, ok := a.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	right, ok := b.Sys().(*syscall.Stat_t)
	return ok && left.Dev == right.Dev
}

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
	var stat unix.Statfs_t
	if e := unix.Statfs(at, &stat); e != nil {
		return 0, 0, e
	}
	free, total := uint64(stat.Bavail)*uint64(stat.Bsize), uint64(stat.Blocks)*uint64(stat.Bsize)
	if free > uint64(MaxBytes) || total > uint64(MaxBytes) {
		return 0, 0, ErrBoundary
	}
	return int64(free), int64(total), nil
}
func renameRoot(from *os.Root, name string, to *os.Root, destination string) error {
	a, e := from.Open(".")
	if e != nil {
		return e
	}
	defer a.Close()
	b, e := to.Open(".")
	if e != nil {
		return e
	}
	defer b.Close()
	return unix.Renameat(int(a.Fd()), name, int(b.Fd()), destination)
}
