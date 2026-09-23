// Package show は hikidashi list が出す全引き出しの概況の表と、hikidashi show が出す 1 つの引き出しの詳細の枠を作る。
// 形は docs/development/architecture.md の「UI」の「hikidashi list」「hikidashi show」を参照。
package show

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/github"
	"github.com/douhashi/hikidashi/internal/render"
	"github.com/douhashi/hikidashi/internal/scan"
	"github.com/douhashi/hikidashi/internal/session"
)

// Issues は引き出しのリポジトリにある Open な Issue の件数。Err があれば件数は得られておらず、Err が理由を持つ。
type Issues struct {
	Count int
	Err   error
}

// String は件数を返す。得られていなければ、0 件と見分けられるよう ? を返す。
func (i Issues) String() string {
	if i.Err != nil {
		return "?"
	}
	return strconv.Itoa(i.Count)
}

// Summary は概況の表の 1 行に対応する、1 つの引き出しの Issue の件数と、実効の状態ごとのセッションの件数。
type Summary struct {
	Drawer                 drawer.Drawer
	Issues                 Issues
	Running, Waiting, Idle int
}

// Summaries は dataRoot に登録済みの全引き出しの概況を、名前の順（同名は slug の順）に返す。
// Issue の件数は引き出しごとに並行して数える。セッションの後始末、中断とバックグラウンドのタスクの扱いは scan に従う。
func Summaries(dataRoot string) ([]Summary, error) {
	drawers, err := drawer.List(dataRoot)
	if err != nil {
		return nil, err
	}
	drawer.SortByName(drawers)

	issues := make([]Issues, len(drawers))
	var wg sync.WaitGroup
	for i, d := range drawers {
		wg.Go(func() { issues[i] = openIssues(d) })
	}
	summaries := make([]Summary, len(drawers))
	for i, d := range drawers {
		summaries[i], err = summarize(d)
		if err != nil {
			break
		}
	}
	wg.Wait()
	if err != nil {
		return nil, err
	}
	for i := range summaries {
		summaries[i].Issues = issues[i]
	}
	return summaries, nil
}

// summarize は d のセッションを実効の状態ごとに数える。
func summarize(d drawer.Drawer) (Summary, error) {
	entries, err := scan.Drawer(d)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Drawer: d}
	for _, e := range entries {
		switch e.Session.State {
		case session.Running:
			s.Running++
		case session.Waiting:
			s.Waiting++
		case session.Idle:
			s.Idle++
		}
	}
	return s, nil
}

// Table は summaries を 1 引き出し 1 行の罫線付きの表にする。件数は 1 件以上を状態ごとの色で示す。
func Table(summaries []Summary) string {
	t := table.New().Border(lipgloss.RoundedBorder()).BorderStyle(lipgloss.NewStyle().Foreground(lineColor)).
		Headers("DRAWER", "SLUG", "ISSUES", "RUNNING", "WAITING", "IDLE").
		StyleFunc(func(row, col int) lipgloss.Style {
			return cellStyle(summaries, row, col).Padding(0, 1).Align(cellAlign(col))
		})
	for _, s := range summaries {
		t.Row(render.OneLine(s.Drawer.Name), render.OneLine(s.Drawer.Slug()), s.Issues.String(),
			strconv.Itoa(s.Running), strconv.Itoa(s.Waiting), strconv.Itoa(s.Idle))
	}
	return t.String()
}

// cellStyle は表の row 行 col 列のセルの色と太さを返す。row が table.HeaderRow なら見出し。
func cellStyle(summaries []Summary, row, col int) lipgloss.Style {
	if row == table.HeaderRow {
		return muted.Bold(true)
	}
	s := summaries[row]
	switch col {
	case 0:
		return strong
	case 1:
		return muted
	case 2:
		return issuesStyle(s.Issues)
	case 3:
		return count(s.Running, stateColors[session.Running])
	case 4:
		return count(s.Waiting, stateColors[session.Waiting])
	}
	return count(s.Idle, stateColors[session.Idle])
}

// cellAlign は col 列の寄せ。件数の列は右に寄せる。
func cellAlign(col int) lipgloss.Position {
	if col >= 2 {
		return lipgloss.Right
	}
	return lipgloss.Left
}

// issuesStyle は Issue の件数の見た目。得られなかった ? は 0 件と同じく目立たせない。
func issuesStyle(i Issues) lipgloss.Style {
	if i.Err != nil {
		return muted
	}
	return count(i.Count, issuesColor)
}

