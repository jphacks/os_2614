package model

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/kv"
)

// ModerationJob は modq（11 §3 の再判定待ち）の 1 要素。
// 04 §3 のとおり modq は list。要素ごとに試行回数と次回時刻を持つことで、
// 指数バックオフをリストのまま表現する（次回時刻までは再判定しない）。
type ModerationJob struct {
	URI      string `json:"uri"`      // records.uri
	Attempts int    `json:"attempts"` // これまでの失敗回数
	NextAt   int64  `json:"nextAt"`   // unix ミリ秒。0 なら即時
}

// ModerationJobEntry は生の要素（削除・入れ替えに使う）と解釈した Job の組。
type ModerationJobEntry struct {
	Raw string
	Job ModerationJob
}

// EnqueueModerationJob は再判定待ちを積む。URI だけを入れ、本文は records から読む
// （Redis に投稿本文を複製しない。02 §8）。
func EnqueueModerationJob(ctx context.Context, r *redis.Client, uri string) error {
	b, err := json.Marshal(ModerationJob{URI: uri})
	if err != nil {
		return err
	}
	return r.LPush(ctx, kv.ModerationQueueKey(), string(b)).Err()
}

// ListModerationJobs は先頭から最大 limit 件を覗く（取り出さない）。壊れた要素も Raw 付きで返す。
func ListModerationJobs(ctx context.Context, r *redis.Client, limit int) ([]ModerationJobEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	raws, err := r.LRange(ctx, kv.ModerationQueueKey(), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	entries := make([]ModerationJobEntry, 0, len(raws))
	for _, raw := range raws {
		var job ModerationJob
		_ = json.Unmarshal([]byte(raw), &job)
		entries = append(entries, ModerationJobEntry{Raw: raw, Job: job})
	}
	return entries, nil
}

// RemoveModerationJob は生の要素を 1 つ消す（同じ値は URI が一意なので 1 つだけ）。
func RemoveModerationJob(ctx context.Context, r *redis.Client, raw string) error {
	return r.LRem(ctx, kv.ModerationQueueKey(), 1, raw).Err()
}

// RequeueModerationJob は生の要素を、試行回数と次回時刻を更新した要素に置き換える。
func RequeueModerationJob(ctx context.Context, r *redis.Client, raw string, job ModerationJob) error {
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	key := kv.ModerationQueueKey()
	_, err = r.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.LRem(ctx, key, 1, raw)
		pipe.LPush(ctx, key, string(b))
		return nil
	})
	return err
}
