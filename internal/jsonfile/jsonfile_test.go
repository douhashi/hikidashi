package jsonfile

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/douhashi/hikidashi/internal/testutil"
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
	testutil.WriteFile(t, path, "old\n")

	if err := Write(path, sample{Name: "new"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	assertContent(t, path, "{\n  \"name\": \"new\",\n  \"count\": 0\n}\n")
	testutil.AssertEntries(t, dir, "a.json")
}

func TestWriteKeepsExistingFileWhenEncodingFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	testutil.WriteFile(t, path, "old\n")

	if err := Write(path, math.Inf(1)); err == nil {
		t.Fatal("Write succeeded, want an encoding error")
	}

	assertContent(t, path, "old\n")
	testutil.AssertEntries(t, dir, "a.json")
}

func TestWriteKeepsExistingFileWhenDirectoryIsNotWritable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	testutil.WriteFile(t, path, "old\n")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := Write(path, sample{Name: "new"}); err == nil {
		t.Fatal("Write succeeded, want a permission error")
	}

	assertContent(t, path, "old\n")
	testutil.AssertEntries(t, dir, "a.json")
}

func TestWriteRemovesTemporaryFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.json")
	// 空でないディレクトリへの rename は失敗する。一時ファイルを書いた後の失敗を起こすために使う。
	testutil.WriteFile(t, filepath.Join(path, "keep"), "")

	if err := Write(path, sample{Name: "new"}); err == nil {
		t.Fatal("Write succeeded, want a rename error")
	}

	testutil.AssertEntries(t, dir, "a.json")
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

func TestReadDecodesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.json")
	testutil.WriteFile(t, path, `{"name":"x","count":2}`)

	got, ok, err := Read[sample](path)

	if err != nil || !ok {
		t.Fatalf("Read = ok %v, err %v, want the decoded value", ok, err)
	}
	if want := (sample{Name: "x", Count: 2}); got != want {
		t.Errorf("Read = %+v, want %+v", got, want)
	}
}

func TestReadTreatsMissingOrBrokenFileAsAbsent(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.json")
	testutil.WriteFile(t, broken, `{"name":"x",`)

	for name, path := range map[string]string{
		"missing file":      filepath.Join(dir, "missing.json"),
		"missing directory": filepath.Join(dir, "no", "a.json"),
		"broken file":       broken,
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := Read[sample](path)

			if ok || err != nil || got != (sample{}) {
				t.Errorf("Read = %+v, ok %v, err %v, want zero, false, nil", got, ok, err)
			}
		})
	}
}

func TestReadFailsWhenFileCannotBeRead(t *testing.T) {
	// ディレクトリを読ませ、無い・壊れているのどちらとも違う読み込みの失敗を起こす。
	if _, _, err := Read[sample](t.TempDir()); err == nil {
		t.Error("Read succeeded, want an error")
	}
}
