//go:build !linux && !darwin && !windows

package storage

import "errors"

func Space(string) (int64, int64, error) {
	return 0, 0, errors.New("当前平台尚未认证容量探测，显示未知并暂停新领取")
}
