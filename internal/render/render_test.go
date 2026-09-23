package render

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
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
