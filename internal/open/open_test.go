package open

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/scan"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

var now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func entry(dir, name, id string, state session.State, since time.Time) scan.Entry {
	return scan.Entry{
		Drawer:  drawer.Drawer{Dir: filepath.Join("/data/drawers", dir), Name: name},
		Session: session.Session{SessionID: id, TmuxPane: "%" + id, State: state},
		Since:   since,
	}
}

func TestLinesShowDrawerStateNeglectAndNextActionBehindHiddenKey(t *testing.T) {
	withHuman := entry("api-3f2a9c1b", "api", "s1", session.Waiting, now.Add(-5*time.Minute))
	withHuman.Next, withHuman.HasNext = session.Next{Summary: "テストを直している", HumanNext: "差分を確認する"}, true
	summaryOnly := entry("frontend-0a1b2c3d", "frontend", "s2", session.Idle, now.Add(-3*time.Hour-59*time.Minute))
	summaryOnly.Next, summaryOnly.HasNext = session.Next{Summary: "設計を\n比較している\t途中"}, true
	notExtracted := entry("api-3f2a9c1b", "api", "s3", session.Running, now.Add(-49*time.Hour))
	emptyNext := entry("api-3f2a9c1b", "api", "s4", session.Running, now.Add(time.Minute))
	emptyNext.HasNext = true

	got := Lines([]scan.Entry{withHuman, summaryOnly, notExtracted, emptyNext}, now)

	want := []string{
		"api-3f2a9c1b/s1\tapi       waiting   5m  差分を確認する",
		"frontend-0a1b2c3d/s2\tfrontend  idle      3h  設計を 比較している 途中",
		"api-3f2a9c1b/s3\tapi       running   2d  -",
		"api-3f2a9c1b/s4\tapi       running   0m  -",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Lines =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLinesSanitizeDrawerName(t *testing.T) {
	e := entry("a-3f2a9c1b", "a\x1b[31mb", "s1", session.Idle, now)

	got := Lines([]scan.Entry{e}, now)

	if want := []string{"a-3f2a9c1b/s1\ta [31mb  idle      0m  -"}; !slices.Equal(got, want) {
		t.Errorf("Lines = %q, want %q", got, want)
	}
}

func TestSelectedFindsEntryByHiddenKey(t *testing.T) {
	entries := []scan.Entry{
		entry("api-3f2a9c1b", "api", "s1", session.Idle, now),
		entry("api-3f2a9c1b", "api", "s2", session.Idle, now),
	}
	lines := Lines(entries, now)

	got, ok := Selected(entries, lines[1])

	if !ok || got.Session.SessionID != "s2" {
		t.Errorf("Selected = %+v, ok %v, want s2", got, ok)
	}
	if _, ok := Selected(entries, "api-3f2a9c1b/s9\tapi"); ok {
		t.Error("Selected found an entry for an unknown key")
	}
}

// previewFixture は api の引き出し（ディレクトリ名 api-3f2a9c1b）を登録したデータルートと、その引き出しを返す。
func previewFixture(t *testing.T) (dataRoot string, d drawer.Drawer) {
	t.Helper()
	dataRoot = t.TempDir()
	d = drawer.Drawer{Dir: filepath.Join(dataRoot, "drawers", "api-3f2a9c1b"), Path: "/src/api", Name: "api"}
	if err := d.Register(); err != nil {
		t.Fatal(err)
	}
	return dataRoot, d
}

func TestPreviewShowsWholeNextActionAndNotes(t *testing.T) {
	dataRoot, d := previewFixture(t)
	generated := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	testutil.WriteFile(t, filepath.Join(d.Dir, "sessions", "s1.next.json"), `{
  "summary": "API のテストを直している",
  "human_next": "差分を確認する",
  "claude_next": "",
  "blockers": ["CI が落ちている", "仕様が未確定"],
  "generated_at": "2026-09-23T10:00:00Z"
}`)
	testutil.WriteFile(t, d.NotesPath(), "# 案件メモ\n- 本番は触らない\n")

	got, err := Preview(dataRoot, "api-3f2a9c1b/s1")

	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	want := "api  /src/api\n" +
		"\n" +
		"summary:      API のテストを直している\n" +
		"human_next:   差分を確認する\n" +
		"claude_next:  -\n" +
		"blockers:\n" +
		"  - CI が落ちている\n" +
		"  - 仕様が未確定\n" +
		"generated_at: " + generated.Local().Format(time.DateTime) + "\n" +
		"\n" +
		"── notes.md ──\n" +
		"# 案件メモ\n- 本番は触らない\n"
	if got != want {
		t.Errorf("Preview =\n%s\nwant\n%s", got, want)
	}
}

func TestPreviewWithoutNextActionOrNotes(t *testing.T) {
	dataRoot, d := previewFixture(t)
	testutil.WriteFile(t, filepath.Join(d.Dir, "sessions", "s2.next.json"), `{"summary":"x","blockers":[]}`)

	for id, next := range map[string]string{
		"s1": "(next action not extracted yet)\n",
		"s2": "summary:      x\nhuman_next:   -\nclaude_next:  -\nblockers:     -\ngenerated_at: -\n",
	} {
		t.Run(id, func(t *testing.T) {
			got, err := Preview(dataRoot, "api-3f2a9c1b/"+id)

			if err != nil {
				t.Fatalf("Preview: %v", err)
			}
			want := "api  /src/api\n\n" + next + "\n── notes.md ──\n(no notes)\n"
			if got != want {
				t.Errorf("Preview =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestPreviewDoesNotReadOutsideDataRoot(t *testing.T) {
	base := t.TempDir()
	dataRoot := filepath.Join(base, "root")
	registered := drawer.Drawer{Dir: filepath.Join(dataRoot, "drawers", "api-3f2a9c1b"), Path: "/src/api", Name: "api"}
	// データルートの外に、引き出しと同じ形の秘密のファイルを置く。
	outside := drawer.Drawer{Dir: filepath.Join(base, "secret"), Path: "/secret", Name: "secret"}
	for _, d := range []drawer.Drawer{registered, outside} {
		if err := d.Register(); err != nil {
			t.Fatal(err)
		}
	}
	testutil.WriteFile(t, outside.NotesPath(), "SECRET")
	testutil.WriteFile(t, filepath.Join(outside.Dir, "sessions", "s1.next.json"), `{"summary":"SECRET"}`)
	testutil.WriteFile(t, filepath.Join(base, "s1.next.json"), `{"summary":"SECRET"}`)

	for _, key := range []string{
		"../../secret/s1",
		"../secret/s1",
		"..",
		"../..",
		"api-3f2a9c1b/../../../secret/sessions/s1",
		"api-3f2a9c1b/../../..",
		"api-3f2a9c1b/..",
		"api-3f2a9c1b/.",
		"api-3f2a9c1b/s1/x",
		"api-3f2a9c1b/*",
		"api-3f2a9c1b/",
		"api-3f2a9c1b",
		"/s1",
		"./s1",
		"../s1",
		filepath.Join(base, "secret") + "/s1",
		"",
	} {
		t.Run(key, func(t *testing.T) {
			got, err := Preview(dataRoot, key)

			if err == nil {
				t.Errorf("Preview(%q) = %q, want an error", key, got)
			}
			if strings.Contains(got, "SECRET") {
				t.Errorf("Preview(%q) read outside the data root: %q", key, got)
			}
		})
	}
}
