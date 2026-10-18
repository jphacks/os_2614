# 07 フレンドと位置共有

## 1. 方針

フレンドの関係は Bluesky のフォロー（`app.bsky.graph.follow`）をそのまま使い、位置・beacon・Party の権限だけを CHAIN 側で持つ（19 §5.1）。

| 呼び方 | 定義 | できること |
| --- | --- | --- |
| フレンド | どちらも CHAIN の利用者で、**Bluesky で相互フォロー**している 2 人 | フレンド一覧に出る。プロフィールを見られる（13 §1.2） |
| 会ったフレンド | フレンドのうち、CHAIN で**会った裏付け**（§2.4）がある 2 人。`friendships` に持つ | 上に加えて、位置共有（§4）、beacon の配布と Party の自動作成（08） |

- draft どおり**ユーザー検索は作らない**。CHAIN からフォローを書けるのは、会った裏付けになりうる関係（§2）がある相手だけ
- Bluesky のアプリで相互フォローになった相手もフレンドになる。ただし会った裏付けが無い間は、位置も beacon も Party も無い
- 08・09 などで「フレンド」と書いている権限の条件は、すべて「会ったフレンド」を指す
- 会ったフレンドの上限は 500（beacon の配布量のため）。フレンド（相互フォロー）には上限を設けない
- フォローは公開の record なので、**CHAIN でフレンド申請・承認をすると、Bluesky でもフォローしたことが公開される**。申請と承認のボタンの近くにその旨を出す（19 §0.2 の「操作ごとに本人が選ぶ」を、申請という操作で満たす）

## 2. フレンドになる経路（P0）

| 経路 | CHAIN で書くもの | 会った裏付け |
| --- | --- | --- |
| 招待コード（QR） | 2 人のフォローを両方書く | `invite`。読み取った時点で成立 |
| すれ違いから申請 | 申請した人のフォロー。相手が承認したら相手のフォロー | 相手との **revealed** な encounter（`encounter`） |
| Space から申請 | 同上 | 同じ Space（active / archived）のメンバーだった（`space`） |
| Bluesky のアプリで相互フォロー | 何も書かない（Jetstream で知る） | 上の 3 つの関係があれば付く（§2.4） |

CHAIN からの申請は、上記の関係がない相手には `403 forbidden`。

### 2.1 招待コード

`POST /api/v1/friends/invites` → `201`

```json
{ "code": "7KQ2-M9XD", "url": "chain://friend/7KQ2M9XD", "expiresAt": "2026-10-17T03:22:45Z" }
```

- 8 文字の Crockford Base32（約 40 ビット）。有効期限 10 分、1 回限り。DB には SHA-256 だけを保存する
- 同時に有効なコードは 1 ユーザー 1 個。新しく作ると古いコードは失効する
- コードを作ることは、読み取った相手をフォローすることへの同意とみなす（作成画面にその旨を出す）

`POST /api/v1/friends/invites/{code}/accept`

- 期限切れ・使用済みは `410 gone`。自分のコードは `400`。ブロック関係は `403 blocked`。既に会ったフレンドなら `409 conflict`
- 読み取った人のフォローを、その人のセッションで書く。作成者のフォローを、作成者のセッションで書く。作成者の ATProto セッションが失効していたら、作成者の次のログイン時に書く（`friend_invites.follow_pending = 1`）
- フォローを待たずに、`friend_invites.used_by` で会った裏付け（`invite`）を記録し、作成者に `friend.accepted` を通知する。会ったフレンドになるのは、相互フォローがそろった時（§2.4）

### 2.2 申請（= フォロー）

申請は「相手を Bluesky でフォローすること」、承認は「フォローを返すこと」として表す。申請の表は持たず、`follows`（§5）から作る。

`POST /api/v1/friends/requests` `{ "did": "...", "via": "encounter" | "space", "viaRef": "encounter または space の ID" }`

- `viaRef` が §2 の条件を満たすかを確認し、自分のセッションで `app.bsky.graph.follow` を書く
- 既に相手が自分をフォローしていれば、そのままフレンドになる（承認と同じ）
- scope が足りなければ `403 atproto_reauth_required`（17 §2.4）

| API | 中身 |
| --- | --- |
| `GET /api/v1/friends/requests?box=incoming` | 自分をフォローしていて、自分はフォローしていない CHAIN の利用者のうち、§2 の関係がある人。`dismissed` にした人は出さない |
| `GET /api/v1/friends/requests?box=outgoing` | 自分がフォローしていて、相手はフォローしていない CHAIN の利用者のうち、§2 の関係がある人 |
| `POST /api/v1/friends/requests/{id}/accept` | フォローを返す（受信者のみ） |
| `POST /api/v1/friends/requests/{id}/decline` | `follow_dismissals` に入れて、受信箱に出さなくする。Bluesky には何も書かない。相手のフォローは残る |
| `DELETE /api/v1/friends/requests/{id}` | 自分のフォローを消す（送信者が取り消す） |

