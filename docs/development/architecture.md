# アーキテクチャ

hikidashi の設計原則と仕組みの構成。何を解くか・語彙は [`../business/concept.md`](../business/concept.md) が持つ。

## 設計原則

### 1. クライアントのリポジトリに痕跡を残さない

- 受託案件のリポジトリにある `CLAUDE.md` や `.gitignore`、`.git/` には一切手を入れない
- hooks は Claude Code plugin としてユーザースコープで有効にする。プロジェクトの設定には何も書かない
- データはすべてリポジトリ外の `~/.hikidashi/` に置く（下記「データの置き場」）

### 2. 書き手ごとにファイルを分ける

1 ファイルの書き手は 1 種類に限る。書き手が分かれていれば、同時書き込みの競合を設計で排除できる。

| データ | 書き手 | 形式 | 理由 |
| --- | --- | --- | --- |
| 備忘録 | 人間 | Markdown | エディタでそのまま編集でき、エージェントにも読ませやすい |
| セッション状態 | hook | 1 セッション 1 ファイルの JSON | 状態の現在値。`jq` / fzf で扱いやすい |
| 次アクション | 抽出プロセス | 1 セッション 1 ファイルの JSON | hook とは書き込むタイミングが別なので、ファイルを分ける |
| 履歴・集計（将来） | hook | SQLite（`~/.hikidashi/history.db`） | 放置時間や作業履歴の集計が必要になった段階で追加 |

- JSON の書き込みは一時ファイル → rename でアトミックに行う
- SQLite を足した後も、JSON は「現在値のキャッシュ」として残せる

### 3. 状態は決定論、次アクションは抽出

- **状態** は hook のイベントと transcript の記録から決定論で判定する。LLM は使わない（下記「状態モデル」）
- **次アクション**（いま何をしていて、次に何をすべきか）は会話ログから要約して抽出する

### 4. hook は Claude Code を妨げない

- hook は失敗しても exit 0 で終わり、エラーはログにだけ残す。exit 2（ブロック）は使わない
- 状態を記録する hook は同期で動かし、数 ms で終える。時間のかかる処理（抽出）は非同期に逃がす

## 構成要素

```mermaid
flowchart LR
  CC[Claude Code] -- hook 入力 JSON --> H[hikidashi hook]
  CC -- Stop（async） --> X[hikidashi extract]
  H -- 書く --> S[(sessions/ID.json)]
  H -- SessionStart で stdout --> CC
  N[(notes.md)] -- 読む --> H
  X -- claude -p --> M[軽量モデル]
  X -- 書く --> A[(sessions/ID.next.json)]
  O[hikidashi open] -- 読む --> S
  O -- 読む --> A
  O -- switch-client --> T[tmux pane]
  ST[hikidashi status] -- 読む --> S
```

| 要素 | 実体 | 役割 |
| --- | --- | --- |
| `hikidashi` | Go の単一バイナリ | 以下のサブコマンドをすべて持つ。`PATH` 上に置く |
| `hikidashi hook` | hook から起動 | 引き出しの登録、状態の記録、備忘録の注入 |
| `hikidashi extract` | `Stop` の async hook から起動 | transcript の末尾から次アクションを抽出する |
| `hikidashi open` | tmux の `display-popup` から起動 | fzf で一覧を出し、選んだ pane へ移動する |
| `hikidashi status` | tmux の `status-right` から起動 | 入力待ちの件数を出す |
| `hikidashi notes` | 人間が起動 | 現在の引き出しの `notes.md` を `$EDITOR` で開く |
| plugin | `plugin/hooks/hooks.json` | 各イベントを `hikidashi` に繋ぐだけ。ロジックは持たない |

- 言語は Go とする。hook はツール呼び出しのたびに起動するため起動の速さが効き、単一バイナリで依存なく配れる
- plugin はリポジトリ直下の marketplace（`.claude-plugin/marketplace.json`）から `hikidashi@hikidashi` として配る
- plugin にはバイナリを同梱しない。プラットフォームごとのバイナリを plugin に積むと配布が重くなるため、`PATH` 上の `hikidashi` を呼ぶ
- 外部コマンドへの依存は `git`・`tmux`・`fzf`・`claude` に限る
- 対象 OS は Linux とする。読み手の生存確認が `/proc` に頼るため、他の OS では `open`・`status` がエラーで終わる

