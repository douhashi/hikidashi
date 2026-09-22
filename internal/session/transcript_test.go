package session

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/testutil"
)

// changedAt は transcript の各エントリと比べる state_changed_at。
var changedAt = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

// Claude Code の transcript のエントリを模す。形は実際の transcript（v2.1.278）から写した。
const (
	prompt           = `{"type":"user","isSidechain":false,"message":{"role":"user","content":"テストを直して"},"timestamp":"2026-09-23T10:00:00.000Z"}`
	toolUse          = `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]},"timestamp":"2026-09-23T10:00:05.000Z"}`
	toolResult       = `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},"timestamp":"2026-09-23T10:00:06.000Z"}`
	interrupted      = `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]},"timestamp":"2026-09-23T10:05:00.000Z"}`
	interruptedTool  = `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]},"timestamp":"2026-09-23T10:05:00.000Z"}`
	interruptedText  = `{"type":"user","isSidechain":false,"message":{"role":"user","content":"[Request interrupted by user]"},"timestamp":"2026-09-23T10:05:00.000Z"}`
	interruptedEarly = `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]},"timestamp":"2026-09-23T09:59:59.000Z"}`
	sidechainPrompt  = `{"type":"user","isSidechain":true,"message":{"role":"user","content":"サブエージェントへの指示"},"timestamp":"2026-09-23T10:06:00.000Z"}`
	answer           = `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"中断しました"}]},"timestamp":"2026-09-23T10:06:00.000Z"}`
)

// interruptedAt は中断の記録の時刻。
var interruptedAt = time.Date(2026, 9, 23, 10, 5, 0, 0, time.UTC)

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	testutil.WriteFile(t, path, strings.Join(lines, "\n")+"\n")
	return path
}

func TestInterruptedDetectsInterruptionAfterStateChange(t *testing.T) {
	for name, lines := range map[string][]string{
		"text in array":            {prompt, toolUse, interrupted},
		"during tool use":          {prompt, toolUse, toolResult, interruptedTool},
		"text as string":           {prompt, interruptedText},
		"followed by sidechain":    {prompt, interrupted, sidechainPrompt},
		"followed by assistant":    {prompt, interrupted, answer},
		"followed by a torn line":  {prompt, interrupted, `{"type":"user","mess`},
		"followed by non-JSON log": {prompt, interrupted, "not json"},
	} {
		t.Run(name, func(t *testing.T) {
			at, ok, err := Interrupted(writeTranscript(t, lines...), changedAt)

			if err != nil || !ok || !at.Equal(interruptedAt) {
				t.Errorf("Interrupted = %v, ok %v, err %v, want %v, true, nil", at, ok, err, interruptedAt)
			}
		})
	}
}

func TestInterruptedIgnoresOtherEndings(t *testing.T) {
	for name, lines := range map[string][]string{
		"last user is a prompt":         {interrupted, prompt},
		"last user is a tool result":    {prompt, toolUse, toolResult},
		"interruption before the state": {interruptedEarly},
		"no user entry":                 {toolUse, answer},
		"empty transcript":              {},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := Interrupted(writeTranscript(t, lines...), changedAt)

			if ok || err != nil {
				t.Errorf("Interrupted = ok %v, err %v, want ok false, err nil", ok, err)
			}
		})
	}
}

func TestInterruptedReadsOnlyTheTail(t *testing.T) {
	// 末尾 64KiB より前の中断の記録は見ない。見えるのは切れた行の後の assistant だけになる。
	padding := `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"` +
		strings.Repeat("x", 70<<10) + `"}]},"timestamp":"2026-09-23T10:06:00.000Z"}`
	path := writeTranscript(t, interrupted, padding)

	if _, ok, err := Interrupted(path, changedAt); ok || err != nil {
		t.Errorf("Interrupted = ok %v, err %v, want ok false, err nil", ok, err)
	}
}

func TestInterruptedFindsRecordAfterLongHistory(t *testing.T) {
	// 64KiB を超える transcript でも、末尾にある中断の記録は拾う。
	history := make([]string, 0, 1000)
	for range 1000 {
		history = append(history, toolResult)
	}
	path := writeTranscript(t, append(history, interrupted)...)

	if _, ok, err := Interrupted(path, changedAt); !ok || err != nil {
		t.Errorf("Interrupted = ok %v, err %v, want ok true, err nil", ok, err)
	}
}

func TestInterruptedMissingTranscriptIsNotInterrupted(t *testing.T) {
	if _, ok, err := Interrupted(filepath.Join(t.TempDir(), "missing.jsonl"), changedAt); ok || err != nil {
		t.Errorf("Interrupted = ok %v, err %v, want ok false, err nil", ok, err)
	}
}

func TestInterruptedFailsWhenTranscriptCannotBeRead(t *testing.T) {
	if _, _, err := Interrupted(t.TempDir(), changedAt); err == nil {
		t.Error("Interrupted succeeded, want an error")
	}
}

// message は ScanMessages が渡す 1 件。
type message struct{ role, text string }

func scan(t *testing.T, path string) []message {
	t.Helper()
	var got []message
	if err := ScanMessages(path, func(role, text string) { got = append(got, message{role, text}) }); err != nil {
		t.Fatalf("ScanMessages: %v", err)
	}
	return got
}

func TestScanMessagesPassesOnlyConversationText(t *testing.T) {
	// 形は実際の transcript（v2.1.280）から写した。
	meta := `{"type":"user","isMeta":true,"isSidechain":false,"message":{"role":"user","content":"<local-command-caveat>Caveat</local-command-caveat>"}}`
	thinking := `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"thinking","thinking":"考える"},{"type":"text","text":"直します"},{"type":"tool_use","id":"t2","name":"Edit","input":{}},{"type":"text","text":"続けます"}]}}`
	sidechainAnswer := `{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"サブエージェントの報告"}]}}`
	system := `{"type":"system","content":"hook ran"}`
	path := writeTranscript(t, meta, prompt, toolUse, toolResult, thinking, sidechainPrompt, sidechainAnswer, system,
		"not json", interrupted, answer, `{"type":"assistant","mess`)

	got := scan(t, path)

	want := []message{
		{"user", "テストを直して"},
		{"assistant", "直します\n\n続けます"},
		{"user", "[Request interrupted by user]"},
		{"assistant", "中断しました"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("messages = %q, want %q", got, want)
	}
}

func TestScanMessagesReadsLinesLongerThanAnyBuffer(t *testing.T) {
	long := strings.Repeat("あ", 1<<20)
	line := `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"` + long + `"}]}}`

	got := scan(t, writeTranscript(t, line, prompt))

	if len(got) != 2 || got[0].text != long || got[1].text != "テストを直して" {
		t.Errorf("got %d messages, want the long answer and the prompt", len(got))
	}
}

func TestScanMessagesFailsWhenTranscriptIsMissing(t *testing.T) {
	err := ScanMessages(filepath.Join(t.TempDir(), "missing.jsonl"), func(string, string) {})

	if err == nil {
		t.Error("ScanMessages succeeded, want an error")
	}
}
