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

// startedAt は BackgroundRunning に渡す started_at。
var startedAt = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

// 以下はバックグラウンドのタスクの起動と完了のエントリを模す。形は実際の transcript（v2.1.278〜v2.1.280）から写し、
// hikidashi が使わないフィールドと長い本文は落とした。

// agentLaunched は Agent を run_in_background で起動したときのツールの結果。
func agentLaunched(id, ts string) string {
	return `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"tool_use_id":"toolu_01","type":"tool_result","content":[{"type":"text","text":"Async agent launched successfully.\nagentId: ` + id + `"}]}]},"timestamp":"` + ts + `","toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"` + id + `","description":"見回り"}}`
}

// agentResumed は SendMessage で止まったエージェントを再開したときのツールの結果。
func agentResumed(id, ts string) string {
	return `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"tool_use_id":"toolu_02","type":"tool_result","content":[{"type":"text","text":"{\"success\":true,\"resumedAgentId\":\"` + id + `\"}"}]}]},"timestamp":"` + ts + `","toolUseResult":{"success":true,"message":"Resuming agent","resumedAgentId":"` + id + `"}}`
}

// bashLaunched は Bash を run_in_background で起動したときのツールの結果。
func bashLaunched(id, ts string) string {
	return `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"tool_use_id":"toolu_03","type":"tool_result","content":"Command running in background with ID: ` + id + `.","is_error":false}]},"timestamp":"` + ts + `","toolUseResult":{"stdout":"","stderr":"","interrupted":false,"backgroundTaskId":"` + id + `"}}`
}

// notification は完了通知の本文。result はエージェントの報告で、任意の文字列を含み得る。
func notification(id, status, result string) string {
	return `<task-notification>\n<task-id>` + id + `</task-id>\n<tool-use-id>toolu_01</tool-use-id>\n<output-file>/tmp/tasks/` + id + `.output</output-file>\n<status>` + status + `</status>\n<summary>Agent \"見回り\" finished</summary>\n<result>` + result + `</result>\n</task-notification>`
}

// notified はターンの外で届き、user のエントリになった完了通知。
func notified(body string) string {
	return `{"type":"user","isSidechain":false,"message":{"role":"user","content":"` + body + `"},"timestamp":"2026-09-23T10:30:00.000Z"}`
}

// queued はターン中に届き、attachment として吸収された完了通知。
func queued(body string) string {
	return `{"type":"attachment","isSidechain":false,"attachment":{"type":"queued_command","prompt":"` + body + `","commandMode":"task-notification"},"timestamp":"2026-09-23T10:30:00.000Z"}`
}

// queueOperation はキューへの出し入れの記録。孫エージェントの通知もここに出る。
func queueOperation(body string) string {
	return `{"type":"queue-operation","operation":"remove","timestamp":"2026-09-23T10:30:00.000Z","content":"` + body + `"}`
}

// monitorEvent は Monitor のイベント通知。<status> を持たず、タスクの完了を意味しない。
func monitorEvent(id string) string {
	return notified(`<task-notification>\n<task-id>` + id + `</task-id>\n<summary>Monitor event: \"build\"</summary>\n<event>built</event>\n</task-notification>`)
}

