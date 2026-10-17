-- 開発用の seed（specification/backend/04-data-model.md §5）。
-- ユーザーは ATProto のアカウントを持たない架空の DID なので、OAuth ではログインできない。
-- 時刻は 2026-10-14T00:00:00Z（1792022400000）を基準にした固定値。

INSERT INTO users (did, handle, pds_url, created_at) VALUES
  ('did:web:seed-1.chain.i32.jp', 'seed1.chain.i32.jp', 'https://seed.invalid', 1792022400000),
  ('did:web:seed-2.chain.i32.jp', 'seed2.chain.i32.jp', 'https://seed.invalid', 1792022400000),
  ('did:web:seed-3.chain.i32.jp', 'seed3.chain.i32.jp', 'https://seed.invalid', 1792022400000),
  ('did:web:seed-4.chain.i32.jp', 'seed4.chain.i32.jp', 'https://seed.invalid', 1792022400000);

INSERT INTO profiles (did, display_name, description, updated_at) VALUES
  ('did:web:seed-1.chain.i32.jp', 'Seed 1', 'seed ユーザー 1', 1792022400000),
  ('did:web:seed-2.chain.i32.jp', 'Seed 2', 'seed ユーザー 2', 1792022400000),
  ('did:web:seed-3.chain.i32.jp', 'Seed 3', 'seed ユーザー 3', 1792022400000),
  ('did:web:seed-4.chain.i32.jp', 'Seed 4', 'seed ユーザー 4（誰ともフレンドでない）', 1792022400000);

INSERT INTO user_settings (did, encounter_mode, encounter_delay_sec, content_scan_consent_version, content_scan_consent_at, updated_at) VALUES
  ('did:web:seed-1.chain.i32.jp', 'immediate', 0,    1, 1792022400000, 1792022400000),
  ('did:web:seed-2.chain.i32.jp', 'delayed',   3600, 1, 1792022400000, 1792022400000),
  ('did:web:seed-3.chain.i32.jp', 'delayed',   3600, 1, 1792022400000, 1792022400000),
  ('did:web:seed-4.chain.i32.jp', 'private',   3600, NULL, NULL,       1792022400000);

-- 1-2、2-3 がフレンド。3 は 1 のフレンドのフレンド（08 の Party で使う）。
INSERT INTO friendships (user_a, user_b, created_at, via) VALUES
  ('did:web:seed-1.chain.i32.jp', 'did:web:seed-2.chain.i32.jp', 1792022400000, 'invite'),
  ('did:web:seed-2.chain.i32.jp', 'did:web:seed-3.chain.i32.jp', 1792022400000, 'invite');

-- open グループ 2 つ。演習室は大学に含まれる（parent_space_id）。
INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at) VALUES
  ('01900000-0000-7000-8000-000000000001', 'jp.i32.chain.space.group', 'did:web:chain.i32.jp',
   'at://did:web:chain.i32.jp/space/jp.i32.chain.space.group/01900000-0000-7000-8000-000000000001',
   '大学（seed）', 'active', 'did:web:seed-1.chain.i32.jp', 1792022400000),
  ('01900000-0000-7000-8000-000000000002', 'jp.i32.chain.space.group', 'did:web:chain.i32.jp',
   'at://did:web:chain.i32.jp/space/jp.i32.chain.space.group/01900000-0000-7000-8000-000000000002',
   '演習室（seed）', 'active', 'did:web:seed-2.chain.i32.jp', 1792022400000);

INSERT INTO groups (space_id, description, visibility, origin, persistent, center_lat, center_lon, radius_m, parent_space_id) VALUES
  ('01900000-0000-7000-8000-000000000001', 'seed の大きいグループ', 'open', 'explicit', 1, 35.681236, 139.767125, 800, NULL),
  ('01900000-0000-7000-8000-000000000002', 'seed の小さいグループ', 'open', 'explicit', 0, 35.681500, 139.767300, 40,
   '01900000-0000-7000-8000-000000000001');

INSERT INTO space_members (space_id, did, role, joined_at) VALUES
  ('01900000-0000-7000-8000-000000000001', 'did:web:seed-1.chain.i32.jp', 'owner',  1792022400000),
  ('01900000-0000-7000-8000-000000000001', 'did:web:seed-2.chain.i32.jp', 'member', 1792022400000),
  ('01900000-0000-7000-8000-000000000001', 'did:web:seed-3.chain.i32.jp', 'member', 1792022400000),
  ('01900000-0000-7000-8000-000000000002', 'did:web:seed-2.chain.i32.jp', 'owner',  1792022400000);

-- archived の Party（1・2・3）と投稿。
INSERT INTO spaces (id, type, authority, ref, name, state, created_by, created_at, archived_at) VALUES
  ('01900000-0000-7000-8000-000000000003', 'jp.i32.chain.space.party', 'did:web:chain.i32.jp',
   'at://did:web:chain.i32.jp/space/jp.i32.chain.space.party/01900000-0000-7000-8000-000000000003',
   '10/14 13:00 の party', 'archived', 'did:web:seed-1.chain.i32.jp', 1792069200000, 1792087200000);

INSERT INTO parties (space_id, phase, locked, tentative_at, confirmed_at, ended_at, last_activity_at) VALUES
  ('01900000-0000-7000-8000-000000000003', 'ended', 0, 1792069200000, 1792069500000, 1792087200000, 1792086600000);

INSERT INTO space_members (space_id, did, can_write, role, joined_at, left_at) VALUES
  ('01900000-0000-7000-8000-000000000003', 'did:web:seed-1.chain.i32.jp', 0, 'member', 1792069200000, 1792087200000),
  ('01900000-0000-7000-8000-000000000003', 'did:web:seed-2.chain.i32.jp', 0, 'member', 1792069200000, 1792087200000),
  ('01900000-0000-7000-8000-000000000003', 'did:web:seed-3.chain.i32.jp', 0, 'member', 1792069800000, 1792087200000);

INSERT INTO records (uri, space_id, author_did, collection, rkey, value_json, created_at) VALUES
  ('at://did:web:chain.i32.jp/space/jp.i32.chain.space.party/01900000-0000-7000-8000-000000000003/did:web:seed-1.chain.i32.jp/jp.i32.chain.instance.post/3m2seedpost01',
   '01900000-0000-7000-8000-000000000003', 'did:web:seed-1.chain.i32.jp', 'jp.i32.chain.instance.post', '3m2seedpost01',
   '{"text":"集合しました","format":"plain","createdAt":"2026-10-14T04:06:00.000Z"}', 1792069560000),
  ('at://did:web:chain.i32.jp/space/jp.i32.chain.space.party/01900000-0000-7000-8000-000000000003/did:web:seed-2.chain.i32.jp/jp.i32.chain.instance.post/3m2seedpost02',
   '01900000-0000-7000-8000-000000000003', 'did:web:seed-2.chain.i32.jp', 'jp.i32.chain.instance.post', '3m2seedpost02',
   '{"text":"次は **15:00** に駅前","format":"markdown","createdAt":"2026-10-14T05:00:00.000Z"}', 1792072800000),
  ('at://did:web:chain.i32.jp/space/jp.i32.chain.space.party/01900000-0000-7000-8000-000000000003/did:web:seed-3.chain.i32.jp/jp.i32.chain.instance.post/3m2seedpost03',
   '01900000-0000-7000-8000-000000000003', 'did:web:seed-3.chain.i32.jp', 'jp.i32.chain.instance.post', '3m2seedpost03',
   '{"text":"おつかれさまでした","format":"plain","createdAt":"2026-10-14T08:30:00.000Z"}', 1792085400000);
