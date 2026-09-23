package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/x/term"

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
	return detail(root, key, outputWidth(stdout), colorWriter(stdout), stderr)
}

// outputWidth は stdout の幅を返す。fzf のプレビュー（FZF_PREVIEW_COLUMNS が正の整数）ならその桁数、
// 端末ならその幅、どちらでもなければ 0（制限なし）を返す。
func outputWidth(stdout io.Writer) int {
	if columns, err := strconv.Atoi(os.Getenv("FZF_PREVIEW_COLUMNS")); err == nil && columns > 0 {
		return columns
	}
	f, ok := stdout.(*os.File)
	if !ok || !term.IsTerminal(f.Fd()) {
		return 0
	}
	width, _, err := term.GetSize(f.Fd())
	if err != nil {
		return 0
	}
	return width
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
	d, inGit, err := currentDrawer(root, cwd)
	if err != nil {
		return "", err
	}
	if !inGit {
		return "", fmt.Errorf("%s is not in a Git repository; run \"hikidashi list\" to see all drawers", cwd)
	}
	return d.Slug(), nil
}

// detail は key の引き出しの詳細を、幅 width（0 は制限なし）で stdout に書く。
func detail(root, key string, width int, stdout, stderr io.Writer) error {
	text, issues, err := show.Detail(root, key, time.Now(), width)
	if err != nil {
		return err
	}
	reportIssues(stderr, "show", issues)
	_, err = io.WriteString(stdout, text)
	return err
}
