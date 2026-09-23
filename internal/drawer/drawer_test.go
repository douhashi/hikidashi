package drawer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

func TestLookupFindsRegisteredDrawerFromWorktreeAndSubdirectory(t *testing.T) {
	testutil.IsolateGit(t)
	base := t.TempDir()
	repo := testutil.NewRepo(t, filepath.Join(base, "api"))
	sub := filepath.Join(repo, "a")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(base, "api-wt")
	testutil.Git(t, repo, "worktree", "add", "-q", worktree)
	dataRoot := t.TempDir()
	want := expectedDrawer(t, dataRoot, repo)
	if err := want.Register(); err != nil {
		t.Fatal(err)
	}

	for _, cwd := range []string{repo, sub, worktree} {
		t.Run(filepath.Base(cwd), func(t *testing.T) {
			got, ok, err := Lookup(dataRoot, cwd)

			if err != nil || !ok {
				t.Fatalf("Lookup(%q) = ok %v, err %v, want the registered drawer", cwd, ok, err)
			}
			if got.Dir != want.Dir || got.Path != want.Path || got.Name != want.Name || got.CreatedAt.IsZero() {
				t.Errorf("Lookup(%q) = %+v, want %+v with created_at", cwd, got, want)
			}
		})
	}
}

func TestLookupDoesNotFindUnregisteredDrawer(t *testing.T) {
	testutil.IsolateGit(t)
	base := t.TempDir()
	repo := testutil.NewRepo(t, filepath.Join(base, "repo"))
	plain := filepath.Join(base, "plain")
	if err := os.Mkdir(plain, 0o700); err != nil {
		t.Fatal(err)
	}

	for name, cwd := range map[string]string{
		"unregistered repository": repo,
		"not a repository":        plain,
	} {
		t.Run(name, func(t *testing.T) {
			dataRoot := t.TempDir()

			_, ok, err := Lookup(dataRoot, cwd)

			if ok || err != nil {
				t.Errorf("Lookup(%q) = ok %v, err %v, want ok false, err nil", cwd, ok, err)
			}
			testutil.AssertEntries(t, dataRoot)
		})
	}
}

func TestLookupFailsOnBrokenDrawerJSON(t *testing.T) {
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "api"))
	dataRoot := t.TempDir()
	d := expectedDrawer(t, dataRoot, repo)
	testutil.WriteFile(t, filepath.Join(d.Dir, "drawer.json"), `{"path":`)

	if _, ok, err := Lookup(dataRoot, repo); ok || err == nil {
		t.Errorf("Lookup = ok %v, err %v, want an error", ok, err)
	}
}

func TestLookupFailsWhenGitCannotStart(t *testing.T) {
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "repo"))
	t.Setenv("PATH", t.TempDir())

	if _, _, err := Lookup(t.TempDir(), repo); err == nil {
		t.Error("Lookup succeeded without git, want an error")
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

func TestSlugIsDrawerDirName(t *testing.T) {
	d := Drawer{Dir: "/data/drawers/api-3f2a9c1b"}

	if got, want := d.Slug(), "api-3f2a9c1b"; got != want {
		t.Errorf("Slug = %q, want %q", got, want)
	}
}

func TestTmuxSessionReplacesDotAndColonInSlug(t *testing.T) {
	d := Drawer{Dir: "/data/drawers/example.com:8080-3f2a9c1b"}

	if got, want := d.TmuxSession(), "example_com_8080-3f2a9c1b"; got != want {
		t.Errorf("TmuxSession = %q, want %q", got, want)
	}
}

func TestTmuxSessionOfRepositoryWithDot(t *testing.T) {
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "example.com"))

	d := mustResolve(t, t.TempDir(), repo)

	if got, want := d.TmuxSession(), "example_com-"+d.Slug()[len("example.com-"):]; got != want {
		t.Errorf("TmuxSession = %q, want %q", got, want)
	}
}

func TestTmuxSessionSeparatesSameNameAtDifferentPaths(t *testing.T) {
	testutil.IsolateGit(t)
	base := t.TempDir()
	first := testutil.NewRepo(t, filepath.Join(base, "x", "app"))
	second := testutil.NewRepo(t, filepath.Join(base, "y", "app"))
	dataRoot := t.TempDir()

	a := mustResolve(t, dataRoot, first).TmuxSession()
	b := mustResolve(t, dataRoot, second).TmuxSession()

	if a == b {
		t.Errorf("both repositories got the tmux session %q, want different names", a)
	}
}

func TestNotesPathIsInDrawerDir(t *testing.T) {
	d := Drawer{Dir: "/data/drawers/api-3f2a9c1b"}

	if got, want := d.NotesPath(), "/data/drawers/api-3f2a9c1b/notes.md"; got != want {
		t.Errorf("NotesPath = %q, want %q", got, want)
	}
}

