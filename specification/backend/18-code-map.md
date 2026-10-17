# 18 コードの地図

機能とコードのパス、コントローラーとそのパスの一覧。
**このファイルは `backend/scripts/codemap.py` が生成する。手で直さず、コードを変えたら作り直す**（`backend/` で `make codemap`）。
フォルダーの分け方の規則は 01 §3 と、各層の `doc.go`。

## 1. 機能とコードのパス

各層の機能フォルダーと、層の直下に残っている 1 ファイルの機能。パスは `backend/` からの相対。テスト（`*_test.go`）と `doc.go` は省く。

| 機能 | 仕様 | controller | service | model |
| --- | --- | --- | --- | --- |
| service の共通部分（Deps・定数・共通の型）（`core`） | 01 §3 | — | `internal/service/core/common.go`<br>`internal/service/core/geohash.go`<br>`internal/service/core/image.go`<br>`internal/service/core/params.go` | — |
| 層の共通部分（`helpers`） | 01 §3 | `internal/controller/helpers.go` | — | `internal/model/helpers.go` |
| ルートの登録（`routes`） | 01 §3 | `internal/controller/routes.go` | — | — |
| 認証・アプリセッション・アカウント削除（`auth`） | 03 §2・§3、13 §5 | `internal/controller/auth/account.go`<br>`internal/controller/auth/auth.go` | `internal/service/auth/account.go`<br>`internal/service/auth/auth.go`<br>`internal/service/auth/session.go` | — |
| ATProto OAuth のセッション（`oauth`） | 03 §2.2 | — | — | `internal/model/oauth.go` |
| アプリセッション（`session`） | 03 §3 | — | — | `internal/model/session.go` |
| 自分（設定・同意・ステータス・プロフィール編集）（`me`） | 13 §1、02 §8 | `internal/controller/me.go` | `internal/service/me/me.go`<br>`internal/service/me/profile_image.go` | — |
| ユーザー設定（`settings`） | 13 §1 | — | — | `internal/model/settings.go` |
| ユーザーとプロフィールの取得（`user`） | 13 §1.2 | `internal/controller/user.go` | `internal/service/user.go` | `internal/model/user.go` |
| handle の更新（`user_handle`） | 03 §5 | — | — | `internal/model/user_handle.go` |
| プロフィール（`profile`） | 13 §1 | — | — | `internal/model/profile.go` |
| 近接判定・beacon token・位置（`proximity`） | 05 | — | `internal/service/proximity/beacon.go`<br>`internal/service/proximity/beacon_payload.go`<br>`internal/service/proximity/location.go`<br>`internal/service/proximity/proximity.go` | `internal/model/proximity.go` |
| beacon token（`beacon`） | 05 §2 | — | — | `internal/model/beacon.go` |
| 位置（`location`） | 05 §4 | — | — | `internal/model/location.go` |
| すれ違い・統計（`encounter`） | 06、13 §3 | `internal/controller/encounter/beacon.go`<br>`internal/controller/encounter/encounter.go`<br>`internal/controller/encounter/location.go` | `internal/service/encounter/encounter.go`<br>`internal/service/encounter/stats.go`<br>`internal/service/encounter/summary.go` | `internal/model/encounter/encounter.go`<br>`internal/model/encounter/feed.go` |
| 統計（`stats`） | 13 §3 | `internal/controller/stats.go` | — | `internal/model/stats.go` |
| ブロック（`block`） | 13 §4 | `internal/controller/block.go` | `internal/service/block.go` | `internal/model/block.go` |
| 関係の判定（ブロック・フレンドなど）（`relationship`） | 13 §1.2・§4 | — | — | `internal/model/relationship.go` |
| フレンド・位置共有（`friend`） | 07 | `internal/controller/friend/friend.go`<br>`internal/controller/friend/location_share.go` | `internal/service/friend/friend.go`<br>`internal/service/friend/location_share.go` | `internal/model/friend/friend.go`<br>`internal/model/friend/location_share.go` |
| Party・memory（`party`） | 08、13 §2 | `internal/controller/party/memory.go`<br>`internal/controller/party/party.go` | `internal/service/party/party.go` | `internal/model/party.go` |
| グループ（作成・参加・招待・投票・入場通知・グラフ）（`group`） | 09 §1〜§7 | `internal/controller/group/group.go`<br>`internal/controller/group/invite.go`<br>`internal/controller/group/vote.go` | `internal/service/group/circle.go`<br>`internal/service/group/entry.go`<br>`internal/service/group/graph.go`<br>`internal/service/group/group.go`<br>`internal/service/group/invite.go`<br>`internal/service/group/vote.go` | `internal/model/group/group.go`<br>`internal/model/group/invite.go`<br>`internal/model/group/vote.go` |
| グループの時間帯 / 予定（自動グループ）（`schedule`） | 09 §3、09 §8.2 | `internal/controller/schedule.go` | `internal/service/schedule/schedule.go`<br>`internal/service/schedule/validate.go`<br>`internal/service/schedule/window.go` | `internal/model/schedule/match.go`<br>`internal/model/schedule/schedule.go` |
| 自動グループ（`natural`） | 09 §8 | — | `internal/service/natural/cluster.go`<br>`internal/service/natural/embedder.go`<br>`internal/service/natural/groups.go`<br>`internal/service/natural/schedule.go` | — |
| アクセス判定（`access`） | 10 §5 | — | `internal/service/access/access.go` | — |
| instance・Pin・メディア・フィード・公開投稿・Jetstream・WebSocket（`content`） | 10、17 | `internal/controller/content/feed.go`<br>`internal/controller/content/instance.go`<br>`internal/controller/content/media.go`<br>`internal/controller/content/media_serve.go`<br>`internal/controller/content/public_post.go` | `internal/service/content/feed.go`<br>`internal/service/content/instance.go`<br>`internal/service/content/jetstream.go`<br>`internal/service/content/media.go`<br>`internal/service/content/pin.go`<br>`internal/service/content/public_post.go`<br>`internal/service/content/realtime.go`<br>`internal/service/content/requeue.go` | — |
| Space・record・メディア・memory（`space`） | 04 §2.4、10 | — | — | `internal/model/space/instance.go`<br>`internal/model/space/media.go`<br>`internal/model/space/memory.go`<br>`internal/model/space/pin.go`<br>`internal/model/space/record.go`<br>`internal/model/space/space.go` |
| moderation（`moderation`） | 11 | — | `internal/service/moderation/jev.go`<br>`internal/service/moderation/moderation.go`<br>`internal/service/moderation/prompts.go` | — |
| moderation の再判定キュー（`modq`） | 11 §3 | — | — | `internal/model/modq.go` |
| 通知（積む・送る・端末・ダイジェスト）（`notify`） | 12 | — | `internal/service/notify/device.go`<br>`internal/service/notify/digest.go`<br>`internal/service/notify/notification.go`<br>`internal/service/notify/sender.go` | — |
| 通知（`notification`） | 12 | `internal/controller/notification.go` | — | `internal/model/notification.go` |
| 端末の登録（`device`） | 12 §2 | `internal/controller/device.go` | — | `internal/model/device.go` |
| 公開投稿（`public_post`） | 17 §2 | — | — | `internal/model/public_post.go` |
| Jetstream のカーソル（`jetstream`） | 03 §5 | — | — | `internal/model/jetstream.go` |
| アカウント削除と掃除（`maintenance`） | 13 §5、04 §4 | — | — | `internal/model/maintenance/account.go`<br>`internal/model/maintenance/janitor.go` |
| 掃除（`janitor`） | 04 §4 | — | `internal/service/janitor.go` | — |
| 監査ログ（`audit`） | 02 §7 | — | — | `internal/model/audit.go` |
| 排他（lock:）（`lock`） | 04 §3 | — | — | `internal/model/lock.go` |
| ヘルスチェック（`health`） | — | `internal/controller/health.go` | `internal/service/health.go` | — |
| WebSocket（`ws`） | 10 §7 | `internal/controller/ws.go` | — | — |