## 状態モデル

| 状態 | 識別子 | 意味 |
| --- | --- | --- |
| 走行中 | `running` | Claude がターンを処理している |
| 入力待ち | `waiting` | ターンの途中で人間の判断（権限の承認・入力フォーム）を待っている |
| アイドル | `idle` | ターンが終わり、人間の次の指示を待っている |

終了したセッションは状態を持たず、ファイルごと消える（下記「セッションの後始末」）。

```mermaid
stateDiagram-v2
  [*] --> idle: SessionStart
  idle --> running: UserPromptSubmit
  running --> waiting: PermissionRequest / Notification（elicitation）
  waiting --> running: PostToolUse / UserPromptSubmit
  running --> idle: Stop / StopFailure / 中断の記録
  waiting --> idle: Stop / 中断の記録
  idle --> [*]: SessionEnd
  running --> [*]: SessionEnd
  waiting --> [*]: SessionEnd
```

| イベント | matcher | 状態 | その他の処理 |
| --- | --- | --- | --- |
| `SessionStart` | `startup` `resume` `clear` `fork` | `idle` | 引き出しを登録し、備忘録を注入する |
| `SessionStart` | `compact` | 変えない | 備忘録を注入する（圧縮で失われるため） |
| `UserPromptSubmit` | — | `running` | |
| `PermissionRequest` | — | `waiting` | |
| `Notification` | `elicitation_dialog` `elicitation_url_dialog` | `waiting` | |
| `PostToolUse` | — | `running` | 権限の承認後に走行へ戻ったことを拾う |
| `Stop` / `StopFailure` | — | `idle` | `Stop` では async で `hikidashi extract` を起動する |
| `SessionEnd` | — | — | セッションのファイルを消す |

- `PermissionRequest` で `waiting` にする理由: 権限プロンプトの直前に発火する。`Notification` の `permission_prompt` は表示から約 6 秒遅れて届くため使わない
- `PostToolUse` が必要な理由: 権限を承認しても `UserPromptSubmit` は発火しない。承認後に最初に発火するのはツール実行後の `PostToolUse` である
- 状態が変わらないイベントではファイルを書かない。`PostToolUse` は頻繁に発火するため、読むだけで終える
- `PostToolUse` は `waiting` のときだけ `running` にする（状態図どおり）。ファイルが無い・`idle` のセッションは走行中にしない
- `SessionStart`（`compact` 以外）は、ファイルがあっても `idle` で書き直し、`started_at` を今にする
- 他のイベントでファイルが無ければ（hikidashi の導入前から続くセッション等）、`started_at` を今にして作る
- 表の matcher の絞り込みは hikidashi が入力の `hook_event_name`・`source`・`notification_type` で行い、plugin の matcher に頼らない。何もしない入力（表に無い通知種別・未知のイベント）は git を起動する前に終える
- `session_id` はファイル名に使うため `[A-Za-z0-9_-]+` に限る。それ以外はパス横断を防ぐため入力の誤りとし、ログに残して終える

### 中断の扱い

ユーザーが Esc で中断すると、`Stop` も `Notification`（`idle_prompt` を含む）も発火せず、状態を変える hook が無い。
一方 transcript には、中断の記録として `[Request interrupted by user]`（ツール実行中は `[Request interrupted by user for tool use]`）で始まる `user` のエントリが残る。

- 読み手（`open` / `status`）は、`running` / `waiting` のセッションについて transcript の末尾を読み、最後の `user` のエントリが中断の記録で、その時刻が `state_changed_at` より後なら `idle` として扱う
- セッションのファイルは書き換えない（書き手は hook だけ）。次の `UserPromptSubmit` で hook が正しい状態に戻す

## データの置き場

```
~/.hikidashi/
├── hikidashi.log                      # hook・extract のエラーログ
└── drawers/<slug>/
    ├── drawer.json                    # 引き出しのメタ情報
    ├── notes.md                       # 備忘録（人間が書く）
    └── sessions/
        ├── <session_id>.json          # セッション状態（hook が書く）
        ├── <session_id>.next.json     # 次アクション（extract が書く）
        └── <session_id>.extract.lock  # 抽出の排他・間引き用
```

