package session

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"syscall"
)

// Alive は pid のプロセスが生きていて、名前（/proc/<pid>/comm）が claude かを返す。
// PID の再利用で別のプロセスを生きている claude と取り違えないよう、名前まで確かめる。
func Alive(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	comm, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	// 読む途中でプロセスが終わると ESRCH になる。
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return string(bytes.TrimSuffix(comm, []byte("\n"))) == "claude", nil
}
