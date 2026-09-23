package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

// showEnv はデータルートを一時ディレクトリに向け、gh を偽物に差し替える。
type showEnv struct {
	dataRoot string
	gh       *testutil.FakeGh
	pid      int
}

func newShowEnv(t *testing.T) showEnv {
	t.Helper()
	return showEnv{dataRoot: isolateHome(t), gh: testutil.NewFakeGh(t), pid: testutil.StartClaude(t)}
}

// drawer は slug の引き出しを、実在するリポジトリのルートとともに登録して返す。
func (e showEnv) drawer(t *testing.T, name, slug string) drawer.Drawer {
	t.Helper()
	d := drawer.Drawer{Dir: filepath.Join(e.dataRoot, "drawers", slug), Path: filepath.Join(t.TempDir(), name), Name: name}
	testutil.WriteFile(t, filepath.Join(d.Path, ".keep"), "")
	if err := d.Register(); err != nil {
		t.Fatal(err)
	}
	return d
}

// session は d に、生きている claude の state のセッションを changed から書いて返す。
func (e showEnv) session(t *testing.T, d drawer.Drawer, id string, state session.State, changed time.Time) session.Session {
	t.Helper()
	s := session.Session{
		SessionID: id, TmuxPane: "%" + id, ClaudePID: e.pid, State: state,
		TranscriptPath: filepath.Join(t.TempDir(), id+".jsonl"),
		StateChangedAt: changed, StartedAt: changed,
	}
	if err := session.Write(d.Dir, s); err != nil {
		t.Fatal(err)
	}
	return s
}

// dead は d に、claude のプロセスが既に無い waiting のセッションを書く。
func (e showEnv) dead(t *testing.T, d drawer.Drawer, id string) {
	t.Helper()
	s := e.session(t, d, id, session.Waiting, time.Now())
	s.ClaudePID = testutil.DeadPID(t)
	if err := session.Write(d.Dir, s); err != nil {
		t.Fatal(err)
	}
}

// interrupt は s の transcript に、at の中断の記録を書く。
func interrupt(t *testing.T, s session.Session, at time.Time) {
	t.Helper()
	testutil.WriteFile(t, s.TranscriptPath,
		`{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]},"timestamp":"`+
			at.UTC().Format(time.RFC3339)+`"}`+"\n")
}

func TestShowDetailPrintsSessionsAndNotes(t *testing.T) {
	env := newShowEnv(t)
	now := time.Now()
	api := env.drawer(t, "api", "api-0123abcd")
	env.drawer(t, "web", "web-0123abcd")
	env.session(t, api, "r1", session.Running, now.Add(-3*time.Hour))
	env.session(t, api, "w1", session.Waiting, now.Add(-10*time.Minute))
	interrupt(t, env.session(t, api, "i1", session.Running, now.Add(-10*time.Minute)), now.Add(-5*time.Minute))
	env.dead(t, api, "gone")
	testutil.WriteFile(t, filepath.Join(api.Dir, "sessions", "w1.next.json"),
		`{"summary":"API を直している","human_next":"権限を承認する","claude_next":"","blockers":["CI が落ちている"]}`)
	testutil.WriteFile(t, api.NotesPath(), "# 案件メモ\n本番は触らない\n")
	env.gh.OpenIssues(t, api.Path, 3)

	want := "api  " + api.Path + "\n" +
		"slug:   api-0123abcd\n" +
		"issues: 3\n" +
		"\n── session w1 ──\n" +
		"state:        waiting (10m)\n" +
		"pane:         %w1\n" +
		"summary:      API を直している\n" +
		"human_next:   権限を承認する\n" +
		"claude_next:  -\n" +
		"blockers:\n" +
		"  - CI が落ちている\n" +
		"generated_at: -\n" +
		"\n── session i1 ──\n" +
		"state:        idle (5m)\n" +
		"pane:         %i1\n" +
		"(next action not extracted yet)\n" +
		"\n── session r1 ──\n" +
		"state:        running (3h)\n" +
		"pane:         %r1\n" +
		"(next action not extracted yet)\n" +
		"\n── notes.md ──\n" +
		"# 案件メモ\n本番は触らない\n"
	// slug でも、一意に決まるリポジトリ名でも引ける。
	for _, key := range []string{"api-0123abcd", "api"} {
		t.Run(key, func(t *testing.T) {
			code, stdout, stderr := invoke(commands, "", "show", key)

			if code != 0 || stderr != "" {
				t.Errorf("show %s = %d, stderr %q, want 0 and silent", key, code, stderr)
			}
			if stdout != want {
				t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
			}
		})
	}
	testutil.AssertEntries(t, filepath.Join(api.Dir, "sessions"), "i1.json", "r1.json", "w1.json", "w1.next.json")
}

