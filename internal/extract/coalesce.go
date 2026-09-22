package extract

import (
	"errors"
	"os"
	"syscall"

	"github.com/douhashi/hikidashi/internal/session"
)

// coalesce は引き出し drawerDir の id の抽出の要求を 1 つ出し、誰も抽出していなければ自分で run を回す。
// 抽出中の保持者がいれば要求を残すだけで終え、保持者が後でそれを拾う。連続した要求は後ろ寄せで 1 回にまとまり、
// 同じセッションの run が並行することはない。
//
// 要求の印は extract.lock の中身（空でなければ要求あり）で、排他は同じファイルの flock で取る。
// 保持者は「印を消して run」を印が無くなるまで繰り返す。解放の直前に立った印の要求者は flock を取れずに終わっているため、
// 解放後に印が残っていれば取り直す。
func coalesce(drawerDir, id string, run func() error) error {
	f, err := session.OpenExtractLock(drawerDir, id)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteAt([]byte("1"), 0); err != nil {
		return err
	}
	var errs []error
	for {
		held, err := tryLock(f)
		if err != nil || !held {
			return errors.Join(append(errs, err)...)
		}
		for {
			requested, err := marked(f)
			if err != nil {
				errs = append(errs, err)
				break
			}
			if !requested {
				break
			}
			if err := f.Truncate(0); err != nil {
				errs = append(errs, err)
				break
			}
			if err := run(); err != nil {
				errs = append(errs, err)
			}
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
			return errors.Join(append(errs, err)...)
		}
		if requested, err := marked(f); err != nil || !requested {
			return errors.Join(append(errs, err)...)
		}
	}
}

// tryLock は f の排他ロックを待たずに取る。他が持っていれば held=false を返す。
func tryLock(f *os.File) (held bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

// marked は抽出の要求の印が立っているかを返す。
func marked(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	return info.Size() > 0, nil
}
