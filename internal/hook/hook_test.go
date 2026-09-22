package hook

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

const sessionID = "4f1c-a_B"

// now はテストの hook が受け取る現在時刻。事前状態の時刻 earlier より後。
var (
	now     = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	earlier = time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
)

// env は hook が読む環境変数の既定値。
var env = map[string]string{"TMUX_PANE": "%7", "CLAUDE_PID": "31337"}

// fixture は hook を動かす実リポジトリとデータルート。
type fixture struct {
	dataRoot string
	repo     string
	drawer   drawer.Drawer
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "api"))
	dataRoot := t.TempDir()
	d, ok, err := drawer.Resolve(dataRoot, repo)
	if err != nil || !ok {
		t.Fatalf("Resolve(%q) = ok %v, err %v", repo, ok, err)
	}
	return fixture{dataRoot: dataRoot, repo: repo, drawer: d}
}

func (f fixture) sessionFile() string {
	return filepath.Join(f.drawer.Dir, "sessions", sessionID+".json")
}

// input は event の hook 入力を JSON で組み立てる。extra は入力に足すフィールド。
func (f fixture) input(event string, extra map[string]string) string {
	in := map[string]string{
		"session_id":      sessionID,
		"transcript_path": "/home/u/.claude/projects/api/" + sessionID + ".jsonl",
		"cwd":             f.repo,
		"hook_event_name": event,
		"permission_mode": "default",
	}
	for k, v := range extra {
		in[k] = v
	}
	data, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// run は env に overrides を重ねた環境で Run を呼ぶ。
func (f fixture) run(stdin string, overrides map[string]string) error {
	return Run(f.dataRoot, strings.NewReader(stdin), getenv(overrides), now)
}

func getenv(overrides map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := overrides[k]; ok {
			return v
		}
		return env[k]
	}
}

