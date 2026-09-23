package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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

// drawerFrame は、名前が api で slug が api-0123abcd の引き出しの枠を、色なしで返す。
// 枠の幅は最も長い path の行（ASCII のパス）に合わせる。
func drawerFrame(path, issues string) string {
	width := len("path          ") + len(path)
	row := func(s string) string { return fmt.Sprintf("│ %-*s │\n", width, s) }
	return "╭─ api " + strings.Repeat("─", width-4) + "╮\n" +
		row("path          "+path) +
		row("slug          api-0123abcd") +
		row("issues        "+issues) +
		"╰" + strings.Repeat("─", width+2) + "╯\n"
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
	testutil.WriteFile(t, api.NotesPath(), "# プロジェクトメモ\n本番は触らない\n")
	env.gh.OpenIssues(t, api.Path, 3)

	// 端末でない stdout には色を付けない。
	want := drawerFrame(api.Path, "3 open") +
		"╭─ session w1  WAITING 10m  ──────╮\n" +
		"│ pane          %w1               │\n" +
		"│ summary       API を直している  │\n" +
		"│ human_next    権限を承認する    │\n" +
		"│ claude_next   -                 │\n" +
		"│ blockers      - CI が落ちている │\n" +
		"│ generated_at  -                 │\n" +
		"╰─────────────────────────────────╯\n" +
		"╭─ session i1  IDLE 5m  ──────────╮\n" +
		"│ pane          %i1               │\n" +
		"│ (next action not extracted yet) │\n" +
		"╰─────────────────────────────────╯\n" +
		"╭─ session r1  RUNNING 3h  ───────╮\n" +
		"│ pane          %r1               │\n" +
		"│ (next action not extracted yet) │\n" +
		"╰─────────────────────────────────╯\n" +
		"╭─ notes.md ─────────╮\n" +
		"│ # プロジェクトメモ │\n" +
		"│ 本番は触らない     │\n" +
		"╰────────────────────╯\n"
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

func TestShowKeepsColorsWhenForced(t *testing.T) {
	env := newShowEnv(t)
	api := env.drawer(t, "api", "api-0123abcd")
	env.session(t, api, "w1", session.Waiting, time.Now())
	env.gh.OpenIssues(t, api.Path, 1)
	// fzf のプレビューと同じく、端末でない stdout に色を強制する。
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")

	code, stdout, _ := invoke(commands, "", "show", "api")

	// どの色かは internal/show のテストが確かめる。ここでは stdout が色を落とさないことだけを見る。
	if code != 0 || !strings.Contains(stdout, "\x1b[") {
		t.Errorf("show api = %d, stdout %q, want 0 and colored", code, stdout)
	}
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
	want := drawerFrame(api.Path, "?") +
		"(no sessions)\n" +
		"╭─ notes.md ─╮\n" +
		"│ (no notes) │\n" +
		"╰────────────╯\n"
	if stdout != want {
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

func TestShowFillsFzfPreviewColumns(t *testing.T) {
	env := newShowEnv(t)
	api := env.drawer(t, "api", "api-0123abcd")
	env.session(t, api, "w1", session.Waiting, time.Now())
	testutil.WriteFile(t, filepath.Join(api.Dir, "sessions", "w1.next.json"),
		`{"summary":"`+strings.Repeat("API のテストを直している ", 4)+`"}`)
	testutil.WriteFile(t, api.NotesPath(), strings.Repeat("本番は触らない。", 10)+"\n")
	env.gh.OpenIssues(t, api.Path, 1)
	t.Setenv("FZF_PREVIEW_COLUMNS", "50")

	code, stdout, stderr := invoke(commands, "", "show", "api")

	if code != 0 || stderr != "" {
		t.Errorf("show api = %d, stderr %q, want 0 and silent", code, stderr)
	}
	// 引き出し・セッション・notes.md のどの枠も、長い値を折り返して 50 桁に収まる。
	assertFrameLines(t, "stdout", outputLines(stdout), 50, false)
}

func TestShowIgnoresUnusableFzfPreviewColumns(t *testing.T) {
	env := newShowEnv(t)
	api := env.drawer(t, "api", "api-0123abcd")
	env.gh.OpenIssues(t, api.Path, 1)
	_, want, _ := invoke(commands, "", "show", "api")

	// 正の整数でない、または値の列が 1 桁も取れない（18 - 4 - 14 = 0）なら、パイプに出したときと同じく幅の制限なしで出す。
	for _, columns := range []string{"0", "-1", "wide", "18"} {
		t.Run(columns, func(t *testing.T) {
			t.Setenv("FZF_PREVIEW_COLUMNS", columns)

			code, stdout, _ := invoke(commands, "", "show", "api")

			if code != 0 || stdout != want {
				t.Errorf("show api = %d, stdout =\n%s\nwant\n%s", code, stdout, want)
			}
		})
	}
}

// longDetailEnv は、全角の長い値を持つセッション 2 つと長い備忘録を持つ引き出し api を登録して返す。
func longDetailEnv(t *testing.T) drawer.Drawer {
	t.Helper()
	env := newShowEnv(t)
	now := time.Now()
	api := env.drawer(t, "api", "api-0123abcd")
	env.session(t, api, "w1", session.Waiting, now.Add(-10*time.Minute))
	env.session(t, api, "r1", session.Running, now.Add(-3*time.Hour))
	testutil.WriteFile(t, filepath.Join(api.Dir, "sessions", "w1.next.json"),
		`{"summary":"`+strings.Repeat("API のテストを直している ", 6)+`","human_next":"権限を承認する"}`)
	testutil.WriteFile(t, api.NotesPath(), strings.Repeat("本番は触らない。", 20)+"\n")
	env.gh.OpenIssues(t, api.Path, 1)
	return api
}

// outputLines は stdout を行に分ける。
func outputLines(stdout string) []string {
	return strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
}

// frameTitles は、lines のうち枠の上辺の行について、タイトルの最初の語を上から順に返す。
func frameTitles(lines []string) []string {
	var titles []string
	for _, l := range lines {
		if strings.HasPrefix(l, "╭─ ") {
			titles = append(titles, strings.Fields(l)[1])
		}
	}
	return titles
}

// assertFrameLines は、各行が幅 width の枠の行（枠の線で始まり、枠の線で終わる）であることを確かめる。
// blank が真なら空白だけの行も許す。
func assertFrameLines(t *testing.T, name string, lines []string, width int, blank bool) {
	t.Helper()
	for i, l := range lines {
		if blank && strings.TrimSpace(l) == "" && lipgloss.Width(l) == width {
			continue
		}
		first, _ := utf8.DecodeRuneInString(l)
		last, _ := utf8.DecodeLastRuneInString(l)
		if w := lipgloss.Width(l); w != width || !strings.ContainsRune("│╭╰", first) || !strings.ContainsRune("│╮╯", last) {
			t.Errorf("%s line %d %q is %d wide, want a %d wide frame line", name, i, l, w, width)
		}
	}
}

func TestShowPlacesSessionsBesideDrawerAndNotesWhenWide(t *testing.T) {
	for _, width := range []int{120, 140} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			api := longDetailEnv(t)
			t.Setenv("FZF_PREVIEW_COLUMNS", strconv.Itoa(width))
			// 色の制御文字が入っても、行の表示幅が width を超えないことを見る。
			t.Setenv("CLICOLOR_FORCE", "1")
			t.Setenv("NO_COLOR", "")

			code, stdout, stderr := invoke(commands, "", "show", api.Slug())

			if code != 0 || stderr != "" || !strings.Contains(stdout, "\x1b[") {
				t.Fatalf("show = %d, stderr %q, want 0, silent and colored", code, stderr)
			}
			// 列の間は 1 桁で、割り切れない 1 桁は左の列に寄せる。
			left, right := width/2, width-1-width/2
			var lefts, gaps, rights []string
			for i, l := range outputLines(stdout) {
				if w := lipgloss.Width(l); w != width {
					t.Errorf("line %d %q is %d wide, want %d", i, l, w, width)
				}
				plain := ansi.Strip(l)
				lefts = append(lefts, ansi.Cut(plain, 0, left))
				gaps = append(gaps, ansi.Cut(plain, left, left+1))
				rights = append(rights, ansi.Cut(plain, left+1, width))
			}
			// 左はセッションの枠、右は引き出しの枠と notes.md の枠で、どちらも上揃え。短い側の下は空白で埋める。
			assertFrameLines(t, "left", lefts, left, true)
			assertFrameLines(t, "right", rights, right, true)
			if got := strings.Join(gaps, ""); strings.TrimSpace(got) != "" {
				t.Errorf("gap = %q, want spaces", got)
			}
			if got, want := frameTitles(lefts), []string{"session", "session"}; !slices.Equal(got, want) {
				t.Errorf("left titles = %q, want %q", got, want)
			}
			if got, want := frameTitles(rights), []string{"api", "notes.md"}; !slices.Equal(got, want) {
				t.Errorf("right titles = %q, want %q", got, want)
			}
			if !strings.HasPrefix(lefts[0], "╭─") || !strings.HasPrefix(rights[0], "╭─") {
				t.Errorf("first line = %q | %q, want both columns to start at the top", lefts[0], rights[0])
			}
		})
	}
}

