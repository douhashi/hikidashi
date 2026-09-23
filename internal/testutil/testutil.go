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
// プロセス名が claude になるため、Claude Code 本体の生存確認の対象になる。プロセスはテストの終わりに止める。
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
	for _, c := range callDirs(f.dir) {
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
	return calls
}

// FakeTmux は PATH の先頭に置いた偽の tmux コマンド。外部境界である tmux をテストで模す唯一の置き場で、
// 呼ばれるたびに引数を記録する。既存のセッションを覚え、has-session はその有無で、new-session はその追加で応え、
// switch-client・attach-session は無いセッションを指されたら実物と同じく失敗する。
type FakeTmux struct {
	dir string
}

// fakeTmuxScript は偽の tmux の本体。%s はデータのディレクトリ（単一引用符で囲める値）。
// fail-<サブコマンド> があれば、そのサブコマンドの呼び出しは stderr-<サブコマンド> を出して fail-<サブコマンド> の終了コードで終わる。
// 引数の位置は internal/tmux が組み立てる形（<サブコマンド> -t =<name> / new-session -d -s <name> -c <dir>）に合わせる。
const fakeTmuxScript = `#!/bin/sh
d='%s'
n=1
while ! mkdir "$d/call$n" 2>/dev/null; do n=$((n + 1)); done
printf '%%s\0' "$@" >"$d/call$n/args"
if [ -e "$d/fail-$1" ]; then
	cat "$d/stderr-$1" >&2
	exit "$(cat "$d/fail-$1")"
fi
case "$1" in
has-session | switch-client | attach-session)
	[ -e "$d/sessions/${3#=}" ] && exit 0
	echo "can't find session: ${3#=}" >&2
	exit 1
	;;
new-session) : >"$d/sessions/$4" ;;
esac
`

// NewFakeTmux は既存のセッションが無い偽の tmux を PATH の先頭に置く。
func NewFakeTmux(t *testing.T) *FakeTmux {
	t.Helper()
	bin, dir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tmux"), fmt.Appendf(nil, fakeTmuxScript, dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &FakeTmux{dir: dir}
}

// AddSession は name のセッションを既存にする。
func (f *FakeTmux) AddSession(t *testing.T, name string) {
	t.Helper()
	WriteFile(t, filepath.Join(f.dir, "sessions", name), "")
}

// Fail は以降の subcommand の呼び出しを、stderr に msg を出して code で終わらせる。
func (f *FakeTmux) Fail(t *testing.T, subcommand, msg string, code int) {
	t.Helper()
	WriteFile(t, filepath.Join(f.dir, "stderr-"+subcommand), msg+"\n")
	WriteFile(t, filepath.Join(f.dir, "fail-"+subcommand), strconv.Itoa(code))
}

// Calls はこれまでの呼び出しの引数を呼ばれた順に返す。
func (f *FakeTmux) Calls(t *testing.T) [][]string {
	t.Helper()
	var calls [][]string
	for _, c := range callDirs(f.dir) {
		calls = append(calls, nulSplit(ReadFile(t, filepath.Join(c, "args"))))
	}
	return calls
}

// FakeGh は PATH の先頭に置いた偽の gh コマンド。外部境界である GitHub CLI をテストで模す唯一の置き場で、
// 呼ばれるたびに引数と作業ディレクトリを記録する。応答は作業ディレクトリ（リポジトリ）ごとに決める。
type FakeGh struct {
	dir string
}

// GhCall は偽の gh が受けた 1 回の呼び出し。
type GhCall struct {
	Args []string
	Dir  string
}

