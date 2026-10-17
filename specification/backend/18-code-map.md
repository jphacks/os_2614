# 18 コードの地図

機能とコードのパス、コントローラーとそのパスの一覧。
**このファイルは `backend/scripts/codemap.py` が生成する。手で直さず、コードを変えたら作り直す**（`backend/` で `make codemap`）。

## 1. 機能とコードのパス

パスは `backend/` からの相対。テスト（`*_test.go`）と `doc.go` は省く。

| 機能 | 仕様 | controller | service | model | worker | external | realtime | storage |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 認証・アプリセッション | 03 §2・§3 | `internal/controller/account.go`<br>`internal/controller/auth.go` | `internal/service/account.go`<br>`internal/service/auth.go`<br>`internal/service/session.go` | `internal/model/account.go`<br>`internal/model/oauth.go`<br>`internal/model/session.go` | — | `internal/external/atproto/oauth.go`<br>`internal/external/atproto/store.go` | — | — |
| 自分（設定・同意） | 13 §1、02 §8 | `internal/controller/me.go` | `internal/service/me.go` | `internal/model/settings.go` | — | — | — | — |
| プロフィール | 13 §1 | `internal/controller/user.go` | `internal/service/profile_image.go`<br>`internal/service/user.go` | `internal/model/profile.go`<br>`internal/model/user.go`<br>`internal/model/user_handle.go` | — | `internal/external/atproto/profile.go` | — | — |
| 近接判定の共通部分 | 05 | `internal/controller/beacon.go` | `internal/service/beacon.go`<br>`internal/service/beacon_payload.go`<br>`internal/service/geohash.go`<br>`internal/service/params.go`<br>`internal/service/proximity.go` | `internal/model/beacon.go`<br>`internal/model/proximity.go` | — | — | — | — |
| 位置 | 05 §4 | `internal/controller/location.go` | `internal/service/location.go` | `internal/model/location.go` | — | — | — | — |
| すれ違い | 06 | `internal/controller/encounter.go` | `internal/service/encounter.go`<br>`internal/service/encounter_summary.go` | `internal/model/encounter.go`<br>`internal/model/encounter_feed.go` | `internal/worker/encounter_revealer.go` | — | — | — |
| ブロック | 13 §4 | `internal/controller/block.go` | `internal/service/block.go` | `internal/model/block.go`<br>`internal/model/relationship.go` | — | — | — | — |
| フレンド | 07 §1〜§3 | `internal/controller/friend.go` | `internal/service/friend.go` | `internal/model/friend.go` | — | — | — | — |
| 位置共有 | 07 §4 | `internal/controller/location_share.go` | `internal/service/location_share.go` | `internal/model/location_share.go` | — | — | — | — |
| Party | 08 | `internal/controller/memory.go`<br>`internal/controller/party.go` | `internal/service/party.go` | `internal/model/memory.go`<br>`internal/model/party.go` | `internal/worker/party_engine.go` | — | — | — |
| グループ | 09 §1〜§5・§9 | `internal/controller/group.go` | `internal/service/group.go`<br>`internal/service/group_circle.go`<br>`internal/service/schedule.go` | `internal/model/group.go` | — | — | — | — |
| closed グループの招待 | 09 §6 | `internal/controller/group_invite.go` | `internal/service/group_invite.go` | `internal/model/group_invite.go` | — | — | — | — |
| 入場通知・グラフ | 09 §6.1・§7 | — | `internal/service/group_entry.go`<br>`internal/service/group_graph.go` | — | — | — | — | — |
| 自動グループ | 09 §8 | `internal/controller/schedule.go` | `internal/service/embedder.go`<br>`internal/service/natural_cluster.go`<br>`internal/service/natural_groups.go`<br>`internal/service/schedule_entries.go` | `internal/model/schedule.go`<br>`internal/model/schedule_match.go` | `internal/worker/embed_backfill.go`<br>`internal/worker/natural_groups.go` | `internal/external/embed/client.go` | — | — |
| 投票 | 09 §8.6 | `internal/controller/group_vote.go` | `internal/service/group_vote.go` | `internal/model/group_vote.go` | `internal/worker/group_votes.go` | — | — | — |
| instance（Space の投稿） | 10 §1〜§5 | `internal/controller/instance.go` | `internal/service/access.go`<br>`internal/service/instance.go` | `internal/model/instance.go`<br>`internal/model/record.go`<br>`internal/model/space.go` | — | — | — | — |
| Pin | 10 §6 | — | `internal/service/pin.go` | `internal/model/pin.go` | — | — | — | — |
| メディア | 10 §4.4 | `internal/controller/media.go`<br>`internal/controller/media_serve.go` | `internal/service/media.go` | `internal/model/media.go` | — | — | — | `internal/storage/media.go`<br>`internal/storage/sign.go` |
| WebSocket | 10 §7 | `internal/controller/ws.go` | `internal/service/realtime.go` | — | — | — | `internal/realtime/hub.go` | — |
| 公開投稿・Jetstream | 17 §2、03 §4・§5 | `internal/controller/public_post.go` | `internal/service/jetstream.go`<br>`internal/service/public_post.go` | `internal/model/jetstream.go`<br>`internal/model/public_post.go` | `internal/worker/jetstream.go` | `internal/external/atproto/firstload.go`<br>`internal/external/atproto/jetstream.go`<br>`internal/external/atproto/writer.go` | — | — |
| フィード | 17 §3 | `internal/controller/feed.go` | `internal/service/feed.go` | — | — | — | — | — |
| moderation | 11 | — | `internal/service/moderation.go`<br>`internal/service/moderation_jev.go`<br>`internal/service/moderation_prompts.go`<br>`internal/service/moderation_queue.go` | `internal/model/modq.go` | `internal/worker/moderation_requeue.go` | `internal/external/nanogpt/client.go` | — | — |
| 通知 | 12 | `internal/controller/device.go`<br>`internal/controller/notification.go` | `internal/service/device.go`<br>`internal/service/digest.go`<br>`internal/service/notification.go`<br>`internal/service/notify_sender.go` | `internal/model/device.go`<br>`internal/model/notification.go` | `internal/worker/digest.go`<br>`internal/worker/notify_sender.go` | `internal/external/fcm/client.go`<br>`internal/external/fcm/credentials.go`<br>`internal/external/fcm/wire.go` | — | — |
| 統計 | 13 §3 | `internal/controller/stats.go` | `internal/service/stats.go` | `internal/model/stats.go` | — | — | — | — |
| 掃除・排他 | 04 §4 | — | `internal/service/janitor.go` | `internal/model/audit.go`<br>`internal/model/janitor.go`<br>`internal/model/lock.go` | `internal/worker/janitor.go` | — | — | — |
| ヘルスチェック | — | `internal/controller/health.go` | `internal/service/health.go` | — | — | — | — | — |

