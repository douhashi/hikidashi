// Package github は hikidashi が GitHub CLI（gh）を呼び出す唯一の境界。
// 使い方は docs/development/architecture.md の「UI」の「hikidashi show」を参照。
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// timeout は gh の 1 回の呼び出しを待つ上限。応答しない gh（ネットワークの不調等）で show を止めないため。
const timeout = 10 * time.Second

// OpenIssues は dir（リポジトリのルート）の GitHub リポジトリにある Open な Issue の件数を返す。
// gh が選ぶリポジトリ（gh repo set-default、無ければ git のリモート）の件数で、Pull Request は含まない。
// GitHub のリモートが無い・gh が無い・認証切れ・timeout 超過などで件数を得られなければ、理由を含む error を返す。
func OpenIssues(ctx context.Context, dir string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "issues")
	cmd.Dir = dir
	// gh が止められても子プロセスが出力を握ったまま残ったときに、待ち続けないようにする。
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return 0, fail(ctx.Err())
	}
	if err != nil {
		if msg := bytes.TrimSpace(stderr.Bytes()); len(msg) > 0 {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return 0, fail(err)
	}

	var v struct {
		Issues struct {
			TotalCount *int `json:"totalCount"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return 0, fail(fmt.Errorf("decode output: %w", err))
	}
	if v.Issues.TotalCount == nil {
		return 0, fail(fmt.Errorf("unexpected output %q", bytes.TrimSpace(out)))
	}
	return *v.Issues.TotalCount, nil
}

// fail は err に、失敗した gh の呼び出しを添える。
func fail(err error) error {
	return fmt.Errorf("gh repo view --json issues: %w", err)
}
