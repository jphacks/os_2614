//go:build integration

package content_test

import (
	"context"
	"testing"

	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/content"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/testutil"
)

// feed.updated（10 §7.3、17 §3.4）の宛先を確かめる。

func newFeedEventHarness(t *testing.T) (*testutil.Env, *content.InstanceService, *testutil.FakeBroker) {
	t.Helper()
	env := testutil.NewEnv(t)
	broker := testutil.NewFakeBroker()
	access := &access.AccessService{Deps: env.Deps, Zones: env.Zones}
	inst := &content.InstanceService{
		Deps: env.Deps, Store: env.Store, Access: access, Assessor: moderation.ModerationNoop{},
		Broker: broker, Signer: storage.NewSigner(env.Config.JWTKey),
	}
	return env, inst, broker
}

func TestGroupPostEmitsFeedUpdatedGroups(t *testing.T) {
	ctx := context.Background()
	env, inst, broker := newFeedEventHarness(t)
	const author, member = "did:plc:gauthor", "did:plc:gmember"
	env.CreateUser(t, author, "gauthor.test")
	env.CreateUser(t, member, "gmember.test")
	env.Consent(t, author)

	testutil.Space(t, env, "sp-group", "jp.i32.chain.space.group", "active", author)
	testutil.Group(t, env, "sp-group", true)
	testutil.Member(t, env, "sp-group", author, true)
	testutil.Member(t, env, "sp-group", member, false)

	if _, err := inst.CreatePosts(ctx, author, content.CreatePostInput{
		SpaceIDs: []string{"sp-group"}, Text: "グループへの投稿", Format: "plain",
	}); err != nil {
		t.Fatalf("CreatePosts(group): %v", err)
	}

	events := broker.EventsFor("feed.updated", "")
	if len(events) != 1 {
		t.Fatalf("feed.updated(group) = %+v, want 1", events)
	}
	if events[0].User != member || events[0].Space != "" {
		t.Fatalf("event = %+v, want user %s", events[0], member)
	}
	data, ok := events[0].Data.(map[string]string)
	if !ok || data["source"] != "groups" {
		t.Fatalf("data = %+v, want source=groups", events[0].Data)
	}
	if got := broker.EventsFor("feed.updated", author); len(got) != 0 {
		t.Fatalf("author got feed.updated = %+v, want none", got)
	}
}

func TestPartyPostDoesNotEmitFeedUpdated(t *testing.T) {
	ctx := context.Background()
	env, inst, broker := newFeedEventHarness(t)
	const author, member = "did:plc:pauthor", "did:plc:pmember"
	env.CreateUser(t, author, "pauthor.test")
	env.CreateUser(t, member, "pmember.test")
	env.Consent(t, author)

	now := env.Clock.Now().UnixMilli()
	testutil.Space(t, env, "sp-party", "jp.i32.chain.space.party", "active", author)
	testutil.Exec(t, env, `INSERT INTO parties (space_id, phase, tentative_at, confirmed_at, last_activity_at)
		VALUES ('sp-party', 'confirmed', ?, ?, ?)`, now, now, now)
	testutil.Member(t, env, "sp-party", author, true)
	testutil.Member(t, env, "sp-party", member, false)

	if _, err := inst.CreatePosts(ctx, author, content.CreatePostInput{
		SpaceIDs: []string{"sp-party"}, Text: "Party への投稿", Format: "plain",
	}); err != nil {
		t.Fatalf("CreatePosts(party): %v", err)
	}

	if got := broker.EventsFor("feed.updated", ""); len(got) != 0 {
		t.Fatalf("feed.updated(party) = %+v, want none", got)
	}
}

func TestPublicPostEmitsFeedUpdatedHome(t *testing.T) {
	ctx := context.Background()
	env := testutil.NewEnv(t)
	broker := testutil.NewFakeBroker()
	env.Broker = broker
	const author, friend, other = "did:plc:homeauthor", "did:plc:homefriend", "did:plc:homeother"
	env.CreateUser(t, author, "homeauthor.test")
	env.CreateUser(t, friend, "homefriend.test")
	env.CreateUser(t, other, "homeother.test")
	env.Login(t, author)
	sid := testutil.SessionID(t, env, author)

	a, b := testutil.Pair(author, friend)
	testutil.Exec(t, env, `INSERT INTO friendships (user_a, user_b, created_at, via) VALUES (?, ?, ?, 'space')`,
		a, b, env.Clock.Now().UnixMilli())

	if _, err := env.PublicPosts().Create(ctx, author, sid, content.CreatePublicPostInput{Text: "公開投稿"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := broker.EventsFor("feed.updated", author); len(got) != 1 {
		t.Fatalf("author feed.updated = %+v, want 1", got)
	}
	friendEvents := broker.EventsFor("feed.updated", friend)
	if len(friendEvents) != 1 {
		t.Fatalf("friend feed.updated = %+v, want 1", friendEvents)
	}
	data, ok := friendEvents[0].Data.(map[string]string)
	if !ok || data["source"] != "home" {
		t.Fatalf("data = %+v, want source=home", friendEvents[0].Data)
	}
	if got := broker.EventsFor("feed.updated", other); len(got) != 0 {
		t.Fatalf("other feed.updated = %+v, want none", got)
	}
}
