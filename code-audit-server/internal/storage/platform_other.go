//go:build !linux && !darwin

package storage

import (
	"errors"
	"os"
)

const CleanupSupported = false

func sameDevice(a, b os.FileInfo) bool { return true }

func renameRoot(*os.Root, string, *os.Root, string) error {
	return errors.New("当前平台尚未认证隔离清理，保留数据")
}
