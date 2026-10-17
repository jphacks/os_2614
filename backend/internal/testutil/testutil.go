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
	return &Env{
		Deps:  deps,
		Clock: clk,
		Store: model.NewSQLiteStore(conn, clk, idgen.NewSeqTID()),
		Zones: NewFakeZonePresence(),
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

// NewApp は controller まで含めた fiber.App を fake の外部依存で組み立てる。
// main.go と同じ配線だが、OAuth と identity はテスト用の fake に差し替える。
func NewApp(e *Env) *fiber.App {
	store := &atproto.Store{DB: e.DB, Redis: e.Redis, SecretKey: e.Config.SecretKey, Clock: e.Clock}
	oauthFake := &fakeOAuth{
		store: store,
		session: &oauth.ClientSessionData{
			SessionID:  "atp-fake",
			AccountDID: syntax.DID("did:plc:fake"),
			HostURL:    "https://pds.example",
		},
	}
	authSvc := &service.AuthService{Deps: e.Deps, OAuth: oauthFake, Dir: fakeDir{}, Profiles: &fakeProfiles{}}
	sessions := &service.SessionService{Deps: e.Deps}
	health := &service.HealthService{Deps: e.Deps}
	me := &service.MeService{Deps: e.Deps}
	user := &service.UserService{Deps: e.Deps}
	beacon := &service.BeaconService{Deps: e.Deps}
	location := &service.LocationService{Deps: e.Deps, Hooks: service.NoopLocationHooks{}}
	hub := realtime.NewHub()
	encounters := &service.EncounterService{Deps: e.Deps, Proximity: &service.BLEPeerProximity{Deps: e.Deps}, Broker: hub}
	blocks := &service.BlockService{Deps: e.Deps}
	friends := &service.FriendService{Deps: e.Deps, Broker: hub}
	notifications := &service.NotificationService{Deps: e.Deps}

	mediaStore := storage.NewStore(e.Config.MediaDir)
	signer := storage.NewSigner(e.Config.JWTKey)
	zones := &service.GPSZonePresence{Deps: e.Deps}
	access := &service.AccessService{Deps: e.Deps, Zones: zones}
	realtimeSvc := &service.RealtimeService{Deps: e.Deps, Access: access, Sessions: sessions}
	mediaSvc := &service.MediaService{Deps: e.Deps, Store: e.Store, Access: access, Broker: hub, Storage: mediaStore, Signer: signer}
	instances := &service.InstanceService{Deps: e.Deps, Store: e.Store, Access: access, Assessor: service.ModerationNoop{}, Broker: hub, Signer: signer}
	moderator := e.Moderator
	if moderator == nil {
		moderator = service.ModerationNoop{}
	}
	groups := &service.GroupService{Deps: e.Deps, Zones: zones, Access: access, Moderator: moderator, Broker: hub}

	app := fiber.New(fiber.Config{
		AppName:      "chain-test",
		ErrorHandler: httpx.ErrorHandler,
		BodyLimit:    16 << 20,
	})

	controller.Mount(app, httpx.RequireAuth(e.Config.JWTKey, e.Clock, sessions), controller.Routes{
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
			&controller.UserController{User: user},
			&controller.BeaconController{Beacon: beacon},
			&controller.LocationController{Location: location},
			&controller.EncounterController{Encounters: encounters},
			&controller.BlockController{Blocks: blocks},
			&controller.FriendController{Friends: friends},
			&controller.NotificationController{Notifications: notifications},
			&controller.InstanceController{Instances: instances},
			&controller.MediaController{Media: mediaSvc},
			&controller.GroupController{Groups: groups},
		},
	})
	return app
}
