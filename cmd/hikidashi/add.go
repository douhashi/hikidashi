package main

import (
	"fmt"
	"io"
	"os"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/tmux"
)

// runAdd は hikidashi add の入口。作業ディレクトリの案件を引き出しとして登録し、tmux セッションを用意する。
// 引数があれば exit 2、Git 管理外・git や tmux の失敗は exit 1 とする。
func runAdd(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		report(stderr, "Usage: hikidashi add\n")
		return 2
	}
	if err := add(stdout); err != nil {
		report(stderr, fmt.Sprintf("hikidashi add: %v\n", err))
		return 1
	}
	return 0
}

// add は引き出しとセッションのうち、足りない方だけを作り、それぞれの結果を 1 行ずつ stdout に書く。
// Git 管理外ならデータルートにも tmux にも何も作らずにエラーを返す。
func add(stdout io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}

	d, registered, err := drawer.Lookup(root, cwd)
	if err != nil {
		return err
	}
	status := "already registered"
	if !registered {
		var ok bool
		d, ok, err = drawer.Resolve(root, cwd)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s is not in a Git repository", cwd)
		}
		if err := d.Register(); err != nil {
			return err
		}
		status = "registered"
	}
	if _, err := fmt.Fprintf(stdout, "drawer: %s (%s)\n", d.Slug(), status); err != nil {
		return err
	}

	name := d.TmuxSession()
	exists, err := tmux.HasSession(name)
	if err != nil {
		return err
	}
	status = "already exists"
	if !exists {
		if err := tmux.NewSession(name, d.Path); err != nil {
			return err
		}
		status = "created"
	}
	_, err = fmt.Fprintf(stdout, "tmux session: %s (%s)\n", name, status)
	return err
}
