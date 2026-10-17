# 17 公開投稿とフィード

公開投稿（ユーザーの PDS に書く投稿）と、ホーム・グループ・個別の Space の投稿を同じ形で返す、共通のフィード。
ATProto の書き込みと Jetstream の仕組みそのものは 03 §4・§5。

## 1. 方針

| 論点 | 決定 |
| --- | --- |
| 公開投稿の置き場所 | `jp.i32.chain.feed.post` をユーザーの PDS に書く |
| Bluesky への同時投稿 | **投稿ごとに選べる**。`alsoBluesky: true` なら `app.bsky.feed.post` も書く。既定は false |
| フィード | **取得元（source）を差し替えられる 1 つの API と 1 つの要素の形**にする。クライアントは 1 つの Feed コンポーネントに source を渡すだけで、ホーム・グループ・個別の Space を表示できる。あとから source を足せる |

## 2. 公開投稿（P1）

### 2.1 作成

`POST /api/v1/feed/posts`

```json
{ "text": "今日はいい天気", "langs": ["ja"], "alsoBluesky": true }
```

- テキストのみ（画像は P2。PDS の blob に上げる必要があるため）。1000 grapheme まで
- moderation（11）を PDS に書く**前**に行う。block なら 422 で、どこにも書かない
- 書く順番:
  1. `alsoBluesky` なら、先に `app.bsky.feed.post`（`text`、`langs`、`createdAt`）を書く
  2. `jp.i32.chain.feed.post` を書く。1 で書いた投稿があれば、その `{uri, cid}` を `bsky` に入れる
  3. 2 が失敗したら、1 の投稿を消す（Bluesky にだけ残らないように）
- 書いた直後に `public_posts` に入れる（03 §4 の read-after-write）
- instance への投稿と同時に公開もする場合は、`POST /api/v1/posts` の `alsoPublic: true`（と `alsoBluesky`）を使う。処理は同じ

### 2.2 削除

`DELETE /api/v1/feed/posts/{rkey}`: `jp.i32.chain.feed.post` と、`bsky` で指している `app.bsky.feed.post` の両方を消す。片方が既に消えていても成功にする。

### 2.3 lexicon の変更

`jp.i32.chain.feed.post`（03 §6）に次を足す。

```json
{
  "text": "string, maxGraphemes 1000",
  "langs": ["ja"],
  "bsky": { "uri": "at-uri", "cid": "cid" },
  "createdAt": "datetime"
}
```

`bsky` は任意。Bluesky 側の投稿との対応を残し、削除や将来の取り込みで使う。

### 2.4 scope

- `alsoBluesky` には `repo:app.bsky.feed.post` の書き込み権限が要る。03 §2.1 の既定の scope に `repo:app.bsky.feed.post?action=create&action=delete` を足す（**書き方が bsky.social の認可サーバで通るかは、実装時に S1 と同じ方法で確かめる**）
- scope を足すと、既存のユーザーは再ログインが要る。権限がないときの PDS の応答は `403 atproto_reauth_required`（02 §3 に追加）で返し、クライアントは再ログインを促す

### 2.5 取り込み（Jetstream と初回の読み込み）

- Jetstream で `jp.i32.chain.feed.post` を購読して `public_posts` に入れる（03 §5）。CHAIN 以外のクライアントが書いた record も入る
- **初回の読み込み**: ユーザーが初めてログインしたとき、`com.atproto.repo.listRecords` で `jp.i32.chain.feed.post` を新しい順に 100 件まで読み、`public_posts` に入れる（Jetstream の購読を始める前の投稿を拾うため）
- Bluesky の公式アプリなどで書いた `app.bsky.feed.post` は取り込まない（CHAIN の投稿だけを出す）。取り込むかどうかは P2 で再検討する
- 取り込んだ投稿は非同期で moderation にかける（11 §2）

## 3. フィード

### 3.1 API

`GET /api/v1/feed?source={source}&cursor=&limit=`

