package atproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Bluesky のソーシャルグラフ（specification/backend/07-friend-location.md §2.5）。
// フォローとブロックは app.bsky.graph の record をそのまま使う。

const (
	CollectionBskyFollow = "app.bsky.graph.follow"
	CollectionBskyBlock  = "app.bsky.graph.block"

	// listRecordsPageSize は 1 ページの件数（listRecords の上限は 100）。
	listRecordsPageSize = 100
	// listRecordsMaxPages は読み直しで辿るページ数の上限。フォローが 10 万件を超える人は途中で切る。
	listRecordsMaxPages = 1000
)

// Record は listRecords が返す 1 件。
type Record struct {
	URI   string
	CID   string
	Value json.RawMessage
}

// GraphRecord は検証済みの follow / block の record。Subject は相手の DID。
type GraphRecord struct {
	Subject   string
	CreatedAt time.Time
}

// ParseGraphRecord は app.bsky.graph.follow / block の record から subject と createdAt を読む。
// subject が DID でなければエラー。createdAt が読めなければゼロ値のまま返す（呼び出し側が現在時刻で補う）。
func ParseGraphRecord(raw json.RawMessage) (*GraphRecord, error) {
	var v struct {
		Subject   string `json:"subject"`
		CreatedAt string `json:"createdAt"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("graph record: %w", err)
	}
	if _, err := syntax.ParseDID(v.Subject); err != nil {
		return nil, fmt.Errorf("graph record: subject is not a DID")
	}
	rec := &GraphRecord{Subject: v.Subject}
	if t, err := time.Parse(time.RFC3339, v.CreatedAt); err == nil {
		rec.CreatedAt = t
	}
	return rec, nil
}

// GraphURI は at://{did}/{collection}/{rkey}。
func GraphURI(did, collection, rkey string) string {
	return "at://" + did + "/" + collection + "/" + rkey
}

// ListRecords は collection の record を PDS から認証なしですべて読む（公開 record。17 §2.5 と同じ方法）。
func (f *FirstLoadFetcher) ListRecords(ctx context.Context, pdsURL, did, collection string) ([]Record, error) {
	var out []Record
	cursor := ""
	for page := 0; page < listRecordsMaxPages; page++ {
		q := url.Values{
			"repo":       {did},
			"collection": {collection},
			"limit":      {strconv.Itoa(listRecordsPageSize)},
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		recs, next, err := f.listPage(ctx, pdsURL, q)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
		if next == "" || len(recs) == 0 {
			break
		}
		cursor = next
	}
	return out, nil
}

func (f *FirstLoadFetcher) listPage(ctx context.Context, pdsURL string, q url.Values) ([]Record, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		pdsURL+"/xrpc/com.atproto.repo.listRecords?"+q.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("listRecords: status %d", resp.StatusCode)
	}
	var body struct {
		Cursor  string `json:"cursor"`
		Records []struct {
			URI   string          `json:"uri"`
			CID   string          `json:"cid"`
			Value json.RawMessage `json:"value"`
		} `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, "", err
	}
	recs := make([]Record, 0, len(body.Records))
	for _, r := range body.Records {
		recs = append(recs, Record{URI: r.URI, CID: r.CID, Value: r.Value})
	}
	return recs, body.Cursor, nil
}
