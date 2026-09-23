package main

import (
	"fmt"
	"io"
	"os"

	"github.com/douhashi/hikidashi/internal/drawer"
)

// runRemove は hikidashi remove の入口。指定のプロジェクト（無ければ作業ディレクトリのプロジェクト）の登録を取り消す。
// 引数が 2 個以上なら exit 2、Git 管理外・未登録・曖昧な名前・I/O の失敗は exit 1 とする。tmux には触れない。
func runRemove(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		report(stderr, "Usage: hikidashi remove [<drawer>]\n")
		return 2
	}
	if err := remove(args, stdout); err != nil {
		report(stderr, fmt.Sprintf("hikidashi remove: %v\n", err))
		return 1
	}
	return 0
}

// remove は対象の引き出しの登録を取り消し、結果を stdout に書く。備忘録を残したときはそのパスも書く。
// 対象が見つからなければデータルートに触れずにエラーを返す。
func remove(args []string, stdout io.Writer) error {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}
	d, err := removeTarget(root, args)
	if err != nil {
		return err
	}

	notesKept, err := d.Unregister()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "drawer: %s (removed)\n", d.Slug()); err != nil {
		return err
	}
	if notesKept {
		_, err = fmt.Fprintf(stdout, "notes: %s (kept)\n", d.NotesPath())
	}
	return err
}

// removeTarget は取り消す引き出しを返す。引数があれば slug か名前で、無ければ作業ディレクトリから引く。
func removeTarget(root string, args []string) (drawer.Drawer, error) {
	if len(args) == 1 {
		return findDrawer(root, args[0])
	}

	cwd, err := os.Getwd()
	if err != nil {
		return drawer.Drawer{}, err
	}
	d, ok, err := drawer.Lookup(root, cwd)
	if err == nil && !ok {
		err = notRegistered(cwd + " is not in a registered drawer")
	}
	return d, err
}
