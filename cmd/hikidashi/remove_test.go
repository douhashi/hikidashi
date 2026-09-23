package main

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/scan"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

// removeEnv は hook を tmux の中の Claude Code から起動されたように整え、tmux を偽物に差し替える。
func removeEnv(t *testing.T) (dataRoot string, fake *testutil.FakeTmux) {
	t.Helper()
	return hookEnv(t), testutil.NewFakeTmux(t)
}

// removeOutput は hikidashi remove が d について出す行。notesKept なら備忘録を残した行も続ける。
func removeOutput(d drawer.Drawer, notesKept bool) string {
	out := "drawer: " + d.Slug() + " (removed)\n"
	if notesKept {
		out += "notes: " + d.NotesPath() + " (kept)\n"
	}
	return out
}

// assertRemove は hikidashi remove を args で実行し、exit 0 で want を出し、tmux に触れていないことを確かめる。
func assertRemove(t *testing.T, fake *testutil.FakeTmux, want string, args ...string) {
	t.Helper()
	code, stdout, stderr := invoke(commands, "", append([]string{"remove"}, args...)...)

	if code != 0 || stderr != "" {
		t.Errorf("remove = %d, stderr %q, want 0 and silent", code, stderr)
	}
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if got := fake.Calls(t); got != nil {
		t.Errorf("tmux ran with %q, want not run", got)
	}
}

// snapshot は dir 配下の全ファイル・ディレクトリのパスと中身（ディレクトリは空）を返す。dir が無ければ nil を返す。
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		files[path] = ""
		if !e.IsDir() {
			files[path] = testutil.ReadFile(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// assertRemoveFails は hikidashi remove を args で実行し、exit 1 で stderr が wantErr で始まり、
// データルートと tmux が一切変わらないことを確かめる。
func assertRemoveFails(t *testing.T, dataRoot string, fake *testutil.FakeTmux, wantErr string, args ...string) {
	t.Helper()
	before := snapshot(t, dataRoot)

	code, stdout, stderr := invoke(commands, "", append([]string{"remove"}, args...)...)

	if code != 1 || stdout != "" {
		t.Errorf("remove = %d, stdout %q, want 1 and no stdout", code, stdout)
	}
	if !strings.HasPrefix(stderr, "hikidashi remove: "+wantErr) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, "hikidashi remove: "+wantErr)
	}
	if after := snapshot(t, dataRoot); !maps.Equal(after, before) {
		t.Errorf("data root = %q, want unchanged %q", after, before)
	}
	if got := fake.Calls(t); got != nil {
		t.Errorf("tmux ran with %q, want not run", got)
	}
}

func TestRemoveCurrentDrawerStopsRecordingAndListing(t *testing.T) {
	dataRoot, fake := removeEnv(t)
	repo, d := newRegisteredRepo(t, dataRoot)
	statusSession(t, d, "s1", testutil.StartClaude(t), session.Waiting)
	if _, stdout, _ := invoke(commands, "", "status"); stdout != "1\n" {
		t.Fatalf("status before remove = %q, want %q", stdout, "1\n")
	}
	sub := filepath.Join(repo, "src")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	assertRemove(t, fake, removeOutput(d, false))

	testutil.AssertEntries(t, filepath.Join(dataRoot, "drawers"))
	if code, stdout, _ := invoke(commands, "", "status"); code != 0 || stdout != "" {
		t.Errorf("status = %d, %q, want 0 and nothing waiting", code, stdout)
	}
	if entries, err := scan.Collect(dataRoot); err != nil || len(entries) != 0 {
		t.Errorf("Collect = %+v, err %v, want no sessions to open", entries, err)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
		code, stdout, _ := invoke(commands, hookInput(event, repo), "hook")
		assertSilentSuccess(t, code, stdout)
	}
	testutil.AssertEntries(t, filepath.Join(dataRoot, "drawers"))
}