- `{id}` は `follows.id`（申請の元になったフォロー）
- §2 の関係が無い相手からのフォローは、受信箱に出さない（Bluesky のフォロワーは CHAIN の申請ではない）。その相手とは、Bluesky のアプリでフォローを返せばフレンドになる
- 相手へ `friend.request` を通知するのは、受信箱に出るフォローを Jetstream で受けた時だけ（12）
- 申請を拒否された後の「7 日間は再申請できない」はやめる（フォローは Bluesky のアプリからいつでも書けるため）

### 2.3 一覧と削除

- `GET /api/v1/friends`: フレンド（相互フォロー）の一覧。プロフィールの要約、`since`（相互フォローになった時刻）、`met`（会ったフレンドか）、`via`（会った裏付けの経路。無ければ `null`）、自分が相手に設定している共有精度（会ったフレンドでなければ `off`）を返す。`met` と `via` は足すだけで、いまの項目の形は変えない
- `GET /api/v1/friends/requests?box=incoming|outgoing`（§2.2）
- `DELETE /api/v1/friends/{did}`: 自分のフォローを消す（Bluesky でもフォローが外れる）。相互フォローでなくなるので、会ったフレンドでもなくなり、双方向の `location_shares` も消える（§2.4）。相手には通知しない

### 2.4 会った裏付けと `friendships`

`friendships` は「会ったフレンド」の表で、次の 2 つがそろった組だけを持つ。

1. 相互フォロー（`follows` に両方向がある）で、どちらの向きにもブロックが無い（13 §4）
2. 会った裏付けがある: revealed な encounter（hidden にしたものは除く）、同じ Space のメンバーだった、招待コードを使った、のどれか

- `friend.Reconciler` が、次のきっかけで組ごとに 1 と 2 を確かめ、`friendships` に入れる / 消す
  - `follows` の追加・削除（Jetstream、CHAIN からの書き込み、ログイン時の読み込み）
  - encounter が revealed になった / hidden になった
  - 同じ Space に入った（入った人と、既にいるメンバーのうちフレンドの人だけを確かめる）
  - 招待コードを使った
  - ブロックした / 解除した
- `friendships.via` には、最初に満たした裏付けの経路を入れる
- 入れたら `friend.changed` を送り、相手への位置の既定（`area`、§4.1）を作る。消したら双方向の `location_shares` を消し、`friend.changed` を送る
- 裏付けは、相互フォローになる前に満たしていてもよい（例: すれ違った後、Bluesky のアプリで相互フォローになった）。相互フォローになった時点で会ったフレンドになる
- フォローを外して付け直した場合も、裏付けが残っていれば会ったフレンドに戻る

### 2.5 フォローの取り込み

- Jetstream の購読（03 §5）に `app.bsky.graph.follow` と `app.bsky.graph.block` を足す。作者が `users` にいないイベントは捨てる
- 相手（`subject`）が CHAIN の利用者でないフォローも `follows` に入れる。後から相手が登録した時に、すぐフレンドを判定できるようにするため
- ログイン時（初回と、24 時間以上たってからのログイン）に、自分の PDS の `listRecords` で `app.bsky.graph.follow` と `app.bsky.graph.block` をすべて読み直す（17 §2.5 と同じ方法）。読み直した結果に無いフォローは消す
- 自分をフォローしている人は、相手の repo を読まないと分からない。CHAIN の利用者どうしだけが要るので、各利用者が自分のフォローを取り込んでいれば足りる
- Bluesky のブロックは `blocks` に `source = 'bsky'` で入れ、CHAIN のブロックと同じ効果を持たせる（13 §4）。CHAIN からは解除できない（Bluesky のアプリで解除する）
- CHAIN でのブロックは、いまどおり CHAIN の中だけのもの（`source = 'chain'`。相手に知られない）。Bluesky のフォローは書き換えない。ブロックの間は 2.4 の 1 を満たさないので、フレンド一覧にも出ない

## 3. 状態遷移

```
                 片方がフォロー          もう片方もフォロー
 (なし) ─────────────────▶ 申請中 ─────────────────▶ フレンド
                    ▲   │ 取消（フォローを消す）        │  会った裏付けがそろう
                    │   ▼                               ▼
                    └ (なし)                       会ったフレンド
                                                        │ フォローを外す・ブロック
                                                        ▼
                                                  申請中 / (なし)
```

- 拒否（decline）は状態を変えず、受信箱に出さなくするだけ

## 4. 位置共有（P1）

### 4.1 精度

draft の論点「位置情報の粒度をどうするか」には、相手ごとに精度を選べる形で答える。

