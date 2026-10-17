package controller

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"

	"github.com/applemango/chain/internal/clock"
	"github.com/applemango/chain/internal/httpx"
	"github.com/applemango/chain/internal/realtime"
	contentsvc "github.com/applemango/chain/internal/service/content"
)

// WebSocket の接続・認証・購読（10 §7）。
// 認証は接続後の auth メッセージで行う（トークンを URL に載せないため）。

type RealtimeConfig struct {
	AuthTimeout     time.Duration
	PingInterval    time.Duration
	PongTimeout     time.Duration
	RecheckInterval time.Duration
	TokenGrace      time.Duration
}

func DefaultRealtimeConfig() RealtimeConfig {
	return RealtimeConfig{
		AuthTimeout:     5 * time.Second,
		PingInterval:    30 * time.Second,
		PongTimeout:     10 * time.Second,
		RecheckInterval: 10 * time.Minute,
		TokenGrace:      60 * time.Second,
	}
}

type WSController struct {
	Realtime *contentsvc.RealtimeService
	Hub      *realtime.Hub
	// Timers は ping 間隔や期限のような実時間のタイマーに使う（15 §2）。時計の進め方とは独立。
	Timers clock.Clock
	Config RealtimeConfig
}

func (h *WSController) Register(r fiber.Router) {
	r.Get("/ws", websocket.New(h.handle, websocket.Config{AllowEmptyOrigin: true}))
}

type wsMessage struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Token string `json:"token"`
	Space string `json:"space"`
}

type wsRead struct {
	data []byte
	err  error
}

const (
	wsReadLimit = 64 << 10
	wsWriteWait = 10 * time.Second
)

func (h *WSController) handle(conn *websocket.Conn) {
	conn.SetReadLimit(wsReadLimit)
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	readCh := make(chan wsRead)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			select {
			case readCh <- wsRead{data: data, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	did, tokenExp, ok := h.awaitAuth(conn, readCh)
	if !ok {
		return
	}

	client := realtime.NewClient(did)
	h.Hub.Register(client)
	defer h.Hub.Unregister(client)
	client.Send(mustJSON(fiber.Map{"type": "ready", "did": did}))

	writeErr := make(chan error, 1)
	go h.writeLoop(ctx, conn, client, writeErr)

	h.readLoop(ctx, conn, client, readCh, writeErr, did, tokenExp)
}

func (h *WSController) awaitAuth(conn *websocket.Conn, readCh <-chan wsRead) (string, time.Time, bool) {
	ticker := h.Timers.NewTicker(h.Config.AuthTimeout)
	defer ticker.Stop()
	for {
		select {
		case res := <-readCh:
			if res.err != nil {
				return "", time.Time{}, false
			}
			var msg wsMessage
			if json.Unmarshal(res.data, &msg) != nil || msg.Type != "auth" || msg.Token == "" {
				continue
			}
			did, exp, err := h.Realtime.Authorize(context.Background(), msg.Token)
			if err != nil {
				_ = conn.WriteMessage(websocket.TextMessage, mustJSON(fiber.Map{"type": "error", "code": errorCode(err)}))
				return "", time.Time{}, false
			}
			return did, exp, true
		case <-ticker.C():
			return "", time.Time{}, false
		}
	}
}

func (h *WSController) readLoop(ctx context.Context, conn *websocket.Conn, client *realtime.Client, readCh <-chan wsRead, writeErr <-chan error, did string, tokenExp time.Time) {
	conn.SetReadDeadline(h.Timers.Now().Add(h.pongWait()))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(h.Timers.Now().Add(h.pongWait()))
		return nil
	})

	ping := h.Timers.NewTicker(h.Config.PingInterval)
	defer ping.Stop()
	recheck := h.Timers.NewTicker(h.Config.RecheckInterval)
	defer recheck.Stop()
	token := h.Timers.NewTicker(h.Config.TokenGrace)
	defer token.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-client.Done():
			return
		case <-writeErr:
			return
		case res := <-readCh:
			if res.err != nil {
				return
			}
			h.onMessage(ctx, client, did, res.data)
		case <-ping.C():
			if err := conn.WriteControl(websocket.PingMessage, nil, h.Timers.Now().Add(wsWriteWait)); err != nil {
				return
			}
		case <-recheck.C():
			h.recheck(ctx, client, did)
		case <-token.C():
			if !tokenExp.IsZero() && h.Realtime.Clock.Now().After(tokenExp.Add(h.Config.TokenGrace)) {
				return
			}
		}
	}
}

func (h *WSController) onMessage(ctx context.Context, client *realtime.Client, did string, data []byte) {
	var msg wsMessage
	if json.Unmarshal(data, &msg) != nil {
		return
	}
	switch msg.Type {
	case "sub":
		if msg.Space == "" {
			return
		}
		if err := h.Realtime.CheckRead(ctx, msg.Space, did); err != nil {
			client.Send(mustJSON(fiber.Map{"type": "error", "id": msg.ID, "code": errorCode(err)}))
			return
		}
		if !client.Subscribe(msg.Space) {
			client.Send(mustJSON(fiber.Map{"type": "error", "id": msg.ID, "code": "too_many_subscriptions"}))
			return
		}
		client.Send(mustJSON(fiber.Map{"type": "subscribed", "id": msg.ID, "space": msg.Space}))
	case "unsub":
		client.Unsubscribe(msg.Space)
	case "auth":
		if _, _, err := h.Realtime.Authorize(ctx, msg.Token); err != nil {
			client.Close()
		}
	}
}

func (h *WSController) recheck(ctx context.Context, client *realtime.Client, did string) {
	for _, space := range client.Subscriptions() {
		if err := h.Realtime.CheckRead(ctx, space, did); err != nil {
			client.Unsubscribe(space)
			client.Send(mustJSON(fiber.Map{"type": "unsubscribed", "space": space, "reason": "access_lost"}))
		}
	}
}

func (h *WSController) writeLoop(ctx context.Context, conn *websocket.Conn, client *realtime.Client, errCh chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-client.Done():
			return
		case payload := <-client.Messages():
			conn.SetWriteDeadline(h.Timers.Now().Add(wsWriteWait))
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
		}
	}
}

func (h *WSController) pongWait() time.Duration {
	return h.Config.PingInterval + h.Config.PongTimeout
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

func errorCode(err error) string {
	var e *httpx.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "internal"
}
