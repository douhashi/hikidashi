package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

func TestExtractWritesNextActionSilently(t *testing.T) {
	dataRoot := hookEnv(t)
	repo, d := newRegisteredRepo(t, dataRoot)
	transcript := filepath.Join(t.TempDir(), "s1.jsonl")
	testutil.WriteFile(t, transcript, `{"type":"user","message":{"role":"user","content":"テストを直して"}}`+"\n")
	claude := testutil.NewFakeClaude(t)
	claude.Respond(t, `{"type":"result","subtype":"success","is_error":false,`+
		`"structured_output":{"summary":"テストを直している","human_next":"","claude_next":"","blockers":[]}}`, 0)
	stop := fmt.Sprintf(`{"session_id":"s1","transcript_path":%q,"cwd":%q,"hook_event_name":"Stop"}`, transcript, repo)
	if code, _, _ := invoke(commands, stop, "hook"); code != 0 {
		t.Fatalf("hook exit code = %d, want 0", code)
	}

	code, stdout, stderr := invoke(commands, stop, "extract")

	assertSilentSuccess(t, code, stdout)
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if next, ok, err := session.ReadNext(d.Dir, "s1"); err != nil || !ok || next.Summary != "テストを直している" {
		t.Errorf("ReadNext = %+v, ok %v, err %v, want the extracted next action", next, ok, err)
	}
	testutil.AssertNotExist(t, filepath.Join(dataRoot, "hikidashi.log"))
}

func TestExtractIgnoresUnregisteredRepositorySilently(t *testing.T) {
	dataRoot := hookEnv(t)
	repo, _ := newRepo(t, dataRoot)
	claude := testutil.NewFakeClaude(t)

	code, stdout, stderr := invoke(commands, hookInput("Stop", repo), "extract")

	assertSilentSuccess(t, code, stdout)
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if calls := claude.Calls(t); len(calls) != 0 {
		t.Errorf("claude ran %d times, want never", len(calls))
	}
	testutil.AssertNotExist(t, dataRoot)
}

func TestExtractLogsFailureAndExitsZero(t *testing.T) {
	dataRoot := hookEnv(t)

	code, stdout, _ := invoke(commands, "not json", "extract")

	assertSilentSuccess(t, code, stdout)
	assertLog(t, dataRoot, "extract", regexp.MustCompile(`decode input: .+`))
}

func TestExtractRecoversPanic(t *testing.T) {
	dataRoot := hookEnv(t)
	var out, errOut strings.Builder

	code := run(commands, []string{"extract"}, panicReader{}, &out, &errOut)

	assertSilentSuccess(t, code, out.String())
	assertLog(t, dataRoot, "extract", regexp.MustCompile(`panic: stdin exploded`))
}
