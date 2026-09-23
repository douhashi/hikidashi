// Package scan は引き出しのセッションを集め、読み手（status / list / show）が表示する形に整える。
// 終わったセッションの後始末と中断の扱いは docs/development/architecture.md の「セッションの後始末」
// 「中断の扱い」、並び順は「hikidashi show」を参照。
package scan

import (
	"cmp"
	"slices"
	"time"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/session"
)

// Entry は一覧の 1 行に対応する、生きているセッション。
type Entry struct {
	Drawer drawer.Drawer
	// Session の State は中断を反映した実効の状態。ファイルの中身とは異なり得る。
	Session session.Session
	// Since は放置の起点。実効の状態に入った時刻。
	Since time.Time
	// Next は次アクション。HasNext が偽（未抽出）なら空。
	Next    session.Next
	HasNext bool
}

// stateOrder は並び順。人間が捌くべきものを先に出す。
var stateOrder = []session.State{session.Waiting, session.Idle, session.Running}

// Collect は dataRoot 配下の全引き出しの生きているセッションを、並び順どおりに返す。
// claude のプロセスが既に無いセッションは、そのファイルを消して一覧から外す。
func Collect(dataRoot string) ([]Entry, error) {
	drawers, err := drawer.List(dataRoot)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, d := range drawers {
		es, err := live(d)
		if err != nil {
			return nil, err
		}
		entries = append(entries, es...)
	}
	sortEntries(entries)
	return entries, nil
}

// Drawer は引き出し d の生きているセッションを、並び順どおりに返す。後始末は Collect と同じ。
func Drawer(d drawer.Drawer) ([]Entry, error) {
	entries, err := live(d)
	if err != nil {
		return nil, err
	}
	sortEntries(entries)
	return entries, nil
}

// live は d の生きているセッションを、ファイルの順に返す。
// claude のプロセスが既に無いセッションは、そのファイルを消して外す。
func live(d drawer.Drawer) ([]Entry, error) {
	sessions, err := session.List(d.Dir)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, s := range sessions {
		alive, err := session.Alive(s.ClaudePID)
		if err != nil {
			return nil, err
		}
		if !alive {
			// SessionEnd が発火せずに終わったセッション。書き手が二度と書かないため、消しても競合しない。
			if err := session.Remove(d.Dir, s.SessionID); err != nil {
				return nil, err
			}
			continue
		}
		e, err := newEntry(d, s)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// sortEntries は entries を状態・放置の長さ・引き出し名の順に並べる。
func sortEntries(entries []Entry) {
	slices.SortStableFunc(entries, func(a, b Entry) int {
		return cmp.Or(
			cmp.Compare(rank(a.Session.State), rank(b.Session.State)),
			a.Since.Compare(b.Since),
			cmp.Compare(a.Drawer.Name, b.Drawer.Name),
		)
	})
}

// newEntry は s の実効の状態と次アクションを求める。
// running / waiting のまま中断されたセッションは、中断の時刻から idle とする。
func newEntry(d drawer.Drawer, s session.Session) (Entry, error) {
	e := Entry{Drawer: d, Session: s, Since: s.StateChangedAt}
	if s.State != session.Idle {
		at, interrupted, err := session.Interrupted(s.TranscriptPath, s.StateChangedAt)
		if err != nil {
			return Entry{}, err
		}
		if interrupted {
			e.Session.State, e.Since = session.Idle, at
		}
	}

	next, ok, err := session.ReadNext(d.Dir, s.SessionID)
	if err != nil {
		return Entry{}, err
	}
	e.Next, e.HasNext = next, ok
	return e, nil
}

// rank は state の並び順の位置を返す。未知の状態は最後に回す。
func rank(state session.State) int {
	if i := slices.Index(stateOrder, state); i >= 0 {
		return i
	}
	return len(stateOrder)
}
