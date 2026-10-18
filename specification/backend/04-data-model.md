# 04 データモデル

## 1. 方針

- SQLite は永続化が必要なものだけを持つ。位置・BLE の観測・近接の連続時間といった揮発データは Redis に置き、TTL で消す
- **位置の履歴は保存しない**。Redis に最新の 1 点だけを持つ
- 時刻はすべて unix ミリ秒の INTEGER。真偽値は INTEGER の 0 / 1
- 2 人の組の行は `user_a < user_b`（文字列比較）に正規化し、`CHECK` で強制する
- 削除は、ユーザーが復元を期待するもの（投稿・メディア）だけ論理削除（`deleted_at`）。それ以外は物理削除

## 2. SQLite スキーマ

以下を `db/schema.sql` とする。migration の履歴は持たず、変更のたびに DB を作り直す（§5）。

### 2.1 ユーザー・認証

```sql
CREATE TABLE users (
  did         TEXT PRIMARY KEY,
  handle      TEXT NOT NULL,
  pds_url     TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  deleted_at  INTEGER                       -- 削除済みは Jetstream のイベントも無視する
);
CREATE INDEX users_handle ON users(handle);

CREATE TABLE profiles (
  did           TEXT PRIMARY KEY REFERENCES users(did) ON DELETE CASCADE,
  display_name  TEXT,
  description   TEXT,
  avatar_cid    TEXT,
  banner_cid    TEXT,
  status        TEXT,                       -- jp.i32.chain.actor.profile.status
  status_mod    TEXT NOT NULL DEFAULT 'ok', -- 11
  updated_at    INTEGER NOT NULL
);

CREATE TABLE user_settings (
  did                  TEXT PRIMARY KEY REFERENCES users(did) ON DELETE CASCADE,
  encounter_mode       TEXT NOT NULL DEFAULT 'delayed' CHECK (encounter_mode IN ('immediate','delayed','private')),
  encounter_delay_sec  INTEGER NOT NULL DEFAULT 3600,     -- 06 §5。mode=delayed のときのみ意味を持つ
  friend_beacon_share  INTEGER NOT NULL DEFAULT 1,        -- 08 §2
  party_auto           INTEGER NOT NULL DEFAULT 1,        -- 08 §2
  location_paused      INTEGER NOT NULL DEFAULT 0,        -- 07 §4
  warn_display         TEXT NOT NULL DEFAULT 'blur' CHECK (warn_display IN ('show','blur','hide')),
  natural_groups       INTEGER NOT NULL DEFAULT 1,        -- 09 §8。0 なら自動グループの照合に使わない
  record_encounter_place INTEGER NOT NULL DEFAULT 0,      -- 06 §7。1 のときだけすれ違い場所（geohash6）を残す
  notify_json          TEXT NOT NULL DEFAULT '{}',        -- 12 §3。種別ごとの on/off
  content_scan_consent_version INTEGER,                    -- 02 §8。NULL なら未同意
  content_scan_consent_at      INTEGER,
  updated_at           INTEGER NOT NULL
);

CREATE TABLE atproto_oauth_sessions (
  id          TEXT PRIMARY KEY,             -- indigo の SessionID（OAuth の state を流用）
  did         TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  data_enc    BLOB NOT NULL,                -- indigo oauth.ClientSessionData の JSON を AES-256-GCM で暗号化（トークン・DPoP 鍵・nonce を含む）
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,
  revoked_at  INTEGER                       -- 取り消したら data_enc を空にして時刻を入れる
);
CREATE INDEX atproto_oauth_sessions_did ON atproto_oauth_sessions(did);

CREATE TABLE app_sessions (
  id                TEXT PRIMARY KEY,         -- JWT の sid
  did               TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  atproto_session   TEXT NOT NULL REFERENCES atproto_oauth_sessions(id),
  family_id         TEXT NOT NULL,            -- ローテーションの系列。再利用検知で一括失効
  refresh_hash      TEXT NOT NULL UNIQUE,     -- SHA-256 hex
  device_name       TEXT,
  created_at        INTEGER NOT NULL,
  last_used_at      INTEGER NOT NULL,
  expires_at        INTEGER NOT NULL,
  revoked_at        INTEGER
);
CREATE INDEX app_sessions_did ON app_sessions(did);
CREATE INDEX app_sessions_family ON app_sessions(family_id);

CREATE TABLE devices (
  id          TEXT PRIMARY KEY,
  did         TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  session_id  TEXT REFERENCES app_sessions(id) ON DELETE CASCADE,
  fcm_token   TEXT NOT NULL UNIQUE,
  platform    TEXT NOT NULL DEFAULT 'android',
  updated_at  INTEGER NOT NULL
);
CREATE INDEX devices_did ON devices(did);
```

