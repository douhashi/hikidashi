package session

import (
	"bytes"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Alive は pid のプロセスが生きていて、名前（sysctl kern.proc.pid の p_comm）が claude かを返す。
// PID の再利用で別のプロセスを生きている claude と取り違えないよう、名前まで確かめる。
func Alive(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	// 存在しない pid では sysctl が成功して 0 バイトを返す。unix.SysctlKinfoProc はこれを EIO にして
	// 他の失敗と区別できないため、生のバイト列を読む。
	buf, err := unix.SysctlRaw("kern.proc.pid", pid)
	if errors.Is(err, unix.ESRCH) || (err == nil && len(buf) == 0) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(buf) != unix.SizeofKinfoProc {
		return false, fmt.Errorf("sysctl kern.proc.pid.%d returned %d bytes, want %d", pid, len(buf), unix.SizeofKinfoProc)
	}
	comm := (*unix.KinfoProc)(unsafe.Pointer(&buf[0])).Proc.P_comm[:]
	if i := bytes.IndexByte(comm, 0); i >= 0 {
		comm = comm[:i]
	}
	return string(comm) == processName, nil
}
