package atproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/fasthttp/websocket"
)

// Jetstream の購読（specification/backend/03-auth-atproto.md §5）。
// WebSocket でイベントを受け取り、型付きの JetstreamEvent にして呼び出し側へ渡す。
// 切断時は指数バックオフで再接続する。カーソルは読むだけで、保存は service が行う。

// イベントの種類（03 §5）。
const (
	JetstreamKindCommit   = "commit"
	JetstreamKindIdentity = "identity"
	JetstreamKindAccount  = "account"
)

// 購読・取り込みの対象の collection（03 §5・§6）。
const (
	CollectionFeedPost     = "jp.i32.chain.feed.post"
	CollectionChainProfile = "jp.i32.chain.actor.profile"
	CollectionBskyProfile  = "app.bsky.actor.profile"

	// wantChainCollections は CHAIN の record をまとめて購読するワイルドカード（03 §5）。
	wantChainCollections = "jp.i32.chain.*"

	// MaxFeedPostRunes は feed.post の text の上限（03 §6 の maxGraphemes 1000）。
	MaxFeedPostRunes = 1000
)

// jetstreamResumeMarginUS は再接続時に巻き戻す幅（5 秒、マイクロ秒）。重複は upsert が吸収する（03 §5）。
const jetstreamResumeMarginUS int64 = 5_000_000

// JetstreamEvent は Jetstream が配る 1 イベント。
type JetstreamEvent struct {
	DID      string             `json:"did"`
	TimeUS   int64              `json:"time_us"`
	Kind     string             `json:"kind"`
	Commit   *JetstreamCommit   `json:"commit,omitempty"`
	Identity *JetstreamIdentity `json:"identity,omitempty"`
	Account  *JetstreamAccount  `json:"account,omitempty"`
}

// JetstreamCommit は commit イベントの中身。
type JetstreamCommit struct {
	Rev        string          `json:"rev"`
	Operation  string          `json:"operation"` // create / update / delete
	Collection string          `json:"collection"`
	RKey       string          `json:"rkey"`
	Record     json.RawMessage `json:"record,omitempty"`
	CID        string          `json:"cid,omitempty"`
}

// JetstreamIdentity は identity イベントの中身。
type JetstreamIdentity struct {
	DID    string `json:"did"`
	Handle string `json:"handle"`
}

// JetstreamAccount は account イベントの中身。active=false は停止・削除を表す（03 §5）。
type JetstreamAccount struct {
	DID    string `json:"did"`
	Active bool   `json:"active"`
	Status string `json:"status,omitempty"`
}

// FeedPostURI は公開投稿の正規の URI（at://{did}/jp.i32.chain.feed.post/{rkey}、17 §2）。
func FeedPostURI(did, rkey string) string {
	return "at://" + did + "/" + CollectionFeedPost + "/" + rkey
}

// RKeyFromURI は at-uri の rkey を返す。壊れていれば空文字。
func RKeyFromURI(uri string) string {
	u, err := syntax.ParseATURI(uri)
	if err != nil {
		return ""
	}
	return u.RecordKey().String()
}

// BskyRef は Bluesky 側の投稿への参照（17 §2.3）。
type BskyRef struct {
	URI string
	CID string
}

// FeedPostRecord は検証済みの jp.i32.chain.feed.post の値（03 §6、17 §2.3）。
type FeedPostRecord struct {
	Text      string
	Langs     []string
	Bsky      *BskyRef
	CreatedAt time.Time
}

// ParseFeedPost は record を検証する。text が文字列で 1000 grapheme 以内、createdAt が日時であること（03 §6）。
// 本文はログに出さない（02 §7）ため、エラーには本文を含めない。
func ParseFeedPost(raw json.RawMessage) (*FeedPostRecord, error) {
	var v struct {
		Text  *string  `json:"text"`
		Langs []string `json:"langs"`
		Bsky  *struct {
			URI string `json:"uri"`
			CID string `json:"cid"`
		} `json:"bsky"`
		CreatedAt string `json:"createdAt"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("feed.post: %w", err)
	}
	if v.Text == nil {
		return nil, errors.New("feed.post: text is missing or not a string")
	}
	if n := utf8.RuneCountInString(*v.Text); n > MaxFeedPostRunes {
		return nil, fmt.Errorf("feed.post: text has %d graphemes (max %d)", n, MaxFeedPostRunes)
	}
	if v.CreatedAt == "" {
		return nil, errors.New("feed.post: createdAt is missing")
	}
	created, err := time.Parse(time.RFC3339, v.CreatedAt)
	if err != nil {
		return nil, errors.New("feed.post: createdAt is not a datetime")
	}
	rec := &FeedPostRecord{Text: *v.Text, Langs: v.Langs, CreatedAt: created}
	if v.Bsky != nil && v.Bsky.URI != "" {
		rec.Bsky = &BskyRef{URI: v.Bsky.URI, CID: v.Bsky.CID}
	}
	return rec, nil
}

// BskyProfileRecord は app.bsky.actor.profile の値（13 §1.1）。
type BskyProfileRecord struct {
	DisplayName string
	Description string
	AvatarCID   string
	BannerCID   string
}

// ParseBskyProfile は app.bsky.actor.profile の record から表示に使う項目を読む（13 §1.1）。
func ParseBskyProfile(raw json.RawMessage) (*BskyProfileRecord, error) {
	var v struct {
		DisplayName string   `json:"displayName"`
		Description string   `json:"description"`
		Avatar      *blobRef `json:"avatar"`
		Banner      *blobRef `json:"banner"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("actor.profile: %w", err)
	}
	p := &BskyProfileRecord{DisplayName: v.DisplayName, Description: v.Description}
	if v.Avatar != nil {
		p.AvatarCID = v.Avatar.Ref.Link
	}
	if v.Banner != nil {
		p.BannerCID = v.Banner.Ref.Link
	}
	return p, nil
}

