# 19 開放性・つながり・選択（ATProto の特長を活かす拡張）

CHAIN を「Bluesky でログインできるアプリ」から「ATProto でしかできないアプリ」にするための拡張をまとめる。
03（ATProto 連携）・10（instance）・11（moderation）・13（アカウント削除）・17（フィード）を前提にし、足りない決めごとだけをここで足す。
既存仕様書の変更が要るところは §9 にまとめる。

## 0. 目的

### 0.1 評価軸との対応

Bluesky 賞の観点（ATProto の特長を活かしているか / つながりやコミュニティを生むか / 利用者が選べて安心か）と、ATProto の理念（credible exit、データを自分で持つ、モデレーションを選んで重ねる）に対して、いまの設計の弱点と、それを埋める機能を対応させる。

| 弱点（いまの設計） | 埋める機能 | 主に効く観点 |
| --- | --- | --- |
| moderation が運営の一律判定だけで、利用者もコミュニティも選べない | F1 Space ごとの moderation 方針、外部ラベラーの購読 | 選べる・安心 |
| すれ違い・フレンド・Space の中身がサーバーにしかなく、持ち出せない | F2 データのエクスポート、F3 の個人用 Space | 持ち運べる |
| グループ / Party が CHAIN の中で閉じている | F3 Spaces 統合（他のアプリからも同じコミュニティを使える） | 持ち運べる・つながり |
| CHAIN で生まれた関係が Bluesky に持ち出せない | F4 Bluesky へのつながりの持ち出し（フォロー、リスト、すれ違いフィード） | つながり・開放性 |
| lexicon がリポジトリにも外部にも無く、他の開発者が使えない | F5 lexicon の公開 | 開放性 |
| どのデータがどこにあるか利用者から見えない | F6 データの置き場所の表示、PDS の選択 | 選べる・安心 |

### 0.2 守る前提（変えないもの）

- 位置・すれ違いの生データは公開の PDS に書かない（README「個人データ」）
- Space 内の内容と moderation の結果は、Space の外（公開ラベラーなど）に出さない（03 §7.1）
- Bluesky 側に何かを書くときは、**操作ごとに本人が選ぶ**。自動では書かない（17 §1 の `alsoBluesky` と同じ考え方）
- 他人の関係（誰と誰がいっしょにいたか）を公開する操作は、**関係するすべての人の同意**を取る

## 1. 一覧と実現可能性

| # | 機能 | 優先度 | 規模 | 実現可能性 | 主なリスク・依存 |
| --- | --- | --- | --- | --- | --- |
| F1a | Space ごとの moderation 方針 | P1 | M（backend 3〜4 日、web 2 日） | 高 | Jev のカテゴリ別の日本語精度（README Q10）。質問数が増えた時の費用（L7） |
| F1b | 外部ラベラーの購読（公開投稿） | P2 | M | 中〜高 | CHAIN の公開投稿（`jp.i32.chain.feed.post`）にラベルを付ける外部ラベラーがほぼ無い。Bluesky への同時投稿分でしか効かない |
| F1c | CHAIN をラベラーとして公開（公開投稿のみ） | P2 | M | 中 | `subscribeLabels`（CBOR のフレーム配信）を自前で実装する。ラベル署名は indigo `atproto/labeling` にある |
| F2 | データのエクスポート | P1 | S〜M（backend 2〜3 日、web 1 日） | 高 | Android でのファイル保存（Capacitor のプラグイン追加） |
| F3a | Spaces alpha でのデモ統合 | P2 | L（1.5〜3 週間） | 中〜低 | alpha は破壊的変更あり・バックアップ無し・終了時に PDS ごと消える。Go の SDK が無い |
| F3b | Spaces 正式版での本統合 | P2（正式版後） | M（indigo が対応した場合） | 正式版次第 | bsky.social の対応状況（README Q4） |
| F3c | 個人用 Space（フレンドなどの持ち出し） | P3 | M | 正式版次第 | 単一メンバーの Space の仕様が固まるか |
| F4a | フレンド → Bluesky フォロー | P1 | S（1 日） | 高 | scope の追加で既存ユーザーの再ログインが要る |
| F4b | Party / グループ → Bluesky リスト・スターターパック | P2 | M（3 日） | 高 | 同意の取り方の UX |
| F4c | すれ違いフィード（Bluesky のカスタムフィード） | P1 | M（3〜4 日） | 高 | Jetstream で `app.bsky.feed.post` を DID で絞る方法（§5.3.4） |
| F5 | lexicon の公開 | P1 | S | 高 | CHAIN 用の did:plc アカウントと DNS の設定 |
| F6 | データの置き場所の表示、PDS の選択 | P1 | S | 高 | PDS の URL でログインできるかの確認（§7.2） |

規模: S = 1〜2 日、M = 3〜5 日、L = 1 週間超（1 人で作る場合）。

### 1.1 着手の順番

| 段階 | 内容 | 理由 |
| --- | --- | --- |
| 1 | F5 → F6 → F2 | 小さく確実。「開放性」と「安心」を説明する土台になる |
| 2 | F1a → F4a → F4c | コミュニティごとに選べること、関係を外に持ち出せることを見せられる。どれも alpha に依存しない |
| 3 | F4b、F1b / F1c | 同意の UX とラベラーの運用が要る |
| 4 | F3a（デモ）→ 正式版後に F3b・F3c | alpha に依存するので、主動線に組み込まない。デモと設計の説明に使う |

### 1.2 CHAIN が持つ ATProto の身元

いまは space authority 用の `did:web:{API_HOST}` だけを持つ。F1c・F4c・F5 のために、レコードを置ける repo を持つアカウントがもう 1 つ要る。

| 身元 | 形 | 役割 | 機能 |
| --- | --- | --- | --- |
| サービス DID | `did:web:chain.i32.jp`（既存） | space authority、フィードジェネレーターのサービス | F3、F4c |
| CHAIN アカウント | `did:plc:...`（handle `chain.i32.jp`）。PDS は bsky.social で作る | lexicon の schema record、`app.bsky.feed.generator`、`app.bsky.labeler.service` を置く repo。ラベル署名鍵 | F1c、F4c、F5 |

- CHAIN アカウントの認証情報（app password またはその PDS の OAuth）は、ユーザーのセッションとは別に設定値 `CHAIN_SERVICE_ACCOUNT_*` で持つ。record の作成はめったに無いので、`cmd/` に運用用のコマンドを置き、backend の常駐プロセスからは書かない
- ラベラーにするには、DID ドキュメントに `#atproto_labeler` のサービスとラベル署名鍵を足す PLC 操作が要る（`goat` で行う）。F1c に着手するときにだけ行う

## 2. F1 Space ごとの moderation 方針

### 2.1 考え方

ATProto のモデレーションは「ラベルを付ける」ことと「ラベルをどう扱うか」を分けている。ラベルを付けるのはラベラー、扱いを決めるのは利用者。
CHAIN では、Space 内の投稿にラベルを付けるのは CHAIN（Jev）のまま、**ラベルの扱いを Space（コミュニティ）と閲覧者が決める**ようにする。