### 2.2 近接・すれ違い

```sql
CREATE TABLE beacon_tokens (
  token_hash    BLOB PRIMARY KEY,             -- SHA-256(token) の 32 バイト。逆引き用
  token_enc     BLOB NOT NULL,                -- AES-256-GCM。再取得とフレンド配布用（08 §2）
  did           TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  window_start  INTEGER NOT NULL              -- unix 秒。900 の倍数（05 §2）
) WITHOUT ROWID;
CREATE UNIQUE INDEX beacon_tokens_did_window ON beacon_tokens(did, window_start);

CREATE TABLE encounters (
  id              TEXT PRIMARY KEY,
  user_a          TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  user_b          TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  established_at  INTEGER NOT NULL,           -- 双方向が初めて揃った時刻
  first_seen_at   INTEGER NOT NULL,
  last_seen_at    INTEGER NOT NULL,
  max_rssi        INTEGER,
  visibility      TEXT NOT NULL CHECK (visibility IN ('pending','revealed','hidden')),
  reveal_at       INTEGER,                    -- visibility=hidden なら NULL
  geohash6        TEXT,                       -- 06 §7。双方が場所記録を許可した場合のみ
  CHECK (user_a < user_b)
);
CREATE INDEX encounters_pair ON encounters(user_a, user_b, last_seen_at);
CREATE INDEX encounters_a ON encounters(user_a, established_at);
CREATE INDEX encounters_b ON encounters(user_b, established_at);
CREATE INDEX encounters_reveal ON encounters(visibility, reveal_at);
```

### 2.3 フレンド・位置共有

```sql
CREATE TABLE friendships (
  user_a      TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  user_b      TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL,
  via         TEXT NOT NULL CHECK (via IN ('encounter','invite','space')),
  PRIMARY KEY (user_a, user_b),
  CHECK (user_a < user_b)
) WITHOUT ROWID;
CREATE INDEX friendships_b ON friendships(user_b);

CREATE TABLE friend_requests (
  id          TEXT PRIMARY KEY,
  from_did    TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  to_did      TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  via         TEXT NOT NULL CHECK (via IN ('encounter','space')),
  via_ref     TEXT,                           -- encounters.id または spaces.id
  status      TEXT NOT NULL CHECK (status IN ('pending','accepted','declined','canceled')),
  created_at  INTEGER NOT NULL,
  decided_at  INTEGER
);
CREATE UNIQUE INDEX friend_requests_pending ON friend_requests(from_did, to_did) WHERE status = 'pending';
CREATE INDEX friend_requests_to ON friend_requests(to_did, status);

CREATE TABLE friend_invites (
  code_hash   TEXT PRIMARY KEY,               -- SHA-256 hex
  did         TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL,
  expires_at  INTEGER NOT NULL,
  used_by     TEXT,
  used_at     INTEGER
);

CREATE TABLE location_shares (
  owner       TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,   -- 位置を見せる側
  viewer      TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  precision   TEXT NOT NULL CHECK (precision IN ('exact','approx','area','direction','off')),
  areas_only  INTEGER NOT NULL DEFAULT 0,
  updated_at  INTEGER NOT NULL,
  PRIMARY KEY (owner, viewer)
) WITHOUT ROWID;

CREATE TABLE areas (
  id          TEXT PRIMARY KEY,
  did         TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  label       TEXT NOT NULL,
  lat         REAL NOT NULL,
  lon         REAL NOT NULL,
  radius_m    INTEGER NOT NULL CHECK (radius_m BETWEEN 50 AND 5000),
  created_at  INTEGER NOT NULL
);
CREATE INDEX areas_did ON areas(did);
```

