-- specification/backend/04-data-model.md §2 から生成した DDL。
-- 変更するときは仕様書とこのファイルの両方を直す。

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
  mod_label        TEXT NOT NULL DEFAULT 'ok'
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
  reply_root   TEXT GENERATED ALWAYS AS (json_extract(value_json, '$.reply.root')) STORED,
  created_at   INTEGER NOT NULL,
  deleted_at   INTEGER,
  deleted_by   TEXT,
  mod_label    TEXT NOT NULL DEFAULT 'ok' CHECK (mod_label IN ('ok','warn','block')),
  mod_state    TEXT NOT NULL DEFAULT 'done' CHECK (mod_state IN ('pending','done'))
);
CREATE INDEX records_space ON records(space_id, collection, created_at);
CREATE INDEX records_reply_root ON records(space_id, reply_root, created_at);
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
  mod_state   TEXT NOT NULL DEFAULT 'pending',
  audience    TEXT NOT NULL DEFAULT 'public' CHECK (audience IN ('public','nearby')),  -- 17 §2。nearby は今近くにいる人だけに配信する限定投稿
  expires_at  INTEGER                         -- nearby の失効時刻（unix ms）。NULL なら失効しない
);
CREATE INDEX public_posts_author ON public_posts(author_did, created_at);
CREATE INDEX public_posts_audience ON public_posts(audience, created_at);

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
