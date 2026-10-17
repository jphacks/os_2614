# 13 profile・memory・統計・ブロック・アカウント削除

## 1. profile

### 1.1 項目と出どころ

| 項目（draft） | 出どころ | 編集 | 優先度 |
| --- | --- | --- | --- |
| id | DID（内部）。表示は ATProto の handle | CHAIN では変えない（README Q5） | P0 |
| 名前 | `app.bsky.actor.profile.displayName` | CHAIN から編集できる（§1.3） | P0（編集は P1） |
| アイコン / バナー | `app.bsky.actor.profile.avatar` / `banner`（blob の CID） | 同上 | P0（編集は P1） |
| 説明 | `app.bsky.actor.profile.description` | 同上 | P0（編集は P1） |
| ステータス | `jp.i32.chain.actor.profile.status`（PDS に公開で保存） | `PUT /api/v1/me/status`。moderation の対象 | P1 |

- 取得: ログイン時に、ユーザーの PDS から `com.atproto.repo.getRecord` で両方の record を読み、`profiles` に入れる。以後は Jetstream で更新する（03 §5）。Jetstream の取りこぼしに備えて、24 時間以上更新されていなければ閲覧時に裏で取り直す
- 画像の URL は Bluesky の CDN の形（`https://cdn.bsky.app/img/avatar/plain/{did}/{cid}@jpeg`）で組み立てる。CDN に載らない PDS（自前 PDS など）のユーザーは、`com.atproto.sync.getBlob` を backend で中継する（P2）
- `PUT /api/v1/me/status` は PDS に `putRecord` してから `profiles` を更新する

### 1.2 誰のプロフィールを返すか

`GET /api/v1/users/{did}` は、呼び出し元と次のどれかの関係がある相手にだけ返す。それ以外は `404`。

- 自分自身
- フレンド、または申請中・申請を受けている相手
- revealed な encounter のある相手
- 同じ Space（active / archived）のメンバーだった相手

ATProto 上のプロフィール自体は公開されている。ここで制限しているのは「CHAIN を使っていて、自分と接点がある」という関係の方である。

### 1.3 CHAIN からの編集（P1）

Bluesky のプロフィール（`app.bsky.actor.profile` の `self`）を、ユーザーの PDS に直接書き換える。Bluesky のアプリから見ても同じプロフィールになる。

| API | 内容 |
| --- | --- |
| `PATCH /api/v1/me/profile` | `{"displayName": "...", "description": "..."}`。来た項目だけ変える。空文字はその項目を消す |
| `PUT /api/v1/me/avatar` | multipart の `file`。アイコンを差し替える |
| `PUT /api/v1/me/banner` | multipart の `file`。バナーを差し替える |
| `DELETE /api/v1/me/avatar`、`/me/banner` | 画像を消す |

- **既存の record を保つ**: 先に PDS から今の `app.bsky.actor.profile/self` を `getRecord` で読み、変える項目だけを差し替えて `putRecord` する（`pronouns`、`website`、`labels` など、CHAIN が知らない項目を消さないため）。`putRecord` には読んだ時の `cid` を `swapRecord` として渡し、その間に Bluesky のアプリなどで変更されていたら 409 `conflict` を返す（クライアントは読み直してやり直す）。record がまだなければ新しく作る
- **上限**（Bluesky の lexicon に合わせる）: displayName は 64 grapheme、description は 256 grapheme
- **moderation**: displayName と description を、書く前に判定する（kind `profile`）。block なら 422
- **画像**: JPEG / PNG / WebP を受け付け、デコードして再エンコードする（EXIF を落とす、10 §4.4 と同じ）。アイコンは 1000×1000 に収まるよう縮小（正方形にはクロップしない。クロップはクライアントの責務）、バナーは 3000×1000 に収める。JPEG で 1,000,000 バイト以下になるまで品質を下げる（85 から 5 ずつ、下限 50。それでも超えたら縮小を続ける）。`com.atproto.repo.uploadBlob` で上げ、返った blob 参照を record に入れて `putRecord`
- 書いた後に `profiles` を更新し、`GET /me` に反映する。Jetstream から同じ変更が届いても、そのまま上書きされるだけ
- **scope**: `repo:app.bsky.actor.profile` の作成・更新と、`blob:image/*` が要る（03 §2.1 の既定の scope に追加）。scope が足りなければ 403 `atproto_reauth_required`（17 §2.4 と同じ）
- PDS のエラーの扱いは 13 §1.1 のステータスと同じ（失効 401、その他 502）

