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

func TestEnsureCreatesMissingSessionInDir(t *testing.T) {
	fake := testutil.NewFakeTmux(t)

	created, err := Ensure("api-3f2a9c1b", "/src/api")

	if err != nil || !created {
		t.Errorf("Ensure = %v, %v, want true, nil", created, err)
	}
	assertCalls(t, fake,
		[]string{"has-session", "-t", "=api-3f2a9c1b"},
		[]string{"new-session", "-d", "-s", "api-3f2a9c1b", "-c", "/src/api"})
}

func TestEnsureKeepsExistingSession(t *testing.T) {
	fake := testutil.NewFakeTmux(t)
	fake.AddSession(t, "api-3f2a9c1b")

	created, err := Ensure("api-3f2a9c1b", "/src/api")

	if err != nil || created {
		t.Errorf("Ensure = %v, %v, want false, nil", created, err)
	}
	assertCalls(t, fake, []string{"has-session", "-t", "=api-3f2a9c1b"})
}

func TestEnsureMatchesExactName(t *testing.T) {
	// = を付けないと、tmux は api で始まる api-3f2a9c1b にも一致させる。
	fake := testutil.NewFakeTmux(t)
	fake.AddSession(t, "api-3f2a9c1b")

	created, err := Ensure("api", "/src/api")

	if err != nil || !created {
		t.Errorf("Ensure = %v, %v, want true, nil", created, err)
	}
	assertCalls(t, fake,
		[]string{"has-session", "-t", "=api"},
		[]string{"new-session", "-d", "-s", "api", "-c", "/src/api"})
}

func TestEnsureCreatesWhenServerIsNotRunning(t *testing.T) {
	// サーバー未起動の has-session も exit 1 で終わり、new-session がサーバーを起動する。
	fake := testutil.NewFakeTmux(t)
	fake.Fail(t, "has-session", "no server running on /tmp/tmux-1000/default", 1)

	created, err := Ensure("api", "/src/api")

	if err != nil || !created {
		t.Errorf("Ensure = %v, %v, want true, nil", created, err)
	}
	assertCalls(t, fake,
		[]string{"has-session", "-t", "=api"},
		[]string{"new-session", "-d", "-s", "api", "-c", "/src/api"})
}

func TestEnsureFailsWithStderr(t *testing.T) {
	for name, tc := range map[string]struct {
		code int
		want string
	}{
		"has-session": {2, "tmux has-session -t =api: exit status 2: boom"},
		"new-session": {1, "tmux new-session -d -s api -c /src/api: exit status 1: boom"},
	} {
		t.Run(name, func(t *testing.T) {
			testutil.NewFakeTmux(t).Fail(t, name, "boom", tc.code)

			_, err := Ensure("api", "/src/api")

			assertError(t, err, tc.want)
		})
	}
}

func TestSwitchClientMatchesExactSession(t *testing.T) {
	fake := testutil.NewFakeTmux(t)
	fake.AddSession(t, "api-3f2a9c1b")

	if err := SwitchClient("api-3f2a9c1b"); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake, []string{"switch-client", "-t", "=api-3f2a9c1b"})
}

func TestSwitchClientFailsWithStderr(t *testing.T) {
	// 偽の tmux は、無いセッションへの switch-client を実物と同じく失敗させる。
	testutil.NewFakeTmux(t)

	err := SwitchClient("api")

	assertError(t, err, "tmux switch-client -t =api: exit status 1: can't find session: api")
}

func TestAttachMatchesExactSession(t *testing.T) {
	fake := testutil.NewFakeTmux(t)
	fake.AddSession(t, "api-3f2a9c1b")

	if err := Attach("api-3f2a9c1b"); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake, []string{"attach-session", "-t", "=api-3f2a9c1b"})
}

func TestAttachFails(t *testing.T) {
	testutil.NewFakeTmux(t).Fail(t, "attach-session", "open terminal failed: not a terminal", 1)

	err := Attach("api")

	assertError(t, err, "tmux attach-session -t =api: exit status 1")
}

func TestRunFailsWhenTmuxCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := Ensure("api", "/src/api")

	if err == nil {
		t.Error("Ensure without tmux succeeded, want an error")
	}
}
