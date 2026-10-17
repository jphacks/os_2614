//go:build integration

package controller_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"

	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/testutil"
)

// requestFull は request と同じだが、Retry-After を見るためにレスポンスヘッダも返す。
func requestFull(t *testing.T, app *fiber.App, method, path, body, token string) (int, []byte, http.Header) {
	t.Helper()

	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatalf("app.Test(%s %s): %v", method, path, err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s %s: %v", method, path, err)
	}
	return resp.StatusCode, b, resp.Header
}

func assertRetryAfter(t *testing.T, h http.Header, window time.Duration) {
	t.Helper()
	v := h.Get("Retry-After")
	if v == "" {
		t.Fatal("Retry-After header is missing")
	}
	secs, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("Retry-After = %q: %v", v, err)
	}
	if secs < 1 || secs > int(window.Seconds()) {
		t.Fatalf("Retry-After = %d, want 1..%d", secs, int(window.Seconds()))
	}
}

// TestRateLimitPerBucket は 02 §5 の各バケットで、上限ちょうどまでは通り、1 回超えると
// 429 と Retry-After になり、窓が明けると再び通ることを確かめる。
func TestRateLimitPerBucket(t *testing.T) {
	const did = "did:plc:ratelimit"

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		limit  int
		window time.Duration
	}{
		{"sightings", http.MethodPost, "/api/v1/encounters/sightings", `{"items":[]}`, 6, time.Minute},
		{"presence", http.MethodPost, "/api/v1/party/presence", `{}`, 12, time.Minute},
		{"location", http.MethodPut, "/api/v1/me/location", `{}`, 6, time.Minute},
		{"post", http.MethodPost, "/api/v1/posts", `{}`, 30, time.Minute},
		{"media", http.MethodPost, "/api/v1/media", `{}`, 20, time.Minute},
		{"friend_req", http.MethodPost, "/api/v1/friends/requests", `{}`, 30, time.Hour},
		{"default", http.MethodGet, "/api/v1/me", "", 300, time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := testutil.NewEnv(t)
			env.CreateUser(t, did, "ratelimit.test")
			token := env.Login(t, did)
			app := testutil.NewApp(env)

			for i := 0; i < tc.limit; i++ {
				status, body, _ := requestFull(t, app, tc.method, tc.path, tc.body, token)
				if status == http.StatusTooManyRequests {
					t.Fatalf("request %d/%d got 429 before the limit (body=%s)", i+1, tc.limit, body)
				}
			}

			status, body, hdr := requestFull(t, app, tc.method, tc.path, tc.body, token)
			if status != http.StatusTooManyRequests {
				t.Fatalf("request %d: status = %d, want 429 (body=%s)", tc.limit+1, status, body)
			}
			testutil.WantErrorCode(t, body, "rate_limited")
			assertRetryAfter(t, hdr, tc.window)

			env.Clock.Advance(tc.window)
			if status, body, _ := requestFull(t, app, tc.method, tc.path, tc.body, token); status == http.StatusTooManyRequests {
				t.Fatalf("still 429 after the window closed (body=%s)", body)
			}
		})
	}
}

// TestRateLimitDedicatedAlsoCountsDefault は、専用バケットのルートでも default を数えることを確かめる。
func TestRateLimitDedicatedAlsoCountsDefault(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateUser(t, "did:plc:rl-def", "rl-def.test")
	token := env.Login(t, "did:plc:rl-def")
	env.RateLimit = &httpx.RateLimitConfig{
		Redis: env.Redis, Clock: env.Clock,
		Limits: httpx.Limits{httpx.BucketDefault: {Max: 2, Window: time.Minute}},
	}
	app := testutil.NewApp(env)

	// sightings の専用上限は 6/分なので、default を 2 に絞ると 3 回目は default 超過で 429 になる。
	for i := 0; i < 2; i++ {
		if status, body, _ := requestFull(t, app, http.MethodPost, "/api/v1/encounters/sightings", `{"items":[]}`, token); status == http.StatusTooManyRequests {
			t.Fatalf("sighting %d: unexpected 429 (body=%s)", i+1, body)
		}
	}
	status, body, _ := requestFull(t, app, http.MethodPost, "/api/v1/encounters/sightings", `{"items":[]}`, token)
	if status != http.StatusTooManyRequests {
		t.Fatalf("3rd sighting: status = %d, want 429 (default must count dedicated routes) (body=%s)", status, body)
	}

	// 専用バケットの無いルートも同じ default を使い切っている。
	status, body, _ = requestFull(t, app, http.MethodGet, "/api/v1/me", "", token)
	if status != http.StatusTooManyRequests {
		t.Fatalf("GET /me: status = %d, want 429 (shared default bucket) (body=%s)", status, body)
	}
}

