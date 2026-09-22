package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/douhashi/hikidashi/internal/drawer"
)

// runNotes は hikidashi notes の入口。現在の引き出しの notes.md を $EDITOR で開く。
// 引数があれば exit 2、引き出しを決められない・$EDITOR が無い・エディタが失敗したときは exit 1 とする。
func runNotes(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		report(stderr, "Usage: hikidashi notes\n")
		return 2
	}
	// 空白で分割し、シェルを介さずに起動する。引用符付きの値には対応しない。
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		report(stderr, "hikidashi: $EDITOR is not set\n")
		return 1
	}

	path, err := prepareNotes()
	if err != nil {
		report(stderr, fmt.Sprintf("hikidashi: %v\n", err))
		return 1
	}

	cmd := exec.Command(editor[0], append(editor[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		report(stderr, fmt.Sprintf("hikidashi: run editor: %v\n", err))
		return 1
	}
	return 0
}

// prepareNotes は作業ディレクトリの引き出しを登録し、notes.md が無ければ空（0600）で作って、そのパスを返す。
// Git 管理外ならデータルートに何も作らずエラーを返す。
func prepareNotes() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := drawer.DefaultRoot()
	if err != nil {
		return "", err
	}
	d, ok, err := drawer.Resolve(root, cwd)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%s is not in a Git repository", cwd)
	}
	if err := d.Register(); err != nil {
		return "", err
	}

	path := d.NotesPath()
	// O_TRUNC を付けないので、既存の備忘録の中身と権限は変わらない。
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return "", err
	}
	return path, f.Close()
}
