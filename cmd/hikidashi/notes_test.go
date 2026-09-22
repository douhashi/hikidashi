package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/testutil"
)

// notesFixture は hikidashi notes を動かすデータルートと、偽のエディタの記録先。
type notesFixture struct {
	dataRoot string
	// record は偽のエディタが受け取った引数（1 行ずつ）と stdin を書くファイル。
	record string
}

// notesEnv はデータルートを一時ディレクトリに向け、偽のエディタを `$EDITOR`（引数 --wait 付き）に設定する。
// 偽のエディタは引数と stdin を record に書き、stdout に "edited" と出して exitCode で終わる。
func notesEnv(t *testing.T, exitCode int) notesFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$EDITOR_RECORD\"\ncat >> \"$EDITOR_RECORD\"\necho edited\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(editor, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "record")
	t.Setenv("EDITOR_RECORD", record)
	t.Setenv("EDITOR", editor+" --wait")
	return notesFixture{dataRoot: filepath.Join(home, ".hikidashi"), record: record}
}

// enterRepo は新しいリポジトリの下位ディレクトリに移り、その引き出しを返す。
func (f notesFixture) enterRepo(t *testing.T) drawer.Drawer {
	t.Helper()
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "api"))
	sub := filepath.Join(repo, "src")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	d, ok, err := drawer.Resolve(f.dataRoot, repo)
	if err != nil || !ok {
		t.Fatalf("Resolve(%q) = ok %v, err %v", repo, ok, err)
	}
	return d
}

// assertFailed は hikidashi notes が code で終わり、stderr が want を含み、エディタもデータルートも触っていないことを確かめる。
func (f notesFixture) assertFailed(t *testing.T, code int, stderr string, wantCode int, want string) {
	t.Helper()
	if code != wantCode {
		t.Errorf("exit code = %d, want %d", code, wantCode)
	}
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
	testutil.AssertNotExist(t, f.record)
	testutil.AssertNotExist(t, f.dataRoot)
}

func TestNotesCreatesNotesAndOpensEditor(t *testing.T) {
	f := notesEnv(t, 0)
	d := f.enterRepo(t)

	code, stdout, stderr := invoke(commands, "typed", "notes")

	if code != 0 || stderr != "" {
		t.Errorf("exit code, stderr = %d, %q, want 0, empty", code, stderr)
	}
	if stdout != "edited\n" {
		t.Errorf("stdout = %q, want the editor's output", stdout)
	}
	if got, want := testutil.ReadFile(t, f.record), "--wait\n"+d.NotesPath()+"\ntyped"; got != want {
		t.Errorf("editor got %q, want args and stdin %q", got, want)
	}
	if got := testutil.ReadFile(t, d.NotesPath()); got != "" {
		t.Errorf("notes.md = %q, want empty", got)
	}
	testutil.ReadFile(t, filepath.Join(d.Dir, "drawer.json"))
	for _, dir := range []string{f.dataRoot, filepath.Join(f.dataRoot, "drawers"), d.Dir} {
		testutil.AssertPerm(t, dir, 0o700)
	}
	testutil.AssertPerm(t, d.NotesPath(), 0o600)
}

func TestNotesKeepsExistingNotes(t *testing.T) {
	f := notesEnv(t, 0)
	d := f.enterRepo(t)
	testutil.WriteFile(t, d.NotesPath(), "keep me\n")

	code, _, stderr := invoke(commands, "", "notes")

	if code != 0 || stderr != "" {
		t.Errorf("exit code, stderr = %d, %q, want 0, empty", code, stderr)
	}
	if got := testutil.ReadFile(t, d.NotesPath()); got != "keep me\n" {
		t.Errorf("notes.md = %q, want it kept", got)
	}
}

func TestNotesOutsideGitFails(t *testing.T) {
	f := notesEnv(t, 0)
	t.Chdir(t.TempDir())

	code, _, stderr := invoke(commands, "", "notes")

	f.assertFailed(t, code, stderr, 1, "not in a Git repository")
}

func TestNotesWithoutEditorFails(t *testing.T) {
	for _, editor := range []string{"", " "} {
		t.Run(editor, func(t *testing.T) {
			f := notesEnv(t, 0)
			f.enterRepo(t)
			t.Setenv("EDITOR", editor)

			code, _, stderr := invoke(commands, "", "notes")

			f.assertFailed(t, code, stderr, 1, "$EDITOR is not set")
		})
	}
}

func TestNotesReportsEditorFailure(t *testing.T) {
	f := notesEnv(t, 3)
	f.enterRepo(t)

	code, _, stderr := invoke(commands, "", "notes")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "run editor: ") {
		t.Errorf("stderr = %q, want the editor failure", stderr)
	}
}

func TestNotesRejectsArguments(t *testing.T) {
	f := notesEnv(t, 0)
	f.enterRepo(t)

	code, _, stderr := invoke(commands, "", "notes", "extra")

	f.assertFailed(t, code, stderr, 2, "Usage: hikidashi notes")
}
