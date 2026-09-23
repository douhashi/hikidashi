// Package drawer は作業ディレクトリから引き出しを解決・登録・検索し、登録済みの引き出しを列挙する。
// 規則は docs/development/architecture.md の「引き出しの解決と登録」を参照。
package drawer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/douhashi/hikidashi/internal/jsonfile"
)

// Drawer は 1 つのリポジトリに対応する引き出し。JSON の形は drawer.json のスキーマ。
type Drawer struct {
	// Dir はデータルート配下の引き出しのディレクトリ（drawers/<slug>）。
	Dir string `json:"-"`
	// Path はリポジトリのルートの絶対パス（シンボリックリンク解決済み）。
	Path string `json:"path"`
	// Name は Path の basename。一覧での表示名。
	Name string `json:"name"`
	// CreatedAt は登録時刻。Register が drawer.json を書くときに決まる。
	CreatedAt time.Time `json:"created_at"`
}

// DefaultRoot は既定のデータルート（~/.hikidashi）を返す。
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hikidashi"), nil
}

// Resolve は cwd を含むリポジトリの引き出しを、dataRoot 配下に解決する。ファイルは作らない。
// cwd が Git の作業ツリーの中に無ければ ok=false を返す。git を起動できないときだけ error を返す。
func Resolve(dataRoot, cwd string) (Drawer, bool, error) {
	root, ok, err := repoRoot(cwd)
	if !ok || err != nil {
		return Drawer{}, ok, err
	}
	name := filepath.Base(root)
	sum := sha256.Sum256([]byte(root))
	return Drawer{
		Dir:  filepath.Join(drawersDir(dataRoot), name+"-"+hex.EncodeToString(sum[:])[:8]),
		Path: root,
		Name: name,
	}, true, nil
}

// Lookup は cwd を含むリポジトリの、登録済み（drawer.json がある）の引き出しを返す。ファイルは作らない。
// Git 管理外・未登録なら ok=false を返す。git を起動できない・drawer.json を読めない・壊れているときは error を返す。
func Lookup(dataRoot, cwd string) (Drawer, bool, error) {
	d, ok, err := Resolve(dataRoot, cwd)
	if !ok || err != nil {
		return Drawer{}, ok, err
	}
	file := metaFile(d.Dir)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Drawer{}, false, nil
	}
	if err != nil {
		return Drawer{}, false, err
	}
	var meta Drawer
	if err := json.Unmarshal(data, &meta); err != nil {
		return Drawer{}, false, fmt.Errorf("decode %s: %w", file, err)
	}
	d.CreatedAt = meta.CreatedAt
	return d, true, nil
}

// repoRoot は cwd のリポジトリのルートを返す。共通の .git を持つ通常のリポジトリはその親
// （worktree からでもメイン worktree に寄る）、それ以外（submodule 等）は作業ツリーの最上位とする。
func repoRoot(cwd string) (string, bool, error) {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir", "--show-toplevel").Output()
	if _, exited := errors.AsType[*exec.ExitError](err); exited {
		// Git 管理外・bare・.git の中・cwd が無い、のいずれか。どれも追跡しない。
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("run git: %w", err)
	}

	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 {
		return "", false, fmt.Errorf("unexpected git rev-parse output %q", out)
	}
	commonDir, toplevel := lines[0], lines[1]
	root := toplevel
	if filepath.Base(commonDir) == ".git" {
		root = filepath.Dir(commonDir)
	}

	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", false, err
	}
	return root, true, nil
}

// Register は引き出しのディレクトリ（0700）を作り、drawer.json（0600）が無ければ書く。
// 既に登録済みなら何もしない。同時の初回登録は後勝ちになるが、どちらも正しい内容なので問題ない。
func (d Drawer) Register() error {
	if err := os.MkdirAll(d.Dir, 0o700); err != nil {
		return err
	}
	file := metaFile(d.Dir)
	_, err := os.Stat(file)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	d.CreatedAt = time.Now()
	return jsonfile.Write(file, d)
}

// NotesPath は引き出しの備忘録（notes.md）のパスを返す。ファイルがあるとは限らない。
func (d Drawer) NotesPath() string {
	return filepath.Join(d.Dir, "notes.md")
}

// List は dataRoot 配下に登録済みの引き出しを、ディレクトリ名の順にすべて返す。
// drawer.json が無い（登録の途中）・壊れているディレクトリは引き出しとして扱わない。
func List(dataRoot string) ([]Drawer, error) {
	entries, err := os.ReadDir(drawersDir(dataRoot))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var drawers []Drawer
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(drawersDir(dataRoot), e.Name())
		d, ok, err := jsonfile.Read[Drawer](metaFile(dir))
		if err != nil {
			return nil, err
		}
		if ok {
			d.Dir = dir
			drawers = append(drawers, d)
		}
	}
	return drawers, nil
}

// drawersDir は dataRoot 配下の、引き出しのディレクトリを並べる場所を返す。
func drawersDir(dataRoot string) string {
	return filepath.Join(dataRoot, "drawers")
}

// metaFile は引き出しのディレクトリ dir にある、引き出しのメタ情報のファイルのパスを返す。
func metaFile(dir string) string {
	return filepath.Join(dir, "drawer.json")
}
