package show

import (
	"errors"
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/scan"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

func TestTableAlignsColumnsAndMarksUnknownIssues(t *testing.T) {
	summaries := []Summary{
		{Drawer: drawer.Drawer{Dir: "/data/drawers/api-3f2a9c1b", Name: "api"}, Issues: Issues{Count: 12}, Running: 1, Waiting: 2, Idle: 3, Note: "# 方針"},
		{Drawer: drawer.Drawer{Dir: "/data/drawers/api-7e6d5c4b", Name: "api"}, Issues: Issues{Count: 4}},
		{Drawer: drawer.Drawer{Dir: "/data/drawers/frontend-0a1b2c3d", Name: "frontend"}, Issues: Issues{Err: errors.New("no remote")}, Note: "- fix CI"},
		{Drawer: drawer.Drawer{Dir: "/data/drawers/x-00000000", Name: "x\x1b[31m"}},
	}

	got := ansi.Strip(Table(summaries, 0))

	// 同名の引き出しも 1 引き出し 1 行で出し、slug の列は持たない（見分けは hikidashi show の候補で行う）。
	// 幅が分からない（0）ときは NOTES を切り詰めない。
	want := "╭──────────┬────────┬─────────┬─────────┬──────┬──────────╮\n" +
		"│ DRAWER   │ ISSUES │ RUNNING │ WAITING │ IDLE │ NOTES    │\n" +
		"├──────────┼────────┼─────────┼─────────┼──────┼──────────┤\n" +
		"│ api      │     12 │       1 │       2 │    3 │ # 方針   │\n" +
		"│ api      │      4 │       0 │       0 │    0 │          │\n" +
		"│ frontend │      ? │       0 │       0 │    0 │ - fix CI │\n" +
		"│ x [31m   │      0 │       0 │       0 │    0 │          │\n" +
		"╰──────────┴────────┴─────────┴─────────┴──────┴──────────╯"
	if got != want {
		t.Errorf("Table =\n%s\nwant\n%s", got, want)
	}
}

func TestTableTruncatesNotesToWidth(t *testing.T) {
	var summaries []Summary
	for _, note := range []string{"0123456789abcdef", "本番は触らない", "ok"} {
		summaries = append(summaries, Summary{Drawer: drawer.Drawer{Dir: "/data/drawers/api-3f2a9c1b", Name: "api"}, Note: note})
	}

	got := ansi.Strip(Table(summaries, 60))

	// 全角の行も表示幅で切り詰め、全行が揃って 60 桁に収まる。
	want := "╭────────┬────────┬─────────┬─────────┬──────┬─────────────╮\n" +
		"│ DRAWER │ ISSUES │ RUNNING │ WAITING │ IDLE │ NOTES       │\n" +
		"├────────┼────────┼─────────┼─────────┼──────┼─────────────┤\n" +
		"│ api    │      0 │       0 │       0 │    0 │ 0123456789… │\n" +
		"│ api    │      0 │       0 │       0 │    0 │ 本番は触ら… │\n" +
		"│ api    │      0 │       0 │       0 │    0 │ ok          │\n" +
		"╰────────┴────────┴─────────┴─────────┴──────┴─────────────╯"
	if got != want {
		t.Errorf("Table =\n%s\nwant\n%s", got, want)
	}
	for _, width := range []int{60, 61, 80} {
		lines := strings.Split(Table(summaries, width), "\n")
		top := lipgloss.Width(lines[0])
		for i, l := range lines {
			if w := lipgloss.Width(l); w != top || w > width {
				t.Errorf("width %d: line %d is %d columns (top %d): %q", width, i, w, top, l)
			}
		}
	}
}

func TestTableKeepsNotesColumnWhenNarrow(t *testing.T) {
	summaries := []Summary{{Drawer: drawer.Drawer{Dir: "/data/drawers/api-3f2a9c1b", Name: "api"}, Note: "0123456789"}}

	got := ansi.Strip(Table(summaries, 20))

	// 残りの桁が NOTES の見出しより狭くても、見出しの幅までは出す。
	if want := "│ api    │      0 │       0 │       0 │    0 │ 0123… │"; !strings.Contains(got, want) {
		t.Errorf("Table =\n%s\nwant a row %q", got, want)
	}
}

func TestTableLinesSplitHeaderAndRowsWithoutOuterBorder(t *testing.T) {
	summaries := []Summary{
		{Drawer: drawer.Drawer{Dir: "/data/drawers/api-3f2a9c1b", Name: "api"}, Issues: Issues{Count: 12}, Running: 1, Note: "0123456789abcdef"},
		{Drawer: drawer.Drawer{Dir: "/data/drawers/frontend-0a1b2c3d", Name: "frontend"}, Waiting: 2},
	}

	header, rows := TableLines(summaries, 60)

	// 外枠は無く、選べない見出しの 2 行（見出し・区切り線）と、summaries の順の 1 引き出し 1 行に分かれる。
	// 外枠が無い分、NOTES の列は Table より 2 桁広く、幅いっぱいまで使う。
	got := make([]string, 0, len(header)+len(rows))
	for _, l := range append(slices.Clone(header), rows...) {
		got = append(got, ansi.Strip(l))
	}
	want := []string{
		" DRAWER   │ ISSUES │ RUNNING │ WAITING │ IDLE │ NOTES       ",
		"──────────┼────────┼─────────┼─────────┼──────┼─────────────",
		" api      │     12 │       1 │       0 │    0 │ 0123456789… ",
		" frontend │      0 │       0 │       2 │    0 │             ",
	}
	if !slices.Equal(got, want) {
		t.Errorf("TableLines =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(header) != 2 {
		t.Errorf("header has %d lines, want 2", len(header))
	}
	for i, l := range got {
		if strings.ContainsAny(l, "╭╮╰╯├┤") || strings.HasPrefix(l, "│") || strings.HasSuffix(l, "│") {
			t.Errorf("line %d has an outer border: %q", i, l)
		}
		if i == 1 {
			if strings.Trim(l, "─┼") != "" {
				t.Errorf("header separator = %q, want only ─ and ┼", l)
			}
		} else if !strings.Contains(l, "│") {
			t.Errorf("line %d has no column separators: %q", i, l)
		}
	}
}

func TestTableKeepsOuterBorder(t *testing.T) {
	summaries := []Summary{{Drawer: drawer.Drawer{Dir: "/data/drawers/api-3f2a9c1b", Name: "api"}}}

	lines := strings.Split(ansi.Strip(Table(summaries, 60)), "\n")

	// hikidashi list の表は外枠を持つ。
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasPrefix(lines[len(lines)-1], "╰") {
		t.Errorf("Table =\n%s\nwant top and bottom borders", strings.Join(lines, "\n"))
	}
	for i, l := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(l, "│") && !strings.HasPrefix(l, "├") || !strings.HasSuffix(l, "│") && !strings.HasSuffix(l, "┤") {
			t.Errorf("line %d has no side borders: %q", i+1, l)
		}
	}
}

func TestFirstLineIsTheFirstNonBlankLine(t *testing.T) {
	for notes, want := range map[string]string{
		"":                      "",
		" \n\t\n":               "",
		"\n  \n  # 見出し  \n本文\n": "# 見出し",
		"- foo\n- bar\n":        "- foo",
		" \r\n x\r\n":           "x",
		"a\x1b[31mb\n":          "a [31mb",
	} {
		if got := firstLine(notes); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", notes, got, want)
		}
	}
}

