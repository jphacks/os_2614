package atproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Profile は app.bsky.actor.profile と jp.i32.chain.actor.profile から取り出した値（13 §1.1）。
type Profile struct {
	DisplayName string
	Description string
	AvatarCID   string
	BannerCID   string
	Status      string
}

// ProfileFetcher はユーザーの PDS から公開プロフィールの record を読む。認証は要らない。
type ProfileFetcher struct {
	Client *http.Client
}

func NewProfileFetcher() *ProfileFetcher {
	return &ProfileFetcher{Client: &http.Client{Timeout: 10 * time.Second}}
}

type blobRef struct {
	Ref struct {
		Link string `json:"$link"`
	} `json:"ref"`
}

func (f *ProfileFetcher) FetchProfile(ctx context.Context, pdsURL, did string) (*Profile, error) {
	var p Profile

	var bsky struct {
		DisplayName string   `json:"displayName"`
		Description string   `json:"description"`
		Avatar      *blobRef `json:"avatar"`
		Banner      *blobRef `json:"banner"`
	}
	if _, err := f.getRecord(ctx, pdsURL, did, "app.bsky.actor.profile", &bsky); err != nil {
		return nil, err
	}
	p.DisplayName = bsky.DisplayName
	p.Description = bsky.Description
	if bsky.Avatar != nil {
		p.AvatarCID = bsky.Avatar.Ref.Link
	}
	if bsky.Banner != nil {
		p.BannerCID = bsky.Banner.Ref.Link
	}

	var chain struct {
		Status string `json:"status"`
	}
	if _, err := f.getRecord(ctx, pdsURL, did, "jp.i32.chain.actor.profile", &chain); err != nil {
		return nil, err
	}
	p.Status = chain.Status
	return &p, nil
}

// getRecord は record がなければ found=false を返す（プロフィール未設定のユーザーもいるため）。
func (f *ProfileFetcher) getRecord(ctx context.Context, pdsURL, did, collection string, v any) (found bool, err error) {
	q := url.Values{"repo": {did}, "collection": {collection}, "rkey": {"self"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pdsURL+"/xrpc/com.atproto.repo.getRecord?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		var body struct {
			Value json.RawMessage `json:"value"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return false, err
		}
		return true, json.Unmarshal(body.Value, v)
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound:
		// 未作成の record は 400 RecordNotFound で返る。
		return false, nil
	default:
		return false, fmt.Errorf("getRecord %s: status %d", collection, resp.StatusCode)
	}
}

// AvatarURL / BannerURL は Bluesky の CDN の URL（13 §1.1）。
func AvatarURL(did, cid string) string {
	if cid == "" {
		return ""
	}
	return "https://cdn.bsky.app/img/avatar/plain/" + did + "/" + cid + "@jpeg"
}

func BannerURL(did, cid string) string {
	if cid == "" {
		return ""
	}
	return "https://cdn.bsky.app/img/banner/plain/" + did + "/" + cid + "@jpeg"
}
