package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
)

func TestParseFeedPost(t *testing.T) {
	valid := `{"text":"こんにちは","langs":["ja"],"bsky":{"uri":"at://did:plc:a/app.bsky.feed.post/1","cid":"bafyreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxgf5kpqcsgz7soitae"},"createdAt":"2026-10-17T03:12:45.123Z"}`
	rec, err := ParseFeedPost(json.RawMessage(valid))
	if err != nil {
		t.Fatalf("ParseFeedPost: %v", err)
	}
	if rec.Text != "こんにちは" || len(rec.Langs) != 1 || rec.Langs[0] != "ja" {
		t.Fatalf("record = %+v", rec)
	}
	if rec.Bsky == nil || rec.Bsky.URI != "at://did:plc:a/app.bsky.feed.post/1" || rec.Bsky.CID != "bafyreib2rxk3rybk3aobmv5cjuql3bm2twh4jo5uxgf5kpqcsgz7soitae" {
		t.Fatalf("bsky = %+v", rec.Bsky)
	}
	if rec.CreatedAt.UTC().Format(time.RFC3339Nano) != "2026-10-17T03:12:45.123Z" {
		t.Fatalf("createdAt = %v", rec.CreatedAt)
	}

	tooLong := `{"text":"` + strings.Repeat("あ", MaxFeedPostRunes+1) + `","createdAt":"2026-10-17T03:00:00Z"}`
	cases := map[string]string{
		"text missing":      `{"langs":["ja"],"createdAt":"2026-10-17T03:00:00Z"}`,
		"text not string":   `{"text":123,"createdAt":"2026-10-17T03:00:00Z"}`,
		"text too long":     tooLong,
		"createdAt bad":     `{"text":"x","createdAt":"not-a-date"}`,
		"createdAt missing": `{"text":"x"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFeedPost(json.RawMessage(raw)); err == nil {
				t.Fatalf("ParseFeedPost(%s) = nil error", raw)
			}
		})
	}
}

func TestRKeyFromURI(t *testing.T) {
	if got := RKeyFromURI("at://did:plc:a/jp.i32.chain.feed.post/3kabc"); got != "3kabc" {
		t.Fatalf("rkey = %q", got)
	}
	if got := RKeyFromURI("not-a-uri"); got != "" {
		t.Fatalf("rkey = %q, want empty", got)
	}
}

func TestNextBackoffDoublesUpToMax(t *testing.T) {
	cases := []struct{ cur, max, want time.Duration }{
		{time.Second, 60 * time.Second, 2 * time.Second},
		{2 * time.Second, 60 * time.Second, 4 * time.Second},
		{30 * time.Second, 60 * time.Second, 60 * time.Second},
		{60 * time.Second, 60 * time.Second, 60 * time.Second},
	}
	for _, c := range cases {
		if got := nextBackoff(c.cur, c.max); got != c.want {
			t.Fatalf("nextBackoff(%v) = %v, want %v", c.cur, got, c.want)
		}
	}
}

func TestSubscribeURLUsesSavedCursorMinusFiveSeconds(t *testing.T) {
	stored := int64(1_000_000_000)
	j := NewJetstream(JetstreamOptions{
		URL:    "wss://jet.test/subscribe",
		Cursor: func(context.Context) (int64, error) { return stored, nil },
	})
	raw, err := j.subscribeURL(context.Background())
	if err != nil {
		t.Fatalf("subscribeURL: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if got := q.Get("cursor"); got != "995000000" {
		t.Fatalf("cursor = %q, want 995000000", got)
	}
	cols := q["wantedCollections"]
	if len(cols) != 2 || cols[0] != "jp.i32.chain.*" || cols[1] != "app.bsky.actor.profile" {
		t.Fatalf("wantedCollections = %v", cols)
	}

	// カーソルが無ければ cursor は付けない。
	empty := NewJetstream(JetstreamOptions{URL: "wss://jet.test/subscribe"})
	raw, err = empty.subscribeURL(context.Background())
	if err != nil {
		t.Fatalf("subscribeURL: %v", err)
	}
	u, _ = url.Parse(raw)
	if _, ok := u.Query()["cursor"]; ok {
		t.Fatalf("cursor should be absent: %s", raw)
	}
}

// dialFunc はテスト用の JetstreamDialer。
type dialFunc func(ctx context.Context, url string) (JetstreamConn, error)

func (f dialFunc) Dial(ctx context.Context, url string) (JetstreamConn, error) { return f(ctx, url) }

// fakeConn はキューに積んだメッセージを返す。eofAfter のときは読み切ると EOF を返し、
// そうでなければ Close されるまでブロックする。
type fakeConn struct {
	msgs     [][]byte
	i        int
	eofAfter bool
	closed   chan struct{}
	once     sync.Once
}

func (c *fakeConn) ReadMessage() (int, []byte, error) {
	if c.i < len(c.msgs) {
		m := c.msgs[c.i]
		c.i++
		return 1, m, nil
	}
	if c.eofAfter {
		return 0, nil, errors.New("eof")
	}
	<-c.closed
	return 0, nil, errors.New("connection closed")
}

func (c *fakeConn) Close() error {
	c.once.Do(func() {
		if c.closed != nil {
			close(c.closed)
		}
	})
	return nil
}

func TestJetstreamReconnectUsesResumeCursor(t *testing.T) {
	var mu sync.Mutex
	var urls []string
	first := make(chan struct{})
	second := make(chan struct{})
	dials := 0
	dial := dialFunc(func(_ context.Context, u string) (JetstreamConn, error) {
		mu.Lock()
		urls = append(urls, u)
		dials++
		n := dials
		mu.Unlock()
		switch n {
		case 1:
			close(first)
			msg := []byte(`{"did":"did:plc:a","time_us":1500000000,"kind":"identity","identity":{"did":"did:plc:a","handle":"a.test"}}`)
			return &fakeConn{msgs: [][]byte{msg}, eofAfter: true}, nil
		case 2:
			close(second)
			return &fakeConn{closed: make(chan struct{})}, nil
		default:
			return &fakeConn{closed: make(chan struct{})}, nil
		}
	})
	j := NewJetstream(JetstreamOptions{
		URL:         "wss://jet.test/subscribe",
		Cursor:      func(context.Context) (int64, error) { return 1_000_000_000, nil },
		Dialer:      dial,
		BackoffBase: time.Millisecond,
		BackoffMax:  2 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = j.Run(ctx, nil) }()

	<-first
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("再接続しなかった")
	}
	mu.Lock()
	got := urls[1]
	mu.Unlock()
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c := u.Query().Get("cursor"); c != "995000000" {
		t.Fatalf("再接続の cursor = %q, want 995000000", c)
	}
}

func TestJetstreamRetriesOnDialFailure(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	reached := make(chan struct{})
	dial := dialFunc(func(context.Context, string) (JetstreamConn, error) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 3 {
			close(reached)
		}
		return nil, errors.New("boom")
	})
	j := NewJetstream(JetstreamOptions{
		URL:         "wss://jet.test/subscribe",
		Dialer:      dial,
		BackoffBase: time.Millisecond,
		BackoffMax:  4 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = j.Run(ctx, nil) }()
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("接続失敗後に再試行しなかった")
	}
}

func TestJetstreamReceivesEventsOverWebSocket(t *testing.T) {
	events := []map[string]any{
		{"did": "did:plc:a", "time_us": 100, "kind": "identity", "identity": map[string]any{"did": "did:plc:a", "handle": "a.test"}},
		{"did": "did:plc:a", "time_us": 200, "kind": "commit", "commit": map[string]any{
			"operation": "create", "collection": "jp.i32.chain.feed.post", "rkey": "r1", "cid": "c1", "record": map[string]any{"text": "x"},
		}},
	}
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for _, e := range events {
			b, _ := json.Marshal(e)
			if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	got := make(chan JetstreamEvent, len(events))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	j := NewJetstream(JetstreamOptions{
		URL:         "ws" + strings.TrimPrefix(srv.URL, "http"),
		BackoffBase: 10 * time.Millisecond,
		BackoffMax:  20 * time.Millisecond,
	})
	go func() {
		_ = j.Run(ctx, func(_ context.Context, ev JetstreamEvent) error {
			got <- ev
			return nil
		})
	}()

	for range events {
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			t.Fatal("イベントを受け取れなかった")
		}
	}
	deadline := time.After(3 * time.Second)
	for j.LastTimeUS() != 200 {
		select {
		case <-deadline:
			t.Fatalf("LastTimeUS = %d, want 200", j.LastTimeUS())
		case <-time.After(5 * time.Millisecond):
		}
	}
}
