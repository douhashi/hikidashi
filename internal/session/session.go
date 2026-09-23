// Package session は引き出しの sessions/ にあるセッション状態を読み書きし、後始末する。
// 状態の意味は docs/development/architecture.md の「状態モデル」、形は「スキーマ」を参照。
package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/douhashi/hikidashi/internal/jsonfile"
)

// State はセッションの状態の識別子。
type State string

const (
	// Running は Claude がターンを処理している状態。
	Running State = "running"
	// Waiting はターンの途中で人間の判断を待っている状態。
	Waiting State = "waiting"
	// Idle はターンが終わり、人間の次の指示を待っている状態。
	Idle State = "idle"
)

// processName は Claude Code 本体のプロセス名。Alive が PID の再利用と見分けるのに使う。
const processName = "claude"

// Session は 1 セッションの状態。JSON の形は sessions/<session_id>.json のスキーマ。
type Session struct {
	SessionID      string    `json:"session_id"`
	Cwd            string    `json:"cwd"`
	TmuxPane       string    `json:"tmux_pane"`
	ClaudePID      int       `json:"claude_pid"`
	TranscriptPath string    `json:"transcript_path"`
	State          State     `json:"state"`
	StateChangedAt time.Time `json:"state_changed_at"`
	StartedAt      time.Time `json:"started_at"`
}

// validID はファイル名に使える session_id。パスの区切り・`.`・glob のメタ文字を含まない。
var validID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidID は id がファイル名に使える session_id（`[A-Za-z0-9_-]+`）かを返す。
// それ以外の id でファイルを扱うと、パス横断や glob の誤爆を起こす。
func ValidID(id string) bool {
	return validID.MatchString(id)
}

// Read は引き出し drawerDir にある id のセッション状態を返す。
// ファイルが無ければ ok=false を返す。壊れたファイルも書き直すべきものとして、無いものと同じに扱う。
func Read(drawerDir, id string) (Session, bool, error) {
	return jsonfile.Read[Session](file(drawerDir, id))
}

// List は引き出し drawerDir にあるセッション状態を、ファイル名の順にすべて返す。
// 読むのは <session_id>.json だけで、壊れたファイルと中身の session_id がファイル名と食い違うファイルは飛ばす。
func List(drawerDir string) ([]Session, error) {
	entries, err := os.ReadDir(dir(drawerDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var sessions []Session
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !ValidID(id) {
			continue
		}
		s, ok, err := Read(drawerDir, id)
		if err != nil {
			return nil, err
		}
		if ok && s.SessionID == id {
			sessions = append(sessions, s)
		}
	}
	return sessions, nil
}

// Write は s を引き出し drawerDir の sessions/ にアトミックに書く。sessions/ が無ければ 0700 で作る。
func Write(drawerDir string, s Session) error {
	if err := makeDir(drawerDir); err != nil {
		return err
	}
	return jsonfile.Write(file(drawerDir, s.SessionID), s)
}

// Remove は引き出し drawerDir の sessions/ から id のファイル（<id>.*）をすべて消す。
// id は glob のメタ文字を含まないこと。
func Remove(drawerDir, id string) error {
	files, err := filepath.Glob(filepath.Join(dir(drawerDir), id+".*"))
	if err != nil {
		return err
	}
	for _, f := range files {
		// 同じセッションの SessionEnd が重なって先に消されていても、目的は果たせている。
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// makeDir は引き出し drawerDir の sessions/ を、無ければ 0700 で作る。
func makeDir(drawerDir string) error {
	return os.MkdirAll(dir(drawerDir), 0o700)
}

func dir(drawerDir string) string {
	return filepath.Join(drawerDir, "sessions")
}

func file(drawerDir, id string) string {
	return filepath.Join(dir(drawerDir), id+".json")
}
