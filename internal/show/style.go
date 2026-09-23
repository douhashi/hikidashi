package show

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/douhashi/hikidashi/internal/session"
)

// 表と枠の色。色は常に付け、端末でない・NO_COLOR のときに落とすのは書き出す側（colorprofile.Writer）の役目。
var (
	lineColor   = lipgloss.Color("#3b4150") // 表の罫線と notes.md の枠
	mutedColor  = lipgloss.Color("#6b7280") // 見出し・ラベル・0 件・値が無いこと
	strongColor = lipgloss.Color("#eef1f5") // 引き出しの名前
	issuesColor = lipgloss.Color("#c792ea") // 1 件以上の Issue
	drawerColor = lipgloss.Color("#82aaff") // 引き出しの枠
	badgeText   = lipgloss.Color("#16181d") // 状態の札の文字
	// stateColors は実効の状態ごとの色。list の件数、show のセッションの枠と札に使う。
	stateColors = map[session.State]color.Color{
		session.Running: lipgloss.Color("#86d49a"),
		session.Waiting: lipgloss.Color("#ffb454"),
		session.Idle:    lipgloss.Color("#9aa4b2"),
	}
)

var (
	plain  = lipgloss.NewStyle()
	muted  = lipgloss.NewStyle().Foreground(mutedColor)
	strong = lipgloss.NewStyle().Foreground(strongColor).Bold(true)
)

// count は件数 n の見た目。1 件以上なら c の太字、0 件なら目立たせない。
func count(n int, c color.Color) lipgloss.Style {
	if n > 0 {
		return lipgloss.NewStyle().Foreground(c).Bold(true)
	}
	return muted
}

// badge は状態の札。背景を状態の色にし、放置時間を添える。
func badge(state session.State, age string) string {
	return lipgloss.NewStyle().Background(stateColors[state]).Foreground(badgeText).Bold(true).
		Render(" " + strings.ToUpper(string(state)) + " " + age + " ")
}

// frameInset は枠の左右の線と余白の桁数。枠の中身の幅は、枠の幅からこれを引いたもの。
const frameInset = 4

// frame は lines を c の色の角丸の枠で囲み、上辺に title を埋め込む。
// width が 0 なら幅は最長の行（または title）に合わせる。正なら幅を width にし、収まらない title を切り詰める。
// width が正のとき、lines は中身の幅（width - frameInset）に折り返してあること。
func frame(title string, c color.Color, lines []string, width int) string {
	border := lipgloss.RoundedBorder()
	body := strings.Join(lines, "\n")
	// 上辺は「╭─ title ─…╮」で、title の後ろに線を 1 本以上残す。
	if width == 0 {
		width = max(lipgloss.Width(body)+frameInset, lipgloss.Width(title)+6)
	} else {
		title = ansi.Truncate(title, width-6, "…")
	}
	line := lipgloss.NewStyle().Foreground(c)
	top := line.Render(border.TopLeft+border.Top+" ") + title +
		line.Render(" "+strings.Repeat(border.Top, width-lipgloss.Width(title)-5)+border.TopRight)
	box := lipgloss.NewStyle().Border(border, false, true, true).BorderForeground(c).Padding(0, 1).Width(width).Render(body)
	return top + "\n" + box
}
