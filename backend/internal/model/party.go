package model

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/kv"
)

// PartyPresenceTTL は pp:{space} と pleft:{space} の寿命。MaxDuration（7 日）より長くする（08 §4）。
const PartyPresenceTTL = 8 * 24 * time.Hour

// Party は parties の行（04 §2.4）。phase でアクセス判定が変わる（10 §5）。
type Party struct {
	SpaceID        string
	Phase          string
	Locked         bool
	TentativeAt    time.Time
	ConfirmedAt    *time.Time
	EndedAt        *time.Time
	LastActivityAt time.Time
}

func scanParty(scan db.Scan, p *Party) error {
	var tentative, last int64
	var confirmed, ended sql.NullInt64
	if err := scan(&p.SpaceID, &p.Phase, &p.Locked, &tentative, &confirmed, &ended, &last); err != nil {
		return err
	}
	p.TentativeAt = clock.FromUnixMilli(tentative)
	p.LastActivityAt = clock.FromUnixMilli(last)
	if confirmed.Valid {
		t := clock.FromUnixMilli(confirmed.Int64)
		p.ConfirmedAt = &t
	}
	if ended.Valid {
		t := clock.FromUnixMilli(ended.Int64)
		p.EndedAt = &t
	}
	return nil
}

const partyColumns = `space_id, phase, locked, tentative_at, confirmed_at, ended_at, last_activity_at`

func GetParty(ctx context.Context, q db.Querier, spaceID string) (*Party, error) {
	return db.GetItem(ctx, q, "SELECT "+partyColumns+" FROM parties WHERE space_id = ?",
		[]any{spaceID}, scanParty)
}

