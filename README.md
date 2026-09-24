# hikidashi

複数プロジェクトで並行して走らせている Claude Code セッションをプロジェクトごとの「引き出し」に整理し、
いま何が動いていて、次に誰が何をすべきかをすぐ取り出せるようにするローカルツール。

## hikidashi について

プロジェクトをまたいで Claude Code を並行で動かしていると、どの作業がオープンで、どれが自分の入力待ちかを追いきれなくなる。
hikidashi は、プロジェクト（1 リポジトリ）ごとに引き出しを用意し、開ければ中身と状態がすぐわかる状態にする。

- **登録したプロジェクトだけを扱う**: `hikidashi add` で登録したプロジェクトのセッションだけを記録する。単発の小さな作業で引き出しが散らからない
- **1 プロジェクト = 1 tmux セッション**: 登録するとプロジェクト用の tmux セッションを用意する。Claude Code はその window / pane で動かす
- **状態を自動で記録する**: plugin が Claude Code の hooks を `hikidashi` に繋ぎ、セッションごとの状態を記録する。ターンが終わるたびに次アクションを要約する
- **備忘録を持てる**: プロジェクトごとの備忘録（`notes.md`）を書いておくと、セッションの開始時に Claude Code へ渡される
- **一覧と詳細で見る**: 全プロジェクトの概況、1 プロジェクトの詳細、入力待ちの件数（tmux のステータスバー）で状況を取り出す。Claude Code に自然な言葉で頼むこともできる

セッションの状態は次の 3 つ。終了したセッションは記録から消える。

| 状態 | 意味 |
| --- | --- |
| `running` | Claude がターンを処理している（バックグラウンドのエージェントが動いているときも含む） |
| `waiting` | ターンの途中で人間の判断（権限の承認・入力フォーム）を待っている |
| `idle` | ターンが終わり、人間の次の指示を待っている |

hikidashi 本体（`hikidashi` コマンド）と、Claude Code の hooks を `hikidashi` に繋ぐ plugin からなる。
記録したデータは `~/.hikidashi/` に置き、プロジェクトのリポジトリには何も書かない。

## インストール

### 前提

- Linux または macOS
- git 2.31 以上
- tmux（tmux の pane で動くセッションだけを記録する）
- fzf（`hikidashi open` がプロジェクトの一覧に使う）
- GitHub CLI（`gh`。`hikidashi list`・`hikidashi show` が Open な Issue を数えるのに使う。`gh auth login` 済みであること）
- Claude Code

### hikidashi と plugin を入れる

