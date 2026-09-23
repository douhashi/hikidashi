// Package tmux は hikidashi が tmux を呼び出す唯一の境界。
// 使い方は docs/development/architecture.md の「hikidashi add」と「hikidashi open」を参照。
package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Ensure は name とちょうど一致するセッションが無ければ、作業ディレクトリを dir としてクライアントを繋がずに作る。
// 作ったかを返す。has-session の exit 1（セッションが無い・サーバーが未起動）は「無い」とし、
// サーバーが未起動なら new-session が起動する。
func Ensure(name, dir string) (created bool, err error) {
	err = run("has-session", "-t", exact(name))
	if err == nil {
		return false, nil
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); !ok || exit.ExitCode() != 1 {
		return false, err
	}
	return true, run("new-session", "-d", "-s", name, "-c", dir)
}

// SwitchClient は現在のクライアントを name のセッションへ切り替える。tmux のクライアントの中から呼ぶ。
func SwitchClient(name string) error {
	return run("switch-client", "-t", exact(name))
}

// Attach は name のセッションに、この端末からクライアントとして繋ぐ。tmux の外から呼び、デタッチするまで戻らない。
// tmux が端末を使うため標準入出力を引き継ぎ、失敗の理由は tmux が stderr に直接出す。
func Attach(name string) error {
	args := []string{"attach-session", "-t", exact(name)}
	cmd := exec.Command("tmux", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// exact は name とちょうど一致するセッションを指すターゲットを返す。
// = を付けないと、tmux は name で始まる別のセッションにも一致させる。
func exact(name string) string {
	return "=" + name
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
