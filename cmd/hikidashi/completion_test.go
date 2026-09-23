package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/douhashi/hikidashi/internal/drawer"
)

// complete は hikidashi __complete を words で実行し、exit 0 で stderr に何も出さなかったことを確かめて stdout を返す。
func complete(t *testing.T, words ...string) string {
	t.Helper()
	code, stdout, stderr := invoke(commands, "", append([]string{"__complete"}, words...)...)
	if code != 0 || stderr != "" {
		t.Fatalf("__complete %q = %d, stderr %q, want 0 and silent", words, code, stderr)
	}
	return stdout
}

// registerAt は dataRoot に slug の引き出しを、name のリポジトリのルート path とともに登録して返す。
func registerAt(t *testing.T, dataRoot, name, slug, path string) drawer.Drawer {
	t.Helper()
	d := drawer.Drawer{Dir: filepath.Join(dataRoot, "drawers", slug), Path: path, Name: name}
	if err := d.Register(); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCompleteListsSubcommandsMatchingThePrefix(t *testing.T) {
	cmds := []command{
		{name: "add", summary: "register"},
		{name: "list", summary: "overview"},
		{name: "lint", summary: "check"},
	}
	for _, tc := range []struct{ prefix, want string }{
		{"", "add\tregister\nlist\toverview\nlint\tcheck\n"},
		{"li", "list\toverview\nlint\tcheck\n"},
		{"x", ""},
	} {
		code, stdout, stderr := invoke(cmds, "", "__complete", tc.prefix)

		if code != 0 || stdout != tc.want || stderr != "" {
			t.Errorf("__complete %q = %d, stdout %q, stderr %q, want 0 and %q", tc.prefix, code, stdout, stderr, tc.want)
		}
	}
}

func TestCompleteListsEveryCommandButNotItself(t *testing.T) {
	isolateHome(t)

	got := complete(t, "")

	var want strings.Builder
	for _, c := range commands {
		want.WriteString(c.name + "\t" + c.summary + "\n")
	}
	if got != want.String() {
		t.Errorf("stdout = %q, want %q", got, want.String())
	}
	if !strings.Contains(got, "completion\t") || strings.Contains(got, "__complete") {
		t.Errorf("stdout = %q, want completion listed and __complete hidden", got)
	}
}

func TestCompleteListsDrawersForCommandsTakingOne(t *testing.T) {
	dataRoot := isolateHome(t)
	home := filepath.Dir(dataRoot)
	registerAt(t, dataRoot, "web", "web-11111111", filepath.Join(home, "src", "web"))
	registerAt(t, dataRoot, "api", "api-22222222", "/srv/api")
	registerAt(t, dataRoot, "api", "api-11111111", filepath.Join(home, "a", "api"))
	// 名前が別の引き出しの slug と同じなら、名前では別の引き出しに当たるため slug を出す。
	registerAt(t, dataRoot, "web-11111111", "web-11111111-33333333", filepath.Join(home, "x", "web-11111111"))

	want := "api-11111111\t~/a/api\n" +
		"api-22222222\t/srv/api\n" +
		"web\t~/src/web\n" +
		"web-11111111-33333333\t~/x/web-11111111\n"
	for _, name := range []string{"open", "show", "remove"} {
		if got := complete(t, name, ""); got != want {
			t.Errorf("__complete %s = %q, want %q", name, got, want)
		}
	}
	if got, want := complete(t, "show", "ap"), "api-11111111\t~/a/api\napi-22222222\t/srv/api\n"; got != want {
		t.Errorf("__complete show ap = %q, want %q", got, want)
	}
}

func TestCompleteDrawerValuesResolveToTheirDrawer(t *testing.T) {
	dataRoot := isolateHome(t)
	home := filepath.Dir(dataRoot)
	registerAt(t, dataRoot, "web", "web-11111111", filepath.Join(home, "web"))
	registerAt(t, dataRoot, "api", "api-11111111", filepath.Join(home, "a", "api"))
	registerAt(t, dataRoot, "api", "api-22222222", filepath.Join(home, "b", "api"))
	registerAt(t, dataRoot, "web-11111111", "web-11111111-33333333", filepath.Join(home, "x", "web-11111111"))

	for line := range strings.Lines(complete(t, "open", "")) {
		value, desc, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		d, err := findDrawer(dataRoot, value)
		if err != nil {
			t.Errorf("findDrawer(%q) = %v", value, err)
			continue
		}
		if want := filepath.Join(home, strings.TrimPrefix(desc, "~/")); d.Path != want {
			t.Errorf("findDrawer(%q) = %s, want %s", value, d.Path, want)
		}
	}
}

func TestCompleteReflectsAddAndRemove(t *testing.T) {
	dataRoot, _ := addEnv(t)
	repo, d := newRepo(t, dataRoot)
	t.Chdir(repo)

	if got := complete(t, "open", ""); got != "" {
		t.Errorf("before add = %q, want empty", got)
	}
	if code, _, stderr := invoke(commands, "", "add"); code != 0 {
		t.Fatalf("add = %d, stderr %q", code, stderr)
	}
	if got, want := complete(t, "open", ""), d.Name+"\t"+d.Path+"\n"; got != want {
		t.Errorf("after add = %q, want %q", got, want)
	}
	if code, _, stderr := invoke(commands, "", "remove", d.Name); code != 0 {
		t.Fatalf("remove = %d, stderr %q", code, stderr)
	}
	if got := complete(t, "open", ""); got != "" {
		t.Errorf("after remove = %q, want empty", got)
	}
}

func TestCompleteListsNothingWhereNoArgumentIsTaken(t *testing.T) {
	dataRoot := isolateHome(t)
	registerAt(t, dataRoot, "api", "api-11111111", "/srv/api")

	for _, words := range [][]string{
		{"list", ""},
		{"add", ""},
		{"status", "a"},
		{"unknown", ""},
		{"open", "api", ""},
		{"completion", "zsh", ""},
	} {
		if got := complete(t, words...); got != "" {
			t.Errorf("__complete %q = %q, want empty", words, got)
		}
	}
}

func TestCompleteListsShellsForCompletion(t *testing.T) {
	isolateHome(t)

	if got, want := complete(t, "completion", ""), "zsh\tzsh completion script\nbash\tbash completion script\n"; got != want {
		t.Errorf("__complete completion = %q, want %q", got, want)
	}
}

func TestCompleteFailsSilentlyOnStdout(t *testing.T) {
	t.Setenv("HOME", "")

	code, stdout, stderr := invoke(commands, "", "__complete", "open", "")

	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "hikidashi __complete: ") {
		t.Errorf("__complete = %d, stdout %q, stderr %q, want 1, nothing on stdout and the reason", code, stdout, stderr)
	}
}

