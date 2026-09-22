# ロードマップ

<!-- 書式: docs/document_system/templates/roadmap.template.md -->

## 予定（上から着手順）

- [ ] Go のプロジェクト雛形を作り、テストと lint を `mise run check` に載せる。
- [ ] 引き出しを登録し状態を記録する `hikidashi hook` と、それを繋ぐ plugin を作る。
- [ ] `SessionStart` で案件の備忘録をコンテキストに注入する。
- [ ] 次アクションを抽出する `hikidashi extract` と要約プロンプトを作る。
- [ ] 一覧から pane へ移動する `hikidashi open` と `hikidashi status` を作る。
- [ ] 備忘録を開く `hikidashi notes` を作る。

## 完了

- [x] データの置き場をリポジトリ外の `~/.hikidashi/drawers/` に決める。
