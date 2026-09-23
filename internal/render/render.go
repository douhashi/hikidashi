// Package render は読み手（open / list / show）がセッションと引き出しを文字にする共通の書式を持つ。
// 書式は docs/development/architecture.md の「UI」を参照。
package render

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
)

// None は値が無いことを表す表示。
const None = "-"

// Age は放置時間を 5m / 3h / 2d の形にする。単位に満たない端数は切り捨てる。
func Age(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", max(d/time.Minute, 0))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dd", d/(24*time.Hour))
}

// OneLine は制御文字（改行・TAB・エスケープ等）を空白に置き換え、1 行の表示を崩さないようにする。
func OneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// WriteNext は次アクションの全項目を、sessions/<session_id>.next.json のフィールド名で書く。
// ok が偽（未抽出）なら、その旨だけを書く。
func WriteNext(b *strings.Builder, n session.Next, ok bool) {
	if !ok {
		b.WriteString("(next action not extracted yet)\n")
		return
	}
	fmt.Fprintf(b, "summary:      %s\n", orNone(n.Summary))
	fmt.Fprintf(b, "human_next:   %s\n", orNone(n.HumanNext))
	fmt.Fprintf(b, "claude_next:  %s\n", orNone(n.ClaudeNext))
	if len(n.Blockers) == 0 {
		fmt.Fprintf(b, "blockers:     %s\n", None)
	} else {
		b.WriteString("blockers:\n")
		for _, blocker := range n.Blockers {
			fmt.Fprintf(b, "  - %s\n", blocker)
		}
	}
	generated := None
	if !n.GeneratedAt.IsZero() {
		generated = n.GeneratedAt.Local().Format(time.DateTime)
	}
	fmt.Fprintf(b, "generated_at: %s\n", generated)
}

// Notes は d の備忘録の本文を返す。notes.md が無い・空白だけなら、その旨を返す。
func Notes(d drawer.Drawer) (string, error) {
	notes, ok, err := d.Notes()
	if err != nil || !ok {
		return "(no notes)\n", err
	}
	return notes, nil
}

func orNone(s string) string {
	if s == "" {
		return None
	}
	return s
}
