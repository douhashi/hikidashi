package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

const sessionID = "4f1c-a_B"

// Claude Code の transcript のエントリを模す。形は実際の transcript（v2.1.280）から写した。
const (
	prompt     = `{"type":"user","isSidechain":false,"message":{"role":"user","content":"テストを直して"}}`
	toolUse    = `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`
	toolResult = `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`
	answer     = `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"直しました。確認してください"}]}}`
)

// conversation は prompt・toolUse・toolResult・answer の transcript から claude に渡る会話。
const conversation = "### user\nテストを直して\n\n### assistant\n直しました。確認してください\n\n"

// extracted は claude が返す構造化出力。
const extracted = `{"summary":"テストを直している","human_next":"差分を確認する","claude_next":"","blockers":["CI が落ちている"]}`

// result は claude -p --output-format json の成功時の出力を、structured を構造化出力として組み立てる。
// 形は実際の出力（v2.1.280）から写した。
func result(structured string) string {
	return `{"type":"result","subtype":"success","is_error":false,"num_turns":2,"result":"","permission_denials":[],"structured_output":` + structured + `}`
}

// fixture は登録済みの引き出しのリポジトリで、tmux の中の Claude Code から Stop で起動された extract の環境。
type fixture struct {
	dataRoot   string
	repo       string
	drawer     drawer.Drawer
	transcript string
	claude     *testutil.FakeClaude
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	testutil.IsolateGit(t)
	t.Setenv("HIKIDASHI_DISABLE", "")
	t.Setenv("TMUX_PANE", "%7")
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "api"))
	dataRoot := t.TempDir()
	d, ok, err := drawer.Resolve(dataRoot, repo)
	if err != nil || !ok {
		t.Fatalf("Resolve(%q) = ok %v, err %v", repo, ok, err)
	}
	if err := d.Register(); err != nil {
		t.Fatal(err)
	}
	f := fixture{
		dataRoot:   dataRoot,
		repo:       repo,
		drawer:     d,
		transcript: filepath.Join(t.TempDir(), sessionID+".jsonl"),
		claude:     testutil.NewFakeClaude(t),
	}
	f.writeTranscript(t, prompt, toolUse, toolResult, answer)
	err = session.Write(d.Dir, session.Session{
		SessionID:      sessionID,
		Cwd:            repo,
		TmuxPane:       "%7",
		ClaudePID:      31337,
		TranscriptPath: f.transcript,
		State:          session.Idle,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.claude.Respond(t, result(extracted), 0)
	return f
}

func (f fixture) writeTranscript(t *testing.T, lines ...string) {
	t.Helper()
	testutil.WriteFile(t, f.transcript, strings.Join(lines, "\n")+"\n")
}

// input は Stop の hook 入力。
func (f fixture) input() string {
	data, err := json.Marshal(map[string]any{
		"session_id":             sessionID,
		"transcript_path":        f.transcript,
		"cwd":                    f.repo,
		"hook_event_name":        "Stop",
		"stop_hook_active":       false,
		"last_assistant_message": "直しました。確認してください",
	})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func (f fixture) run() error {
	return Run(f.dataRoot, strings.NewReader(f.input()))
}

func (f fixture) nextFile() string {
	return filepath.Join(f.drawer.Dir, "sessions", sessionID+".next.json")
}

func (f fixture) lockFile() string {
	return filepath.Join(f.drawer.Dir, "sessions", sessionID+".extract.lock")
}

func TestRunWritesNextActionFromClaude(t *testing.T) {
	f := newFixture(t)
	before := time.Now()

	if err := f.run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, ok, err := session.ReadNext(f.drawer.Dir, sessionID)
	if err != nil || !ok {
		t.Fatalf("ReadNext = ok %v, err %v, want the extracted next action", ok, err)
	}
	if got.Summary != "テストを直している" || got.HumanNext != "差分を確認する" || got.ClaudeNext != "" ||
		!slices.Equal(got.Blockers, []string{"CI が落ちている"}) {
		t.Errorf("next = %+v, want the structured output", got)
	}
	if got.GeneratedAt.Before(before) || got.GeneratedAt.After(time.Now()) {
		t.Errorf("generated_at = %v, want the time of extraction", got.GeneratedAt)
	}
	testutil.AssertPerm(t, f.nextFile(), 0o600)
	if mark := testutil.ReadFile(t, f.lockFile()); mark != "" {
		t.Errorf("lock = %q, want no pending request", mark)
	}
}

func TestRunCallsClaudeWithoutToolsHooksOrPersistence(t *testing.T) {
	f := newFixture(t)

	if err := f.run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	calls := f.claude.Calls(t)
	if len(calls) != 1 {
		t.Fatalf("claude ran %d times, want once", len(calls))
	}
	c := calls[0]
	wantArgs := []string{
		"-p", "--model", "haiku", "--output-format", "json", "--json-schema", schema,
		"--no-session-persistence", "--tools", "", "--system-prompt", systemPrompt,
	}
	if !slices.Equal(c.Args, wantArgs) {
		t.Errorf("args = %q, want %q", c.Args, wantArgs)
	}
	if !json.Valid([]byte(schema)) || !strings.Contains(schema, `"human_next"`) {
		t.Errorf("schema = %s, want a JSON schema of the next action", schema)
	}
	if c.Env["HIKIDASHI_DISABLE"] != "1" {
		t.Errorf("HIKIDASHI_DISABLE = %q, want 1 so that the child's hooks do nothing", c.Env["HIKIDASHI_DISABLE"])
	}
	if want, err := filepath.EvalSymlinks(f.dataRoot); err != nil || c.Dir != want {
		t.Errorf("dir = %q, want the data root %q (err %v)", c.Dir, want, err)
	}
	if c.Stdin != conversation {
		t.Errorf("stdin = %q, want %q", c.Stdin, conversation)
	}
}

func TestRunSendsOnlyTheNewestConversation(t *testing.T) {
	f := newFixture(t)
	var lines []string
	var full strings.Builder
	for i := range 200 {
		text := fmt.Sprintf("メッセージ %03d %s", i, strings.Repeat("あ", 100))
		lines = append(lines, fmt.Sprintf(`{"type":"user","isSidechain":false,"message":{"role":"user","content":%q}}`, text))
		full.WriteString("### user\n" + text + "\n\n")
	}
	f.writeTranscript(t, lines...)

	if err := f.run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	sent := f.claude.Calls(t)[0].Stdin
	if len(sent) > conversationLimit || len(sent) <= conversationLimit-utf8.UTFMax {
		t.Errorf("sent %d bytes, want up to the limit %d", len(sent), conversationLimit)
	}
	if !utf8.ValidString(sent) || !strings.HasSuffix(full.String(), sent) {
		t.Errorf("sent %q..., want the valid UTF-8 tail of the conversation", sent[:min(len(sent), 40)])
	}
}

func TestRunKeepsPreviousNextActionWhenExtractionFails(t *testing.T) {
	for name, out := range map[string]struct {
		stdout string
		code   int
	}{
		"claude fails":              {"", 1},
		"not JSON":                  {"Error: not logged in", 0},
		"is_error":                  {`{"type":"result","subtype":"success","is_error":true,"result":"API Error"}`, 0},
		"not success":               {`{"type":"result","subtype":"error_max_structured_output_retries","is_error":false}`, 0},
		"no structured output":      {`{"type":"result","subtype":"success","is_error":false,"result":"done"}`, 0},
		"structured output missing": {result(`{"summary":"s","human_next":"","claude_next":""}`), 0},
		"blockers null":             {result(`{"summary":"s","human_next":"","claude_next":"","blockers":null}`), 0},
		"summary not a string":      {result(`{"summary":1,"human_next":"","claude_next":"","blockers":[]}`), 0},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			previous := `{"summary":"前回の要約"}`
			testutil.WriteFile(t, f.nextFile(), previous)
			f.claude.Respond(t, out.stdout, out.code)

			err := f.run()

			if err == nil || !strings.HasPrefix(err.Error(), sessionID+": ") {
				t.Errorf("Run = %v, want an error prefixed with the session", err)
			}
			if got := testutil.ReadFile(t, f.nextFile()); got != previous {
				t.Errorf("next = %q, want the previous %q", got, previous)
			}
		})
	}
}

