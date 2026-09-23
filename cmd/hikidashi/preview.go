package main

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/show"
)

// previewUsage は hikidashi __preview の使い方。
const previewUsage = "Usage: hikidashi __preview <drawer> <issues|?>\n"

// errUnlisted は、一覧で Issue の件数が得られなかった（?）ことを表す。理由は一覧の側で捨てている。
var errUnlisted = errors.New("open issues unavailable")

// runPreview は hikidashi open の fzf のプレビューの入口。<drawer> の引き出しの詳細を hikidashi show と同じ形で stdout に出すが、
// Issue の件数は gh で数えず、一覧で数えた <issues>（非負の整数、得られなければ ?）を使う。
// 引数が 2 個でない・件数が非負の整数でも ? でもなければ exit 2、引き出しが無ければ exit 1 とする。
func runPreview(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		report(stderr, previewUsage)
		return 2
	}
	issues, ok := parseIssues(args[1])
	if !ok {
		report(stderr, previewUsage)
		return 2
	}
	if err := previewDrawer(args[0], issues, stdout); err != nil {
		report(stderr, fmt.Sprintf("hikidashi __preview: %v\n", err))
		return 1
	}
	return 0
}

// parseIssues は一覧の ISSUES 列の値（show.Issues.String() の出力）を件数に戻す。非負の整数でも ? でもなければ ok=false を返す。
func parseIssues(s string) (show.Issues, bool) {
	if s == "?" {
		return show.Issues{Err: errUnlisted}, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return show.Issues{}, false
	}
	return show.Issues{Count: n}, true
}

// previewDrawer は key の引き出しの詳細を、Issue の件数を issues にして stdout に書く。幅と色は hikidashi show と同じ規則で決める。
func previewDrawer(key string, issues show.Issues, stdout io.Writer) error {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}
	text, _, err := show.Detail(root, key, func(drawer.Drawer) show.Issues { return issues }, time.Now(), outputWidth(stdout))
	if err != nil {
		return err
	}
	_, err = io.WriteString(colorWriter(stdout), text)
	return err
}