```
投稿 ──▶ Jev（1 回の呼び出し）──▶ カテゴリごとの確率（records.mod_scores_json に保存）
                                         │
           ┌─────────────────────────────┼─────────────────────────────┐
           ▼                             ▼                             ▼
     運営の下限（変えられない）    Space の方針（owner / メンバー）     閲覧者の好み（本人）
           └──────────────── いちばん厳しいものを採る ──────────────────┘
                                         ▼
                              表示の指示（show / blur / hide）
```

- 確率を保存しておき、扱いは**読むたびに計算する**。方針を変えたとき、Jev を呼び直さずに過去の投稿にも効く
- 投稿を拒否する（`reject`）かどうかは、書く時点の下限と Space の方針で決める。閲覧者の好みは表示だけに効く

### 2.2 カテゴリ

| カテゴリ | 意味 | 区分 |
| --- | --- | --- |
| `harassment` | 特定の個人や集団への誹謗中傷 | 下限（常に reject） |
| `hate` | 属性に基づく差別・憎悪の扇動 | 下限 |
| `threat` | 脅迫・殺害予告 | 下限 |
| `doxxing` | 個人情報の暴露 | 下限 |
| `sexual_minors` | 未成年に関わる性的内容 | 下限 |
| `self_harm_promotion` | 自傷・自殺の扇動 | 下限 |
| `profanity` | 特定の相手に向けていない乱暴な言葉 | 選べる |
| `sexual` | 性的な内容（成人どうし） | 選べる |
| `violence` | 暴力的・グロテスクな描写 | 選べる |
| `self_harm` | 自傷の話題（扇動ではないもの） | 選べる |
| `spam` | 宣伝・勧誘・同じ内容の繰り返し | 選べる |
| `politics` | 政治・宗教の話題 | 選べる |
| `spoiler` | 作品のネタバレ（P2。文脈が無いと精度が出ないため実験扱い） | 選べる |

- 下限のカテゴリは、いまの `block`（11 §1）の範囲と同じ。02 §8 の「block を保証する」はそのまま守る
- 下限を Space ごとに緩めることはできない

### 2.3 扱い（action）

| action | 書く時 | 表示 |
| --- | --- | --- |
| `allow` | 通す | そのまま |
| `warn` | 通す | ぼかし。タップで表示（いまの `blur`） |
| `hide` | 通す | 返さない。投稿者本人には `"hidden": "moderation"` で返す |
| `reject` | `422 moderation_blocked` で拒否する | — |

`reject` を選べるのは Space の方針だけ（閲覧者の好みには無い）。

### 2.4 方針を決める人

| Space | 方針を決める人 | 既定 |
| --- | --- | --- |
| グループ（`origin=explicit`） | owner | 作成時に選んだプリセット（既定は「標準」） |
| グループ（`origin=natural`） | メンバーの投票（P2。`group_votes.kind` に `mod_policy` を足す） | 「標準」 |
| Party | 決める人はいない。**未離脱のメンバー全員の「Party の既定」のうち、カテゴリごとにいちばん厳しいもの**を自動で採る | 各メンバーの `party_mod_prefs`。未設定なら「標準」 |

- Party に owner がいないのは 08 の設計どおり。誰かが緩くすると他のメンバーが見たくないものを見ることになるので、いちばん厳しいものに合わせる
- Party のメンバーが変わったら方針を計算し直し、`acc:` と同じ契機でキャッシュを消す

### 2.5 プリセット

UI ではまずプリセットを選ばせ、詳細でカテゴリごとに変えられるようにする。

| カテゴリ | ゆるい | 標準 | 厳しい |
| --- | --- | --- | --- |
| `profanity` | allow | allow | warn |
| `sexual` | warn | warn | reject |
| `violence` | allow | warn | hide |
| `self_harm` | warn | warn | hide |
| `spam` | warn | hide | reject |
| `politics` | allow | allow | warn |
| `spoiler` | allow | allow | warn |

いまの挙動（`warn` は既定で blur）は「標準」と同じになるように選んである。

### 2.6 閲覧者の好み

`user_settings.mod_prefs_json` に、選べるカテゴリごとの `allow` / `warn` / `hide` を持つ。いまの `warn_display` はこれに置き換える（§9）。

最終的な表示は次の順で決める。

1. 下限のカテゴリに当たる（`mod_label = block`）→ 返さない
2. Space の方針と閲覧者の好みを、カテゴリごとに厳しい方で合わせる（`hide` > `warn` > `allow`）
3. 当たったカテゴリ（§2.7 のしきい値を超えたもの）の中でいちばん厳しい扱いを採る

公開投稿（17）には Space の方針が無いので、閲覧者の好みだけで決める。

### 2.7 判定（Jev への質問）

11 §5.2 の質問を次のように変える。呼び出しは 1 回のまま。

| 質問 | type | 用途 |
| --- | --- | --- |
| `safety` | choice（`ok` / `block`） | 下限。`block` の基準文は下限のカテゴリをまとめたもの |
| `floor_category` | choice | 下限のどれに当たるか（422 の `details.categories` 用） |
| `c_profanity` 〜 `c_spoiler` | noul（カテゴリごとに 1 つ） | 選べるカテゴリの確率 |
| `pin` | noul | 10 §6。instance の投稿のときだけ |

- 選べるカテゴリは、確率 `>= 0.6` で「当たった」とみなす（仮置き。README Q10 と同じ評価用データで調整する）
- `spoiler` は Space の方針で `allow` 以外になっているときだけ質問に含める（精度が低く、費用も増えるため）
- 質問が増えると費用が増えるかどうかは NanoGPT の課金体系による。L7 の見積もりで確かめる
- 保存する値: `records.mod_scores_json = {"profanity":0.12,"sexual":0.03,...}`、`records.mod_label` は下限の結果（`ok` / `block`）だけを持つようにする。`warn` は確率から計算するので保存しない
- 方針の版: `prompts.go` の版番号を `records.mod_version` に残す。版が変わった後に方針を厳しくしても、古い投稿は古い質問の確率で判定される。これは許容する

### 2.8 Space 内のラベルの形

Space 内のラベルは Space の外に出さないが、将来 Spaces に移した時に他のアプリも同じものを扱えるよう、ATProto のラベル（`com.atproto.label.defs#label`）と同じ形で API に返す。

```json
{
  "src": "did:web:chain.i32.jp",
  "uri": "at://did:web:chain.i32.jp/space/jp.i32.chain.space.group/0192.../did:plc:.../jp.i32.chain.instance.post/3m...",
  "val": "sexual",
  "cts": "2026-10-18T03:00:00.000Z"
}
```

- 署名はしない（Space の外に出さないため）。F1c で公開する公開投稿のラベルだけを署名する
- `val` はカテゴリ名そのまま。ATProto の全体共通のラベル値（`porn`、`gore` など）とは対応表を §2.11 に持つ

