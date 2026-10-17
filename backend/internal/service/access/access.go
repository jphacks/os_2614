package access

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/config"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	groupmodel "github.com/applemango/chain/internal/model/group"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/service/core"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/service/schedule"
)

// アクセス判定（10 §5、Spaces の checkUserAccess に相当）。

type Mode string

const (
	ModeRead  Mode = "read"
	ModeWrite Mode = "write"
)

// AccessChecker は space への read / write を判定する（10 §5）。
// 許可なら (true, nil)。恒久的な拒否は (false, nil)、理由を返す場合（gone / not_in_geofence / location_stale / not_found）は
// 対応する httpx.Err* を返す。
type AccessChecker interface {
	Check(ctx context.Context, spaceID string, did syntax.DID, mode Mode) (bool, error)
}

type AccessService struct {
	core.Deps
	Zones proximity.ZonePresence
}

var _ AccessChecker = (*AccessService)(nil)

const (
	accessAllowTTL = 60 * time.Second
	accessDenyTTL  = 10 * time.Second
)

func (s *AccessService) Check(ctx context.Context, spaceID string, did syntax.DID, mode Mode) (bool, error) {
	key := kv.AccessKey(spaceID, did.String(), string(mode))
	if allowed, found, err := s.cachedAccess(ctx, key); err != nil {
		return false, err
	} else if found {
		return allowed, nil
	}

	allowed, err := s.evaluate(ctx, spaceID, did, mode)
	if err != nil {
		return false, err
	}
	ttl := accessDenyTTL
	if allowed {
		ttl = accessAllowTTL
	}
	if err := s.Redis.Set(ctx, key, boolString(allowed), ttl).Err(); err != nil {
		return false, err
	}
	return allowed, nil
}

func (s *AccessService) evaluate(ctx context.Context, spaceID string, did syntax.DID, mode Mode) (bool, error) {
	sp, err := spacemodel.GetSpace(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, httpx.ErrNotFound
	}
	if err != nil {
		return false, err
	}
	switch sp.State {
	case "deleted":
		return false, httpx.ErrNotFound
	case "archived":
		return s.checkArchived(ctx, spaceID, did, mode)
	}

	switch sp.Type {
	case "jp.i32.chain.space.group":
		return s.checkGroup(ctx, sp, did, mode)
	case "jp.i32.chain.space.party":
		return s.checkParty(ctx, spaceID, did, mode)
	default:
		return false, nil
	}
}

// checkArchived は archived の Space の read を「メンバー（離脱済みを含む）」に許し、write を 410 にする（10 §5）。
func (s *AccessService) checkArchived(ctx context.Context, spaceID string, did syntax.DID, mode Mode) (bool, error) {
	member, err := s.member(ctx, spaceID, did)
	if err != nil {
		return false, err
	}
	if member == nil {
		return false, nil
	}
	if mode == ModeWrite {
		return false, httpx.ErrGone
	}
	return member.CanRead, nil
}

// checkGroup はグループの判定（09 §4）。
func (s *AccessService) checkGroup(ctx context.Context, sp *spacemodel.Space, did syntax.DID, mode Mode) (bool, error) {
	member, err := s.member(ctx, sp.ID, did)
	if err != nil {
		return false, err
	}
	if member == nil || member.LeftAt != nil || !member.CanRead {
		return false, nil
	}

	group, err := groupmodel.GetGroup(ctx, s.DB, sp.ID)
	if err != nil {
		return false, err
	}
	if !group.Persistent {
		if !schedule.ScheduleActive(group.ScheduleJSON, s.Clock.Now()) {
			return false, nil
		}
		inside, err := s.inside(ctx, did, sp.ID, group.CenterLat, group.CenterLon, group.RadiusM)
		if err != nil {
			return false, err
		}
		if !inside {
			return false, httpx.ErrNotInGeofence
		}
	}
	if mode == ModeWrite {
		return member.CanWrite, nil
	}
	return true, nil
}

// checkParty は Party の判定（10 §5 の表）。
func (s *AccessService) checkParty(ctx context.Context, spaceID string, did syntax.DID, mode Mode) (bool, error) {
	member, err := s.member(ctx, spaceID, did)
	if err != nil {
		return false, err
	}
	party, err := model.GetParty(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	switch party.Phase {
	case "tentative":
		if member == nil || member.LeftAt != nil || !member.CanRead {
			return false, nil
		}
		if mode == ModeWrite {
			return false, nil
		}
		return true, nil
	case "confirmed":
		if member == nil || !member.CanRead {
			return false, nil
		}
		if mode == ModeWrite {
			return member.LeftAt == nil && member.CanWrite, nil
		}
		return true, nil
	default:
		return false, nil
	}
}

func (s *AccessService) member(ctx context.Context, spaceID string, did syntax.DID) (*spacemodel.SpaceMember, error) {
	m, err := spacemodel.GetSpaceMember(ctx, s.DB, spaceID, did.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

// inside は presence を判定し、位置が古ければ 403 location_stale を返す（09 §4、10 §5）。
func (s *AccessService) inside(ctx context.Context, did syntax.DID, spaceID string, lat, lon float64, radiusM int) (bool, error) {
	if s.Zones == nil {
		return false, httpx.ErrLocationStale
	}
	inside, err := s.Zones.Inside(ctx, did.String(), proximity.Zone{
		ID: spaceID, CenterLat: lat, CenterLon: lon, RadiusM: radiusM,
	}, config.PresenceGrace)
	if errors.Is(err, proximity.ErrStale) {
		return false, httpx.ErrLocationStale
	}
	if err != nil {
		return false, err
	}
	return inside, nil
}

// InvalidateSpace は Space のメンバー変更・状態変更のときに判定結果を消す（10 §5）。
func (s *AccessService) InvalidateSpace(ctx context.Context, spaceID string) error {
	return s.deleteByPattern(ctx, kv.AccessPrefix(spaceID)+"*")
}

// Invalidate は 1 人分の判定結果を消す。
func (s *AccessService) Invalidate(ctx context.Context, spaceID, did string) error {
	return s.deleteByPattern(ctx, kv.AccessPrefix(spaceID)+did+":*")
}

func (s *AccessService) deleteByPattern(ctx context.Context, pattern string) error {
	iter := s.Redis.Scan(ctx, 0, pattern, 100).Iterator()
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
	return s.Redis.Del(ctx, keys...).Err()
}

func (s *AccessService) cachedAccess(ctx context.Context, key string) (bool, bool, error) {
	v, err := s.Redis.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return v == "1", true, nil
}

func boolString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
