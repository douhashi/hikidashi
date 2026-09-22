package session

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/testutil"
)

func TestReadNextDecodesSchema(t *testing.T) {
	drawerDir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(drawerDir, "sessions", "abc.next.json"), `{
  "summary": "API のテストを直している",
  "human_next": "差分を確認する",
  "claude_next": "",
  "blockers": ["CI が落ちている", "仕様が未確定"],
  "generated_at": "2026-09-23T10:00:00Z"
}`)

	got, ok, err := ReadNext(drawerDir, "abc")

	if err != nil || !ok {
		t.Fatalf("ReadNext = ok %v, err %v, want the next action", ok, err)
	}
	want := Next{
		Summary:     "API のテストを直している",
		HumanNext:   "差分を確認する",
		Blockers:    []string{"CI が落ちている", "仕様が未確定"},
		GeneratedAt: time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
	}
	if got.Summary != want.Summary || got.HumanNext != want.HumanNext || got.ClaudeNext != want.ClaudeNext ||
		!slices.Equal(got.Blockers, want.Blockers) || !got.GeneratedAt.Equal(want.GeneratedAt) {
		t.Errorf("ReadNext = %+v, want %+v", got, want)
	}
}

func TestReadNextNotExtractedIsNotOK(t *testing.T) {
	for name, drawerDir := range map[string]string{
		"no file":     t.TempDir(),
		"broken file": sessionsDirWith(t, "abc.next.json", `{"summary":`),
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := ReadNext(drawerDir, "abc")

			if ok || err != nil {
				t.Errorf("ReadNext = ok %v, err %v, want ok false, err nil", ok, err)
			}
		})
	}
}

func TestWriteNextThenReadNextRoundTrips(t *testing.T) {
	drawerDir := t.TempDir()
	want := Next{
		Summary:     "API のテストを直している",
		ClaudeNext:  "失敗しているテストを直す",
		Blockers:    []string{},
		GeneratedAt: time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
	}

	if err := WriteNext(drawerDir, "abc", want); err != nil {
		t.Fatalf("WriteNext: %v", err)
	}
	got, ok, err := ReadNext(drawerDir, "abc")

	if err != nil || !ok {
		t.Fatalf("ReadNext = ok %v, err %v, want the written next action", ok, err)
	}
	if got.Summary != want.Summary || got.ClaudeNext != want.ClaudeNext || got.HumanNext != "" ||
		!slices.Equal(got.Blockers, want.Blockers) || !got.GeneratedAt.Equal(want.GeneratedAt) {
		t.Errorf("ReadNext = %+v, want %+v", got, want)
	}
	testutil.AssertPerm(t, filepath.Join(drawerDir, "sessions"), 0o700)
	testutil.AssertPerm(t, filepath.Join(drawerDir, "sessions", "abc.next.json"), 0o600)
}

func TestOpenExtractLockCreatesPrivateFile(t *testing.T) {
	drawerDir := t.TempDir()

	f, err := OpenExtractLock(drawerDir, "abc")
	if err != nil {
		t.Fatalf("OpenExtractLock: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	path := filepath.Join(drawerDir, "sessions", "abc.extract.lock")
	if f.Name() != path {
		t.Errorf("lock = %s, want %s", f.Name(), path)
	}
	if _, err := f.WriteString("1"); err != nil {
		t.Errorf("lock is not writable: %v", err)
	}
	testutil.AssertPerm(t, filepath.Join(drawerDir, "sessions"), 0o700)
	testutil.AssertPerm(t, path, 0o600)
}