func TestListReturnsRegisteredDrawers(t *testing.T) {
	dataRoot := t.TempDir()
	drawers := filepath.Join(dataRoot, "drawers")
	api := Drawer{Dir: filepath.Join(drawers, "api-3f2a9c1b"), Path: "/src/api", Name: "api"}
	web := Drawer{Dir: filepath.Join(drawers, "web-0a1b2c3d"), Path: "/src/web", Name: "web"}
	for _, d := range []Drawer{web, api} {
		if err := d.Register(); err != nil {
			t.Fatal(err)
		}
	}
	// 登録の途中（drawer.json がまだ無い）・壊れた drawer.json・ディレクトリでないものは引き出しではない。
	if err := os.MkdirAll(filepath.Join(drawers, "half-00000000"), 0o700); err != nil {
		t.Fatal(err)
	}
	testutil.WriteFile(t, filepath.Join(drawers, "broken-11111111", "drawer.json"), `{"path":`)
	testutil.WriteFile(t, filepath.Join(drawers, "stray.txt"), "")

	got, err := List(dataRoot)

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List = %+v, want api and web", got)
	}
	for i, want := range []Drawer{api, web} {
		if got[i].Dir != want.Dir || got[i].Path != want.Path || got[i].Name != want.Name || got[i].CreatedAt.IsZero() {
			t.Errorf("List[%d] = %+v, want %+v with created_at", i, got[i], want)
		}
	}
}

func TestListWithoutDrawersIsEmpty(t *testing.T) {
	got, err := List(t.TempDir())

	if err != nil || len(got) != 0 {
		t.Errorf("List = %+v, err %v, want empty", got, err)
	}
}

func TestListFailsWhenDrawerJSONCannotBeRead(t *testing.T) {
	dataRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataRoot, "drawers", "api-3f2a9c1b", "drawer.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := List(dataRoot); err == nil {
		t.Error("List succeeded, want an error")
	}
}

