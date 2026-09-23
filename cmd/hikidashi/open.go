package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/colorprofile"

	"github.com/douhashi/hikidashi/internal/drawer"
	"github.com/douhashi/hikidashi/internal/show"
	"github.com/douhashi/hikidashi/internal/tmux"
)

// openUsage は hikidashi open の使い方。
const openUsage = "Usage: hikidashi open [<drawer>]\n"

// runOpen は hikidashi open の入口。<drawer>（slug またはリポジトリ名）の引き出し、無ければ作業ディレクトリの
// 引き出しの tmux セッションを開く。作業ディレクトリが Git 管理外なら、全引き出しから fzf で選ばせる。
// 引数が 2 個以上なら exit 2、未登録・曖昧な名前・fzf や tmux の失敗は exit 1 とする。fzf を閉じただけなら exit 0 とする。
func runOpen(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		report(stderr, openUsage)
		return 2
	}
	if err := openDrawer(args, outputWidth(stdout), stderr); err != nil {
		report(stderr, fmt.Sprintf("hikidashi open: %v\n", err))
		return 1
	}
	return 0
}

// openDrawer は対象の引き出しの tmux セッションを、無ければ hikidashi add と同じ規則で作ってから開く。
// tmux の中からは今のクライアントを切り替え、外からは attach する。width は fzf を開く端末の幅（0 は不明）。
func openDrawer(args []string, width int, stderr io.Writer) error {
	root, err := drawer.DefaultRoot()
	if err != nil {
		return err
	}
	d, ok, err := openTarget(root, args, width, stderr)
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
// 作業ディレクトリが Git 管理外なら幅 width（0 は不明）の端末の fzf で選ばせる。fzf で何も選ばれなければ ok=false を返す。
func openTarget(root string, args []string, width int, stderr io.Writer) (drawer.Drawer, bool, error) {
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
	return chooseDrawer(root, width, stderr)
}

// chooseDrawer は全引き出しを hikidashi list の表の行にして幅 width（0 は不明）の端末の fzf に並べ、選ばれた引き出しを返す。
// 何も選ばれなければ ok=false を返す。Issue の件数が得られなかった理由は fzf の画面に上書きされるため出さず、表の ? だけで示す。
func chooseDrawer(root string, width int, stderr io.Writer) (drawer.Drawer, bool, error) {
	summaries, err := show.Summaries(root)
	if err != nil {
		return drawer.Drawer{}, false, err
	}
	if len(summaries) == 0 {
		return drawer.Drawer{}, false, notRegistered("no drawers registered")
	}
	exe, err := os.Executable()
	if err != nil {
		return drawer.Drawer{}, false, err
	}

	window, tableWidth := previewLayout(width)
	header, rows := show.TableLines(summaries, tableWidth)
	// 各行は「slug TAB 表の行」。見出しの行は slug を持たず、fzf が選べない行として一覧の上に固定する。
	lines := make([]string, 0, len(header)+len(rows))
	for _, h := range header {
		lines = append(lines, "\t"+h)
	}
	for i, r := range rows {
		lines = append(lines, summaries[i].Drawer.Slug()+"\t"+r)
	}
	line, ok, err := choose(lines, len(header), exe, window, stderr)
	if err != nil || !ok {
		return drawer.Drawer{}, false, err
	}
	slug, _, _ := strings.Cut(line, "\t")
	for _, s := range summaries {
		if s.Drawer.Slug() == slug {
			return s.Drawer, true, nil
		}
	}
	return drawer.Drawer{}, false, fmt.Errorf("unexpected selection %q", line)
}

// downPreviewMinWidth は、プレビューを一覧の下に全幅で出す端末の最小の幅。これより狭ければ、今までどおり右に出す。
const downPreviewMinWidth = 100

// fzfListInset は fzf の一覧の 1 行のうち、行の文字に使えない桁数。左のカーソルと印の 2 桁と、右のスクロールバーの 1 桁。
const fzfListInset = 3

// previewLayout は幅 width（0 は不明）の端末での fzf のプレビューの配置（--preview-window）と、一覧に収める表の幅（0 は制限なし）を返す。
// 広い端末ではプレビューを一覧の下に置き、一覧は全幅になる。狭い・不明なら右に端末の幅の 50%（切り捨て）で置き、一覧は残りになる。
// fzf の --preview-window の条件（<N）は down では高さ、right では幅を見て「広ければ下」を表せないため、起動時にここで決める。
func previewLayout(width int) (window string, tableWidth int) {
	switch {
	case width >= downPreviewMinWidth:
		return "down,50%", width - fzfListInset
	case width > 0:
		return "right,50%", width - width*50/100 - fzfListInset
	}
	return "right,50%", 0
}

// choose は lines を fzf に渡し、選ばれた行を返す。先頭の headerLines 行は選べない見出しとして一覧の上に固定する。
// 各行の先頭の列（slug）は見せず、プレビューの hikidashi show にだけ渡す。絞り込みは表の名前のセルにだけ当てる。
// window はプレビューの配置。Esc 等で何も選ばれなければ ok=false を返す。fzf の画面は端末（/dev/tty）と stderr に出る。
func choose(lines []string, headerLines int, exe, window string, stderr io.Writer) (line string, ok bool, err error) {
	cmd := exec.Command("fzf",
		// 表の行の色を見せ、区切りの TAB と表の縦の罫線で列に分ける。1 列目は slug、2 列目は表の左の罫線より後ろの名前のセル。
		"--ansi", fmt.Sprintf("--header-lines=%d", headerLines), "--delimiter=\t|│", "--with-nth=2..", "--nth=2",
		// 並び順どおりに、先頭の行を上に出す。
		"--no-sort", "--layout=reverse",
		// プレビューのコマンドの引用を、利用者のログインシェルによらず POSIX sh の規則に揃える。
		"--with-shell=sh -c",
		"--preview="+shellQuote(exe)+" show {1}", "--preview-window="+window,
	)
	// 一覧とプレビューの出力は fzf へのパイプで端末でないため、色を強制する。NO_COLOR が設定されていれば強制しない。
	cmd.Env = os.Environ()
	if os.Getenv("NO_COLOR") == "" {
		cmd.Env = append(cmd.Env, "CLICOLOR_FORCE=1")
	}
	var stdin strings.Builder
	if _, err := colorprofile.NewWriter(&stdin, cmd.Env).WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		return "", false, err
	}
	cmd.Stdin = strings.NewReader(stdin.String())
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