func InsertParty(ctx context.Context, q db.Querier, p Party) error {
	_, err := db.Execute(ctx, q, `
		INSERT INTO parties (space_id, phase, locked, tentative_at, confirmed_at, ended_at, last_activity_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.SpaceID, p.Phase, p.Locked, clock.UnixMilli(p.TentativeAt),
		NullableTime(p.ConfirmedAt), NullableTime(p.EndedAt), clock.UnixMilli(p.LastActivityAt))
	return err
}

// SetPartyPhase は phase と、確定・終了の時刻をまとめて更新する（08 §4.2）。
func SetPartyPhase(ctx context.Context, q db.Querier, spaceID, phase string, confirmedAt, endedAt *time.Time) error {
	_, err := db.Execute(ctx, q,
		"UPDATE parties SET phase = ?, confirmed_at = ?, ended_at = ? WHERE space_id = ?",
		phase, NullableTime(confirmedAt), NullableTime(endedAt), spaceID)
	return err
}

// UpdatePartyLocked は固定（lock）の状態を切り替える（08 §4.4）。
func UpdatePartyLocked(ctx context.Context, q db.Querier, spaceID string, locked bool) error {
	_, err := db.Execute(ctx, q, "UPDATE parties SET locked = ? WHERE space_id = ?", locked, spaceID)
	return err
}

// TouchPartyActivity は在席または投稿の最終時刻を進める（08 §4.2）。
func TouchPartyActivity(ctx context.Context, q db.Querier, spaceID string, at time.Time) error {
	_, err := db.Execute(ctx, q, "UPDATE parties SET last_activity_at = ? WHERE space_id = ?",
		clock.UnixMilli(at), spaceID)
	return err
}

// ListActiveParties は state が active な Space の、終了していない party を返す（08 §4.1 step4）。
func ListActiveParties(ctx context.Context, q db.Querier) ([]Party, error) {
	return db.GetItems(ctx, q, `
		SELECT `+partyColumns+` FROM parties p
		JOIN spaces s ON s.id = p.space_id
		WHERE s.state = 'active' AND p.phase IN ('tentative','confirmed')`,
		nil, scanParty)
}

// PartyMemberRef は party の所属メンバー 1 件（space_id, did）。
type PartyMemberRef struct {
	SpaceID string
	DID     string
}

// ListActivePartyMembers は active な party の全メンバーを返す（party:of の組み直しに使う。08 §4）。
func ListActivePartyMembers(ctx context.Context, q db.Querier) ([]PartyMemberRef, error) {
	return db.GetItems(ctx, q, `
		SELECT p.space_id, m.did FROM parties p
		JOIN spaces s ON s.id = p.space_id
		JOIN space_members m ON m.space_id = p.space_id
		WHERE s.state = 'active' AND p.phase IN ('tentative','confirmed') AND m.left_at IS NULL`,
		nil, func(scan db.Scan, r *PartyMemberRef) error { return scan(&r.SpaceID, &r.DID) })
}

// LeavePartyMember は離脱させる。can_write を 0 にし、can_read は 1 のまま残す（08 §4.2）。
func LeavePartyMember(ctx context.Context, q db.Querier, spaceID, did string, leftAt time.Time) error {
	_, err := db.Execute(ctx, q,
		"UPDATE space_members SET left_at = ?, can_write = 0, can_read = 1 WHERE space_id = ? AND did = ?",
		clock.UnixMilli(leftAt), spaceID, did)
	return err
}

// PartyAutoMap は did → party_auto を返す。行がなければ既定の true（04 §2.1、08 §4.1）。
func PartyAutoMap(ctx context.Context, q db.Querier, dids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(dids))
	if len(dids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(dids))
	for _, d := range dids {
		args = append(args, d)
		out[d] = true
	}
	rows, err := db.GetItems(ctx, q,
		"SELECT did, party_auto FROM user_settings WHERE did IN ("+Placeholders(len(dids))+")",
		args, func(scan db.Scan, v *partyAutoRow) error { return scan(&v.DID, &v.Auto) })
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.DID] = r.Auto
	}
	return out, nil
}

type partyAutoRow struct {
	DID  string
	Auto bool
}

// DeletedDIDs は dids のうち deleted_at が入っているものを返す（08 §4.1）。
func DeletedDIDs(ctx context.Context, q db.Querier, dids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(dids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(dids))
	for _, d := range dids {
		args = append(args, d)
	}
	rows, err := db.GetItems(ctx, q,
		"SELECT did FROM users WHERE deleted_at IS NOT NULL AND did IN ("+Placeholders(len(dids))+")",
		args, func(scan db.Scan, s *string) error { return scan(s) })
	if err != nil {
		return nil, err
	}
	for _, d := range rows {
		out[d] = true
	}
	return out, nil
}

// GetPartyOf は party:of:{did} を返す（08 §4）。
func GetPartyOf(ctx context.Context, r *redis.Client, did string) (string, bool, error) {
	v, err := r.Get(ctx, kv.PartyOfKey(did)).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// GetPartyOfMany は複数 DID の所属 party をまとめて返す。
func GetPartyOfMany(ctx context.Context, r *redis.Client, dids []string) (map[string]string, error) {
	out := make(map[string]string, len(dids))
	if len(dids) == 0 {
		return out, nil
	}
	keys := make([]string, 0, len(dids))
	for _, d := range dids {
		keys = append(keys, kv.PartyOfKey(d))
	}
	vals, err := r.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for i, v := range vals {
		if s, ok := v.(string); ok && s != "" {
			out[dids[i]] = s
		}
	}
	return out, nil
}

func SetPartyOf(ctx context.Context, r *redis.Client, did, spaceID string) error {
	return r.Set(ctx, kv.PartyOfKey(did), spaceID, 0).Err()
}

func ClearPartyOf(ctx context.Context, r *redis.Client, did string) error {
	return r.Del(ctx, kv.PartyOfKey(did)).Err()
}

// ClearAllPartyOf は party:of:* をすべて消す（組み直しの前段。08 §4）。
func ClearAllPartyOf(ctx context.Context, r *redis.Client) error {
	iter := r.Scan(ctx, 0, kv.PartyOfPrefix()+"*", 200).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	return r.Del(ctx, keys...).Err()
}

// PartyPresence は pp:{space} の did → 最終在席時刻を返す（08 §4）。
func PartyPresence(ctx context.Context, r *redis.Client, spaceID string) (map[string]time.Time, error) {
	vals, err := r.HGetAll(ctx, kv.PartyPresenceKey(spaceID)).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]time.Time, len(vals))
	for did, v := range vals {
		ms, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			continue
		}
		out[did] = clock.FromUnixMilli(ms)
	}
	return out, nil
}

func SetPartyPresence(ctx context.Context, r *redis.Client, spaceID, did string, at time.Time) error {
	key := kv.PartyPresenceKey(spaceID)
	if err := r.HSet(ctx, key, did, at.UnixMilli()).Err(); err != nil {
		return err
	}
	return r.Expire(ctx, key, PartyPresenceTTL).Err()
}

func DeletePartyPresence(ctx context.Context, r *redis.Client, spaceID string) error {
	return r.Del(ctx, kv.PartyPresenceKey(spaceID)).Err()
}

// AddPartyLeaver は明示退出した DID を pleft:{space} に記録する（08 §4.2）。
func AddPartyLeaver(ctx context.Context, r *redis.Client, spaceID, did string) error {
	key := kv.PartyLeftKey(spaceID)
	if err := r.SAdd(ctx, key, did).Err(); err != nil {
		return err
	}
	return r.Expire(ctx, key, PartyPresenceTTL).Err()
}

func PartyLeavers(ctx context.Context, r *redis.Client, spaceID string) (map[string]bool, error) {
	members, err := r.SMembers(ctx, kv.PartyLeftKey(spaceID)).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(members))
	for _, m := range members {
		out[m] = true
	}
	return out, nil
}

func DeletePartyLeavers(ctx context.Context, r *redis.Client, spaceID string) error {
	return r.Del(ctx, kv.PartyLeftKey(spaceID)).Err()
}
