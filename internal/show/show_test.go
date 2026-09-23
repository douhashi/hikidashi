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
		{Drawer: drawer.Drawer{Dir: "/data/drawers/api-3f2a9c1b", Name: "api"}, Issues: Issues{Count: 12}, Running: 1, Waiting: 2, Idle: 3},
		{Drawer: drawer.Drawer{Dir: "/data/drawers/frontend-0a1b2c3d", Name: "frontend"}, Issues: Issues{Err: errors.New("no remote")}},
		{Drawer: drawer.Drawer{Dir: "/data/drawers/x-00000000", Name: "x\x1b[31m"}},
	}

	got := ansi.Strip(Table(summaries))

	want := "╭──────────┬───────────────────┬────────┬─────────┬─────────┬──────╮\n" +
		"│ DRAWER   │ SLUG              │ ISSUES │ RUNNING │ WAITING │ IDLE │\n" +
		"├──────────┼───────────────────┼────────┼─────────┼─────────┼──────┤\n" +
		"│ api      │ api-3f2a9c1b      │     12 │       1 │       2 │    3 │\n" +
		"│ frontend │ frontend-0a1b2c3d │      ? │       0 │       0 │    0 │\n" +
		"│ x [31m   │ x-00000000        │      0 │       0 │       0 │    0 │\n" +
		"╰──────────┴───────────────────┴────────┴─────────┴─────────┴──────╯"
	if got != want {
		t.Errorf("Table =\n%s\nwant\n%s", got, want)
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

	got := forceColor(t, Table(summaries))

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