### 2.9 方針の置き場所と lexicon

方針は Space の属性なので、authority が書く Space 内の record として持つ（`jp.i32.chain.instance.pin` と同じ扱い）。Spaces に移した時に、他のアプリも同じ方針に従えるようにするため。

`jp.i32.chain.instance.modPolicy`（record、key: `literal:self`、authority が書く）

```json
{
  "preset": "standard | relaxed | strict | custom",
  "categories": {
    "profanity": "allow", "sexual": "warn", "violence": "warn",
    "self_harm": "warn", "spam": "hide", "politics": "allow", "spoiler": "allow"
  },
  "updatedBy": "did（owner。Party の自動計算なら authority）",
  "updatedAt": "datetime"
}
```

- P1 の間は `spaces.mod_policy_json` に同じ JSON を持つ（SQLite の `records` に入れると、一覧の取得のたびに引く必要があるため）。Spaces 統合時に record として書き出す
- 下限のカテゴリはこの record に書かない（変えられないため）

### 2.10 API

| メソッド | パス | 内容 | 権限 |
| --- | --- | --- | --- |
| GET | `/api/v1/spaces/{id}/moderation` | 方針（プリセット、カテゴリごとの扱い、最終更新者と時刻）。Party なら計算結果と「いちばん厳しい人に合わせた」旨 | open グループは非メンバーも可。それ以外はメンバー |
| PUT | `/api/v1/groups/{id}/moderation` | `{"preset":"custom","categories":{...}}` | owner |
| PATCH | `/api/v1/me/settings` | `modPrefs`（閲覧者の好み）、`partyModPrefs`（Party の既定）を追加 | 本人 |

- `PUT` で方針が変わったら `audit_log`（`moderation.policy`）に残し、Space のチャンネルに `space.moderation_updated` を送る。メンバーに通知（種別 `space.policy`）を積む。**緩くなる変更**のときは、通知に「変更点」を入れ、気に入らないメンバーが退出できるようにする
- open グループの参加前の画面（09 §5 の nearby の詳細）に方針の要約を出す。「どんな場所か」を知ってから参加できるようにする
- 投稿の要素の `moderation`（11 §4）は次の形にする

```json
{ "moderation": { "display": "blur", "labels": ["sexual"], "source": "space" } }
```

`source` は、表示を決めたのが Space の方針（`space`）か閲覧者の好み（`viewer`）か。UI で「このグループの設定でぼかしています」と出すため。

### 2.11 外部ラベラーの購読（F1b、P2）

ATProto の「モデレーションを選んで重ねる」に対応する。**公開投稿（17）のフィードだけ**に効く。Space 内の投稿には効かない（外部ラベラーは Space の中身を読めないため）。

- `user_settings.labelers_json`: 購読するラベラーの DID の配列（最大 5 件）
- フィードを返す時、対象の投稿の URI（`jp.i32.chain.feed.post` と、`bsky` で指す `app.bsky.feed.post`）について、各ラベラーのサービス（DID ドキュメントの `#atproto_labeler`）の `com.atproto.label.queryLabels` を呼ぶ。結果は Redis `lbl:{labelerDid}:{uri}` に 10 分キャッシュする
- ラベル値の扱いは、ラベラーの `app.bsky.labeler.service` record の `policies.labelValueDefinitions` を読み、閲覧者が値ごとに `allow` / `warn` / `hide` を選ぶ。全体共通の値は次のように CHAIN のカテゴリに対応させて、閲覧者の好みの初期値にする

| 全体共通のラベル値 | CHAIN のカテゴリ |
| --- | --- |
| `porn`、`sexual`、`nudity` | `sexual` |
| `graphic-media`、`gore` | `violence` |
| `!hide` | 常に hide |
| `!warn` | 常に warn |

- 限界: 外部ラベラーが見ているのは主に `app.bsky.*` なので、Bluesky に同時投稿していない CHAIN の投稿にはほぼラベルが付かない。これは UI に書く

### 2.12 CHAIN をラベラーとして公開（F1c、P2）

公開投稿に CHAIN（Jev）が付けたラベルを、Bluesky の利用者も購読できるようにする。

- 対象: `public_posts`（`jp.i32.chain.feed.post`）と、その `bsky` が指す `app.bsky.feed.post`。**Space 内の投稿は対象外**（0.2）
- エンドポイント: `GET /xrpc/com.atproto.label.queryLabels`、`GET /xrpc/com.atproto.label.subscribeLabels`（WebSocket、CBOR のフレーム）
- 署名: CHAIN アカウント（1.2）のラベル署名鍵で、indigo `atproto/labeling.Label.Sign` を使う
- 新しい表 `labels_out`（`seq`、`uri`、`cid`、`val`、`neg`、`cts`、`sig`）。`seq` は `subscribeLabels` のカーソル
- `app.bsky.labeler.service` record（CHAIN アカウントの repo）に、§2.2 の選べるカテゴリを `labelValueDefinitions` として載せる。下限のカテゴリは、CHAIN では投稿自体を拒否するので付くことがない

### 2.13 データモデルの変更

| 表 | 列 | 内容 |
| --- | --- | --- |
| `spaces` | `mod_policy_json TEXT` | §2.9。NULL なら「標準」 |
| `records` | `mod_scores_json TEXT`、`mod_version INTEGER` | §2.7 |
| `records` | `mod_label` の CHECK | `('ok','block')` にする（`warn` は計算で出す） |
| `public_posts` | `mod_scores_json TEXT`、`mod_version INTEGER` | 公開投稿も同じ形 |
| `user_settings` | `mod_prefs_json TEXT NOT NULL DEFAULT '{}'`、`party_mod_prefs_json TEXT`、`labelers_json TEXT NOT NULL DEFAULT '[]'` | §2.6、§2.4、§2.11。`warn_display` は削除 |
| `group_votes` | `kind` の CHECK に `'mod_policy'` を追加、`payload_json TEXT` | §2.4（P2） |

04 §5 のとおり migration は使わないので、schema.sql と 04 を直して作り直す。

### 2.14 テスト

- 方針の合成: 下限・Space・閲覧者の組み合わせで、表に書いた結果になる（表駆動）
- Party: メンバーが加わる / 抜けると方針が計算し直され、いちばん厳しいものになる
- 方針の変更が過去の投稿にも効く（Jev を呼ばない。`moderation.Fake` の呼び出し回数で確かめる）
- `reject` の Space には投稿できず、別の Space には同じ本文で投稿できる（`spaceIds` が複数のときは、1 つでも reject なら全体を拒否する。10 §4.1 の write 判定と同じ扱い）
- 非メンバーが open グループの方針を読める / closed グループの方針は読めない

## 3. F2 データのエクスポート

### 3.1 方針

