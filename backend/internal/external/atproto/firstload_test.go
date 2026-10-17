package atproto

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFirstLoadFetcherListFeedPosts(t *testing.T) {
	var query url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xrpc/com.atproto.repo.listRecords" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"records":[{"uri":"at://did:plc:a/jp.i32.chain.feed.post/r1","cid":"c1","value":{"text":"x","createdAt":"2026-10-17T03:00:00Z"}}]}`))
	}))
	defer srv.Close()

	f := &FirstLoadFetcher{Client: srv.Client()}
	posts, err := f.ListFeedPosts(context.Background(), srv.URL, "did:plc:a", 100)
	if err != nil {
		t.Fatalf("ListFeedPosts: %v", err)
	}
	if query.Get("repo") != "did:plc:a" || query.Get("collection") != CollectionFeedPost ||
		query.Get("limit") != "100" || query.Get("reverse") != "true" {
		t.Fatalf("query = %v", query)
	}
	if len(posts) != 1 || posts[0].CID != "c1" || posts[0].URI != "at://did:plc:a/jp.i32.chain.feed.post/r1" {
		t.Fatalf("posts = %+v", posts)
	}
}

func TestFirstLoadFetcherListFeedPostsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	f := &FirstLoadFetcher{Client: srv.Client()}
	if _, err := f.ListFeedPosts(context.Background(), srv.URL, "did:plc:a", 100); err == nil {
		t.Fatal("500 でエラーにならない")
	}
}
