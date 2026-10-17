//go:build integration

package content_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/applemango/chain/internal/external/nanogpt"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/service/access"
	"github.com/applemango/chain/internal/service/content"
	"github.com/applemango/chain/internal/service/moderation"
	"github.com/applemango/chain/internal/service/notify"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/testutil"
)

// 11 §3・§5.4 の再判定。同期判定が失敗した投稿が pending になり、modq を消費して
// block になれば非表示 + moderation.hidden、5 回失敗したら諦めることを確かめる。

type modHarness struct {
	t      *testing.T
	env    *testutil.Env
	inst   *content.InstanceService
	queue  *content.ModerationQueueService
	broker *testutil.FakeBroker
}

func newModHarness(t *testing.T, assessor moderation.PostAssessor) *modHarness {
	t.Helper()
	env := testutil.NewEnv(t)
	env.CreateUser(t, "did:plc:member", "member.test")
	broker := testutil.NewFakeBroker()
	access := &access.AccessService{Deps: env.Deps, Zones: &proximity.GPSZonePresence{Deps: env.Deps}}
	inst := &content.InstanceService{Deps: env.Deps, Store: env.Store, Access: access, Assessor: assessor, Broker: broker, Signer: storage.NewSigner(env.Config.JWTKey)}
	queue := &content.ModerationQueueService{Deps: env.Deps, Assessor: assessor, Broker: broker}
	return &modHarness{t: t, env: env, inst: inst, queue: queue, broker: broker}
}

// writableGroup は persistent な open グループを作る（位置判定を通さず投稿できる）。
func writableGroup(t *testing.T, env *testutil.Env, id, did string) {
	t.Helper()
	testutil.MkSpace(t, env, id, "jp.i32.chain.space.group", "active")
	testutil.MkGroup(t, env, id, true, nil)
	testutil.MkMember(t, env, id, did, true, true, false)
}

func enqueue(t *testing.T, env *testutil.Env, uri string) {
	t.Helper()
	if err := model.EnqueueModerationJob(context.Background(), env.Redis, uri); err != nil {
		t.Fatalf("EnqueueModerationJob: %v", err)
	}
}

func modQueueJobs(t *testing.T, env *testutil.Env) []model.ModerationJob {
	t.Helper()
	raws, err := env.Redis.LRange(context.Background(), kv.ModerationQueueKey(), 0, -1).Result()
	if err != nil {
		t.Fatalf("LRANGE modq: %v", err)
	}
	jobs := make([]model.ModerationJob, 0, len(raws))
	for _, raw := range raws {
		var j model.ModerationJob
		if err := json.Unmarshal([]byte(raw), &j); err != nil {
			t.Fatalf("decode modq element: %v", err)
		}
		jobs = append(jobs, j)
	}
	return jobs
}

func recordMod(t *testing.T, env *testutil.Env, uri string) (label, state string) {
	t.Helper()
	if err := env.DB.QueryRowContext(context.Background(),
		"SELECT mod_label, mod_state FROM records WHERE uri = ?", uri).Scan(&label, &state); err != nil {
		t.Fatalf("select mod_label/mod_state: %v", err)
	}
	return label, state
}

func insertPendingPost(t *testing.T, env *testutil.Env, uri, spaceID, author, text string) {
	t.Helper()
	value, _ := json.Marshal(map[string]string{"text": text, "format": "plain", "createdAt": "2026-10-17T00:00:00.000Z"})
	testutil.Exec(t, env, `INSERT INTO records (uri, space_id, author_did, collection, rkey, value_json, created_at, mod_label, mod_state)
		VALUES (?, ?, ?, 'jp.i32.chain.instance.post', 'rkey1', ?, ?, 'ok', 'pending')`,
		uri, spaceID, author, string(value), env.Clock.Now().UnixMilli())
}

// errAssessor は常にエラーを返す判定器（タイムアウトなどの代わり）。
type errAssessor struct{ err error }

func (e errAssessor) Judge(context.Context, moderation.Content) (moderation.Verdict, error) {
	return moderation.Verdict{}, e.err
}

