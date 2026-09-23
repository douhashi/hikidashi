// Package drawer は作業ディレクトリや名前から引き出しを解決・登録・検索・取り消しし、登録済みの引き出しを列挙する。
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
	"slices"
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

// Slug は引き出しの識別子（<name>-<ハッシュ 8 桁>）。引き出しのディレクトリ名である。
func (d Drawer) Slug() string {
	return filepath.Base(d.Dir)
}

// TmuxSession は引き出しに対応する tmux セッションの名前を返す。
// tmux はセッション名の . と : をターゲットの区切りに使うため、slug のそれらを _ に置き換える。
func (d Drawer) TmuxSession() string {
	return strings.NewReplacer(".", "_", ":", "_").Replace(d.Slug())
}

// NotesPath は引き出しの備忘録（notes.md）のパスを返す。ファイルがあるとは限らない。
func (d Drawer) NotesPath() string {
	return filepath.Join(d.Dir, "notes.md")
}

// Notes は引き出しの備忘録の本文を返す。notes.md が無い・空白だけなら ok=false を返す。
func (d Drawer) Notes() (string, bool, error) {
	data, err := os.ReadFile(d.NotesPath())
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	notes := string(data)
	return notes, strings.TrimSpace(notes) != "", nil
}

// Unregister は引き出しの登録を取り消す。先に drawer.json を消して以後の記録・一覧の対象から外し、
// 次に notes.md 以外（sessions/ 等）を消す。備忘録が無い・空白だけならディレクトリごと消す。
// 空でない備忘録は残し、同じリポジトリの再登録（Register）で戻る。残したかどうかを返す。
func (d Drawer) Unregister() (notesKept bool, err error) {
	if err := os.Remove(metaFile(d.Dir)); err != nil {
		return false, err
	}
	_, notesKept, err = d.Notes()
	if err != nil {
		return false, err
	}
	if !notesKept {
		return false, os.RemoveAll(d.Dir)
	}
	entries, err := os.ReadDir(d.Dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Name() == filepath.Base(d.NotesPath()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(d.Dir, e.Name())); err != nil {
			return false, err
		}
	}
	return true, nil
}

// Find は dataRoot に登録済みの引き出しから、name を slug または名前（Name）に持つものを返す。
// slug の完全一致を優先し、無ければ名前の一致が 1 件のときだけそれを返す。どれにも一致しなければ ok=false、
// 名前が複数に一致すれば候補の slug を並べたエラーを返す。
func Find(dataRoot, name string) (Drawer, bool, error) {
	drawers, err := List(dataRoot)
	if err != nil {
		return Drawer{}, false, err
	}
	if i := slices.IndexFunc(drawers, func(d Drawer) bool { return d.Slug() == name }); i >= 0 {
		return drawers[i], true, nil
	}

	var matched []Drawer
	for _, d := range drawers {
		if d.Name == name {
			matched = append(matched, d)
		}
	}
	switch len(matched) {
	case 0:
		return Drawer{}, false, nil
	case 1:
		return matched[0], true, nil
	}
	slugs := make([]string, 0, len(matched))
	for _, d := range matched {
		slugs = append(slugs, d.Slug())
	}
	return Drawer{}, false, fmt.Errorf("%q matches more than one drawer: %s", name, strings.Join(slugs, ", "))
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
