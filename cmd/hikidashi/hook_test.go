package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/testutil"
)

// hookEnv は hook を tmux の中の Claude Code から起動されたように整え、データルートを返す。
func hookEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HIKIDASHI_DISABLE", "")
	t.Setenv("TMUX_PANE", "%3")
	t.Setenv("CLAUDE_PID", "4242")
	return filepath.Join(home, ".hikidashi")
}

func hookInput(event, cwd string) string {
	return fmt.Sprintf(`{"session_id":"s1","transcript_path":"/t.jsonl","cwd":%q,"hook_event_name":%q,"source":"startup"}`, cwd, event)
}

// assertSilentSuccess は hook が exit 0 で終わり、stdout に何も出していないことを確かめる。
func assertSilentSuccess(t *testing.T, code int, stdout string) {
	t.Helper()
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

// assertLog はログの各行が `<RFC3339> <name>: <message>` で、message が wants に順に一致することを確かめる。
func assertLog(t *testing.T, dataRoot, name string, wants ...*regexp.Regexp) {
	t.Helper()
	file := filepath.Join(dataRoot, "hikidashi.log")
	lines := strings.SplitAfter(testutil.ReadFile(t, file), "\n")
	if last := lines[len(lines)-1]; last != "" {
		t.Errorf("log ends with %q, want a newline", last)
	}
	lines = lines[:len(lines)-1]
	if len(lines) != len(wants) {
		t.Fatalf("log = %q, want %d lines", lines, len(wants))
	}
	prefix := `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2}) ` + name + `: `
	for i, want := range wants {
		if !regexp.MustCompile(prefix + want.String() + "\n$").MatchString(lines[i]) {
			t.Errorf("log line %d = %q, want %s", i, lines[i], want)
		}
	}
	testutil.AssertPerm(t, dataRoot, 0o700)
	testutil.AssertPerm(t, file, 0o600)
}

func TestHookRecordsSessionSilently(t *testing.T) {
	dataRoot := hookEnv(t)
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "api"))

	code, stdout, stderr := invoke(commands, hookInput("SessionStart", repo), "hook")

	assertSilentSuccess(t, code, stdout)
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	d, _, err := drawer.Resolve(dataRoot, repo)
	if err != nil {
		t.Fatal(err)
	}
	testutil.ReadFile(t, filepath.Join(d.Dir, "sessions", "s1.json"))
	testutil.AssertNotExist(t, filepath.Join(dataRoot, "hikidashi.log"))
}

func TestHookWritesNotesToStdout(t *testing.T) {
	dataRoot := hookEnv(t)
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "api"))
	d, _, err := drawer.Resolve(dataRoot, repo)
	if err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, d.NotesPath(), "remember the staging DB\n")

	code, stdout, stderr := invoke(commands, hookInput("SessionStart", repo), "hook")

	if code != 0 || stderr != "" {
		t.Errorf("exit code, stderr = %d, %q, want 0, empty", code, stderr)
	}
	if !strings.HasPrefix(stdout, `{"hookSpecificOutput":`) || !strings.Contains(stdout, "remember the staging DB") {
		t.Errorf("stdout = %q, want the notes as hook output JSON", stdout)
	}
}

func TestHookLogsInvalidInputAndExitsZero(t *testing.T) {
	dataRoot := hookEnv(t)

	for _, in := range []string{"not json", `{"session_id":"../x","transcript_path":"/t","cwd":"/","hook_event_name":"Stop"}`} {
		code, stdout, _ := invoke(commands, in, "hook")

		assertSilentSuccess(t, code, stdout)
	}

	assertLog(t, dataRoot, "hook",
		regexp.MustCompile(`decode input: .+`),
		regexp.MustCompile(regexp.QuoteMeta(`invalid session_id "../x"`)),
	)
}

func TestHookLogsEventAndSessionOfFailure(t *testing.T) {
	dataRoot := hookEnv(t)
	t.Setenv("PATH", t.TempDir())

	code, stdout, _ := invoke(commands, hookInput("Stop", t.TempDir()), "hook")

	assertSilentSuccess(t, code, stdout)
	assertLog(t, dataRoot, "hook", regexp.MustCompile(`Stop s1: run git: .+`))
}

func TestHookRecoversPanic(t *testing.T) {
	dataRoot := hookEnv(t)
	var out, errOut strings.Builder

	code := run(commands, []string{"hook"}, panicReader{}, &out, &errOut)

	assertSilentSuccess(t, code, out.String())
	assertLog(t, dataRoot, "hook", regexp.MustCompile(`panic: stdin exploded`))
}

// panicReader は読まれると panic する stdin。hook の内側で起きた panic を模す。
type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("stdin exploded") }

func TestHookReportsToStderrWhenLogIsUnavailable(t *testing.T) {
	hookEnv(t)
	t.Setenv("HOME", "")

	code, stdout, stderr := invoke(commands, "not json", "hook")

	assertSilentSuccess(t, code, stdout)
	if !strings.Contains(stderr, " hook: ") || !strings.Contains(stderr, "write log: ") {
		t.Errorf("stderr = %q, want the error and why the log was not written", stderr)
	}
}

func TestHookDisabledDoesNothing(t *testing.T) {
	dataRoot := hookEnv(t)
	t.Setenv("HIKIDASHI_DISABLE", "1")

	code, stdout, stderr := invoke(commands, "not json", "hook")

	assertSilentSuccess(t, code, stdout)
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	testutil.AssertNotExist(t, dataRoot)
}
