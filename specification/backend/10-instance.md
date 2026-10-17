# 10 instance（グループ / Party 内の投稿）

## 1. 範囲

- instance とは、グループと Party に共通する「Space の中の投稿の場」のこと
- 投稿・写真・Pin は、すべて Space 内の record（Spaces 同形、03 §7）として `records` に保存する
- 配信は WebSocket（§7）、取得と作成は REST

## 2. record の識別子

```
at://{authority}/space/{spaceType}/{skey}/{authorDid}/{collection}/{rkey}

例: at://did:web:chain.i32.jp/space/jp.i32.chain.space.group/0192f0.../did:plc:abc.../jp.i32.chain.instance.post/3l7x2abcd2s2a
```

- `rkey` は TID（indigo `atproto/syntax` の TID 生成。clock id はプロセス起動時に乱数で決める）
- API の応答では `uri` を正とし、パスには `{rkey}` と、投稿者以外が操作する場合の `{authorDid}` を使う

## 3. lexicon（P0）

### `jp.i32.chain.instance.post`

```json
{
  "text": "string, maxGraphemes 2000, maxLength 20000",
  "format": "plain | markdown",
  "reply": { "root": "at-uri", "parent": "at-uri" },
  "media": [ { "id": "media id", "alt": "string, maxGraphemes 300" } ],
  "createdAt": "datetime"
}
```

- `media` は最大 4 件。Spaces 統合時に blob 参照へ変換する（Spaces の blob の扱いは alpha で未確認。README Q4）
- `reply` は P1。同じ Space 内の投稿だけを指せる
- `format=markdown`（P1）: CommonMark ＋ GFM の表・取り消し線・自動リンク ＋ 数式（`$...$`、`$$...$$`）。**生の HTML は不可**。backend はレンダリングせず原文を保存し、クライアントが HTML を無効にして描画する（KaTeX / marked、research §6）

### `jp.i32.chain.instance.media`（Party の自動共有写真）

```json
{ "media": { "id": "media id", "alt": "" }, "takenAt": "datetime", "createdAt": "datetime" }
```

### `jp.i32.chain.instance.pin`（authority が書く、P1）

```json
{ "subject": "at-uri（post）", "reason": "auto | manual", "pinnedBy": "did（manual のとき）", "createdAt": "datetime" }
```

## 4. 投稿とメディア

### 4.1 投稿（P0）

`POST /api/v1/posts`

```json
{
  "spaceIds": ["0192f0..."],
  "text": "明日の集合は 9:00 正門",
  "format": "plain",
  "media": [ { "id": "0192aa...", "alt": "" } ],
  "alsoPublic": false
}
```

- draft「投稿は instance を限定（選択式）」への対応。`spaceIds` は 1〜5 件で、Space ごとに別々の record を作る（Space をまたぐ record は作らない。Spaces の読み取り権限は Space 単位で全か無かのため）
- 投稿者が自動判定に同意していなければ `403 consent_required`（02 §8）
- 各 Space に対して `write` のアクセス判定を行う。1 つでも通らなければ全体を拒否する
- moderation（11）は 1 回だけ実行し、結果をすべての record に付ける
- `media` は投稿者自身がアップロードした未添付のものに限る。`media` を付けられるのは `spaceIds` が 1 件のときだけ（`media.space_id` は 1 つの Space にしか属さないため。複数の Space に画像を出したい場合は、Space ごとにアップロードする）
- `alsoPublic: true`（P1）なら、テキストだけを `jp.i32.chain.feed.post` としてユーザーの PDS にも書く（03 §4）。PDS への書き込みに失敗しても instance への投稿は成功させ、応答の `public.error` で知らせる
- 成功すると `201 { "items": [ { "uri": "...", "spaceId": "..." } ] }`。各 Space のチャンネルに `post.created` を送る

### 4.2 取得（P0）

`GET /api/v1/instances/{spaceId}/posts?cursor=&limit=`

- `read` のアクセス判定。新しい順。`jp.i32.chain.instance.post` と `jp.i32.chain.instance.media` を時系列で混ぜて返す
- 各要素に投稿者のプロフィール要約、メディアの署名付き URL（§4.4）、moderation の表示指示（11 §4）を付ける
- `mod_label = block` と論理削除済みのものは返さない（投稿者本人には `"hidden": "moderation"` として返す）
- ブロック関係（どちらの向きでも）にある相手の投稿は返さない

### 4.3 削除

- `DELETE /api/v1/instances/{spaceId}/posts/{rkey}`: 投稿者本人
- `DELETE /api/v1/instances/{spaceId}/posts/{authorDid}/{rkey}`: グループの owner（P1）
- 論理削除し、30 日後に本文を消す（04 §4）。`post.deleted` を配信する

### 4.4 メディア（P0）

`POST /api/v1/media`（multipart、`file` と任意の `takenAt`）

| 項目 | 規則 |
| --- | --- |
| 受け付ける形式 | JPEG / PNG / WebP。それ以外は `400` |
| サイズ | 15 MB まで。超えたら `413` |
| 変換 | デコードして長辺 2048 px に縮小し、JPEG（品質 85）で再エンコードする。**EXIF（位置情報を含む）はこれで落ちる** |
| サムネイル | 長辺 400 px の JPEG を同時に作る |
| 保存 | `{MEDIA_DIR}/{yyyy}/{mm}/{id}.jpg` と `{id}_t.jpg`。`sha256` は変換後のファイルで計算する |
| 応答 | `201 { "id": "...", "width": 2048, "height": 1536 }` |

