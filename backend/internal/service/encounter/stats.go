package encounter

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	friendmodel "github.com/applemango/chain/internal/model/friend"
	"github.com/applemango/chain/internal/service/core"
)

// StatsCacheTTL は stats:{did} のキャッシュの寿命（13 §3、04 §3）。
const StatsCacheTTL = 5 * time.Minute

// StatsService は個人の統計を返す（13 §3）。
type StatsService struct {
	core.Deps
	Encounters *EncounterService
}

// MeStats は GET /me/stats の値（13 §3）。
type MeStats struct {
	Encounters EncounterSummary `json:"encounters"`
	Friends    int              `json:"friends"`
	Parties    StatsParties     `json:"parties"`
	Groups     StatsGroups      `json:"groups"`
	Posts      StatsPosts       `json:"posts"`
}

type StatsParties struct {
	Total int `json:"total"`
}

type StatsGroups struct {
	Member int `json:"member"`
}

type StatsPosts struct {
	Month int `json:"month"`
}

// statsCacheEntry は Redis に置く値。時刻を自分で持ち、Fake の時計でも期限を判定できるようにする。
type statsCacheEntry struct {
	At    int64   `json:"at"`
	Stats MeStats `json:"stats"`
}

// Get は統計をその場で集計して返す。stats:{did} に StatsCacheTTL だけキャッシュする（13 §3）。
func (s *StatsService) Get(ctx context.Context, did string) (*MeStats, error) {
	now := s.Clock.Now()
	if cached, found, err := s.cached(ctx, did, now); err != nil {
		return nil, err
	} else if found {
		return cached, nil
	}
	st, err := s.compute(ctx, did)
	if err != nil {
		return nil, err
	}
	s.store(ctx, did, st, now)
	return st, nil
}

func (s *StatsService) cached(ctx context.Context, did string, now time.Time) (*MeStats, bool, error) {
	raw, err := s.Redis.Get(ctx, kv.StatsKey(did)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var e statsCacheEntry
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, false, nil
	}
	if now.Sub(time.UnixMilli(e.At)) >= StatsCacheTTL {
		return nil, false, nil
	}
	return &e.Stats, true, nil
}

func (s *StatsService) store(ctx context.Context, did string, st *MeStats, now time.Time) {
	b, err := json.Marshal(statsCacheEntry{At: now.UnixMilli(), Stats: *st})
	if err != nil {
		slog.WarnContext(ctx, "stats: marshal cache failed", "err", err)
		return
	}
	if err := s.Redis.Set(ctx, kv.StatsKey(did), b, StatsCacheTTL).Err(); err != nil {
		slog.WarnContext(ctx, "stats: cache store failed", "err", err)
	}
}

func (s *StatsService) compute(ctx context.Context, did string) (*MeStats, error) {
	enc, err := s.Encounters.Summary(ctx, did)
	if err != nil {
		return nil, err
	}
	friends, err := friendmodel.CountFriends(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	parties, err := model.CountPartiesForUser(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	groups, err := model.CountActiveGroupsForUser(ctx, s.DB, did)
	if err != nil {
		return nil, err
	}
	monthStart := core.JstMonthStart(s.Clock.Now())
	posts, err := model.CountPostsForUserBetween(ctx, s.DB, did, monthStart, monthStart.AddDate(0, 1, 0))
	if err != nil {
		return nil, err
	}
	return &MeStats{
		Encounters: *enc,
		Friends:    friends,
		Parties:    StatsParties{Total: parties},
		Groups:     StatsGroups{Member: groups},
		Posts:      StatsPosts{Month: posts},
	}, nil
}