層に属さないもの:

| フォルダー | ファイル |
| --- | --- |
| `internal/worker/` | `internal/worker/digest.go`<br>`internal/worker/embed_backfill.go`<br>`internal/worker/encounter_revealer.go`<br>`internal/worker/group_votes.go`<br>`internal/worker/janitor.go`<br>`internal/worker/jetstream.go`<br>`internal/worker/moderation_requeue.go`<br>`internal/worker/natural_groups.go`<br>`internal/worker/notify_sender.go`<br>`internal/worker/party_engine.go` |
| `internal/external/` | `internal/external/atproto/firstload.go`<br>`internal/external/atproto/jetstream.go`<br>`internal/external/atproto/oauth.go`<br>`internal/external/atproto/profile.go`<br>`internal/external/atproto/store.go`<br>`internal/external/atproto/writer.go`<br>`internal/external/embed/client.go`<br>`internal/external/fcm/client.go`<br>`internal/external/fcm/credentials.go`<br>`internal/external/fcm/wire.go`<br>`internal/external/nanogpt/client.go` |
| `internal/realtime/` | `internal/realtime/hub.go` |
| `internal/storage/` | `internal/storage/media.go`<br>`internal/storage/sign.go` |

共通の部品: `internal/httpx/`（エラー・認証・レート制限・Bind）、`internal/auth/`（JWT・暗号化）、`internal/db/`、`internal/kv/`、`internal/config/`、`internal/clock/`、`internal/idgen/`、`internal/utils/`、`internal/testutil/`、`cmd/dbreset/`、`cmd/keygen/`

