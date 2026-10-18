# Openness 実装メモ（19 の補足）

`specification/backend/19-openness.md`（以下 19）に対して、いまのコードがどこまで来ているか、Bluesky / ATProto との互換性をどこまで約束するか、どの順で実装するかをまとめる。
ここに書くのは決まったことだけ。仕様の本体（API・表・テスト）は 19 にあり、ここでは繰り返さない。

## 1. いまの実装

### 1.1 済んでいるもの（19 より前の仕様の分）

| 内容 | 実装 | 仕様 |
| --- | --- | --- |
| ATProto の OAuth（confidential client、トークンは backend だけが持つ） | `auth`、`external/atproto` | 03 §2 |
| 公開投稿を PDS に書く（`jp.i32.chain.feed.post`） | `service/content/public_post.go` | 17 §2.1 |
| `alsoBluesky: true` で `app.bsky.feed.post` も書く。`feed.post` の `bsky` で対応を残す。片方が失敗したら消す | 同上 | 17 §2.1 |
| `app.bsky.actor.profile` の読み込みと、CHAIN からの編集 | `external/atproto/profile.go`、`service/me/me.go` | 13 §1 |
| Jetstream で `jp.i32.chain.*` と `app.bsky.actor.profile` を受ける | `external/atproto/jetstream.go`、`service/content/jetstream.go` | 03 §5 |
| 初回のログインで `listRecords` から公開投稿を読む | `external/atproto/firstload.go` | 17 §2.5 |
| 投稿内容の判定への同意。同意が無いと Space に投稿できない（`403 consent_required`） | `service/content/instance.go`、`service/me/me.go` | 02 §8 |
| 判定の 3 段階（ok / warn / block）と、閲覧者の `warn_display`（show / blur / hide） | `service/content/feed.go`、`model/settings.go` | 11 §1・§4 |

### 1.2 まだのもの

19 の F1〜F6 はどれも未着手。

- `backend/lexicons/` には README しかない。正式な lexicon の JSON（`required`、`knownValues`、`ref` / `union` など）は仕様書にもリポジトリにも無い。定義は 03 §6・10 §3・17 §2.3・19 §6.1 に、項目名と型と上限を文章で書いた略式が分かれて書かれている
- 03 §6 の「indigo の lexicon 検証を使う」も、コードには無い。Jetstream の取り込みは `ParseFeedPost` が必要な項目を読むだけ
- 19 §9 の、他の仕様書（02・03・04・10・11・12・13・14・17・01）への反映もまだ

## 2. 基本方針: フロントエンドを変えずに、中身を Bluesky のデータに寄せる

- フロントエンドが呼ぶ API（`/api/v1/*`）の要求と応答の形は変えない。足すのは項目の追加だけにする
- backend の中身は、Bluesky に同じものがあれば Bluesky のデータ（ユーザーの PDS にある `app.bsky.*`）を正にし、backend はそれを索引する AppView になる
- backend に残すのは、次の 2 つと、Bluesky に同じものが無いものだけ
  - 位置・すれ違い・BLE・Party の検出・geofence（計算が backend でないとできない）
  - グループ / Party の投稿（限定公開。Spaces の正式版までは SQLite）

| データ | 正 | 状態 |
| --- | --- | --- |
| プロフィール | `app.bsky.actor.profile` | 済み |
| フレンド | `app.bsky.graph.follow` の相互フォロー（07 §1） | 仕様を決めた（O5） |
| ブロック | CHAIN のブロック（非公開）＋ `app.bsky.graph.block` の取り込み | 仕様を決めた（O5） |
| 公開投稿 | `jp.i32.chain.feed.post`（＋ `alsoBluesky` で `app.bsky.feed.post`） | いまのまま。`app.bsky.feed.post` だけにするかは D5 |
| ステータス、通知、設定 | CHAIN（Bluesky に同じものが無い） | いまのまま |
| すれ違い、位置、Party、会った裏付け | backend | いまのまま |
| グループ / Party の投稿 | backend の SQLite → Spaces | Spaces の正式版待ち |

### 2.1 フレンド＝相互フォロー

決めたこと（07 §1〜§3・§5、19 §5.1）

