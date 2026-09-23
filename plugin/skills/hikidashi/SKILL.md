---
name: hikidashi
description: hikidashi の案件の登録・状況確認・登録取り消しを、hikidashi コマンドを実行して引き受ける。「この案件を登録しといて」「このリポジトリを hikidashi に追加して」のように登録を頼まれたとき、「案件の状況は？」「api はどうなってる？」「待ってるセッションある？」のように案件やセッションの状況を尋ねられたとき、「この案件の登録を外して」「api を hikidashi から消して」のように登録の取り消しを頼まれたときに使う。
allowed-tools:
  - Bash(command -v hikidashi)
  - Bash(hikidashi add)
  - Bash(hikidashi show *)
---

# hikidashi

案件の登録・状況確認・登録取り消しを頼まれたら、`hikidashi` のコマンドを実行し、その結果を伝える。
実体は `hikidashi` のコマンドで、この skill はロジックを持たない。
`~/.hikidashi/` を直接読み書きせず、判断の根拠はコマンドの出力だけにする。

## 最初に確かめること

どの操作でも、最初に `command -v hikidashi` だけを単独で実行し、他のコマンドと繋げない。
何も出なければ `hikidashi` は未導入なので、コマンドを実行せずに次を案内して終える。

- 導入: `go install github.com/douhashi/hikidashi/cmd/hikidashi@latest`
- `go install` の出力先（`go env GOBIN`、未設定なら `$(go env GOPATH)/bin`）を `PATH` に通す
- 詳しくは https://github.com/douhashi/hikidashi の README を読む

## 登録する（add）

- 対象のリポジトリの中で、引数なしの `hikidashi add` を実行する
- 対象が今の作業ディレクトリと別のリポジトリなら `cd <path> && hikidashi add` とする
- 出力に出た引き出しの名前と tmux セッション名を伝える

## 状況を見る（show）

- 全案件の概況は `hikidashi show`、特定の案件を尋ねられたら `hikidashi show <案件>` を実行する
- `<案件>` はリポジトリ名か、同名の案件があるときは slug（`api-3f2a9c1b` の形）を渡す
- 答えは出力にある事実だけから組み立て、推測で補わない
- `issues:?` は件数を取れなかった（GitHub のリモートが無い・`gh` が使えない等）ことを表すので、取得に失敗したと伝える

## 登録を取り消す（remove）

`hikidashi remove` は記録したセッションを消すため、ユーザーの明示的な同意を得るまで実行しない。

1. 対象を示す。`hikidashi show <案件>` を実行し、見出しの行の name・path と `slug:` の行を示す。案件が指定されていなければ、先に `hikidashi show` の一覧から今の作業ディレクトリのリポジトリに当たる案件を選ぶ
2. 取り消してよいかを尋ね、ユーザーの返事を待つ。同意が無ければ実行しない
3. 同意を得たら、示した slug を渡して `hikidashi remove <slug>` を実行する
4. 空でない備忘録（`notes.md`）は残り、同じリポジトリで `hikidashi add` すると戻ること、tmux セッションは閉じないことを伝える
