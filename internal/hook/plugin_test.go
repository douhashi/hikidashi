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

// hookCommand は hooks.json から hikidashi hook を起動するコマンド。
const hookCommand = "hikidashi hook"

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

// TestPluginRoutesEveryHandledEventToHook は、hooks.json が hikidashi hook に繋ぐイベントが
// classify の扱うイベントとちょうど一致し、各イベントで同期に 1 度だけ、絞り込みなしで起動することを確かめる。
// 絞り込みは classify が行うため、matcher で入力を落とすと状態の遷移や compact 後の備忘録の注入を取りこぼす。
// async にすると SessionStart の stdout が Claude に届かず、備忘録を注入できない。
func TestPluginRoutesEveryHandledEventToHook(t *testing.T) {
	data, err := os.ReadFile(hooksJSON)
	if err != nil {
		t.Fatal(err)
	}
	var file hooksFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("decode %s: %v", hooksJSON, err)
	}

	routed := map[string]int{}
	for event, groups := range file.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if h.Type != "command" || h.Command != hookCommand {
					continue
				}
				routed[event]++
				if g.Matcher != nil {
					t.Errorf("%s: %q has matcher %q, want none", event, hookCommand, *g.Matcher)
				}
				if h.Async {
					t.Errorf("%s: %q is async, want synchronous", event, hookCommand)
				}
			}
		}
	}

	if got, want := slices.Sorted(maps.Keys(routed)), slices.Sorted(maps.Keys(events)); !slices.Equal(got, want) {
		t.Errorf("events routed to %q = %v, want %v", hookCommand, got, want)
	}
	for event, n := range routed {
		if n != 1 {
			t.Errorf("%s: %q runs %d times, want once", event, hookCommand, n)
		}
	}
}