### 2.4 Space（グループ / Party）と record

Spaces と同形にする（03 §7）。グループと Party の共通部分を `spaces` に、固有部分を `groups` / `parties` に分ける。

```sql
CREATE TABLE spaces (
  id            TEXT PRIMARY KEY,             -- skey（UUIDv7）
  type          TEXT NOT NULL CHECK (type IN ('jp.i32.chain.space.group','jp.i32.chain.space.party')),
  authority     TEXT NOT NULL,                -- did:web:{API_HOST}
  ref           TEXT NOT NULL UNIQUE,         -- at://{authority}/space/{type}/{id}
  read_policy   TEXT NOT NULL DEFAULT 'managing-app',
  write_policy  TEXT NOT NULL DEFAULT 'managing-app',
  name          TEXT NOT NULL,
  state         TEXT NOT NULL CHECK (state IN ('active','archived','deleted')),
  created_by    TEXT NOT NULL REFERENCES users(did),
  created_at    INTEGER NOT NULL,
  archived_at   INTEGER
);
CREATE INDEX spaces_state ON spaces(type, state);

CREATE TABLE space_members (
  space_id    TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  did         TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  can_read    INTEGER NOT NULL DEFAULT 1,     -- com.atproto.simplespace の read
  can_write   INTEGER NOT NULL DEFAULT 1,     -- 同 write
  role        TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','member')),
  joined_at   INTEGER NOT NULL,
  left_at     INTEGER,
  hidden_from_memory INTEGER NOT NULL DEFAULT 0,  -- 13 §2
  PRIMARY KEY (space_id, did)
) WITHOUT ROWID;
CREATE INDEX space_members_did ON space_members(did, left_at);

CREATE TABLE groups (
  space_id         TEXT PRIMARY KEY REFERENCES spaces(id) ON DELETE CASCADE,
  description      TEXT NOT NULL DEFAULT '',
  visibility       TEXT NOT NULL CHECK (visibility IN ('open','closed')),
  origin           TEXT NOT NULL CHECK (origin IN ('explicit','natural')),
  persistent       INTEGER NOT NULL DEFAULT 0,
  center_lat       REAL NOT NULL,
  center_lon       REAL NOT NULL,
  radius_m         INTEGER NOT NULL CHECK (radius_m BETWEEN 5 AND 2000),
  schedule_json    TEXT,                      -- 09 §3。NULL なら常時有効
  invite_quorum    INTEGER NOT NULL DEFAULT 1,
  parent_space_id  TEXT REFERENCES spaces(id) ON DELETE SET NULL,
  mod_label        TEXT NOT NULL DEFAULT 'ok',
  icon_media_id    TEXT REFERENCES media(id) ON DELETE SET NULL   -- 09 §2。作成時に添付した画像
);

CREATE TABLE group_invites (
  id          TEXT PRIMARY KEY,
  space_id    TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  target_did  TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  created_by  TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  status      TEXT NOT NULL CHECK (status IN ('collecting','offered','accepted','declined','expired')),
  created_at  INTEGER NOT NULL,
  expires_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX group_invites_open ON group_invites(space_id, target_did) WHERE status IN ('collecting','offered');

CREATE TABLE group_invite_approvals (
  invite_id   TEXT NOT NULL REFERENCES group_invites(id) ON DELETE CASCADE,
  did         TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (invite_id, did)
) WITHOUT ROWID;

CREATE TABLE group_votes (                    -- P2
  id          TEXT PRIMARY KEY,
  space_id    TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL CHECK (kind IN ('make_closed','make_persistent')),
  created_by  TEXT NOT NULL REFERENCES users(did),
  status      TEXT NOT NULL CHECK (status IN ('open','passed','failed')),
  created_at  INTEGER NOT NULL,
  closes_at   INTEGER NOT NULL
);
CREATE TABLE group_vote_ballots (
  vote_id     TEXT NOT NULL REFERENCES group_votes(id) ON DELETE CASCADE,
  did         TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  yes         INTEGER NOT NULL,
  PRIMARY KEY (vote_id, did)
) WITHOUT ROWID;

CREATE TABLE parties (
  space_id      TEXT PRIMARY KEY REFERENCES spaces(id) ON DELETE CASCADE,
  phase         TEXT NOT NULL CHECK (phase IN ('tentative','confirmed','ended')),
  locked        INTEGER NOT NULL DEFAULT 0,
  tentative_at  INTEGER NOT NULL,
  confirmed_at  INTEGER,
  ended_at      INTEGER,
  last_activity_at INTEGER NOT NULL           -- 在席または投稿の最終時刻
);
CREATE INDEX parties_phase ON parties(phase);

CREATE TABLE records (
  uri          TEXT PRIMARY KEY,              -- at://{authority}/space/{type}/{skey}/{author}/{collection}/{rkey}
  space_id     TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  author_did   TEXT NOT NULL,                 -- pin は authority の DID
  collection   TEXT NOT NULL,
  rkey         TEXT NOT NULL,                 -- TID
  value_json   TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  deleted_at   INTEGER,
  deleted_by   TEXT,
  mod_label    TEXT NOT NULL DEFAULT 'ok' CHECK (mod_label IN ('ok','warn','block')),
  mod_state    TEXT NOT NULL DEFAULT 'done' CHECK (mod_state IN ('pending','done'))
);
CREATE INDEX records_space ON records(space_id, collection, created_at);
CREATE INDEX records_author ON records(author_did, created_at);

CREATE TABLE media (
  id          TEXT PRIMARY KEY,
  owner_did   TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  space_id    TEXT REFERENCES spaces(id) ON DELETE CASCADE,  -- 未添付なら NULL
  mime        TEXT NOT NULL,                  -- 再エンコード後は常に image/jpeg
  width       INTEGER NOT NULL,
  height      INTEGER NOT NULL,
  bytes       INTEGER NOT NULL,
  sha256      TEXT NOT NULL,
  taken_at    INTEGER,                        -- クライアント申告の撮影時刻
  created_at  INTEGER NOT NULL,
  deleted_at  INTEGER
);
CREATE INDEX media_space ON media(space_id, created_at);
CREATE INDEX media_unattached ON media(created_at) WHERE space_id IS NULL;
```

