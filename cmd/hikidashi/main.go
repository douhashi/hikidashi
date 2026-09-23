// hikidashi コマンドのエントリポイント。
// 何を解くかは docs/business/concept.md、サブコマンドの構成は docs/development/architecture.md の「構成要素」を参照。
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// command は 1 つのサブコマンド。run はサブコマンド名より後ろの引数と標準入出力を受け取り、終了コードを返す。
type command struct {
	name    string
	summary string
	run     func(args []string, stdin io.Reader, stdout, stderr io.Writer) int
}

// commands は hikidashi が持つサブコマンドの表。
var commands = []command{
	{name: "add", summary: "register the current repository as a drawer and prepare its tmux session", run: runAdd},
	{name: "extract", summary: "extract the next action of a session from its transcript (Stop hook)", run: runExtract},
	{name: "hook", summary: "record the session state and inject notes from a Claude Code hook input", run: runHook},
	{name: "notes", summary: "open the notes of the current drawer in $EDITOR", run: runNotes},
	{name: "open", summary: "choose a session with fzf and switch to its tmux pane", run: runOpen},
	{name: "status", summary: "print the number of sessions waiting for input, for the tmux status bar", run: runStatus},
}

func main() {
	os.Exit(run(commands, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run は args の先頭をサブコマンド名として cmds から引き、実行する。
// 使い方の誤りは exit 2、help の要求は使い方を stdout に出して exit 0 とする。
func run(cmds []command, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		report(stderr, usage(cmds))
		return 2
	}

	name := args[0]
	switch name {
	case "help", "-h", "--help":
		if _, err := io.WriteString(stdout, usage(cmds)); err != nil {
			report(stderr, fmt.Sprintf("hikidashi: %v\n", err))
			return 1
		}
		return 0
	}

	for _, c := range cmds {
		if c.name == name {
			return c.run(args[1:], stdin, stdout, stderr)
		}
	}

	report(stderr, fmt.Sprintf("hikidashi: unknown command %q\n\n%s", name, usage(cmds)))
	return 2
}

// usage は使い方の文面を返す。サブコマンドがあれば名前と概要を揃えて並べる。
func usage(cmds []command) string {
	var b strings.Builder
	b.WriteString("Usage: hikidashi <command> [arguments]\n")
	if len(cmds) == 0 {
		return b.String()
	}

	width := 0
	for _, c := range cmds {
		width = max(width, len(c.name))
	}
	b.WriteString("\nCommands:\n")
	for _, c := range cmds {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, c.name, c.summary)
	}
	return b.String()
}

// report は stderr に書く。stderr に書けなければ失敗を伝える先が無いため、書き込みの失敗は捨てる。
func report(stderr io.Writer, msg string) {
	_, _ = io.WriteString(stderr, msg)
}
