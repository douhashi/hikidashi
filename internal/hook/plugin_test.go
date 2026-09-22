package hook

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"testing"
)

// hooksJSON は Claude Code plugin が各イベントを hikidashi に繋ぐ定義。
const hooksJSON = "../../plugin/hooks/hooks.json"

// hooksFile は hooks.json のうち、この検査が見るフィールド。
type hooksFile struct {
	Hooks map[string][]struct {
		Matcher *string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Async   bool   `json:"async"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// route は hooks.json が 1 つのコマンドを繋ぐべきイベントと、起動の仕方。
type route struct {
	events []string
	async  bool
}

// routes は hooks.json が繋ぐコマンドのすべて。
//   - hikidashi hook は classify の扱うイベント（events）に同期で繋ぐ。async にすると SessionStart の stdout が
//     Claude に届かず、備忘録を注入できない。
//   - hikidashi extract は Stop にだけ async で繋ぐ。抽出は数秒かかり、Claude Code を待たせない
//     （docs/development/architecture.md の「次アクションの抽出」）。
var routes = map[string]route{
	"hikidashi hook":    {events: slices.Sorted(maps.Keys(events))},
	"hikidashi extract": {events: []string{"Stop"}, async: true},
}

// TestPluginRoutesEventsToCommands は、hooks.json が routes のコマンドだけを、それぞれ決めたイベントにちょうど、
// 各イベントで 1 度だけ、決めた起動の仕方で、絞り込みなしで繋ぐことを確かめる。
// 絞り込みは hikidashi が行うため、matcher で入力を落とすと状態の遷移や compact 後の備忘録の注入を取りこぼす。
func TestPluginRoutesEventsToCommands(t *testing.T) {
	data, err := os.ReadFile(hooksJSON)
	if err != nil {
		t.Fatal(err)
	}
	var file hooksFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("decode %s: %v", hooksJSON, err)
	}

	routed := map[string]map[string]int{}
	for event, groups := range file.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				r, ok := routes[h.Command]
				if h.Type != "command" || !ok {
					t.Errorf("%s: unexpected hook %s %q", event, h.Type, h.Command)
					continue
				}
				if routed[h.Command] == nil {
					routed[h.Command] = map[string]int{}
				}
				routed[h.Command][event]++
				if g.Matcher != nil {
					t.Errorf("%s: %q has matcher %q, want none", event, h.Command, *g.Matcher)
				}
				if h.Async != r.async {
					t.Errorf("%s: %q has async %v, want %v", event, h.Command, h.Async, r.async)
				}
			}
		}
	}

	for command, r := range routes {
		if got := slices.Sorted(maps.Keys(routed[command])); !slices.Equal(got, r.events) {
			t.Errorf("events routed to %q = %v, want %v", command, got, r.events)
		}
		for event, n := range routed[command] {
			if n != 1 {
				t.Errorf("%s: %q runs %d times, want once", event, command, n)
			}
		}
	}
}