- **自分のデータだけ**を出す。他人の投稿や写真は、同じ Space にあっても出さない（URI の参照だけを出す）。他人のデータを本人の同意なく持ち出させないため
- ATProto の record と同じ形（`$type` 付きの JSON と at:// の URI）で出し、将来 Spaces や別のサーバーに書き戻せるようにする
- PDS にあるデータ（公開投稿、プロフィール）は、PDS のエクスポート（`com.atproto.sync.getRepo` の CAR。Bluesky アプリの設定からも取れる）が正。CHAIN のエクスポートには、索引にある分の写しと、取り方の案内を入れる

### 3.2 含めるもの

| データ | 含める範囲 | 置き場所（ZIP 内） |
| --- | --- | --- |
| アカウント | DID、handle、PDS の URL、登録日時 | `manifest.json` |
| 設定・同意 | `user_settings` の全列（同意の版と時刻を含む） | `chain/settings.json` |
| プロフィール | `profiles` の写し | `chain/profile.json` |
| すれ違い | `visibility = revealed` のものだけ。相手の DID と handle、成立日時、回数。**pending・hidden は出さない**（相手がまだ公開していない、または非公開にしたすれ違いを漏らさないため）。ブロック関係の相手は出さない | `chain/encounters.json` |
| フレンド | 相手の DID と handle、成立日時、経路（`via`） | `chain/friends.json` |
| フレンド申請・招待 | 自分が出した / 受けた申請の状態。招待コードそのものは出さない | `chain/friend_requests.json` |
| 位置共有 | 相手ごとの精度の設定、エリア（自分が作ったもの） | `chain/location_shares.json`、`chain/areas.json` |
| 位置の履歴 | **保存していない**（Redis の TTL で消える）。その旨を README に書く | — |
| Space への参加 | Space の ID・種類・名前・役割・参加 / 離脱日時・状態 | `chain/spaces.json` |
| 自分が書いた Space 内の record | 投稿・写真の record（論理削除済みを除く）。moderation のスコアと表示の扱いも入れる | `repo/{spaceId}.jsonl` |
| 自分が上げた写真 | 変換後の JPEG | `media/{id}.jpg` |
| 公開投稿 | `public_posts` の写し（URI・CID・本文） | `repo/public.jsonl` |
| ブロック | 自分がブロックした DID | `chain/blocks.json` |
| 予定 | `schedule_entries`（今日と明日の分しか持たない） | `chain/schedule.json` |
| 通知 | 直近 90 日 | `chain/notifications.json` |
| 自分の投稿への moderation の記録 | `audit_log` のうち `target` が自分の record のもの | `chain/moderation_log.json` |

含めないもの: beacon token（他人の端末に配った識別子のため）、OAuth・アプリのセッション、他人の投稿と写真、`audit_log` の他人の操作。

### 3.3 形式

ZIP 1 つ。

```
chain-export-{did の英数字部分}-{yyyymmdd}.zip
├─ README.txt                 中身の説明（日本語）、PDS のエクスポートの取り方
├─ manifest.json
├─ chain/*.json
├─ repo/public.jsonl
├─ repo/{spaceId}.jsonl
├─ media/{id}.jpg
└─ schemas/*.json             chain/*.json の JSON Schema、使った lexicon
```

`manifest.json`

```json
{
  "format": "jp.i32.chain.export",
  "version": 1,
  "generatedAt": "2026-10-18T03:00:00.000Z",
  "did": "did:plc:...",
  "handle": "alice.bsky.social",
  "pds": "https://...",
  "counts": { "encounters": 120, "friends": 14, "spaces": 9, "records": 342, "media": 51 }
}
```

`repo/*.jsonl` の 1 行

```json
{"uri":"at://did:web:chain.i32.jp/space/jp.i32.chain.space.party/0192.../did:plc:.../jp.i32.chain.instance.post/3m...","collection":"jp.i32.chain.instance.post","rkey":"3m...","value":{"$type":"jp.i32.chain.instance.post","text":"...","createdAt":"..."},"moderation":{"labels":[],"scores":{"profanity":0.02}}}
```

- `format` と `version` は後方互換を保つ。中身の形を変えるときは `version` を上げる
- 署名付きの CAR にはしない（Space 内の record はまだユーザーの鍵で署名されていないため）。Spaces 統合後は、Spaces 側の repo のエクスポートに任せる

### 3.4 API

| メソッド | パス | 内容 |
| --- | --- | --- |
| POST | `/api/v1/me/exports` | 作成を依頼する。`202 {"id": "...", "status": "queued"}`。24 時間に 1 回まで（超えたら `429`） |
| GET | `/api/v1/me/exports` | 自分の依頼の一覧（直近 5 件） |
| GET | `/api/v1/me/exports/{id}` | `{"status": "queued|building|ready|failed|expired", "size": 12345678, "downloadUrl": "...", "expiresAt": "..."}`。`downloadUrl` は `ready` のときだけ、この取得のたびに発行する |
| GET | `/exports/{id}.zip?exp=&sig=` | ダウンロード。署名と期限だけを確認する（10 §4.4 のメディアと同じ方式） |
| DELETE | `/api/v1/me/exports/{id}` | ファイルを消す |

- 署名付き URL の期限は 15 分（メディアの 1 時間より短くする。中身が機微なため）。`Cache-Control: no-store`
- 作成は常駐ワーカー `export.Builder`（outbox と同じくポーリング、同時に 1 件）。ZIP はストリームで書き、写真をメモリに溜めない
- 保存先は `{DATA_DIR}/exports/{id}.zip`。作成から 24 時間で `cleanup.Janitor` が消し、`expired` にする
- 完成したら通知（種別 `export.ready`）とユーザーチャンネルの WebSocket `export.ready` を送る
- アカウント削除（13 §5）の時は、依頼とファイルをすぐに消す。削除の確認画面に「先にエクスポートする」導線を置く

新しい表

```sql
CREATE TABLE exports (
  id          TEXT PRIMARY KEY,
  did         TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  status      TEXT NOT NULL CHECK (status IN ('queued','building','ready','failed','expired')),
  size_bytes  INTEGER,
  error       TEXT,
  created_at  INTEGER NOT NULL,
  ready_at    INTEGER,
  expires_at  INTEGER
);
CREATE INDEX exports_did ON exports(did, created_at);
CREATE INDEX exports_status ON exports(status, created_at);
```

### 3.5 クライアント

- 設定 → 「データの置き場所」（§7）→「エクスポート」
- Android では `downloadUrl` を取得し、ファイルとして保存して共有シートを開く（`@capacitor/filesystem` と共有用のプラグインを追加する。web では `<a download>`）
- 作成中は状態を表示し、`export.ready` で更新する

### 3.6 書き戻し（P3）

- いまの CHAIN は 1 台のサーバーなので、書き戻しが要るのは「別の CHAIN 互換のサーバーに移る」か「Spaces の自分の repo に移す」ときだけ。形式（`version: 1`）を固定しておき、書き戻しの実装は F3b・F3c と合わせて行う

### 3.7 テスト