// TestRateLimitPerUser は DID 単位で独立していることを確かめる。
func TestRateLimitPerUser(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateUser(t, "did:plc:rl-a", "rl-a.test")
	env.CreateUser(t, "did:plc:rl-b", "rl-b.test")
	tokenA := env.Login(t, "did:plc:rl-a")
	tokenB := env.Login(t, "did:plc:rl-b")
	app := testutil.NewApp(env)

	sight := func(token string) int {
		status, _, _ := requestFull(t, app, http.MethodPost, "/api/v1/encounters/sightings", `{"items":[]}`, token)
		return status
	}

	for i := 0; i < 6; i++ {
		if s := sight(tokenA); s == http.StatusTooManyRequests {
			t.Fatalf("A request %d: unexpected 429", i+1)
		}
	}
	if s := sight(tokenA); s != http.StatusTooManyRequests {
		t.Fatalf("A 7th request: status = %d, want 429", s)
	}
	if s := sight(tokenB); s == http.StatusTooManyRequests {
		t.Fatalf("B request: got 429, but B should be independent of A")
	}
}

// TestRateLimitAuthByIP は /auth/* が IP 単位で 30/分に制限されることを確かめる。
func TestRateLimitAuthByIP(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)

	const exchange = `{"code":"bad","deviceName":"test"}`
	for i := 0; i < 30; i++ {
		status, body, _ := requestFull(t, app, http.MethodPost, "/api/v1/auth/exchange", exchange, "")
		if status == http.StatusTooManyRequests {
			t.Fatalf("auth request %d: unexpected 429 (body=%s)", i+1, body)
		}
	}

	status, body, hdr := requestFull(t, app, http.MethodPost, "/api/v1/auth/exchange", exchange, "")
	if status != http.StatusTooManyRequests {
		t.Fatalf("31st auth request: status = %d, want 429 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "rate_limited")
	assertRetryAfter(t, hdr, time.Minute)

	// 別の /auth/* も同じ IP バケットを共有する。
	if s, _, _ := requestFull(t, app, http.MethodGet, "/auth/dev/complete?code=x", "", ""); s != http.StatusTooManyRequests {
		t.Fatalf("/auth/dev/complete: status = %d, want 429 (shared IP bucket)", s)
	}

	env.Clock.Advance(time.Minute)
	if s, b, _ := requestFull(t, app, http.MethodPost, "/api/v1/auth/exchange", exchange, ""); s == http.StatusTooManyRequests {
		t.Fatalf("after the window: still 429 (body=%s)", b)
	}
}

// TestRateLimitAllowsWhenRedisDown は Redis が落ちていても通すことを確かめる。
func TestRateLimitAllowsWhenRedisDown(t *testing.T) {
	env := testutil.NewEnv(t)
	env.CreateUser(t, "did:plc:rl-down", "rl-down.test")
	token := env.Login(t, "did:plc:rl-down")

	broken := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  10 * time.Millisecond,
		ReadTimeout:  10 * time.Millisecond,
		WriteTimeout: 10 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = broken.Close() })
	env.RateLimit = &httpx.RateLimitConfig{
		Redis: broken, Clock: env.Clock,
		Limits: httpx.Limits{
			httpx.BucketSightings: {Max: 1, Window: time.Minute},
			httpx.BucketDefault:   {Max: 1, Window: time.Minute},
		},
	}
	app := testutil.NewApp(env)

	// 上限は 1 だが Redis に接続できないので、何回でも通す（429 にしない）。
	for i := 0; i < 5; i++ {
		status, body, _ := requestFull(t, app, http.MethodPost, "/api/v1/encounters/sightings", `{"items":[]}`, token)
		if status == http.StatusTooManyRequests {
			t.Fatalf("request %d: got 429 while Redis is down (body=%s)", i+1, body)
		}
	}
}

// TestRateLimitSkipsHealth は /health に適用しないことを確かめる。
func TestRateLimitSkipsHealth(t *testing.T) {
	env := testutil.NewEnv(t)
	env.RateLimit = &httpx.RateLimitConfig{
		Redis: env.Redis, Clock: env.Clock,
		Limits: httpx.Limits{httpx.BucketDefault: {Max: 1, Window: time.Minute}},
	}
	app := testutil.NewApp(env)

	for i := 0; i < 20; i++ {
		if status, body, _ := requestFull(t, app, http.MethodGet, "/health", "", ""); status == http.StatusTooManyRequests {
			t.Fatalf("health request %d: got 429 (body=%s)", i+1, body)
		}
	}
}
