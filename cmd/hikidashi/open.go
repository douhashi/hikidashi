package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/open"
	"github.com/douhashi/hikidashi/internal/scan"
	"github.com/douhashi/hikidashi/internal/tmux"
)

// openUsage は hikidashi open の使い方。--preview は fzf のプレビューから呼ばれる。
const openUsage = "Usage: hikidashi open [--preview <key>]\n"

// runOpen は hikidashi open の入口。引数が無ければ一覧を fzf で出して選んだ pane へ移動し、
// --preview <key> なら key のセッションのプレビューを stdout に出す。
func runOpen(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	var err error
	switch {
	case len(args) == 0:
		err = switchToChosen(stderr)
	case len(args) == 2 && args[0] == "--preview":
		err = preview(stdout, args[1])
	default:
		report(stderr, openUsage)
		return 2
	}
	if err != nil {
		report(stderr, fmt.Sprintf("hikidashi open: %v\n", err))
		return 1
	}
	return 0
}

// switchToChosen は全引き出しのセッションを fzf に並べ、選ばれたセッションの pane へ tmux で移動する。
// 何も選ばずに fzf を閉じたときは何もしない。
func switchToChosen(stderr io.Writer) error {
	// switch-client は tmux のクライアントの中からでなければ移動先のクライアントが決まらない。
	if os.Getenv("TMUX") == "" {
		return errors.New("not inside tmux (run it in tmux, e.g. from display-popup)")
	}
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}
	entries, err := scan.Collect(root)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	line, ok, err := choose(open.Lines(entries, time.Now()), exe, stderr)
	if err != nil || !ok {
		return err
	}
	e, ok := open.Selected(entries, line)
	if !ok {
		return fmt.Errorf("unexpected selection %q", line)
	}
	return tmux.SwitchClient(e.Session.TmuxPane)
}

// choose は lines を fzf に渡し、選ばれた行を返す。隠しキー（先頭の列）は見せず、プレビューにだけ渡す。
// Esc 等で何も選ばれなければ ok=false を返す。fzf の画面は端末（/dev/tty）と stderr に出る。
func choose(lines []string, exe string, stderr io.Writer) (line string, ok bool, err error) {
	cmd := exec.Command("fzf",
		"--delimiter=\t", "--with-nth=2..", "--no-sort",
		// 並び順どおりに、人間が捌くべきものを上に出す。
		"--layout=reverse",
		// プレビューのコマンドの引用を、利用者のログインシェルによらず POSIX sh の規則に揃える。
		"--with-shell=sh -c",
		"--preview="+shellQuote(exe)+" open --preview {1}",
	)
	var in strings.Builder
	for _, l := range lines {
		in.WriteString(l + "\n")
	}
	cmd.Stdin = strings.NewReader(in.String())
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if exit, exited := errors.AsType[*exec.ExitError](err); exited {
		switch exit.ExitCode() {
		case 1, 130: // 1: 一致する行が無い、130: Esc / Ctrl-C で閉じた
			return "", false, nil
		}
	}
	if err != nil {
		return "", false, fmt.Errorf("run fzf: %w", err)
	}
	return strings.TrimSuffix(string(out), "\n"), true, nil
}

// preview は隠しキー key のセッションのプレビューを stdout に書く。
func preview(stdout io.Writer, key string) error {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}
	text, err := open.Preview(root, key)
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, text)
	return err
}

// shellQuote は s を POSIX sh の単一引用符で囲み、そのまま 1 語として渡るようにする。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
