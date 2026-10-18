package main

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/config"
	"github.com/applemango/chain/internal/controller"
	authctrl "github.com/applemango/chain/internal/controller/auth"
	contentctrl "github.com/applemango/chain/internal/controller/content"
	encounterctrl "github.com/applemango/chain/internal/controller/encounter"
	friendctrl "github.com/applemango/chain/internal/controller/friend"
	groupctrl "github.com/applemango/chain/internal/controller/group"
	partyctrl "github.com/applemango/chain/internal/controller/party"
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/external/embed"
	"github.com/applemango/chain/internal/external/fcm"
	"github.com/applemango/chain/internal/external/nanogpt"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/idgen"
	"github.com/applemango/chain/internal/kv"
	spacemodel "github.com/applemango/chain/internal/model/space"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service"
	"github.com/applemango/chain/internal/service/access"
	authsvc "github.com/applemango/chain/internal/service/auth"
	"github.com/applemango/chain/internal/service/content"
	core "github.com/applemango/chain/internal/service/core"
	encountersvc "github.com/applemango/chain/internal/service/encounter"
	friendsvc "github.com/applemango/chain/internal/service/friend"
	groupsvc "github.com/applemango/chain/internal/service/group"
	mesvc "github.com/applemango/chain/internal/service/me"
	"github.com/applemango/chain/internal/service/moderation"
	naturalsvc "github.com/applemango/chain/internal/service/natural"
	"github.com/applemango/chain/internal/service/notify"
	partysvc "github.com/applemango/chain/internal/service/party"
	"github.com/applemango/chain/internal/service/proximity"
	"github.com/applemango/chain/internal/storage"
	"github.com/applemango/chain/internal/worker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := db.EnsureSchema(ctx, conn); err != nil {
		return err
	}

	rdb, err := kv.Open(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()

	deps := core.Deps{Config: cfg, DB: conn, Redis: rdb, Clock: clock.Real{}, IDs: idgen.Real{}}
	hub := realtime.NewHub()

	moderator, assessor := moderationClients(cfg)
	sender, err := fcmSender(ctx, cfg, deps.Clock)
	if err != nil {
		return err
	}

	oauthApp, err := atproto.NewOAuthApp(cfg, &atproto.Store{DB: conn, Redis: rdb, SecretKey: cfg.SecretKey, Clock: deps.Clock})
	if err != nil {
		return err
	}
	if cfg.OAuthLoopback() {
		slog.Info("oauth: loopback client (dev). open http://127.0.0.1:" + cfg.ListenPort() + "/auth/login?return=dev")
	}
	pdsWriter := atproto.NewWriter(oauthApp)

	sessions := &authsvc.SessionService{Deps: deps}
	health := &service.HealthService{Deps: deps}
	jetstreamSvc := &content.JetstreamService{Deps: deps, Posts: atproto.NewFirstLoadFetcher(), Broker: hub}
	authSvc := &authsvc.AuthService{Deps: deps, OAuth: oauthApp, Dir: oauthApp.Dir, Profiles: atproto.NewProfileFetcher(), FirstLoad: jetstreamSvc}
	me := &mesvc.MeService{Deps: deps, Moderator: moderator, Writer: pdsWriter}
	schedule := &naturalsvc.ScheduleService{Deps: deps, Embedder: embedderFor(cfg)}
	user := &service.UserService{Deps: deps}
	beacon := &proximity.BeaconService{Deps: deps}
	location := &proximity.LocationService{Deps: deps, Hooks: &groupsvc.GroupEntryService{Deps: deps, Broker: hub}}
	locationShares := &friendsvc.LocationShareService{Deps: deps}
	encounters := &encountersvc.EncounterService{Deps: deps, Proximity: &proximity.BLEPeerProximity{Deps: deps}, Broker: hub}
	stats := &encountersvc.StatsService{Deps: deps, Encounters: encounters}
	digest := &notify.Digest{Deps: deps}
	blocks := &service.BlockService{Deps: deps}
	friends := &friendsvc.FriendService{Deps: deps, Broker: hub}
	notifications := &notify.NotificationService{Deps: deps}
	revealer := &worker.EncounterRevealer{Deps: deps, Encounters: encounters}

	tids := idgen.NewTIDGenerator(deps.Clock, tidClockID(deps.IDs))
	store := spacemodel.NewSQLiteStore(conn, deps.Clock, tids)
	mediaStore := storage.NewStore(cfg.MediaDir)
	signer := storage.NewSigner(cfg.JWTKey)
	account := &authsvc.AccountService{Deps: deps, OAuth: oauthApp, Storage: mediaStore}
	janitorSvc := &service.Janitor{Deps: deps, Storage: mediaStore}
	access := &access.AccessService{Deps: deps, Zones: &proximity.GPSZonePresence{Deps: deps}}
	realtimeSvc := &content.RealtimeService{Deps: deps, Access: access, Sessions: sessions}
	mediaSvc := &content.MediaService{Deps: deps, Store: store, Access: access, Broker: hub, Storage: mediaStore, Signer: signer}
	pins := &content.PinService{Deps: deps, Store: store, Access: access, Broker: hub}
	publicPosts := &content.PublicPostService{Deps: deps, Writer: pdsWriter, Moderator: moderator, Broker: hub}
	instances := &content.InstanceService{Deps: deps, Store: store, Access: access, Assessor: assessor, Broker: hub, Signer: signer, Pins: pins, Public: publicPosts}
	feed := &content.FeedService{Deps: deps, Access: access, Signer: signer, Proximity: &proximity.BLEPeerProximity{Deps: deps}}
	nearbyPosts := &content.NearbyPostService{
		Deps: deps, Moderator: moderator, Broker: hub,
		Proximity: &proximity.BLEPeerProximity{Deps: deps}, TID: tids,
	}
	groups := &groupsvc.GroupService{Deps: deps, Zones: &proximity.GPSZonePresence{Deps: deps}, Access: access, Moderator: moderator, Broker: hub}
	naturalGroups := &naturalsvc.NaturalGroupService{Deps: deps, Moderator: moderator, Broker: hub}
	groupVotes := &groupsvc.GroupVoteService{Deps: deps, Access: access, Broker: hub}
	groupInvites := &groupsvc.GroupInviteService{Deps: deps, Zones: &proximity.GPSZonePresence{Deps: deps}, Access: access, Broker: hub}
	groups.Invites = groupInvites
	parties := &partysvc.PartyService{
		Deps: deps, Proximity: &proximity.BLEPeerProximity{Deps: deps}, Access: access,
		Moderator: moderator, Broker: hub, Signer: signer, Beacon: beacon, Store: store,
	}
	if err := parties.RebuildIndex(ctx); err != nil {
		return err
	}
	partyEngine := &worker.PartyEngine{Deps: deps, Parties: parties}

	devices := &notify.DeviceService{Deps: deps}
	notifier := &notify.NotifySender{Deps: deps, Sender: sender}
	modQueue := &content.ModerationQueueService{Deps: deps, Assessor: assessor, Broker: hub, Pins: pins}
	notifyWorker := &worker.NotifySender{Deps: deps, Notifier: notifier}
	requeueWorker := &worker.ModerationRequeue{Deps: deps, Queue: modQueue}
	janitorWorker := &worker.Janitor{Deps: deps, Janitor: janitorSvc}
	embedBackfill := &worker.EmbedBackfill{Deps: deps, Schedule: schedule}
	digestWorker := &worker.Digest{Deps: deps, Digest: digest}
	naturalGroupsWorker := &worker.NaturalGroups{Deps: deps, Groups: naturalGroups}
	groupVoteWorker := &worker.GroupVotes{Deps: deps, Votes: groupVotes}

	app := fiber.New(fiber.Config{
		AppName:      "chain",
		ErrorHandler: httpx.ErrorHandler,
		BodyLimit:    16 << 20, // メディアの上限 15 MB（10 §4.4）に余裕を持たせる
		// フロントは DID を encodeURIComponent して送る（did%3Aplc%3A...）。:did パラメータを復号して受ける。
		UnescapePath: true,
		// Cloudflare Tunnel 経由では接続元がすべて手元の cloudflared（ループバック）に見える。
		// ループバックから来たリクエストに限り CF-Connecting-IP を信頼し、c.IP() を本来の接続元にする
		// （/auth/* の IP 単位のレート制限が全ユーザーで共有されないように。01 §6、02 §5）。
		ProxyHeader:      "CF-Connecting-IP",
		TrustProxy:       true,
		TrustProxyConfig: fiber.TrustProxyConfig{Loopback: true},
	})
	app.Use(recover.New())
	app.Use(cors.New(cors.Config{AllowOrigins: cfg.CORSOrigins}))

	// CHAIN_DISABLE_RATE_LIMIT=1 で一時的にレート制限を無効化する（開発・負荷確認用）。
	var rateLimit fiber.Handler
	if os.Getenv("CHAIN_DISABLE_RATE_LIMIT") == "1" {
		slog.Warn("rate limit disabled by CHAIN_DISABLE_RATE_LIMIT")
	} else {
		rateLimit = httpx.RateLimit(httpx.RateLimitConfig{Redis: rdb, Clock: deps.Clock})
	}
	controller.Mount(app, httpx.RequireAuth(cfg.JWTKey, deps.Clock, sessions), rateLimit, controller.Routes{
		Root: []controller.Registrar{
			&controller.HealthController{Health: health},
			&authctrl.OAuthController{Config: cfg, App: oauthApp, Auth: authSvc},
			&contentctrl.MediaDeliveryController{Media: mediaSvc},
		},
		Public: []controller.Registrar{
			&authctrl.AuthController{Auth: authSvc},
			&controller.WSController{
				Realtime: realtimeSvc, Hub: hub, Timers: clock.Real{},
				Config: controller.DefaultRealtimeConfig(),
			},
		},
		Private: []controller.Registrar{
			&authctrl.LogoutController{Auth: authSvc},
			&controller.MeController{Me: me},
			&controller.ScheduleController{Schedule: schedule},
			&controller.StatsController{Stats: stats},
			&controller.UserController{User: user},
			&controller.DeviceController{Devices: devices},
			&encounterctrl.BeaconController{Beacon: beacon},
			&encounterctrl.LocationController{Location: location},
			&friendctrl.LocationShareController{Shares: locationShares},
			&encounterctrl.EncounterController{Encounters: encounters},
			&controller.BlockController{Blocks: blocks},
			&friendctrl.FriendController{Friends: friends},
			&controller.NotificationController{Notifications: notifications},
			&contentctrl.InstanceController{Instances: instances, Pins: pins, Feed: feed},
			&contentctrl.FeedPostController{Posts: publicPosts},
			&contentctrl.NearbyPostController{Posts: nearbyPosts},
			&contentctrl.FeedController{Feed: feed},
			&contentctrl.MediaController{Media: mediaSvc},
			&groupctrl.GroupController{Groups: groups},
			&groupctrl.GroupVoteController{Votes: groupVotes},
			&groupctrl.GroupInviteController{Invites: groupInvites},
			&partyctrl.PartyController{Parties: parties},
			&partyctrl.MemoryController{Parties: parties},
			&authctrl.AccountController{Account: account},
		},
	})

	// seed のグループを geo:groups に載せ直す（09 §5）。
	if err := groups.RebuildGeoIndex(ctx); err != nil {
		slog.WarnContext(ctx, "geo index rebuild failed", "err", err)
	}

	go runEvery(ctx, time.Minute, revealer.Tick)
	go runEvery(ctx, 5*time.Second, partyEngine.Tick)
	go runEvery(ctx, 2*time.Second, notifyWorker.Tick)
	go runEvery(ctx, 5*time.Second, requeueWorker.Tick)
	go runEvery(ctx, 10*time.Minute, janitorWorker.Tick)
	go runEvery(ctx, time.Minute, embedBackfill.Tick)
	go runEvery(ctx, time.Minute, digestWorker.Tick)
	go runEvery(ctx, 5*time.Minute, naturalGroupsWorker.Tick)
	go runEvery(ctx, time.Minute, groupVoteWorker.Tick)
	if cfg.JetstreamURL != "" {
		go runJetstream(ctx, deps, jetstreamSvc, cfg.JetstreamURL)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Listen(cfg.Listen, fiber.ListenConfig{DisableStartupMessage: true})
	}()
	slog.Info("listening", "addr", cfg.Listen, "env", cfg.Env)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return app.ShutdownWithContext(shutdownCtx)
}

