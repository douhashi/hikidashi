// Package extract は Stop の hook 入力を受け、transcript の末尾の会話から次アクションを抽出して書く。
// 手順は docs/development/architecture.md の「次アクションの抽出」を参照。
package extract

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/hook"
	"github.com/douhashi/hikidashi/internal/session"
)

// systemPrompt は要約を指示するシステムプロンプト。
//
//go:embed prompt.md
var systemPrompt string

// schema は構造化出力の JSON Schema。形は sessions/<session_id>.next.json から generated_at を除いたもの。
//
//go:embed schema.json
var schema string

const (
	// conversationLimit は claude に渡す会話の上限（バイト）。新しい側から残す。
	conversationLimit = 32 << 10
	// claudeTimeout は 1 回の claude -p を待つ上限。
	claudeTimeout = 120 * time.Second
	// killGrace は claude を止めた後、その子孫が出力を閉じるのを待つ上限。
	killGrace = time.Second
)

// Run は stdin の Stop の hook 入力を受け、そのセッションの次アクションを抽出して dataRoot 配下に書く。
// HIKIDASHI_DISABLE=1（抽出の子プロセス）・tmux 外・Git 管理外・未登録の引き出しのセッションでは、stdin を読んだ後に何もせず終える。
// 同じセッションの抽出は並行させず、抽出中に届いた要求は後ろ寄せで 1 回にまとめる。
func Run(dataRoot string, stdin io.Reader) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	if os.Getenv("HIKIDASHI_DISABLE") == "1" || os.Getenv("TMUX_PANE") == "" {
		return nil
	}
	in, err := hook.Parse(bytes.NewReader(data))
	if err != nil {
		return err
	}
	d, ok, err := drawer.Lookup(dataRoot, in.Cwd)
	if err != nil || !ok {
		return err
	}
	if err := coalesce(d.Dir, in.SessionID, func() error { return extractOnce(dataRoot, d.Dir, in) }); err != nil {
		return fmt.Errorf("%s: %w", in.SessionID, err)
	}
	return nil
}

// extractOnce は in の transcript から次アクションを 1 回抽出し、セッションが続いていれば書く。
// 失敗したときは前回の次アクションに触れない。
func extractOnce(dataRoot, drawerDir string, in hook.Input) error {
	conv, err := readConversation(in.TranscriptPath)
	if err != nil {
		return err
	}
	if conv == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), claudeTimeout)
	defer cancel()
	next, err := summarize(ctx, dataRoot, conv)
	if err != nil {
		return err
	}

	// 抽出の間に SessionEnd が来ていれば、もう読み手のいない次アクションを残さない。
	if _, ok, err := session.Read(drawerDir, in.SessionID); err != nil || !ok {
		return err
	}
	next.GeneratedAt = time.Now()
	return session.WriteNext(drawerDir, in.SessionID, next)
}

// readConversation は transcript の会話を `### <role>` の見出しで並べ、新しい側から conversationLimit バイトまでを返す。
// 途中で切れる文字は落とす。
func readConversation(transcriptPath string) (string, error) {
	var chunks []string
	size := 0
	err := session.ScanMessages(transcriptPath, func(role, text string) {
		c := "### " + role + "\n" + text + "\n\n"
		chunks = append(chunks, c)
		size += len(c)
		// 残りだけで上限を満たす古いメッセージは、読みながら捨てて手元に溜めない。
		for len(chunks) > 1 && size-len(chunks[0]) >= conversationLimit {
			size -= len(chunks[0])
			chunks = chunks[1:]
		}
	})
	if err != nil {
		return "", err
	}

	conv := strings.Join(chunks, "")
	if len(conv) > conversationLimit {
		conv = conv[len(conv)-conversationLimit:]
		for conv != "" && !utf8.RuneStart(conv[0]) {
			conv = conv[1:]
		}
	}
	return conv, nil
}

// claudeResult は claude -p --output-format json の出力のうち、hikidashi が使うフィールド。
type claudeResult struct {
	Subtype          string          `json:"subtype"`
	IsError          bool            `json:"is_error"`
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
}

// structured は構造化出力。欠けたフィールドを見分けるためにポインタで受ける。
type structured struct {
	Summary    *string   `json:"summary"`
	HumanNext  *string   `json:"human_next"`
	ClaudeNext *string   `json:"claude_next"`
	Blockers   *[]string `json:"blockers"`
}

// summarize は conv を軽量モデルで要約し、次アクション（generated_at を除く）を返す。
// 子の claude はツールを持たず、セッションを保存せず、dir で動く。HIKIDASHI_DISABLE=1 を渡し、
// 子で発火する hikidashi の hooks に偽のセッションの記録や再帰の抽出をさせない。
func summarize(ctx context.Context, dir, conv string) (session.Next, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p",
		"--model", "haiku",
		"--output-format", "json",
		"--json-schema", schema,
		"--no-session-persistence",
		"--tools", "",
		"--system-prompt", systemPrompt,
	)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HIKIDASHI_DISABLE=1")
	cmd.Stdin = strings.NewReader(conv)
	cmd.WaitDelay = killGrace
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return session.Next{}, fmt.Errorf("claude: %w: stdout %q, stderr %q", err, out, bytes.TrimSpace(stderr.Bytes()))
	}

	var res claudeResult
	if err := json.Unmarshal(out, &res); err != nil {
		return session.Next{}, fmt.Errorf("decode claude output %q: %w", out, err)
	}
	if res.IsError || res.Subtype != "success" {
		return session.Next{}, fmt.Errorf("claude failed: subtype %q, is_error %v, result %q", res.Subtype, res.IsError, res.Result)
	}
	var s structured
	if err := json.Unmarshal(res.StructuredOutput, &s); err != nil {
		return session.Next{}, fmt.Errorf("decode structured output %q: %w", res.StructuredOutput, err)
	}
	if s.Summary == nil || s.HumanNext == nil || s.ClaudeNext == nil || s.Blockers == nil {
		return session.Next{}, fmt.Errorf("structured output %q lacks a field", res.StructuredOutput)
	}
	return session.Next{Summary: *s.Summary, HumanNext: *s.HumanNext, ClaudeNext: *s.ClaudeNext, Blockers: *s.Blockers}, nil
}