- pending・hidden のすれ違い、ブロック関係の相手、他人の投稿が含まれない
- 論理削除済みの自分の投稿が含まれない
- 24 時間に 2 回目の依頼は `429`
- 期限切れの署名付き URL は `403`。アカウント削除後はファイルが残っていない

## 4. F3 Spaces 統合

### 4.1 目的

03 §7 で決めた「Spaces と同じ形で作っておき、後で保存先を差し替える」を、具体的な段階に分ける。

| 目的 | 意味 |
| --- | --- |
| 投稿者の PDS に置く | グループ / Party の投稿が、投稿者自身の PDS（Space ごとの repo）に置かれる。CHAIN が止まっても投稿は投稿者の手元に残る |
| 他のアプリからも同じコミュニティを使える | メンバーは、CHAIN 以外の Spaces 対応アプリ（例: HappyView を使った AppView）からも、同じグループを読み書きできる。CHAIN は authority としてアクセスを判定するだけ |
| authority を選べる（将来） | `spaces.authority` は行ごとに持っているので、CHAIN 互換の別のサーバーを authority にした Space も扱える形にしておく |

### 4.2 alpha の制約（2026-08-20 公開、2026-10 時点）

- 本番には使えない。破壊的な変更が入り、バックアップは無く、alpha 用の PDS は alpha の終了時に消える
- SDK は TypeScript の `@atproto`（`alpha` タグ）だけ。indigo には無い
- 同期はリレーを通さず、各 PDS から直接行う
- bsky.social の対応は告知されていない（README Q4）
- **アクセスを制限するだけで、暗号化はしない**（03 §7.3）

そのため、**F3a はデモと設計の検証だけに使い、主動線のデータは SQLite を正とする**。

### 4.3 CHAIN の位置付け（alpha のデモ、F3a）

```
                        ┌───────────── chain-backend (Go) ─────────────┐
 アプリ ── REST ──────▶ │ AccessChecker（geofence・在席・招待）         │
                        │ InstanceStore = SQLiteStore + SpacesStore     │
                        │   SpacesStore の書き込み: Go から XRPC で直接 │
                        └──────┬──────────────────────▲─────────────────┘
                               │ 内部 HTTP（127.0.0.1） │ 内部 webhook（同期した op）
                        ┌──────▼──────────────────────┴─────────────────┐
                        │ spaces-bridge（Node、@atproto alpha）          │
                        │  ・authority 側: 資格（credential）の発行       │
                        │  ・読み取り側: listRepos / listRepoOps で同期   │
                        └──────┬────────────────────────────────────────┘
                               ▼
                 メンバーの PDS（pds-spaces-alpha）
```

| 部分 | 担当 | 理由 |
| --- | --- | --- |
| アクセス判定 | Go の `AccessChecker`（10 §5、変えない） | geofence・在席・招待の判定は CHAIN の中心。Spaces の `managing-app` 方針の `checkUserAccess` にそのまま当たる |
| 資格の発行 | spaces-bridge。発行の前に Go の内部エンドポイント `POST /internal/spaces/check` を呼ぶ | HTTP Message Signature や鍵の扱いは TypeScript の SDK に任せる |
| 書き込み | Go が、ユーザーの OAuth セッション（DPoP）で PDS の XRPC を直接呼ぶ | PDS のトークンは Go だけが持つ（03 §1）。sidecar に渡さない |
| 同期 | spaces-bridge が op を受け、Go の内部 webhook `POST /internal/spaces/ops` に渡す。Go は `records` に索引する | `records` を AppView の索引として使い続ける |

- 内部エンドポイントは 127.0.0.1 だけで待ち受け、共有の秘密（`CHAIN_INTERNAL_TOKEN`）を確かめる
- 機能の切り替え: `CHAIN_SPACES_ALPHA=1` のときだけ spaces-bridge を使う。`0` ならいまと同じ（SQLite だけ）
- XRPC のメソッド名、scope の書き方、DID ドキュメントの鍵とサービスの ID は alpha で変わりうる。**実装の着手時に proposal（`bluesky-social/proposals/0016-permissioned-data`）と SDK のソースで確かめ、この節を直す**

### 4.4 geofence と資格の期限

Spaces の資格は短命（既定 10 分）で、資格を出し直すたびに authority がアクセスを判定する。
CHAIN のグループの read 条件（09 §4: geofence の中にいる、鮮度 10 分）と同じ長さなので、**エリアを出たら 10 分以内に他のアプリからも読めなくなる**。場所に結び付いたコミュニティを、CHAIN の外でもそのまま守れる。

- 資格の期限は `PresenceGrace` を超えないようにする（設定値で揃える）
- `persistent = 1` のグループは位置に関係なく出す

### 4.5 書き込みの経路

| 投稿者の PDS | 経路 | `records.storage` |
| --- | --- | --- |
| Spaces 対応で、`space:` scope を認可済み | 投稿者の PDS の Space の repo に書く。書いた直後に `records` に入れる（03 §4 と同じ read-after-write）。同期で同じ URI・CID が来たら何もしない | `pds` |
| それ以外 | いまどおり SQLite に書く | `local` |

- 1 つの Space の中で `pds` と `local` が混ざってよい。一覧は `records` から返すので、クライアントからは区別しない
- Spaces 対応の判定: ログインのたびに、ユーザーの PDS が Spaces の XRPC を持つかを確かめ、`users.spaces_capable` に入れる（確かめ方は実装時に決める）
- `space:` scope を足すと既存ユーザーは再ログインが要る（17 §2.4 と同じ `atproto_reauth_required`）。scope は `CHAIN_SPACES_ALPHA=1` のときだけ要求する

### 4.6 CHAIN を通らない書き込みと moderation

Spaces では、メンバーは CHAIN 以外のアプリからも Space に書ける。**CHAIN は書く前の判定（11 §3）ができない**。

- 同期で届いた、CHAIN を通っていない record は、Jetstream で取り込んだ公開投稿（11 §2）と同じく、索引した後に非同期で判定する。判定が出るまでは `mod_state = pending` で、**一覧に出さない**（公開投稿と違い、Space の方針が `reject` のカテゴリを含むことがあるため）
- 下限か Space の `reject` に当たったら索引で非表示にする。同じ投稿者が 24 時間に 3 回当たったら、authority としてその人の `can_write` を外し、owner に通知する
- 他のアプリにも同じ扱いをしてもらうため、§2.9 の `modPolicy` record を Space に置く。従うかどうかはそのアプリ次第であることを、方針の画面に書く

### 4.7 個人用 Space（F3c、P3）

いまサーバーにしか無いフレンドなどを、利用者の手元に移すための Space。alpha の告知でも、単一メンバーの Space（設定や下書き）が用途に挙がっている。

