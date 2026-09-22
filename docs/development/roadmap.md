# ロードマップ

<!-- 書式: docs/document_system/templates/roadmap.template.md -->

## 予定（上から着手順）

- [ ] Go の雛形と test・lint のタスクを整える。 → #3
- [ ] 引き出しの解決と登録を実装する。 [dep #3] → #5
- [ ] hook イベントからセッション状態を記録する。 [dep #4] [dep #5] → #6
- [ ] hooks を hikidashi に繋ぐ plugin を追加する。 [dep #6] → #7
- [ ] 備忘録の注入と hikidashi notes を追加する。 [dep #6] → #8
- [ ] Stop 時に次アクションを抽出する。 [dep #6] [dep #7] → #9
- [ ] 引き出しの一覧から pane へ移動する。 [dep #6] → #10
- [ ] ステータスバーに入力待ちの件数を出す。 [dep #10] → #11

## 完了

- [x] データの置き場をリポジトリ外の `~/.hikidashi/drawers/` に決める。
- [x] 未検証の前提を実環境で確かめて反映する。 → #4