- **フレンド**は、CHAIN の利用者どうしの Bluesky の相互フォロー。フレンド一覧はここから作る。Bluesky のアプリで相互フォローになった相手も入る
- **会ったフレンド**は、フレンドのうち、CHAIN で会った裏付け（revealed な encounter、同じ Space、招待コード）がある組。位置共有・beacon・Party の自動作成はこの組だけ。表は今の `friendships` をそのまま使うので、08 の判定処理は変わらない
- 申請はフォローを書くこと、承認はフォローを返すこと、フレンドの削除は自分のフォローを消すこと。拒否は CHAIN の中で受信箱から消すだけ
- `friend.Reconciler` が、フォロー・encounter・Space・招待・ブロックの変化を受けて `friendships` を作り直す
- CHAIN のブロックは非公開のまま（Bluesky には書かない）。Bluesky のブロックは取り込んで同じ効果を持たせる
- 画面で変えるのは文言だけ（「Bluesky でもフォローします（公開されます）」）。`GET /friends` に `met` と `via` を足す

## 3. 互換性をどこまで約束するか

| 層 | 対象 | 約束 |
| --- | --- | --- |
| ① Bluesky と完全に互換 | OAuth・DID・handle・PDS、`app.bsky.feed.post`、`app.bsky.actor.profile`、`app.bsky.graph.follow` / `block`（フレンドとブロック）。今後足す `app.bsky.graph.list` など、フィードジェネレーター、ラベラー、self-label | Bluesky の lexicon をそのまま使うので、もともと互換。CHAIN 側で形を変えない |
| ② ATProto として互換（Bluesky のアプリには出ない） | `jp.i32.chain.feed.post`、`jp.i32.chain.actor.profile`（ユーザーの PDS にある） | **lexicon を外部に公開した時点（F5）から**後方互換を守る。それまでは形を変えてよい |
| ③ Spaces と形だけ合わせる | グループ / Party の URI、メンバーの権限、`AccessChecker`（= `checkUserAccess`）、Space 内のラベルの形（19 §2.8） | 保存先を差し替えやすくしておくだけ。実際の相互運用は Spaces の正式版から。alpha の仕様が変わったら CHAIN 側を合わせる（19 Q13） |
| ④ 約束しない | すれ違い、会った裏付け、位置、beacon、Party の検出、設定、moderation の結果、`/api/v1/*`（XRPC ではない CHAIN 専用の API） | CHAIN の中だけのもの。持ち出しは F2（エクスポート）と F3c（個人用 Space）で別に用意する |

## 4. 危険性の確認で利用者が選べること

19 §2.15 で決めた。線引きは「**判定そのもの（Jev に聞くこと）は止められないが、結果をどう扱うかは選べる**」。

| 何を | 投稿者 | 閲覧者 | Space |
| --- | --- | --- | --- |
| Space 内の投稿を判定にかけるか | 選べない（同意が無ければ投稿できない） | — | 選べない |
| 判定への同意 | 同意する / 撤回する。撤回しても閲覧と公開投稿はできる | — | — |
| 公開投稿を判定にかけるか | 選べない（同意の有無にかかわらず判定する） | — | — |
| 下限のカテゴリ | 選べない | 選べない | 緩められない |
| 選べるカテゴリの扱い | 自分で注意を付けられる（self-label）。判定を緩めることはできない | allow / warn / hide | allow / warn / hide / reject |
| 判定の結果を知る | 投稿の応答で、各 Space での扱いを受け取る | — | — |
| 判定への異議 | 通報と同じ一覧に出す（P2） | — | owner が確認する（P2） |

決めたこと（19 §2.15 の根拠の要約）

- 判定を止める設定（投稿者ごと・Space ごとに Jev を呼ばない設定）は作らない。下限の保証が崩れるため（02 §8）
- 閲覧者が選べるカテゴリをすべて allow にすると、下限以外はそのまま見られる。これが「検閲を切る」に当たる
- 公開投稿は同意が無くても判定する。公開されたデータで、Jetstream からの取り込みでももともと判定しているため。02 §8 の表に書き足す（19 §9）
- 投稿前に「試しに判定する」API は作らない。Jev の呼び出しが 2 回になり、下限に引っかかる言葉を探る使い方もできてしまうため
- self-label で付けられるのは `sexual`、`violence`、`self_harm`、`spoiler` だけ

## 5. 実装の順番

いまのコードを前提に、軽い順に並べた。規模は 1 人で作る場合の目安。

### 5.1 段階 1: 新しい権限も運用の準備も要らない

| 順 | 内容 | 規模 | 19 |
| --- | --- | --- | --- |
| O1 | PDS のホスト名を表示する | 半日 | §7.2 |
| O2 | lexicon の JSON をリポジトリに置き、書き込みと取り込みで検証する（`feed.post`、`actor.profile` だけ） | 1〜2 日 | §6.1 |
| O3 | 公開投稿の self-label | 1〜1.5 日 | §2.15.3 |
| O4 | データの置き場所の画面 | 1.5〜2 日 | §7.1 |

