package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/show"
)

// showUsage は hikidashi show の使い方。
const showUsage = "Usage: hikidashi show [<drawer>]\n"

// runShow は hikidashi show の入口。引数が無ければ全引き出しの概況を 1 引き出し 1 行で、
// <drawer>（slug またはリポジトリ名）があればその引き出しの詳細を stdout に出す。
// Issue の件数が得られなかった引き出しは、理由を stderr に出したうえで exit 0 とする。
func runShow(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		report(stderr, showUsage)
		return 2
	}
	root, err := drawer.DefaultRoot()
	if err == nil {
		if len(args) == 0 {
			err = overview(root, stdout, stderr)
		} else {
			err = detail(root, args[0], stdout, stderr)
		}
	}
	if err != nil {
		report(stderr, fmt.Sprintf("hikidashi show: %v\n", err))
		return 1
	}
	return 0
}

// overview は全引き出しの概況を stdout に書く。登録済みの引き出しが無ければ、その旨を書く。
func overview(root string, stdout, stderr io.Writer) error {
	summaries, err := show.Summaries(root)
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		_, err := io.WriteString(stdout, "no drawers registered (run hikidashi add in a repository)\n")
		return err
	}
	for _, s := range summaries {
		reportIssues(stderr, s.Issues)
	}
	_, err = io.WriteString(stdout, strings.Join(show.Lines(summaries), "\n")+"\n")
	return err
}

// detail は key の引き出しの詳細を stdout に書く。
func detail(root, key string, stdout, stderr io.Writer) error {
	text, issues, err := show.Detail(root, key, time.Now())
	if err != nil {
		return err
	}
	reportIssues(stderr, issues)
	_, err = io.WriteString(stdout, text)
	return err
}

// reportIssues は Issue の件数が得られなかった理由を stderr に書く。件数の欄は ? になっている。
func reportIssues(stderr io.Writer, issues show.Issues) {
	if issues.Err != nil {
		report(stderr, fmt.Sprintf("hikidashi show: %v\n", issues.Err))
	}
}
