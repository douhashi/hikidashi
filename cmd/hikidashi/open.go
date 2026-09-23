package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/render"
	"github.com/douhashi/hikidashi/internal/tmux"
)

// openUsage は hikidashi open の使い方。
const openUsage = "Usage: hikidashi open [<drawer>]\n"

// runOpen は hikidashi open の入口。<drawer>（slug またはリポジトリ名）の引き出し、無ければ作業ディレクトリの
// 引き出しの tmux セッションを開く。作業ディレクトリが Git 管理外なら、全引き出しから fzf で選ばせる。
// 引数が 2 個以上なら exit 2、未登録・曖昧な名前・fzf や tmux の失敗は exit 1 とする。fzf を閉じただけなら exit 0 とする。
func runOpen(args []string, _ io.Reader, _, stderr io.Writer) int {
	if len(args) > 1 {
		report(stderr, openUsage)
		return 2
	}
	if err := openDrawer(args, stderr); err != nil {
		report(stderr, fmt.Sprintf("hikidashi open: %v\n", err))
		return 1
	}
	return 0
}

// openDrawer は対象の引き出しの tmux セッションを、無ければ hikidashi add と同じ規則で作ってから開く。
// tmux の中からは今のクライアントを切り替え、外からは attach する。
func openDrawer(args []string, stderr io.Writer) error {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}
	d, ok, err := openTarget(root, args, stderr)
	if err != nil || !ok {
		return err
	}

	name := d.TmuxSession()
	if _, err := tmux.Ensure(name, d.Path); err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		return tmux.SwitchClient(name)
	}
	return tmux.Attach(name)
}

// openTarget は開く引き出しを返す。引数があれば slug か名前で、無ければ作業ディレクトリから引き、
// 作業ディレクトリが Git 管理外なら fzf で選ばせる。fzf で何も選ばれなければ ok=false を返す。
func openTarget(root string, args []string, stderr io.Writer) (drawer.Drawer, bool, error) {
	if len(args) == 1 {
		d, err := findDrawer(root, args[0])
		return d, err == nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return drawer.Drawer{}, false, err
	}
	d, inGit, err := currentDrawer(root, cwd)
	if err != nil || inGit {
		return d, err == nil, err
	}
	return chooseDrawer(root, stderr)
}

// chooseDrawer は全引き出しを fzf に並べ、選ばれた引き出しを返す。何も選ばれなければ ok=false を返す。
func chooseDrawer(root string, stderr io.Writer) (drawer.Drawer, bool, error) {
	drawers, err := drawer.List(root)
	if err != nil {
		return drawer.Drawer{}, false, err
	}
	if len(drawers) == 0 {
		return drawer.Drawer{}, false, notRegistered("no drawers registered")
	}
	drawer.SortByName(drawers)
	home, err := os.UserHomeDir()
	if err != nil {
		return drawer.Drawer{}, false, err
	}
	exe, err := os.Executable()
	if err != nil {
		return drawer.Drawer{}, false, err
	}

	line, ok, err := choose(drawerLines(drawers, home), exe, stderr)
	if err != nil || !ok {
		return drawer.Drawer{}, false, err
	}
	slug, _, _ := strings.Cut(line, "\t")
	for _, d := range drawers {
		if d.Slug() == slug {
			return d, true, nil
		}
	}
	return drawer.Drawer{}, false, fmt.Errorf("unexpected selection %q", line)
}

// drawerLines は drawers を 1 引き出し 1 行にする。行は「slug TAB 名前  パス（ホーム配下は ~ 始まり）」で、名前の列は幅を揃える。
// fzf には TAB より後ろだけを見せ、slug はプレビューと選択に使う。
func drawerLines(drawers []drawer.Drawer, home string) []string {
	width := 0
	for _, d := range drawers {
		width = max(width, utf8.RuneCountInString(render.OneLine(d.Name)))
	}
	lines := make([]string, 0, len(drawers))
	for _, d := range drawers {
		lines = append(lines, fmt.Sprintf("%s\t%-*s  %s", d.Slug(), width, render.OneLine(d.Name), render.OneLine(tildePath(d.Path, home))))
	}
	return lines
}

// tildePath は home 配下の path の home を ~ に縮める。案件を見分ける末尾が fzf や補完の候補の幅で切られにくくするため。
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

// choose は lines を fzf に渡し、選ばれた行を返す。先頭の列（slug）は見せず、プレビューの hikidashi show にだけ渡す。
// Esc 等で何も選ばれなければ ok=false を返す。fzf の画面は端末（/dev/tty）と stderr に出る。
func choose(lines []string, exe string, stderr io.Writer) (line string, ok bool, err error) {
	cmd := exec.Command("fzf",
		"--delimiter=\t", "--with-nth=2..", "--no-sort",
		// 並び順どおりに、先頭の行を上に出す。
		"--layout=reverse",
		// プレビューのコマンドの引用を、利用者のログインシェルによらず POSIX sh の規則に揃える。
		"--with-shell=sh -c",
		"--preview="+shellQuote(exe)+" show {1}",
	)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	// プレビューの出力は fzf へのパイプで端末でないため、色を強制する。NO_COLOR が設定されていれば強制しない。
	cmd.Env = os.Environ()
	if os.Getenv("NO_COLOR") == "" {
		cmd.Env = append(cmd.Env, "CLICOLOR_FORCE=1")
	}
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if exit, exited := errors.AsType[*exec.ExitError](err); exited {
		switch exit.ExitCode() {
		case 1, 130: // 1: 一致する行が無い、130: Esc / Ctrl-C で閉じた
			return "", false, nil
		}
	}
	if err != nil {
		return "", false, fmt.Errorf("run fzf: %w", err)
	}
	return strings.TrimSuffix(string(out), "\n"), true, nil
}

// shellQuote は s を POSIX sh の単一引用符で囲み、そのまま 1 語として渡るようにする。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
