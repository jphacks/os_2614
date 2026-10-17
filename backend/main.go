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
	"github.com/applemango/chain/internal/db"
	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/external/embed"
	"github.com/applemango/chain/internal/external/fcm"
	"github.com/applemango/chain/internal/external/nanogpt"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/idgen"
	"github.com/applemango/chain/internal/kv"
	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/realtime"
	"github.com/applemango/chain/internal/service"
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

	deps := service.Deps{Config: cfg, DB: conn, Redis: rdb, Clock: clock.Real{}, IDs: idgen.Real{}}
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

	sessions := &service.SessionService{Deps: deps}
	health := &service.HealthService{Deps: deps}
	jetstreamSvc := &service.JetstreamService{Deps: deps, Posts: atproto.NewFirstLoadFetcher()}
	authSvc := &service.AuthService{Deps: deps, OAuth: oauthApp, Dir: oauthApp.Dir, Profiles: atproto.NewProfileFetcher(), FirstLoad: jetstreamSvc}
	me := &service.MeService{Deps: deps, Moderator: moderator, Writer: pdsWriter}
	schedule := &service.ScheduleService{Deps: deps, Embedder: embedderFor(cfg)}
	user := &service.UserService{Deps: deps}
	beacon := &service.BeaconService{Deps: deps}
	location := &service.LocationService{Deps: deps, Hooks: service.NoopLocationHooks{}}
	locationShares := &service.LocationShareService{Deps: deps}
	encounters := &service.EncounterService{Deps: deps, Proximity: &service.BLEPeerProximity{Deps: deps}, Broker: hub}
	stats := &service.StatsService{Deps: deps, Encounters: encounters}
	digest := &service.Digest{Deps: deps}
	blocks := &service.BlockService{Deps: deps}
	friends := &service.FriendService{Deps: deps, Broker: hub}
	notifications := &service.NotificationService{Deps: deps}
	revealer := &worker.EncounterRevealer{Deps: deps, Encounters: encounters}

	store := model.NewSQLiteStore(conn, deps.Clock, idgen.NewTIDGenerator(deps.Clock, tidClockID(deps.IDs)))
	mediaStore := storage.NewStore(cfg.MediaDir)
	signer := storage.NewSigner(cfg.JWTKey)
	account := &service.AccountService{Deps: deps, OAuth: oauthApp, Storage: mediaStore}
	janitorSvc := &service.Janitor{Deps: deps, Storage: mediaStore}
	// 位置判定は S3 が作る。ここでは常に location_stale を返す仮実装を差す。
	access := &service.AccessService{Deps: deps, Zones: &service.GPSZonePresence{Deps: deps}}
	realtimeSvc := &service.RealtimeService{Deps: deps, Access: access, Sessions: sessions}
	mediaSvc := &service.MediaService{Deps: deps, Store: store, Access: access, Broker: hub, Storage: mediaStore, Signer: signer}
	pins := &service.PinService{Deps: deps, Store: store, Access: access, Broker: hub}
	publicPosts := &service.PublicPostService{Deps: deps, Writer: pdsWriter, Moderator: moderator}
	instances := &service.InstanceService{Deps: deps, Store: store, Access: access, Assessor: assessor, Broker: hub, Signer: signer, Pins: pins, Public: publicPosts}
	feed := &service.FeedService{Deps: deps, Access: access, Signer: signer}
	groups := &service.GroupService{Deps: deps, Zones: &service.GPSZonePresence{Deps: deps}, Access: access, Moderator: moderator, Broker: hub}
	naturalGroups := &service.NaturalGroupService{Deps: deps, Moderator: moderator, Broker: hub}
	groupVotes := &service.GroupVoteService{Deps: deps, Access: access, Broker: hub}
	parties := &service.PartyService{
		Deps: deps, Proximity: &service.BLEPeerProximity{Deps: deps}, Access: access,
		Moderator: moderator, Broker: hub, Signer: signer, Beacon: beacon, Store: store,
	}
	if err := parties.RebuildIndex(ctx); err != nil {
		return err
	}
	partyEngine := &worker.PartyEngine{Deps: deps, Parties: parties}

	devices := &service.DeviceService{Deps: deps}
	notifier := &service.NotifySender{Deps: deps, Sender: sender}
	modQueue := &service.ModerationQueueService{Deps: deps, Assessor: assessor, Broker: hub, Pins: pins}
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
		// Cloudflare Tunnel 経由では接続元がすべて手元の cloudflared（ループバック）に見える。
		// ループバックから来たリクエストに限り CF-Connecting-IP を信頼し、c.IP() を本来の接続元にする
		// （/auth/* の IP 単位のレート制限が全ユーザーで共有されないように。01 §6、02 §5）。
		ProxyHeader:      "CF-Connecting-IP",
		TrustProxy:       true,
		TrustProxyConfig: fiber.TrustProxyConfig{Loopback: true},
	})
	app.Use(recover.New())
	app.Use(cors.New(cors.Config{AllowOrigins: cfg.CORSOrigins}))

	controller.Mount(app, httpx.RequireAuth(cfg.JWTKey, deps.Clock, sessions), httpx.RateLimit(httpx.RateLimitConfig{Redis: rdb, Clock: deps.Clock}), controller.Routes{
		Root: []controller.Registrar{
			&controller.HealthController{Health: health},
			&controller.OAuthController{Config: cfg, App: oauthApp, Auth: authSvc},
			&controller.MediaDeliveryController{Media: mediaSvc},
		},
		Public: []controller.Registrar{
			&controller.AuthController{Auth: authSvc},
			&controller.WSController{
				Realtime: realtimeSvc, Hub: hub, Timers: clock.Real{},
				Config: controller.DefaultRealtimeConfig(),
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
			&controller.PartyController{Parties: parties},
			&controller.MemoryController{Parties: parties},
			&controller.AccountController{Account: account},
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
func runJetstream(ctx context.Context, deps service.Deps, svc *service.JetstreamService, url string) {
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
func moderationClients(cfg *config.Config) (service.Moderator, service.PostAssessor) {
	if cfg.NanoGPTKey == "" {
		noop := service.ModerationNoop{}
		return noop, noop
	}
	jev := &service.Jev{Client: nanogpt.NewClient(cfg.NanoGPTURL, cfg.NanoGPTKey, cfg.JevModel)}
	return jev, jev
}

// embedderFor は CHAIN_EMBED_URL の有無で Embedder を選ぶ（09 §8.3）。未設定なら nil にし、計算をしない。
func embedderFor(cfg *config.Config) service.Embedder {
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
