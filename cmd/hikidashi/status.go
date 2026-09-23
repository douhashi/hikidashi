package main

import (
	"fmt"
	"io"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/scan"
	"github.com/douhashi/hikidashi/internal/session"
)

// runStatus は hikidashi status の入口。tmux の status-right の #() から呼ばれ、
// 入力待ち（実効の状態が waiting）のセッションの件数と改行だけを stdout に出す。0 件なら何も出さない。
// 引数があれば stdout に何も出さず exit 2、集計に失敗すれば stdout に `!` を出して exit 1 とする。
func runStatus(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		report(stderr, "Usage: hikidashi status\n")
		return 2
	}

	n, err := countWaiting()
	if err == nil && n > 0 {
		_, err = fmt.Fprintf(stdout, "%d\n", n)
	}
	if err != nil {
		report(stderr, fmt.Sprintf("hikidashi status: %v\n", err))
		// #() は stderr を捨てるため、失敗を 0 件と見分けられるよう印をバーに出す。
		// stdout にも書けなければ伝える先が無いため、その失敗は捨てる。
		_, _ = io.WriteString(stdout, "!\n")
		return 1
	}
	return 0
}

// countWaiting は全引き出しの生きているセッションのうち、実効の状態が waiting のものを数える。
// 死んだセッションの除外、中断とバックグラウンドのタスクの反映は scan.Collect が済ませる。
func countWaiting() (int, error) {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return 0, err
	}
	entries, err := scan.Collect(root)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.Session.State == session.Waiting {
			n++
		}
	}
	return n, nil
}
