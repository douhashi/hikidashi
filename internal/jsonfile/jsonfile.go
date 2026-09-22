// Package jsonfile は JSON のファイルを読み、アトミックに書く。
// 書き込みの方式は docs/development/architecture.md の「設計原則」2 を参照。
package jsonfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Read は path の JSON を T として返す。
// ファイルが無ければ ok=false を返す。壊れたファイルも、書き手が書き直すべきものとして無いものと同じに扱う。
func Read[T any](path string) (v T, ok bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return v, false, nil
	}
	if err != nil {
		return v, false, err
	}
	if err := json.Unmarshal(data, &v); err != nil {
		var zero T
		return zero, false, nil
	}
	return v, true, nil
}

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
