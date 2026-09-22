// Package testutil はテストが共通に使う準備と検査をまとめる。テストからだけ使う。
package testutil

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
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