### 2.5 公開投稿・その他

```sql
CREATE TABLE public_posts (
  uri         TEXT PRIMARY KEY,               -- at://{did}/jp.i32.chain.feed.post/{rkey}
  cid         TEXT NOT NULL,
  author_did  TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  text        TEXT NOT NULL,
  langs       TEXT,                           -- JSON 配列
  bsky_uri    TEXT,                           -- 17 §2.3。Bluesky にも投稿したときの app.bsky.feed.post
  bsky_cid    TEXT,
  created_at  INTEGER NOT NULL,
  indexed_at  INTEGER NOT NULL,
  deleted_at  INTEGER,
  mod_label   TEXT NOT NULL DEFAULT 'ok',
  mod_state   TEXT NOT NULL DEFAULT 'pending'
);
CREATE INDEX public_posts_author ON public_posts(author_did, created_at);

CREATE TABLE schedule_entries (               -- 09 §8.2。今日と明日の分だけを持つ
  id           TEXT PRIMARY KEY,
  did          TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  date         TEXT NOT NULL,                  -- Asia/Tokyo の YYYY-MM-DD
  source       TEXT NOT NULL CHECK (source IN ('calendar','manual')),  -- 09 §8.2。同じ source だけを置き換える
  start_min    INTEGER NOT NULL,               -- 0:00 からの分
  end_min      INTEGER NOT NULL,
  title        TEXT NOT NULL,                  -- 平文（README L8）
  embedding    BLOB,                           -- float32 リトルエンディアン。未計算なら NULL
  embed_model  TEXT,
  created_at   INTEGER NOT NULL,
  CHECK (start_min < end_min)
);
CREATE INDEX schedule_entries_did_date ON schedule_entries(did, date, source);
CREATE INDEX schedule_entries_date ON schedule_entries(date, start_min);

CREATE TABLE jetstream_cursor (
  id         INTEGER PRIMARY KEY CHECK (id = 1),
  cursor_us  INTEGER NOT NULL
);

CREATE TABLE blocks (
  blocker     TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  blocked     TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  created_at  INTEGER NOT NULL,
  PRIMARY KEY (blocker, blocked)
) WITHOUT ROWID;
CREATE INDEX blocks_blocked ON blocks(blocked);

CREATE TABLE notifications (
  id          TEXT PRIMARY KEY,
  did         TEXT NOT NULL REFERENCES users(did) ON DELETE CASCADE,
  type        TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at  INTEGER NOT NULL,
  read_at     INTEGER
);
CREATE INDEX notifications_did ON notifications(did, created_at);

CREATE TABLE notification_outbox (
  id               TEXT PRIMARY KEY,
  notification_id  TEXT NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
  status           TEXT NOT NULL CHECK (status IN ('queued','sent','failed')),
  attempts         INTEGER NOT NULL DEFAULT 0,
  next_attempt_at  INTEGER NOT NULL
);
CREATE INDEX notification_outbox_due ON notification_outbox(status, next_attempt_at);

CREATE TABLE audit_log (
  id          TEXT PRIMARY KEY,
  actor_did   TEXT,                           -- アカウント削除後は NULL にする
  action      TEXT NOT NULL,                  -- moderation.block / block.create / account.delete など
  target      TEXT,
  detail_json TEXT,
  created_at  INTEGER NOT NULL
);
```