| 項目 | 内容 |
| --- | --- |
| type | `jp.i32.chain.space.personal`。メンバーは本人だけ |
| record | `jp.i32.chain.graph.friend`（`subject`: 相手の DID、`createdAt`、`via`）、`jp.i32.chain.graph.encounterSummary`（相手の DID、成立日、回数。revealed のみ） |
| 正 | CHAIN の SQLite を正とし、個人用 Space には写しを書く。利用者が CHAIN を離れても、自分の関係の記録を持っていける |
| 書かないもの | 位置、pending・hidden のすれ違い、beacon token |

フレンドは双方の合意で成り立つが、写しはそれぞれの個人用 Space に自分の側だけを書く。公開の repo には書かない（「検索はなし」（draft）とフレンド一覧を他人に見せないことを守るため）。

### 4.8 データモデルの変更

| 表 | 列 | 内容 |
| --- | --- | --- |
| `records` | `storage TEXT NOT NULL DEFAULT 'local' CHECK (storage IN ('local','pds'))`、`cid TEXT`、`rev TEXT` | §4.5 |
| `users` | `spaces_capable INTEGER NOT NULL DEFAULT 0` | §4.5 |
| `spaces_sync_cursor` | `space_id`、`repo_did`、`rev` | 同期の続きから再開するため |

### 4.9 段階

| 段階 | 内容 | 終わりの条件 |
| --- | --- | --- |
| F3a-1 | did:web のドキュメントに Spaces の鍵とサービスを出す。spaces-bridge で authority 側（資格の発行）を動かす | alpha の PDS のテスト用アカウントが、CHAIN のグループの資格を得られる |
| F3a-2 | Go から alpha の PDS に書く（`storage = pds`） | CHAIN で投稿した内容が、そのアカウントの Space の repo に入る |
| F3a-3 | 同期して `records` に索引する。CHAIN を通らない書き込みの判定（§4.6） | 別のクライアント（SDK のサンプル）で書いた投稿が CHAIN に出る |
| F3b | 正式版に合わせて直す。indigo が対応したら spaces-bridge を Go に置き換える | 本番の PDS で動く |
| F3c | 個人用 Space | §4.7 |

### 4.10 テスト

- alpha の PDS を compose で立て、`CHAIN_SPACES_ALPHA=1` の結合テストを別のターゲット（`make test-spaces`）にする。普段の CI では回さない（alpha が変わると壊れるため）
- エリアを出た後、資格の期限を過ぎると読めなくなる
- `local` と `pds` が混ざった Space の一覧が時系列で正しく並ぶ

## 5. F4 Bluesky へつながりを持ち出す

### 5.1 フレンド → Bluesky フォロー（F4a、P1）

- フレンドが成立した画面と、フレンドのプロフィール画面に「Bluesky でもフォローする」ボタンを出す。**押した人の側だけ**フォローする（相手のフォローは相手が決める）
- 書く record: `app.bsky.graph.follow`（`subject`: 相手の DID、`createdAt`）。rkey は TID
- 既にフォローしているかは、公開の AppView の `app.bsky.graph.getRelationships`（`actor` = 自分、`others` = 相手）で確かめ、ボタンを「フォロー中」にする。結果は Redis に 5 分キャッシュする
- フォローを外す操作は CHAIN では提供しない（Bluesky のアプリで行う）。CHAIN が書いたフォローの URI は `friendships` に残さない（Bluesky 側で消されても困らないように、毎回 `getRelationships` を正とする）
- scope に `repo:app.bsky.graph.follow?action=create` を足す（03 §2.1）

| メソッド | パス | 内容 |
| --- | --- | --- |
| GET | `/api/v1/friends/{did}/bsky` | `{"following": true, "followedBy": false}` |
| POST | `/api/v1/friends/{did}/bsky-follow` | フォローを書く。フレンドでなければ `404`。既にフォロー済みなら `200` で何もしない |

### 5.2 Party / グループ → Bluesky リスト・スターターパック（F4b、P2）

いっしょにいた人たちとの関係を、Bluesky のリスト（`app.bsky.graph.list`）やスターターパック（`app.bsky.graph.starterpack`）として残す。
**誰がいっしょにいたかが公開される**ので、メンバー一人ひとりの同意を取る。

#### 5.2.1 流れ

```
1. メンバーが「Bluesky リストを作る」を押す（Party: archived の memory から、グループ: persistent のもの）
     → 提案を作る（name、種類 list | starterpack）
2. 他のメンバー全員に通知（種別 space.bsky_list）。各自が「載せる / 載せない」を選ぶ。既定は「載せない」
3. 48 時間後、または提案者が締め切った時に、提案者の repo に作る
     - app.bsky.graph.list（purpose: app.bsky.graph.defs#curatelist）
     - 「載せる」を選んだ人ごとに app.bsky.graph.listitem
     - starterpack なら app.bsky.graph.starterpack（list を指す）
4. 作った後に「載せない」に変えた人は、提案者のセッションで listitem を消す
```

- 提案者自身も「載せる」扱い。ブロック関係にある 2 人は、どちらかが「載せない」になる（あとから選んだ方を拒否する）
- リストの description の既定は空。**場所と日時は既定で入れない**（Party の時刻や geofence を公開しないため）。提案者が自分で書くのは自由
- 限界: リストは提案者の repo にあるので、提案者が CHAIN を離れた後は、「載せない」への変更を CHAIN からは反映できない。同意の画面にこのことを書く
- scope に `repo:app.bsky.graph.list`、`repo:app.bsky.graph.listitem`、`repo:app.bsky.graph.starterpack` を足す

#### 5.2.2 API と表

| メソッド | パス | 内容 |
| --- | --- | --- |
| POST | `/api/v1/spaces/{id}/bsky-lists` | 提案を作る `{"name": "...", "kind": "list|starterpack"}` |
| GET | `/api/v1/spaces/{id}/bsky-lists` | 提案と、自分の選択 |
| PUT | `/api/v1/bsky-lists/{id}/consent` | `{"include": true}` |
| POST | `/api/v1/bsky-lists/{id}/finalize` | 提案者だけ。締め切って作る |

```sql
CREATE TABLE bsky_list_proposals (
  id            TEXT PRIMARY KEY,
  space_id      TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  proposer_did  TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  kind          TEXT NOT NULL CHECK (kind IN ('list','starterpack')),
  name          TEXT NOT NULL,
  status        TEXT NOT NULL CHECK (status IN ('collecting','created','failed','canceled')),
  list_uri      TEXT,
  starterpack_uri TEXT,
  created_at    INTEGER NOT NULL,
  closes_at     INTEGER NOT NULL
);
CREATE TABLE bsky_list_consents (
  proposal_id   TEXT NOT NULL REFERENCES bsky_list_proposals(id) ON DELETE CASCADE,
  did           TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  include       INTEGER NOT NULL,
  listitem_uri  TEXT,
  decided_at    INTEGER NOT NULL,
  PRIMARY KEY (proposal_id, did)
) WITHOUT ROWID;
```

name は moderation（11）の対象。

### 5.3 すれ違いフィード（F4c、P1）