## 2. コントローラー一覧

| コントローラー | ファイル | 登録先 | パスの数 |
| --- | --- | --- | --- |
| `HealthController` | `internal/controller/health.go` | ルート（`/api/v1` の外、認証なし） | 1 |
| `OAuthController` | `internal/controller/auth/auth.go` | ルート（`/api/v1` の外、認証なし） | 5 |
| `MediaDeliveryController` | `internal/controller/content/media_serve.go` | ルート（`/api/v1` の外、認証なし） | 1 |
| `AuthController` | `internal/controller/auth/auth.go` | `/api/v1`（認証なし） | 2 |
| `WSController` | `internal/controller/ws.go` | `/api/v1`（認証なし） | 1 |
| `LogoutController` | `internal/controller/auth/auth.go` | `/api/v1`（認証あり） | 1 |
| `MeController` | `internal/controller/me.go` | `/api/v1`（認証あり） | 9 |
| `ScheduleController` | `internal/controller/schedule.go` | `/api/v1`（認証あり） | 3 |
| `StatsController` | `internal/controller/stats.go` | `/api/v1`（認証あり） | 1 |
| `UserController` | `internal/controller/user.go` | `/api/v1`（認証あり） | 1 |
| `DeviceController` | `internal/controller/device.go` | `/api/v1`（認証あり） | 2 |
| `BeaconController` | `internal/controller/encounter/beacon.go` | `/api/v1`（認証あり） | 1 |
| `LocationController` | `internal/controller/encounter/location.go` | `/api/v1`（認証あり） | 1 |
| `LocationShareController` | `internal/controller/friend/location_share.go` | `/api/v1`（認証あり） | 7 |
| `EncounterController` | `internal/controller/encounter/encounter.go` | `/api/v1`（認証あり） | 3 |
| `BlockController` | `internal/controller/block.go` | `/api/v1`（認証あり） | 3 |
| `FriendController` | `internal/controller/friend/friend.go` | `/api/v1`（認証あり） | 9 |
| `NotificationController` | `internal/controller/notification.go` | `/api/v1`（認証あり） | 2 |
| `InstanceController` | `internal/controller/content/instance.go` | `/api/v1`（認証あり） | 7 |
| `FeedPostController` | `internal/controller/content/public_post.go` | `/api/v1`（認証あり） | 2 |
| `FeedController` | `internal/controller/content/feed.go` | `/api/v1`（認証あり） | 1 |
| `MediaController` | `internal/controller/content/media.go` | `/api/v1`（認証あり） | 2 |
| `GroupController` | `internal/controller/group/group.go` | `/api/v1`（認証あり） | 8 |
| `GroupVoteController` | `internal/controller/group/vote.go` | `/api/v1`（認証あり） | 3 |
| `GroupInviteController` | `internal/controller/group/invite.go` | `/api/v1`（認証あり） | 7 |
| `PartyController` | `internal/controller/party/party.go` | `/api/v1`（認証あり） | 7 |
| `MemoryController` | `internal/controller/party/memory.go` | `/api/v1`（認証あり） | 2 |
| `AccountController` | `internal/controller/auth/account.go` | `/api/v1`（認証あり） | 1 |

