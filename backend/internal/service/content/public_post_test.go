//go:build integration

package content_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/content"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/testutil"
)

// 公開投稿の作成と削除（17 §2.1・§2.2）。

const publicPostDID = "did:plc:public"

func newPublicPostHarness(t *testing.T, moderator moderation.Moderator) (*testutil.Env, *content.PublicPostService, string) {
	t.Helper()
	env := testutil.NewEnv(t)
	env.CreateUser(t, publicPostDID, "public.test")
	env.Login(t, publicPostDID)
	env.Moderator = moderator
	return env, env.PublicPosts(), testutil.SessionID(t, env, publicPostDID)
}

func lastSegment(t *testing.T, uri string) string {
	t.Helper()
	parts := strings.Split(uri, "/")
	if len(parts) < 2 {
		t.Fatalf("uri %q has no rkey", uri)
	}
	return parts[len(parts)-1]
}

func TestPublicPostCreateOrderAndIndex(t *testing.T) {
	env, svc, sid := newPublicPostHarness(t, nil)

	res, err := svc.Create(context.Background(), publicPostDID, sid, content.CreatePublicPostInput{
		Text: "今日はいい天気", Langs: []string{"ja"}, AlsoBluesky: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	calls := env.Writer.CreateCalls()
	if len(calls) != 2 {
		t.Fatalf("create calls = %d, want 2", len(calls))
	}
	if calls[0].Collection != content.CollectionBskyPost {
		t.Fatalf("1st collection = %q, want %q", calls[0].Collection, content.CollectionBskyPost)
	}
	if calls[1].Collection != content.CollectionFeedPost {
		t.Fatalf("2nd collection = %q, want %q", calls[1].Collection, content.CollectionFeedPost)
	}

	bsky, ok := calls[0].Record.(map[string]any)
	if !ok || bsky["$type"] != content.CollectionBskyPost || bsky["text"] != "今日はいい天気" {
		t.Fatalf("bluesky record = %+v", calls[0].Record)
	}
	if langs, ok := bsky["langs"].([]string); !ok || len(langs) != 1 || langs[0] != "ja" {
		t.Fatalf("bluesky langs = %+v", bsky["langs"])
	}

	feed, ok := calls[1].Record.(map[string]any)
	if !ok || feed["$type"] != content.CollectionFeedPost || feed["text"] != "今日はいい天気" {
		t.Fatalf("feed record = %+v", calls[1].Record)
	}
	ref, ok := feed["bsky"].(map[string]any)
	if !ok || ref["uri"] != res.BskyURI || ref["cid"] == "" {
		t.Fatalf("feed bsky ref = %+v, want uri %q", feed["bsky"], res.BskyURI)
	}

	if res.URI == "" || res.CID == "" || res.BskyURI == "" {
		t.Fatalf("result = %+v", res)
	}

	pp, err := model.GetPublicPost(context.Background(), env.DB, res.URI)
	if err != nil {
		t.Fatalf("GetPublicPost: %v", err)
	}
	if pp.AuthorDID != publicPostDID || pp.Text != "今日はいい天気" {
		t.Fatalf("indexed post = %+v", pp)
	}
	if pp.BskyURI != res.BskyURI || pp.BskyCID == "" {
		t.Fatalf("indexed bsky = %q/%q, want %q", pp.BskyURI, pp.BskyCID, res.BskyURI)
	}
	if pp.ModLabel != "ok" || pp.ModState != "done" {
		t.Fatalf("indexed moderation = %s/%s, want ok/done", pp.ModLabel, pp.ModState)
	}
}

func TestPublicPostRollsBackBlueskyOnFeedFailure(t *testing.T) {
	env, svc, sid := newPublicPostHarness(t, nil)
	env.Writer.SetCreateError(content.CollectionFeedPost, errors.New("pds down"))

	_, err := svc.Create(context.Background(), publicPostDID, sid, content.CreatePublicPostInput{
		Text: "こんにちは", AlsoBluesky: true,
	})
	testutil.WantHTTPError(t, err, "upstream_error")

	deletes := env.Writer.DeleteCalls()
	if len(deletes) != 1 || deletes[0].Collection != content.CollectionBskyPost {
		t.Fatalf("deletes = %+v, want 1 delete of %q", deletes, content.CollectionBskyPost)
	}
}

func TestPublicPostWithoutBlueskyWritesOnlyFeed(t *testing.T) {
	env, svc, sid := newPublicPostHarness(t, nil)

	res, err := svc.Create(context.Background(), publicPostDID, sid, content.CreatePublicPostInput{Text: "ひとりごと"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	calls := env.Writer.CreateCalls()
	if len(calls) != 1 || calls[0].Collection != content.CollectionFeedPost {
		t.Fatalf("create calls = %+v, want only feed", calls)
	}
	if feed, _ := calls[0].Record.(map[string]any); feed["bsky"] != nil {
		t.Fatalf("feed record has bsky = %+v, want none", feed["bsky"])
	}
	if res.BskyURI != "" || len(env.Writer.DeleteCalls()) != 0 {
		t.Fatalf("res = %+v, deletes = %+v", res, env.Writer.DeleteCalls())
	}
}

func TestPublicPostModerationBlocked(t *testing.T) {
	env, svc, sid := newPublicPostHarness(t, &testutil.FakeAssessor{BlockWords: []string{"誹謗"}})

	_, err := svc.Create(context.Background(), publicPostDID, sid, content.CreatePublicPostInput{Text: "これは誹謗です"})
	testutil.WantHTTPError(t, err, "moderation_blocked")
	if len(env.Writer.CreateCalls()) != 0 || len(env.Writer.DeleteCalls()) != 0 {
		t.Fatalf("writer was called: create=%+v delete=%+v", env.Writer.CreateCalls(), env.Writer.DeleteCalls())
	}
}

func TestPublicPostModerationPendingEnqueues(t *testing.T) {
	env, svc, sid := newPublicPostHarness(t, &testutil.FakeAssessor{FailWords: []string{"pending"}})

	res, err := svc.Create(context.Background(), publicPostDID, sid, content.CreatePublicPostInput{Text: "pending な内容"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	pp, err := model.GetPublicPost(context.Background(), env.DB, res.URI)
	if err != nil {
		t.Fatalf("GetPublicPost: %v", err)
	}
	if pp.ModLabel != "ok" || pp.ModState != "pending" {
		t.Fatalf("moderation = %s/%s, want ok/pending", pp.ModLabel, pp.ModState)
	}

	entries, err := model.ListModerationJobs(context.Background(), env.Redis, 10)
	if err != nil {
		t.Fatalf("ListModerationJobs: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Job.URI == res.URI {
			found = true
		}
	}
	if !found {
		t.Fatalf("modq = %+v, want %q", entries, res.URI)
	}
}

func TestPublicPostWriteErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"session expired", atproto.ErrSessionExpired, "atproto_session_expired"},
		{"reauth required", atproto.ErrReauthRequired, "atproto_reauth_required"},
		{"upstream", errors.New("pds down"), "upstream_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, svc, sid := newPublicPostHarness(t, nil)
			env.Writer.SetCreateError(content.CollectionFeedPost, tc.err)
			_, err := svc.Create(context.Background(), publicPostDID, sid, content.CreatePublicPostInput{Text: "こんにちは"})
			testutil.WantHTTPError(t, err, tc.code)
		})
	}
}

func TestPublicPostDeleteBoth(t *testing.T) {
	env, svc, sid := newPublicPostHarness(t, nil)
	res, err := svc.Create(context.Background(), publicPostDID, sid, content.CreatePublicPostInput{
		Text: "消す投稿", AlsoBluesky: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(context.Background(), publicPostDID, sid, lastSegment(t, res.URI)); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	deletes := env.Writer.DeleteCalls()
	if len(deletes) != 2 {
		t.Fatalf("delete calls = %+v, want 2", deletes)
	}
	if deletes[0].Collection != content.CollectionFeedPost || deletes[1].Collection != content.CollectionBskyPost {
		t.Fatalf("delete collections = %q, %q", deletes[0].Collection, deletes[1].Collection)
	}
	if deletes[1].RKey != lastSegment(t, res.BskyURI) {
		t.Fatalf("bluesky rkey = %q, want %q", deletes[1].RKey, lastSegment(t, res.BskyURI))
	}

	pp, err := model.GetPublicPost(context.Background(), env.DB, res.URI)
	if err != nil {
		t.Fatalf("GetPublicPost: %v", err)
	}
	if pp.DeletedAt == nil {
		t.Fatal("deleted_at = nil, want set")
	}
}

func TestPublicPostDeleteMissingRecordStillSucceeds(t *testing.T) {
	for _, collection := range []string{content.CollectionFeedPost, content.CollectionBskyPost} {
		t.Run(collection, func(t *testing.T) {
			env, svc, sid := newPublicPostHarness(t, nil)
			res, err := svc.Create(context.Background(), publicPostDID, sid, content.CreatePublicPostInput{
				Text: "消す投稿", AlsoBluesky: true,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			env.Writer.SetDeleteError(collection, atproto.ErrRecordNotFound)

			if err := svc.Delete(context.Background(), publicPostDID, sid, lastSegment(t, res.URI)); err != nil {
				t.Fatalf("Delete: %v", err)
			}
		})
	}
}

func TestPublicPostDeleteNotFound(t *testing.T) {
	env, svc, sid := newPublicPostHarness(t, nil)
	_ = env
	err := svc.Delete(context.Background(), publicPostDID, sid, "does-not-exist")
	testutil.WantHTTPError(t, err, "not_found")
}