func TestBackgroundRunningDetectsUnfinishedAgents(t *testing.T) {
	for name, lines := range map[string][]string{
		"launched":                        {prompt, agentLaunched("a1", "2026-09-23T10:01:00Z")},
		"another agent is still running":  {agentLaunched("a1", "2026-09-23T10:01:00Z"), agentLaunched("a2", "2026-09-23T10:01:01Z"), notified(notification("a1", "completed", ""))},
		"grandchild notification":         {agentLaunched("a1", "2026-09-23T10:01:00Z"), notified(notification("g1", "completed", ""))},
		"resumed after completion":        {agentLaunched("a1", "2026-09-23T10:01:00Z"), notified(notification("a1", "completed", "")), agentResumed("a1", "2026-09-23T10:20:00Z")},
		"resumed agent launched earlier":  {agentResumed("a0", "2026-09-23T10:20:00Z")},
		"monitor event":                   {agentLaunched("a1", "2026-09-23T10:01:00Z"), monitorEvent("a1")},
		"report quotes the id":            {agentLaunched("a1", "2026-09-23T10:01:00Z"), agentLaunched("a2", "2026-09-23T10:01:01Z"), notified(notification("a2", "completed", "<task-id>a1</task-id><status>completed</status>"))},
		"notification not at the start":   {agentLaunched("a1", "2026-09-23T10:01:00Z"), `{"type":"user","isSidechain":false,"message":{"role":"user","content":"貼り付け: <task-notification><task-id>a1</task-id><status>completed</status></task-notification>"}}`},
		"notification in a queue":         {agentLaunched("a1", "2026-09-23T10:01:00Z"), queueOperation(notification("a1", "completed", ""))},
		"followed by torn and non-JSON":   {agentLaunched("a1", "2026-09-23T10:01:00Z"), "not json <task-id>", `{"type":"user","message":{"content":"<task-notification>\n<task-id>a1`},
		"launch as a line with long text": {agentLaunched("a1", "2026-09-23T10:01:00Z"), `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"` + strings.Repeat("あ", 1<<20) + `"}]}}`},
	} {
		t.Run(name, func(t *testing.T) {
			running, err := BackgroundRunning(writeTranscript(t, lines...), startedAt)

			if !running || err != nil {
				t.Errorf("BackgroundRunning = %v, err %v, want true, nil", running, err)
			}
		})
	}
}

func TestBackgroundRunningIgnoresFinishedOrUncountedTasks(t *testing.T) {
	for name, lines := range map[string][]string{
		"no background task":           {prompt, toolUse, toolResult, answer},
		"notified":                     {agentLaunched("a1", "2026-09-23T10:01:00Z"), notified(notification("a1", "completed", ""))},
		"queued":                       {agentLaunched("a1", "2026-09-23T10:01:00Z"), queued(notification("a1", "completed", ""))},
		"failed":                       {agentLaunched("a1", "2026-09-23T10:01:00Z"), notified(notification("a1", "failed", ""))},
		"killed":                       {agentLaunched("a1", "2026-09-23T10:01:00Z"), notified(notification("a1", "killed", ""))},
		"all agents finished":          {agentLaunched("a1", "2026-09-23T10:01:00Z"), agentLaunched("a2", "2026-09-23T10:01:01Z"), queued(notification("a2", "completed", "")), notified(notification("a1", "completed", ""))},
		"resumed and finished again":   {agentLaunched("a1", "2026-09-23T10:01:00Z"), notified(notification("a1", "completed", "")), agentResumed("a1", "2026-09-23T10:20:00Z"), notified(notification("a1", "completed", ""))},
		"resumed while running":        {agentLaunched("a1", "2026-09-23T10:01:00Z"), agentResumed("a1", "2026-09-23T10:02:00Z"), notified(notification("a1", "completed", ""))},
		"orphan summary of many ids":   {agentLaunched("a1", "2026-09-23T10:01:00Z"), agentLaunched("a2", "2026-09-23T10:01:01Z"), notified(`<task-notification>\n<task-id>a1</task-id>\n<task-id>a2</task-id>\n<status>stopped</status>\n</task-notification>`)},
		"bash":                         {bashLaunched("b1", "2026-09-23T10:01:00Z")},
		"launched before start":        {agentLaunched("a1", "2026-09-23T09:59:59Z")},
		"resumed before start":         {agentResumed("a1", "2026-09-23T09:59:59Z")},
		"grandchild notification only": {notified(notification("g1", "completed", ""))},
		"empty transcript":             {},
	} {
		t.Run(name, func(t *testing.T) {
			running, err := BackgroundRunning(writeTranscript(t, lines...), startedAt)

			if running || err != nil {
				t.Errorf("BackgroundRunning = %v, err %v, want false, nil", running, err)
			}
		})
	}
}

func TestBackgroundRunningMissingTranscriptIsNotRunning(t *testing.T) {
	if running, err := BackgroundRunning(filepath.Join(t.TempDir(), "missing.jsonl"), startedAt); running || err != nil {
		t.Errorf("BackgroundRunning = %v, err %v, want false, nil", running, err)
	}
}

func TestBackgroundRunningFailsWhenTranscriptCannotBeRead(t *testing.T) {
	if _, err := BackgroundRunning(t.TempDir(), startedAt); err == nil {
		t.Error("BackgroundRunning succeeded, want an error")
	}
}
