package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// tailSize は中断の判定で transcript の末尾から読む大きさ。
const tailSize = 64 << 10

// interruptionPrefix は、Esc で中断したときに transcript に残る user のエントリの書き出し。
// ツール実行中の中断では `[Request interrupted by user for tool use]` になる。
const interruptionPrefix = "[Request interrupted by user"

// entry は transcript の 1 行のうち、hikidashi が使うフィールド。
type entry struct {
	Type        string    `json:"type"`
	IsSidechain bool      `json:"isSidechain"`
	IsMeta      bool      `json:"isMeta"`
	Timestamp   time.Time `json:"timestamp"`
	Message     struct {
		// Content は文字列、または type を持つ要素の配列。
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// Interrupted は transcript の末尾を読み、サブエージェントのものでない最後の user のエントリが中断の記録で、
// その時刻が since より後なら、その時刻と true を返す。transcript が無ければ中断していないものとする。
// 規則は docs/development/architecture.md の「中断の扱い」を参照。
func Interrupted(transcriptPath string, since time.Time) (time.Time, bool, error) {
	tail, err := readTail(transcriptPath)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}

	lines := bytes.Split(tail, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var e entry
		// 書きかけの行や JSON でない行は、エントリではないので飛ばす。
		if json.Unmarshal(lines[i], &e) != nil || e.Type != "user" || e.IsSidechain {
			continue
		}
		if isInterruption(e) && e.Timestamp.After(since) {
			return e.Timestamp, true, nil
		}
		return time.Time{}, false, nil
	}
	return time.Time{}, false, nil
}

// readTail は path の末尾 tailSize バイトを返す。途中から読んだときは、切れた先頭の行を落とす。
func readTail(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := max(info.Size()-tailSize, 0)
	data, err := io.ReadAll(io.NewSectionReader(f, offset, info.Size()-offset))
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		_, data, _ = bytes.Cut(data, []byte("\n"))
	}
	return data, nil
}

// isInterruption は user のエントリ e が中断の記録かを返す。
func isInterruption(e entry) bool {
	return slices.ContainsFunc(e.texts(), func(text string) bool {
		return strings.HasPrefix(text, interruptionPrefix)
	})
}