func TestRemoveKeepsNotesAndReaddRestoresThem(t *testing.T) {
	dataRoot, fake := removeEnv(t)
	repo, d := newRegisteredRepo(t, dataRoot)
	testutil.WriteFile(t, filepath.Join(d.Dir, "sessions", "s1.json"), "{}")
	testutil.WriteFile(t, d.NotesPath(), "remember the staging DB\n")
	t.Chdir(repo)

	assertRemove(t, fake, removeOutput(d, true))

	testutil.AssertEntries(t, d.Dir, "notes.md")
	if got, err := drawer.List(dataRoot); err != nil || len(got) != 0 {
		t.Errorf("List = %+v, err %v, want no drawers", got, err)
	}
	code, stdout, _ := invoke(commands, hookInput("SessionStart", repo), "hook")
	assertSilentSuccess(t, code, stdout)

	if code, _, stderr := invoke(commands, "", "add"); code != 0 {
		t.Fatalf("add = %d, stderr %q, want 0", code, stderr)
	}
	_, stdout, _ = invoke(commands, hookInput("SessionStart", repo), "hook")
	if !strings.Contains(stdout, "remember the staging DB") {
		t.Errorf("hook stdout after add = %q, want the kept notes injected", stdout)
	}
}

func TestRemoveByName(t *testing.T) {
	for name, arg := range map[string]func(drawer.Drawer) string{
		"slug": drawer.Drawer.Slug,
		"name": func(d drawer.Drawer) string { return d.Name },
	} {
		t.Run(name, func(t *testing.T) {
			dataRoot, fake := removeEnv(t)
			_, d := newRegisteredRepo(t, dataRoot)
			t.Chdir(t.TempDir())

			assertRemove(t, fake, removeOutput(d, false), arg(d))

			testutil.AssertEntries(t, filepath.Join(dataRoot, "drawers"))
		})
	}
}

func TestRemoveChangesNothingWhenDrawerIsNotFound(t *testing.T) {
	t.Run("unregistered name", func(t *testing.T) {
		dataRoot, fake := removeEnv(t)
		newRegisteredRepo(t, dataRoot)

		assertRemoveFails(t, dataRoot, fake,
			`no registered drawer "web"; run "hikidashi add" in the repository to register it`, "web")
	})
	t.Run("ambiguous name", func(t *testing.T) {
		dataRoot, fake := removeEnv(t)
		_, a := newRegisteredRepo(t, dataRoot)
		_, b := newRegisteredRepo(t, dataRoot)
		slugs := []string{a.Slug(), b.Slug()}
		if slugs[0] > slugs[1] {
			slugs[0], slugs[1] = slugs[1], slugs[0]
		}

		assertRemoveFails(t, dataRoot, fake,
			`"api" matches more than one drawer: `+strings.Join(slugs, ", "), "api")
	})
	t.Run("unregistered repository", func(t *testing.T) {
		dataRoot, fake := removeEnv(t)
		newRegisteredRepo(t, dataRoot)
		repo, _ := newRepo(t, dataRoot)
		t.Chdir(repo)

		assertRemoveFails(t, dataRoot, fake, repo+` is not in a registered drawer; run "hikidashi add"`)
	})
	t.Run("outside Git", func(t *testing.T) {
		dataRoot, fake := removeEnv(t)
		newRegisteredRepo(t, dataRoot)
		cwd := t.TempDir()
		t.Chdir(cwd)

		assertRemoveFails(t, dataRoot, fake, cwd+` is not in a registered drawer; run "hikidashi add"`)
	})
}

func TestRemoveRejectsExtraArguments(t *testing.T) {
	dataRoot, fake := removeEnv(t)
	newRegisteredRepo(t, dataRoot)
	before := snapshot(t, dataRoot)

	code, _, stderr := invoke(commands, "", "remove", "api", "web")

	if code != 2 || stderr != "Usage: hikidashi remove [<drawer>]\n" {
		t.Errorf("remove = %d, stderr %q, want 2 and usage", code, stderr)
	}
	if after := snapshot(t, dataRoot); !maps.Equal(after, before) {
		t.Errorf("data root = %q, want unchanged %q", after, before)
	}
	if got := fake.Calls(t); got != nil {
		t.Errorf("tmux ran with %q, want not run", got)
	}
}