## 3. Redis キー

`{did}` などはそのまま埋め込む。DID に含まれる `:` はキーの区切りと衝突するが、各キーで DID の位置が固定されるので分解はしない。

| キー | 型 | TTL | 内容 | 仕様 |
| --- | --- | --- | --- | --- |
| `oauth:req:{state}` | string（暗号化した JSON） | 10 分 | indigo `oauth.AuthRequestData`（PKCE verifier、DPoP 鍵、要求した DID） | 03 §2 |
| `otc:{code}` | string | 60 秒 | one-time code → app セッション作成に必要な DID と ATProto セッション ID | 03 §2 |
| `sess:{sid}` | string | 15 分 | アプリセッションの失効確認キャッシュ | 03 §3 |
| `lock:{name}` | string | 用途ごと | `SET NX PX` による排他 | 各所 |
| `bt:{hex(token_hash)}` | string | 窓終了 + 1 時間 | `{did}|{window_start}` | 05 §2 |
| `loc:{did}` | hash | 30 分 | `lat`, `lon`, `acc`, `ts`, `src` | 05 §4 |
| `geo:users` | geo | なし（janitor が掃除） | 位置共有・グループ判定用の最新位置 | 05 §4 |
| `geo:groups` | geo | なし | active なグループの中心 | 09 §5 |
| `sgt:{a}:{b}:{bucket}` | string | 26 時間 | 観測者 a が b を見た 10 分バケットごとの `first|last|maxRssi` | 06 §3 |
| `ble:recent:{a}` | zset | 30 分 | a が直近に見た DID と最終観測時刻（score） | 05 §3 |
| `pe:{a}:{b}` | hash | 30 分 | フレンド近接の連続性 `since`, `last` | 08 §3 |
| `pe:active` | zset | なし（score で掃除） | `{a}|{b}` → 最終観測時刻 | 08 §3 |
| `party:of:{did}` | string | なし | 所属中の party の space ID。active な party の**未離脱**のメンバーのときだけ持つ（離脱・退出で消し、再合流で戻す） | 08 §4 |
| `pp:{space}` | hash | 8 日（party 終了時に削除） | メンバーごとの最終在席時刻。不在による離脱の判定に使う | 08 §4.2 |
| `pleft:{space}` | set | 8 日（party 終了時に削除） | 自分から退出したメンバー。その party が終わるまで自動で再合流させない | 08 §4.2 |
| `acc:{space}:{did}:{mode}` | string | 最大 10 分 | アクセス判定結果（`1` / `0`） | 10 §5 |
| `gn:{did}:{space}` | string | 24 時間 | グループ入場通知の送信済み印 | 09 §6.1 |
| `ng:cand:{geohash7}:{slot}` | string（JSON） | 30 分 | 自動グループの候補と、続けて見つかった回数 | 09 §8.4 |
| `rl:{bucket}:{did}:{window}` | string | 窓長 | レート制限カウンタ | 02 §5 |
| `idem:{did}:{key}` | string | 24 時間 | 冪等キーと最初のレスポンス | 02 §1 |
| `modq` | list | なし | moderation 再判定待ち | 11 §3 |
| `stats:{did}` | string(JSON) | 5 分 | 統計のキャッシュ | 13 §3 |

