package drawer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/testutil"
)

func TestDefaultRootIsUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot: %v", err)
	}
	if want := filepath.Join(home, ".hikidashi"); got != want {
		t.Errorf("DefaultRoot = %q, want %q", got, want)
	}
}

func TestResolveUnifiesWorktreeSubdirectoryAndSymlink(t *testing.T) {
	testutil.IsolateGit(t)
	base := t.TempDir()
	repo := testutil.NewRepo(t, filepath.Join(base, "api"))
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(base, "api-wt")
	testutil.Git(t, repo, "worktree", "add", "-q", worktree)
	link := filepath.Join(base, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	dataRoot := t.TempDir()
	want := expectedDrawer(t, dataRoot, repo)

	for _, cwd := range []string{repo, sub, worktree, link} {
		t.Run(filepath.Base(cwd), func(t *testing.T) {
			got := mustResolve(t, dataRoot, cwd)

			if got != want {
				t.Errorf("Resolve(%q) = %+v, want %+v", cwd, got, want)
			}
		})
	}
}

func TestResolveSeparatesSameNameAtDifferentPaths(t *testing.T) {
	testutil.IsolateGit(t)
	base := t.TempDir()
	first := testutil.NewRepo(t, filepath.Join(base, "x", "app"))
	second := testutil.NewRepo(t, filepath.Join(base, "y", "app"))
	dataRoot := t.TempDir()

	a := mustResolve(t, dataRoot, first)
	b := mustResolve(t, dataRoot, second)

	if a.Name != "app" || b.Name != "app" {
		t.Errorf("names = %q, %q, want both %q", a.Name, b.Name, "app")
	}
	if a.Dir == b.Dir {
		t.Errorf("both repositories resolved to %q, want different drawers", a.Dir)
	}
}

func TestResolveSubmoduleToItsOwnRoot(t *testing.T) {
	testutil.IsolateGit(t)
	base := t.TempDir()
	lib := testutil.NewRepo(t, filepath.Join(base, "lib"))
	super := testutil.NewRepo(t, filepath.Join(base, "super"))
	testutil.Git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "mods/lib")
	submodule := filepath.Join(super, "mods", "lib")
	dataRoot := t.TempDir()

	got := mustResolve(t, dataRoot, submodule)

	if want := expectedDrawer(t, dataRoot, submodule); got != want {
		t.Errorf("Resolve = %+v, want %+v", got, want)
	}
}

func TestResolveDoesNotTrackOutsideWorkTree(t *testing.T) {
	testutil.IsolateGit(t)
	base := t.TempDir()
	plain := filepath.Join(base, "plain")
	if err := os.Mkdir(plain, 0o700); err != nil {
		t.Fatal(err)
	}
	repo := testutil.NewRepo(t, filepath.Join(base, "repo"))
	bare := filepath.Join(base, "bare.git")
	testutil.Git(t, base, "init", "-q", "--bare", bare)

	for name, cwd := range map[string]string{
		"not a repository": plain,
		"bare repository":  bare,
		"inside .git":      filepath.Join(repo, ".git"),
		"missing cwd":      filepath.Join(base, "missing"),
	} {
		t.Run(name, func(t *testing.T) {
			dataRoot := t.TempDir()

			_, ok, err := Resolve(dataRoot, cwd)

			if ok || err != nil {
				t.Errorf("Resolve(%q) = ok %v, err %v, want ok false, err nil", cwd, ok, err)
			}
			testutil.AssertEntries(t, dataRoot)
		})
	}
}

func TestResolveFailsWhenGitCannotStart(t *testing.T) {
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "repo"))
	t.Setenv("PATH", t.TempDir())

	if _, _, err := Resolve(t.TempDir(), repo); err == nil {
		t.Error("Resolve succeeded without git, want an error")
	}
}

func TestRegisterWritesDrawerJSONReadableOnlyByOwner(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), ".hikidashi")
	d := Drawer{Dir: filepath.Join(dataRoot, "drawers", "api-3f2a9c1b"), Path: "/src/api", Name: "api"}
	before := time.Now()

	if err := d.Register(); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var got Drawer
	data, err := os.ReadFile(filepath.Join(d.Dir, "drawer.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("drawer.json = %s: %v", data, err)
	}
	if got.Path != d.Path || got.Name != d.Name || got.Dir != "" {
		t.Errorf("drawer.json = %s, want path %q and name %q only", data, d.Path, d.Name)
	}
	if got.CreatedAt.Before(before) || got.CreatedAt.After(time.Now()) {
		t.Errorf("created_at = %v, want the time of registration", got.CreatedAt)
	}
	for _, dir := range []string{dataRoot, filepath.Dir(d.Dir), d.Dir} {
		testutil.AssertPerm(t, dir, 0o700)
	}
	testutil.AssertPerm(t, filepath.Join(d.Dir, "drawer.json"), 0o600)
}

func TestRegisterKeepsExistingDrawerJSON(t *testing.T) {
	d := Drawer{Dir: filepath.Join(t.TempDir(), "drawers", "api-3f2a9c1b"), Path: "/src/api", Name: "api"}
	if err := d.Register(); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	file := filepath.Join(d.Dir, "drawer.json")
	first, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	if err := d.Register(); err != nil {
		t.Fatalf("second Register: %v", err)
	}

	second, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Errorf("drawer.json = %s after re-registration, want unchanged %s", second, first)
	}
}

func TestRegisterFailsWhenDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	d := Drawer{Dir: filepath.Join(blocker, "drawers", "api-3f2a9c1b"), Path: "/src/api", Name: "api"}

	if err := d.Register(); err == nil {
		t.Error("Register succeeded, want an error")
	}
}

func mustResolve(t *testing.T, dataRoot, cwd string) Drawer {
	t.Helper()
	d, ok, err := Resolve(dataRoot, cwd)
	if err != nil || !ok {
		t.Fatalf("Resolve(%q) = ok %v, err %v, want a drawer", cwd, ok, err)
	}
	return d
}

// expectedDrawer は docs/development/architecture.md の規則どおりに、root を持つ引き出しを組み立てる。
func expectedDrawer(t *testing.T, dataRoot, root string) Drawer {
	t.Helper()
	path, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	sum := sha256.Sum256([]byte(path))
	return Drawer{
		Dir:  filepath.Join(dataRoot, "drawers", name+"-"+hex.EncodeToString(sum[:])[:8]),
		Path: path,
		Name: name,
	}
}
