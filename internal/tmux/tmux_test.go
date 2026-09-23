package tmux

import (
	"slices"
	"testing"

	"github.com/douhashi/hikidashi/internal/testutil"
)

// assertCalls は偽の tmux が want の引数でこの順に呼ばれたことを確かめる。
func assertCalls(t *testing.T, fake *testutil.FakeTmux, want ...[]string) {
	t.Helper()
	if got := fake.Calls(t); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("tmux calls = %q, want %q", got, want)
	}
}

// assertError は err が want のメッセージであることを確かめる。
func assertError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestHasSessionMatchesExactName(t *testing.T) {
	fake := testutil.NewFakeTmux(t)
	fake.AddSession(t, "api-3f2a9c1b")

	for name, want := range map[string]bool{"api-3f2a9c1b": true, "api": false} {
		got, err := HasSession(name)

		if err != nil || got != want {
			t.Errorf("HasSession(%q) = %v, %v, want %v", name, got, err, want)
		}
	}
	assertCalls(t, fake, []string{"has-session", "-t", "=api-3f2a9c1b"}, []string{"has-session", "-t", "=api"})
}

func TestHasSessionTreatsExitOneAsAbsent(t *testing.T) {
	// サーバー未起動も exit 1 で終わる。
	testutil.NewFakeTmux(t).Fail(t, "no server running on /tmp/tmux-1000/default", 1)

	got, err := HasSession("api")

	if err != nil || got {
		t.Errorf("HasSession = %v, %v, want false, nil", got, err)
	}
}

func TestHasSessionFailsOnOtherExit(t *testing.T) {
	testutil.NewFakeTmux(t).Fail(t, "boom", 2)

	_, err := HasSession("api")

	assertError(t, err, "tmux has-session -t =api: exit status 2: boom")
}

func TestNewSessionStartsDetachedInDir(t *testing.T) {
	fake := testutil.NewFakeTmux(t)

	if err := NewSession("api-3f2a9c1b", "/src/api"); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake, []string{"new-session", "-d", "-s", "api-3f2a9c1b", "-c", "/src/api"})
	if ok, err := HasSession("api-3f2a9c1b"); err != nil || !ok {
		t.Errorf("HasSession after NewSession = %v, %v, want true", ok, err)
	}
}

func TestNewSessionFailsWithStderr(t *testing.T) {
	testutil.NewFakeTmux(t).Fail(t, "duplicate session: api", 1)

	err := NewSession("api", "/src/api")

	assertError(t, err, "tmux new-session -d -s api -c /src/api: exit status 1: duplicate session: api")
}

func TestSwitchClientMovesToPane(t *testing.T) {
	fake := testutil.NewFakeTmux(t)

	if err := SwitchClient("%1"); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake, []string{"switch-client", "-t", "%1"})
}

func TestSwitchClientFailsWithStderr(t *testing.T) {
	testutil.NewFakeTmux(t).Fail(t, "can't find pane: %9", 1)

	err := SwitchClient("%9")

	assertError(t, err, "tmux switch-client -t %9: exit status 1: can't find pane: %9")
}

func TestRunFailsWhenTmuxCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := HasSession("api")

	if err == nil {
		t.Error("HasSession without tmux succeeded, want an error")
	}
}
