// Package testutil はテストが共通に使う準備と検査をまとめる。テストからだけ使う。
package testutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// IsolateGit はテスト中の git をユーザー・システムの設定から切り離し、コミットの作者を固定する。
func IsolateGit(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":   "/dev/null",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "hikidashi",
		"GIT_AUTHOR_EMAIL":    "hikidashi@example.com",
		"GIT_COMMITTER_NAME":  "hikidashi",
		"GIT_COMMITTER_EMAIL": "hikidashi@example.com",
	} {
		t.Setenv(k, v)
	}
}

// NewRepo は dir にコミットを 1 つ持つリポジトリを作り、dir を返す。
func NewRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q")
	Git(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
	return dir
}

// Git は dir で git を実行し、失敗したらテストを止める。
func Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %q: %v\n%s", args, err, out)
	}
}

// WriteFile は親ディレクトリを作ってから path に content を書く。
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ReadFile は path の中身を返す。
func ReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// AssertEntries は dir の直下の名前が want（名前順）だけであることを確かめる。want が無ければ dir は空である。
func AssertEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, want) {
		t.Errorf("entries of %s = %q, want %q", dir, got, want)
	}
}

// AssertNotExist は path が存在しないことを確かめる。
func AssertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s exists (err %v), want it absent", path, err)
	}
}

// AssertPerm は path のパーミッションが want であることを確かめる。
func AssertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %o, want %o", path, got, want)
	}
}

// StartClaude は sleep の実行ファイルを claude という名前で複製して起動し、その PID を返す。
// /proc/<pid>/comm が claude になるため、Claude Code 本体の生存確認の対象になる。プロセスはテストの終わりに止める。
func StartClaude(t *testing.T) int {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(claude, data, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(claude, "3600")
	// comm は実行ファイルの名前から決まる。argv[0] は sleep のままにし、argv[0] で動作を選ぶ
	// マルチコール版の coreutils（uutils・busybox）でも sleep として動かす。
	cmd.Args[0] = "sleep"
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// DeadPID は終了して回収済みのプロセスの PID を返す。SIGKILL 等で消えた claude を模す。
func DeadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// FakeClaude は PATH の先頭に置いた偽の claude コマンド。外部境界である Claude Code の CLI（claude -p）を
// テストで模す唯一の置き場で、呼ばれるたびに引数・環境変数・作業ディレクトリ・stdin を記録し、決めた応答を返す。
type FakeClaude struct {
	dir string
}

// ClaudeCall は偽の claude が受けた 1 回の呼び出し。
type ClaudeCall struct {
	Args  []string
	Env   map[string]string
	Dir   string
	Stdin string
}

// fakeClaudeScript は偽の claude の本体。%s はデータのディレクトリ（単一引用符で囲める値）。
// 呼び出しごとに call<N>/ を mkdir で排他的に取って記録し、hold がある間は待つ。
// 別の呼び出しが走っている間に呼ばれたら overlapped を残す。
const fakeClaudeScript = `#!/bin/sh
d='%s'
mkdir "$d/running" 2>/dev/null || : >"$d/overlapped"
n=1
while ! mkdir "$d/call$n" 2>/dev/null; do n=$((n + 1)); done
c="$d/call$n"
printf '%%s\0' "$@" >"$c/args"
env -0 >"$c/env"
pwd -P >"$c/dir"
cat >"$c/stdin"
if [ -e "$d/hold" ]; then
	: >"$c/held"
	while [ -e "$d/hold" ]; do sleep 0.01; done
fi
rmdir "$d/running" 2>/dev/null
cat "$d/stdout"
exit "$(cat "$d/code")"
`

// NewFakeClaude は偽の claude を PATH の先頭に置く。応答は Respond で決めるまで、空の stdout と exit 0 とする。
func NewFakeClaude(t *testing.T) *FakeClaude {
	t.Helper()
	bin, dir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), fmt.Appendf(nil, fakeClaudeScript, dir), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := &FakeClaude{dir: dir}
	f.Respond(t, "", 0)
	return f
}

// Respond は以降の呼び出しで、stdout に出す中身と終了コードを決める。
func (f *FakeClaude) Respond(t *testing.T, stdout string, code int) {
	t.Helper()
	WriteFile(t, filepath.Join(f.dir, "stdout"), stdout)
	WriteFile(t, filepath.Join(f.dir, "code"), strconv.Itoa(code))
}

// Hold は以降の呼び出しを、返した release が呼ばれるまで応答させずに待たせる。
func (f *FakeClaude) Hold(t *testing.T) (release func()) {
	t.Helper()
	hold := filepath.Join(f.dir, "hold")
	WriteFile(t, hold, "")
	return func() {
		if err := os.Remove(hold); err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Error(err)
		}
	}
}

// WaitHeld は n 回目の呼び出しが Hold で待ち始めるまで待つ。5 秒で来なければテストを止める。
func (f *FakeClaude) WaitHeld(t *testing.T, n int) {
	t.Helper()
	held := filepath.Join(f.dir, "call"+strconv.Itoa(n), "held")
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(held); err == nil {
			return
		}
	}
	t.Fatalf("claude call %d was not held within 5s", n)
}

// Overlapped は、ある呼び出しが走っている間に別の呼び出しが始まったことがあるかを返す。
func (f *FakeClaude) Overlapped() bool {
	_, err := os.Stat(filepath.Join(f.dir, "overlapped"))
	return err == nil
}

// Calls はこれまでの呼び出しを呼ばれた順に返す。
func (f *FakeClaude) Calls(t *testing.T) []ClaudeCall {
	t.Helper()
	var calls []ClaudeCall
	for n := 1; ; n++ {
		c := filepath.Join(f.dir, "call"+strconv.Itoa(n))
		if _, err := os.Stat(c); errors.Is(err, fs.ErrNotExist) {
			return calls
		}
		env := map[string]string{}
		for _, kv := range nulSplit(ReadFile(t, filepath.Join(c, "env"))) {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		calls = append(calls, ClaudeCall{
			Args:  nulSplit(ReadFile(t, filepath.Join(c, "args"))),
			Env:   env,
			Dir:   strings.TrimSuffix(ReadFile(t, filepath.Join(c, "dir")), "\n"),
			Stdin: ReadFile(t, filepath.Join(c, "stdin")),
		})
	}
}

// nulSplit は NUL で終わる要素の並びを分ける。
func nulSplit(s string) []string {
	items := strings.Split(s, "\x00")
	return items[:len(items)-1]
}
