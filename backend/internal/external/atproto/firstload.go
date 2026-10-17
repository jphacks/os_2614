package atproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// 初回の読み込み（specification/backend/17-feed.md §2.5）。
// ユーザーが初めてログインしたとき、PDS の listRecords で jp.i32.chain.feed.post を新しい順に読む。
// 認証は要らない（record は公開）。HTTP クライアントはテストで差し替えられる。

// FeedPost は listRecords が返す 1 件。
type FeedPost struct {
	URI   string
	CID   string
	Value json.RawMessage
}

// FirstLoadFetcher は com.atproto.repo.listRecords を認証なしで呼ぶ。
type FirstLoadFetcher struct {
	Client *http.Client
}

func NewFirstLoadFetcher() *FirstLoadFetcher {
	return &FirstLoadFetcher{Client: &http.Client{Timeout: 30 * time.Second}}
}

// ListFeedPosts は jp.i32.chain.feed.post を新しい順に最大 limit 件読む（17 §2.5）。
func (f *FirstLoadFetcher) ListFeedPosts(ctx context.Context, pdsURL, did string, limit int) ([]FeedPost, error) {
	q := url.Values{
		"repo":       {did},
		"collection": {CollectionFeedPost},
		"limit":      {strconv.Itoa(limit)},
		"reverse":    {"true"}, // 新しい順
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		pdsURL+"/xrpc/com.atproto.repo.listRecords?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listRecords: status %d", resp.StatusCode)
	}

	var body struct {
		Records []struct {
			URI   string          `json:"uri"`
			CID   string          `json:"cid"`
			Value json.RawMessage `json:"value"`
		} `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	posts := make([]FeedPost, 0, len(body.Records))
	for _, r := range body.Records {
		posts = append(posts, FeedPost{URI: r.URI, CID: r.CID, Value: r.Value})
	}
	return posts, nil
}