- ディレクトリは 0700、ファイルは 0600 で作る。案件の情報を他のユーザーから読めなくするため
- リポジトリ内（`<repo>/.hikidashi/` を `.git/info/exclude` で除外）は採らない。`.git` も案件リポジトリの一部であり、原則 1 に反するため
- 備忘録がリポジトリの横に無い分は、`hikidashi notes` で補う
- リポジトリを移動すると別の引き出しになる。頻度が低いため、移行の仕組みは持たない

### スキーマ

`drawer.json`

| フィールド | 内容 |
| --- | --- |
| `path` | リポジトリのルートの絶対パス（シンボリックリンク解決済み） |
| `name` | リポジトリ名（`path` の basename）。一覧での表示名 |
| `created_at` | 登録時刻（RFC 3339） |

`sessions/<session_id>.json`

| フィールド | 内容 |
| --- | --- |
| `session_id` | Claude Code のセッション ID |
| `cwd` | 直近の hook 入力の作業ディレクトリ |
| `tmux_pane` | `$TMUX_PANE`（例: `%12`） |
| `claude_pid` | `$CLAUDE_PID`（Claude Code 本体の PID）。生存確認に使う |
| `transcript_path` | 会話ログの JSONL のパス |
| `state` | `running` / `waiting` / `idle` |
| `state_changed_at` | 現在の状態に入った時刻。放置時間の表示に使う |
| `started_at` | セッションの開始時刻 |

`sessions/<session_id>.next.json`

| フィールド | 内容 |
| --- | --- |
| `summary` | いま何をしているか（1〜2 文） |
| `human_next` | 人間の次アクション。無ければ空 |
| `claude_next` | Claude の次アクション。無ければ空 |
| `blockers` | ブロッカーの配列（要素は文字列） |
| `generated_at` | 抽出した時刻 |

## 引き出しの解決と登録

hook は入力の `cwd` から引き出しを決める。

1. `git -C <cwd> rev-parse --path-format=absolute --git-common-dir --show-toplevel` を 1 回呼ぶ。共通の `.git` の basename が `.git` ならその親を、それ以外（submodule 等）は toplevel をリポジトリのルートとし、シンボリックリンクを解決する。worktree からでもメイン worktree に寄り、submodule はそれ自身の引き出しになる
2. git が非 0 で終わる `cwd`（Git 管理外・bare・`.git` の中・存在しない）は追跡しない（1 案件 = 1 リポジトリ）。備忘録の注入も行わない。git を起動できないときだけエラーにする
3. `<slug>` は `<name>-<ルートの絶対パスの SHA-256 の先頭 8 桁>` とする（例: `api-3f2a9c1b`）
4. `drawers/<slug>/` を作り、`drawer.json` が無ければ書く。これが登録であり、別途の一覧ファイルは持たない

- slug をハッシュにする理由: パスを `-` で繋ぐ方式は `a-b/c` と `a/b-c` が衝突し、衝突の検出と回避を別途書くことになる。ハッシュなら固定長で衝突を考えなくてよく、読みやすさは `name` の接頭辞で保つ
- 引き出しの一覧は `drawers/` 配下の列挙で得る。ディレクトリの作成は冪等なので、同時に発火しても競合・重複しない。同時の初回登録では `drawer.json` が後勝ちになるが、どちらも正しい内容なので許容する
- submodule を親の引き出しに寄せない理由: 独立したリポジトリであり、語彙どおり別の案件として扱う。submodule の worktree も別の引き出しになる
- `$TMUX_PANE` が空（tmux 外）のセッションは状態を記録しない。一覧から選んでも移動先が無いため。登録と備忘録の注入は行う

## 備忘録

### 注入

- `SessionStart` のすべての `source` で、`notes.md` を JSON の `hookSpecificOutput.additionalContext` に入れて stdout に 1 回で出す。tmux 外のセッションにも出す
- `additionalContext` は見出し `# hikidashi notes for <name> (<notes.md の絶対パス>)`、空行、`notes.md` の本文の順に並べる
- 素の Markdown で出さない理由: 本文が `{`〜`}` だと JSON と解釈され、注入の成否が内容次第になる
- `notes.md` が無い・空白だけなら何も出さない。注入と状態の記録は独立させ、片方が失敗してももう片方を行う
- Claude Code は 10,000 字を超える `additionalContext` をファイルに退避し、そのパスと先頭 2,000 字のプレビュー（見出しを含む）だけを渡す。Claude は退避先を読むよう促されないため、常に見せたいことは先頭に書く。hikidashi は切り詰めない

