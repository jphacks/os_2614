package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/applemango/chain/internal/external/atproto"
	"github.com/applemango/chain/internal/service"
)

// Jetstream は Jetstream に常時接続し、イベントを service に渡す（03 §5）。
// 接続と再接続は atproto.Jetstream が担い、ここではカーソルの定期的な保存だけを行う。
type Jetstream struct {
	service.Deps
	Service *service.JetstreamService
	Client  *atproto.Jetstream
	// SaveInterval はカーソルを保存する間隔。0 なら service.JetstreamCursorInterval。
	SaveInterval time.Duration
}

// Run は context が終わるまで接続し続ける。main.go から goroutine で起動する。
func (w *Jetstream) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Client.Run(runCtx, w.Service.Handle) }()

	ticker := w.Clock.NewTicker(w.saveInterval())
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			w.saveCursor(context.WithoutCancel(ctx))
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		case <-ctx.Done():
			cancel()
			w.saveCursor(context.WithoutCancel(ctx))
			return ctx.Err()
		case <-ticker.C():
			w.saveCursor(ctx)
		}
	}
}

func (w *Jetstream) saveInterval() time.Duration {
	if w.SaveInterval > 0 {
		return w.SaveInterval
	}
	return service.JetstreamCursorInterval
}

// saveCursor は最後に処理した time_us を保存する（03 §5）。まだイベントが無ければ何もしない。
func (w *Jetstream) saveCursor(ctx context.Context) {
	us := w.Client.LastTimeUS()
	if us <= 0 {
		return
	}
	if err := w.Service.SaveCursor(ctx, us); err != nil {
		slog.WarnContext(ctx, "jetstream: saving cursor failed", "err", err)
	}
}