// fakeGhScript は偽の gh の本体。%s はデータのディレクトリ（単一引用符で囲める値）。
// 応答は repos/<作業ディレクトリ>/ に置く。hang があれば止められるまで待ち、
// 無ければ stdout と stderr を出して code で終わる。応答の無いリポジトリでは、GitHub のリモートが無いときの gh を模す。
// 本物の gh と同じく、CLICOLOR_FORCE が 0 以外なら --json の出力にも色を付ける。
const fakeGhScript = `#!/bin/sh
d='%s'
n=1
while ! mkdir "$d/call$n" 2>/dev/null; do n=$((n + 1)); done
printf '%%s\0' "$@" >"$d/call$n/args"
pwd -P >"$d/call$n/dir"
r="$d/repos$(pwd -P)"
[ -e "$r/hang" ] && exec sleep 60
if [ ! -e "$r/code" ]; then
	echo "none of the git remotes configured for this repository point to a known GitHub host" >&2
	exit 1
fi
[ -n "$CLICOLOR_FORCE" ] && [ "$CLICOLOR_FORCE" != 0 ] && printf '\033[1;37m'
cat "$r/stdout"
cat "$r/stderr" >&2
exit "$(cat "$r/code")"
`

// NewFakeGh は、どのリポジトリにも GitHub のリモートが無いものとして応える偽の gh を PATH の先頭に置く。
func NewFakeGh(t *testing.T) *FakeGh {
	t.Helper()
	bin, dir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), fmt.Appendf(nil, fakeGhScript, dir), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &FakeGh{dir: dir}
}

// OpenIssues は repo で実行された gh repo view --json issues に、Open な Issue が n 件あると応えさせる。
func (f *FakeGh) OpenIssues(t *testing.T, repo string, n int) {
	t.Helper()
	f.respond(t, repo, fmt.Sprintf(`{"issues":{"totalCount":%d}}`+"\n", n), "", 0)
}

// Fail は repo での呼び出しを、stderr に msg を出して code で終わらせる。
func (f *FakeGh) Fail(t *testing.T, repo, msg string, code int) {
	t.Helper()
	f.respond(t, repo, "", msg+"\n", code)
}

// Respond は repo での呼び出しに、stdout と stderr を出して code で終わらせる。
func (f *FakeGh) Respond(t *testing.T, repo, stdout string, code int) {
	t.Helper()
	f.respond(t, repo, stdout, "", code)
}

// Hang は repo での呼び出しを、止められるまで応答させない。
func (f *FakeGh) Hang(t *testing.T, repo string) {
	t.Helper()
	WriteFile(t, filepath.Join(f.repoDir(t, repo), "hang"), "")
}

func (f *FakeGh) respond(t *testing.T, repo, stdout, stderr string, code int) {
	t.Helper()
	r := f.repoDir(t, repo)
	WriteFile(t, filepath.Join(r, "stdout"), stdout)
	WriteFile(t, filepath.Join(r, "stderr"), stderr)
	WriteFile(t, filepath.Join(r, "code"), strconv.Itoa(code))
}

// repoDir は repo の応答を置くディレクトリ。偽の gh が pwd -P で引けるよう、シンボリックリンクを解決する。
func (f *FakeGh) repoDir(t *testing.T, repo string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(f.dir, "repos", real)
}

// Calls はこれまでの呼び出しを呼ばれた順に返す。
func (f *FakeGh) Calls(t *testing.T) []GhCall {
	t.Helper()
	var calls []GhCall
	for _, c := range callDirs(f.dir) {
		calls = append(calls, GhCall{
			Args: nulSplit(ReadFile(t, filepath.Join(c, "args"))),
			Dir:  strings.TrimSuffix(ReadFile(t, filepath.Join(c, "dir")), "\n"),
		})
	}
	return calls
}

// callDirs は偽のコマンドが dir に残した呼び出しごとの記録（call<N>/）を、呼ばれた順に返す。
func callDirs(dir string) []string {
	var dirs []string
	for n := 1; ; n++ {
		c := filepath.Join(dir, "call"+strconv.Itoa(n))
		if _, err := os.Stat(c); errors.Is(err, fs.ErrNotExist) {
			return dirs
		}
		dirs = append(dirs, c)
	}
}

// nulSplit は NUL で終わる要素の並びを分ける。
func nulSplit(s string) []string {
	items := strings.Split(s, "\x00")
	return items[:len(items)-1]
}