func runEvery(ctx context.Context, d time.Duration, tick func(context.Context) error) {
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := tick(ctx); err != nil {
				slog.ErrorContext(ctx, "worker tick failed", "err", err)
			}
		}
	}
}

// runJetstream は Jetstream の購読を常時動かす（03 §5）。context が終わるまで戻らない。
func runJetstream(ctx context.Context, deps core.Deps, svc *content.JetstreamService, url string) {
	client := atproto.NewJetstream(atproto.JetstreamOptions{URL: url, Cursor: svc.Cursor})
	w := &worker.Jetstream{Deps: deps, Service: svc, Client: client}
	if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.ErrorContext(ctx, "jetstream worker stopped", "err", err)
	}
}

// tidClockID は TID の clock id（10 bits）を起動時の乱数で決める（10 §2）。
func tidClockID(ids idgen.Generator) uint {
	return uint(binary.BigEndian.Uint16(ids.Bytes(2))) & 0x3FF
}

// moderationClients は CHAIN_NANOGPT_KEY の有無で Jev か Noop を選ぶ（11 §5.5）。
func moderationClients(cfg *config.Config) (moderation.Moderator, moderation.PostAssessor) {
	if cfg.NanoGPTKey == "" {
		noop := moderation.ModerationNoop{}
		return noop, noop
	}
	jev := &moderation.Jev{Client: nanogpt.NewClient(cfg.NanoGPTURL, cfg.NanoGPTKey, cfg.JevModel)}
	return jev, jev
}

// embedderFor は CHAIN_EMBED_URL の有無で Embedder を選ぶ（09 §8.3）。未設定なら nil にし、計算をしない。
func embedderFor(cfg *config.Config) naturalsvc.Embedder {
	if cfg.EmbedURL == "" {
		return nil
	}
	return embed.NewClient(cfg.EmbedURL, cfg.EmbedModel, cfg.EmbedDim)
}

// fcmSender は CHAIN_FCM_CREDENTIALS の有無で実送信かログ出力だけかを選ぶ（12 §2）。
func fcmSender(ctx context.Context, cfg *config.Config, clk clock.Clock) (fcm.Sender, error) {
	if cfg.FCMCredPath == "" {
		return fcm.LogSender{}, nil
	}
	return fcm.NewClient(ctx, cfg.FCMCredPath, clk)
}