func (e errAssessor) Assess(context.Context, moderation.Content) (moderation.Verdict, bool, error) {
	return moderation.Verdict{}, false, e.err
}

func TestCreatePostStoresPendingOnModerationError(t *testing.T) {
	ctx := context.Background()

	servers := map[string]func(t *testing.T) moderation.PostAssessor{
		"429": func(t *testing.T) moderation.PostAssessor {
			return jevForStatus(t, http.StatusTooManyRequests)
		},
		"500": func(t *testing.T) moderation.PostAssessor {
			return jevForStatus(t, http.StatusInternalServerError)
		},
		"400": func(t *testing.T) moderation.PostAssessor {
			return jevForStatus(t, http.StatusBadRequest)
		},
		"401": func(t *testing.T) moderation.PostAssessor {
			return jevForStatus(t, http.StatusUnauthorized)
		},
		"timeout": func(t *testing.T) moderation.PostAssessor {
			return errAssessor{err: context.DeadlineExceeded}
		},
	}
	for name, makeAssessor := range servers {
		t.Run(name, func(t *testing.T) {
			h := newModHarness(t, makeAssessor(t))
			h.env.CreateUser(t, alice, "alice.test")
			h.env.Consent(t, alice)
			writableGroup(t, h.env, "sp1", alice)

			res, err := h.inst.CreatePosts(ctx, alice, content.CreatePostInput{SpaceIDs: []string{"sp1"}, Text: "こんにちは"})
			if err != nil {
				t.Fatalf("CreatePosts: %v", err)
			}
			if len(res.Items) != 1 {
				t.Fatalf("items = %d, want 1", len(res.Items))
			}
			uri := res.Items[0].URI
			label, state := recordMod(t, h.env, uri)
			if label != "ok" || state != "pending" {
				t.Fatalf("mod = (%q,%q), want (ok,pending)", label, state)
			}
			jobs := modQueueJobs(t, h.env)
			if len(jobs) != 1 || jobs[0].URI != uri {
				t.Fatalf("modq = %+v, want the created URI %q", jobs, uri)
			}
		})
	}
}

