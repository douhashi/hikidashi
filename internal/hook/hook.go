// Package hook は Claude Code の hook 入力から、登録済みの引き出しにセッション状態を記録し、備忘録を注入する。
// 遷移の規則は docs/development/architecture.md の「状態モデル」を参照。
package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
)

// Input は hook 入力のうち hikidashi が使うフィールド。
type Input struct {
	SessionID        string `json:"session_id"`
	TranscriptPath   string `json:"transcript_path"`
	Cwd              string `json:"cwd"`
	HookEventName    string `json:"hook_event_name"`
	Source           string `json:"source"`
	NotificationType string `json:"notification_type"`
}

// action は 1 つの hook 入力がセッション状態に及ぼす作用。
type action int

const (
	// ignore は何もしない。
	ignore action = iota
	// start は備忘録を注入し、セッション状態を idle で書き直す。
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

// Run は stdin の hook 入力を受け、dataRoot 配下の登録済みの引き出しのセッション状態を更新する。未登録なら何もしない。
// getenv は環境変数を、now は現在時刻を与える。stdout には SessionStart の備忘録だけを出す。
// HIKIDASHI_DISABLE=1 のときは stdin も読まずに終える（抽出の子プロセスからの再帰を断つ）。
func Run(dataRoot string, stdin io.Reader, stdout io.Writer, getenv func(string) string, now time.Time) error {
	if getenv("HIKIDASHI_DISABLE") == "1" {
		return nil
	}
	in, err := Parse(stdin)
	if err != nil {
		return err
	}
	// apply が失敗するのは classify が知るイベントだけで、session_id も検証済みなので、そのまま前置してよい。
	if err := apply(dataRoot, in, stdout, getenv, now); err != nil {
		return fmt.Errorf("%s %s: %w", in.HookEventName, in.SessionID, err)
	}
	return nil
}

// Parse は hook 入力を読み、必須のフィールドと session_id の形を確かめる。hikidashi extract も同じ入力を受けて使う。
// エラーの文面には不正な値を引用符付きで入れ、改行等がログの行を崩さないようにする。
func Parse(stdin io.Reader) (Input, error) {
	var in Input
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return Input{}, fmt.Errorf("decode input: %w", err)
	}
	for _, f := range []struct{ name, value string }{
		{"hook_event_name", in.HookEventName},
		{"cwd", in.Cwd},
		{"transcript_path", in.TranscriptPath},
	} {
		if f.value == "" {
			return Input{}, fmt.Errorf("input has no %s", f.name)
		}
	}
	if !session.ValidID(in.SessionID) {
		return Input{}, fmt.Errorf("invalid session_id %q", in.SessionID)
	}
	return in, nil
}

// rule は 1 つのイベントの入力から作用と目標の状態を決める。
type rule func(in Input) (action, session.State)

// events は hikidashi hook が扱うイベントとその規則。plugin/hooks/hooks.json が繋ぐイベントの SSoT。
// plugin の matcher には頼らず、ここで入力を絞り込む。
var events = map[string]rule{
	"SessionStart": func(in Input) (action, session.State) {
		// compact は同じセッションの続きであり、状態を変えない。圧縮で失われる備忘録だけを入れ直す。
		if in.Source == "compact" {
			return inject, ""
		}
		return start, session.Idle
	},
	"UserPromptSubmit":  always(enter, session.Running),
	"PermissionRequest": always(enter, session.Waiting),
	"Notification": func(in Input) (action, session.State) {
		switch in.NotificationType {
		case "elicitation_dialog", "elicitation_url_dialog":
			return enter, session.Waiting
		}
		return ignore, ""
	},
	"PostToolUse": always(resume, session.Running),
	"Stop":        always(enter, session.Idle),
	"StopFailure": always(enter, session.Idle),
	"SessionEnd":  always(end, ""),
}

// always は入力によらず act と state を返す規則。
func always(act action, state session.State) rule {
	return func(Input) (action, session.State) { return act, state }
}

// classify は入力から作用と目標の状態を決める。未知のイベントは何もしない。
func classify(in Input) (action, session.State) {
	r, ok := events[in.HookEventName]
	if !ok {
		return ignore, ""
	}
	return r(in)
}

// apply は入力の作用を引き出し・stdout・セッション状態に反映する。何もしない入力では git も起動しない。
// 備忘録の注入と状態の記録は、片方が失敗してももう片方を行う。
func apply(dataRoot string, in Input, stdout io.Writer, getenv func(string) string, now time.Time) error {
	act, state := classify(in)
	if act == ignore {
		return nil
	}

	d, ok, err := drawer.Lookup(dataRoot, in.Cwd)
	if err != nil || !ok {
		return err
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
	notes, ok, err := d.Notes()
	if !ok || err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(hookOutput{sessionStartOutput{
		HookEventName:     "SessionStart",
		AdditionalContext: fmt.Sprintf("# hikidashi notes for %s (%s)\n\n%s", d.Name, d.NotesPath(), notes),
	}})
}

// record は act をセッション状態に反映する。
func record(d drawer.Drawer, act action, state session.State, in Input, getenv func(string) string, now time.Time) error {
	// tmux 外のセッションは状態を記録しない。案件の tmux セッションの外は追う対象にしない（#41）。
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
