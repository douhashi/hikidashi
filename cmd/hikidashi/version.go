package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

const versionUsage = "Usage: hikidashi version\n"

// runVersion は hikidashi version の入口。ビルド情報に埋まった版（タグ・疑似バージョン・(devel)）を 1 行で stdout に出す。
// 版は Go のツールチェインが VCS の情報から埋めるため、ldflags やビルド手順に頼らない。
// ビルド情報が読めなければ (unknown) を出す。引数があれば exit 2 とする。
func runVersion(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		report(stderr, versionUsage)
		return 2
	}

	version := "(unknown)"
	if info, ok := debug.ReadBuildInfo(); ok {
		version = info.Main.Version
	}
	if _, err := fmt.Fprintln(stdout, version); err != nil {
		report(stderr, fmt.Sprintf("hikidashi version: %v\n", err))
		return 1
	}
	return 0
}
