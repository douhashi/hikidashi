# hikidashi

複数案件で並行して走らせている Claude Code セッションを案件ごとの「引き出し」に整理し、
いま何が動いていて、次に誰が何をすべきかをすぐ取り出せるようにするローカルツール。

hikidashi 本体（`hikidashi` コマンド）と、Claude Code の hooks を `hikidashi` に繋ぐ plugin からなる。
記録したデータは `~/.hikidashi/` に置き、案件のリポジトリには何も書かない。

## 前提

- git 2.31 以上
- tmux（tmux の pane で動くセッションだけを記録する）
- fzf（`hikidashi open` が一覧に使う）
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

## 無効化・削除

```sh
claude plugin disable hikidashi@hikidashi     # 記録を止める（再開は enable）
claude plugin uninstall hikidashi@hikidashi   # plugin を削除する
claude plugin marketplace remove hikidashi
rm "$(command -v hikidashi)"
```

記録したデータも消すときは `~/.hikidashi/` を削除する。