### `hikidashi notes`

- 作業ディレクトリの引き出しを登録し、`notes.md` が無ければ空（0600）で作ってから `$EDITOR` で開く。既存の中身は変えない
- `$EDITOR` は空白で分割し、シェルを介さずに起動する。引用符付きの値には対応しない
- 引数があれば exit 2。Git 管理外・`$EDITOR` が空・エディタの失敗は exit 1 とする。Git 管理外・`$EDITOR` が空ではデータルートに何も作らない

## 次アクションの抽出

1. `Stop` の async hook が `hikidashi extract` を起動する（hook の完了は待たれない）
2. `extract.lock` を flock で取る。取れなければ「再実行要求」の印を残して終わる。保持者は抽出後に印を見て、あればもう一度だけ回す（連続したイベントを後ろ寄せで 1 回にまとめる）
3. transcript の JSONL から末尾の `user` / `assistant` のテキストをバイト上限まで集める。ツールの入出力は落とす
4. `claude -p --model haiku --output-format json --json-schema <スキーマ> --no-session-persistence` に渡す
5. 成功したら `<session_id>.next.json` をアトミックに書く。失敗したら前回の結果を残し、ログに書く

- 再帰の防止: 子の `claude -p` でもユーザーの plugin の hooks は発火し、`$TMUX_PANE` も引き継がれる。放置すると抽出のたびに偽のセッションが記録されるため、子には `HIKIDASHI_DISABLE=1` を渡し、`hikidashi hook` はこれを見たら何もせずに終える。hooks を飛ばす `--bare` は OAuth 認証で使えないため採らない
- `--no-session-persistence` により、抽出の実行は transcript を残さない。構造化出力は結果の JSON の `structured_output` に入る
- 要約用のプロンプトとスキーマはバイナリに埋め込む

## セッションの後始末

- `SessionEnd` で `<session_id>.*` を消す。`--resume` で戻れば `SessionStart` で作り直される
- `SessionEnd` は `/exit` や pane の kill（SIGHUP）では発火するが、SIGKILL やクラッシュでは発火しない
- 取り残されたファイルは、読み手（`open` / `status`）が `claude_pid` のプロセスが生きていて名前（`/proc/<pid>/comm`）が `claude` であることを確かめ、そうでなければ消す
- pane の存在では生死を判定しない。claude が死んでも pane はシェルに戻って残るため
- 読み手が消すのは原則 2 の例外だが、消すのは書き手が二度と書かないファイルに限るため競合しない

## UI

- `hikidashi open` は fzf の一覧を出す。1 セッション 1 行で、引き出し名 → 状態 → 放置時間 → 次アクションの列順に並べる。「引き出し → セッション」はこの列順のことで、一覧を引き出しごとにはまとめない
- 並び順は `waiting` → `idle` → `running`。人間が捌くべきものを上に出す。同順位は放置の長い順、次に引き出し名の順とする
- 状態と放置時間は中断を反映した実効の値で出す。放置時間はその状態に入ってからの経過で、`5m` / `3h` / `2d` の形に切り捨てる
- 次アクションの列は `human_next`、空なら `summary` を出す。未抽出なら `-` を出し、一覧からは外さない
- fzf のプレビューに次アクションの全文と備忘録を出す。各行の先頭に fzf には見せない隠しキー `<slug>/<session_id>` を持たせ、プレビューは `hikidashi open --preview {1}` でそれを受け取る
- 隠しキーからパスは組み立てない。slug は登録済みの引き出しのディレクトリ名と照合し、`session_id` はファイル名に使える形に限る。データルートの外を読ませないため
- Enter で `tmux switch-client -t <pane>` し、該当 pane に移動する
- `hikidashi status` は `waiting` の件数だけを出す。0 件なら何も出さない
- tmux への組み込み（キーバインドと `status-right`）はユーザーが `tmux.conf` に書く。hikidashi は `tmux.conf` を書き換えない
- tmux のセッション名はリポジトリ名に揃える運用を推奨する。移動は pane ID で行うため必須ではない

## 未決事項

- **SQLite を導入するタイミング**