| source | 中身 | 優先度 |
| --- | --- | --- |
| `home` | 公開投稿のうち、自分・フレンド・直近 30 日に revealed な encounter がある相手のもの | P1 |
| `groups` | 自分が今 read できるグループ（active）すべての instance の投稿とメディア | P1 |
| `space:{id}` | 1 つの Space（グループ / Party）の instance の投稿とメディア。今の `GET /instances/{id}/posts` と同じ中身 | P1 |
| `user:{did}` | そのユーザーの公開投稿（13 §1.2 の関係がある相手のみ。なければ 404） | P1 |

- `source` は将来カンマ区切りで複数を受け付けられるようにしておく（例 `home,groups`）。P1 では 1 つだけ
- `GET /api/v1/instances/{id}/posts` は互換のため残し、中身は `source=space:{id}` を呼ぶだけにする

### 3.2 要素の形

すべての source で同じ形を返す。

```json
{
  "items": [
    {
      "kind": "public_post",
      "uri": "at://did:plc:.../jp.i32.chain.feed.post/3m...",
      "author": { "did": "did:plc:...", "handle": "bob.bsky.social", "displayName": "Bob", "avatarUrl": "..." },
      "text": "今日はいい天気",
      "format": "plain",
      "media": [],
      "reply": null,
      "space": null,
      "bsky": { "uri": "at://..." },
      "moderation": { "label": "ok", "display": "show" },
      "createdAt": "2026-10-17T03:12:45.123Z"
    },
    {
      "kind": "instance_post",
      "uri": "at://did:web:chain.i32.jp/space/jp.i32.chain.space.group/0192.../did:plc:.../jp.i32.chain.instance.post/3m...",
      "space": { "id": "0192...", "type": "group", "name": "情報科学演習 A" },
      "...": "..."
    }
  ],
  "cursor": "..."
}
```

| kind | 元 |
| --- | --- |
| `public_post` | `public_posts` |
| `instance_post` | `records`（`jp.i32.chain.instance.post`） |
| `instance_media` | `records`（`jp.i32.chain.instance.media`） |

`space` は instance の要素のときだけ入る（`groups` のように複数の Space を混ぜて出すとき、どこの投稿かを表示するため）。

### 3.3 実装

```go
// FeedSource は 1 種類の取得元。before より古いものを、新しい順に最大 limit 件返す。
type FeedSource interface {
	Fetch(ctx context.Context, viewer string, before FeedCursor, limit int) ([]FeedItem, error)
}
```

- `FeedService` が source の名前から `FeedSource` を組み立て、複数のときは `createdAt`（同じなら `uri`）の新しい順にマージする
- カーソルは `(createdAt, uri)`。すべての source で同じ形にして、マージしても続きを取れるようにする
- ブロック関係の相手の除外、moderation の表示指示（11 §4）、メディアの署名付き URL は、source ではなく `FeedService` で一律にかける
- `groups` は、自分がメンバーの active なグループのうち `AccessChecker` で read が通るものだけを対象にする（判定はキャッシュされる。10 §5）
- source を足すときは `FeedSource` を 1 つ実装して登録するだけにする

### 3.4 クライアント

- Feed コンポーネントは 1 つで、`source` を受け取って `GET /feed` を呼ぶ
- ホーム画面は `home`、グループ一覧の画面は `groups`、個別のグループ / Party の画面は `space:{id}`、プロフィール画面は `user:{did}`
- WebSocket の `post.created`（10 §7.3）は `space:{id}` の画面が使う
- `home` と `groups` の画面は、ユーザー宛ての `feed.updated`（10 §7.3）を受けて取り直す。配る先は次のとおり
  - `groups`: グループ（Party は含めない）の instance に投稿が入ったとき（moderation が ok のときだけ）、投稿者以外の未離脱メンバー
  - `home`: 公開投稿を `public_posts` に入れたとき（作成と Jetstream の取り込みで、内容が変わったときだけ）、投稿者と、`homeAuthors` に投稿者を含むユーザー（自分・フレンド・直近 30 日に revealed な encounter の相手。関係は対称なので、投稿者の `homeAuthors` と同じ集合）
