package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

// openEnv は hikidashi show のテストの環境（偽の gh と生きた claude）に加えて fzf と tmux を偽物に差し替え、
// tmux の中から open を起動したように整える。
// 偽の fzf は受け取った引数・標準入力・環境変数 CLICOLOR_FORCE を dir に書き残し、FAKE_FZF_SELECT を選んだ行として返して FAKE_FZF_EXIT で終わる。
// realFzf は差し替える前の PATH で見つけた本物の fzf（mise.toml が入れる版）で、見つからなければ空。
type openEnv struct {
	showEnv
	dir     string
	realFzf string
	tmux    *testutil.FakeTmux
}

func newOpenEnv(t *testing.T) openEnv {
	t.Helper()
	base := newShowEnv(t)
	realFzf, _ := exec.LookPath("fzf")
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	dir := t.TempDir()
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("FAKE_FZF_SELECT", "")
	t.Setenv("FAKE_FZF_EXIT", "0")
	// 一覧とプレビューの色の強制を確かめるため、利用者の環境の色の設定を持ち込まない。
	t.Setenv("CLICOLOR_FORCE", "")
	t.Setenv("NO_COLOR", "")
	writeFake(t, dir, "fzf", `printf '%s\n' "$@" > "$FAKE_DIR/fzf.args"
printf '%s' "$CLICOLOR_FORCE" > "$FAKE_DIR/fzf.clicolor_force"
cat > "$FAKE_DIR/fzf.stdin"
if [ "$FAKE_FZF_EXIT" != 0 ]; then exit "$FAKE_FZF_EXIT"; fi
printf '%s\n' "$FAKE_FZF_SELECT"`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return openEnv{showEnv: base, dir: dir, realFzf: realFzf, tmux: testutil.NewFakeTmux(t)}
}

func writeFake(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// fzfArgs は偽の fzf が受け取った引数を返す。fzf が起動されていなければ nil を返す。
func (e openEnv) fzfArgs(t *testing.T) []string {
	t.Helper()
	path := filepath.Join(e.dir, "fzf.args")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(testutil.ReadFile(t, path), "\n"), "\n")
}

// fzfStdin は偽の fzf が標準入力で受け取った一覧を返す。
func (e openEnv) fzfStdin(t *testing.T) string {
	t.Helper()
	return testutil.ReadFile(t, filepath.Join(e.dir, "fzf.stdin"))
}

// assertTmux は偽の tmux が want の引数でこの順に呼ばれたことを確かめる。want が無ければ呼ばれていない。
func (e openEnv) assertTmux(t *testing.T, want ...[]string) {
	t.Helper()
	if got := e.tmux.Calls(t); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("tmux calls = %q, want %q", got, want)
	}
}

// openCalls は open が d のセッションを開くときの tmux の呼び出し。セッションが無ければ作ってから、move で開く。
func openCalls(d drawer.Drawer, exists bool, move string) [][]string {
	name := d.TmuxSession()
	calls := [][]string{{"has-session", "-t", "=" + name}}
	if !exists {
		calls = append(calls, []string{"new-session", "-d", "-s", name, "-c", d.Path})
	}
	return append(calls, []string{move, "-t", "=" + name})
}

// assertOpens は open を args で実行し、exit 0 で何も出さずに want の tmux の呼び出しで開いたことを確かめる。
func (e openEnv) assertOpens(t *testing.T, want [][]string, args ...string) {
	t.Helper()
	code, stdout, stderr := invoke(commands, "", append([]string{"open"}, args...)...)

	if code != 0 || stdout != "" || stderr != "" {
		t.Errorf("open %q = %d, stdout %q, stderr %q, want 0 and silent", args, code, stdout, stderr)
	}
	e.assertTmux(t, want...)
}

// assertFails は open を args で実行し、exit 1 で stderr に want を出して tmux を呼ばなかったことを確かめる。
func (e openEnv) assertFails(t *testing.T, want string, args ...string) {
	t.Helper()
	code, stdout, stderr := invoke(commands, "", append([]string{"open"}, args...)...)

	if code != 1 || stdout != "" || stderr != want {
		t.Errorf("open %q = %d, stdout %q, stderr %q, want 1 and %q", args, code, stdout, stderr, want)
	}
	e.assertTmux(t)
}

