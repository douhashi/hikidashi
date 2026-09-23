// hikidashi コマンドのエントリポイント。
// 何を解くかは docs/business/concept.md、サブコマンドの構成は docs/development/architecture.md の「構成要素」を参照。
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/douhashi/hikidashi/internal/drawer"
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
	{name: "list", summary: "print an overview of all drawers", run: runList},
	{name: "notes", summary: "open the notes of the current drawer in $EDITOR", run: runNotes},
	{name: "open", summary: "open the tmux session of a drawer (the current one if omitted, chosen with fzf outside Git)", run: runOpen},
	{name: "remove", summary: "unregister a drawer (the current one if omitted), keeping its non-empty notes", run: runRemove},
	{name: "show", summary: "print the details of a drawer (the current one if omitted)", run: runShow},
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

// notRegistered は reason（登録済みの引き出しが見つからない理由）に、hikidashi add で登録できることを添えたエラーを返す。
func notRegistered(reason string) error {
	return fmt.Errorf("%s; run \"hikidashi add\" in the repository to register it", reason)
}

// findDrawer は登録済みの引き出しから、name を slug または名前に持つものを返す。
// 見つからなければ hikidashi add を案内するエラー、名前が複数に当たれば候補の slug を並べたエラーを返す。
func findDrawer(root, name string) (drawer.Drawer, error) {
	d, ok, err := drawer.Find(root, name)
	if err == nil && !ok {
		err = notRegistered(fmt.Sprintf("no registered drawer %q", name))
	}
	return d, err
}

// currentDrawer は cwd のリポジトリの登録済みの引き出しを返す。cwd が Git 管理外なら inGit=false を返し、
// Git 管理下で未登録なら hikidashi add を案内するエラーを返す。
func currentDrawer(root, cwd string) (d drawer.Drawer, inGit bool, err error) {
	if _, inGit, err = drawer.Resolve(root, cwd); err != nil || !inGit {
		return drawer.Drawer{}, inGit, err
	}
	d, ok, err := drawer.Lookup(root, cwd)
	if err == nil && !ok {
		err = notRegistered(cwd + " is not in a registered drawer")
	}
	return d, true, err
}

// report は stderr に書く。stderr に書けなければ失敗を伝える先が無いため、書き込みの失敗は捨てる。
func report(stderr io.Writer, msg string) {
	_, _ = io.WriteString(stderr, msg)
}
