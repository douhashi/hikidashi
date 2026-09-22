//go:build !linux

package session

import "errors"

// Alive は Linux 以外では生存を確かめられないため、常にエラーを返す。
// 確かめずに生きているとみなすと、終わったセッションが一覧に残り続けるため。
func Alive(int) (bool, error) {
	return false, errors.New("checking whether claude is running is supported only on Linux")
}
