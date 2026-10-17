package model

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/kv"
)

// AcquireLock は SET NX PX による排他を取る（04 §3 lock:{name}）。取れなければ false。
func AcquireLock(ctx context.Context, r *redis.Client, name string, ttl time.Duration) (bool, error) {
	return r.SetNX(ctx, kv.LockKey(name), "1", ttl).Result()
}

func ReleaseLock(ctx context.Context, r *redis.Client, name string) error {
	return r.Del(ctx, kv.LockKey(name)).Err()
}