// seed は事前状態 state のセッション状態を、今回の入力とは違う値で書く。
func (f fixture) seed(t *testing.T, state session.State) {
	t.Helper()
	err := session.Write(f.drawer.Dir, session.Session{
		SessionID:      sessionID,
		Cwd:            "/old/cwd",
		TmuxPane:       "%1",
		ClaudePID:      1,
		TranscriptPath: "/old/transcript.jsonl",
		State:          state,
		StateChangedAt: earlier,
		StartedAt:      earlier,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// outcome は 1 つの hook 入力の後に期待するセッションのファイル。
type outcome struct {
	// keep はファイルが事前状態のまま（無ければ無いまま）であること。
	keep bool
	// state は keep でないときに書かれている状態。
	state session.State
	// fresh は started_at が now で作り直されていること。偽なら事前状態の started_at を保つ。
	fresh bool
}

var (
	keep         = outcome{keep: true}
	toIdle       = outcome{state: session.Idle}
	toRunning    = outcome{state: session.Running}
	toWaiting    = outcome{state: session.Waiting}
	startIdle    = outcome{state: session.Idle, fresh: true}
	startRunning = outcome{state: session.Running, fresh: true}
	startWaiting = outcome{state: session.Waiting, fresh: true}
)

// none は事前状態が無い（ファイルが無い）ことを表す。
const none session.State = ""

// label はサブテスト名に使う事前状態の名前。
func label(pre session.State) string {
	if pre == none {
		return "none"
	}
	return string(pre)
}

func TestRunFollowsStateModel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event string
		extra map[string]string
		want  map[session.State]outcome
	}{
		{"SessionStart startup", "SessionStart", map[string]string{"source": "startup"},
			map[session.State]outcome{none: startIdle, session.Idle: startIdle, session.Running: startIdle, session.Waiting: startIdle}},
		{"SessionStart resume", "SessionStart", map[string]string{"source": "resume"},
			map[session.State]outcome{none: startIdle, session.Idle: startIdle, session.Running: startIdle, session.Waiting: startIdle}},
		{"SessionStart clear", "SessionStart", map[string]string{"source": "clear"},
			map[session.State]outcome{none: startIdle, session.Idle: startIdle, session.Running: startIdle, session.Waiting: startIdle}},
		{"SessionStart fork", "SessionStart", map[string]string{"source": "fork"},
			map[session.State]outcome{none: startIdle, session.Idle: startIdle, session.Running: startIdle, session.Waiting: startIdle}},
		{"SessionStart compact", "SessionStart", map[string]string{"source": "compact"},
			map[session.State]outcome{none: keep, session.Idle: keep, session.Running: keep, session.Waiting: keep}},
		{"UserPromptSubmit", "UserPromptSubmit", map[string]string{"prompt": "hi"},
			map[session.State]outcome{none: startRunning, session.Idle: toRunning, session.Running: keep, session.Waiting: toRunning}},
		{"PermissionRequest", "PermissionRequest", map[string]string{"tool_name": "Bash"},
			map[session.State]outcome{none: startWaiting, session.Idle: toWaiting, session.Running: toWaiting, session.Waiting: keep}},
		{"Notification elicitation_dialog", "Notification", map[string]string{"notification_type": "elicitation_dialog"},
			map[session.State]outcome{none: startWaiting, session.Idle: toWaiting, session.Running: toWaiting, session.Waiting: keep}},
		{"Notification elicitation_url_dialog", "Notification", map[string]string{"notification_type": "elicitation_url_dialog"},
			map[session.State]outcome{none: startWaiting, session.Idle: toWaiting, session.Running: toWaiting, session.Waiting: keep}},
		{"Notification permission_prompt", "Notification", map[string]string{"notification_type": "permission_prompt"},
			map[session.State]outcome{none: keep, session.Idle: keep, session.Running: keep, session.Waiting: keep}},
		{"Notification idle_prompt", "Notification", map[string]string{"notification_type": "idle_prompt"},
			map[session.State]outcome{none: keep, session.Idle: keep, session.Running: keep, session.Waiting: keep}},
		{"PostToolUse", "PostToolUse", map[string]string{"tool_name": "Bash"},
			map[session.State]outcome{none: keep, session.Idle: keep, session.Running: keep, session.Waiting: toRunning}},
		{"Stop", "Stop", map[string]string{"last_assistant_message": "done"},
			map[session.State]outcome{none: startIdle, session.Idle: keep, session.Running: toIdle, session.Waiting: toIdle}},
		{"StopFailure", "StopFailure", map[string]string{"error": "rate_limit"},
			map[session.State]outcome{none: startIdle, session.Idle: keep, session.Running: toIdle, session.Waiting: toIdle}},
		{"unknown event", "PreToolUse", map[string]string{"tool_name": "Bash"},
			map[session.State]outcome{none: keep, session.Idle: keep, session.Running: keep, session.Waiting: keep}},
	} {
		for _, pre := range []session.State{none, session.Idle, session.Running, session.Waiting} {
			t.Run(tc.name+"/from "+label(pre), func(t *testing.T) {
				f := newFixture(t)
				if pre != none {
					f.seed(t, pre)
				}
				before := readIfExists(t, f.sessionFile())

				if err := f.run(f.input(tc.event, tc.extra), nil); err != nil {
					t.Fatalf("Run: %v", err)
				}

				f.assertOutcome(t, tc.want[pre], before)
			})
		}
	}
}

func (f fixture) assertOutcome(t *testing.T, want outcome, before *string) {
	t.Helper()
	if want.keep {
		after := readIfExists(t, f.sessionFile())
		if (before == nil) != (after == nil) || (before != nil && *before != *after) {
			t.Errorf("session file = %s, want unchanged %s", show(after), show(before))
		}
		return
	}

	got, ok, err := session.Read(f.drawer.Dir, sessionID)
	if err != nil || !ok {
		t.Fatalf("session.Read = ok %v, err %v, want a session", ok, err)
	}
	started := earlier
	if want.fresh {
		started = now
	}
	expected := session.Session{
		SessionID:      sessionID,
		Cwd:            f.repo,
		TmuxPane:       "%7",
		ClaudePID:      31337,
		TranscriptPath: "/home/u/.claude/projects/api/" + sessionID + ".jsonl",
		State:          want.state,
		StateChangedAt: now,
		StartedAt:      started,
	}
	if !got.StateChangedAt.Equal(expected.StateChangedAt) || !got.StartedAt.Equal(expected.StartedAt) {
		t.Errorf("times = changed %v, started %v, want %v, %v", got.StateChangedAt, got.StartedAt, expected.StateChangedAt, expected.StartedAt)
	}
	got.StateChangedAt, got.StartedAt = expected.StateChangedAt, expected.StartedAt
	if got != expected {
		t.Errorf("session = %+v, want %+v", got, expected)
	}
}

func TestRunSessionStartRegistersDrawer(t *testing.T) {
	f := newFixture(t)

	if err := f.run(f.input("SessionStart", map[string]string{"source": "startup"}), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := testutil.ReadFile(t, filepath.Join(f.drawer.Dir, "drawer.json"))
	if !strings.Contains(got, `"path": "`+f.drawer.Path+`"`) {
		t.Errorf("drawer.json = %s, want path %q", got, f.drawer.Path)
	}
}

func TestRunWritesFilesReadableOnlyByOwner(t *testing.T) {
	f := newFixture(t)

	if err := f.run(f.input("SessionStart", map[string]string{"source": "startup"}), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, dir := range []string{filepath.Join(f.dataRoot, "drawers"), f.drawer.Dir, filepath.Dir(f.sessionFile())} {
		testutil.AssertPerm(t, dir, 0o700)
	}
	testutil.AssertPerm(t, f.sessionFile(), 0o600)
}

func TestRunWithoutTmuxPaneRegistersButDoesNotRecord(t *testing.T) {
	f := newFixture(t)
	noPane := map[string]string{"TMUX_PANE": ""}

	for _, in := range []string{
		f.input("SessionStart", map[string]string{"source": "startup"}),
		f.input("UserPromptSubmit", nil),
		f.input("Stop", nil),
	} {
		if err := f.run(in, noPane); err != nil {
			t.Fatalf("Run(%s): %v", in, err)
		}
	}

	testutil.ReadFile(t, filepath.Join(f.drawer.Dir, "drawer.json"))
	testutil.AssertNotExist(t, filepath.Dir(f.sessionFile()))
}

func TestRunSessionEndRemovesFilesOfTheSession(t *testing.T) {
	f := newFixture(t)
	f.seed(t, session.Running)
	sessions := filepath.Dir(f.sessionFile())
	next := filepath.Join(sessions, sessionID+".next.json")
	lock := filepath.Join(sessions, sessionID+".extract.lock")
	other := filepath.Join(sessions, "other.json")
	for _, p := range []string{next, lock, other} {
		testutil.WriteFile(t, p, "{}")
	}

	if err := f.run(f.input("SessionEnd", map[string]string{"reason": "prompt_input_exit"}), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, p := range []string{f.sessionFile(), next, lock} {
		testutil.AssertNotExist(t, p)
	}
	testutil.ReadFile(t, other)
}

func TestRunDisabledDoesNotReadInput(t *testing.T) {
	f := newFixture(t)

	err := Run(f.dataRoot, unreadable{t}, getenv(map[string]string{"HIKIDASHI_DISABLE": "1"}), now)

	if err != nil {
		t.Errorf("Run: %v", err)
	}
	testutil.AssertEntries(t, f.dataRoot)
}

// unreadable は読まれたらテストを失敗させる stdin。
type unreadable struct{ t *testing.T }

func (u unreadable) Read([]byte) (int, error) {
	u.t.Error("stdin was read")
	return 0, io.EOF
}

func TestRunOutsideGitDoesNothing(t *testing.T) {
	f := newFixture(t)
	f.repo = t.TempDir()

	if err := f.run(f.input("SessionStart", map[string]string{"source": "startup"}), nil); err != nil {
		t.Errorf("Run: %v", err)
	}

	testutil.AssertEntries(t, f.dataRoot)
}

func TestRunIgnoredEventsDoNotStartGit(t *testing.T) {
	f := newFixture(t)
	// git を起動できない環境。起動しようとすれば Resolve がエラーを返す。
	t.Setenv("PATH", t.TempDir())

	for _, in := range []string{
		f.input("SessionStart", map[string]string{"source": "compact"}),
		f.input("Notification", map[string]string{"notification_type": "idle_prompt"}),
		f.input("PreToolUse", nil),
	} {
		if err := f.run(in, nil); err != nil {
			t.Errorf("Run(%s): %v", in, err)
		}
	}
}

func TestRunReportsGitFailureWithEventAndSession(t *testing.T) {
	f := newFixture(t)
	t.Setenv("PATH", t.TempDir())

	err := f.run(f.input("Stop", nil), nil)

	if err == nil || !strings.HasPrefix(err.Error(), "Stop "+sessionID+": ") {
		t.Errorf("Run = %v, want an error prefixed with the event and session", err)
	}
}

func TestRunRejectsInvalidInput(t *testing.T) {
	f := newFixture(t)
	valid := map[string]any{
		"session_id":      sessionID,
		"transcript_path": "/t.jsonl",
		"cwd":             f.repo,
		"hook_event_name": "SessionStart",
		"source":          "startup",
	}
	// with は valid に overrides を重ねた入力を返す。値が nil のフィールドは消す。
	with := func(overrides map[string]any) string {
		in := maps.Clone(valid)
		for k, v := range overrides {
			if v == nil {
				delete(in, k)
			} else {
				in[k] = v
			}
		}
		data, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	for name, in := range map[string]string{
		"not JSON":                   `{"session_id":`,
		"empty input":                "",
		"session_id traversal":       with(map[string]any{"session_id": "../x"}),
		"session_id with slash":      with(map[string]any{"session_id": "a/b"}),
		"session_id with dot":        with(map[string]any{"session_id": "a.b"}),
		"session_id with glob":       with(map[string]any{"session_id": "*"}),
		"session_id with newline":    with(map[string]any{"session_id": "a\nb"}),
		"session_id not a string":    with(map[string]any{"session_id": 1}),
		"missing session_id":         with(map[string]any{"session_id": nil}),
		"missing cwd":                with(map[string]any{"cwd": nil}),
		"missing transcript_path":    with(map[string]any{"transcript_path": nil}),
		"missing hook_event_name":    with(map[string]any{"hook_event_name": nil}),
		"traversal on SessionEnd":    with(map[string]any{"session_id": "..", "hook_event_name": "SessionEnd"}),
		"traversal on unknown event": with(map[string]any{"session_id": "../x", "hook_event_name": "PreToolUse"}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := f.run(in, nil); err == nil {
				t.Error("Run succeeded, want an error")
			}
			testutil.AssertEntries(t, f.dataRoot)
		})
	}
}

func TestRunRejectsNonIntegerClaudePID(t *testing.T) {
	for _, pid := range []string{"", "abc", "12.5"} {
		t.Run(pid, func(t *testing.T) {
			f := newFixture(t)

			err := f.run(f.input("UserPromptSubmit", nil), map[string]string{"CLAUDE_PID": pid})

			if err == nil {
				t.Error("Run succeeded, want an error")
			}
			testutil.AssertNotExist(t, f.sessionFile())
		})
	}
}

// readIfExists は path の中身を返す。無ければ nil を返す。
func readIfExists(t *testing.T, path string) *string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	return &s
}

func show(s *string) string {
	if s == nil {
		return "(absent)"
	}
	return *s
}
