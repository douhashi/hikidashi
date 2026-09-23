// Package tmux は hikidashi が tmux を呼び出す唯一の境界。
// 使い方は docs/development/architecture.md の「hikidashi add」と「UI」を参照。
package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// HasSession は name とちょうど一致するセッションがあるかを返す。
// has-session の exit 1（セッションが無い・サーバーが未起動）は「無い」とし、それ以外の失敗は error を返す。
func HasSession(name string) (bool, error) {
	// = を付けないと、tmux は name で始まる別のセッションにも一致させる。
	err := run("has-session", "-t", "="+name)
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 {
		return false, nil
	}
	return err == nil, err
}

// NewSession は作業ディレクトリを dir とする name のセッションを、クライアントを繋がずに作る。
// サーバーが未起動なら tmux が起動する。
func NewSession(name, dir string) error {
	return run("new-session", "-d", "-s", name, "-c", dir)
}

// SwitchClient は現在のクライアントを target（pane ID 等）へ移動する。tmux のクライアントの中から呼ぶ。
func SwitchClient(target string) error {
	return run("switch-client", "-t", target)
}

// run は tmux を args で実行する。失敗したら、引数と tmux の stderr を含むエラーを返す。
func run(args ...string) error {
	cmd := exec.Command("tmux", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		err = fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		if msg := bytes.TrimSpace(stderr.Bytes()); len(msg) > 0 {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
