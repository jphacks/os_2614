# 14 エンドポイント一覧

共通規約は 02。認証列の「—」は認証不要、「✓」はアクセス JWT が必要。

## 認証・メタデータ

| メソッド | パス | 認証 | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- | --- |
| GET | `/health` | — | 死活確認（SQLite と Redis への疎通） | P0 | — |
| GET | `/oauth-client-metadata.json` | — | OAuth client metadata | P0 | 03 §2.1 |
| GET | `/oauth/jwks.json` | — | client の公開鍵 | P0 | 03 §2.1 |
| GET | `/.well-known/did.json` | — | authority の DID ドキュメント（did:web） | P2 | 03 §7 |
| GET | `/auth/login?handle=` | — | OAuth 開始。認可画面へ 302 | P0 | 03 §2 |
| GET | `/auth/callback` | — | OAuth コールバック。アプリの deep link へ 302 | P0 | 03 §2 |
| GET | `/auth/dev/complete` | — | 開発用の完了画面。トークンを JSON で表示する（`CHAIN_ENV=dev` のときだけ） | P0 | 03 §2.1.2 |
| POST | `/api/v1/auth/exchange` | — | one-time code → アプリトークン | P0 | 03 §2 |
| POST | `/api/v1/auth/refresh` | — | トークンのローテーション | P0 | 03 §3 |
| POST | `/api/v1/auth/logout` | ✓ | セッション失効 | P0 | 03 §3 |

## 自分

| メソッド | パス | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- |
| GET | `/api/v1/me` | 自分のプロフィールと設定 | P0 | 13 §1 |
| PATCH | `/api/v1/me/settings` | すれ違い公開設定、party、warn 表示、通知 | P0 | 06 §5, 08 §2, 11 §1, 12 §3 |
| PUT | `/api/v1/me/consents` | 投稿内容の自動判定への同意 `{"contentScan": 1}`（版番号）。`0` で撤回 | P0 | 02 §8 |
| PUT | `/api/v1/me/status` | ステータス | P1 | 13 §1 |
| PATCH | `/api/v1/me/profile` | 名前・説明の編集（Bluesky のプロフィール） | P1 | 13 §1.3 |
| PUT / DELETE | `/api/v1/me/avatar`、`/api/v1/me/banner` | アイコン・バナーの差し替え / 削除 | P1 | 13 §1.3 |
| PUT | `/api/v1/me/location` | 位置の送信 | P0 | 05 §4 |
| PUT | `/api/v1/me/location-sharing` | 位置共有の一時停止 | P1 | 07 §4 |
| GET / POST | `/api/v1/me/areas` | エリア一覧 / 作成 | P1 | 07 §4.3 |
| PATCH / DELETE | `/api/v1/me/areas/{id}` | エリアの変更 / 削除 | P1 | 07 §4.3 |
| PUT | `/api/v1/me/devices` | FCM トークンの登録 | P0 | 12 §2 |
| DELETE | `/api/v1/me/devices/{fcmToken}` | FCM トークンの削除 | P0 | 12 §2 |
| GET | `/api/v1/me/stats` | 統計 | P1 | 13 §3 |
| GET / PUT / DELETE | `/api/v1/me/schedule-today` | 今日・明日の予定（タイトルと時間）。`source` は `calendar`（native が端末のカレンダーから）/ `manual`（手入力） | P2 | 09 §8.2、android §11 |
| DELETE | `/api/v1/me` | アカウント削除 | P0 | 13 §5 |

## ユーザー・ブロック

| メソッド | パス | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- |
| GET | `/api/v1/users/{did}` | プロフィール（接点のある相手のみ） | P0 | 13 §1.2 |
| GET / POST | `/api/v1/blocks` | ブロック一覧 / ブロック | P0 | 13 §4 |
| DELETE | `/api/v1/blocks/{did}` | ブロック解除 | P0 | 13 §4 |

## すれ違い

| メソッド | パス | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- |
| GET | `/api/v1/beacon/tokens?hours=` | beacon token の取得 | P0 | 05 §2.2 |
| POST | `/api/v1/encounters/sightings` | sighting の送信 | P0 | 06 §2 |
| GET | `/api/v1/encounters` | 公開済みのすれ違い一覧 | P0 | 06 §6 |
| GET | `/api/v1/encounters/summary` | 回数の集計 | P1 | 06 §6 |

## フレンド・位置共有

| メソッド | パス | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- |
| GET | `/api/v1/friends` | フレンド一覧 | P0 | 07 §2.3 |
| DELETE | `/api/v1/friends/{did}` | フレンド削除 | P0 | 07 §2.3 |
| POST | `/api/v1/friends/invites` | 招待コードの作成 | P0 | 07 §2.1 |
| POST | `/api/v1/friends/invites/{code}/accept` | 招待コードの使用 | P0 | 07 §2.1 |
| GET | `/api/v1/friends/requests?box=` | 申請一覧 | P0 | 07 §2.2 |
| POST | `/api/v1/friends/requests` | 申請 | P0 | 07 §2.2 |
| POST | `/api/v1/friends/requests/{id}/accept`、`/decline` | 承認 / 拒否 | P0 | 07 §2.2 |
| DELETE | `/api/v1/friends/requests/{id}` | 申請の取り消し | P0 | 07 §2.2 |
| PUT | `/api/v1/friends/{did}/location-share` | 相手ごとの共有精度 | P1 | 07 §4.2 |
| GET | `/api/v1/friends/locations` | フレンドの位置 | P1 | 07 §4.2 |