func TestSummariesReadTheFirstLineOfNotes(t *testing.T) {
	testutil.NewFakeGh(t)
	dataRoot := t.TempDir()
	for name, notes := range map[string]string{"a": "\n- 本番は触らない\n2 行目\n", "b": " \n\t\n"} {
		d := drawer.Drawer{Dir: filepath.Join(dataRoot, "drawers", name+"-0123abcd"), Path: t.TempDir(), Name: name}
		if err := d.Register(); err != nil {
			t.Fatal(err)
		}
		testutil.WriteFile(t, d.NotesPath(), notes)
	}
	d := drawer.Drawer{Dir: filepath.Join(dataRoot, "drawers", "c-0123abcd"), Path: t.TempDir(), Name: "c"}
	if err := d.Register(); err != nil {
		t.Fatal(err)
	}

	got, err := Summaries(dataRoot)

	if err != nil {
		t.Fatalf("Summaries: %v", err)
	}
	var notes []string
	for _, s := range got {
		notes = append(notes, s.Note)
	}
	if want := []string{"- 本番は触らない", "", ""}; !slices.Equal(notes, want) {
		t.Errorf("notes = %q, want %q", notes, want)
	}
}

func TestSummariesAreInNameOrder(t *testing.T) {
	testutil.NewFakeGh(t)
	dataRoot := t.TempDir()
	// ディレクトリ名の順（a+-… が a-… より前）と名前の順（a が a+ より前）が異なる。
	for _, name := range []string{"a+", "a"} {
		d := drawer.Drawer{Dir: filepath.Join(dataRoot, "drawers", name+"-0123abcd"), Path: t.TempDir(), Name: name}
		if err := d.Register(); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Summaries(dataRoot)

	if err != nil {
		t.Fatalf("Summaries: %v", err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Drawer.Name)
	}
	if want := []string{"a", "a+"}; !slices.Equal(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
}

// forceColor は、端末でなくても色を出す Writer を通して s を書いた結果を返す（fzf のプレビューと同じ条件）。
func forceColor(t *testing.T, s string) string {
	t.Helper()
	var b strings.Builder
	w := colorprofile.NewWriter(&b, []string{"CLICOLOR_FORCE=1", "TERM=xterm-256color", "COLORTERM=truecolor"})
	if _, err := w.WriteString(s); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// sgr は c の 24 ビット色の SGR の引数を返す。kind は 38（前景）か 48（背景）。
func sgr(kind string, c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%s;2;%d;%d;%d", kind, r>>8, g>>8, b>>8)
}

func TestTableColorsCountsByState(t *testing.T) {
	summaries := []Summary{{Drawer: drawer.Drawer{Dir: "/data/drawers/api-3f2a9c1b", Name: "api"}, Issues: Issues{Count: 1}, Running: 1, Waiting: 1, Idle: 1}}

	got := forceColor(t, Table(summaries, 0))

	for name, c := range map[string]color.Color{
		"issues": issuesColor, "running": stateColors[session.Running], "waiting": stateColors[session.Waiting], "idle": stateColors[session.Idle],
	} {
		if !strings.Contains(got, sgr("38", c)) {
			t.Errorf("Table has no %s color: %q", name, got)
		}
	}
}

func TestSessionFrameShowsStateByBorderAndBadge(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	e := scan.Entry{
		Session: session.Session{SessionID: "w1", TmuxPane: "%1", State: session.Waiting},
		Since:   now.Add(-10 * time.Minute),
	}

	got := sessionFrame(e, now, 0)

	want := "╭─ session w1  WAITING 10m  ──────╮\n" +
		"│ pane          %1                │\n" +
		"│ (next action not extracted yet) │\n" +
		"╰─────────────────────────────────╯"
	if plain := ansi.Strip(got); plain != want {
		t.Errorf("sessionFrame =\n%s\nwant\n%s", plain, want)
	}
	// 枠の線（前景）と札（背景）が状態の色になる。
	colored := forceColor(t, got)
	for _, kind := range []string{"38", "48"} {
		if !strings.Contains(colored, sgr(kind, stateColors[session.Waiting])) {
			t.Errorf("sessionFrame has no waiting color (SGR %s): %q", kind, colored)
		}
	}
}

func TestNextRowsShowEveryFieldOrNotExtracted(t *testing.T) {
	generated := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		next session.Next
		ok   bool
		want []string
	}{
		"extracted": {
			next: session.Next{Summary: "s", HumanNext: "h", Blockers: []string{"b1", "b2"}, GeneratedAt: generated},
			ok:   true,
			want: []string{
				"summary       s", "human_next    h", "claude_next   -", "blockers      - b1", "              - b2",
				"generated_at  " + generated.Local().Format(time.DateTime),
			},
		},
		"empty": {ok: true, want: []string{
			"summary       -", "human_next    -", "claude_next   -", "blockers      -", "generated_at  -",
		}},
		"not extracted": {want: []string{"(next action not extracted yet)"}},
	} {
		t.Run(name, func(t *testing.T) {
			rows := nextRows(tc.next, tc.ok, 0)

			got := make([]string, len(rows))
			for i, r := range rows {
				got[i] = ansi.Strip(r)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("nextRows =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// assertFrameWidth は、色を落とした枠 got の全行が width 桁で、右端が枠の線であることを確かめる。
func assertFrameWidth(t *testing.T, got string, width int) {
	t.Helper()
	for i, l := range strings.Split(ansi.Strip(got), "\n") {
		if w := lipgloss.Width(l); w != width {
			t.Errorf("line %d %q is %d wide, want %d", i, l, w, width)
		}
		if !strings.HasSuffix(l, "│") && !strings.HasSuffix(l, "╮") && !strings.HasSuffix(l, "╯") {
			t.Errorf("line %d %q does not end with the border", i, l)
		}
	}
}

func TestSessionFrameFillsWidthAndWrapsValues(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	e := scan.Entry{
		Session: session.Session{SessionID: "w1", TmuxPane: "%1", State: session.Waiting},
		Since:   now.Add(-10 * time.Minute),
		Next:    session.Next{Summary: "fix the flaky test in the api client", Blockers: []string{"CI が落ちていて権限の承認も必要"}},
		HasNext: true,
	}

	got := sessionFrame(e, now, 40)

	// 値の列は 40 - 4（左右の線と余白）- 14（ラベルの列）= 22 桁で、2 行目以降はラベルの列を空けて続ける。
	// 空白の無い全角の並びは語の途中で切り、全角を含む行も右の線が揃う。
	want := "╭─ session w1  WAITING 10m  ───────────╮\n" +
		"│ pane          %1                     │\n" +
		"│ summary       fix the flaky test in  │\n" +
		"│               the api client         │\n" +
		"│ human_next    -                      │\n" +
		"│ claude_next   -                      │\n" +
		"│ blockers      - CI                   │\n" +
		"│               が落ちていて権限の承認 │\n" +
		"│               も必要                 │\n" +
		"│ generated_at  -                      │\n" +
		"╰──────────────────────────────────────╯"
	if plain := ansi.Strip(got); plain != want {
		t.Errorf("sessionFrame =\n%s\nwant\n%s", plain, want)
	}
	assertFrameWidth(t, got, 40)
}

func TestFrameKeepsWidthForLongTitleAndNotExtracted(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	e := scan.Entry{
		Session: session.Session{SessionID: "0123456789abcdef-0123456789abcdef", TmuxPane: "%1", State: session.Idle},
		Since:   now.Add(-10 * time.Minute),
	}

	got := sessionFrame(e, now, 24)

	// タイトル（札まで含む）は 24 - 6 桁に切り詰め、枠内の案内行も中身の幅 20 桁で折り返す。
	want := "╭─ session 012345678… ─╮\n" +
		"│ pane          %1     │\n" +
		"│ (next action not     │\n" +
		"│ extracted yet)       │\n" +
		"╰──────────────────────╯"
	if plain := ansi.Strip(got); plain != want {
		t.Errorf("sessionFrame =\n%s\nwant\n%s", plain, want)
	}
	assertFrameWidth(t, got, 24)
}

func TestDetailShowsIssuesFromTheGivenCounter(t *testing.T) {
	dataRoot := t.TempDir()
	d := drawer.Drawer{Dir: filepath.Join(dataRoot, "drawers", "api-0123abcd"), Path: t.TempDir(), Name: "api"}
	if err := d.Register(); err != nil {
		t.Fatal(err)
	}
	for want, given := range map[string]Issues{"7 open": {Count: 7}, "?": {Err: errors.New("unknown")}} {
		t.Run(want, func(t *testing.T) {
			var asked []string
			text, issues, err := Detail(dataRoot, "api", func(got drawer.Drawer) Issues {
				asked = append(asked, got.Slug())
				return given
			}, time.Now(), 0)

			if err != nil {
				t.Fatalf("Detail: %v", err)
			}
			// 件数は引き出しごとに 1 回だけ問い合わせ、その値を枠の issues の行と戻り値に使う。
			if !slices.Equal(asked, []string{d.Slug()}) || issues != given {
				t.Errorf("asked %q and returned %+v, want %q and %+v", asked, issues, []string{d.Slug()}, given)
			}
			if row := "issues        " + want + " "; !strings.Contains(ansi.Strip(text), row) {
				t.Errorf("Detail =\n%s\nwant the row %q", text, row)
			}
		})
	}
}