func jevForStatus(t *testing.T, status int) moderation.PostAssessor {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"x"}`))
	}))
	t.Cleanup(srv.Close)
	return &moderation.Jev{Client: nanogpt.NewClient(srv.URL, "k", "typesafe/jev-1.13")}
}

func TestModerationRequeueHidesOnBlock(t *testing.T) {
	ctx := context.Background()
	h := newModHarness(t, &testutil.FakeAssessor{BlockWords: []string{"誹謗"}})
	h.env.CreateUser(t, alice, "alice.test")
	writableGroup(t, h.env, "sp1", alice)

	uri := "at://did:web:chain.test/space/jp.i32.chain.space.group/sp1/" + alice + "/jp.i32.chain.instance.post/rkey1"
	insertPendingPost(t, h.env, uri, "sp1", alice, "これは誹謗です")
	enqueue(t, h.env, uri)

	if _, err := h.queue.ProcessPending(ctx); err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}

	if label, state := recordMod(t, h.env, uri); label != "block" || state != "done" {
		t.Fatalf("mod = (%q,%q), want (block,done)", label, state)
	}
	if jobs := modQueueJobs(t, h.env); len(jobs) != 0 {
		t.Fatalf("modq = %+v, want empty", jobs)
	}

	var n int
	if err := h.env.DB.QueryRowContext(ctx,
		"SELECT count(*) FROM notifications WHERE did = ? AND type = ?", alice, notify.NotificationModerationHidden).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	if n != 1 {
		t.Fatalf("moderation.hidden notifications = %d, want 1", n)
	}
	var outbox int
	if err := h.env.DB.QueryRowContext(ctx,
		"SELECT count(*) FROM notification_outbox WHERE status = 'queued'").Scan(&outbox); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outbox != 1 {
		t.Fatalf("queued outbox = %d, want 1", outbox)
	}
	if ev := h.broker.EventsFor("notification", alice); len(ev) != 1 {
		t.Fatalf("notification events = %d, want 1", len(ev))
	}
}

func TestModerationRequeueUpdatesNonBlockLabel(t *testing.T) {
	ctx := context.Background()
	h := newModHarness(t, &testutil.FakeAssessor{WarnWords: []string{"乱暴"}})
	h.env.CreateUser(t, alice, "alice.test")
	writableGroup(t, h.env, "sp1", alice)

	uri := "at://did:web:chain.test/space/jp.i32.chain.space.group/sp1/" + alice + "/jp.i32.chain.instance.post/rkey2"
	insertPendingPost(t, h.env, uri, "sp1", alice, "少し乱暴な表現")
	enqueue(t, h.env, uri)

	if _, err := h.queue.ProcessPending(ctx); err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if label, state := recordMod(t, h.env, uri); label != "warn" || state != "done" {
		t.Fatalf("mod = (%q,%q), want (warn,done)", label, state)
	}
	if jobs := modQueueJobs(t, h.env); len(jobs) != 0 {
		t.Fatalf("modq = %+v, want empty", jobs)
	}
}

func TestModerationRequeueGivesUpAfterFiveFailures(t *testing.T) {
	ctx := context.Background()
	h := newModHarness(t, errAssessor{err: context.DeadlineExceeded})
	h.env.CreateUser(t, alice, "alice.test")
	writableGroup(t, h.env, "sp1", alice)

	uri := "at://did:web:chain.test/space/jp.i32.chain.space.group/sp1/" + alice + "/jp.i32.chain.instance.post/rkey3"
	insertPendingPost(t, h.env, uri, "sp1", alice, "判定できない投稿")
	enqueue(t, h.env, uri)

	// 1 回目: 失敗して attempts=1、nextAt は未来。
	if _, err := h.queue.ProcessPending(ctx); err != nil {
		t.Fatalf("ProcessPending #1: %v", err)
	}
	jobs := modQueueJobs(t, h.env)
	if len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatalf("after #1: modq = %+v, want attempts=1", jobs)
	}
	// 時刻を進めないと再判定されない（バックオフ）。
	if _, err := h.queue.ProcessPending(ctx); err != nil {
		t.Fatalf("ProcessPending #1b: %v", err)
	}
	if jobs := modQueueJobs(t, h.env); len(jobs) != 1 || jobs[0].Attempts != 1 {
		t.Fatalf("not due should be skipped: modq = %+v", jobs)
	}

	for i := 2; i <= 5; i++ {
		h.env.Clock.Advance(time.Hour)
		if _, err := h.queue.ProcessPending(ctx); err != nil {
			t.Fatalf("ProcessPending #%d: %v", i, err)
		}
	}
	if jobs := modQueueJobs(t, h.env); len(jobs) != 0 {
		t.Fatalf("after 5 failures: modq = %+v, want empty", jobs)
	}
	if label, state := recordMod(t, h.env, uri); label != "ok" || state != "pending" {
		t.Fatalf("mod = (%q,%q), want unchanged (ok,pending)", label, state)
	}
}

func TestModerationRequeueSkipsNonPendingAndDeleted(t *testing.T) {
	ctx := context.Background()
	h := newModHarness(t, &testutil.FakeAssessor{BlockWords: []string{"誹謗"}})
	h.env.CreateUser(t, alice, "alice.test")
	writableGroup(t, h.env, "sp1", alice)

	uri := "at://did:web:chain.test/space/jp.i32.chain.space.group/sp1/" + alice + "/jp.i32.chain.instance.post/rkey4"
	insertPendingPost(t, h.env, uri, "sp1", alice, "誹謗")
	testutil.Exec(t, h.env, "UPDATE records SET mod_state = 'done' WHERE uri = ?", uri)
	enqueue(t, h.env, uri)

	if _, err := h.queue.ProcessPending(ctx); err != nil {
		t.Fatalf("ProcessPending: %v", err)
	}
	if jobs := modQueueJobs(t, h.env); len(jobs) != 0 {
		t.Fatalf("modq = %+v, want empty", jobs)
	}
	if label, _ := recordMod(t, h.env, uri); label != "ok" {
		t.Fatalf("mod_label = %q, want unchanged ok", label)
	}
}