[Releases](https://github.com/douhashi/hikidashi/releases) の最新版から、OS・arch に合うバイナリを `~/.local/bin` に入れる。

```sh
os=$(uname -s | tr '[:upper:]' '[:lower:]')   # linux / darwin
arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
mkdir -p ~/.local/bin
curl -fsSL -o ~/.local/bin/hikidashi \
  "https://github.com/douhashi/hikidashi/releases/latest/download/hikidashi_${os}_${arch}"
chmod +x ~/.local/bin/hikidashi
```

成果物を照合するときは、同じ場所の `checksums.txt` と `sha256sum`（macOS は `shasum -a 256`）の値を比べる。
`~/.local/bin` は `PATH` に通しておく。`PATH` 上の hikidashi の版は `hikidashi version` で確かめられる。

```sh
claude plugin marketplace add douhashi/hikidashi
claude plugin install hikidashi@hikidashi
```

plugin は起動時に読み込まれるため、動いている Claude Code のセッションは再起動する。
ターンが終わるたびに `claude -p`（haiku）で次アクションを要約するため、その分の利用枠を使う。

### tmux に組み込む

`tmux.conf` に、プロジェクトの一覧・備忘録・Open な Issue をポップアップで開くキーバインドと、セッションの状態ごとの件数をステータスバーに出す設定を書く。

```tmux
# prefix + o でプロジェクトの一覧を開く。-E で、切り替えた後や Esc で閉じた後にポップアップも閉じる。
# -d / で Git 管理外から起動し、常に全プロジェクトの一覧を出す。
bind-key o display-popup -E -d / -w 90% -h 80% hikidashi open

# prefix + N で、今いるペインのプロジェクトの備忘録（notes.md）を開く。
bind-key N display-popup -E -d '#{pane_current_path}' -w 80% -h 80% hikidashi notes

# prefix + I で、今いるペインのリポジトリの Open な Issue を選び、ブラウザで開く。
bind-key I display-popup -E -d '#{pane_current_path}' -w 80% -h 60% "gh issue list --limit 100 | fzf --layout=reverse --delimiter '\t' --with-nth 1,3 | cut -f1 | xargs -r gh issue view --web"

# セッションの状態ごとの件数を色分けして出す（0 件の状態は出さず、全部 0 件なら何も出さない）。
set -g status-right '#(hikidashi tmux status) %H:%M'
```

`o` は tmux 既定の「次のペインへ順に移る」（`select-pane -t :.+`）を上書きする。使っているなら空いている別のキーにする。
`hikidashi`・`fzf`・`gh` は tmux サーバーの `PATH` から見える場所に置く。
`#{pane_current_path}` はペインの前面のプロセスの作業ディレクトリなので、Claude Code が動いているペインでもそのプロジェクトの備忘録や Issue が開く。
書き換えた備忘録が動いている Claude Code に渡るのは、次の `SessionStart`（`/clear`・compact・再開）からになる。
ステータスバーの表示は `status-interval`（既定 15 秒）ごとに更新される。

### シェルの補完を有効にする

サブコマンド名（`tmux status` のような入れ子も含む）と、`open`・`show`・`remove` に渡す引き出しの名前、`tmux status` に渡す状態名を Tab で補完できる。

```sh
# zsh: ~/.zshrc の compinit の後に書く
source <(hikidashi completion zsh)

# bash: ~/.bashrc に書く
source <(hikidashi completion bash)
```

同名のプロジェクトが複数あるときは、名前の代わりに slug が候補に出る。

## 使い方

### 流れ

1. プロジェクトのリポジトリで `hikidashi add` を実行し、引き出しとして登録する
2. 用意された tmux セッションで Claude Code を動かす。状態と次アクションは自動で記録される
3. `hikidashi list` で全プロジェクトの概況を、`hikidashi show` で 1 プロジェクトの詳細を見る。入力待ちはステータスバーに出る
4. `prefix + o`（または `hikidashi open`）でプロジェクトを選び、その tmux セッションに移る
5. 終わったプロジェクトは `hikidashi remove` で登録を取り消す

引き出しの名前を渡すコマンド（`open`・`show`・`remove`）には、リポジトリ名を渡す。
同名のプロジェクトが複数あるときは slug（`api-3f2a9c1b`）を渡す。

### プロジェクトを登録する（`hikidashi add`）

プロジェクトのリポジトリの中で実行する。

```sh
cd ~/src/api
hikidashi add
# drawer: api-3f2a9c1b (registered)
# tmux session: api-3f2a9c1b (created)
```

プロジェクトを引き出しとして登録し、リポジトリのルートを作業ディレクトリとする tmux セッションを用意する（既にあれば何もしない）。
worktree やサブディレクトリから実行しても、メインのリポジトリが登録される。
tmux のセッション名は `<リポジトリ名>-<パスのハッシュ 8 桁>` で、`.` と `:` は `_` に置き換わる（例: `example_com-3f2a9c1b`）。

### プロジェクトの tmux セッションを開く（`hikidashi open`）

プロジェクトの tmux セッションを開く。セッションが無ければ `hikidashi add` と同じ規則で作ってから開く。
tmux の中からは今のクライアントをそのセッションへ切り替え、tmux の外からはそのセッションに attach する。

```sh
hikidashi open api      # 名前を指定する
hikidashi open          # 今いるリポジトリ（worktree・サブディレクトリを含む）のプロジェクト
```

引数を省いて Git 管理外で実行すると、全プロジェクトを `hikidashi list` と同じ表の行で fzf に並べ、選んだプロジェクトのセッションを開く。
表の見出しは一覧の上に固定され、絞り込みはプロジェクトの名前にだけ当たる。
プレビュー（`hikidashi show <プロジェクト>` と同じ詳細）は一覧の下に全幅で出る。Issue の件数は一覧で数えた値を使い、カーソルを動かしても数え直さない。
未登録のリポジトリで引数を省くと、何も開かずに `hikidashi add` を案内する。

### プロジェクトの状況を見る（`hikidashi list`・`hikidashi show`）

`hikidashi list` は、登録済みの全プロジェクトの概況を 1 プロジェクト 1 行の表で出す。
Open な Issue の件数と、状態ごとのセッションの件数、備忘録（`notes.md`）の最初の 1 行が並び、1 件以上の件数は状態ごとの色で示す。
備忘録の行は端末の幅に収まるよう `…` で切り詰める。

```sh
hikidashi list
# ╭──────────┬────────┬─────────┬─────────┬──────┬──────────────────╮
# │ DRAWER   │ ISSUES │ RUNNING │ WAITING │ IDLE │ NOTES            │
# ├──────────┼────────┼─────────┼─────────┼──────┼──────────────────┤
# │ api      │      3 │       1 │       1 │    2 │ - 本番は触らない │
# │ frontend │      ? │       0 │       0 │    0 │                  │
# ╰──────────┴────────┴─────────┴─────────┴──────┴──────────────────╯
```

`ISSUES` の `?` は件数を取れなかった（GitHub のリモートが無い・`gh` が使えない等）ことを表し、理由は stderr に出る。
件数は `gh` が選ぶリポジトリのもので、fork で `upstream` を持つリポジトリでは `gh repo set-default` で数える先を選べる。

`hikidashi show <プロジェクト>` は、そのプロジェクトのセッションごとの状態・放置時間・pane・次アクションと、備忘録を枠に分けて出す。
セッションの枠の色と札で状態が分かる。`<プロジェクト>` を省くと、作業ディレクトリのプロジェクトを出す。
幅が 119 桁以上あってセッションがあれば、左にセッション、右にプロジェクトと備忘録を並べて出す。

```sh
hikidashi show api
```

色は端末に出すときだけ付く。パイプの先や `NO_COLOR` を設定したときは色の制御文字を含まない罫線だけのテキストになり、Claude Code に読ませてもそのまま使える。

### ステータスバーに状態ごとの件数を出す（`hikidashi tmux status`）

セッションの件数を状態ごとに、tmux の書式（`#[fg=...]`）で色分けして出す。tmux の `status-right` に組み込んで使う（「tmux に組み込む」）。
先頭に引き出しのアイコン（Nerd Font の nf-fa-archive、U+F187）を付け、`running` を `▶`、`waiting` を `?`、`idle` を `✓` の記号と件数で並べる。
0 件の状態は出さず、全部 0 件ならアイコンも含めて何も出さない。アイコンの表示には Nerd Font が要る。

状態名（`running` / `waiting` / `idle`）を渡すと、その状態の件数だけを色なしの数字で出す（0 件でも `0`）。
自分で書式を組みたいときに使う。

```sh
hikidashi tmux status          # 例: <アイコン> ▶1 ?2 ✓3（tmux の書式付き）
hikidashi tmux status waiting  # 例: 2
```

件数の代わりに `!` が出たら、集計に失敗している。tmux は理由（stderr）を捨てるため、
端末で `hikidashi tmux status` を実行して理由を見る。

### 備忘録を書く（`hikidashi notes`）

プロジェクトのリポジトリの中で実行すると、そのプロジェクトの備忘録（`notes.md`）を `$EDITOR` で開く。無ければ空で作ってから開く。

```sh
cd ~/src/api
hikidashi notes
```

備忘録は、そのプロジェクトで Claude Code のセッションが始まるたび（`/clear`・`/compact` の後を含む）に Claude Code へ渡される。
プロジェクトの前提や引き継ぎたいことを書いておく。10,000 字を超えると Claude Code は先頭 2,000 字しか見ないため、常に見せたいことは先頭に書く。

### プロジェクトの登録を取り消す（`hikidashi remove`）

プロジェクトの登録を取り消し、記録したセッションを消す。プロジェクトのリポジトリの中で実行するか、引き出しの名前を渡す。

```sh
hikidashi remove api
# drawer: api-3f2a9c1b (removed)
# notes: /home/you/.hikidashi/drawers/api-3f2a9c1b/notes.md (kept)
```

備忘録は空でなければ残り、同じリポジトリで `hikidashi add` すると戻る。
tmux セッションは閉じないため、不要なら `tmux kill-session -t api-3f2a9c1b` で閉じる。

### Claude Code に頼む

plugin の skill（`hikidashi:hikidashi`）により、登録・状況確認・登録取り消しを Claude Code に自然な言葉で頼める。
Claude Code は `hikidashi` のコマンドを実行し、その出力をもとに答える。

```text
このプロジェクトを登録しといて  # hikidashi add
プロジェクトの状況は？          # hikidashi list
api はどうなってる？            # hikidashi show api
このプロジェクトの登録を外して  # 確認のあと hikidashi remove
```

登録の取り消しは、対象の name・slug・path を示して確認を求め、同意を得てから実行する。
`hikidashi` が `PATH` に無ければ、コマンドを実行せず導入の手順を案内する。

## 無効化・削除

```sh
claude plugin disable hikidashi@hikidashi     # 記録を止める（再開は enable）
claude plugin uninstall hikidashi@hikidashi   # plugin を削除する
claude plugin marketplace remove hikidashi
rm "$(command -v hikidashi)"
```

記録したデータも消すときは `~/.hikidashi/` を削除する。
