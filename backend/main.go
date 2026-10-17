package main

import (
	"context"
	"encoding/binary"
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

	oauthApp, err := atproto.NewOAuthApp(cfg, &atproto.Store{DB: conn, Redis: rdb, SecretKey: cfg.SecretKey, Clock: deps.Clock})
	if err != nil {
		return err
	}
	if cfg.OAuthLoopback() {
		slog.Info("oauth: loopback client (dev). open http://127.0.0.1:" + cfg.ListenPort() + "/auth/login?return=dev")
	}

	sessions := &service.SessionService{Deps: deps}
	health := &service.HealthService{Deps: deps}
	authSvc := &service.AuthService{Deps: deps, OAuth: oauthApp, Dir: oauthApp.Dir, Profiles: atproto.NewProfileFetcher()}
	me := &service.MeService{Deps: deps}
	user := &service.UserService{Deps: deps}
	beacon := &service.BeaconService{Deps: deps}
	location := &service.LocationService{Deps: deps, Hooks: service.NoopLocationHooks{}}
	encounters := &service.EncounterService{Deps: deps, Proximity: &service.BLEPeerProximity{Deps: deps}, Broker: hub}
	blocks := &service.BlockService{Deps: deps}
	friends := &service.FriendService{Deps: deps, Broker: hub}
	notifications := &service.NotificationService{Deps: deps}
	revealer := &worker.EncounterRevealer{Deps: deps, Encounters: encounters}

	store := model.NewSQLiteStore(conn, deps.Clock, idgen.NewTIDGenerator(deps.Clock, tidClockID(deps.IDs)))
	mediaStore := storage.NewStore(cfg.MediaDir)
	signer := storage.NewSigner(cfg.JWTKey)
	// 位置判定は S3 が作る。ここでは常に location_stale を返す仮実装を差す。
	access := &service.AccessService{Deps: deps, Zones: &service.GPSZonePresence{Deps: deps}}
	realtimeSvc := &service.RealtimeService{Deps: deps, Access: access, Sessions: sessions}
	mediaSvc := &service.MediaService{Deps: deps, Store: store, Access: access, Broker: hub, Storage: mediaStore, Signer: signer}
	instances := &service.InstanceService{Deps: deps, Store: store, Access: access, Assessor: service.ModerationNoop{}, Broker: hub, Signer: signer}
	groups := &service.GroupService{Deps: deps, Zones: &service.GPSZonePresence{Deps: deps}, Access: access, Moderator: service.ModerationNoop{}, Broker: hub}

	app := fiber.New(fiber.Config{
		AppName:      "chain",
		ErrorHandler: httpx.ErrorHandler,
		BodyLimit:    16 << 20, // メディアの上限 15 MB（10 §4.4）に余裕を持たせる
	})
	app.Use(recover.New())
	app.Use(cors.New(cors.Config{AllowOrigins: cfg.CORSOrigins}))

	controller.Mount(app, httpx.RequireAuth(cfg.JWTKey, deps.Clock, sessions), controller.Routes{
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

	go runEvery(ctx, time.Minute, revealer.Tick)

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

// tidClockID は TID の clock id（10 bits）を起動時の乱数で決める（10 §2）。
func tidClockID(ids idgen.Generator) uint {
	return uint(binary.BigEndian.Uint16(ids.Bytes(2))) & 0x3FF
}
