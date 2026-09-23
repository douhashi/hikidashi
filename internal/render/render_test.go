package render

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

func TestAgeTruncatesToLargestUnit(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-time.Minute:                  "0m",
		59*time.Minute + time.Second:  "59m",
		time.Hour:                     "1h",
		23*time.Hour + 59*time.Minute: "23h",
		24 * time.Hour:                "1d",
		49 * time.Hour:                "2d",
	} {
		if got := Age(d); got != want {
			t.Errorf("Age(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestOneLineReplacesControlCharacters(t *testing.T) {
	if got, want := OneLine("a\nb\tc\x1b[31md"), "a b c [31md"; got != want {
		t.Errorf("OneLine = %q, want %q", got, want)
	}
}

func TestWriteNextWritesEveryFieldOrNotExtracted(t *testing.T) {
	generated := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		next session.Next
		ok   bool
		want string
	}{
		"extracted": {
			next: session.Next{Summary: "s", HumanNext: "h", Blockers: []string{"b1", "b2"}, GeneratedAt: generated},
			ok:   true,
			want: "summary:      s\nhuman_next:   h\nclaude_next:  -\nblockers:\n  - b1\n  - b2\n" +
				"generated_at: " + generated.Local().Format(time.DateTime) + "\n",
		},
		"empty":         {ok: true, want: "summary:      -\nhuman_next:   -\nclaude_next:  -\nblockers:     -\ngenerated_at: -\n"},
		"not extracted": {want: "(next action not extracted yet)\n"},
	} {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder

			WriteNext(&b, tc.next, tc.ok)

			if b.String() != tc.want {
				t.Errorf("WriteNext =\n%s\nwant\n%s", b.String(), tc.want)
			}
		})
	}
}

func TestNotesReadsNotesOrSaysNone(t *testing.T) {
	d := drawer.Drawer{Dir: filepath.Join(t.TempDir(), "api-3f2a9c1b")}

	if got, err := Notes(d); err != nil || got != "(no notes)\n" {
		t.Errorf("Notes without notes.md = %q, err %v, want (no notes)", got, err)
	}
	testutil.WriteFile(t, d.NotesPath(), " \n\t\n")
	if got, err := Notes(d); err != nil || got != "(no notes)\n" {
		t.Errorf("Notes of blank notes.md = %q, err %v, want (no notes)", got, err)
	}
	testutil.WriteFile(t, d.NotesPath(), "本番は触らない\n")
	if got, err := Notes(d); err != nil || got != "本番は触らない\n" {
		t.Errorf("Notes = %q, err %v, want the notes", got, err)
	}
}
