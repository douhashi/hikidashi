// Package show は hikidashi list が出す全引き出しの概況の行と、hikidashi show が出す 1 つの引き出しの詳細の本文を作る。
// 形は docs/development/architecture.md の「UI」の「hikidashi list」「hikidashi show」を参照。
package show

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

// Summary は概況の 1 行に対応する、1 つの引き出しの Issue の件数と、実効の状態ごとのセッションの件数。
type Summary struct {
	Drawer                 drawer.Drawer
	Issues                 Issues
	Running, Waiting, Idle int
}

// Summaries は dataRoot に登録済みの全引き出しの概況を、名前の順（同名は slug の順）に返す。
// Issue の件数は引き出しごとに並行して数える。セッションの後始末と中断の扱いは scan に従う。
func Summaries(dataRoot string) ([]Summary, error) {
	drawers, err := drawer.List(dataRoot)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(drawers, func(a, b drawer.Drawer) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Slug(), b.Slug()))
	})

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

// Lines は summaries を 1 引き出し 1 行にする。名前と slug の列は幅を揃える。
func Lines(summaries []Summary) []string {
	nameWidth, slugWidth := 0, 0
	for _, s := range summaries {
		nameWidth = max(nameWidth, utf8.RuneCountInString(render.OneLine(s.Drawer.Name)))
		slugWidth = max(slugWidth, utf8.RuneCountInString(render.OneLine(s.Drawer.Slug())))
	}
	lines := make([]string, 0, len(summaries))
	for _, s := range summaries {
		lines = append(lines, fmt.Sprintf("%-*s  %-*s  issues:%s  running:%d  waiting:%d  idle:%d",
			nameWidth, render.OneLine(s.Drawer.Name), slugWidth, render.OneLine(s.Drawer.Slug()),
			s.Issues, s.Running, s.Waiting, s.Idle))
	}
	return lines
}

// Detail は key（slug、または一意に決まるリポジトリ名）の引き出しについて、Issue の件数、
// セッションごとの実効の状態・放置時間・pane・次アクションの全項目、備忘録を並べた本文を返す。
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

	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", d.Name, d.Path)
	fmt.Fprintf(&b, "slug:   %s\n", d.Slug())
	fmt.Fprintf(&b, "issues: %s\n", issues)
	if len(entries) == 0 {
		b.WriteString("\n(no sessions)\n")
	}
	for _, e := range entries {
		fmt.Fprintf(&b, "\n── session %s ──\n", e.Session.SessionID)
		fmt.Fprintf(&b, "state:        %s (%s)\n", e.Session.State, render.Age(now.Sub(e.Since)))
		fmt.Fprintf(&b, "pane:         %s\n", e.Session.TmuxPane)
		render.WriteNext(&b, e.Next, e.HasNext)
	}
	b.WriteString("\n── notes.md ──\n")
	b.WriteString(notes)
	return b.String(), issues, nil
}

// openIssues は d のリポジトリのルートで、Open な Issue を数える。数えられなければ、どの引き出しかを理由に添える。
func openIssues(d drawer.Drawer) Issues {
	n, err := github.OpenIssues(context.Background(), d.Path)
	if err != nil {
		return Issues{Err: fmt.Errorf("%s: open issues unavailable: %w", d.Slug(), err)}
	}
	return Issues{Count: n}
}