| precision | 相手に見えるもの | 優先度 |
| --- | --- | --- |
| `exact` | 緯度経度（小数 5 桁 ≒ 1 m）と精度 | P1 |
| `approx` | geohash 6 桁のセルの中心（約 1.2 km × 0.6 km） | P1 |
| `area` | 自分が登録したエリアの label（「大学」など）。どのエリアにもいなければ「エリア外」 | P1（既定） |
| `direction` | 見る側から見て相手がいる 8 方位と距離帯（<100 m / <500 m / <2 km / <10 km / それ以上）。見る側の位置も 2 分以内のものが要る | P1 |
| `off` | 何も見えない | P1 |

- `areas_only = 1` にすると、自分が登録エリアのどれかにいるときだけ、その精度で見せる（draft「指定したエリアのみも可」、P1）。エリアの外にいるときは `off` と同じ
- `direction` で、見る側の位置が 2 分より古い（またはない）ときは、方角と距離を返さずに `"viewerStale": true` を返す（見る側に位置の送信を促すため）
- 方角は北を 0° とした真方位で、N / NE / E / SE / S / SW / W / NW の 8 つに丸める。距離は haversine
- 新しく会ったフレンドになった相手への既定は `area`。会ったフレンドでない相手には位置を共有できない（`PUT .../location-share` は `403 forbidden`）
- `PUT /api/v1/me/location-sharing {"paused": true}` で全員への共有を一時停止できる

### 4.2 API

- `PUT /api/v1/friends/{did}/location-share` `{ "precision": "area", "areasOnly": false }`。会ったフレンドだけ
- `GET /api/v1/friends/locations`: 会ったフレンドだけを返す

```json
{
  "items": [
    { "did": "did:plc:...", "precision": "area", "area": { "label": "大学" }, "updatedAt": "2026-10-17T03:12:00Z" },
    { "did": "did:plc:...", "precision": "exact", "lat": 35.02613, "lon": 135.78089, "accuracyM": 12, "updatedAt": "..." },
    { "did": "did:plc:...", "precision": "off" }
  ]
}
```

- 位置は相手の `loc:{did}`（最長 30 分前まで）から、相手が自分に設定した精度で毎回計算して返す。保存はしない
- `updatedAt` は分単位に丸める
- 30 分以上更新のない相手は `"stale": true` とし、位置を返さない

### 4.3 エリア

- `GET / POST /api/v1/me/areas`、`PATCH / DELETE /api/v1/me/areas/{id}`
- 1 ユーザー 10 個まで、半径 50〜5000 m、label は 20 文字まで
- エリアの label は、area 精度で共有している相手にだけ見える

### 4.4 クライアントへの要求

- 常時の位置共有にはバックグラウンド位置（native）が必要（research §5）。backend はどこから来た位置かを区別しない
- 推奨の送信頻度: 前面では 30 秒ごと、バックグラウンドでは 5 分ごとか 100 m 移動ごと

## 5. データモデル

| 表 | 変更 |
| --- | --- |
| `follows`（新規） | CHAIN の利用者が書いた `app.bsky.graph.follow` の索引 |
| `follow_dismissals`（新規） | 受信箱で拒否したフォロー |
| `friendships` | 「会ったフレンド」の表にする。列は変えない（`via` は会った裏付けの経路） |
| `friend_requests` | 削除（申請は `follows` から作る） |
| `friend_invites` | `follow_pending INTEGER NOT NULL DEFAULT 0` を追加（§2.1） |
| `blocks` | `source TEXT NOT NULL DEFAULT 'chain' CHECK (source IN ('chain','bsky'))`、`uri TEXT`（Bluesky のブロックの record）を追加 |

```sql
CREATE TABLE follows (
  id           TEXT PRIMARY KEY,               -- UUIDv7。申請の ID として使う
  uri          TEXT NOT NULL UNIQUE,           -- at://{author}/app.bsky.graph.follow/{rkey}
  author_did   TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  subject_did  TEXT NOT NULL,                  -- CHAIN の利用者とは限らない
  created_at   INTEGER NOT NULL,               -- record の createdAt
  indexed_at   INTEGER NOT NULL
);
CREATE UNIQUE INDEX follows_pair ON follows(author_did, subject_did);
CREATE INDEX follows_subject ON follows(subject_did);

CREATE TABLE follow_dismissals (
  did          TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,   -- 拒否した人
  follower_did TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  PRIMARY KEY (did, follower_did)
) WITHOUT ROWID;
```

- 同じ相手へのフォローが Bluesky 側で 2 件以上ある場合は、古い方を `follows` に持つ（`follows_pair` で 1 件に絞る）。CHAIN から消すときは、`listRecords` で見つかった同じ相手へのフォローをすべて消す
- 04 §5 のとおり migration は使わないので、schema.sql と 04 を直して作り直す。いまの `friendships` の行は引き継がない