## Party

| メソッド | パス | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- |
| GET | `/api/v1/party/current` | 所属中の party | P0 | 08 §6 |
| GET | `/api/v1/party/friend-beacons` | フレンドの token | P0 | 08 §2 |
| POST | `/api/v1/party/presence` | フレンドの近接の報告 | P0 | 08 §2 |
| PATCH | `/api/v1/parties/{id}` | 名前の変更 | P0 | 08 §6 |
| POST | `/api/v1/parties/{id}/leave` | 退出 | P0 | 08 §4.2 |
| POST | `/api/v1/parties/{id}/lock`、`/unlock` | 固定 / 解除 | P1 | 08 §4.4 |

## グループ

| メソッド | パス | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- |
| POST | `/api/v1/groups` | 作成 | P0 | 09 §2 |
| GET | `/api/v1/groups/nearby` | 付近の open グループ | P0 | 09 §5 |
| GET | `/api/v1/groups/graph` | 包含関係のグラフ | P1 | 09 §7 |
| GET | `/api/v1/groups/{id}` | 詳細 | P0 | 09 §4 |
| PATCH | `/api/v1/groups/{id}` | 変更（owner） | P0 | 09 §9 |
| DELETE | `/api/v1/groups/{id}` | 削除（owner） | P1 | 09 §9 |
| POST | `/api/v1/groups/{id}/join` | 参加 | P0 | 09 §5 |
| POST | `/api/v1/groups/{id}/leave` | 退出 | P0 | 09 §5 |
| GET | `/api/v1/groups/{id}/members` | メンバー一覧 | P0 | 09 §4 |
| DELETE | `/api/v1/groups/{id}/members/{did}` | 追放（owner） | P1 | 09 §9 |
| POST | `/api/v1/groups/{id}/invites` | 招待の作成 | P1 | 09 §6 |
| POST | `/api/v1/groups/{id}/invites/{inviteId}/approve` | 招待の承認 | P1 | 09 §6 |
| GET | `/api/v1/groups/{id}/invites` | 承認待ちの招待（メンバー） | P1 | 09 §6 |
| DELETE | `/api/v1/groups/{id}/invites/{inviteId}` | 招待の取り消し（招待者、`collecting` の間だけ） | P1 | 09 §6 |
| GET | `/api/v1/group-invites` | 自分宛ての招待 | P1 | 09 §6 |
| POST | `/api/v1/group-invites/{inviteId}/accept`、`/decline` | 招待への応答（対象者） | P1 | 09 §6 |
| GET | `/api/v1/groups/{id}/votes` | 投票の一覧（開いているものと、7 日以内に締まったもの） | P2 | 09 §8.6 |
| POST | `/api/v1/groups/{id}/votes` | 投票の発議 | P2 | 09 §8.6 |
| POST | `/api/v1/groups/{id}/votes/{voteId}/ballot` | 投票 | P2 | 09 §8 |

## instance・メディア

| メソッド | パス | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- |
| GET | `/api/v1/instances` | 自分の Space の一覧（チャット一覧） | P0 | 13 §2 |
| POST | `/api/v1/posts` | 投稿（複数 Space、公開投稿の同時作成） | P0 | 10 §4.1 |
| GET | `/api/v1/instances/{spaceId}/posts` | 投稿一覧 | P0 | 10 §4.2 |
| DELETE | `/api/v1/instances/{spaceId}/posts/{rkey}` | 自分の投稿の削除 | P0 | 10 §4.3 |
| DELETE | `/api/v1/instances/{spaceId}/posts/{authorDid}/{rkey}` | 他人の投稿の削除（owner） | P1 | 10 §4.3 |
| POST | `/api/v1/media` | 画像のアップロード | P0 | 10 §4.4 |
| POST | `/api/v1/instances/{spaceId}/media` | 写真の共有 | P0 | 10 §4.4 |
| GET | `/media/{id}?v=&exp=&sig=` | 画像の配信（署名付き URL） | P0 | 10 §4.4 |
| GET / POST | `/api/v1/instances/{spaceId}/pins` | Pin 一覧 / 手動 Pin | P1 | 10 §6 |
| DELETE | `/api/v1/instances/{spaceId}/pins/{rkey}` | Pin の解除 | P1 | 10 §6 |
| GET | `/api/v1/ws` | WebSocket | P0 | 10 §7 |

## 公開投稿・フィード

| メソッド | パス | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- |
| POST | `/api/v1/feed/posts` | 公開投稿（`alsoBluesky` で Bluesky にも） | P1 | 17 §2.1 |
| DELETE | `/api/v1/feed/posts/{rkey}` | 公開投稿の削除（Bluesky 側も） | P1 | 17 §2.2 |
| GET | `/api/v1/feed?source=` | 共通のフィード。source は `home` / `groups` / `space:{id}` / `user:{did}` | P1 | 17 §3 |

## memory・通知・その他

| メソッド | パス | 内容 | 優先度 | 仕様 |
| --- | --- | --- | --- | --- |
| GET | `/api/v1/memories` | archive 済み Space の一覧 | P0 | 13 §2 |
| DELETE | `/api/v1/memories/{spaceId}` | 自分の memory から隠す | P1 | 13 §2 |
| GET | `/api/v1/notifications` | 通知一覧 | P0 | 12 §1 |
| POST | `/api/v1/notifications/read` | `{"ids": [...]}` または `{"all": true}` で既読にする | P0 | 12 §1 |
| POST | `/api/v1/reports` | 通報 | P2 | 11 §6 |