func TestCompleteRequiresAWord(t *testing.T) {
	code, stdout, stderr := invoke(commands, "", "__complete")

	if code != 2 || stdout != "" || stderr != "Usage: hikidashi __complete <word>...\n" {
		t.Errorf("__complete = %d, stdout %q, stderr %q, want 2 and usage", code, stdout, stderr)
	}
}

func TestUsageHidesComplete(t *testing.T) {
	_, stdout, _ := invoke(commands, "", "help")

	if strings.Contains(stdout, "__complete") {
		t.Errorf("usage = %q, want __complete hidden", stdout)
	}
}

func TestCompletionPrintsTheScriptOfTheShell(t *testing.T) {
	for shell, marker := range map[string]string{
		"zsh":  "compdef _hikidashi hikidashi",
		"bash": "complete -F _hikidashi hikidashi",
	} {
		code, stdout, stderr := invoke(commands, "", "completion", shell)

		if code != 0 || stderr != "" || !strings.Contains(stdout, marker) || !strings.Contains(stdout, "hikidashi __complete") {
			t.Errorf("completion %s = %d, stdout %q, stderr %q, want 0 and the script", shell, code, stdout, stderr)
		}
	}
}

func TestCompletionRejectsOtherArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"fish"}, {"zsh", "bash"}} {
		code, stdout, stderr := invoke(commands, "", append([]string{"completion"}, args...)...)

		if code != 2 || stdout != "" || stderr != "Usage: hikidashi completion <zsh|bash>\n" {
			t.Errorf("completion %q = %d, stdout %q, stderr %q, want 2 and usage", args, code, stdout, stderr)
		}
	}
}

func TestTildePath(t *testing.T) {
	for name, tc := range map[string]struct{ path, home, want string }{
		"under home":             {"/home/a/x", "/home/a", "~/x"},
		"home itself":            {"/home/a", "/home/a", "~"},
		"outside home":           {"/src/x", "/home/a", "/src/x"},
		"sibling sharing prefix": {"/home/ab", "/home/a", "/home/ab"},
		"home with trailing /":   {"/home/a/x", "/home/a/", "~/x"},
		"home is root":           {"/x", "/", "/x"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tildePath(tc.path, tc.home); got != tc.want {
				t.Errorf("tildePath(%q, %q) = %q, want %q", tc.path, tc.home, got, tc.want)
			}
		})
	}
}
