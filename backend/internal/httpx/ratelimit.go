package httpx

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/kv"
)

// Bucket はレート制限の単位（02 §5 の表）。
type Bucket string

const (
	BucketSightings Bucket = "sightings"
	BucketPresence  Bucket = "presence"
	BucketLocation  Bucket = "location"
	BucketPost      Bucket = "post"
	BucketMedia     Bucket = "media"
	BucketFriendReq Bucket = "friend_req"
	BucketDefault   Bucket = "default"
	// BucketAuth は /auth/* の、DID ではなく IP をキーにする制限。
	BucketAuth Bucket = "auth"
)

// Limit は 1 つのバケットの上限と窓長。
type Limit struct {
	Max    int
	Window time.Duration
}

// Limits はバケットごとの上限。欠けたバケットは DefaultLimits で補う。
type Limits map[Bucket]Limit

// DefaultLimits は 02 §5 の表。
var DefaultLimits = Limits{
	BucketSightings: {Max: 6, Window: time.Minute},
	BucketPresence:  {Max: 12, Window: time.Minute},
	BucketLocation:  {Max: 6, Window: time.Minute},
	BucketPost:      {Max: 30, Window: time.Minute},
	BucketMedia:     {Max: 20, Window: time.Minute},
	BucketFriendReq: {Max: 30, Window: time.Hour},
	BucketDefault:   {Max: 300, Window: time.Minute},
}

// AuthLimit は /auth/* の IP 単位の上限（02 §5）。
var AuthLimit = Limit{Max: 30, Window: time.Minute}

// RateLimitConfig は RateLimit ミドルウェアの設定。
type RateLimitConfig struct {
	Redis  *redis.Client
	Clock  clock.Clock
	Limits Limits
}

// routeBuckets は method とパスから専用バケットを決める表。ここに無い認証ルートは default だけ。
var routeBuckets = []struct {
	method string
	path   string
	bucket Bucket
}{
	{fiber.MethodPost, "/api/v1/encounters/sightings", BucketSightings},
	{fiber.MethodPost, "/api/v1/party/presence", BucketPresence},
	{fiber.MethodPut, "/api/v1/me/location", BucketLocation},
	{fiber.MethodPost, "/api/v1/posts", BucketPost},
	{fiber.MethodPost, "/api/v1/feed/posts", BucketPost},
	{fiber.MethodPost, "/api/v1/media", BucketMedia},
	{fiber.MethodPut, "/api/v1/me/avatar", BucketMedia},
	{fiber.MethodPut, "/api/v1/me/banner", BucketMedia},
	{fiber.MethodPost, "/api/v1/friends/requests", BucketFriendReq},
}

// authIPRoutes は IP 単位で数える /auth/*（02 §5）。
var authIPRoutes = []struct {
	method string
	path   string
}{
	{fiber.MethodGet, "/auth/login"},
	{fiber.MethodGet, "/auth/callback"},
	{fiber.MethodGet, "/auth/dev/complete"},
	{fiber.MethodPost, "/api/v1/auth/exchange"},
	{fiber.MethodPost, "/api/v1/auth/refresh"},
}

const incrScript = `
local v = redis.call('INCR', KEYS[1])
if v == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return v
`

type limiter struct {
	rdb    *redis.Client
	clock  clock.Clock
	limits Limits
	script *redis.Script
}

// RateLimit は Redis の固定窓カウンタでレート制限するミドルウェアを返す（02 §5）。
// app 全体と、requireAuth の直後の両方に差す。認証前（DID 未設定）は何もしない。
func RateLimit(cfg RateLimitConfig) fiber.Handler {
	limits := make(Limits, len(DefaultLimits)+len(cfg.Limits))
	for k, v := range DefaultLimits {
		limits[k] = v
	}
	for k, v := range cfg.Limits {
		limits[k] = v
	}
	l := &limiter{rdb: cfg.Redis, clock: cfg.Clock, limits: limits, script: redis.NewScript(incrScript)}
	return l.handle
}

func (l *limiter) handle(c fiber.Ctx) error {
	method, path := c.Method(), c.Path()
	if path == "/health" {
		return c.Next()
	}
	if method == fiber.MethodGet && path == "/api/v1/ws" {
		return c.Next()
	}

	now := l.clock.Now()

	if isAuthIPRoute(method, path) {
		if err := l.allow(c, BucketAuth, c.IP(), AuthLimit, now); err != nil {
			return err
		}
		return c.Next()
	}

	// 認証ありのルートは DID 単位。requireAuth 前は DID が無いので通す。
	did := DID(c)
	if did == "" {
		return c.Next()
	}

	if bucket, ok := bucketFor(method, path); ok {
		if err := l.allow(c, bucket, did, l.limits[bucket], now); err != nil {
			return err
		}
	}
	if err := l.allow(c, BucketDefault, did, l.limits[BucketDefault], now); err != nil {
		return err
	}
	return c.Next()
}

// allow は 1 バケット分を数え、上限を超えていれば ErrRateLimited を返す。
// Redis のエラーでは止めず、ログを出して通す。
func (l *limiter) allow(c fiber.Ctx, bucket Bucket, id string, lim Limit, now time.Time) error {
	count, err := l.incr(c.Context(), bucket, id, lim.Window, now)
	if err != nil {
		slog.ErrorContext(c.Context(), "rate limit: redis error, allowing request", "err", err, "bucket", bucket)
		return nil
	}
	if count <= int64(lim.Max) {
		return nil
	}
	c.Set(fiber.HeaderRetryAfter, strconv.Itoa(retryAfterSeconds(now, lim.Window)))
	return ErrRateLimited
}

func (l *limiter) incr(ctx context.Context, bucket Bucket, id string, window time.Duration, now time.Time) (int64, error) {
	secs := int64(window.Seconds())
	windowNum := now.Unix() / secs
	key := kv.RateLimitKey(string(bucket), id, windowNum)
	if bucket == BucketAuth {
		key = kv.RateLimitAuthKey(id, windowNum)
	}
	return l.script.Run(ctx, l.rdb, []string{key}, secs).Int64()
}

func bucketFor(method, path string) (Bucket, bool) {
	for _, rb := range routeBuckets {
		if rb.method == method && rb.path == path {
			return rb.bucket, true
		}
	}
	if method == fiber.MethodPost && instanceMediaPath(path) {
		return BucketMedia, true
	}
	return "", false
}

// instanceMediaPath は POST /api/v1/instances/{id}/media を判別する。
func instanceMediaPath(path string) bool {
	const prefix = "/api/v1/instances/"
	const suffix = "/media"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return mid != "" && !strings.Contains(mid, "/")
}

func isAuthIPRoute(method, path string) bool {
	for _, ar := range authIPRoutes {
		if ar.method == method && ar.path == path {
			return true
		}
	}
	return false
}

// windowStart は now が属する固定窓の開始時刻。窓番号は floor(now / 窓長)（02 §5）。
func windowStart(now time.Time, window time.Duration) time.Time {
	secs := int64(window.Seconds())
	return time.Unix((now.Unix()/secs)*secs, 0)
}

func retryAfterSeconds(now time.Time, window time.Duration) int {
	remaining := window - now.Sub(windowStart(now, window))
	secs := int(math.Ceil(remaining.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return secs
}
