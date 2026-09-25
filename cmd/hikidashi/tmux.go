package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/scan"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/show"
)

// tmuxStatusUsage は hikidashi tmux status の使い方。
const tmuxStatusUsage = "Usage: hikidashi tmux status [running|waiting|idle]\n"

// tmuxIcon は hikidashi tmux status の要約の先頭に付けるヘッダ。Nerd Font の nf-fa-archive（引き出しの箱）。
const tmuxIcon = "\uf187"

// tmuxCommands は hikidashi tmux の子のサブコマンドの表。
var tmuxCommands = []command{
	{name: "status", summary: "print the number of sessions by state for the status bar, or that of one state", run: runTmuxStatus, complete: stateCandidates},
}

// tmuxStates は hikidashi tmux status が扱う状態。要約はこの順に並べる。記号は Nerd Font の字形。
var tmuxStates = []struct {
	state       session.State
	symbol      string
	description string
}{
	{state: session.Running, symbol: "\uf04b", description: "Claude is processing a turn"},                                  // nf-fa-play
	{state: session.Waiting, symbol: "\uf128", description: "Claude is waiting for a decision in the middle of a turn"},     // nf-fa-question
	{state: session.Idle, symbol: "\uf00c", description: "the turn is over and Claude is waiting for the next instruction"}, // nf-fa-check
}

// runTmuxStatus は hikidashi tmux status の入口。tmux の status-right の #() から呼ばれる。
// 引数が無ければ、1 件以上の状態の記号と件数を tmux の書式で色分けして並べる。全部 0 件なら何も出さない。
// 引数が状態名 1 個なら、その状態の件数を色なしの数字と改行だけで出す（0 件でも 0）。
// 引数がそれ以外なら stdout に何も出さず exit 2、集計に失敗すれば stdout に `!` を出して exit 1 とする。
func runTmuxStatus(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 1 || len(args) == 1 && !knownTmuxState(session.State(args[0])) {
		report(stderr, tmuxStatusUsage)
		return 2
	}

	counts, err := countStates()
	if err == nil {
		out := tmuxSummary(counts)
		if len(args) == 1 {
			out = fmt.Sprintf("%d\n", counts[session.State(args[0])])
		}
		_, err = io.WriteString(stdout, out)
	}
	if err != nil {
		report(stderr, fmt.Sprintf("hikidashi tmux status: %v\n", err))
		// #() は stderr を捨てるため、失敗を 0 件と見分けられるよう印をバーに出す。
		// stdout にも書けなければ伝える先が無いため、その失敗は捨てる。
		_, _ = io.WriteString(stdout, "!\n")
		return 1
	}
	return 0
}

// knownTmuxState は state が hikidashi tmux status の扱う状態かを返す。
func knownTmuxState(state session.State) bool {
	for _, s := range tmuxStates {
		if s.state == state {
			return true
		}
	}
	return false
}

// tmuxSummary は counts のうち 1 件以上の状態を、tmux の書式で色分けした記号・空白・件数にして空白区切りで並べる。
// 記号と件数の間の空白は、Nerd Font の字形が 1 マスを超えて描かれても件数に重ならないようにするため。
// 先頭にアイコン、末尾に後続の表示の色を戻す #[default] と改行を付ける。全部 0 件なら空を返す。
func tmuxSummary(counts map[session.State]int) string {
	var parts []string
	for _, s := range tmuxStates {
		if n := counts[s.state]; n > 0 {
			parts = append(parts, fmt.Sprintf("#[fg=%s]%s %d", show.StateHex[s.state], s.symbol, n))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return tmuxIcon + " " + strings.Join(parts, " ") + "#[default]\n"
}

// stateCandidates は hikidashi tmux status の引数の候補（状態名）を返す。
func stateCandidates() ([]candidate, error) {
	cands := make([]candidate, 0, len(tmuxStates))
	for _, s := range tmuxStates {
		cands = append(cands, candidate{value: string(s.state), description: s.description})
	}
	return cands, nil
}

// countStates は全引き出しの生きているセッションを、実効の状態ごとに数える。
// 死んだセッションの除外、中断とバックグラウンドのタスクの反映は scan.Collect が済ませる。
func countStates() (map[session.State]int, error) {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return nil, err
	}
	entries, err := scan.Collect(root)
	if err != nil {
		return nil, err
	}
	counts := map[session.State]int{}
	for _, e := range entries {
		counts[e.Session.State]++
	}
	return counts, nil
}
