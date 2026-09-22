package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/hook"
)

// runHook は hikidashi hook の入口。Claude Code を妨げないよう、panic を含むどの失敗でも exit 0 で終え、
// エラーはデータルートの hikidashi.log にだけ残す（docs/development/architecture.md の「設計原則」4）。
func runHook(_ []string, stdin io.Reader, _, stderr io.Writer) int {
	if err := recordHook(stdin); err != nil {
		logHookError(stderr, fmt.Sprintf("%s hook: %v\n", time.Now().Format(time.RFC3339), err))
	}
	return 0
}

// recordHook は hook.Run を既定のデータルートで呼び、panic もエラーとして返す。
func recordHook(stdin io.Reader) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}
	return hook.Run(root, stdin, os.Getenv, time.Now())
}

// logHookError は line をログに追記する。ログにも書けなければ、line と書けなかった理由を stderr に出す。
func logHookError(stderr io.Writer, line string) {
	if err := appendLog(line); err != nil {
		report(stderr, line)
		report(stderr, fmt.Sprintf("hikidashi: write log: %v\n", err))
	}
}

// appendLog は既定のデータルート（0700）の hikidashi.log（0600）に line を追記する。
func appendLog(line string) error {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(root, "hikidashi.log"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
