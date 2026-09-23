package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/show"
)

// showUsage は hikidashi show の使い方。
const showUsage = "Usage: hikidashi show [<drawer>]\n"

// runShow は hikidashi show の入口。<drawer>（slug またはリポジトリ名）の引き出し、
// 無ければ作業ディレクトリの引き出しの詳細を stdout に出す。
// 引数が 2 個以上なら exit 2、作業ディレクトリが Git 管理外・未登録なら exit 1 とする。
// Issue の件数が得られなかった引き出しは、理由を stderr に出したうえで exit 0 とする。
func runShow(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		report(stderr, showUsage)
		return 2
	}
	if err := showDrawer(args, stdout, stderr); err != nil {
		report(stderr, fmt.Sprintf("hikidashi show: %v\n", err))
		return 1
	}
	return 0
}

// showDrawer は対象の引き出しの詳細を stdout に書く。
func showDrawer(args []string, stdout, stderr io.Writer) error {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}
	key, err := showKey(root, args)
	if err != nil {
		return err
	}
	return detail(root, key, stdout, stderr)
}

// showKey は詳細を出す引き出しの鍵を返す。引数があればそれを、無ければ作業ディレクトリの引き出しの slug を返す。
func showKey(root string, args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	_, inGit, err := drawer.Resolve(root, cwd)
	if err != nil {
		return "", err
	}
	if !inGit {
		return "", fmt.Errorf("%s is not in a Git repository; run \"hikidashi list\" to see all drawers", cwd)
	}
	d, ok, err := drawer.Lookup(root, cwd)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", notRegistered(cwd + " is not in a registered drawer")
	}
	return d.Slug(), nil
}

// detail は key の引き出しの詳細を stdout に書く。
func detail(root, key string, stdout, stderr io.Writer) error {
	text, issues, err := show.Detail(root, key, time.Now())
	if err != nil {
		return err
	}
	reportIssues(stderr, "show", issues)
	_, err = io.WriteString(stdout, text)
	return err
}