func TestOpenNamedDrawerSwitchesInsideTmux(t *testing.T) {
	for name, exists := range map[string]bool{"existing session": true, "missing session": false} {
		t.Run(name, func(t *testing.T) {
			env := newOpenEnv(t)
			d := env.drawer(t, "api", "api-3f2a9c1b")
			if exists {
				env.tmux.AddSession(t, d.TmuxSession())
			}

			env.assertOpens(t, openCalls(d, exists, "switch-client"), "api")
		})
	}
}

func TestOpenNamedDrawerAttachesOutsideTmux(t *testing.T) {
	env := newOpenEnv(t)
	t.Setenv("TMUX", "")
	d := env.drawer(t, "api", "api-3f2a9c1b")

	env.assertOpens(t, openCalls(d, false, "attach-session"), "api")
}

func TestOpenDrawerBySlug(t *testing.T) {
	// 同名の引き出しは slug で選ぶ。セッション名は slug の . を _ に置き換えたもの（drawer.TmuxSession）になる。
	env := newOpenEnv(t)
	env.drawer(t, "example.com", "example.com-11111111")
	d := env.drawer(t, "example.com", "example.com-22222222")

	env.assertOpens(t, openCalls(d, false, "switch-client"), "example.com-22222222")
}

func TestOpenFailsForUnknownOrAmbiguousDrawer(t *testing.T) {
	env := newOpenEnv(t)
	env.drawer(t, "api", "api-11111111")
	env.drawer(t, "api", "api-22222222")

	for name, want := range map[string]string{
		"nope": "hikidashi open: no registered drawer \"nope\"; run \"hikidashi add\" in the repository to register it\n",
		"api":  "hikidashi open: \"api\" matches more than one drawer: api-11111111, api-22222222\n",
	} {
		t.Run(name, func(t *testing.T) {
			env.assertFails(t, want, name)
		})
	}
}

func TestOpenWithoutArgumentsOpensCurrentDrawer(t *testing.T) {
	for name, cwd := range map[string]func(t *testing.T, repo string) string{
		"root": func(_ *testing.T, repo string) string { return repo },
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
			env := newOpenEnv(t)
			repo, d := newRegisteredRepo(t, env.dataRoot)
			t.Chdir(cwd(t, repo))

			env.assertOpens(t, openCalls(d, false, "switch-client"))
			if got := env.fzfArgs(t); got != nil {
				t.Errorf("fzf ran with %q, want not run", got)
			}
		})
	}
}

func TestOpenWithoutArgumentsFailsInUnregisteredRepository(t *testing.T) {
	env := newOpenEnv(t)
	env.drawer(t, "web", "web-0123abcd")
	repo, _ := newRepo(t, env.dataRoot)
	t.Chdir(repo)

	env.assertFails(t, "hikidashi open: "+repo+` is not in a registered drawer; run "hikidashi add" in the repository to register it`+"\n")
	if got := env.fzfArgs(t); got != nil {
		t.Errorf("fzf ran with %q, want not run", got)
	}
}

// outsideGit は作業ディレクトリを Git 管理外にし、名前と slug の順が登録の順と異なる 3 つの引き出しを登録する。
func (e openEnv) outsideGit(t *testing.T) (front, api1, api2 drawer.Drawer) {
	t.Helper()
	t.Chdir(t.TempDir())
	front = e.drawer(t, "frontend", "0-frontend-0123abcd")
	api2 = e.drawer(t, "api", "api-22222222")
	api1 = e.drawer(t, "api", "api-11111111")
	return front, api1, api2
}