## 3. パスの一覧

- 認証: 「要」は `Authorization: Bearer` のアクセス JWT が要る（03 §3）。WebSocket は接続後の `auth` メッセージで認証する（10 §7.1）
- レート制限: 02 §5 のバケット。認証ありのパスは専用のバケットに加えて `default` も数える

| メソッド | パス | コントローラー | ハンドラ | 認証 | レート制限 | 仕様 |
| --- | --- | --- | --- | --- | --- | --- |
| GET | `/health` | `HealthController` | `get` | 不要 | なし | — |
| GET | `/oauth-client-metadata.json` | `OAuthController` | `metadata` | 不要 | なし | 03 §2.1 |
| GET | `/oauth/jwks.json` | `OAuthController` | `jwks` | 不要 | なし | 03 §2.1 |
| GET | `/auth/login` | `OAuthController` | `login` | 不要 | `auth`（IP 単位） | 03 §2 |
| GET | `/auth/callback` | `OAuthController` | `callback` | 不要 | `auth`（IP 単位） | 03 §2 |
| GET | `/auth/dev/complete` | `OAuthController` | `devComplete` | 不要 | `auth`（IP 単位） | 03 §2.1.2 |
| GET | `/media/{id}` | `MediaDeliveryController` | `serve` | 不要 | なし | 10 §4.4 |
| POST | `/api/v1/auth/exchange` | `AuthController` | `exchange` | 不要 | `auth`（IP 単位） | 03 §2 |
| POST | `/api/v1/auth/refresh` | `AuthController` | `refresh` | 不要 | `auth`（IP 単位） | 03 §3 |
| GET | `/api/v1/ws` | `WSController` | `handle` | auth メッセージ | なし | 10 §7 |
| POST | `/api/v1/auth/logout` | `LogoutController` | `logout` | 要 | `default` | 03 §3 |
| GET | `/api/v1/me` | `MeController` | `get` | 要 | `default` | 13 §1 |
| PATCH | `/api/v1/me/settings` | `MeController` | `updateSettings` | 要 | `default` | 06 §5, 08 §2, 11 §1, 12 §3 |
| PUT | `/api/v1/me/consents` | `MeController` | `updateConsent` | 要 | `default` | 02 §8 |
| PUT | `/api/v1/me/status` | `MeController` | `updateStatus` | 要 | `default` | 13 §1 |
| PATCH | `/api/v1/me/profile` | `MeController` | `updateProfile` | 要 | `default` | 13 §1.3 |
| PUT | `/api/v1/me/avatar` | `MeController` | `updateAvatar` | 要 | `media` ＋ `default` | 13 §1.3 |
| DELETE | `/api/v1/me/avatar` | `MeController` | `deleteAvatar` | 要 | `default` | 13 §1.3 |
| PUT | `/api/v1/me/banner` | `MeController` | `updateBanner` | 要 | `media` ＋ `default` | 13 §1.3 |
| DELETE | `/api/v1/me/banner` | `MeController` | `deleteBanner` | 要 | `default` | 13 §1.3 |
| PUT | `/api/v1/me/schedule-today` | `ScheduleController` | `put` | 要 | `default` | 09 §8.2、android §11 |
| GET | `/api/v1/me/schedule-today` | `ScheduleController` | `get` | 要 | `default` | 09 §8.2、android §11 |
| DELETE | `/api/v1/me/schedule-today` | `ScheduleController` | `delete` | 要 | `default` | 09 §8.2、android §11 |
| GET | `/api/v1/me/stats` | `StatsController` | `get` | 要 | `default` | 13 §3 |
| GET | `/api/v1/users/{did}` | `UserController` | `get` | 要 | `default` | 13 §1.2 |
| PUT | `/api/v1/me/devices` | `DeviceController` | `put` | 要 | `default` | 12 §2 |
| DELETE | `/api/v1/me/devices/{fcmToken}` | `DeviceController` | `delete` | 要 | `default` | 12 §2 |
| GET | `/api/v1/beacon/tokens` | `BeaconController` | `tokens` | 要 | `default` | 05 §2.2 |
| PUT | `/api/v1/me/location` | `LocationController` | `update` | 要 | `location` ＋ `default` | 05 §4 |
| PUT | `/api/v1/friends/{did}/location-share` | `LocationShareController` | `setShare` | 要 | `default` | 07 §4.2 |
| GET | `/api/v1/friends/locations` | `LocationShareController` | `listLocations` | 要 | `default` | 07 §4.2 |
| PUT | `/api/v1/me/location-sharing` | `LocationShareController` | `setPaused` | 要 | `default` | 07 §4 |
| GET | `/api/v1/me/areas` | `LocationShareController` | `listAreas` | 要 | `default` | 07 §4.3 |
| POST | `/api/v1/me/areas` | `LocationShareController` | `createArea` | 要 | `default` | 07 §4.3 |
| PATCH | `/api/v1/me/areas/{id}` | `LocationShareController` | `updateArea` | 要 | `default` | 07 §4.3 |
| DELETE | `/api/v1/me/areas/{id}` | `LocationShareController` | `deleteArea` | 要 | `default` | 07 §4.3 |
| POST | `/api/v1/encounters/sightings` | `EncounterController` | `sightings` | 要 | `sightings` ＋ `default` | 06 §2 |
| GET | `/api/v1/encounters` | `EncounterController` | `list` | 要 | `default` | 06 §6 |
| GET | `/api/v1/encounters/summary` | `EncounterController` | `summary` | 要 | `default` | 06 §6 |
| POST | `/api/v1/blocks` | `BlockController` | `create` | 要 | `default` | 13 §4 |
| DELETE | `/api/v1/blocks/{did}` | `BlockController` | `delete` | 要 | `default` | 13 §4 |
| GET | `/api/v1/blocks` | `BlockController` | `list` | 要 | `default` | 13 §4 |
| POST | `/api/v1/friends/invites` | `FriendController` | `createInvite` | 要 | `default` | 07 §2.1 |
| POST | `/api/v1/friends/invites/{code}/accept` | `FriendController` | `acceptInvite` | 要 | `default` | 07 §2.1 |
| POST | `/api/v1/friends/requests` | `FriendController` | `createRequest` | 要 | `friendreq` ＋ `default` | 07 §2.2 |
| GET | `/api/v1/friends/requests` | `FriendController` | `listRequests` | 要 | `default` | 07 §2.2 |
| POST | `/api/v1/friends/requests/{id}/accept` | `FriendController` | `acceptRequest` | 要 | `default` | 07 §2.2 |
| POST | `/api/v1/friends/requests/{id}/decline` | `FriendController` | `declineRequest` | 要 | `default` | 07 §2.2 |
| DELETE | `/api/v1/friends/requests/{id}` | `FriendController` | `cancelRequest` | 要 | `default` | 07 §2.2 |
| GET | `/api/v1/friends` | `FriendController` | `list` | 要 | `default` | 07 §2.3 |
| DELETE | `/api/v1/friends/{did}` | `FriendController` | `delete` | 要 | `default` | 07 §2.3 |
| GET | `/api/v1/notifications` | `NotificationController` | `list` | 要 | `default` | 12 §1 |
| POST | `/api/v1/notifications/read` | `NotificationController` | `read` | 要 | `default` | 12 §1 |
| GET | `/api/v1/instances` | `InstanceController` | `listInstances` | 要 | `default` | 13 §2 |
| POST | `/api/v1/posts` | `InstanceController` | `createPost` | 要 | `post` ＋ `default` | 10 §4.1 |
| GET | `/api/v1/instances/{spaceId}/posts` | `InstanceController` | `listPosts` | 要 | `default` | 10 §4.2 |
| DELETE | `/api/v1/instances/{spaceId}/posts/{rkey}` | `InstanceController` | `deletePost` | 要 | `default` | 10 §4.3 |
| GET | `/api/v1/instances/{spaceId}/pins` | `InstanceController` | `listPins` | 要 | `default` | 10 §6 |
| POST | `/api/v1/instances/{spaceId}/pins` | `InstanceController` | `createPin` | 要 | `default` | 10 §6 |
| DELETE | `/api/v1/instances/{spaceId}/pins/{rkey}` | `InstanceController` | `deletePin` | 要 | `default` | 10 §6 |
| POST | `/api/v1/feed/posts` | `FeedPostController` | `create` | 要 | `post` ＋ `default` | 17 §2.1 |
| DELETE | `/api/v1/feed/posts/{rkey}` | `FeedPostController` | `delete` | 要 | `default` | 17 §2.2 |
| GET | `/api/v1/feed` | `FeedController` | `list` | 要 | `default` | 17 §3 |
| POST | `/api/v1/media` | `MediaController` | `upload` | 要 | `media` ＋ `default` | 10 §4.4 |
| POST | `/api/v1/instances/{spaceId}/media` | `MediaController` | `share` | 要 | `media` ＋ `default` | 10 §4.4 |
| GET | `/api/v1/groups/nearby` | `GroupController` | `nearby` | 要 | `default` | 09 §5 |
| GET | `/api/v1/groups/graph` | `GroupController` | `graph` | 要 | `default` | 09 §7 |
| POST | `/api/v1/groups` | `GroupController` | `create` | 要 | `default` | 09 §2 |
| GET | `/api/v1/groups/{id}` | `GroupController` | `get` | 要 | `default` | 09 §4 |
| PATCH | `/api/v1/groups/{id}` | `GroupController` | `update` | 要 | `default` | 09 §9 |
| POST | `/api/v1/groups/{id}/join` | `GroupController` | `join` | 要 | `default` | 09 §5 |
| POST | `/api/v1/groups/{id}/leave` | `GroupController` | `leave` | 要 | `default` | 09 §5 |
| GET | `/api/v1/groups/{id}/members` | `GroupController` | `members` | 要 | `default` | 09 §4 |
| POST | `/api/v1/groups/{id}/votes` | `GroupVoteController` | `propose` | 要 | `default` | 09 §8.6 |
| GET | `/api/v1/groups/{id}/votes` | `GroupVoteController` | `list` | 要 | `default` | 09 §8.6 |
| POST | `/api/v1/groups/{id}/votes/{voteId}/ballot` | `GroupVoteController` | `ballot` | 要 | `default` | 09 §8 |
| POST | `/api/v1/groups/{id}/invites` | `GroupInviteController` | `create` | 要 | `default` | 09 §6 |
| GET | `/api/v1/groups/{id}/invites` | `GroupInviteController` | `list` | 要 | `default` | 09 §6 |
| POST | `/api/v1/groups/{id}/invites/{inviteId}/approve` | `GroupInviteController` | `approve` | 要 | `default` | 09 §6 |
| DELETE | `/api/v1/groups/{id}/invites/{inviteId}` | `GroupInviteController` | `cancel` | 要 | `default` | 09 §6 |
| GET | `/api/v1/group-invites` | `GroupInviteController` | `inbox` | 要 | `default` | 09 §6 |
| POST | `/api/v1/group-invites/{inviteId}/accept` | `GroupInviteController` | `accept` | 要 | `default` | 09 §6 |
| POST | `/api/v1/group-invites/{inviteId}/decline` | `GroupInviteController` | `decline` | 要 | `default` | 09 §6 |
| GET | `/api/v1/party/friend-beacons` | `PartyController` | `friendBeacons` | 要 | `default` | 08 §2 |
| POST | `/api/v1/party/presence` | `PartyController` | `presence` | 要 | `presence` ＋ `default` | 08 §2 |
| GET | `/api/v1/party/current` | `PartyController` | `current` | 要 | `default` | 08 §6 |
| POST | `/api/v1/parties/{id}/leave` | `PartyController` | `leave` | 要 | `default` | 08 §4.2 |
| POST | `/api/v1/parties/{id}/lock` | `PartyController` | `lock` | 要 | `default` | 08 §4.4 |
| POST | `/api/v1/parties/{id}/unlock` | `PartyController` | `unlock` | 要 | `default` | 08 §4.4 |
| PATCH | `/api/v1/parties/{id}` | `PartyController` | `rename` | 要 | `default` | 08 §6 |
| GET | `/api/v1/memories` | `MemoryController` | `list` | 要 | `default` | 13 §2 |
| DELETE | `/api/v1/memories/{spaceId}` | `MemoryController` | `hide` | 要 | `default` | 13 §2 |
| DELETE | `/api/v1/me` | `AccountController` | `delete` | 要 | `default` | 13 §5 |