- アップロード直後は未添付（`space_id = NULL`）。24 時間添付されなければ消す
- `POST /api/v1/instances/{spaceId}/media {"mediaId": "...", "takenAt": "..."}`: Party の写真共有用。`jp.i32.chain.instance.media` record を作る（`write` の判定あり）
- 配信は `GET /media/{id}?v=full|thumb&exp={unix}&sig={hmac}`。Capacitor の `<img>` はヘッダを付けられないため、署名付き URL にする
  - `sig = base64url(HMAC-SHA256(CHAIN_JWT_KEY, id|v|exp))`、有効期限 1 時間
  - アクセス判定は URL を発行する時点（投稿一覧を返す時）に行う。配信時は署名と期限だけを確認する
  - `Cache-Control: private, max-age=3600`

## 5. アクセス判定

```go
type Mode string

const (
	ModeRead  Mode = "read"
	ModeWrite Mode = "write"
)

type AccessChecker interface {
	// Spaces の managing-app policy における checkUserAccess に相当する。
	Check(ctx context.Context, spaceID string, did syntax.DID, mode Mode) (bool, error)
}
```

| Space | read | write |
| --- | --- | --- |
| グループ（active） | 09 §4 | 09 §4 |
| Party tentative | メンバー | 不可 |
| Party confirmed | メンバー（離脱済みを含む） | 未離脱のメンバー |
| archived（グループ / Party） | メンバー（離脱済みを含む） | 不可（`410 gone`） |
| deleted | 不可（`404`） | 不可（`404`） |

- 判定結果は `acc:{space}:{did}:{mode}` に、許可なら 60 秒、拒否なら 10 秒キャッシュする。メンバー変更・状態変更の時は該当キーを消す
- 位置条件が関わる判定で、位置が古いために拒否する場合は `403 location_stale`、エリア外なら `403 not_in_geofence` を返す

## 6. Pin（P1）

draft「大事な情報は自動で Pin」。

- 投稿作成時の moderation と同じ呼び出し（`PostAssessor.Assess`、11 §5.5）で重要かを判定し、重要なら authority の名義で `jp.i32.chain.instance.pin` を作る。判定が `pending` になった投稿は再判定ワーカーで判定する
- 手動 Pin: `POST /api/v1/instances/{spaceId}/pins {"subject": "at-uri"}`。手動の解除: `DELETE /api/v1/instances/{spaceId}/pins/{rkey}`。どちらも `write` 権限のあるメンバーなら誰でもできる
- 1 instance につき有効な Pin は 10 件まで。超えたら最も古い auto の Pin から外す
- `GET /api/v1/instances/{spaceId}/pins`
- Pin の変更は `pin.created` / `pin.deleted` として配信する

## 7. WebSocket（P0）

`GET /api/v1/ws`（Upgrade）

### 7.1 接続と認証

- トークンを URL に載せない（アクセスログに残るため）。接続後 5 秒以内にクライアントが `auth` を送る。届かなければ切断する

```json
→ { "type": "auth", "token": "<access JWT>" }
← { "type": "ready", "did": "did:plc:..." }
```

- access JWT が切れる前に、クライアントは新しいトークンで `auth` を再送する。期限が切れたまま 60 秒経った接続は閉じる
- サーバは 30 秒ごとに ping を送り、10 秒以内に pong がなければ閉じる

### 7.2 購読

```json
→ { "type": "sub",   "id": "c1", "space": "0192f0..." }
← { "type": "subscribed", "id": "c1", "space": "0192f0..." }
← { "type": "error", "id": "c1", "code": "forbidden" }
→ { "type": "unsub", "space": "0192f0..." }
```

- 購読時に `read` の判定を行う。購読中も 10 分ごとに再判定し、権限を失ったら `{ "type": "unsubscribed", "space": "...", "reason": "access_lost" }` を送る（Spaces の credential の寿命に合わせる）
- 1 接続あたりの購読は 50 Space まで
- ユーザー宛てのイベント（下表の「ユーザー」）は購読なしで届く

### 7.3 イベント

すべて `{ "type": "event", "event": "<name>", "space": "<id または省略>", "data": { ... } }` の形で送る。

| event | 宛先 | data |
| --- | --- | --- |
| `post.created` | Space | 投稿 1 件（§4.2 と同じ形） |
| `post.deleted` | Space | `uri` |
| `media.created` | Space | メディア record 1 件 |
| `pin.created` / `pin.deleted` | Space | Pin 1 件 / `uri` |
| `member.joined` / `member.left` | Space | `did` |
| `space.archived` | Space | — |
| `party.updated` | Space ＋ メンバーのユーザー | `phase`, `locked`, 在席メンバー |
| `encounter.revealed` | ユーザー | encounter 1 件（06 §6 と同じ形） |
| `friend.request` / `friend.accepted` / `friend.changed` | ユーザー | 相手の要約 |
| `notification` | ユーザー | 通知 1 件（12） |

- 配信は at-most-once。取りこぼしは、再接続後に REST で取り直して補う（一覧 API はすべて新しい順のカーソルを持つ）

### 7.4 実装

- `realtime.Hub` をプロセス内に持つ。Space チャンネルとユーザーチャンネルを、接続の集合に対応付ける
- Hub への publish は `realtime.Broker` interface 越しに行い、P0 の実装はプロセス内の配送だけにする。複数プロセスにする時は Redis Pub/Sub の実装に差し替える（P2）
- 送信が詰まった接続（送信キュー 256 件超）は閉じる