Bluesky のアプリの中で「自分がすれ違った人・フレンド・いまの Party の人」の投稿を読めるカスタムフィード。
CHAIN の外（Bluesky のアプリ）で、CHAIN の現実のつながりが役に立つことを示す。

#### 5.3.1 公開するもの

| 項目 | 値 |
| --- | --- |
| フィードの record | `app.bsky.feed.generator`、CHAIN アカウント（1.2）の repo、rkey `nearby`。`did` = `did:web:chain.i32.jp`、displayName「CHAIN: 近くの人」 |
| サービス | `did:web:chain.i32.jp` の DID ドキュメントに `#bsky_fg`（`BskyFeedGenerator`、`https://chain.i32.jp`）を足す |
| エンドポイント | `GET /xrpc/app.bsky.feed.getFeedSkeleton`、`GET /xrpc/app.bsky.feed.describeFeedGenerator` |
| 認証 | Bluesky の AppView が付けるサービス認証の JWT。indigo `atproto/auth.ServiceAuthValidator` で検証する（`aud` = `did:web:chain.i32.jp`、`lxm` = `app.bsky.feed.getFeedSkeleton`）。`iss` が閲覧者の DID |

#### 5.3.2 中身

閲覧者 V について、次の人たち（以下「対象」）が書いた `app.bsky.feed.post` を新しい順に返す。

| 対象 | 条件 |
| --- | --- |
| フレンド | `friendships` |
| すれ違った人 | 直近 30 日に `revealed` になった encounter の相手 |
| いまの Party | V が未離脱で、confirmed の Party のメンバー |

- 対象それぞれの設定 `bsky_feed_visibility` で絞る

| 値 | 意味 |
| --- | --- |
| `off` | どのすれ違いフィードにも出ない |
| `friends`（既定） | フレンドのフィードにだけ出る |
| `all` | すれ違った人・Party のメンバーのフィードにも出る |

- 既定を `friends` にするのは、すれ違っただけの相手の Bluesky での投稿が、相手の知らないうちに届き続けるのを避けるため。CHAIN の中で既に分かっている関係しか使わないので、V が新しく知ることは無いが、相手の期待に合わせて控えめにする
- CHAIN でブロックしている（どちらの向きでも）相手は出さない。Bluesky 側のブロックとミュートは、AppView が骨組み（skeleton）を埋める時に効く
- V が CHAIN の利用者でない、または JWT が無い場合は、空のフィードを返す（`describeFeedGenerator` と record の description に「CHAIN の利用者向け」と書く）
- 返信は、対象どうしのものだけを出す（知らない人への返信が混ざらないように）。リポストは出さない
- 返すのは URI だけ（skeleton）。本文の取得とモデレーションは AppView が行う

#### 5.3.3 索引

```sql
CREATE TABLE bsky_posts (                       -- 対象になりうる CHAIN 利用者の app.bsky.feed.post
  uri         TEXT PRIMARY KEY,
  author_did  TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  reply_parent_did TEXT,                         -- 返信先の投稿者（返信でなければ NULL）
  created_at  INTEGER NOT NULL,
  indexed_at  INTEGER NOT NULL
);
CREATE INDEX bsky_posts_author ON bsky_posts(author_did, created_at);
```

- 7 日より古いものは `cleanup.Janitor` が消す
- 取得: 対象の DID の集合を作り（Redis `bskyfeed:aud:{did}` に 5 分キャッシュ）、`author_did IN (...)` で新しい順に引く。カーソルは `(created_at, uri)`（17 §3.3 と同じ形）
- `bsky_feed_visibility = off` の人と削除済みの人の投稿は入れない

#### 5.3.4 Jetstream

- いまの接続（03 §5）とは別に、`app.bsky.feed.post` だけを受ける 2 本目の接続を張る。全体の `app.bsky.feed.post` は量が多いので、**DID で絞る**（Jetstream の `wantedDids`。上限と、接続したまま絞り込みを変える方法（`options_update`）は実装時に確かめる）
- 対象の DID の集合が変わったら（登録、`bsky_feed_visibility` の変更、削除）絞り込みを更新する。更新の間に取りこぼした投稿は、カーソルを数秒戻して再接続することで拾う
- 初回: `bsky_feed_visibility` を `off` 以外にした時に、`listRecords` で直近 7 日分を読む（17 §2.5 の初回の読み込みと同じ方法）

#### 5.3.5 設定と API

| メソッド | パス | 内容 |
| --- | --- | --- |
| PATCH | `/api/v1/me/settings` | `bskyFeedVisibility` を追加 |
| GET | `/api/v1/bsky/feed-link` | Bluesky でフィードを開く URL（`https://bsky.app/profile/{CHAIN アカウント}/feed/nearby`）。ホーム画面から案内する |

`user_settings.bsky_feed_visibility TEXT NOT NULL DEFAULT 'friends' CHECK (bsky_feed_visibility IN ('off','friends','all'))`

#### 5.3.6 テスト

- JWT の `aud` や `lxm` が違う、期限切れ、署名が違う → 拒否する（indigo の検証に任せた上で、組み込みの確認だけ）
- `friends` の人は、すれ違っただけの相手のフィードに出ない。`off` の人はどこにも出ない
- ブロック関係の相手は出ない
- 対象外の人への返信は出ない

## 6. F5 lexicon の公開

### 6.1 リポジトリに置く

`backend/lexicons/` に、03 §6・10 §3・本書で決めたすべての NSID の lexicon JSON を置く（いまは README しかない）。

| ファイル | 種別 | 公開 |
| --- | --- | --- |
| `jp.i32.chain.feed.post.json` | record | P1 で公開 |
| `jp.i32.chain.actor.profile.json` | record | P1 で公開 |
| `jp.i32.chain.instance.post.json` | record | Spaces 正式版まで公開しない |
| `jp.i32.chain.instance.media.json` | record | 同上 |
| `jp.i32.chain.instance.pin.json` | record | 同上 |
| `jp.i32.chain.instance.modPolicy.json` | record | 同上 |
| `jp.i32.chain.graph.friend.json`、`jp.i32.chain.graph.encounterSummary.json` | record | F3c と合わせる |
| `jp.i32.chain.space.group.json`、`jp.i32.chain.space.party.json`、`jp.i32.chain.space.personal.json` | space type | Spaces の正式版で space type の書き方が決まってから |

- 書き込みと Jetstream の取り込みの検証は、この JSON を indigo の lexicon 検証に読ませて行う（03 §6）。Go の構造体と JSON がずれないように、テストで JSON から作った検証器に既存のテスト用の値を通す

### 6.2 外部に公開する

- 公開した lexicon は後方互換の無い変更ができない（03 §6）。P1 で公開するのは、形が固まっている `feed.post` と `actor.profile` だけ
- CHAIN アカウント（1.2）の repo に `com.atproto.lexicon.schema` record（rkey = NSID）を書く
- DNS の TXT: `_lexicon.feed.chain.i32.jp` と `_lexicon.actor.chain.i32.jp` に `did=did:plc:...`（CHAIN アカウント）
- 書き込みは運用用のコマンド `cmd/lexpublish` で行う（backend の常駐プロセスからは書かない）
- README（リポジトリ直下）に「CHAIN のデータを読む / 書くには」を書き、lexicon と Jetstream の購読例を載せる

