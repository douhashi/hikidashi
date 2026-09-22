// Package jsonfile は JSON をファイルへアトミックに書く。
// 書き込みの方式は docs/development/architecture.md の「設計原則」2 を参照。
package jsonfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Write は v を字下げした JSON（末尾に改行）として path に書く。
// 同じディレクトリの一時ファイル（0600）に書いてから rename するため、読み手が書きかけを見ることはなく、
// 失敗しても既存のファイルは壊れず、一時ファイルも残らない。
func Write(path string, v any) (err error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')

	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}()

	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
