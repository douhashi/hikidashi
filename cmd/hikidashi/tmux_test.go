package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

// statusDrawer はデータルート dataRoot に name の引き出しを登録して返す。
func statusDrawer(t *testing.T, dataRoot, name string) drawer.Drawer {
	t.Helper()
	d := drawer.Drawer{Dir: filepath.Join(dataRoot, "drawers", name+"-0123abcd"), Path: "/src/" + name, Name: name}
	if err := d.Register(); err != nil {
		t.Fatal(err)
	}
	return d
}

// statusSession は d に、pid の claude の state のセッションを書いて返す。
func statusSession(t *testing.T, d drawer.Drawer, id string, pid int, state session.State) session.Session {
	t.Helper()
	changed := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	s := session.Session{
		SessionID: id, TmuxPane: "%" + id, ClaudePID: pid, State: state,
		TranscriptPath: filepath.Join(t.TempDir(), id+".jsonl"),
		StateChangedAt: changed, StartedAt: changed,
	}
	if err := session.Write(d.Dir, s); err != nil {
		t.Fatal(err)
	}
	return s
}

// tmuxStatus は hikidashi tmux status を args で実行し、exit 0 で stderr に何も出さなかったことを確かめて stdout を返す。
func tmuxStatus(t *testing.T, args ...string) string {
	t.Helper()
	code, stdout, stderr := invoke(commands, "", append([]string{"tmux", "status"}, args...)...)
	if code != 0 || stderr != "" {
		t.Fatalf("tmux status %q = %d, stderr %q, want 0 and silent", args, code, stderr)
	}
	return stdout
}

func TestTmuxStatusCountsEffectiveStates(t *testing.T) {
	dataRoot := isolateHome(t)
	api, web := statusDrawer(t, dataRoot, "api"), statusDrawer(t, dataRoot, "web")
	pid := testutil.StartClaude(t)
	statusSession(t, api, "a1", pid, session.Waiting)
	statusSession(t, api, "a2", pid, session.Idle)
	statusSession(t, api, "dead", testutil.DeadPID(t), session.Waiting)
	statusSession(t, web, "w1", pid, session.Waiting)
	statusSession(t, web, "w2", pid, session.Running)
	interrupted := statusSession(t, web, "w3", pid, session.Waiting)
	testutil.WriteFile(t, interrupted.TranscriptPath,
		`{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]},"timestamp":"2026-09-23T10:10:00Z"}`+"\n")

	// 死んだ claude のセッションは数えず、中断したセッションは idle として数える。
	if got, want := tmuxStatus(t), "\uf187 #[fg=#86d49a]▶1 #[fg=#ffb454]?2 #[fg=#9aa4b2]✓2#[default]\n"; got != want {
		t.Errorf("tmux status = %q, want %q", got, want)
	}
	for state, want := range map[string]string{"running": "1\n", "waiting": "2\n", "idle": "2\n"} {
		if got := tmuxStatus(t, state); got != want {
			t.Errorf("tmux status %s = %q, want %q", state, got, want)
		}
	}
	// 死んだ claude のセッションのファイルは消える。
	testutil.AssertEntries(t, filepath.Join(api.Dir, "sessions"), "a1.json", "a2.json")
}

func TestTmuxStatusOmitsStatesWithoutSessions(t *testing.T) {
	d := statusDrawer(t, isolateHome(t), "api")
	pid := testutil.StartClaude(t)
	statusSession(t, d, "w", pid, session.Waiting)
	statusSession(t, d, "i", pid, session.Idle)

	if got, want := tmuxStatus(t), "\uf187 #[fg=#ffb454]?1 #[fg=#9aa4b2]✓1#[default]\n"; got != want {
		t.Errorf("tmux status = %q, want %q", got, want)
	}
	if got := tmuxStatus(t, "running"); got != "0\n" {
		t.Errorf("tmux status running = %q, want %q", got, "0\n")
	}
}

func TestTmuxStatusPrintsNothingWithoutSessions(t *testing.T) {
	for name, seed := range map[string]func(t *testing.T, dataRoot string){
		"dead sessions only": func(t *testing.T, dataRoot string) {
			statusSession(t, statusDrawer(t, dataRoot, "api"), "dead", testutil.DeadPID(t), session.Waiting)
		},
		"no data root": func(*testing.T, string) {},
	} {
		t.Run(name, func(t *testing.T) {
			seed(t, isolateHome(t))

			if got := tmuxStatus(t); got != "" {
				t.Errorf("tmux status = %q, want empty", got)
			}
			for _, state := range []string{"running", "waiting", "idle"} {
				if got := tmuxStatus(t, state); got != "0\n" {
					t.Errorf("tmux status %s = %q, want %q", state, got, "0\n")
				}
			}
		})
	}
}

func TestTmuxStatusFailureShowsMarkOnStdout(t *testing.T) {
	t.Setenv("HOME", "")

	for _, args := range [][]string{{}, {"waiting"}} {
		code, stdout, stderr := invoke(commands, "", append([]string{"tmux", "status"}, args...)...)

		if code != 1 || stdout != "!\n" {
			t.Errorf("tmux status %q = %d, stdout %q, want 1 and %q", args, code, stdout, "!\n")
		}
		if want := "hikidashi tmux status: "; !strings.HasPrefix(stderr, want) {
			t.Errorf("tmux status %q stderr = %q, want prefix %q", args, stderr, want)
		}
	}
}

func TestTmuxStatusRejectsInvalidArguments(t *testing.T) {
	isolateHome(t)

	for _, args := range [][]string{{"x"}, {""}, {"Waiting"}, {"waiting", "idle"}} {
		code, stdout, stderr := invoke(commands, "", append([]string{"tmux", "status"}, args...)...)

		if code != 2 || stdout != "" || stderr != "Usage: hikidashi tmux status [running|waiting|idle]\n" {
			t.Errorf("tmux status %q = %d, stdout %q, stderr %q, want 2 and usage only", args, code, stdout, stderr)
		}
	}
}

func TestTmuxRequiresAKnownSubcommand(t *testing.T) {
	code, stdout, stderr := invoke(commands, "", "tmux")
	if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "Usage: hikidashi tmux <command>") {
		t.Errorf("tmux = %d, stdout %q, stderr %q, want 2 and usage", code, stdout, stderr)
	}

	code, stdout, stderr = invoke(commands, "", "tmux", "nope")
	if code != 2 || stdout != "" || !strings.HasPrefix(stderr, `hikidashi tmux: unknown command "nope"`+"\n") {
		t.Errorf("tmux nope = %d, stdout %q, stderr %q, want 2 and unknown command error", code, stdout, stderr)
	}
}

func TestTmuxHelpListsItsSubcommands(t *testing.T) {
	code, stdout, stderr := invoke(commands, "", "tmux", "help")

	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "Usage: hikidashi tmux <command>") || !strings.Contains(stdout, "\n  status  ") {
		t.Errorf("tmux help = %d, stdout %q, stderr %q, want 0 and usage listing status", code, stdout, stderr)
	}
}

func TestStatusIsNotATopLevelCommand(t *testing.T) {
	isolateHome(t)

	code, stdout, stderr := invoke(commands, "", "status")

	if code != 2 || stdout != "" || !strings.HasPrefix(stderr, `hikidashi: unknown command "status"`+"\n") {
		t.Errorf("status = %d, stdout %q, stderr %q, want 2 and unknown command error", code, stdout, stderr)
	}
}