4 つは互いに依存しない。O3 で `feed.post` に `labels` を足すので、O2 の JSON には最初から `labels` を入れておく。

### 5.2 段階 2: 再ログインか、外部の準備が要る

| 順 | 内容 | 規模 | 19 | 重い理由 |
| --- | --- | --- | --- | --- |
| O5 | フレンド＝相互フォロー（§2.1） | backend 3〜4 日、web は文言だけ | §5.1、07 | scope を足すので、既存ユーザー全員が再ログインする。Jetstream と `listRecords` でのフォローの取り込み、`friend.Reconciler`、表の作り直し |
| O6 | lexicon を外部に公開する | コード 0.5 日＋準備 | §6.2 | CHAIN 用のアカウント（did:plc）、DNS の TXT、`cmd/lexpublish`。公開した後は後方互換の無い変更ができない |
| O7 | ほかの PDS でログインする | 1〜3 日 | §7.2 | indigo の `StartAuthFlow` が URL を受け付けるかで規模が変わる |
| O8 | データのエクスポート | 3〜4 日 | §3 | ワーカー、ZIP、署名付き URL、Android のプラグイン |

### 5.3 段階 3: 新しいプロトコルの実装、または外部の仕様待ち

| 順 | 内容 | 規模 | 19 | 重い理由 |
| --- | --- | --- | --- | --- |
| O9 | すれ違いフィード | 4〜5 日 | §5.3 | サービス認証の JWT、DID ドキュメントの `#bsky_fg`、2 本目の Jetstream（Q12）。O6 のアカウントが前提 |
| O10 | Bluesky のリスト / スターターパック | 3〜4 日 | §5.2 | 同意を集める流れ、通知、表、scope |
| O11 | 外部ラベラーの購読 | 3〜4 日 | §2.11 | `queryLabels`、キャッシュ、設定画面。効くのは Bluesky に同時投稿した分だけ |
| O12 | CHAIN をラベラーとして公開 | 1 週間前後 | §2.12 | `subscribeLabels`（CBOR）を自分で実装、PLC の操作 |
| O13 | Spaces との統合（デモ） | 1.5〜3 週間 | §4 | alpha に依存、Node の補助プロセス |

F1a（Space ごとの moderation 方針）と 19 §2.15 の Space 内の部分は、互換性ではなく「選べる・安心」の機能なので、この表とは別に進める。

### 5.4 順番の決めごと

- **O1〜O4 を先に終える**。どれも後戻りが無い
- **scope の追加は 1 回にまとめる**。O5（`app.bsky.graph.follow` の create / delete）と O10（`list`、`listitem`、`starterpack`）の scope を同時に足し、再ログインを 1 回で済ませる。O10 を作る前でも、scope だけは O5 のときに要求する
- **O6 は O2・O3 で `feed.post` の形が固まってから**。O9 も同じ CHAIN 用のアカウントを使うので、アカウントの準備は O6 でまとめて行う

## 6. 段階 1 の作業

### 6.1 O1 PDS のホスト名

- backend: `users.pds_url` は保存済み（`model/user.go`）。`GET /me` の応答に `pdsHost`（URL のホスト名だけ）を足す
- web: プロフィール画面（自分）に「あなたのデータは {host} にあります」を出す。O4 の画面へのリンクにする

### 6.2 O2 lexicon の JSON と検証

- `backend/lexicons/jp.i32.chain.feed.post.json` と `jp.i32.chain.actor.profile.json` を書く。中身は 03 §6・17 §2.3 の略式を正式な形にしたもの

| NSID | 項目 |
| --- | --- |
| `jp.i32.chain.feed.post`（key: tid） | `text`（必須、maxGraphemes 1000、maxLength 10000）、`langs`（任意、言語コードの配列、最大 3）、`bsky`（任意、`com.atproto.repo.strongRef`）、`labels`（任意、`union` で `com.atproto.label.defs#selfLabels`）、`createdAt`（必須、datetime） |
| `jp.i32.chain.actor.profile`（key: literal:self） | `status`（任意、maxGraphemes 40）、`createdAt`（必須、datetime） |

