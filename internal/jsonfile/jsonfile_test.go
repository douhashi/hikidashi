package jsonfile

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestWriteCreatesIndentedJSONReadableOnlyByOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")

	if err := Write(path, sample{Name: "x", Count: 1}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	assertContent(t, path, "{\n  \"name\": \"x\",\n  \"count\": 1\n}\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
}

func TestWriteReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	writeFile(t, path, "old\n")

	if err := Write(path, sample{Name: "new"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	assertContent(t, path, "{\n  \"name\": \"new\",\n  \"count\": 0\n}\n")
	assertEntries(t, dir, "a.json")
}

func TestWriteKeepsExistingFileWhenEncodingFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	writeFile(t, path, "old\n")

	if err := Write(path, math.Inf(1)); err == nil {
		t.Fatal("Write succeeded, want an encoding error")
	}

	assertContent(t, path, "old\n")
	assertEntries(t, dir, "a.json")
}

func TestWriteKeepsExistingFileWhenDirectoryIsNotWritable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	writeFile(t, path, "old\n")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := Write(path, sample{Name: "new"}); err == nil {
		t.Fatal("Write succeeded, want a permission error")
	}

	assertContent(t, path, "old\n")
	assertEntries(t, dir, "a.json")
}

func TestWriteRemovesTemporaryFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	// 空でないディレクトリへの rename は失敗する。一時ファイルを書いた後の失敗を起こすために使う。
	writeFile(t, filepath.Join(path, "keep"), "")

	if err := Write(path, sample{Name: "new"}); err == nil {
		t.Fatal("Write succeeded, want a rename error")
	}

	assertEntries(t, dir, "a.json")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

// assertEntries は dir の直下が want だけであること、つまり一時ファイルが残っていないことを確かめる。
func assertEntries(t *testing.T, dir string, want ...string) {
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