func TestShowWithoutArgumentsPrintsCurrentDrawer(t *testing.T) {
	for name, cwd := range map[string]func(t *testing.T, repo string) string{
		"subdirectory": func(t *testing.T, repo string) string {
			sub := filepath.Join(repo, "src")
			if err := os.MkdirAll(sub, 0o700); err != nil {
				t.Fatal(err)
			}
			return sub
		},
		"worktree": func(t *testing.T, repo string) string {
			worktree := filepath.Join(t.TempDir(), "api-wt")
			testutil.Git(t, repo, "worktree", "add", "-q", worktree)
			return worktree
		},
	} {
		t.Run(name, func(t *testing.T) {
			env := newShowEnv(t)
			repo, d := newRegisteredRepo(t, env.dataRoot)
			env.drawer(t, "web", "web-0123abcd")
			env.session(t, d, "w1", session.Waiting, time.Now().Add(-10*time.Minute))
			testutil.WriteFile(t, d.NotesPath(), "本番は触らない\n")
			env.gh.OpenIssues(t, repo, 2)
			t.Chdir(cwd(t, repo))
			_, want, _ := invoke(commands, "", "show", d.Slug())

			code, stdout, stderr := invoke(commands, "", "show")

			if code != 0 || stderr != "" {
				t.Errorf("show = %d, stderr %q, want 0 and silent", code, stderr)
			}
			if stdout != want {
				t.Errorf("stdout =\n%s\nwant the same as show %s\n%s", stdout, d.Slug(), want)
			}
		})
	}
}

func TestShowWithoutArgumentsFailsOutsideRegisteredDrawer(t *testing.T) {
	for name, tc := range map[string]struct {
		cwd  func(t *testing.T, dataRoot string) string
		want string
	}{
		"unregistered repository": {
			cwd: func(t *testing.T, dataRoot string) string {
				repo, _ := newRepo(t, dataRoot)
				return repo
			},
			want: ` is not in a registered drawer; run "hikidashi add" in the repository to register it`,
		},
		"outside Git": {
			cwd:  func(t *testing.T, _ string) string { return t.TempDir() },
			want: ` is not in a Git repository; run "hikidashi list" to see all drawers`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			env := newShowEnv(t)
			env.drawer(t, "web", "web-0123abcd")
			cwd := tc.cwd(t, env.dataRoot)
			t.Chdir(cwd)

			code, stdout, stderr := invoke(commands, "", "show")

			if want := "hikidashi show: " + cwd + tc.want + "\n"; code != 1 || stdout != "" || stderr != want {
				t.Errorf("show = %d, stdout %q, stderr %q, want 1 and %q", code, stdout, stderr, want)
			}
			if got := env.gh.Calls(t); got != nil {
				t.Errorf("gh ran with %+v, want not run", got)
			}
		})
	}
}

func TestShowDetailWithoutSessionsNotesOrIssues(t *testing.T) {
	env := newShowEnv(t)
	api := env.drawer(t, "api", "api-0123abcd")
	env.gh.Fail(t, api.Path, "HTTP 401: Bad credentials", 1)

	code, stdout, stderr := invoke(commands, "", "show", "api")

	if code != 0 {
		t.Errorf("show api = %d, want 0", code)
	}
	if want := "api  " + api.Path + "\nslug:   api-0123abcd\nissues: ?\n\n(no sessions)\n\n── notes.md ──\n(no notes)\n"; stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	if want := "hikidashi show: api-0123abcd: open issues unavailable: gh repo view --json issues: exit status 1: HTTP 401: Bad credentials\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestShowDetailFailsForUnknownOrAmbiguousDrawer(t *testing.T) {
	env := newShowEnv(t)
	env.drawer(t, "api", "api-11111111")
	env.drawer(t, "api", "api-22222222")

	for key, want := range map[string]string{
		"nope": "hikidashi show: no drawer \"nope\"\n",
		"api":  "hikidashi show: \"api\" matches more than one drawer: api-11111111, api-22222222\n",
	} {
		t.Run(key, func(t *testing.T) {
			code, stdout, stderr := invoke(commands, "", "show", key)

			if code != 1 || stdout != "" || stderr != want {
				t.Errorf("show %s = %d, stdout %q, stderr %q, want 1 and %q", key, code, stdout, stderr, want)
			}
		})
	}
	if got := env.gh.Calls(t); got != nil {
		t.Errorf("gh ran with %+v, want not run", got)
	}
}

func TestShowFailsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")

	code, stdout, stderr := invoke(commands, "", "show")

	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "hikidashi show: ") {
		t.Errorf("show = %d, stdout %q, stderr %q, want 1 and the reason", code, stdout, stderr)
	}
}

func TestShowRejectsExtraArguments(t *testing.T) {
	isolateHome(t)

	code, stdout, stderr := invoke(commands, "", "show", "a", "b")

	if code != 2 || stdout != "" || stderr != "Usage: hikidashi show [<drawer>]\n" {
		t.Errorf("show a b = %d, stdout %q, stderr %q, want 2 and usage", code, stdout, stderr)
	}
}
