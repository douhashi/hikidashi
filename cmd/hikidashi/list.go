package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/show"
)

// runList は hikidashi list の入口。全引き出しの概況を 1 引き出し 1 行で stdout に出す。
// 引数があれば exit 2 とする。Issue の件数が得られなかった引き出しは、理由を stderr に出したうえで exit 0 とする。
func runList(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		report(stderr, "Usage: hikidashi list\n")
		return 2
	}
	root, err := drawer.DefaultRoot()
	if err == nil {
		err = overview(root, stdout, stderr)
	}
	if err != nil {
		report(stderr, fmt.Sprintf("hikidashi list: %v\n", err))
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
		reportIssues(stderr, "list", s.Issues)
	}
	_, err = io.WriteString(stdout, strings.Join(show.Lines(summaries), "\n")+"\n")
	return err
}

// reportIssues は Issue の件数が得られなかった理由を、hikidashi <name> の接頭辞で stderr に書く。件数の欄は ? になっている。
func reportIssues(stderr io.Writer, name string, issues show.Issues) {
	if issues.Err != nil {
		report(stderr, fmt.Sprintf("hikidashi %s: %v\n", name, issues.Err))
	}
}
