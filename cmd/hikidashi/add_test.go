package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/testutil"
)

// addEnv はデータルートを一時ディレクトリに向け、tmux を偽物に差し替える。
func addEnv(t *testing.T) (dataRoot string, fake *testutil.FakeTmux) {
	t.Helper()
	return isolateHome(t), testutil.NewFakeTmux(t)
}

// addOutput は hikidashi add が d について出す 2 行。
func addOutput(d drawer.Drawer, drawerStatus, sessionStatus string) string {
	return "drawer: " + d.Slug() + " (" + drawerStatus + ")\ntmux session: " + d.TmuxSession() + " (" + sessionStatus + ")\n"
}

// hasSessionCall と newSessionCall は hikidashi add が d について tmux に渡す引数。
func hasSessionCall(d drawer.Drawer) []string {
	return []string{"has-session", "-t", "=" + d.TmuxSession()}
}

func newSessionCall(d drawer.Drawer) []string {
	return []string{"new-session", "-d", "-s", d.TmuxSession(), "-c", d.Path}
}

// assertAdd は hikidashi add を実行し、exit 0 で want を出し、それまでの tmux の呼び出しが calls で、
// d が登録済みになったことを確かめる。
func assertAdd(t *testing.T, fake *testutil.FakeTmux, dataRoot string, d drawer.Drawer, want string, calls ...[]string) {
	t.Helper()
	code, stdout, stderr := invoke(commands, "", "add")

	if code != 0 || stderr != "" {
		t.Errorf("add = %d, stderr %q, want 0 and silent", code, stderr)
	}
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if got := fake.Calls(t); !slices.EqualFunc(got, calls, slices.Equal) {
		t.Errorf("tmux calls = %q, want %q", got, calls)
	}
	got, ok, err := drawer.Lookup(dataRoot, d.Path)
	if err != nil || !ok || got.Dir != d.Dir {
		t.Errorf("Lookup = %+v, ok %v, err %v, want %s registered", got, ok, err, d.Dir)
	}
}

func TestAddRegistersDrawerAndCreatesSessionAtRoot(t *testing.T) {
	dataRoot, fake := addEnv(t)
	repo, d := newRepo(t, dataRoot)
	sub := filepath.Join(repo, "src")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	assertAdd(t, fake, dataRoot, d,
		addOutput(d, "registered", "created"), hasSessionCall(d), newSessionCall(d))
	for _, dir := range []string{dataRoot, filepath.Join(dataRoot, "drawers"), d.Dir} {
		testutil.AssertPerm(t, dir, 0o700)
	}
	testutil.AssertPerm(t, filepath.Join(d.Dir, "drawer.json"), 0o600)
}

func TestAddFromWorktreeRegistersMainRepository(t *testing.T) {
	dataRoot, fake := addEnv(t)
	repo, d := newRepo(t, dataRoot)
	worktree := filepath.Join(t.TempDir(), "api-wt")
	testutil.Git(t, repo, "worktree", "add", "-q", worktree)
	t.Chdir(worktree)

	assertAdd(t, fake, dataRoot, d,
		addOutput(d, "registered", "created"), hasSessionCall(d), newSessionCall(d))
}

func TestAddTwiceKeepsDrawerAndSession(t *testing.T) {
	dataRoot, fake := addEnv(t)
	repo, d := newRepo(t, dataRoot)
	t.Chdir(repo)
	invoke(commands, "", "add")
	drawerJSON := testutil.ReadFile(t, filepath.Join(d.Dir, "drawer.json"))

	assertAdd(t, fake, dataRoot, d,
		addOutput(d, "already registered", "already exists"),
		hasSessionCall(d), newSessionCall(d), hasSessionCall(d))
	if got := testutil.ReadFile(t, filepath.Join(d.Dir, "drawer.json")); got != drawerJSON {
		t.Errorf("drawer.json = %q, want it kept as %q", got, drawerJSON)
	}
}

func TestAddCreatesOnlyWhatIsMissing(t *testing.T) {
	t.Run("drawer only", func(t *testing.T) {
		dataRoot, fake := addEnv(t)
		repo, d := newRegisteredRepo(t, dataRoot)
		t.Chdir(repo)

		assertAdd(t, fake, dataRoot, d,
			addOutput(d, "already registered", "created"), hasSessionCall(d), newSessionCall(d))
	})
	t.Run("session only", func(t *testing.T) {
		dataRoot, fake := addEnv(t)
		repo, d := newRepo(t, dataRoot)
		fake.AddSession(t, d.TmuxSession())
		t.Chdir(repo)

		assertAdd(t, fake, dataRoot, d,
			addOutput(d, "registered", "already exists"), hasSessionCall(d))
	})
}

func TestAddOutsideGitCreatesNothing(t *testing.T) {
	dataRoot, fake := addEnv(t)
	cwd := t.TempDir()
	t.Chdir(cwd)

	code, stdout, stderr := invoke(commands, "", "add")

	if code != 1 || stdout != "" {
		t.Errorf("add = %d, stdout %q, want 1 and no stdout", code, stdout)
	}
	if want := "hikidashi add: " + cwd + " is not in a Git repository\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if got := fake.Calls(t); got != nil {
		t.Errorf("tmux ran with %q, want not run", got)
	}
	testutil.AssertNotExist(t, dataRoot)
}

func TestAddReportsTmuxFailure(t *testing.T) {
	for name, code := range map[string]int{"has-session": 2, "new-session": 1} {
		t.Run(name, func(t *testing.T) {
			dataRoot, fake := addEnv(t)
			repo, _ := newRepo(t, dataRoot)
			t.Chdir(repo)
			fake.Fail(t, name, "boom", code)

			got, _, stderr := invoke(commands, "", "add")

			if got != 1 {
				t.Errorf("exit code = %d, want 1", got)
			}
			if want := "hikidashi add: tmux " + name; !strings.HasPrefix(stderr, want) || !strings.HasSuffix(stderr, ": boom\n") {
				t.Errorf("stderr = %q, want %q ... boom", stderr, want)
			}
		})
	}
}

func TestAddRejectsArguments(t *testing.T) {
	dataRoot, fake := addEnv(t)

	code, _, stderr := invoke(commands, "", "add", "extra")

	if code != 2 || stderr != "Usage: hikidashi add\n" {
		t.Errorf("add = %d, stderr %q, want 2 and usage", code, stderr)
	}
	if got := fake.Calls(t); got != nil {
		t.Errorf("tmux ran with %q, want not run", got)
	}
	testutil.AssertNotExist(t, dataRoot)
}