// texts は e の content のうちテキストを順に返す。content が文字列ならそれ自体、配列なら text の要素だけとし、
// ツールの入出力（tool_use / tool_result）や thinking は含めない。
func (e entry) texts() []string {
	var text string
	if json.Unmarshal(e.Message.Content, &text) == nil {
		return []string{text}
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(e.Message.Content, &parts) != nil {
		return nil
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return texts
}

// ScanMessages は transcript を先頭から 1 行ずつ流し読みし、人間と Claude の会話のテキストを順に fn へ渡す。
// role は user か assistant で、text は 1 エントリのテキストを空行で繋いだもの。
// 渡すのはサブエージェントのものでも、Claude Code が差し込んだもの（isMeta）でもない user / assistant のエントリで、
// テキストを持たないもの（ツールの入出力だけ等）・空白だけのものは飛ばす。書きかけの行や JSON でない行も飛ばす。
func ScanMessages(transcriptPath string, fn func(role, text string)) error {
	return eachLine(transcriptPath, func(line []byte) {
		var e entry
		if json.Unmarshal(line, &e) == nil && (e.Type == "user" || e.Type == "assistant") && !e.IsSidechain && !e.IsMeta {
			if text := strings.Join(e.texts(), "\n\n"); strings.TrimSpace(text) != "" {
				fn(e.Type, text)
			}
		}
	})
}

// eachLine は path を先頭から 1 行ずつ流し読みし、各行（末尾の書きかけの行を含む）を fn へ渡す。
func eachLine(path string, fn func(line []byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		fn(line)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// backgroundKeywords は、バックグラウンドのタスクの起動か完了を記録し得る行が必ず含む文字列。
// どれも含まない行は JSON を解かずに飛ばし、大きな transcript の全体を読んでも速く終える。
var backgroundKeywords = [][]byte{[]byte(`"async_launched"`), []byte(`"resumedAgentId"`), []byte("<task-id>")}

// notificationPrefix は完了通知の本文の書き出し。
const notificationPrefix = "<task-notification>"

// taskIDPattern は完了通知の中のタスクの ID。
var taskIDPattern = regexp.MustCompile(`<task-id>([^<]*)</task-id>`)

// backgroundEntry は transcript の 1 行のうち、バックグラウンドのタスクの判定に使うフィールド。
type backgroundEntry struct {
	entry
	// ToolUseResult はツールの結果の構造。ツールによって文字列のこともある。
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Attachment    struct {
		Type string `json:"type"`
		// Prompt は queued_command の本文。文字列とは限らない。
		Prompt json.RawMessage `json:"prompt"`
	} `json:"attachment"`
}

// BackgroundRunning は transcript を先頭から 1 行ずつ流し読みし、since より後にメインが起動（または再開）した
// バックグラウンドのエージェントのうち、完了通知が届いていないものがあれば true を返す。
// transcript が無ければ走行中のタスクは無いものとする。
// 規則は docs/development/architecture.md の「バックグラウンドのタスクの扱い」を参照。
func BackgroundRunning(transcriptPath string, since time.Time) (bool, error) {
	// 同じエージェントは再開のたびに通知し直すため、集合の差ではなく記録の順に出し入れする。
	running := map[string]bool{}
	err := eachLine(transcriptPath, func(line []byte) {
		if slices.ContainsFunc(backgroundKeywords, func(k []byte) bool { return bytes.Contains(line, k) }) {
			applyBackground(line, since, running)
		}
	})
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(running) > 0, nil
}

// applyBackground は 1 行 line が記録する起動を running に足し、完了を running から除く。
// 書きかけの行や JSON でない行、サブエージェントのものは飛ばす。
func applyBackground(line []byte, since time.Time, running map[string]bool) {
	var e backgroundEntry
	if json.Unmarshal(line, &e) != nil || e.IsSidechain {
		return
	}
	switch {
	case e.Type == "user":
		if id, ok := launchedAgent(e.ToolUseResult); ok && e.Timestamp.After(since) {
			running[id] = true
		}
		var text string
		if json.Unmarshal(e.Message.Content, &text) == nil {
			finish(text, running)
		}
	case e.Type == "attachment" && e.Attachment.Type == "queued_command":
		var text string
		if json.Unmarshal(e.Attachment.Prompt, &text) == nil {
			finish(text, running)
		}
	}
}

// launchedAgent は、ツールの結果 raw が Agent の run_in_background による起動か SendMessage による再開なら、
// そのエージェントの ID を返す。Bash の run_in_background（backgroundTaskId）は数えない。
func launchedAgent(raw json.RawMessage) (string, bool) {
	var result struct {
		Status         string `json:"status"`
		AgentID        string `json:"agentId"`
		ResumedAgentID string `json:"resumedAgentId"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "", false
	}
	if result.Status == "async_launched" && result.AgentID != "" {
		return result.AgentID, true
	}
	return result.ResumedAgentID, result.ResumedAgentID != ""
}

// finish は text が完了通知なら、通知されたタスクを running から除く。
// 完了通知は <task-notification> で始まり <status> を持つもので、ID は <status> より前の <task-id> とする
// （後ろの <result> はエージェントの報告で、任意の文字列を含み得る）。<status> の無い Monitor のイベント通知は完了としない。
func finish(text string, running map[string]bool) {
	if !strings.HasPrefix(text, notificationPrefix) {
		return
	}
	header, _, ok := strings.Cut(text, "<status>")
	if !ok {
		return
	}
	for _, m := range taskIDPattern.FindAllStringSubmatch(header, -1) {
		delete(running, m[1])
	}
}
