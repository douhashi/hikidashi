package scan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
	"github.com/douhashi/hikidashi/internal/testutil"
)

// at は 2026-09-23 の hh:mm（UTC）。
func at(hh, mm int) time.Time {
	return time.Date(2026, 9, 23, hh, mm, 0, 0, time.UTC)
}

// fixture は実ファイルのデータルートと、生きている claude の PID。
type fixture struct {
	t        *testing.T
	dataRoot string
	pid      int
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return fixture{t: t, dataRoot: t.TempDir(), pid: testutil.StartClaude(t)}
}

// drawer は name の引き出しを登録して返す。
func (f fixture) drawer(name string) drawer.Drawer {
	f.t.Helper()
	d := drawer.Drawer{Dir: filepath.Join(f.dataRoot, "drawers", name+"-0123abcd"), Path: "/src/" + name, Name: name}
	if err := d.Register(); err != nil {
		f.t.Fatal(err)
	}
	return d
}

// session は d に、生きている claude の state のセッションを書いて返す。transcript は無い。
func (f fixture) session(d drawer.Drawer, id string, state session.State, changedAt time.Time) session.Session {
	f.t.Helper()
	s := session.Session{
		SessionID:      id,
		Cwd:            d.Path,
		TmuxPane:       "%" + id,
		ClaudePID:      f.pid,
		TranscriptPath: filepath.Join(f.t.TempDir(), id+".jsonl"),
		State:          state,
		StateChangedAt: changedAt,
		StartedAt:      at(8, 0),
	}
	f.write(d, s)
	return s
}

func (f fixture) write(d drawer.Drawer, s session.Session) {
	f.t.Helper()
	if err := session.Write(d.Dir, s); err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) collect() []Entry {
	f.t.Helper()
	entries, err := Collect(f.dataRoot)
	if err != nil {
		f.t.Fatalf("Collect: %v", err)
	}
	return entries
}

// keys は entries を「引き出し名/session_id」の並びにする。
func keys(entries []Entry) []string {
	var got []string
	for _, e := range entries {
		got = append(got, e.Drawer.Name+"/"+e.Session.SessionID)
	}
	return got
}

