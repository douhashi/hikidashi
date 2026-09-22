// Package hook は Claude Code の hook 入力から引き出しを登録し、セッション状態を記録し、備忘録を注入する。
// 遷移の規則は docs/development/architecture.md の「状態モデル」を参照。
package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
)

// input は hook 入力のうち hikidashi が使うフィールド。
type input struct {
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	Cwd              string `json:"cwd"`
	HookEventName    string `json:"hook_event_name"`
	Source           string `json:"source"`
	NotificationType string `json:"notification_type"`
}

// validSessionID はファイル名に使える session_id。パスの区切り・`.`・glob のメタ文字を含まない。
var validSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// action は 1 つの hook 入力がセッション状態に及ぼす作用。
type action int

const (
	// ignore は何もしない。
	ignore action = iota
	// start は引き出しを登録し、備忘録を注入し、セッション状態を idle で書き直す。
	start
	// inject は備忘録を注入するだけで、状態を変えない。
	inject
	// enter は目標の状態に入る。既にその状態なら書かない。
	enter
	// resume は waiting のときだけ running に戻す。
	resume
	// end はセッションのファイルを消す。
	end
)

// Run は stdin の hook 入力を受け、dataRoot 配下の引き出しとセッション状態を更新する。
// getenv は環境変数を、now は現在時刻を与える。stdout には SessionStart の備忘録だけを出す。
// HIKIDASHI_DISABLE=1 のときは stdin も読まずに終える（抽出の子プロセスからの再帰を断つ）。
func Run(dataRoot string, stdin io.Reader, stdout io.Writer, getenv func(string) string, now time.Time) error {
	if getenv("HIKIDASHI_DISABLE") == "1" {
		return nil
	}
	in, err := parse(stdin)
	if err != nil {
		return err
	}
	// apply が失敗するのは classify が知るイベントだけで、session_id も検証済みなので、そのまま前置してよい。
	if err := apply(dataRoot, in, stdout, getenv, now); err != nil {
		return fmt.Errorf("%s %s: %w", in.HookEventName, in.SessionID, err)
	}
	return nil
}

// parse は hook 入力を読み、必須のフィールドと session_id の形を確かめる。
// エラーの文面には不正な値を引用符付きで入れ、改行等がログの行を崩さないようにする。
func parse(stdin io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	for _, f := range []struct{ name, value string }{
		{"hook_event_name", in.HookEventName},
		{"cwd", in.Cwd},
		{"transcript_path", in.TranscriptPath},
	} {
		if f.value == "" {
			return input{}, fmt.Errorf("input has no %s", f.name)
		}
	}
	if !validSessionID.MatchString(in.SessionID) {
		return input{}, fmt.Errorf("invalid session_id %q", in.SessionID)
	}
	return in, nil
}

// classify は入力から作用と目標の状態を決める。plugin の matcher には頼らず、ここで絞り込む。
func classify(in input) (action, session.State) {
	switch in.HookEventName {
	case "SessionStart":
		// compact は同じセッションの続きであり、状態を変えない。圧縮で失われる備忘録だけを入れ直す。
		if in.Source == "compact" {
			return inject, ""
		}
		return start, session.Idle
	case "UserPromptSubmit":
		return enter, session.Running
	case "PermissionRequest":
		return enter, session.Waiting
	case "Notification":
		switch in.NotificationType {
		case "elicitation_dialog", "elicitation_url_dialog":
			return enter, session.Waiting
		}
		return ignore, ""
	case "PostToolUse":
		return resume, session.Running
	case "Stop", "StopFailure":
		return enter, session.Idle
	case "SessionEnd":
		return end, ""
	}
	return ignore, ""
}

// apply は入力の作用を引き出し・stdout・セッション状態に反映する。何もしない入力では git も起動しない。
// 備忘録の注入と状態の記録は、片方が失敗してももう片方を行う。
func apply(dataRoot string, in input, stdout io.Writer, getenv func(string) string, now time.Time) error {
	act, state := classify(in)
	if act == ignore {
		return nil
	}

	d, ok, err := drawer.Resolve(dataRoot, in.Cwd)
	if err != nil || !ok {
		return err
	}
	if act == start {
		if err := d.Register(); err != nil {
			return err
		}
	}
	var injected error
	if act == start || act == inject {
		injected = injectNotes(stdout, d)
	}
	if act == inject {
		return injected
	}
	return errors.Join(injected, record(d, act, state, in, getenv, now))
}

// hookOutput は SessionStart の hook が stdout に出す JSON。
type hookOutput struct {
	HookSpecificOutput sessionStartOutput `json:"hookSpecificOutput"`
}

// sessionStartOutput は SessionStart の hookSpecificOutput。additionalContext が Claude のコンテキストに入る。
type sessionStartOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// injectNotes は引き出しの notes.md を、出所の見出しを付けて SessionStart の additionalContext として stdout に 1 回で書く。
// notes.md が無い・空白だけなら何も書かない。
func injectNotes(stdout io.Writer, d drawer.Drawer) error {
	path := d.NotesPath()
	notes, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(notes)) == "" {
		return nil
	}
	return json.NewEncoder(stdout).Encode(hookOutput{sessionStartOutput{
		HookEventName:     "SessionStart",
		AdditionalContext: fmt.Sprintf("# hikidashi notes for %s (%s)\n\n%s", d.Name, path, notes),
	}})
}

// record は act をセッション状態に反映する。
func record(d drawer.Drawer, act action, state session.State, in input, getenv func(string) string, now time.Time) error {
	// tmux 外のセッションは一覧から選んでも移動先が無いため、状態を記録しない。
	pane := getenv("TMUX_PANE")
	if pane == "" {
		return nil
	}
	if act == end {
		return session.Remove(d.Dir, in.SessionID)
	}

	startedAt := now
	if act != start {
		cur, exists, err := session.Read(d.Dir, in.SessionID)
		if err != nil {
			return err
		}
		if !changes(act, state, cur, exists) {
			return nil
		}
		if exists {
			startedAt = cur.StartedAt
		}
	}

	rawPID := getenv("CLAUDE_PID")
	pid, err := strconv.Atoi(rawPID)
	if err != nil {
		return fmt.Errorf("CLAUDE_PID %q is not an integer", rawPID)
	}
	return session.Write(d.Dir, session.Session{
		SessionID:      in.SessionID,
		Cwd:            in.Cwd,
		TmuxPane:       pane,
		ClaudePID:      pid,
		TranscriptPath: in.TranscriptPath,
		State:          state,
		StateChangedAt: now,
		StartedAt:      startedAt,
	})
}

// changes は現在の状態 cur（exists=false なら無い）に act を施すと、状態が state に変わるかを返す。
func changes(act action, state session.State, cur session.Session, exists bool) bool {
	if act == resume {
		// 権限の承認後に走行へ戻ったことだけを拾う。無い・idle のセッションを running にはしない。
		return exists && cur.State == session.Waiting
	}
	return !exists || cur.State != state
}
