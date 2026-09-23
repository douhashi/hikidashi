// Package open は hikidashi open が fzf に渡す一覧の行と、プレビューの本文を作る。
// 一覧の形は docs/development/architecture.md の「UI」を参照。
package open

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/render"
	"github.com/douhashi/hikidashi/internal/scan"
	"github.com/douhashi/hikidashi/internal/session"
)

// Lines は entries を 1 件 1 行にする。行は「隠しキー TAB 引き出し名・状態・放置時間・次アクション」で、
// fzf には TAB より後ろだけを見せ、隠しキーはプレビューと選択に使う。
func Lines(entries []scan.Entry, now time.Time) []string {
	width := 0
	for _, e := range entries {
		width = max(width, utf8.RuneCountInString(render.OneLine(e.Drawer.Name)))
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s\t%-*s  %-7s  %3s  %s",
			key(e), width, render.OneLine(e.Drawer.Name), e.Session.State, render.Age(now.Sub(e.Since)), render.OneLine(render.NextAction(e.Next))))
	}
	return lines
}

// Selected は fzf が返した行 line の隠しキーに当たるエントリを返す。
func Selected(entries []scan.Entry, line string) (scan.Entry, bool) {
	k, _, _ := strings.Cut(line, "\t")
	for _, e := range entries {
		if key(e) == k {
			return e, true
		}
	}
	return scan.Entry{}, false
}

// key はエントリの隠しキー `<slug>/<session_id>` を返す。
func key(e scan.Entry) string {
	return e.Drawer.Slug() + "/" + e.Session.SessionID
}

// Preview は隠しキー k のセッションについて、次アクションの全項目と引き出しの備忘録を返す。
// k の引き出しは dataRoot に登録済みの引き出しから slug で引き（drawer.Find）、k からパスを組み立てない。
// セッション ID もファイル名に使える形に限るため、不正なキーでデータルートの外を読むことはない。
func Preview(dataRoot, k string) (string, error) {
	slug, id, _ := strings.Cut(k, "/")
	if !session.ValidID(id) {
		return "", fmt.Errorf("invalid key %q", k)
	}
	d, ok, err := drawer.Find(dataRoot, slug)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("no drawer %q", slug)
	}
	next, hasNext, err := session.ReadNext(d.Dir, id)
	if err != nil {
		return "", err
	}
	notes, err := render.Notes(d)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n\n", d.Name, d.Path)
	render.WriteNext(&b, next, hasNext)
	b.WriteString("\n── notes.md ──\n")
	b.WriteString(notes)
	return b.String(), nil
}
