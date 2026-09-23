# hikidashi

複数案件で並行して走らせている Claude Code セッションを案件ごとの「引き出し」に整理し、
いま何が動いていて、次に誰が何をすべきかをすぐ取り出せるようにするローカルツール。

hikidashi 本体（`hikidashi` コマンド）と、Claude Code の hooks を `hikidashi` に繋ぐ plugin からなる。
記録したデータは `~/.hikidashi/` に置き、案件のリポジトリには何も書かない。

## 前提

- git 2.31 以上
- tmux（tmux の pane で動くセッションだけを記録する）
- fzf（`hikidashi open` が一覧に使う）
- GitHub CLI（`gh`。`hikidashi show` が Open な Issue を数えるのに使う。`gh auth login` 済みであること）
- Go
- Claude Code

## 導入

```sh
go install github.com/douhashi/hikidashi/cmd/hikidashi@latest
claude plugin marketplace add douhashi/hikidashi
claude plugin install hikidashi@hikidashi
```

`go install` の出力先（`go env GOBIN`、未設定なら `$(go env GOPATH)/bin`）は `PATH` に通しておく。
plugin は起動時に読み込まれるため、動いている Claude Code のセッションは再起動する。
ターンが終わるたびに `claude -p`（haiku）で次アクションを要約するため、その分の利用枠を使う。

> **private の間の注記**: リポジトリが private の間は、上の手順の前に次を済ませる。
> `gh auth setup-git` は `go install` と `marketplace add` の両方が使う git に GitHub の認証を渡す。
>
> ```sh
> gh auth login
> gh auth setup-git
> export GOPRIVATE=github.com/douhashi/hikidashi
> ```

## 案件を登録する

hikidashi は、登録した案件（リポジトリ）のセッションだけを記録する。案件のリポジトリの中で `hikidashi add` を実行する。

```sh
cd ~/src/api
hikidashi add
# drawer: api-3f2a9c1b (registered)
# tmux session: api-3f2a9c1b (created)
```

案件を引き出しとして登録し、リポジトリのルートを作業ディレクトリとする tmux セッションを用意する（既にあれば何もしない）。
worktree やサブディレクトリから実行しても、メインのリポジトリが登録される。
tmux のセッション名は `<リポジトリ名>-<パスのハッシュ 8 桁>` で、`.` と `:` は `_` に置き換わる（例: `example_com-3f2a9c1b`）。
Claude Code はこのセッションの window / pane で動かす。

## 案件の登録を取り消す

`hikidashi remove` は、案件の登録を取り消し、記録したセッションを消す。案件のリポジトリの中で実行するか、引き出しの名前を渡す。

```sh
hikidashi remove api            # 名前（同名が複数あれば api-3f2a9c1b のように指定する）
# drawer: api-3f2a9c1b (removed)
# notes: /home/you/.hikidashi/drawers/api-3f2a9c1b/notes.md (kept)
```

備忘録（`notes.md`）は空でなければ残り、同じリポジトリで `hikidashi add` すると戻る。
tmux セッションは閉じないため、不要なら `tmux kill-session -t api-3f2a9c1b` で閉じる。

## tmux から開く

`hikidashi open` は、全引き出しのセッションを fzf に並べ、選んだセッションの pane へ移動する。
tmux の中で動かす必要があるため、`tmux.conf` にポップアップで開くキーバインドを書く。

```tmux
# prefix + h で一覧を開く。-E で、移動した後や Esc で閉じた後にポップアップも閉じる。
bind-key h display-popup -E -w 80% -h 60% hikidashi open
```

`hikidashi` と `fzf` は tmux サーバーの `PATH` から見える場所に置く。

## ステータスバーに出す

`hikidashi status` は、入力待ち（`waiting`）のセッションの件数を出す。0 件なら何も出さない。
`tmux.conf` の `status-right` に組み込む。

```tmux
set -g status-right '#(hikidashi status) %H:%M'
```

表示は `status-interval`（既定 15 秒）ごとに更新される。`hikidashi` は tmux サーバーの `PATH` から見える場所に置く。

件数の代わりに `!` が出たら、集計に失敗している。tmux は理由（stderr）を捨てるため、
端末で `hikidashi status` を実行して理由を見る。

## 案件の状況を見る

`hikidashi show` は、登録済みの全案件の概況を 1 案件 1 行で出す。
Open な Issue の件数と、状態ごとのセッションの件数が並ぶ。

```sh
hikidashi show
# api       api-3f2a9c1b       issues:3  running:1  waiting:1  idle:2
# frontend  frontend-0a1b2c3d  issues:?  running:0  waiting:0  idle:0
```

`issues:?` は件数を取れなかった（GitHub のリモートが無い・`gh` が使えない等）ことを表し、理由は stderr に出る。
件数は `gh` が選ぶリポジトリのもので、fork で `upstream` を持つリポジトリでは `gh repo set-default` で数える先を選べる。

`hikidashi show <案件>` は、その案件のセッションごとの状態・放置時間・pane・次アクションと、備忘録を出す。
`<案件>` にはリポジトリ名か、同名の案件があるときは slug（`api-3f2a9c1b`）を指定する。

```sh
hikidashi show api
```

出力は素のテキストで、Claude Code に読ませてもそのまま使える。

## Claude Code に頼む

plugin の skill（`hikidashi:hikidashi`）により、登録・状況確認・登録取り消しを Claude Code に自然な言葉で頼める。
Claude Code は `hikidashi` のコマンドを実行し、その出力をもとに答える。

```text
この案件を登録しといて      # hikidashi add
案件の状況は？              # hikidashi show
api はどうなってる？        # hikidashi show api
この案件の登録を外して      # 確認のあと hikidashi remove
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