## 4. 保持期間と掃除（`cleanup.Janitor`）

| 対象 | 保持 | 方法 |
| --- | --- | --- |
| `beacon_tokens` | `window_start` から 48 時間 | 物理削除 |
| BLE 観測（`sgt:*`, `ble:recent:*`） | 26 時間 / 30 分 | Redis TTL |
| 位置（`loc:*`） | 30 分 | Redis TTL。`geo:users` からは対応する `loc:*` が消えたメンバーを janitor が `ZREM` する |
| `encounters` | アカウントがある限り | アカウント削除時に物理削除 |
| `friend_invites` | 期限から 1 日 | 物理削除 |
| `app_sessions` | 失効・期限切れから 7 日 | 物理削除 |
| 未添付の `media` | 作成から 24 時間 | ファイルと行を削除 |
| 論理削除した `records` / `media` | 30 日 | 本文・ファイルを物理削除 |
| `notifications` | 90 日 | 物理削除 |
| `schedule_entries` | `date` が昨日以前になったら | 物理削除 |
| `group_invites` | 期限切れで `expired` に更新し、30 日後に削除 | |

## 5. 作り直しと seed

開発中は migration を使わず、毎回 DB を作り直す。

| コマンド | 内容 |
| --- | --- |
| `make db-reset` | `CHAIN_DB_PATH` のファイル（`-wal`、`-shm` を含む）を消し、`schema.sql` と `seed.sql` を流す。Redis は `FLUSHDB` する（位置・近接・セッションの揮発データが SQLite と食い違わないように） |
| `make db-schema` | seed を入れずにスキーマだけ作る（テスト用） |

- `CHAIN_ENV=prod` では `db-reset` を拒否する
- `seed.sql` の内容: テスト用ユーザー 4 人（DID は `did:web:seed-{n}.chain.i32.jp` の形の架空の値）、そのフレンド関係、open グループ 2 つ（包含関係あり）、archived の Party 1 つと投稿数件
- seed のユーザーは ATProto のアカウントを持たないので、OAuth ログインはできない。開発用のログイン手段は開発用 API と一緒に後で決める（README「後でやること」）
- スキーマの変更は `schema.sql` を直接書き換える。本番運用を始める前に migration 方式に切り替える