func TestCollectOrdersByStateThenNeglectThenDrawerName(t *testing.T) {
	f := newFixture(t)
	api, web := f.drawer("api"), f.drawer("web")
	f.session(api, "a1", session.Running, at(10, 0))
	f.session(api, "a2", session.Idle, at(9, 0))
	f.session(api, "a3", session.Waiting, at(11, 0))
	f.session(web, "w1", session.Waiting, at(10, 0))
	f.session(web, "w2", session.Idle, at(9, 0))
	f.session(web, "w3", session.Running, at(8, 0))
	// 知らない状態（壊れた書き手・将来の状態）は最後に回す。
	f.session(api, "a4", session.State("unknown"), at(7, 0))

	got := keys(f.collect())

	want := []string{"web/w1", "api/a3", "api/a2", "web/w2", "web/w3", "api/a1", "api/a4"}
	if !slices.Equal(got, want) {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestCollectTreatsInterruptedSessionAsIdleSinceInterruption(t *testing.T) {
	f := newFixture(t)
	api := f.drawer("api")
	running := f.session(api, "r", session.Running, at(10, 0))
	waiting := f.session(api, "w", session.Waiting, at(10, 0))
	stillRunning := f.session(api, "s", session.Running, at(10, 0))
	idle := f.session(api, "i", session.Idle, at(9, 30))
	interruption := func(ts string) string {
		return `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]},"timestamp":"` + ts + `"}` + "\n"
	}
	testutil.WriteFile(t, running.TranscriptPath, interruption("2026-09-23T10:20:00Z"))
	testutil.WriteFile(t, waiting.TranscriptPath, interruption("2026-09-23T10:10:00Z"))
	// 状態が変わる前の中断は、その後のプロンプトで既に解けている。
	testutil.WriteFile(t, stillRunning.TranscriptPath, interruption("2026-09-23T09:59:00Z"))
	before := testutil.ReadFile(t, filepath.Join(api.Dir, "sessions", "r.json"))

	got := f.collect()

	if want := []string{"api/i", "api/w", "api/r", "api/s"}; !slices.Equal(keys(got), want) {
		t.Fatalf("order = %q, want %q", keys(got), want)
	}
	for i, want := range []struct {
		state session.State
		since time.Time
	}{
		{session.Idle, idle.StateChangedAt},
		{session.Idle, at(10, 10)},
		{session.Idle, at(10, 20)},
		{session.Running, at(10, 0)},
	} {
		if got[i].Session.State != want.state || !got[i].Since.Equal(want.since) {
			t.Errorf("%s = %s since %v, want %s since %v", keys(got)[i], got[i].Session.State, got[i].Since, want.state, want.since)
		}
	}
	// 読み手はセッションのファイルを書き換えない。
	if after := testutil.ReadFile(t, filepath.Join(api.Dir, "sessions", "r.json")); after != before {
		t.Errorf("session file = %s, want unchanged %s", after, before)
	}
}

func TestCollectTreatsIdleSessionWithUnfinishedAgentAsRunning(t *testing.T) {
	f := newFixture(t)
	api := f.drawer("api")
	busy := f.session(api, "busy", session.Idle, at(10, 0))
	done := f.session(api, "done", session.Idle, at(9, 0))
	interrupted := f.session(api, "int", session.Running, at(9, 30))
	launched := func(id string) string {
		return `{"type":"user","isSidechain":false,"timestamp":"2026-09-23T09:00:00Z","toolUseResult":{"status":"async_launched","agentId":"` + id + `"}}` + "\n"
	}
	notified := `{"type":"user","isSidechain":false,"message":{"content":"<task-notification>\n<task-id>a1</task-id>\n<status>completed</status>\n</task-notification>"}}` + "\n"
	testutil.WriteFile(t, busy.TranscriptPath, launched("a1"))
	testutil.WriteFile(t, done.TranscriptPath, launched("a1")+notified)
	// 中断で idle になったセッションも、エージェントが残っていれば走行中とする。
	testutil.WriteFile(t, interrupted.TranscriptPath, launched("a2")+
		`{"type":"user","isSidechain":false,"message":{"content":"[Request interrupted by user]"},"timestamp":"2026-09-23T10:20:00Z"}`+"\n")
	before := testutil.ReadFile(t, filepath.Join(api.Dir, "sessions", "busy.json"))

	got := f.collect()

	if want := []string{"api/done", "api/busy", "api/int"}; !slices.Equal(keys(got), want) {
		t.Fatalf("order = %q, want %q", keys(got), want)
	}
	for i, want := range []struct {
		state session.State
		since time.Time
	}{
		{session.Idle, at(9, 0)},
		{session.Running, at(10, 0)},
		{session.Running, at(10, 20)},
	} {
		if got[i].Session.State != want.state || !got[i].Since.Equal(want.since) {
			t.Errorf("%s = %s since %v, want %s since %v", keys(got)[i], got[i].Session.State, got[i].Since, want.state, want.since)
		}
	}
	// 読み手はセッションのファイルを書き換えない。
	if after := testutil.ReadFile(t, filepath.Join(api.Dir, "sessions", "busy.json")); after != before {
		t.Errorf("session file = %s, want unchanged %s", after, before)
	}
}

func TestCollectListsSessionsWithAndWithoutNextAction(t *testing.T) {
	f := newFixture(t)
	api := f.drawer("api")
	f.session(api, "done", session.Idle, at(9, 0))
	f.session(api, "todo", session.Idle, at(10, 0))
	testutil.WriteFile(t, filepath.Join(api.Dir, "sessions", "done.next.json"),
		`{"summary":"テストを直した","human_next":"差分を確認する","claude_next":"","blockers":[],"generated_at":"2026-09-23T09:01:00Z"}`)

	got := f.collect()

	if want := []string{"api/done", "api/todo"}; !slices.Equal(keys(got), want) {
		t.Fatalf("order = %q, want %q", keys(got), want)
	}
	if !got[0].HasNext || got[0].Next.HumanNext != "差分を確認する" || got[0].Next.Summary != "テストを直した" {
		t.Errorf("done = has %v, next %+v, want the extracted next action", got[0].HasNext, got[0].Next)
	}
	if got[1].HasNext {
		t.Errorf("todo = has %v, next %+v, want not extracted", got[1].HasNext, got[1].Next)
	}
}

func TestCollectRemovesSessionsWhoseClaudeIsGone(t *testing.T) {
	f := newFixture(t)
	api := f.drawer("api")
	f.session(api, "alive", session.Idle, at(9, 0))
	for id, pid := range map[string]int{"killed": testutil.DeadPID(t), "reused": os.Getpid()} {
		s := f.session(api, id, session.Waiting, at(9, 0))
		s.ClaudePID = pid
		f.write(api, s)
	}
	sessions := filepath.Join(api.Dir, "sessions")
	for _, name := range []string{"alive.next.json", "killed.next.json", "killed.extract.lock", "reused.next.json"} {
		testutil.WriteFile(t, filepath.Join(sessions, name), "{}")
	}

	got := f.collect()

	if want := []string{"api/alive"}; !slices.Equal(keys(got), want) {
		t.Errorf("sessions = %q, want %q", keys(got), want)
	}
	testutil.AssertEntries(t, sessions, "alive.json", "alive.next.json")
}

func TestDrawerReturnsOnlyItsLiveSessionsInOrder(t *testing.T) {
	f := newFixture(t)
	api, web := f.drawer("api"), f.drawer("web")
	f.session(api, "a1", session.Running, at(10, 0))
	f.session(api, "a2", session.Waiting, at(11, 0))
	f.session(api, "a3", session.Idle, at(9, 0))
	dead := f.session(api, "dead", session.Waiting, at(9, 0))
	dead.ClaudePID = testutil.DeadPID(t)
	f.write(api, dead)
	f.session(web, "w1", session.Waiting, at(8, 0))

	got, err := Drawer(api)

	if err != nil {
		t.Fatalf("Drawer: %v", err)
	}
	if want := []string{"api/a2", "api/a3", "api/a1"}; !slices.Equal(keys(got), want) {
		t.Errorf("sessions = %q, want %q", keys(got), want)
	}
	testutil.AssertEntries(t, filepath.Join(api.Dir, "sessions"), "a1.json", "a2.json", "a3.json")
}

func TestCollectWithoutDataIsEmpty(t *testing.T) {
	got, err := Collect(t.TempDir())

	if err != nil || len(got) != 0 {
		t.Errorf("Collect = %+v, err %v, want empty", got, err)
	}
}
