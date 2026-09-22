package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

// openEnv は fzf と tmux を偽物に差し替え、tmux の中から open を起動したように整える。
// 偽物は受け取った引数と標準入力を dir に書き残す。偽の fzf は FAKE_FZF_SELECT を選んだ行として返し、
// FAKE_FZF_EXIT で終わる。偽の tmux は FAKE_TMUX_EXIT で終わる。
type openEnv struct {
	dir      string
	dataRoot string
}

func newOpenEnv(t *testing.T) openEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	dir := t.TempDir()
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("FAKE_FZF_SELECT", "")
	t.Setenv("FAKE_FZF_EXIT", "0")
	t.Setenv("FAKE_TMUX_EXIT", "0")
	writeFake(t, dir, "fzf", `printf '%s\n' "$@" > "$FAKE_DIR/fzf.args"
cat > "$FAKE_DIR/fzf.stdin"
if [ "$FAKE_FZF_EXIT" != 0 ]; then exit "$FAKE_FZF_EXIT"; fi
printf '%s\n' "$FAKE_FZF_SELECT"`)
	writeFake(t, dir, "tmux", `printf '%s\n' "$@" > "$FAKE_DIR/tmux.args"
if [ "$FAKE_TMUX_EXIT" != 0 ]; then echo "can't find pane: %9" >&2; exit "$FAKE_TMUX_EXIT"; fi`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return openEnv{dir: dir, dataRoot: filepath.Join(home, ".hikidashi")}
}

func writeFake(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// args は偽の name が受け取った引数を返す。起動されていなければ nil を返す。
func (e openEnv) args(t *testing.T, name string) []string {
	t.Helper()
	path := filepath.Join(e.dir, name+".args")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(testutil.ReadFile(t, path), "\n"), "\n")
}

// seed は api の引き出しに、生きている claude の waiting と idle のセッションを書く。
func (e openEnv) seed(t *testing.T) drawer.Drawer {
	t.Helper()
	d := drawer.Drawer{Dir: filepath.Join(e.dataRoot, "drawers", "api-3f2a9c1b"), Path: "/src/api", Name: "api"}
	if err := d.Register(); err != nil {
		t.Fatal(err)
	}
	pid := testutil.StartClaude(t)
	changed := time.Now().Add(-10 * time.Minute)
	for id, state := range map[string]session.State{"s1": session.Idle, "s2": session.Waiting} {
		err := session.Write(d.Dir, session.Session{
			SessionID: id, TmuxPane: "%" + id[1:], ClaudePID: pid, State: state,
			StateChangedAt: changed, StartedAt: changed,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestOpenSwitchesToSelectedPane(t *testing.T) {
	env := newOpenEnv(t)
	env.seed(t)
	t.Setenv("FAKE_FZF_SELECT", "api-3f2a9c1b/s1\tapi  idle     10m  -")

	code, stdout, stderr := invoke(commands, "", "open")

	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("open = %d, stdout %q, stderr %q, want 0 and silent", code, stdout, stderr)
	}
	if got, want := env.args(t, "tmux"), []string{"switch-client", "-t", "%1"}; !slices.Equal(got, want) {
		t.Errorf("tmux args = %q, want %q", got, want)
	}
	stdin := testutil.ReadFile(t, filepath.Join(env.dir, "fzf.stdin"))
	if want := "api-3f2a9c1b/s2\tapi  waiting  10m  -\napi-3f2a9c1b/s1\tapi  idle     10m  -\n"; stdin != want {
		t.Errorf("fzf stdin = %q, want %q", stdin, want)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--delimiter=\t", "--with-nth=2..", "--no-sort", "--layout=reverse", "--with-shell=sh -c",
		"--preview=" + shellQuote(exe) + " open --preview {1}",
	}
	if got := env.args(t, "fzf"); !slices.Equal(got, want) {
		t.Errorf("fzf args = %q, want %q", got, want)
	}
}

func TestOpenCancelledDoesNotSwitch(t *testing.T) {
	// 130 は Esc / Ctrl-C、1 は一致する行が無いまま Enter したとき。
	for _, exit := range []string{"130", "1"} {
		t.Run(exit, func(t *testing.T) {
			env := newOpenEnv(t)
			env.seed(t)
			t.Setenv("FAKE_FZF_EXIT", exit)

			code, _, stderr := invoke(commands, "", "open")

			if code != 0 || stderr != "" {
				t.Errorf("open = %d, stderr %q, want 0 and silent", code, stderr)
			}
			if got := env.args(t, "tmux"); got != nil {
				t.Errorf("tmux ran with %q, want not run", got)
			}
		})
	}
}

func TestOpenFails(t *testing.T) {
	for name, tc := range map[string]struct {
		env    map[string]string
		stderr string
	}{
		"outside tmux":         {map[string]string{"TMUX": ""}, "hikidashi open: not inside tmux"},
		"fzf fails":            {map[string]string{"FAKE_FZF_EXIT": "2"}, "hikidashi open: run fzf: exit status 2\n"},
		"tmux fails":           {map[string]string{"FAKE_FZF_SELECT": "api-3f2a9c1b/s1\tapi", "FAKE_TMUX_EXIT": "1"}, "hikidashi open: tmux switch-client -t %1: exit status 1: can't find pane: %9\n"},
		"unexpected selection": {map[string]string{"FAKE_FZF_SELECT": "api-3f2a9c1b/s9\tx"}, "hikidashi open: unexpected selection \"api-3f2a9c1b/s9\\tx\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			env := newOpenEnv(t)
			env.seed(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			code, stdout, stderr := invoke(commands, "", "open")

			if code != 1 || stdout != "" {
				t.Errorf("open = %d, stdout %q, want 1 and no stdout", code, stdout)
			}
			if !strings.HasPrefix(stderr, tc.stderr) {
				t.Errorf("stderr = %q, want prefix %q", stderr, tc.stderr)
			}
		})
	}
}

func TestOpenPreviewPrintsNextActionAndNotes(t *testing.T) {
	env := newOpenEnv(t)
	d := env.seed(t)
	testutil.WriteFile(t, d.NotesPath(), "本番は触らない\n")

	code, stdout, stderr := invoke(commands, "", "open", "--preview", "api-3f2a9c1b/s1")

	if code != 0 || stderr != "" {
		t.Fatalf("open --preview = %d, stderr %q, want 0", code, stderr)
	}
	if want := "api  /src/api\n\n(next action not extracted yet)\n\n── notes.md ──\n本番は触らない\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestOpenPreviewRejectsInvalidKey(t *testing.T) {
	env := newOpenEnv(t)
	env.seed(t)

	code, stdout, stderr := invoke(commands, "", "open", "--preview", "../../etc/passwd")

	if code != 1 || stdout != "" {
		t.Errorf("open --preview = %d, stdout %q, want 1 and no stdout", code, stdout)
	}
	if want := "hikidashi open: invalid key \"../../etc/passwd\"\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestOpenRejectsUnknownArguments(t *testing.T) {
	for _, args := range [][]string{{"x"}, {"--preview"}, {"--preview", "a/b", "c"}} {
		code, _, stderr := invoke(commands, "", append([]string{"open"}, args...)...)

		if code != 2 || !strings.HasPrefix(stderr, "Usage: hikidashi open") {
			t.Errorf("open %q = %d, stderr %q, want 2 and usage", args, code, stderr)
		}
	}
}

func TestShellQuoteSurvivesShell(t *testing.T) {
	for _, s := range []string{"/usr/local/bin/hikidashi", "/path with space/it's $HOME `x`"} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(s)).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != s {
			t.Errorf("sh printed %q, want %q", out, s)
		}
	}
}
