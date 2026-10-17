// Package testutil は結合テストの共通の環境（Redis と SQLite、手で進める時計）を作る。
// 方針は specification/backend/15-testing.md §3。
package testutil

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/auth"
	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/config"
	"github.com/applemango/chain/internal/controller"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/idgen"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/storage"
)

// Epoch は結合テストが基準にする固定の現在時刻。
var Epoch = time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)

// Env は結合テストの環境。service は Deps をそのまま渡して組み立てる。
type Env struct {
	service.Deps
	// Clock は手で進められる時計。Deps.Clock と同じ実体を指す。
	Clock *clock.Fake
	// Store は instance の record を読み書きする SQLite 実装。
	Store *model.SQLiteStore
	// Zones はグループの位置判定の fake。テストが Set / SetStale で制御する。
	Zones *FakeZonePresence
	// Moderator は group の name / description の判定に使う。nil なら Noop。
	Moderator service.Moderator
	// Assessor は instance の投稿の判定に使う。nil なら Moderator が PostAssessor を満たせばそれ、でなければ Noop。
	Assessor service.PostAssessor
	// Writer は PDS への record 書き込み（/me/status）に使う。nil なら空の FakeRecordWriter。
	Writer *FakeRecordWriter
	// Embedder は自動グループの embedding（09 §8.3）。nil なら計算しない（テストの既定）。
	Embedder service.Embedder
	// RateLimit は NewApp が使うレート制限の設定。nil なら e.Redis と DefaultLimits を使う。
	// 壊れた Redis や緩めた上限を試すときに差し替える。
	RateLimit *httpx.RateLimitConfig
	// oauth は NewApp の AuthService と AccountService が共有する fake。
	oauth *fakeOAuth
}

// OAuthLogoutCalls は fake の OAuthClient.Logout が受け取った ATProto セッション ID を返す。
func (e *Env) OAuthLogoutCalls() []string { return e.oauth.logoutCalls() }

// Storage は config の MediaDir を指すメディアのストア。
func (e *Env) Storage() *storage.Store { return storage.NewStore(e.Config.MediaDir) }

// Account は main.go と同じ配線のアカウント削除サービス。
func (e *Env) Account() *service.AccountService {
	return &service.AccountService{Deps: e.Deps, OAuth: e.oauth, Storage: e.Storage()}
}

// Janitor は main.go と同じ配線の掃除サービス。
func (e *Env) Janitor() *service.Janitor {
	return &service.Janitor{Deps: e.Deps, Storage: e.Storage()}
}

// Stats は main.go と同じ配線の統計サービス（13 §3）。
func (e *Env) Stats() *service.StatsService {
	return &service.StatsService{Deps: e.Deps, Encounters: &service.EncounterService{Deps: e.Deps}}
}

// Digest は main.go と同じ配線のダイジェスト（12 §5）。
func (e *Env) Digest() *service.Digest { return &service.Digest{Deps: e.Deps} }

// Schedule は main.go と同じ配線の予定サービス（09 §8.2）。
func (e *Env) Schedule() *service.ScheduleService {
	return &service.ScheduleService{Deps: e.Deps, Embedder: e.Embedder}
}

// NaturalGroups は main.go と同じ配線の自動グループ（09 §8）。Broker は呼び出し側で差し込める。
func (e *Env) NaturalGroups() *service.NaturalGroupService {
	return &service.NaturalGroupService{Deps: e.Deps, Moderator: service.ModerationNoop{}}
}

// PublicPosts は main.go と同じ配線の公開投稿サービス（17 §2）。
func (e *Env) PublicPosts() *service.PublicPostService {
	moderator := e.Moderator
	if moderator == nil {
		moderator = service.ModerationNoop{}
	}
	return &service.PublicPostService{Deps: e.Deps, Writer: e.Writer, Moderator: moderator}
}

// Jetstream は main.go と同じ配線の Jetstream サービス（03 §5、17 §2.5）。
// posts は初回の読み込みに使う。nil なら listRecords は呼ばれない。
func (e *Env) Jetstream(posts service.PublicPostLister) *service.JetstreamService {
	return &service.JetstreamService{Deps: e.Deps, Posts: posts}
}