func TestRunDoesNothingForSessionsItDoesNotTrack(t *testing.T) {
	for name, tc := range map[string]struct {
		env   map[string]string
		input func(t *testing.T, f fixture) string
	}{
		"disabled":     {env: map[string]string{"HIKIDASHI_DISABLE": "1"}, input: func(*testing.T, fixture) string { return "not json" }},
		"outside tmux": {env: map[string]string{"TMUX_PANE": ""}, input: func(*testing.T, fixture) string { return "not json" }},
		"outside a Git repo": {input: func(_ *testing.T, f fixture) string {
			return strings.Replace(f.input(), f.repo, filepath.Dir(f.repo), 1)
		}},
		"unregistered": {input: func(t *testing.T, f fixture) string {
			other := testutil.NewRepo(t, filepath.Join(t.TempDir(), "web"))
			return strings.Replace(f.input(), f.repo, other, 1)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			if err := Run(f.dataRoot, strings.NewReader(tc.input(t, f))); err != nil {
				t.Errorf("Run: %v", err)
			}

			if calls := f.claude.Calls(t); len(calls) != 0 {
				t.Errorf("claude ran %d times, want never", len(calls))
			}
			testutil.AssertNotExist(t, f.lockFile())
			testutil.AssertNotExist(t, f.nextFile())
			testutil.AssertEntries(t, filepath.Join(f.dataRoot, "drawers"), filepath.Base(f.drawer.Dir))
		})
	}
}

