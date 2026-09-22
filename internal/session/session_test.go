package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/testutil"
)

// sample は全フィールドを埋めたセッション状態。
func sample() Session {
	return Session{
		SessionID:      "abc-123_X",
		Cwd:            "/src/api/sub",
		TmuxPane:       "%12",
		ClaudePID:      4242,
		TranscriptPath: "/home/u/.claude/projects/api/abc-123_X.jsonl",
		State:          Waiting,
		StateChangedAt: time.Date(2026, 9, 23, 10, 5, 0, 0, time.UTC),
		StartedAt:      time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	drawerDir := t.TempDir()
	want := sample()

	if err := Write(drawerDir, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, ok, err := Read(drawerDir, want.SessionID)

	if err != nil || !ok {
		t.Fatalf("Read = ok %v, err %v, want the written session", ok, err)
	}
	if got != want {
		t.Errorf("Read = %+v, want %+v", got, want)
	}
}

func TestWriteUsesSchemaFieldsReadableOnlyByOwner(t *testing.T) {
	drawerDir := t.TempDir()
	s := sample()

	if err := Write(drawerDir, s); err != nil {
		t.Fatalf("Write: %v", err)
	}

	file := filepath.Join(drawerDir, "sessions", s.SessionID+".json")
	var fields map[string]any
	if err := json.Unmarshal([]byte(testutil.ReadFile(t, file)), &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"session_id":       s.SessionID,
		"cwd":              s.Cwd,
		"tmux_pane":        s.TmuxPane,
		"claude_pid":       float64(s.ClaudePID),
		"transcript_path":  s.TranscriptPath,
		"state":            "waiting",
		"state_changed_at": "2026-09-23T10:05:00Z",
		"started_at":       "2026-09-23T10:00:00Z",
	}
	for k, v := range want {
		if fields[k] != v {
			t.Errorf("%s = %v, want %v", k, fields[k], v)
		}
	}
	if len(fields) != len(want) {
		t.Errorf("fields = %v, want exactly %v", fields, want)
	}
	testutil.AssertPerm(t, filepath.Dir(file), 0o700)
	testutil.AssertPerm(t, file, 0o600)
}

func TestReadMissingSessionIsNotOK(t *testing.T) {
	for name, drawerDir := range map[string]string{
		"no sessions directory": t.TempDir(),
		"no session file":       sessionsDirWith(t, "other.json", "{}"),
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := Read(drawerDir, "abc")

			if ok || err != nil {
				t.Errorf("Read = ok %v, err %v, want ok false, err nil", ok, err)
			}
		})
	}
}

func TestReadTreatsBrokenFileAsMissing(t *testing.T) {
	drawerDir := sessionsDirWith(t, "abc.json", `{"state": "idle"`)

	_, ok, err := Read(drawerDir, "abc")

	if ok || err != nil {
		t.Errorf("Read = ok %v, err %v, want ok false, err nil", ok, err)
	}
}

func TestReadFailsWhenFileCannotBeRead(t *testing.T) {
	drawerDir := t.TempDir()
	// sessions/abc.json をディレクトリにし、無い・壊れているのどちらとも違う読み込みの失敗を起こす。
	if err := os.MkdirAll(filepath.Join(drawerDir, "sessions", "abc.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Read(drawerDir, "abc"); err == nil {
		t.Error("Read succeeded, want an error")
	}
}

func TestWriteFailsWhenSessionsDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	testutil.WriteFile(t, blocker, "")

	if err := Write(blocker, sample()); err == nil {
		t.Error("Write succeeded, want an error")
	}
}

func TestRemoveDeletesOnlyFilesOfTheSession(t *testing.T) {
	drawerDir := t.TempDir()
	dir := filepath.Join(drawerDir, "sessions")
	for _, name := range []string{
		"abc.json", "abc.next.json", "abc.extract.lock",
		"abc-2.json", "ab.json", "xabc.json",
	} {
		testutil.WriteFile(t, filepath.Join(dir, name), "{}")
	}

	if err := Remove(drawerDir, "abc"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	testutil.AssertEntries(t, dir, "ab.json", "abc-2.json", "xabc.json")
}

func TestRemoveWithoutSessionsDirectorySucceeds(t *testing.T) {
	if err := Remove(t.TempDir(), "abc"); err != nil {
		t.Errorf("Remove: %v", err)
	}
}

// sessionsDirWith は sessions/ に name の 1 ファイルだけを持つ引き出しのディレクトリを作って返す。
func sessionsDirWith(t *testing.T, name, content string) string {
	t.Helper()
	drawerDir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(drawerDir, "sessions", name), content)
	return drawerDir
}
