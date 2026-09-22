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

func TestStatusCountsEffectivelyWaitingSessions(t *testing.T) {
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

	code, stdout, stderr := invoke(commands, "", "status")

	if code != 0 || stdout != "2\n" || stderr != "" {
		t.Errorf("status = %d, stdout %q, stderr %q, want 0 and %q", code, stdout, stderr, "2\n")
	}
	// 死んだ claude のセッションは数えず、ファイルも消える。
	testutil.AssertEntries(t, filepath.Join(api.Dir, "sessions"), "a1.json", "a2.json")
}

func TestStatusPrintsNothingWithoutWaitingSessions(t *testing.T) {
	for name, seed := range map[string]func(t *testing.T, dataRoot string){
		"no waiting": func(t *testing.T, dataRoot string) {
			d := statusDrawer(t, dataRoot, "api")
			pid := testutil.StartClaude(t)
			statusSession(t, d, "i", pid, session.Idle)
			statusSession(t, d, "r", pid, session.Running)
		},
		"no data root": func(*testing.T, string) {},
	} {
		t.Run(name, func(t *testing.T) {
			seed(t, isolateHome(t))

			code, stdout, stderr := invoke(commands, "", "status")

			if code != 0 || stdout != "" || stderr != "" {
				t.Errorf("status = %d, stdout %q, stderr %q, want 0 and silent", code, stdout, stderr)
			}
		})
	}
}

func TestStatusFailureShowsMarkOnStdout(t *testing.T) {
	t.Setenv("HOME", "")

	code, stdout, stderr := invoke(commands, "", "status")

	if code != 1 || stdout != "!\n" {
		t.Errorf("status = %d, stdout %q, want 1 and %q", code, stdout, "!\n")
	}
	if want := "hikidashi status: "; !strings.HasPrefix(stderr, want) {
		t.Errorf("stderr = %q, want prefix %q", stderr, want)
	}
}

func TestStatusRejectsArguments(t *testing.T) {
	isolateHome(t)

	code, stdout, stderr := invoke(commands, "", "status", "x")

	if code != 2 || stdout != "" || stderr != "Usage: hikidashi status\n" {
		t.Errorf("status x = %d, stdout %q, stderr %q, want 2 and usage only", code, stdout, stderr)
	}
}
