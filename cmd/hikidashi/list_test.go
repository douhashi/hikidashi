package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

func TestListCountsStatesAndIssuesPerDrawer(t *testing.T) {
	env := newShowEnv(t)
	now := time.Now()
	web := env.drawer(t, "web", "web-0123abcd")
	api := env.drawer(t, "api", "api-0123abcd")
	env.drawer(t, "frontend", "frontend-0123abcd")
	env.session(t, api, "r1", session.Running, now)
	env.session(t, api, "w1", session.Waiting, now)
	env.session(t, api, "i1", session.Idle, now)
	interrupt(t, env.session(t, api, "i2", session.Running, now.Add(-10*time.Minute)), now.Add(-5*time.Minute))
	env.dead(t, api, "gone")
	env.session(t, web, "w2", session.Waiting, now)
	env.gh.OpenIssues(t, api.Path, 3)
	env.gh.OpenIssues(t, web.Path, 0)

	code, stdout, stderr := invoke(commands, "", "list")

	if code != 0 {
		t.Errorf("list = %d, want 0", code)
	}
	want := "api       api-0123abcd       issues:3  running:1  waiting:1  idle:2\n" +
		"frontend  frontend-0123abcd  issues:?  running:0  waiting:0  idle:0\n" +
		"web       web-0123abcd       issues:0  running:0  waiting:1  idle:0\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if want := "hikidashi list: frontend-0123abcd: open issues unavailable: gh repo view --json issues: exit status 1: none of the git remotes"; !strings.HasPrefix(stderr, want) {
		t.Errorf("stderr = %q, want prefix %q", stderr, want)
	}
	if n := strings.Count(stderr, "\n"); n != 1 {
		t.Errorf("stderr has %d lines, want 1: %q", n, stderr)
	}
	// 死んだ claude のセッションは数えず、ファイルも消える（open / status と同じ後始末）。
	testutil.AssertEntries(t, filepath.Join(api.Dir, "sessions"), "i1.json", "i2.json", "r1.json", "w1.json")
}

func TestListWithoutDrawersSaysSo(t *testing.T) {
	env := newShowEnv(t)

	code, stdout, stderr := invoke(commands, "", "list")

	if code != 0 || stdout != "no drawers registered (run hikidashi add in a repository)\n" || stderr != "" {
		t.Errorf("list = %d, stdout %q, stderr %q, want 0 and the notice", code, stdout, stderr)
	}
	if got := env.gh.Calls(t); got != nil {
		t.Errorf("gh ran with %+v, want not run", got)
	}
}

func TestListRejectsArguments(t *testing.T) {
	isolateHome(t)

	code, stdout, stderr := invoke(commands, "", "list", "api")

	if code != 2 || stdout != "" || stderr != "Usage: hikidashi list\n" {
		t.Errorf("list api = %d, stdout %q, stderr %q, want 2 and usage", code, stdout, stderr)
	}
}

func TestListFailsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")

	code, stdout, stderr := invoke(commands, "", "list")

	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "hikidashi list: ") {
		t.Errorf("list = %d, stdout %q, stderr %q, want 1 and the reason", code, stdout, stderr)
	}
}