func TestRunDoesNotWriteNextActionOfEndedSession(t *testing.T) {
	f := newFixture(t)
	// 抽出の間に SessionEnd がセッションのファイルを消したことを模す。
	if err := session.Remove(f.drawer.Dir, sessionID); err != nil {
		t.Fatal(err)
	}

	if err := f.run(); err != nil {
		t.Errorf("Run: %v", err)
	}

	testutil.AssertNotExist(t, f.nextFile())
}

func TestRunSkipsTranscriptWithoutConversation(t *testing.T) {
	f := newFixture(t)
	f.writeTranscript(t, toolUse, toolResult)

	if err := f.run(); err != nil {
		t.Errorf("Run: %v", err)
	}

	if calls := f.claude.Calls(t); len(calls) != 0 {
		t.Errorf("claude ran %d times, want never", len(calls))
	}
	testutil.AssertNotExist(t, f.nextFile())
}

func TestRunFailsOnBrokenInputOrTranscript(t *testing.T) {
	for name, input := range map[string]func(f fixture) string{
		"not JSON":           func(fixture) string { return "not json" },
		"invalid session_id": func(f fixture) string { return strings.Replace(f.input(), sessionID, "../x", 1) },
		"missing transcript": func(f fixture) string {
			return strings.Replace(f.input(), f.transcript, f.transcript+".missing", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)

			if err := Run(f.dataRoot, strings.NewReader(input(f))); err == nil {
				t.Error("Run succeeded, want an error")
			}

			if calls := f.claude.Calls(t); len(calls) != 0 {
				t.Errorf("claude ran %d times, want never", len(calls))
			}
		})
	}
}

// TestRunCoalescesConcurrentRequests は、同時に起動した 3 本の extract で claude が並行せず、
// 抽出中に届いた 2 本の要求が、それらより後の transcript を読む 1 回の抽出にまとまることを確かめる。
func TestRunCoalescesConcurrentRequests(t *testing.T) {
	f := newFixture(t)
	release := f.claude.Hold(t)
	t.Cleanup(release)
	first := make(chan error, 1)
	go func() { first <- f.run() }()
	f.claude.WaitHeld(t, 1)

	later := `{"type":"user","isSidechain":false,"message":{"role":"user","content":"次はドキュメント"}}`
	f.writeTranscript(t, prompt, answer, later)
	others := make(chan error, 2)
	for range 2 {
		go func() { others <- f.run() }()
	}
	for range 2 {
		select {
		case err := <-others:
			if err != nil {
				t.Errorf("Run while extracting: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Run while extracting did not return, want it to leave the request to the holder")
		}
	}
	if n := len(f.claude.Calls(t)); n != 1 {
		t.Errorf("claude ran %d times while extracting, want once", n)
	}
	release()
	if err := <-first; err != nil {
		t.Errorf("Run: %v", err)
	}

	calls := f.claude.Calls(t)
	if len(calls) != 2 {
		t.Fatalf("claude ran %d times, want twice (the requests while extracting coalesce)", len(calls))
	}
	if !strings.Contains(calls[1].Stdin, "次はドキュメント") {
		t.Errorf("second stdin = %q, want the transcript written after the requests", calls[1].Stdin)
	}
	if f.claude.Overlapped() {
		t.Error("claude ran concurrently, want one at a time")
	}
}

func TestSummarizeStopsAtDeadline(t *testing.T) {
	f := newFixture(t)
	t.Cleanup(f.claude.Hold(t))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()

	_, err := summarize(ctx, f.dataRoot, conversation)

	if err == nil {
		t.Error("summarize succeeded, want the deadline error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("summarize took %v, want it to stop at the deadline", elapsed)
	}
}

func TestRunLeavesNoTemporaryFiles(t *testing.T) {
	f := newFixture(t)

	if err := f.run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	testutil.AssertEntries(t, filepath.Dir(f.nextFile()), sessionID+".extract.lock", sessionID+".json", sessionID+".next.json")
}
