package model

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/kv"
)

// 観測バケット sgt:{a}:{b}:{bucket} の値は「first|last|maxRssi」（unix ミリ秒）。
// RSSI を持たない観測は noRSSI を入れる（RSSI は常に負なので衝突しない）。
const noRSSI = -1000

const (
	SightingTTL   = 26 * time.Hour
	BleRecentTTL  = 30 * time.Minute
	sightingParts = 3
)

func encodeSighting(first, last time.Time, rssi *int) string {
	r := noRSSI
	if rssi != nil {
		r = *rssi
	}
	return strconv.FormatInt(first.UnixMilli(), 10) + "|" +
		strconv.FormatInt(last.UnixMilli(), 10) + "|" +
		strconv.Itoa(r)
}

func parseSighting(v string) (first, last int64, rssi int, ok bool) {
	parts := strings.Split(v, "|")
	if len(parts) != sightingParts {
		return 0, 0, 0, false
	}
	var err error
	if first, err = strconv.ParseInt(parts[0], 10, 64); err != nil {
		return 0, 0, 0, false
	}
	if last, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return 0, 0, 0, false
	}
	if rssi, err = strconv.Atoi(parts[2]); err != nil {
		return 0, 0, 0, false
	}
	return first, last, rssi, true
}

// ObserveSighting は sgt:{a}:{b}:{bucket} を merge し、ble:recent:{a} を更新する（05 §3）。
// merge では first は最小、last と rssi は最大を保つ。
func ObserveSighting(ctx context.Context, r *redis.Client, a, b string, bucket int64, first, last time.Time, rssi *int, now time.Time) error {
	key := kv.SightingKey(a, b, bucket)
	existing, err := r.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return err
	}

	firstMs, lastMs, rssiVal, ok := parseSighting(existing)
	if !ok {
		firstMs, lastMs, rssiVal = first.UnixMilli(), last.UnixMilli(), noRSSI
	}
	if first.UnixMilli() < firstMs {
		firstMs = first.UnixMilli()
	}
	if last.UnixMilli() > lastMs {
		lastMs = last.UnixMilli()
	}
	if rssi != nil && *rssi > rssiVal {
		rssiVal = *rssi
	}
	value := strconv.FormatInt(firstMs, 10) + "|" + strconv.FormatInt(lastMs, 10) + "|" + strconv.Itoa(rssiVal)
	if err := r.Set(ctx, key, value, SightingTTL).Err(); err != nil {
		return err
	}

	zkey := kv.BleRecentKey(a)
	if err := r.ZAdd(ctx, zkey, redis.Z{Score: float64(last.UnixMilli()), Member: b}).Err(); err != nil {
		return err
	}
	if err := r.Expire(ctx, zkey, BleRecentTTL).Err(); err != nil {
		return err
	}
	cutoff := strconv.FormatInt(now.Add(-BleRecentTTL).UnixMilli(), 10)
	return r.ZRemRangeByScore(ctx, zkey, "-inf", cutoff).Err()
}

// MutualSighting は b が a を観測したバケットのうち、at±within と重なるものがあるかを返す（05 §3）。
// a→b の観測は呼び出し側が直前に ObserveSighting したものを前提にする。
func MutualSighting(ctx context.Context, r *redis.Client, a, b string, at time.Time, within, bucketWidth time.Duration) (bool, error) {
	lo := at.Add(-within).UnixMilli()
	hi := at.Add(within).UnixMilli()
	firstBucket := at.Add(-within).Truncate(bucketWidth).Unix()
	lastBucket := at.Add(within).Truncate(bucketWidth).Unix()

	keys := make([]string, 0, (lastBucket-firstBucket)/int64(bucketWidth/time.Second)+1)
	for bucket := firstBucket; bucket <= lastBucket; bucket += int64(bucketWidth / time.Second) {
		keys = append(keys, kv.SightingKey(b, a, bucket))
	}
	values, err := r.MGet(ctx, keys...).Result()
	if err != nil {
		return false, err
	}
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			continue
		}
		first, last, _, ok := parseSighting(s)
		if !ok {
			continue
		}
		if first <= hi && last >= lo {
			return true, nil
		}
	}
	return false, nil
}

// PeerEdgeTTL は pe:{a}:{b} の寿命（04 §3）。
const PeerEdgeTTL = 30 * time.Minute

// PeerEdge はフレンド近接の連続性（08 §3）。
type PeerEdge struct {
	A     string
	B     string
	Since time.Time
	Last  time.Time
}