## 2. memory

draft「archive された party / グループはここで見られる。archive された chat は一覧に残すが、開くと memory ページに飛ぶ」。

- `GET /api/v1/instances`: 自分の Space の一覧（チャット一覧に当たる）。active と archived の両方を返し、`state` で区別する。各要素の `canWrite` は、いま投稿できるか（10 §5 の write 判定。位置が古くて判定できないときは `true`）。クライアントの投稿先は `state = active` かつ `canWrite` のものだけにする。並びは最終投稿の新しい順。クライアントは archived を開くと memory の画面に遷移する
- `GET /api/v1/memories?cursor=`（P0 は Party、P1 でグループも）: archived の Space のうち、自分が `hidden_from_memory = 0` のもの

```json
{
  "items": [
    {
      "spaceId": "...", "type": "party", "name": "10/17 13:05 の party",
      "from": "2026-10-17T04:05:00Z", "until": "2026-10-17T09:40:00Z",
      "memberCount": 4, "mediaCount": 37, "cover": { "thumbUrl": "..." }
    }
  ]
}
```

- `cover` は、その Space の最初の `jp.i32.chain.instance.media`
- 中身は instance の API（10 §4.2）で読む。archived は読み取り専用
- `DELETE /api/v1/memories/{spaceId}`: 自分の memory から隠すだけ（`hidden_from_memory = 1`）。他のメンバーの memory には残る。自分の投稿や写真を消したい場合は、個別に削除する
- archived の Space の保持期間は無期限とする。メンバーが全員アカウントを削除したら Space ごと消す

## 3. 統計（P1）

draft「統計データは backend に保存したい」。

`GET /api/v1/me/stats`

```json
{
  "encounters": { "today": 12, "week": 48, "month": 160, "total": 1024, "uniquePeople": 380 },
  "friends": 23,
  "parties": { "total": 15 },
  "groups": { "member": 6 },
  "posts": { "month": 210 }
}
```

- `encounters` は 06 §6 の summary と同じ値と規則
- その場で集計し、`stats:{did}` に 5 分キャッシュする。重くなったら日次の集計表（`user_daily_stats`）を足す
- 活発度（メッセージ数の推移）は P2
- 事業向けの集計（地域 × 時間帯のすれ違い密度など）は P2。作る場合も個人を特定できない粒度（geohash 5 桁 × 1 時間、10 人未満のセルは出さない）に限り、プライバシーポリシーでの説明と同意を前提にする（README Q7）

## 4. ブロック（P0）

- `POST /api/v1/blocks {"did": "..."}`、`DELETE /api/v1/blocks/{did}`、`GET /api/v1/blocks`
- ブロックした時点で、フレンド関係・申請・位置共有を双方向で消す
- 以後の効果（どちらの向きのブロックでも同じ）

| 機能 | 効果 |
| --- | --- |
| すれ違い | sighting を捨てる（成立しない）。既存の encounter は一覧から消える |
| フレンド | 申請も招待コードの使用もできない |
| Party | 近接エッジを作らない。既に同じ party にいる場合はそのまま（自分から退出できる） |
| グループ | 相手の投稿が互いに見えない。招待の対象にできない |
| プロフィール | `GET /users/{did}` は `404` |

- ブロックしたことは相手に通知しない
- `audit_log` に記録する

## 5. アカウント削除（P0）

`DELETE /api/v1/me?deletePdsRecords=true|false`

1. `deletePdsRecords=true` なら、ユーザーの PDS にある `jp.i32.chain.*` の record を削除する（ATProto のアカウント自体は消さない）
2. ATProto の OAuth セッションを取り消し、アプリセッションをすべて失効させる
3. 次を物理削除する: beacon token、encounter、フレンド関係・申請・招待、位置共有・エリア、Redis の位置と近接データ、端末、通知、ブロック、設定
4. 自分が投稿した `records` とメディアを削除する。Space のメンバー行を消す（owner だった場合は 09 §5 の規則で引き継ぐ）
5. `users.deleted_at` を立てる（行は残して、Jetstream のイベントを無視する印にする）。`audit_log` の `actor_did` は NULL にする
6. 30 日後に `users` と `profiles` の行も消す
