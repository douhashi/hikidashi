package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

// tailSize は中断の判定で transcript の末尾から読む大きさ。
const tailSize = 64 << 10

// interruptionPrefix は、Esc で中断したときに transcript に残る user のエントリの書き出し。
// ツール実行中の中断では `[Request interrupted by user for tool use]` になる。
const interruptionPrefix = "[Request interrupted by user"

// entry は transcript の 1 行のうち、中断の判定に使うフィールド。
type entry struct {
	Type        string    `json:"type"`
	IsSidechain bool      `json:"isSidechain"`
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
		if isInterruption(e.Message.Content) && e.Timestamp.After(since) {
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

// isInterruption は user のエントリの content が中断の記録かを返す。
func isInterruption(content json.RawMessage) bool {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return strings.HasPrefix(text, interruptionPrefix)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) != nil {
		return false
	}
	for _, p := range parts {
		if p.Type == "text" && strings.HasPrefix(p.Text, interruptionPrefix) {
			return true
		}
	}
	return false
}