未分類のファイルはない。

共通の部品（機能に属さないもの）: `internal/controller/routes.go`（ルートの登録）、`internal/httpx/`（エラー・認証・レート制限・Bind）、`internal/auth/`（JWT・暗号化）、`internal/db/`、`internal/kv/`、`internal/config/`、`internal/clock/`、`internal/idgen/`、`internal/utils/`、`internal/testutil/`、`cmd/dbreset/`、`cmd/keygen/`

## 2. コントローラー一覧

| コントローラー | ファイル | 登録先 | パスの数 |
| --- | --- | --- | --- |
| `HealthController` | `internal/controller/health.go` | ルート（`/api/v1` の外、認証なし） | 1 |
| `OAuthController` | `internal/controller/auth.go` | ルート（`/api/v1` の外、認証なし） | 5 |
| `MediaDeliveryController` | `internal/controller/media_serve.go` | ルート（`/api/v1` の外、認証なし） | 1 |
| `AuthController` | `internal/controller/auth.go` | `/api/v1`（認証なし） | 2 |
| `WSController` | `internal/controller/ws.go` | `/api/v1`（認証なし） | 1 |
| `LogoutController` | `internal/controller/auth.go` | `/api/v1`（認証あり） | 1 |
| `MeController` | `internal/controller/me.go` | `/api/v1`（認証あり） | 9 |
| `ScheduleController` | `internal/controller/schedule.go` | `/api/v1`（認証あり） | 3 |
| `StatsController` | `internal/controller/stats.go` | `/api/v1`（認証あり） | 1 |
| `UserController` | `internal/controller/user.go` | `/api/v1`（認証あり） | 1 |
| `DeviceController` | `internal/controller/device.go` | `/api/v1`（認証あり） | 2 |
| `BeaconController` | `internal/controller/beacon.go` | `/api/v1`（認証あり） | 1 |
| `LocationController` | `internal/controller/location.go` | `/api/v1`（認証あり） | 1 |
| `LocationShareController` | `internal/controller/location_share.go` | `/api/v1`（認証あり） | 7 |
| `EncounterController` | `internal/controller/encounter.go` | `/api/v1`（認証あり） | 3 |
| `BlockController` | `internal/controller/block.go` | `/api/v1`（認証あり） | 3 |
| `FriendController` | `internal/controller/friend.go` | `/api/v1`（認証あり） | 9 |
| `NotificationController` | `internal/controller/notification.go` | `/api/v1`（認証あり） | 2 |
| `InstanceController` | `internal/controller/instance.go` | `/api/v1`（認証あり） | 7 |
| `FeedPostController` | `internal/controller/public_post.go` | `/api/v1`（認証あり） | 2 |
| `FeedController` | `internal/controller/feed.go` | `/api/v1`（認証あり） | 1 |
| `MediaController` | `internal/controller/media.go` | `/api/v1`（認証あり） | 2 |
| `GroupController` | `internal/controller/group.go` | `/api/v1`（認証あり） | 8 |
| `GroupVoteController` | `internal/controller/group_vote.go` | `/api/v1`（認証あり） | 3 |
| `GroupInviteController` | `internal/controller/group_invite.go` | `/api/v1`（認証あり） | 7 |
| `PartyController` | `internal/controller/party.go` | `/api/v1`（認証あり） | 7 |
| `MemoryController` | `internal/controller/memory.go` | `/api/v1`（認証あり） | 2 |
| `AccountController` | `internal/controller/account.go` | `/api/v1`（認証あり） | 1 |

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
