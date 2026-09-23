// Package open は hikidashi open が fzf に渡す一覧の行と、プレビューの本文を作る。
// 一覧の形は docs/development/architecture.md の「UI」を参照。
package open

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/scan"
	"github.com/douhashi/hikidashi/internal/session"
)

// none は値が無いことを表す表示。
const none = "-"

// Lines は entries を 1 件 1 行にする。行は「隠しキー TAB 引き出し名・状態・放置時間・次アクション」で、
// fzf には TAB より後ろだけを見せ、隠しキーはプレビューと選択に使う。
func Lines(entries []scan.Entry, now time.Time) []string {
	width := 0
	for _, e := range entries {
		width = max(width, utf8.RuneCountInString(oneLine(e.Drawer.Name)))
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%s\t%-*s  %-7s  %3s  %s",
			key(e), width, oneLine(e.Drawer.Name), e.Session.State, age(now.Sub(e.Since)), oneLine(nextAction(e))))
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

// nextAction は一覧に出す次アクション。人間の次アクション、無ければ要約、未抽出なら none とする。
func nextAction(e scan.Entry) string {
	switch {
	case e.Next.HumanNext != "":
		return e.Next.HumanNext
	case e.Next.Summary != "":
		return e.Next.Summary
	}
	return none
}

// age は放置時間を 5m / 3h / 2d の形にする。単位に満たない端数は切り捨てる。
func age(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(d/time.Minute, 0))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dd", d/(24*time.Hour))
}

// oneLine は制御文字（改行・TAB・エスケープ等）を空白に置き換え、1 行の表示を崩さないようにする。
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// Preview は隠しキー k のセッションについて、次アクションの全項目と引き出しの備忘録を返す。
// k の引き出しは dataRoot に登録済みの引き出しから名前で引き、k からパスを組み立てない。
// セッション ID もファイル名に使える形に限るため、不正なキーでデータルートの外を読むことはない。
func Preview(dataRoot, k string) (string, error) {
	slug, id, _ := strings.Cut(k, "/")
	if !session.ValidID(id) {
		return "", fmt.Errorf("invalid key %q", k)
	}
	d, err := find(dataRoot, slug)
	if err != nil {
		return "", err
	}
	next, hasNext, err := session.ReadNext(d.Dir, id)
	if err != nil {
		return "", err
	}
	notes, err := os.ReadFile(d.NotesPath())
	if errors.Is(err, fs.ErrNotExist) {
		notes = []byte("(no notes)\n")
	} else if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n\n", d.Name, d.Path)
	if hasNext {
		writeNext(&b, next)
	} else {
		b.WriteString("(next action not extracted yet)\n")
	}
	b.WriteString("\n── notes.md ──\n")
	b.Write(notes)
	return b.String(), nil
}

// find は dataRoot に登録済みの引き出しから、slug が一致するものを返す。
func find(dataRoot, slug string) (drawer.Drawer, error) {
	drawers, err := drawer.List(dataRoot)
	if err != nil {
		return drawer.Drawer{}, err
	}
	for _, d := range drawers {
		if d.Slug() == slug {
			return d, nil
		}
	}
	return drawer.Drawer{}, fmt.Errorf("no drawer %q", slug)
}

// writeNext は次アクションの全項目を、sessions/<session_id>.next.json のフィールド名で書く。
func writeNext(b *strings.Builder, n session.Next) {
	fmt.Fprintf(b, "summary:      %s\n", orNone(n.Summary))
	fmt.Fprintf(b, "human_next:   %s\n", orNone(n.HumanNext))
	fmt.Fprintf(b, "claude_next:  %s\n", orNone(n.ClaudeNext))
	if len(n.Blockers) == 0 {
		fmt.Fprintf(b, "blockers:     %s\n", none)
	} else {
		b.WriteString("blockers:\n")
		for _, blocker := range n.Blockers {
			fmt.Fprintf(b, "  - %s\n", blocker)
		}
	}
	generated := none
	if !n.GeneratedAt.IsZero() {
		generated = n.GeneratedAt.Local().Format(time.DateTime)
	}
	fmt.Fprintf(b, "generated_at: %s\n", generated)
}

func orNone(s string) string {
	if s == "" {
		return none
	}
	return s
}