- 検証: indigo の lexicon パッケージで JSON を読み、PDS に書く前（`public_post.go`、`me.go` の status の更新）と Jetstream の取り込み（`service/content/jetstream.go`）で通す。取り込みで通らない record は捨てて warn を出す
- テスト: いまのテスト用の値が JSON から作った検証器を通ること。上限を超えた `text`、`createdAt` の無い record が落ちること
- 外部には公開しない（O6 まで）

### 6.3 O3 公開投稿の self-label

- 要求: `POST /api/v1/feed/posts` と `POST /api/v1/posts`（`alsoPublic` のとき）に `selfLabels`（文字列の配列）。§4 の 4 値以外は `400`
- PDS に書く record: `feed.post` と、`alsoBluesky` のときの `app.bsky.feed.post` の両方に `labels: {"$type": "com.atproto.label.defs#selfLabels", "values": [{"val": "spoiler"}]}`
- 索引: `public_posts.self_labels_json`。Jetstream の取り込みでも `labels` を読む（CHAIN 以外のクライアントが書いたものも入る）
- 表示: いまの `warn_display` の判定（`feed.go` `decorateItem`）で、self-label のある投稿を `warn` と同じに扱う。F1a でカテゴリ別の判定に変わったら、19 §2.15.3 の決め方に移す
- web: 公開投稿の作成画面に「注意を付ける」（4 値の複数選択）

### 6.4 O4 データの置き場所

- backend: `GET /api/v1/me/data-map`。19 §7.1 の表の定義（固定）、自分の件数（公開投稿、Space の投稿、写真、すれ違い、フレンド）、自分の PDS の URL、認可した scope を返す
- scope は `atproto_oauth_sessions` の暗号化した `ClientSessionData` に入っているので、復号して読む。セッションが失効していれば `scopes: null`
- web: 設定 →「データの置き場所」。表と、「CHAIN へのアクセスを取り消す」の案内（Bluesky のアプリの設定へのリンクと、ログアウト）。F2（O8）を作ったら、ここにエクスポートの入口を置く

## 7. フロントエンドへの影響

19 は backend の仕様書なので、画面の決めごとは文中に少しずつしか無い。段階 2 以降に入る前に、frontend 側の仕様（画面の一覧と導線）を別にまとめる。

| 機能 | 画面 |
| --- | --- |
| F1a / 19 §2.6 | グループ作成時のプリセット、owner の方針編集、参加前の方針の要約、ぼかしの理由の表示。設定の `warnDisplay` をカテゴリ別に作り替える（`dto.ts`、`mappers.ts`、`types.ts`、`SettingsScreen.tsx`、e2e 十数本に影響） |
| 19 §2.15 | 投稿画面の self-label、投稿直後の「ぼかして表示されます」と削除・書き直しの導線 |
| F1b | ラベラーの購読と、値ごとの扱いの設定 |
| F2 | エクスポートの依頼、状態の表示、Android でのファイル保存と共有 |
| F4a | 文言だけ（申請・承認・招待コードの作成の近くに「Bluesky でもフォローします（公開されます）」）。位置の設定で、会ったフレンドでない相手の `403` を扱う |
| F4b | 提案の作成、「載せる / 載せない」の同意画面 |
| F4c | `bskyFeedVisibility` の設定、Bluesky のフィードを開く案内 |
| F6 | データの置き場所、ログイン画面の「ほかの PDS を使う」、PDS のホスト名 |
| 共通 | scope を足したときの `atproto_reauth_required` の再ログインの案内 |

backend だけで済むのは F1c、F5、F3a、F4c のフィードの中身。

## 8. 決まっていないこと

| # | 論点 | 仮置き |
| --- | --- | --- |
| D1 | O2 で検証に通らない record を Jetstream で受けたとき、捨てるか、索引して非表示にするか | 捨てて warn を出す |
| D2 | O3 の self-label を、F1a より前（いまの 3 段階の判定）で `warn` と同じに扱ってよいか | 扱う |
| D3 | O5 のときに O10 の scope も先に要求するか（使わない権限を求めることになる） | 要求する |
| D5 | 公開投稿を `app.bsky.feed.post` だけにするか（CHAIN の中だけの公開投稿が無くなり、`alsoBluesky` は意味を失う） | いまのまま（`jp.i32.chain.feed.post` ＋ 任意で `app.bsky.feed.post`） |
| D6 | 会ったフレンドでないフレンドに、フレンド一覧で位置の設定を出すか | 出さない。フロントエンドの文言の変更と合わせて、`met = false` のときは隠す |
| D4 | 19 Q11〜Q14 | 19 §10 のとおり |
