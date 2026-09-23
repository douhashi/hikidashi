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
	"github.com/charmbracelet/x/ansi"

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

// Summary は概況の表の 1 行に対応する、1 つの引き出しの Issue の件数と、実効の状態ごとのセッションの件数と、
// 備忘録の最初の 1 行（無ければ空）。
type Summary struct {
	Drawer                 drawer.Drawer
	Issues                 Issues
	Running, Waiting, Idle int
	Note                   string
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

// summarize は d のセッションを実効の状態ごとに数え、備忘録の最初の 1 行を読む。
func summarize(d drawer.Drawer) (Summary, error) {
	entries, err := scan.Drawer(d)
	if err != nil {
		return Summary{}, err
	}
	notes, ok, err := d.Notes()
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Drawer: d}
	if ok {
		s.Note = firstLine(notes)
	}
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

// firstLine は備忘録の最初の空白でない行を、前後の空白を削って 1 行の表示にして返す。無ければ空を返す。
// Markdown の記号（# や - ）は加工しない。
func firstLine(notes string) string {
	for l := range strings.Lines(notes) {
		if l = strings.TrimSpace(l); l != "" {
			return render.OneLine(l)
		}
	}
	return ""
}

// notesHeader は概況の表の備忘録の列の見出し。列はこの幅より狭くしない。
const notesHeader = "NOTES"

// Table は summaries を 1 引き出し 1 行の罫線付きの表にする。件数は 1 件以上を状態ごとの色で示す。
// width は出力先の幅で、正なら備忘録の列を表が width に収まるよう … で切り詰め（見出しの幅は残す）、0 なら切り詰めない。
func Table(summaries []Summary, width int) string {
	notes := make([]string, len(summaries))
	limit := 0
	if width > 0 {
		// 備忘録の列を空にした表の幅（最も広い行の幅）から、見出しの幅を除いた残りの列と罫線の幅。
		others := lipgloss.Width(summaryTable(summaries, notes)) - len(notesHeader)
		limit = max(width-others, len(notesHeader))
	}
	for i, s := range summaries {
		notes[i] = s.Note
		if limit > 0 {
			notes[i] = ansi.Truncate(s.Note, limit, "…")
		}
	}
	return summaryTable(summaries, notes)
}

// TableLines は Table と同じ表を、見出しの 3 行（上の罫線・見出し・区切りの罫線）と、summaries の順の 1 引き出し 1 行に分けて返す。
// 下の罫線は返さない。hikidashi open の fzf が見出しを一覧の上に固定し、引き出しの行だけを選ばせるため。
func TableLines(summaries []Summary, width int) (header, rows []string) {
	lines := strings.Split(Table(summaries, width), "\n")
	return lines[:3], lines[3 : 3+len(summaries)]
}

// summaryTable は summaries の各行の備忘録の列を notes にした表を返す。
func summaryTable(summaries []Summary, notes []string) string {
	t := table.New().Border(lipgloss.RoundedBorder()).BorderStyle(lipgloss.NewStyle().Foreground(lineColor)).
		Headers("DRAWER", "ISSUES", "RUNNING", "WAITING", "IDLE", notesHeader).
		StyleFunc(func(row, col int) lipgloss.Style {
			return cellStyle(summaries, row, col).Padding(0, 1).Align(cellAlign(col))
		})
	for i, s := range summaries {
		t.Row(render.OneLine(s.Drawer.Name), s.Issues.String(),
			strconv.Itoa(s.Running), strconv.Itoa(s.Waiting), strconv.Itoa(s.Idle), notes[i])
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
		return issuesStyle(s.Issues)
	case 2:
		return count(s.Running, stateColors[session.Running])
	case 3:
		return count(s.Waiting, stateColors[session.Waiting])
	case 4:
		return count(s.Idle, stateColors[session.Idle])
	}
	return plain
}

// cellAlign は col 列の寄せ。件数の列（ISSUES〜IDLE）は右に寄せる。
func cellAlign(col int) lipgloss.Position {
	if col >= 1 && col <= 4 {
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

// row はラベルと値の行。値が無ければ none を目立たせずに出す。
// inner（枠の中身の幅、0 は制限なし）が正なら値をラベルの右の列の中で折り返し、2 行目以降はラベルの列を空ける。
func row(label, value string, style lipgloss.Style, inner int) string {
	if value == "" {
		value, style = none, muted
	}
	lines := wrap(render.OneLine(value), inner-labelWidth)
	for i, l := range lines {
		prefix := strings.Repeat(" ", labelWidth)
		if i == 0 {
			prefix = muted.Render(fmt.Sprintf("%-*s", labelWidth, label))
		}
		lines[i] = prefix + style.Render(l)
	}
	return strings.Join(lines, "\n")
}

// wrap は s を limit 桁で折り返した行を返す。limit が 1 未満なら折り返さない。
func wrap(s string, limit int) []string {
	return strings.Split(ansi.Wrap(s, limit, ""), "\n")
}

// twoColumnWidth は、Detail がセッションの枠を左、引き出しと notes.md の枠を右に並べる出力先の最小の幅。
// 列の間を除いて二分したとき、どちらの列でも値の列が 41 桁（全角 20 字）以上取れる幅とする。
// これより狭いと次アクションの文が細かく折り返され、セッションの枠が縦に伸びて並べた意味が薄れる。
const twoColumnWidth = 120

// columnGap は 2 列の間の桁数。
const columnGap = 1

// Detail は key（slug、または一意に決まるリポジトリ名）の引き出しについて、引き出し（パス・slug・Issue の件数）、
// セッションごと（実効の状態・放置時間・pane・次アクションの全項目）、備忘録をそれぞれ枠で区切った本文を返す。
// width は出力先の幅で、正なら枠をその幅にして長い値を折り返し、0 なら枠を最長の行に合わせて折り返さない。
// 値の列が 1 桁も取れないほど狭い width は 0 として扱う。
// width が twoColumnWidth 以上でセッションがあれば、左にセッションの枠、右に引き出しと備忘録の枠を上揃えで並べ、
// それ以外は上から引き出し・セッション・備忘録の順に積む。
// 引き出しは登録済みのものから引き、key からパスを組み立てない。Issue の件数は得られなくても本文を返す。
func Detail(dataRoot, key string, now time.Time, width int) (string, Issues, error) {
	if width-frameInset-labelWidth < 1 {
		width = 0
	}

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

	if width >= twoColumnWidth && len(entries) > 0 {
		// 割り切れない 1 桁は左の列に寄せる。
		left := (width - columnGap + 1) / 2
		right := width - columnGap - left
		body := lipgloss.JoinHorizontal(lipgloss.Top,
			sessionsBlock(entries, now, left),
			strings.Repeat(" ", columnGap),
			drawerFrame(d, issues, right)+"\n"+notesFrame(notes, right))
		return body + "\n", issues, nil
	}
	blocks := []string{drawerFrame(d, issues, width), sessionsBlock(entries, now, width), notesFrame(notes, width)}
	return strings.Join(blocks, "\n") + "\n", issues, nil
}

// drawerFrame は幅 width（0 は制限なし）の引き出しの枠。タイトルが name で、パス・slug・Issue の件数の行を持つ。
func drawerFrame(d drawer.Drawer, issues Issues, width int) string {
	inner := innerWidth(width)
	issuesValue := "?"
	if issues.Err == nil {
		issuesValue = strconv.Itoa(issues.Count) + " open"
	}
	return frame(strong.Render(render.OneLine(d.Name)), drawerColor, []string{
		row("path", d.Path, plain, inner),
		row("slug", d.Slug(), muted, inner),
		row("issues", issuesValue, issuesStyle(issues), inner),
	}, width)
}

// sessionsBlock は幅 width（0 は制限なし）のセッションの枠を entries の順に積む。無ければ (no sessions) を返す。
func sessionsBlock(entries []scan.Entry, now time.Time, width int) string {
	if len(entries) == 0 {
		return muted.Render("(no sessions)")
	}
	frames := make([]string, len(entries))
	for i, e := range entries {
		frames[i] = sessionFrame(e, now, width)
	}
	return strings.Join(frames, "\n")
}

// notesFrame は幅 width（0 は制限なし）の備忘録の枠。本文の各行をその幅に折り返す。
func notesFrame(notes string, width int) string {
	inner := innerWidth(width)
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(notes, "\n"), "\n") {
		lines = append(lines, wrap(render.OneLine(l), inner)...)
	}
	return frame("notes.md", lineColor, lines, width)
}

// innerWidth は幅 width の枠の中身の幅。width が 0（制限なし）なら 0 を返す。
func innerWidth(width int) int {
	if width == 0 {
		return 0
	}
	return width - frameInset
}

// sessionFrame は幅 width（0 は制限なし）の 1 セッションの枠。枠の色と上辺の札で実効の状態を示し、札に放置時間を添える。
func sessionFrame(e scan.Entry, now time.Time, width int) string {
	inner := innerWidth(width)
	title := "session " + render.OneLine(e.Session.SessionID) + " " + badge(e.Session.State, render.Age(now.Sub(e.Since)))
	lines := append([]string{row("pane", e.Session.TmuxPane, plain, inner)}, nextRows(e.Next, e.HasNext, inner)...)
	return frame(title, stateColors[e.Session.State], lines, width)
}

// nextRows は次アクションの全項目を、sessions/<session_id>.next.json のフィールド名をラベルにした行にする。
// ok が偽（未抽出）なら、その旨だけを返す。inner（枠の中身の幅、0 は制限なし）が正なら、その幅に折り返す。
func nextRows(n session.Next, ok bool, inner int) []string {
	if !ok {
		return []string{muted.Render(strings.Join(wrap("(next action not extracted yet)", inner), "\n"))}
	}
	rows := []string{
		row("summary", n.Summary, plain, inner), row("human_next", n.HumanNext, plain, inner), row("claude_next", n.ClaudeNext, plain, inner),
	}
	if len(n.Blockers) == 0 {
		rows = append(rows, row("blockers", "", plain, inner))
	}
	for i, blocker := range n.Blockers {
		label := ""
		if i == 0 {
			label = "blockers"
		}
		rows = append(rows, row(label, "- "+blocker, plain, inner))
	}
	generated := ""
	if !n.GeneratedAt.IsZero() {
		generated = n.GeneratedAt.Local().Format(time.DateTime)
	}
	return append(rows, row("generated_at", generated, muted, inner))
}

// openIssues は d のリポジトリのルートで、Open な Issue を数える。数えられなければ、どの引き出しかを理由に添える。
func openIssues(d drawer.Drawer) Issues {
	n, err := github.OpenIssues(context.Background(), d.Path)
	if err != nil {
		return Issues{Err: fmt.Errorf("%s: open issues unavailable: %w", d.Slug(), err)}
	}
	return Issues{Count: n}
}
