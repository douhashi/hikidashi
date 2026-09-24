package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/render"
)

// completionUsage は hikidashi completion の使い方。
const completionUsage = "Usage: hikidashi completion <zsh|bash>\n"

// completeUsage は hikidashi __complete の使い方。
const completeUsage = "Usage: hikidashi __complete <word>...\n"

var (
	//go:embed completion.zsh
	zshScript string
	//go:embed completion.bash
	bashScript string
)

// shells は補完スクリプトを出せるシェルと、そのスクリプト。
var shells = []struct{ name, script string }{
	{name: "zsh", script: zshScript},
	{name: "bash", script: bashScript},
}

// candidate は補完の候補の 1 つ。value を補完し、description を候補の説明として見せる。
type candidate struct {
	value       string
	description string
}

// runCompletion は hikidashi completion の入口。シェルの補完スクリプトを stdout に出す。
// 引数が 1 個でない・対応していないシェルなら exit 2 とする。
func runCompletion(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 1 {
		for _, s := range shells {
			if s.name != args[0] {
				continue
			}
			if _, err := io.WriteString(stdout, s.script); err != nil {
				report(stderr, fmt.Sprintf("hikidashi completion: %v\n", err))
				return 1
			}
			return 0
		}
	}
	report(stderr, completionUsage)
	return 2
}

// shellCandidates は hikidashi completion の引数の候補（対応するシェル）を返す。
func shellCandidates() ([]candidate, error) {
	cands := make([]candidate, 0, len(shells))
	for _, s := range shells {
		cands = append(cands, candidate{value: s.name, description: s.name + " completion script"})
	}
	return cands, nil
}

// runComplete は補完スクリプトが呼ぶ hikidashi __complete の入口。words は hikidashi より後ろの語で、最後の語を補完中の接頭辞とする。
// 接頭辞で始まる候補を「値 TAB 説明」の 1 行ずつ stdout に出す。語が無ければ exit 2、候補を得られなければ stdout に何も出さず exit 1 とする。
func runComplete(cmds []command, words []string, stdout, stderr io.Writer) int {
	if len(words) == 0 {
		report(stderr, completeUsage)
		return 2
	}
	cands, err := candidates(cmds, words[:len(words)-1])
	if err == nil {
		var b strings.Builder
		for _, c := range cands {
			if strings.HasPrefix(c.value, words[len(words)-1]) {
				b.WriteString(c.value + "\t" + c.description + "\n")
			}
		}
		_, err = io.WriteString(stdout, b.String())
	}
	if err != nil {
		report(stderr, fmt.Sprintf("hikidashi __complete: %v\n", err))
		return 1
	}
	return 0
}

// candidates は補完中の語より前の語 before から、補完中の語の候補を返す。
// 語が無ければ cmds のサブコマンドを候補とする。先頭の語が子の表を持つサブコマンドなら残りの語で子の表を辿り、
// そうでなければ 1 語のときだけそのサブコマンドの引数の候補とする。それ以外は候補を持たない。
func candidates(cmds []command, before []string) ([]candidate, error) {
	if len(before) == 0 {
		cands := make([]candidate, 0, len(cmds))
		for _, c := range cmds {
			cands = append(cands, candidate{value: c.name, description: c.summary})
		}
		return cands, nil
	}
	for _, c := range cmds {
		if c.name != before[0] {
			continue
		}
		if c.sub != nil {
			return candidates(c.sub, before[1:])
		}
		if len(before) == 1 && c.complete != nil {
			return c.complete()
		}
	}
	return nil, nil
}

// drawerCandidates は登録済みの引き出しを、名前の順（同名は slug の順）に候補として返す。
// 値は、名前が一意でどの slug とも一致しなければ名前、それ以外は slug とする（findDrawer がその引き出しに当てる）。
// 説明はリポジトリのルート（ホーム配下は ~ 始まり）とする。
func drawerCandidates() ([]candidate, error) {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	drawers, err := drawer.List(root)
	if err != nil {
		return nil, err
	}
	drawer.SortByName(drawers)

	slugs := map[string]bool{}
	names := map[string]int{}
	for _, d := range drawers {
		slugs[d.Slug()] = true
		names[d.Name]++
	}
	cands := make([]candidate, 0, len(drawers))
	for _, d := range drawers {
		value := d.Slug()
		if names[d.Name] == 1 && !slugs[d.Name] {
			value = d.Name
		}
		cands = append(cands, candidate{value: value, description: render.OneLine(tildePath(d.Path, home))})
	}
	return cands, nil
}

// tildePath は home 配下の path の home を ~ に縮める。プロジェクトを見分ける末尾が補完の候補の幅で切られにくくするため。
// home の外（home と前方一致するだけの兄弟を含む）や、home が / のときはそのまま返す。
func tildePath(path, home string) string {
	home = strings.TrimSuffix(home, "/")
	switch {
	case home == "":
		return path
	case path == home:
		return "~"
	case strings.HasPrefix(path, home+"/"):
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