## 7. F6 データの置き場所と PDS の選択

### 7.1 データの置き場所の画面

設定 → 「データの置き場所」。種類ごとに、どこにあり、誰が見られ、いつ消え、どう持ち出せるかを表にして見せる。

| データ | 置き場所 | 見られる人 | 保存期間 | 持ち出し |
| --- | --- | --- | --- | --- |
| 公開投稿・プロフィール | あなたの PDS（`{pds_url}`） | 誰でも | 消すまで | PDS のエクスポート |
| グループ / Party の投稿・写真 | CHAIN のサーバー（Spaces 統合後は、対応する PDS ならあなたの PDS） | その Space のメンバーと、CHAIN の運営者 | archive 後も残る（13 §2） | F2 |
| すれ違い・フレンド | CHAIN のサーバー | 本人と相手（公開設定による）、運営者 | 13 §5 | F2 |
| 位置 | CHAIN のサーバー（一時的なもの。Redis） | 位置共有の相手、運営者 | 数分〜数時間で自動で消える（04） | 持ち出せない（保存していない） |
| 投稿の本文（判定のため） | 外部（NanoGPT） | — | 外部の保存の扱い（L4） | — |
| 予定のタイトル | CHAIN のサーバー | 運営者 | 今日と明日の分だけ | F2 |

- backend は `GET /api/v1/me/data-map` で、この表の定義（静的）と、自分の件数、自分の PDS の URL、認可した scope の一覧を返す。scope は OAuth セッション（`atproto_oauth_sessions`）に保存されているものを読む
- 「CHAIN へのアクセスを取り消す」の案内: PDS 側で取り消す方法（Bluesky のアプリの設定）へのリンクと、CHAIN のログアウト
- 文言は 03 §7.3（Spaces は暗号化しない）・02 §8 とずらさない

### 7.2 PDS の選択

- ログイン画面に「ほかの PDS を使う」を出し、handle のほかに PDS（または認可サーバー）の URL を入れられるようにする。いまは handle を省略すると bsky.social に送る（03 §2）
- indigo の `ClientApp.StartAuthFlow` が URL の入力を受け付けるかは実装時に確かめる。受け付けない場合は、URL から `/.well-known/oauth-protected-resource` を引いて認可サーバーを解決する処理を backend に足す
- ログイン後のプロフィール画面に、自分の PDS のホスト名を出す（「あなたのデータは {host} にあります」）

## 8. 審査での見せ方（参考）

| 見せる順 | 内容 | 使う機能 |
| --- | --- | --- |
| 1 | 好きな PDS でログインし、自分のデータの置き場所を確かめる | F6 |
| 2 | 教室のグループを作り、「厳しい」の方針を選ぶ。Party ではいちばん厳しい人に自動で合わせる | F1a |
| 3 | すれ違った人とフレンドになり、Bluesky でもフォローする。Bluesky のアプリで「CHAIN: 近くの人」フィードを開く | F4a、F4c |
| 4 | 全部をエクスポートして持ち出せる | F2 |
| 5 | Spaces alpha の PDS のアカウントで投稿し、自分の PDS の Space の repo に入っていること、エリアを出ると他のアプリからも読めなくなることを見せる。正式版での移行の道筋を説明する | F3a |

## 9. 既存仕様書の変更点

| 仕様書 | 変更 |
| --- | --- |
| README | 文書一覧に 19 を足す（済）。優先度の表に F1〜F6 を足す。未決事項に Q11〜Q14（§10）を足す |
| 02 | エラーコード `429 export_rate_limited` を足す。§8 の同意の文言に、カテゴリ別判定（§2.7）を反映する |
| 03 | §2.1 の scope に `repo:app.bsky.graph.follow?action=create`（F4a）、`repo:app.bsky.graph.list` / `listitem` / `starterpack`（F4b）、`space:`（F3a、設定で切り替え）を足す。§5 に 2 本目の Jetstream（§5.3.4）を足す。§6 の lexicon の表に `instance.modPolicy`、`graph.*`、`space.personal` を足す。§7.2 を §4 の段階に合わせる |
| 04 | §2.13・§3.4・§4.8・§5.2.2・§5.3.3 の表と列。Redis のキー `lbl:*`、`bskyfeed:aud:*` |
| 09 | §2 の作成の要求に `moderation`（プリセット）を足す。§5 の nearby の詳細に方針の要約を足す |
| 10 | §4.1 の投稿の作成で Space の `reject` を判定する。§4.2 の一覧の `moderation` の形（§2.10） |
| 11 | §1 の 3 段階（ok / warn / block）を、下限＋カテゴリ＋扱いに置き換える。§4 の表示の指示、§5.2・§5.3 の質問としきい値を §2.7 に合わせる |
| 12 | 通知の種別 `space.policy`、`space.bsky_list`、`export.ready` |
| 13 | §5 のアカウント削除で、エクスポートのファイルと `bsky_posts` を消す。削除の確認画面にエクスポートの導線 |
| 14 | §2.10・§3.4・§5.1・§5.2.2・§5.3.5・§7.1 のエンドポイント。`/xrpc/app.bsky.feed.*`、`/xrpc/com.atproto.label.*`、`/internal/spaces/*` |
| 17 | §3 のフィードの `moderation` の形を §2.10 に合わせる。外部ラベラーの適用（§2.11）を `FeedService` に足す |
| 01 | §2 のワーカーに `export.Builder`、2 本目の `jetstream.Consumer`（`app.bsky.feed.post`）、spaces-bridge（任意の別プロセス）を足す |

## 10. 未決事項

| # | 論点 | 仮置き | 決める方法 |
| --- | --- | --- | --- |
| Q11 | カテゴリ別の判定（§2.7）の日本語での精度と、質問数による費用 | しきい値 0.6、`spoiler` は実験扱い | Q10 の評価用データにカテゴリの正解を足して実測する。L7 の見積もりに含める |
| Q12 | Jetstream の `wantedDids` の上限と、接続中の絞り込みの変更 | DID で絞った 2 本目の接続 | 実装の着手時に Jetstream のドキュメントとソースで確かめる。上限を超えたら、`app.bsky.feed.post` 全体を受けて `users` で捨てる方式に切り替える |
| Q13 | Spaces alpha の XRPC 名、scope の書き方、DID ドキュメントの鍵の ID、PDS が Spaces に対応しているかの判定方法 | proposal 0016 に合わせる | F3a の着手時に proposal と SDK で確かめ、§4.3・§4.5 を直す |
| Q14 | すれ違いフィードの既定（`friends`）が厳しすぎないか | `friends` | 試用者に聞いて判断する |