// ObservePeerEdge は pe:{a}:{b} の since / last を更新し、pe:active に載せる（08 §3）。
// t - last > gapReset なら since を t に戻す。last は大きい方を保つ。
func ObservePeerEdge(ctx context.Context, r *redis.Client, a, b string, t time.Time, gapReset time.Duration) error {
	if a > b {
		a, b = b, a
	}
	key := kv.PeerEdgeKey(a, b)
	since, last, ok, err := readPeerEdge(ctx, r, key)
	if err != nil {
		return err
	}
	if ok {
		if t.Sub(last) > gapReset {
			since = t
		}
		if t.After(last) {
			last = t
		}
	} else {
		since, last = t, t
	}
	if err := r.HSet(ctx, key, "since", since.UnixMilli(), "last", last.UnixMilli()).Err(); err != nil {
		return err
	}
	if err := r.Expire(ctx, key, PeerEdgeTTL).Err(); err != nil {
		return err
	}
	return r.ZAdd(ctx, kv.PeerActiveKey(), redis.Z{Score: float64(last.UnixMilli()), Member: a + "|" + b}).Err()
}

func readPeerEdge(ctx context.Context, r *redis.Client, key string) (since, last time.Time, ok bool, err error) {
	vals, err := r.HGetAll(ctx, key).Result()
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	if len(vals) == 0 {
		return time.Time{}, time.Time{}, false, nil
	}
	s, err1 := strconv.ParseInt(vals["since"], 10, 64)
	l, err2 := strconv.ParseInt(vals["last"], 10, 64)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, false, nil
	}
	return time.UnixMilli(s).UTC(), time.UnixMilli(l).UTC(), true, nil
}

// LivePeerEdges は now-alive 以降に観測されたエッジ（pe:active の score）を返す（08 §4.1 step1）。
// since は pe の hash から読む。
func LivePeerEdges(ctx context.Context, r *redis.Client, now time.Time, alive time.Duration) ([]PeerEdge, error) {
	lo := strconv.FormatInt(now.Add(-alive).UnixMilli(), 10)
	members, err := r.ZRangeByScore(ctx, kv.PeerActiveKey(), &redis.ZRangeBy{Min: lo, Max: "+inf"}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]PeerEdge, 0, len(members))
	for _, m := range members {
		a, b, found := strings.Cut(m, "|")
		if !found {
			continue
		}
		since, last, ok, err := readPeerEdge(ctx, r, kv.PeerEdgeKey(a, b))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, PeerEdge{A: a, B: b, Since: since, Last: last})
	}
	return out, nil
}

// RecentNeighbor は Neighbors が返す 1 件。
type RecentNeighbor struct {
	DID      string
	LastSeen time.Time
}

// RecentNeighbors は u が直近 within 以内に観測した相手（ble:recent:{u}）と、
// u を観測した相手（pe:active、08 §3）を返す。
func RecentNeighbors(ctx context.Context, r *redis.Client, u string, within time.Duration, now time.Time) ([]RecentNeighbor, error) {
	seen := map[string]time.Time{}
	lo := strconv.FormatInt(now.Add(-within).UnixMilli(), 10)

	forward, err := r.ZRangeByScore(ctx, kv.BleRecentKey(u), &redis.ZRangeBy{Min: lo, Max: "+inf"}).Result()
	if err != nil {
		return nil, err
	}
	for _, did := range forward {
		score, err := r.ZScore(ctx, kv.BleRecentKey(u), did).Result()
		if err != nil {
			return nil, err
		}
		seen[did] = time.UnixMilli(int64(score)).UTC()
	}

	edges, err := r.ZRangeByScore(ctx, kv.PeerActiveKey(), &redis.ZRangeBy{Min: lo, Max: "+inf"}).Result()
	if err != nil {
		return nil, err
	}
	for _, edge := range edges {
		a, b, ok := strings.Cut(edge, "|")
		if !ok || b != u {
			continue
		}
		score, err := r.ZScore(ctx, kv.PeerActiveKey(), edge).Result()
		if err != nil {
			return nil, err
		}
		if t := time.UnixMilli(int64(score)).UTC(); seen[a].IsZero() || t.After(seen[a]) {
			seen[a] = t
		}
	}

	out := make([]RecentNeighbor, 0, len(seen))
	for did, last := range seen {
		out = append(out, RecentNeighbor{DID: did, LastSeen: last})
	}
	return out, nil
}