// ParseChainProfile は jp.i32.chain.actor.profile の status を読む（03 §6、13 §1.1）。
func ParseChainProfile(raw json.RawMessage) (string, error) {
	var v struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("chain.profile: %w", err)
	}
	return v.Status, nil
}

// JetstreamConn は Jetstream への接続。テストでは fake に差し替える。
type JetstreamConn interface {
	ReadMessage() (int, []byte, error)
	Close() error
}

// JetstreamDialer は URL に接続する。テストでは fake に差し替える。
type JetstreamDialer interface {
	Dial(ctx context.Context, url string) (JetstreamConn, error)
}

type wsDialer struct{ dialer *websocket.Dialer }

func (d wsDialer) Dial(ctx context.Context, url string) (JetstreamConn, error) {
	c, _, err := d.dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// JetstreamOptions は Jetstream の設定。URL と Dialer はテストで差し替えられる（03 §5）。
type JetstreamOptions struct {
	// URL は購読先（CHAIN_JETSTREAM_URL）。wantedCollections と cursor はここに足す。
	URL string
	// Cursor は保存済みの cursor_us を返す。nil なら 0 とみなす。再接続時は保存値の 5 秒前から再開する。
	Cursor func(context.Context) (int64, error)
	// Dialer は WebSocket の接続。nil なら実接続。
	Dialer JetstreamDialer
	// BackoffBase / BackoffMax は再接続の待ち時間。既定は 1 秒 / 60 秒。
	BackoffBase time.Duration
	BackoffMax  time.Duration
}

// Jetstream は Jetstream の WebSocket クライアント。接続・再接続と、イベントの型付き配布を担う。
type Jetstream struct {
	url         string
	cursor      func(context.Context) (int64, error)
	dialer      JetstreamDialer
	backoffBase time.Duration
	backoffMax  time.Duration
	lastUS      int64
}

func NewJetstream(opts JetstreamOptions) *Jetstream {
	base := opts.BackoffBase
	if base <= 0 {
		base = time.Second
	}
	maxBackoff := opts.BackoffMax
	if maxBackoff <= 0 {
		maxBackoff = 60 * time.Second
	}
	dialer := opts.Dialer
	if dialer == nil {
		dialer = wsDialer{dialer: websocket.DefaultDialer}
	}
	return &Jetstream{
		url:         opts.URL,
		cursor:      opts.Cursor,
		dialer:      dialer,
		backoffBase: base,
		backoffMax:  maxBackoff,
	}
}

// LastTimeUS は最後に処理したイベントの time_us を返す（03 §5 のカーソル保存用）。
func (j *Jetstream) LastTimeUS() int64 { return atomic.LoadInt64(&j.lastUS) }

// Run は context が終わるまで接続し続け、イベントを handle に渡す。切断時は指数バックオフで再接続する（03 §5）。
func (j *Jetstream) Run(ctx context.Context, handle func(context.Context, JetstreamEvent) error) error {
	backoff := j.backoffBase
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		connected, err := j.runOnce(ctx, handle)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			slog.WarnContext(ctx, "jetstream disconnected; reconnecting", "err", err)
		}
		// 1 つでもイベントを処理できた接続は健全とみなし、バックオフを戻す。
		if connected {
			backoff = j.backoffBase
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = nextBackoff(backoff, j.backoffMax)
	}
}

// runOnce は 1 回の接続で、切れるまでイベントを読む。connected は 1 つ以上イベントを読めたか。
func (j *Jetstream) runOnce(ctx context.Context, handle func(context.Context, JetstreamEvent) error) (bool, error) {
	endpoint, err := j.subscribeURL(ctx)
	if err != nil {
		return false, err
	}
	conn, err := j.dialer.Dial(ctx, endpoint)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()

	connected := false
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return connected, err
		}
		var ev JetstreamEvent
		if err := json.Unmarshal(msg, &ev); err != nil {
			slog.WarnContext(ctx, "jetstream: dropping undecodable frame", "err", err)
			continue
		}
		connected = true
		if handle == nil {
			continue
		}
		if err := handle(ctx, ev); err != nil {
			slog.WarnContext(ctx, "jetstream: handling event failed", "err", err, "kind", ev.Kind)
			continue
		}
		if ev.TimeUS > 0 {
			atomic.StoreInt64(&j.lastUS, ev.TimeUS)
		}
	}
}

// subscribeURL は購読先を組み立てる。cursor は保存値の 5 秒前（再接続時の巻き戻し、03 §5）。
func (j *Jetstream) subscribeURL(ctx context.Context) (string, error) {
	u, err := url.Parse(j.url)
	if err != nil {
		return "", fmt.Errorf("jetstream url: %w", err)
	}
	q := u.Query()
	q.Add("wantedCollections", wantChainCollections)
	q.Add("wantedCollections", CollectionBskyProfile)
	q.Add("wantedCollections", CollectionBskyFollow)
	q.Add("wantedCollections", CollectionBskyBlock)
	if j.cursor != nil {
		stored, err := j.cursor(ctx)
		if err != nil {
			return "", err
		}
		if stored > 0 {
			resume := stored - jetstreamResumeMarginUS
			if resume < 0 {
				resume = 0
			}
			q.Set("cursor", strconv.FormatInt(resume, 10))
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// nextBackoff は次の待ち時間を返す。1 秒から 2 倍にし、上限で止める（03 §5）。
func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next >= max {
		return max
	}
	return next
}