// registerAt は dataRoot 配下に slug の引き出しを、path と name で登録して返す。
func registerAt(t *testing.T, dataRoot, slug, path, name string) Drawer {
	t.Helper()
	d := Drawer{Dir: filepath.Join(dataRoot, "drawers", slug), Path: path, Name: name}
	if err := d.Register(); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFindBySlugOrUniqueName(t *testing.T) {
	dataRoot := t.TempDir()
	api := registerAt(t, dataRoot, "api-3f2a9c1b", "/src/api", "api")
	// 名前が別の引き出しの slug と同じでも、slug の完全一致を優先する。
	registerAt(t, dataRoot, "api-3f2a9c1b-0a1b2c3d", "/src/x/api-3f2a9c1b", "api-3f2a9c1b")

	for name, want := range map[string]Drawer{"api-3f2a9c1b": api, "api": api} {
		t.Run(name, func(t *testing.T) {
			got, ok, err := Find(dataRoot, name)

			if err != nil || !ok {
				t.Fatalf("Find(%q) = ok %v, err %v, want %s", name, ok, err, want.Dir)
			}
			if got.Dir != want.Dir || got.Path != want.Path || got.Name != want.Name || got.CreatedAt.IsZero() {
				t.Errorf("Find(%q) = %+v, want %+v with created_at", name, got, want)
			}
		})
	}
}

func TestFindDoesNotFindUnregisteredName(t *testing.T) {
	dataRoot := t.TempDir()
	registerAt(t, dataRoot, "api-3f2a9c1b", "/src/api", "api")
	// drawer.json の無いディレクトリ（取り消し後に備忘録だけが残ったもの）は引き出しではない。
	testutil.WriteFile(t, filepath.Join(dataRoot, "drawers", "web-0a1b2c3d", "notes.md"), "memo\n")

	for _, name := range []string{"web", "web-0a1b2c3d", "ap", "api-3f2a9c1", ""} {
		t.Run(name, func(t *testing.T) {
			if _, ok, err := Find(dataRoot, name); ok || err != nil {
				t.Errorf("Find(%q) = ok %v, err %v, want ok false, err nil", name, ok, err)
			}
		})
	}
}

func TestFindFailsOnAmbiguousNameWithCandidates(t *testing.T) {
	dataRoot := t.TempDir()
	registerAt(t, dataRoot, "app-11111111", "/x/app", "app")
	registerAt(t, dataRoot, "app-22222222", "/y/app", "app")

	_, ok, err := Find(dataRoot, "app")

	if ok || err == nil {
		t.Fatalf("Find = ok %v, err %v, want an error", ok, err)
	}
	if want := `"app" matches more than one drawer: app-11111111, app-22222222`; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestFindFailsWhenDrawersCannotBeListed(t *testing.T) {
	dataRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataRoot, "drawers", "api-3f2a9c1b", "drawer.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, _, err := Find(dataRoot, "api"); err == nil {
		t.Error("Find succeeded, want an error")
	}
}

func TestNotesReturnsNonBlankNotes(t *testing.T) {
	d := Drawer{Dir: filepath.Join(t.TempDir(), "api-3f2a9c1b")}
	testutil.WriteFile(t, d.NotesPath(), "remember\n")

	got, ok, err := d.Notes()

	if err != nil || !ok || got != "remember\n" {
		t.Errorf("Notes = %q, ok %v, err %v, want the notes", got, ok, err)
	}
}

func TestNotesIgnoresMissingOrBlankNotes(t *testing.T) {
	empty, blank := "", " \n\t\n"
	for name, content := range map[string]*string{"missing": nil, "empty": &empty, "blank": &blank} {
		t.Run(name, func(t *testing.T) {
			d := Drawer{Dir: filepath.Join(t.TempDir(), "api-3f2a9c1b")}
			if content != nil {
				testutil.WriteFile(t, d.NotesPath(), *content)
			}

			if got, ok, err := d.Notes(); ok || err != nil {
				t.Errorf("Notes = %q, ok %v, err %v, want ok false, err nil", got, ok, err)
			}
		})
	}
}

func TestNotesFailsWhenNotesCannotBeRead(t *testing.T) {
	d := Drawer{Dir: filepath.Join(t.TempDir(), "api-3f2a9c1b")}
	if err := os.MkdirAll(d.NotesPath(), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, _, err := d.Notes(); err == nil {
		t.Error("Notes succeeded, want an error")
	}
}

// fillDrawer は登録済みの引き出し d に、セッションのファイルと notes.md（notes が nil なら作らない）を置く。
func fillDrawer(t *testing.T, d Drawer, notes *string) {
	t.Helper()
	for _, name := range []string{"s1.json", "s1.next.json", "s1.extract.lock"} {
		testutil.WriteFile(t, filepath.Join(d.Dir, "sessions", name), "{}")
	}
	if notes != nil {
		testutil.WriteFile(t, d.NotesPath(), *notes)
	}
}

func TestUnregisterKeepsNonBlankNotesOnly(t *testing.T) {
	dataRoot := t.TempDir()
	d := registerAt(t, dataRoot, "api-3f2a9c1b", "/src/api", "api")
	notes := "remember the staging DB\n"
	fillDrawer(t, d, &notes)

	kept, err := d.Unregister()

	if err != nil || !kept {
		t.Fatalf("Unregister = kept %v, err %v, want notes kept", kept, err)
	}
	testutil.AssertEntries(t, d.Dir, "notes.md")
	if got := testutil.ReadFile(t, d.NotesPath()); got != notes {
		t.Errorf("notes.md = %q, want %q", got, notes)
	}
	if got, _ := List(dataRoot); len(got) != 0 {
		t.Errorf("List = %+v, want no drawers", got)
	}
}

func TestUnregisterRemovesDrawerWithoutNotes(t *testing.T) {
	empty, blank := "", " \n\t\n"
	for name, notes := range map[string]*string{"missing": nil, "empty": &empty, "blank": &blank} {
		t.Run(name, func(t *testing.T) {
			dataRoot := t.TempDir()
			d := registerAt(t, dataRoot, "api-3f2a9c1b", "/src/api", "api")
			fillDrawer(t, d, notes)

			kept, err := d.Unregister()

			if err != nil || kept {
				t.Fatalf("Unregister = kept %v, err %v, want nothing kept", kept, err)
			}
			testutil.AssertEntries(t, filepath.Join(dataRoot, "drawers"))
		})
	}
}

func TestUnregisterFailsWhenNotRegistered(t *testing.T) {
	dataRoot := t.TempDir()
	d := Drawer{Dir: filepath.Join(dataRoot, "drawers", "api-3f2a9c1b")}
	testutil.WriteFile(t, d.NotesPath(), "memo\n")

	if _, err := d.Unregister(); err == nil {
		t.Error("Unregister succeeded, want an error")
	}
	if got := testutil.ReadFile(t, d.NotesPath()); got != "memo\n" {
		t.Errorf("notes.md = %q, want it untouched", got)
	}
}

func TestSortByNameOrdersByNameThenSlug(t *testing.T) {
	drawers := []Drawer{
		{Dir: "/d/0-web-00000000", Name: "web"},
		{Dir: "/d/api-22222222", Name: "api"},
		{Dir: "/d/api-11111111", Name: "api"},
	}

	SortByName(drawers)

	var got []string
	for _, d := range drawers {
		got = append(got, d.Slug())
	}
	if want := []string{"api-11111111", "api-22222222", "0-web-00000000"}; !slices.Equal(got, want) {
		t.Errorf("slugs = %q, want %q", got, want)
	}
}