// NewEnv は Redis と SQLite を用意する。Redis に接続できなければテストを skip する。
// SQLite は t.TempDir() に新しく作り、schema.sql だけを流す（seed.sql は流さない）。
func NewEnv(t *testing.T) *Env {
	t.Helper()
	ctx := context.Background()

	url := os.Getenv("CHAIN_TEST_REDIS_URL")
	if url == "" {
		url = "redis://localhost:6379/15"
	}
	rdb, err := kv.Open(ctx, url)
	if err != nil {
		t.Skipf("Redis に接続できないので結合テストを skip します: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("FLUSHDB: %v", err)
	}

	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("SQLite を開けません: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.ApplySchema(ctx, conn); err != nil {
		t.Fatalf("schema.sql: %v", err)
	}

	clk := clock.NewFake(Epoch)
	cfg := &config.Config{
		Env:         config.EnvDev,
		Listen:      ":8080",
		PublicURL:   "https://chain.test",
		AppRedirect: "chain://auth/callback",
		MediaDir:    filepath.Join(dir, "media"),
		SecretKey:   bytes.Repeat([]byte{1}, 32),
		JWTKey:      bytes.Repeat([]byte{2}, 32),
	}
	deps := service.Deps{Config: cfg, DB: conn, Redis: rdb, Clock: clk, IDs: &idgen.Seq{}}
	store := &atproto.Store{DB: conn, Redis: rdb, SecretKey: cfg.SecretKey, Clock: clk}
	return &Env{
		Deps:   deps,
		Clock:  clk,
		Store:  model.NewSQLiteStore(conn, clk, idgen.NewSeqTID()),
		Zones:  NewFakeZonePresence(),
		Writer: NewFakeRecordWriter(),
		oauth: &fakeOAuth{
			store: store,
			session: &oauth.ClientSessionData{
				SessionID:  "atp-fake",
				AccountDID: syntax.DID("did:plc:fake"),
				HostURL:    "https://pds.example",
			},
		},
	}
}

// CreateUser は users・profiles・user_settings をまとめて用意する。
// DID は呼び出し側が固定できるので、テストの期待値にそのまま使える。
func (e *Env) CreateUser(t *testing.T, did, handle string) {
	t.Helper()
	ctx := context.Background()
	now := e.Clock.Now()

	if err := model.UpsertUser(ctx, e.DB, did, handle, "https://pds.example", now); err != nil {
		t.Fatalf("users を作れません (did=%s): %v", did, err)
	}
	if err := model.UpsertProfile(ctx, e.DB, model.Profile{DID: did, DisplayName: handle, UpdatedAt: now}); err != nil {
		t.Fatalf("profiles を作れません (did=%s): %v", did, err)
	}
	if err := model.EnsureSettings(ctx, e.DB, did, now); err != nil {
		t.Fatalf("user_settings を作れません (did=%s): %v", did, err)
	}
}

// Consent は content_scan_consent を最新版にする（投稿には同意が要る。02 §8）。
func (e *Env) Consent(t *testing.T, did string) {
	t.Helper()
	v := service.ContentScanConsentVersion
	if err := model.SetContentScanConsent(context.Background(), e.DB, did, &v, e.Clock.Now()); err != nil {
		t.Fatalf("同意を保存できません (did=%s): %v", did, err)
	}
}

// Login は ATProto セッションとアプリセッションを作り、その sid で access JWT を返す。
// 同じ DID で繰り返し呼んでも ATProto セッションは 1 つのまま（ON CONFLICT DO NOTHING）、
// アプリセッションは毎回新しい sid になる。
func (e *Env) Login(t *testing.T, did string) string {
	t.Helper()
	ctx := context.Background()
	now := e.Clock.Now()

	atpID := "atp-" + did
	if _, err := db.Execute(ctx, e.DB, `
		INSERT INTO atproto_oauth_sessions (id, did, data_enc, created_at, updated_at)
		VALUES (?, ?, x'', ?, ?)
		ON CONFLICT (id) DO NOTHING`,
		atpID, did, now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatalf("atproto_oauth_sessions を作れません (did=%s): %v", did, err)
	}

	sid := e.IDs.NewID()
	if _, err := db.Execute(ctx, e.DB, `
		INSERT INTO app_sessions (id, did, atproto_session, family_id, refresh_hash, device_name, created_at, last_used_at, expires_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		sid, did, atpID, sid, "hash-"+sid, "test",
		now.UnixMilli(), now.UnixMilli(), now.Add(30*24*time.Hour).UnixMilli()); err != nil {
		t.Fatalf("app_sessions を作れません (did=%s): %v", did, err)
	}

	token, err := auth.IssueAccess(e.Config.JWTKey, did, sid, now)
	if err != nil {
		t.Fatalf("access token を発行できません (did=%s): %v", did, err)
	}
	return token
}

// assessorFor は instance の投稿の判定器を選ぶ。Env.Assessor があればそれ、
// 無ければ Moderator が PostAssessor を満たせばそれ、それも無ければ Noop。
func assessorFor(e *Env) service.PostAssessor {
	if e.Assessor != nil {
		return e.Assessor
	}
	if pa, ok := e.Moderator.(service.PostAssessor); ok {
		return pa
	}
	return service.ModerationNoop{}
}

// NewApp は controller まで含めた fiber.App を fake の外部依存で組み立てる。
// main.go と同じ配線だが、OAuth と identity はテスト用の fake に差し替える。
func NewApp(e *Env) *fiber.App {
	authSvc := &service.AuthService{Deps: e.Deps, OAuth: e.oauth, Dir: fakeDir{}, Profiles: &fakeProfiles{}, FirstLoad: fakeFirstLoad{}}
	sessions := &service.SessionService{Deps: e.Deps}
	health := &service.HealthService{Deps: e.Deps}
	moderator := e.Moderator
	if moderator == nil {
		moderator = service.ModerationNoop{}
	}
	me := &service.MeService{Deps: e.Deps, Moderator: moderator, Writer: e.Writer}
	schedule := e.Schedule()
	user := &service.UserService{Deps: e.Deps}
	beacon := &service.BeaconService{Deps: e.Deps}
	hub := realtime.NewHub()
	location := &service.LocationService{Deps: e.Deps, Hooks: &service.GroupEntryService{Deps: e.Deps, Broker: hub}}
	locationShares := &service.LocationShareService{Deps: e.Deps}
	encounters := &service.EncounterService{Deps: e.Deps, Proximity: &service.BLEPeerProximity{Deps: e.Deps}, Broker: hub}
	stats := &service.StatsService{Deps: e.Deps, Encounters: encounters}
	blocks := &service.BlockService{Deps: e.Deps}
	friends := &service.FriendService{Deps: e.Deps, Broker: hub}
	notifications := &service.NotificationService{Deps: e.Deps}

	mediaStore := e.Storage()
	signer := storage.NewSigner(e.Config.JWTKey)
	zones := &service.GPSZonePresence{Deps: e.Deps}
	access := &service.AccessService{Deps: e.Deps, Zones: zones}
	realtimeSvc := &service.RealtimeService{Deps: e.Deps, Access: access, Sessions: sessions}
	mediaSvc := &service.MediaService{Deps: e.Deps, Store: e.Store, Access: access, Broker: hub, Storage: mediaStore, Signer: signer}
	pins := &service.PinService{Deps: e.Deps, Store: e.Store, Access: access, Broker: hub}
	publicPosts := &service.PublicPostService{Deps: e.Deps, Writer: e.Writer, Moderator: moderator}
	instances := &service.InstanceService{Deps: e.Deps, Store: e.Store, Access: access, Assessor: assessorFor(e), Broker: hub, Signer: signer, Pins: pins, Public: publicPosts}
	feed := &service.FeedService{Deps: e.Deps, Access: access, Signer: signer}
	groups := &service.GroupService{Deps: e.Deps, Zones: zones, Access: access, Moderator: moderator, Broker: hub}
	groupVotes := &service.GroupVoteService{Deps: e.Deps, Access: access, Broker: hub}
	groupInvites := &service.GroupInviteService{Deps: e.Deps, Zones: zones, Access: access, Broker: hub}
	groups.Invites = groupInvites
	parties := &service.PartyService{
		Deps: e.Deps, Proximity: &service.BLEPeerProximity{Deps: e.Deps}, Access: access,
		Moderator: moderator, Broker: hub, Signer: signer, Beacon: beacon, Store: e.Store,
	}
	devices := &service.DeviceService{Deps: e.Deps}
	account := &service.AccountService{Deps: e.Deps, OAuth: e.oauth, Storage: mediaStore}

	app := fiber.New(fiber.Config{
		AppName:      "chain-test",
		ErrorHandler: httpx.ErrorHandler,
		BodyLimit:    16 << 20,
	})

	rateLimit := httpx.RateLimitConfig{Redis: e.Redis, Clock: e.Clock}
	if e.RateLimit != nil {
		rateLimit = *e.RateLimit
	}
	controller.Mount(app, httpx.RequireAuth(e.Config.JWTKey, e.Clock, sessions), httpx.RateLimit(rateLimit), controller.Routes{
		Root: []controller.Registrar{
			&controller.HealthController{Health: health},
			&controller.OAuthController{Config: e.Config, Auth: authSvc},
			&controller.MediaDeliveryController{Media: mediaSvc},
		},
		Public: []controller.Registrar{
			&controller.AuthController{Auth: authSvc},
			&controller.WSController{
				Realtime: realtimeSvc, Hub: hub, Timers: clock.Real{},
				Config: controller.RealtimeConfig{
					AuthTimeout:     500 * time.Millisecond,
					PingInterval:    100 * time.Millisecond,
					PongTimeout:     100 * time.Millisecond,
					RecheckInterval: time.Hour,
					TokenGrace:      time.Second,
				},
			},
		},
		Private: []controller.Registrar{
			&controller.LogoutController{Auth: authSvc},
			&controller.MeController{Me: me},
			&controller.ScheduleController{Schedule: schedule},
			&controller.StatsController{Stats: stats},
			&controller.UserController{User: user},
			&controller.DeviceController{Devices: devices},
			&controller.BeaconController{Beacon: beacon},
			&controller.LocationController{Location: location},
			&controller.LocationShareController{Shares: locationShares},
			&controller.EncounterController{Encounters: encounters},
			&controller.BlockController{Blocks: blocks},
			&controller.FriendController{Friends: friends},
			&controller.NotificationController{Notifications: notifications},
			&controller.InstanceController{Instances: instances, Pins: pins, Feed: feed},
			&controller.FeedPostController{Posts: publicPosts},
			&controller.FeedController{Feed: feed},
			&controller.MediaController{Media: mediaSvc},
			&controller.GroupController{Groups: groups},
			&controller.GroupVoteController{Votes: groupVotes},
			&controller.GroupInviteController{Invites: groupInvites},
			&controller.PartyController{Parties: parties},
			&controller.MemoryController{Parties: parties},
			&controller.AccountController{Account: account},
		},
	})
	return app
}