// labelWidth は枠の中のラベルの列の幅。最も長いラベル generated_at に 2 桁の間を足す。
const labelWidth = len("generated_at") + 2

// none は値が無いことを表す表示。
const none = "-"

// row はラベルと値の 1 行。値が無ければ none を目立たせずに出す。
func row(label, value string, style lipgloss.Style) string {
	if value == "" {
		value, style = none, muted
	}
	return muted.Render(fmt.Sprintf("%-*s", labelWidth, label)) + style.Render(render.OneLine(value))
}

// Detail は key（slug、または一意に決まるリポジトリ名）の引き出しについて、引き出し（パス・slug・Issue の件数）、
// セッションごと（実効の状態・放置時間・pane・次アクションの全項目）、備忘録をそれぞれ枠で区切った本文を返す。
// 引き出しは登録済みのものから引き、key からパスを組み立てない。Issue の件数は得られなくても本文を返す。
func Detail(dataRoot, key string, now time.Time) (string, Issues, error) {
	d, ok, err := drawer.Find(dataRoot, key)
	if err != nil {
		return "", Issues{}, err
	}
	if !ok {
		return "", Issues{}, fmt.Errorf("no drawer %q", key)
	}
	entries, err := scan.Drawer(d)
	if err != nil {
		return "", Issues{}, err
	}
	notes, err := render.Notes(d)
	if err != nil {
		return "", Issues{}, err
	}
	issues := openIssues(d)

	issuesValue := "?"
	if issues.Err == nil {
		issuesValue = strconv.Itoa(issues.Count) + " open"
	}
	blocks := []string{frame(strong.Render(render.OneLine(d.Name)), drawerColor, []string{
		row("path", d.Path, plain),
		row("slug", d.Slug(), muted),
		row("issues", issuesValue, issuesStyle(issues)),
	})}
	if len(entries) == 0 {
		blocks = append(blocks, muted.Render("(no sessions)"))
	}
	for _, e := range entries {
		blocks = append(blocks, sessionFrame(e, now))
	}
	notesLines := strings.Split(strings.TrimSuffix(notes, "\n"), "\n")
	for i, l := range notesLines {
		notesLines[i] = render.OneLine(l)
	}
	blocks = append(blocks, frame("notes.md", lineColor, notesLines))
	return strings.Join(blocks, "\n") + "\n", issues, nil
}

// sessionFrame は 1 セッションの枠。枠の色と上辺の札で実効の状態を示し、札に放置時間を添える。
func sessionFrame(e scan.Entry, now time.Time) string {
	title := "session " + render.OneLine(e.Session.SessionID) + " " + badge(e.Session.State, render.Age(now.Sub(e.Since)))
	lines := append([]string{row("pane", e.Session.TmuxPane, plain)}, nextRows(e.Next, e.HasNext)...)
	return frame(title, stateColors[e.Session.State], lines)
}

// nextRows は次アクションの全項目を、sessions/<session_id>.next.json のフィールド名をラベルにした行にする。
// ok が偽（未抽出）なら、その旨だけを返す。
func nextRows(n session.Next, ok bool) []string {
	if !ok {
		return []string{muted.Render("(next action not extracted yet)")}
	}
	rows := []string{row("summary", n.Summary, plain), row("human_next", n.HumanNext, plain), row("claude_next", n.ClaudeNext, plain)}
	if len(n.Blockers) == 0 {
		rows = append(rows, row("blockers", "", plain))
	}
	for i, blocker := range n.Blockers {
		label := ""
		if i == 0 {
			label = "blockers"
		}
		rows = append(rows, row(label, "- "+blocker, plain))
	}
	generated := ""
	if !n.GeneratedAt.IsZero() {
		generated = n.GeneratedAt.Local().Format(time.DateTime)
	}
	return append(rows, row("generated_at", generated, muted))
}

// openIssues は d のリポジトリのルートで、Open な Issue を数える。数えられなければ、どの引き出しかを理由に添える。
func openIssues(d drawer.Drawer) Issues {
	n, err := github.OpenIssues(context.Background(), d.Path)
	if err != nil {
		return Issues{Err: fmt.Errorf("%s: open issues unavailable: %w", d.Slug(), err)}
	}
	return Issues{Count: n}
}
