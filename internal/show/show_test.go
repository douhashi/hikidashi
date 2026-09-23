package show

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/testutil"
)

func TestLinesAlignColumnsAndMarkUnknownIssues(t *testing.T) {
	summaries := []Summary{
		{Drawer: drawer.Drawer{Dir: "/data/drawers/api-3f2a9c1b", Name: "api"}, Issues: Issues{Count: 12}, Running: 1, Waiting: 2, Idle: 3},
		{Drawer: drawer.Drawer{Dir: "/data/drawers/frontend-0a1b2c3d", Name: "frontend"}, Issues: Issues{Err: errors.New("no remote")}},
		{Drawer: drawer.Drawer{Dir: "/data/drawers/x-00000000", Name: "x\x1b[31m"}},
	}

	got := Lines(summaries)

	want := []string{
		"api       api-3f2a9c1b       issues:12  running:1  waiting:2  idle:3",
		"frontend  frontend-0a1b2c3d  issues:?  running:0  waiting:0  idle:0",
		"x [31m    x-00000000         issues:0  running:0  waiting:0  idle:0",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Lines =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSummariesAreInNameOrder(t *testing.T) {
	testutil.NewFakeGh(t)
	dataRoot := t.TempDir()
	// ディレクトリ名の順（a+-… が a-… より前）と名前の順（a が a+ より前）が異なる。
	for _, name := range []string{"a+", "a"} {
		d := drawer.Drawer{Dir: filepath.Join(dataRoot, "drawers", name+"-0123abcd"), Path: t.TempDir(), Name: name}
		if err := d.Register(); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Summaries(dataRoot)

	if err != nil {
		t.Fatalf("Summaries: %v", err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Drawer.Name)
	}
	if want := []string{"a", "a+"}; !slices.Equal(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
}