func TestOpenOutsideGitChoosesDrawerWithFzf(t *testing.T) {
	env := newOpenEnv(t)
	front, api1, api2 := env.outsideGit(t)
	testutil.WriteFile(t, api2.NotesPath(), "本番は触らない\n")
	env.session(t, api2, "w1", session.Waiting, time.Now())
	env.gh.OpenIssues(t, api1.Path, 3)
	env.gh.OpenIssues(t, api2.Path, 5)
	// 同名の引き出しは、選んだ行の slug で後の方を引く。
	t.Setenv("FAKE_FZF_SELECT", api2.Slug()+"\t│ api      │      5 │")

	// Issue の件数が得られない理由（frontend）は fzf の画面に上書きされるため出さず、表の ? だけで示す。
	env.assertOpens(t, openCalls(api2, false, "switch-client"))
	// 見出しの 3 行は slug を持たず、各行は「slug TAB list の表の行」。下の罫線は選べる行にしないため無い。
	want := "\t╭──────────┬────────┬─────────┬─────────┬──────┬────────────────╮\n" +
		"\t│ DRAWER   │ ISSUES │ RUNNING │ WAITING │ IDLE │ NOTES          │\n" +
		"\t├──────────┼────────┼─────────┼─────────┼──────┼────────────────┤\n" +
		api1.Slug() + "\t│ api      │      3 │       0 │       0 │    0 │                │\n" +
		api2.Slug() + "\t│ api      │      5 │       0 │       1 │    0 │ 本番は触らない │\n" +
		front.Slug() + "\t│ frontend │      ? │       0 │       0 │    0 │                │\n"
	if got := ansi.Strip(env.fzfStdin(t)); got != want {
		t.Errorf("fzf stdin =\n%s\nwant\n%s", got, want)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{
		"--ansi", "--header-lines=3", "--delimiter=\t|│", "--with-nth=2..", "--nth=2", "--no-sort", "--layout=reverse",
		"--with-shell=sh -c", "--preview=" + shellQuote(exe) + " show {1}", "--preview-window=down,50%",
	}
	if got := env.fzfArgs(t); !slices.Equal(got, wantArgs) {
		t.Errorf("fzf args = %q, want %q", got, wantArgs)
	}
}

func TestOpenOutsideGitFiltersByNameOnly(t *testing.T) {
	env := newOpenEnv(t)
	if env.realFzf == "" {
		t.Fatal("fzf not found in PATH; run the tests through mise (mise.toml pins fzf)")
	}
	front, api1, api2 := env.outsideGit(t)
	testutil.WriteFile(t, front.NotesPath(), "api の移行待ち\n")
	env.gh.OpenIssues(t, api1.Path, 3)
	t.Setenv("FAKE_FZF_EXIT", "130")
	env.assertOpens(t, nil)

	// 偽の fzf に渡った一覧と引数を、本物の fzf の --filter で絞り込む。件数と NOTES の文字には当たらない。
	for query, want := range map[string][]string{
		"api":    {api1.Slug(), api2.Slug()},
		"front":  {front.Slug()},
		"3":      nil,
		"移行":     nil,
		"DRAWER": nil,
	} {
		t.Run(query, func(t *testing.T) {
			cmd := exec.Command(env.realFzf, append(env.fzfArgs(t), "--filter="+query)...)
			cmd.Stdin = strings.NewReader(env.fzfStdin(t))
			out, err := cmd.Output()
			if exit, ok := errors.AsType[*exec.ExitError](err); err != nil && (!ok || exit.ExitCode() != 1) {
				t.Fatalf("fzf --filter=%s: %v", query, err)
			}
			var got []string
			for l := range strings.Lines(string(out)) {
				slug, _, _ := strings.Cut(l, "\t")
				got = append(got, slug)
			}
			if !slices.Equal(got, want) {
				t.Errorf("fzf --filter=%s matched %q, want %q", query, got, want)
			}
		})
	}
}

func TestListTableWidthFitsFzfList(t *testing.T) {
	// プレビューは一覧の下に置くため、一覧は端末の全幅になる。表の幅は、そこから左のカーソルと印の 2 桁と右のスクロールバーの 1 桁を引いたもの。
	for width, want := range map[int]int{0: 0, 80: 77, 100: 97, 200: 197} {
		if got := listTableWidth(width); got != want {
			t.Errorf("listTableWidth(%d) = %d, want %d", width, got, want)
		}
	}
}

func TestOpenOutsideGitForcesColorInListAndPreviewUnlessNoColor(t *testing.T) {
	// fzf は一覧を標準入力で、プレビューの出力をパイプで受けるため、色を強制しないと表と hikidashi show が色を落とす。
	for noColor, want := range map[string]string{"": "1", "1": ""} {
		t.Run("NO_COLOR="+noColor, func(t *testing.T) {
			env := newOpenEnv(t)
			_, api1, _ := env.outsideGit(t)
			t.Setenv("NO_COLOR", noColor)
			t.Setenv("FAKE_FZF_SELECT", api1.Slug()+"\t│ api      │")

			env.assertOpens(t, openCalls(api1, false, "switch-client"))
			if got := testutil.ReadFile(t, filepath.Join(env.dir, "fzf.clicolor_force")); got != want {
				t.Errorf("fzf CLICOLOR_FORCE = %q, want %q", got, want)
			}
			if stdin := env.fzfStdin(t); strings.Contains(stdin, "\x1b[") != (want != "") {
				t.Errorf("fzf stdin = %q, want colored %v", stdin, want != "")
			}
		})
	}
}

func TestOpenOutsideGitCancelledDoesNothing(t *testing.T) {
	// 130 は Esc / Ctrl-C、1 は一致する行が無いまま Enter したとき。
	for _, exit := range []string{"130", "1"} {
		t.Run(exit, func(t *testing.T) {
			env := newOpenEnv(t)
			env.outsideGit(t)
			t.Setenv("FAKE_FZF_EXIT", exit)

			env.assertOpens(t, nil)
		})
	}
}

func TestOpenOutsideGitFails(t *testing.T) {
	for name, tc := range map[string]struct {
		env    map[string]string
		stderr string
	}{
		"fzf fails":            {env: map[string]string{"FAKE_FZF_EXIT": "2"}, stderr: "hikidashi open: run fzf: exit status 2\n"},
		"unexpected selection": {env: map[string]string{"FAKE_FZF_SELECT": "api-99999999\tx"}, stderr: "hikidashi open: unexpected selection \"api-99999999\\tx\"\n"},
	} {
		t.Run(name, func(t *testing.T) {
			env := newOpenEnv(t)
			env.outsideGit(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			env.assertFails(t, tc.stderr)
		})
	}
}

func TestOpenOutsideGitWithoutDrawersGuidesToAdd(t *testing.T) {
	env := newOpenEnv(t)
	t.Chdir(t.TempDir())

	env.assertFails(t, "hikidashi open: no drawers registered; run \"hikidashi add\" in the repository to register it\n")
	if got := env.fzfArgs(t); got != nil {
		t.Errorf("fzf ran with %q, want not run", got)
	}
}

func TestOpenReportsTmuxFailure(t *testing.T) {
	for _, tc := range []struct {
		subcommand, tmuxEnv string
	}{
		{"has-session", "/tmp/tmux-1000/default,1234,0"},
		{"new-session", "/tmp/tmux-1000/default,1234,0"},
		{"switch-client", "/tmp/tmux-1000/default,1234,0"},
		{"attach-session", ""},
	} {
		t.Run(tc.subcommand, func(t *testing.T) {
			env := newOpenEnv(t)
			t.Setenv("TMUX", tc.tmuxEnv)
			env.drawer(t, "api", "api-3f2a9c1b")
			env.tmux.Fail(t, tc.subcommand, "boom", 2)

			code, stdout, stderr := invoke(commands, "", "open", "api")

			if want := "hikidashi open: tmux " + tc.subcommand; code != 1 || stdout != "" || !strings.HasPrefix(stderr, want) {
				t.Errorf("open = %d, stdout %q, stderr %q, want 1 and prefix %q", code, stdout, stderr, want)
			}
		})
	}
}

func TestOpenRejectsExtraArguments(t *testing.T) {
	env := newOpenEnv(t)

	code, _, stderr := invoke(commands, "", "open", "api", "web")

	if code != 2 || stderr != "Usage: hikidashi open [<drawer>]\n" {
		t.Errorf("open = %d, stderr %q, want 2 and usage", code, stderr)
	}
	env.assertTmux(t)
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