func TestShowStacksFramesWhenNarrowOrWithoutSessions(t *testing.T) {
	t.Run("119 wide", func(t *testing.T) {
		api := longDetailEnv(t)
		t.Setenv("FZF_PREVIEW_COLUMNS", "119")

		_, stdout, _ := invoke(commands, "", "show", api.Slug())

		lines := outputLines(stdout)
		assertFrameLines(t, "stacked", lines, 119, false)
		if got, want := frameTitles(lines), []string{"api", "session", "session", "notes.md"}; !slices.Equal(got, want) {
			t.Errorf("titles = %q, want %q", got, want)
		}
	})
	t.Run("120 wide without sessions", func(t *testing.T) {
		env := newShowEnv(t)
		api := env.drawer(t, "api", "api-0123abcd")
		testutil.WriteFile(t, api.NotesPath(), strings.Repeat("本番は触らない。", 20)+"\n")
		env.gh.OpenIssues(t, api.Path, 1)
		t.Setenv("FZF_PREVIEW_COLUMNS", "120")

		_, stdout, _ := invoke(commands, "", "show", api.Slug())

		lines := outputLines(stdout)
		i := slices.Index(lines, "(no sessions)")
		if i < 0 {
			t.Fatalf("stdout =\n%s\nwant (no sessions) on its own line", stdout)
		}
		assertFrameLines(t, "drawer", lines[:i], 120, false)
		assertFrameLines(t, "notes", lines[i+1:], 120, false)
		if got, want := frameTitles(lines), []string{"api", "notes.md"}; !slices.Equal(got, want) {
			t.Errorf("titles = %q, want %q", got, want)
		}
	})
}
