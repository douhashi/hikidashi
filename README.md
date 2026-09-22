# hikidashi

複数案件で並行して走らせている Claude Code セッションを案件ごとの「引き出し」に整理し、
いま何が動いていて、次に誰が何をすべきかをすぐ取り出せるようにするローカルツール。

hikidashi 本体（`hikidashi` コマンド）と、Claude Code の hooks を `hikidashi` に繋ぐ plugin からなる。
記録したデータは `~/.hikidashi/` に置き、案件のリポジトリには何も書かない。

## 前提

- git 2.31 以上
- tmux（tmux の pane で動くセッションだけを記録する）
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

> **private の間の注記**: リポジトリが private の間は、上の手順の前に次を済ませる。
> `gh auth setup-git` は `go install` と `marketplace add` の両方が使う git に GitHub の認証を渡す。
>
> ```sh
> gh auth login
> gh auth setup-git
> export GOPRIVATE=github.com/douhashi/hikidashi
> ```

## 無効化・削除

```sh
claude plugin disable hikidashi@hikidashi     # 記録を止める（再開は enable）
claude plugin uninstall hikidashi@hikidashi   # plugin を削除する
claude plugin marketplace remove hikidashi
rm "$(command -v hikidashi)"
```

記録したデータも消すときは `~/.hikidashi/` を削除する。
